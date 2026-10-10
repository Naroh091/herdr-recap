package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Codex recaps itself, as Claude does: `codex exec fork --ephemeral` forks
// the pane's session without saving anything and answers one prompt in it.
// So the conversation goes nowhere it hadn't been, and the fork reads from
// the session's own prompt cache.

// processFiles lists the files a process has open (platform_*.go). A
// variable so tests can stand one in.
var processFiles = openFiles

// paneProcs is a pane's foreground processes; a variable so tests can stand
// one in.
var paneProcs = paneProcesses

// codexSessionID is the session a Codex session file is for, from its name:
// rollout-<when>-<id>.jsonl under sessions/. "" for any other file.
func codexSessionID(path string) string {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") ||
		!strings.Contains(filepath.ToSlash(path), "/sessions/") {
		return ""
	}
	name = strings.TrimSuffix(name, ".jsonl")
	if len(name) < 36 || !sessionIDPattern.MatchString(name[len(name)-36:]) {
		return ""
	}
	return name[len(name)-36:]
}

// resolveCodexSession finds the Codex session in a pane by its process: Codex
// keeps the session's file open while it runs. herdr only knows a Codex
// session's ID with its Codex integration installed, so that's what's left
// when the file isn't found (a pane just started, with nothing said yet,
// has neither: its session comes back without a transcript).
func resolveCodexSession(a agentInfo) (claudeSession, error) {
	procs, err := paneProcs(a.PaneID)
	if err != nil {
		return claudeSession{}, err
	}
	s := claudeSession{Agent: "codex", Native: true, Cwd: a.Cwd}
	var pid int
	for i := len(procs) - 1; i >= 0 && s.Transcript == ""; i-- {
		for _, f := range processFiles(procs[i].PID) {
			if id := codexSessionID(f); id != "" {
				s.ID, s.Transcript, pid = id, f, procs[i].PID
				break
			}
		}
	}
	if pid == 0 && len(procs) > 0 {
		pid = procs[len(procs)-1].PID
	}
	env, _ := processEnv(pid)
	s.Path, s.Binary = env["PATH"], agentBinary("codex", env)
	home := env["CODEX_HOME"]
	if home != "" {
		s.ConfigDir, s.ConfigDirSet = home, true
	} else if dir, err := os.UserHomeDir(); err == nil {
		home = filepath.Join(dir, ".codex")
	}
	if id := a.AgentSession.Value; s.Transcript == "" && a.AgentSession.Kind == "id" && sessionIDPattern.MatchString(id) {
		if m, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+id+".jsonl")); len(m) > 0 {
			s.ID, s.Transcript = id, m[0]
		}
	}
	return s, nil
}

// codexPrompt asks for what Claude's /recap gives: a line on where it got to.
const codexPrompt = "Recap this session in at most 40 words, for someone coming back to it: what it's working on, where it got to, and what's next. " +
	"Answer with the recap alone, as plain text. Don't run any commands or use any tools."

// codexArgs forks the session for one answer, written to out: nothing is
// saved (--ephemeral), and nothing it might run can write (read-only).
func codexArgs(id, out string) []string {
	return []string{"exec", "fork", id, "--ephemeral", "--skip-git-repo-check",
		"-c", `sandbox_mode="read-only"`, "-o", out, codexPrompt}
}

// codexBinary is the codex a recap runs: config.json's if set, else the one
// the agent runs, else whatever "codex" is on PATH.
func codexBinary(cfg config, s claudeSession) string {
	switch {
	case cfg.Codex != "":
		return cfg.Codex
	case s.Binary != "":
		return s.Binary
	}
	return "codex"
}

// runCodex runs the fork and returns its answer; a variable so tests can
// stand one in.
var runCodex = func(ctx context.Context, cfg config, s claudeSession) (string, error) {
	dir, err := os.MkdirTemp("", "herdr-recap-codex-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "recap.txt")
	cmd := exec.CommandContext(ctx, codexBinary(cfg, s), codexArgs(s.ID, out)...)
	// Not in the agent's folder: the fork has the conversation, and has no
	// business with the project's files.
	cmd.Dir, cmd.Env = dir, agentEnv(s, "CODEX_HOME")
	cmd.WaitDelay = 5 * time.Second
	_, stderr, err := runBoundedCommand(cmd)
	if err != nil {
		if msg := strings.TrimSpace(stderr); msg != "" && !errors.Is(err, errOutputLimit) {
			return "", fmt.Errorf("%w: %s", err, shorten(clean(lastLine(msg), false), 200))
		}
		return "", err
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return "", errors.New("codex gave no recap")
	}
	return string(data), nil
}

// lastLine is where a command's error is, after whatever it logged on the way.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func runCodexRecap(ctx context.Context, cfg config, s claudeSession) (recap, error) {
	out, err := runCodex(ctx, cfg, s)
	if err != nil {
		return recap{}, fmt.Errorf("codex: %w", err)
	}
	text := strings.Join(strings.Fields(clean(out, false)), " ")
	if text == "" {
		return recap{}, errors.New("codex gave an empty recap")
	}
	return recap{Session: s.ID, Source: "codex", Text: text, At: time.Now()}, nil
}
