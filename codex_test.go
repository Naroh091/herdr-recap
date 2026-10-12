package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const codexNative = "01a127cd-74f5-7890-b082-ecbdc6eee4fa"

func TestCodexSessionID(t *testing.T) {
	for path, want := range map[string]string{
		"/home/me/.codex/sessions/2026/10/10/rollout-2026-10-10T23-52-18-" + codexNative + ".jsonl": codexNative,
		"/home/me/.codex/sessions/2026/10/10/rollout-2026-10-10T23-52-18-not-a-session-id.jsonl":    "",
		"/home/me/notes/rollout-2026-10-10T23-52-18-" + codexNative + ".jsonl":                      "",
		"/home/me/.codex/sessions/2026/10/10/" + codexNative + ".jsonl":                             "",
		"/home/me/.codex/logs_2.sqlite":                                                             "",
		"":                                                                                          "",
	} {
		if got := codexSessionID(path); got != want {
			t.Errorf("%q: %q, want %q", path, got, want)
		}
	}
}

// withCodexPane stands in a pane whose processes have these files open.
func withCodexPane(t *testing.T, files map[int][]string, env map[string]string) {
	t.Helper()
	oldProcs, oldFiles, oldEnv := paneProcs, processFiles, processEnv
	paneProcs = func(string) ([]process, error) {
		var procs []process
		for pid := range files {
			procs = append(procs, process{PID: pid})
		}
		slices.SortFunc(procs, func(a, b process) int { return a.PID - b.PID })
		return procs, nil
	}
	processFiles = func(pid int) []string { return files[pid] }
	processEnv = func(int) (map[string]string, error) { return env, nil }
	t.Cleanup(func() { paneProcs, processFiles, processEnv = oldProcs, oldFiles, oldEnv })
}

