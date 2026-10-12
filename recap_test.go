package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withClaude stands in a claude that answers every recap with reply, and
// counts the calls.
func withClaude(t *testing.T, reply string) *atomic.Int32 {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var calls atomic.Int32
	old := runCommand
	runCommand = func(context.Context, config, claudeSession, []string) ([]byte, error) {
		calls.Add(1)
		return []byte(reply), nil
	}
	t.Cleanup(func() { runCommand = old })
	return &calls
}

func testSessionWith(t *testing.T, transcript string) claudeSession {
	t.Helper()
	path := filepath.Join(t.TempDir(), testSession+".jsonl")
	write(t, path, transcript)
	return claudeSession{ID: testSession, Cwd: "/x", Transcript: path}
}

const okReply = `{"type":"result","subtype":"success","is_error":false,"result":"Fixing the login bug. Next, run the tests.","total_cost_usd":0.0166}`

func TestARecapIsWrittenOnceWhileTheConversationStandsStill(t *testing.T) {
	calls := withClaude(t, okReply)
	s := testSessionWith(t, "one\n")
	cfg := withDefaults(config{})
	r, ran, err := ensureRecap(context.Background(), cfg, s, "Fix login", false)
	if err != nil || !ran || r.Text != "Fixing the login bug. Next, run the tests." || r.CostUSD != 0.0166 {
		t.Fatalf("%+v %v %v", r, ran, err)
	}
	if _, ran, _ := ensureRecap(context.Background(), cfg, s, "Fix login", false); ran || calls.Load() != 1 {
		t.Errorf("unchanged conversation recapped again: %d calls", calls.Load())
	}
	if c, current := (liveSource{cfg}).cached(s); c == nil || !current {
		t.Error("the recap wasn't cached as current")
	}
	// The conversation moves on: out of date, recapped again.
	f, _ := os.OpenFile(s.Transcript, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("two\n")
	f.Close()
	if _, current := (liveSource{cfg}).cached(s); current {
		t.Error("a grown transcript's recap still counts as current")
	}
	if _, ran, _ := ensureRecap(context.Background(), cfg, s, "Fix login", false); !ran || calls.Load() != 2 {
		t.Errorf("changed conversation not recapped: %d calls", calls.Load())
	}
	// Force: again, even though it's current.
	if _, ran, _ := ensureRecap(context.Background(), cfg, s, "Fix login", true); !ran || calls.Load() != 3 {
		t.Errorf("force didn't recap: %d calls", calls.Load())
	}
}

func TestNothingSaidYetCostsNothing(t *testing.T) {
	calls := withClaude(t, okReply)
	_, _, err := ensureRecap(context.Background(), withDefaults(config{}), claudeSession{ID: testSession}, "Fix login", false)
	if !errors.Is(err, errNothingYet) || calls.Load() != 0 {
		t.Errorf("%v, %d calls", err, calls.Load())
	}
}

func TestConcurrentRecapsOfOneSessionRunOnce(t *testing.T) {
	calls := withClaude(t, okReply)
	s := testSessionWith(t, "one\n")
	cfg := withDefaults(config{})
	done := make(chan error)
	for range 4 {
		go func() {
			_, _, err := ensureRecap(context.Background(), cfg, s, "Fix login", false)
			done <- err
		}()
	}
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("%d recaps of one unchanged conversation", calls.Load())
	}
}

func TestRecapDeadlineIncludesLockWait(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	s := testSessionWith(t, "one\n")
	unlock, err := lockSession(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	started := time.Now()
	_, _, err = ensureRecap(context.Background(), config{TimeoutSeconds: 1}, s, "Fix login", false)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("lock wait exceeded recap deadline: %v", elapsed)
	}
}

func TestParseRecap(t *testing.T) {
	for _, c := range []struct {
		name, out, want string
		err             error
	}{
		{"a recap", okReply, "Fixing the login bug. Next, run the tests.", nil},
		{"spacing and escapes cleaned", `{"result":"  Two\nlines \u001b]52;c;aGk=\u0007here  "}`, "Two lines here", nil},
		{"nothing yet", `{"result":"Nothing to recap yet — send a message first."}`, "", errNothingYet},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := parseRecap([]byte(c.out))
			if got != c.want || !errors.Is(err, c.err) {
				t.Errorf("got %q, %v", got, err)
			}
		})
	}
	for _, out := range []string{
		`{"is_error":true,"result":"Invalid API key"}`,
		`{"result":"Couldn't generate a recap. Run with --debug for details."}`,
		`{"result":"   "}`,
		`not json`,
	} {
		if _, _, err := parseRecap([]byte(out)); err == nil {
			t.Errorf("%s: no error", out)
		}
	}
}

func TestTheRecapOnlyReadsTheConversation(t *testing.T) {
	// A fork that isn't saved: the agent's own session is never written to.
	args := recapArgs(testSession)
	for _, want := range []string{"--fork-session", "--no-session-persistence", "--resume"} {
		if !slices.Contains(args, want) {
			t.Errorf("%v lacks %s", args, want)
		}
	}
	if args[len(args)-1] != "/recap" {
		t.Errorf("%v", args)
	}
}

func TestTheRecapIsntTiedToHerdr(t *testing.T) {
	// herdr's Claude hooks would report the recap as an agent in the
	// plugin's pane.
	t.Setenv("HERDR_PANE_ID", "w1:p9")
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("CLAUDE_CONFIG_DIR", "/plugins/own")
	t.Setenv("PATH", "/usr/bin:/bin")
	env := recapEnv(claudeSession{ConfigDir: "/profiles/me", ConfigDirSet: true, Path: "/home/me/.local/bin:/usr/bin"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "HERDR_") || kv == "CLAUDE_CONFIG_DIR=/plugins/own" {
			t.Errorf("passed on %s", kv)
		}
	}
	if !slices.Contains(env, "CLAUDE_CONFIG_DIR=/profiles/me") {
		t.Error("the agent's config dir wasn't passed on")
	}
	if !slices.Contains(env, "PATH=/home/me/.local/bin:/usr/bin") || slices.Contains(env, "PATH=/usr/bin:/bin") {
		t.Error("the recap should run with the agent's PATH, not herdr's")
	}
	for _, kv := range recapEnv(claudeSession{ConfigDir: "/home/me/.claude"}) {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			t.Errorf("an agent without CLAUDE_CONFIG_DIR got %s", kv)
		}
	}
}

func TestACachedRecapIsCleaned(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	if err := saveRecap(recap{Session: testSession, Text: "hi \x1b]0;pwned\x07there", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if r := loadRecap(testSession); r == nil || r.Text != "hi there" {
		t.Errorf("%+v", r)
	}
	if loadRecap("0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e01") != nil {
		t.Error("another session's recap")
	}
}
