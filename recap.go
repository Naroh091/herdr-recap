package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// recap is one session's recap as cached, with the transcript as it was just
// before the recap was asked for: the recap is current while the transcript
// hasn't changed since.
type recap struct {
	Session string    `json:"session"`
	Source  string    `json:"source,omitempty"`
	Stamp   string    `json:"stamp,omitempty"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	CostUSD float64   `json:"cost_usd"`
	// Title is a better title for the session than the one it had when the
	// recap was written, if that one was poor. Renaming: the agent is to be
	// given it, once it's between turns. Settled: it's been given or turned
	// down, and isn't offered again.
	Title    string `json:"title,omitempty"`
	Renaming bool   `json:"renaming,omitempty"`
	Settled  bool   `json:"title_settled,omitempty"`
}

func recapDir() string { return filepath.Join(stateDir(), "recaps") }

func recapPath(sessionID string) string {
	return filepath.Join(recapDir(), sessionID+".json")
}

// loadRecap is the cached recap for a session, or nil.
func loadRecap(sessionID string) *recap {
	data, err := os.ReadFile(recapPath(sessionID))
	if err != nil {
		return nil
	}
	var r recap
	if json.Unmarshal(data, &r) != nil || r.Session != sessionID {
		return nil
	}
	sanitize(&r)
	return &r
}

func saveRecap(r recap) error {
	if err := os.MkdirAll(recapDir(), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := recapPath(r.Session) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, recapPath(r.Session))
}

// transcriptStamp is what a recap's freshness is judged by.
func transcriptStamp(path string) (int64, time.Time) {
	if path == "" {
		return 0, time.Time{}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}
	}
	return fi.Size(), fi.ModTime()
}

// fresh: the cached recap still describes the transcript, which hasn't
// changed since, or only by what isn't conversation (a rename, say).
func fresh(r *recap, s claudeSession) bool {
	if r == nil {
		return false
	}
	if !claudes(s) && !s.Native {
		return r.Source == s.Agent && r.Stamp != "" && r.Stamp == s.Stamp
	}
	size, mod := transcriptStamp(s.Transcript)
	if r.Size == size && r.ModTime.Equal(mod) {
		return true
	}
	// Only Claude's transcripts are read for what was added since.
	return claudes(s) && r.Size > 0 && size > r.Size && quietSince(s.Transcript, r.Size)
}

// claudes: the session is Claude's own, recapped by Claude itself. Another
// agent's is recapped by that agent (Native: Codex) or read through Memex.
func claudes(s claudeSession) bool { return s.Agent == "" || s.Agent == "claude" }

// errNothingYet: the session has no conversation to recap.
var errNothingYet = errors.New("nothing to recap yet")

// ensureRecap returns a current recap for s, writing one (ran) if the cached
// one is out of date, or with force regardless. One recap per session runs
// at a time: a second caller waits for the first and takes its result rather
// than paying twice, unless it forces, when it gets a new one after.
//
// title is the agent's title as herdr shows it. A poor one (poorTitle) has a
// better one asked for alongside the recap, once per session: the suggestion
// is kept from recap to recap.
func ensureRecap(ctx context.Context, cfg config, s claudeSession, title string, force bool) (r recap, ran bool, err error) {
	ctx, cancel := recapContext(ctx, cfg)
	defer cancel()
	if s.Transcript == "" {
		return recap{}, false, errNothingYet
	}
	started := time.Now()
	unlock, err := lockSession(ctx, s.ID)
	if err != nil {
		return recap{}, false, err
	}
	defer unlock()
	c := loadRecap(s.ID)
	if fresh(c, s) && (!force || c.At.After(started)) {
		return *c, false, nil
	}
	size, mod := transcriptStamp(s.Transcript)
	type suggestion struct {
		title string
		cost  float64
		err   error
	}
	// At the same time as the recap: both read the same cached conversation.
	var suggested chan suggestion
	if claudes(s) && (c == nil || c.Title == "") && poorTitle(title, gitBranch(s.Cwd)) {
		suggested = make(chan suggestion, 1)
		go func() {
			t, cost, err := suggestTitle(ctx, cfg, s, title)
			suggested <- suggestion{t, cost, err}
		}()
	}
	r, err = runRecap(ctx, cfg, s)
	if c != nil {
		r.Title, r.Renaming, r.Settled = c.Title, c.Renaming, c.Settled
	}
	if suggested != nil {
		// Without one the recap is as good: it's asked for again with the next.
		sg := <-suggested
		if sg.err != nil {
			fmt.Fprintln(os.Stderr, "herdr-recap: a title for", s.ID+":", sg.err)
		}
		r.Title, r.Renaming, r.CostUSD = sg.title, false, r.CostUSD+sg.cost
	}
	if err != nil {
		return recap{}, false, err
	}
	r.Size, r.ModTime = size, mod
	return r, true, saveRecap(r)
}

// lockSession holds the session's lock file until unlock is called.
func lockSession(ctx context.Context, id string) (func(), error) {
	if err := os.MkdirAll(recapDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(recapDir(), id+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// recapArgs asks Claude Code for its own recap of the session (/recap, the
// same one-line summary it shows when you come back to it), in a fork that is
// never saved: the agent's conversation is only read, never added to.
func recapArgs(id string) []string {
	return []string{"-p", "--resume", id, "--fork-session", "--no-session-persistence", "--output-format", "json", "/recap"}
}

// recapEnv is the plugin's environment for the recap, less anything that
// would tie it to herdr: with HERDR_* set, herdr's Claude hooks would report
// the recap to herdr as an agent in the pane the plugin runs in. It gets the
// agent's CLAUDE_CONFIG_DIR, so it finds the agent's sessions and account,
// and the agent's PATH, which herdr's own often isn't (no ~/.local/bin).
func recapEnv(s claudeSession) []string { return agentEnv(s, "CLAUDE_CONFIG_DIR") }

// agentEnv is recapEnv for any agent: configVar names the variable its
// config directory is given in (CLAUDE_CONFIG_DIR, CODEX_HOME).
func agentEnv(s claudeSession, configVar string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HERDR_") || strings.HasPrefix(kv, configVar+"=") ||
			(s.Path != "" && strings.HasPrefix(kv, "PATH=")) {
			continue
		}
		env = append(env, kv)
	}
	if s.ConfigDirSet {
		env = append(env, configVar+"="+s.ConfigDir)
	}
	if s.Path != "" {
		env = append(env, "PATH="+s.Path)
	}
	return env
}

// runCommand runs claude on the session, for its recap or a title; a
// variable so tests can stand one in.
var runCommand = func(ctx context.Context, cfg config, s claudeSession, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeBinary(cfg, s), args...)
	cmd.Dir, cmd.Env = s.Cwd, recapEnv(s)
	// Past the timeout, don't wait on pipes a hook's child may still hold:
	// the session's lock would be held with them.
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, shorten(clean(msg, false), 200))
		}
		return nil, err
	}
	return out, nil
}

// claudeBinary is the claude a recap runs: config.json's if set, else the one
// the agent runs, else whatever "claude" is on PATH.
func claudeBinary(cfg config, s claudeSession) string {
	switch {
	case cfg.Claude != "":
		return cfg.Claude
	case s.Binary != "":
		return s.Binary
	}
	return "claude"
}

func runRecap(ctx context.Context, cfg config, s claudeSession) (recap, error) {
	switch {
	case s.Native:
		return runCodexRecap(ctx, cfg, s)
	case !claudes(s):
		return runMemexRecap(ctx, cfg, s)
	}
	out, err := runCommand(ctx, cfg, s, recapArgs(s.ID))
	if err != nil {
		return recap{}, err
	}
	text, cost, err := parseRecap(out)
	if err != nil {
		return recap{}, err
	}
	return recap{Session: s.ID, Text: text, At: time.Now(), CostUSD: cost}, nil
}

// claudeResult reads `claude -p --output-format json`'s result: what it
// said, cleaned, and what it cost.
func claudeResult(out []byte, keepBreaks bool) (text string, cost float64, isError bool, err error) {
	var res struct {
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &res); err != nil {
		return "", 0, false, fmt.Errorf("claude's reply: %w", err)
	}
	return clean(res.Result, keepBreaks), res.Cost, res.IsError, nil
}

func parseRecap(out []byte) (string, float64, error) {
	text, cost, isError, err := claudeResult(out, false)
	if err != nil {
		return "", 0, err
	}
	text = strings.Join(strings.Fields(text), " ")
	switch {
	case isError:
		return "", cost, fmt.Errorf("claude: %s", shorten(text, 200))
	case strings.HasPrefix(text, "Nothing to recap yet"):
		return "", cost, errNothingYet
	case strings.HasPrefix(text, "Couldn't generate a recap"), strings.HasPrefix(text, "Recap cancelled"):
		return "", cost, errors.New(text)
	case text == "":
		return "", cost, errors.New("claude gave an empty recap")
	}
	return text, cost, nil
}