func TestACodexSessionIsFoundFromItsProcess(t *testing.T) {
	rollout := "/home/me/.codex/sessions/2026/10/10/rollout-2026-10-10T23-52-18-" + codexNative + ".jsonl"
	withCodexPane(t, map[int][]string{
		10: {"/dev/pts/3"}, // the wrapper it was started by
		11: {"/dev/pts/3", "/home/me/.codex/logs_2.sqlite", rollout},
	}, map[string]string{"PATH": "/opt/bin", "CODEX_HOME": "/profiles/work/codex"})
	s, err := resolveAgentSession(context.Background(), withDefaults(config{}), agentInfo{PaneID: "w1:p3", Agent: "codex", Cwd: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Native || s.Agent != "codex" || s.ID != codexNative || s.Transcript != rollout || s.Cwd != "/repo" {
		t.Errorf("%+v", s)
	}
	// The recap runs as the pane's Codex does.
	if env := agentEnv(s, "CODEX_HOME"); !slices.Contains(env, "PATH=/opt/bin") || !slices.Contains(env, "CODEX_HOME=/profiles/work/codex") {
		t.Errorf("%v", env)
	}
}

func TestACodexPaneWithNothingSaidHasNoTranscript(t *testing.T) {
	withCodexPane(t, map[int][]string{11: {"/dev/pts/3"}}, nil)
	s, err := resolveCodexSession(agentInfo{PaneID: "w1:p3", Agent: "codex"})
	if err != nil || s.Transcript != "" || !s.Native {
		t.Fatalf("%+v %v", s, err)
	}
	if _, _, err := ensureRecap(context.Background(), withDefaults(config{}), s, "", false); !errors.Is(err, errNothingYet) {
		t.Errorf("recapped nothing: %v", err)
	}
}

func TestHerdrsCodexSessionIDFindsTheFileWhenTheProcessDoesnt(t *testing.T) {
	home := t.TempDir()
	rollout := filepath.Join(home, "sessions", "2026", "10", "10", "rollout-2026-10-10T23-52-18-"+codexNative+".jsonl")
	write(t, rollout, "{}\n")
	withCodexPane(t, map[int][]string{11: nil}, map[string]string{"CODEX_HOME": home})
	a := agentInfo{PaneID: "w1:p3", Agent: "codex", AgentSession: agentSession{Source: "herdr:codex", Kind: "id", Value: codexNative}}
	if s, err := resolveCodexSession(a); err != nil || s.Transcript != rollout || s.ID != codexNative {
		t.Fatalf("%+v %v", s, err)
	}
	// An ID that isn't one never reaches the file system.
	a.AgentSession.Value = "../../*"
	if s, _ := resolveCodexSession(a); s.Transcript != "" {
		t.Errorf("%+v", s)
	}
}

func TestCodexRecapsItselfOnceWhileItsSessionStandsStill(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	calls := 0
	oldCodex, oldClaude := runCodex, runCommand
	runCodex = func(_ context.Context, _ config, s claudeSession, prompt string) (string, error) {
		if prompt != codexPrompt {
			return "CDX-7 Fix the Login Bug\n", nil
		}
		calls++
		return "Fixing the login bug.\nNext, run \x1b[2Jthe tests.\n", nil
	}
	runCommand = func(context.Context, config, claudeSession, []string) ([]byte, error) {
		t.Error("claude was run on a Codex session")
		return nil, errors.New("no")
	}
	t.Cleanup(func() { runCodex, runCommand = oldCodex, oldClaude })
	path := filepath.Join(t.TempDir(), "rollout-2026-10-10T23-52-18-"+codexNative+".jsonl")
	write(t, path, `{"type":"session_meta"}`+"\n")
	s := claudeSession{Agent: "codex", Native: true, ID: codexNative, Transcript: path}
	cfg := withDefaults(config{})
	// A slug for a title: Codex is asked for a better one too, in its own fork.
	r, ran, err := ensureRecap(context.Background(), cfg, s, "a-slug-for-a-title", false)
	if err != nil || !ran || r.Text != "Fixing the login bug. Next, run the tests." || r.Title != "CDX-7 Fix the Login Bug" {
		t.Fatalf("%+v %v %v", r, ran, err)
	}
	if _, ran, _ := ensureRecap(context.Background(), cfg, s, "", false); ran || calls != 1 {
		t.Errorf("recapped again: %d calls", calls)
	}
	// Anything added puts it out of date: a Codex session file isn't read
	// for whether it was conversation, as Claude's is.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"event_msg"}` + "\n")
	f.Close()
	if _, ran, _ := ensureRecap(context.Background(), cfg, s, "", false); !ran || calls != 2 {
		t.Errorf("not recapped after the session moved on: %d calls", calls)
	}
}

func TestCodexArgsForkWithoutSavingOrWriting(t *testing.T) {
	args := codexArgs(codexNative, "/tmp/out.txt", codexPrompt)
	for _, want := range []string{"fork", codexNative, "--ephemeral", `sandbox_mode="read-only"`} {
		if !slices.Contains(args, want) {
			t.Errorf("args lack %q: %v", want, args)
		}
	}
	if got := codexBinary(withDefaults(config{}), claudeSession{}); got != "codex" {
		t.Errorf("binary %q", got)
	}
}

func TestCodexIsRecappedWithoutBeingAskedFor(t *testing.T) {
	codex := agentInfo{Agent: "codex", Status: "done"} // no session ID from herdr: found from its process
	opencode := agentInfo{Agent: "opencode", Status: "done"}
	if !recapCapable(withDefaults(config{}), codex) {
		t.Error("codex isn't recapped by default")
	}
	if recapCapable(withDefaults(config{Agents: []string{"claude"}}), codex) {
		t.Error("codex, left out of agents, is recapped")
	}
	named := withDefaults(config{Agents: []string{"claude", "codex", "opencode"}})
	if recapCapable(named, opencode) {
		t.Error("opencode without a session ID for Memex to find")
	}
}

func TestACodexTitleIsItsSessionsNotItsFolders(t *testing.T) {
	for _, c := range []struct {
		a    agentInfo
		want string
	}{
		{agentInfo{Agent: "codex", Title: "Respond to greeting | herdr-recap", Cwd: "/x/herdr-recap"}, "Respond to greeting"},
		{agentInfo{Agent: "codex", Title: "A | B | herdr-recap", Cwd: "/x/herdr-recap"}, "A | B"},
		{agentInfo{Agent: "codex", Title: "herdr-recap", Cwd: "/x/herdr-recap"}, ""},
		{agentInfo{Agent: "claude", Title: "Fix this | that", Cwd: "/x/herdr-recap"}, "Fix this | that"},
	} {
		if got := sessionTitle(c.a); got != c.want {
			t.Errorf("%q: %q, want %q", c.a.Title, got, c.want)
		}
	}
	// Renamed to what was suggested, it's not suggested again.
	e := &entry{agent: agentInfo{Agent: "codex", Title: "CDX-7 Fix the Login Bug | repo"}, recap: &recap{Title: "CDX-7 Fix the Login Bug"}}
	if suggestion(e) != "" {
		t.Error("a title the agent already has is still on offer")
	}
}
