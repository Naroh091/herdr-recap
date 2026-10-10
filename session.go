package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// claudeSession is where an agent's conversation lives, and what
// `claude --resume` needs to pick it up.
type claudeSession struct {
	ID           string
	Agent        string
	Stamp        string
	MessageCount int
	// Deadline is the end of the whole lookup-and-recap operation. Memex
	// resolution, lock acquisition, transcript paging, and summarization share it.
	Deadline time.Time
	// ConfigDir is the agent's CLAUDE_CONFIG_DIR, or ~/.claude. Profile
	// switchers (clauth and the like) give each profile its own, so it's read
	// from the agent's process rather than assumed.
	ConfigDir string
	// ConfigDirSet: the agent had CLAUDE_CONFIG_DIR set, so the recap sets it too.
	ConfigDirSet bool
	// Binary is the claude the agent runs, which the recap runs too: herdr's
	// server often has a bare PATH (no ~/.local/bin), so "claude" alone
	// isn't found from a hook or the popup.
	Binary string
	// Path is the agent's PATH: the recap runs with it, so claude and what
	// it needs (node, for an npm install) are found as they were for the agent.
	Path string
	// Cwd is where the session was started: --resume looks sessions up by it.
	Cwd        string
	Transcript string // its .jsonl, "" when nothing has been said yet
	// Meta is what the transcript says about the session (the popup reads
	// it; recaps don't need it).
	Meta sessionMeta
}

var errNotClaude = errors.New("no Claude process in this pane")
var errSessionNotIndexed = errors.New("session is not indexed by Memex")

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

// processEnv reads a process's environment (platform_*.go). A variable so
// tests can stand one in.
var processEnv = readProcessEnv

// resolveSession finds the Claude session running in a pane. herdr reports a
// session ID of its own for Claude panes, but under a profile switcher it
// needn't be the one Claude writes its transcript under, so this goes by the
// process instead: the pane's claude process, its CLAUDE_CONFIG_DIR, and the
// sessions/<pid>.json Claude keeps there for each running session.
func resolveSession(paneID string) (claudeSession, error) {
	procs, err := paneProcesses(paneID)
	if err != nil {
		return claudeSession{}, err
	}
	pid, argv0 := claudePID(procs)
	if pid == 0 {
		return claudeSession{}, errNotClaude
	}
	return sessionForPID(pid, argv0)
}

// claudePID picks the claude process out of a pane's foreground processes: the
// last one, as a wrapper (a profile switcher, say) comes before what it runs.
// argv0 is what it was started as, when that was claude ("" when it was run
// some other way, under node say).
func claudePID(procs []process) (pid int, argv0 string) {
	for _, p := range procs {
		name := p.Name
		if len(p.Argv) > 0 {
			name = filepath.Base(p.Argv[0])
		}
		if name == "claude" || p.Name == "claude" {
			pid, argv0 = p.PID, ""
			if len(p.Argv) > 0 && filepath.Base(p.Argv[0]) == "claude" {
				argv0 = p.Argv[0]
			}
		}
	}
	return pid, argv0
}

// agentBinary is the claude the agent was started as: argv0 itself when it's
// a path, else the first claude on the agent's own PATH, as its shell found it.
func agentBinary(argv0 string, env map[string]string) string {
	if argv0 == "" || strings.Contains(argv0, "/") {
		return argv0
	}
	for _, dir := range filepath.SplitList(env["PATH"]) {
		p := filepath.Join(dir, argv0)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

func sessionForPID(pid int, argv0 string) (claudeSession, error) {
	env, err := processEnv(pid)
	if err != nil {
		return claudeSession{}, fmt.Errorf("reading claude's environment: %w", err)
	}
	s := claudeSession{Binary: agentBinary(argv0, env), Path: env["PATH"]}
	if dir, ok := env["CLAUDE_CONFIG_DIR"]; ok && dir != "" {
		s.ConfigDir, s.ConfigDirSet = dir, true
	} else {
		home, _ := os.UserHomeDir()
		s.ConfigDir = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(s.ConfigDir, "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return claudeSession{}, fmt.Errorf("claude's session file: %w", err)
	}
	var f struct {
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return claudeSession{}, fmt.Errorf("claude's session file: %w", err)
	}
	if !sessionIDPattern.MatchString(f.SessionID) {
		return claudeSession{}, fmt.Errorf("claude's session file has no session ID")
	}
	s.ID, s.Cwd = f.SessionID, f.Cwd
	s.Transcript = findTranscript(s.ConfigDir, s.Cwd, s.ID)
	return s, nil
}

var notAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// findTranscript is where Claude keeps a session's conversation:
// projects/<cwd with every other character a dash>/<id>.jsonl, or, for a
// session started elsewhere, the same name under another project.
func findTranscript(configDir, cwd, id string) string {
	projects := filepath.Join(configDir, "projects")
	p := filepath.Join(projects, notAlnum.ReplaceAllString(cwd, "-"), id+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if m, _ := filepath.Glob(filepath.Join(projects, "*", id+".jsonl")); len(m) > 0 {
		return m[0]
	}
	return ""
}
