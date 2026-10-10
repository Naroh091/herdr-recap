package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestTicketKey(t *testing.T) {
	for branch, want := range map[string]string{
		"mrolafsson-act-1921-users-that-are-immediately-activated": "ACT-1921",
		"mrolafsson/the-30-invitations-can-be-reissued":            "THE-30",
		"ACT-7":                                  "ACT-7",
		"fix/checkout-flake":                     "",
		"main":                                   "",
		"project-phase-0-substrate-c2c2de2dae78": "",
		"phase-2-web-88-rate-limits":             "WEB-88",
		"release-3":                              "",
		"project-morning-brief-update-689e9d59115": "",
		"": "",
	} {
		if got := ticketKey(branch); got != want {
			t.Errorf("%q: %q, want %q", branch, got, want)
		}
	}
}

func TestPoorTitle(t *testing.T) {
	for _, c := range []struct {
		title, branch string
		poor          bool
	}{
		{"", "main", true},
		{"Claude Code", "main", true},
		{"fix-channelless-assignment-notify", "main", true},
		{"review_merge_before_push", "main", true},
		{"Sensor improvements", "project-sensors-overhaul-3b30ce3f1ec4", false},
		{"scratch", "main", false},
		{"PR 2 voices", "mrolafsson-act-1870-review-jira-sensors", true},
		{"act-1870 Jira sensors on a par", "mrolafsson-act-1870-review-jira-sensors", false},
		{"Phase 0 substrate", "project-phase-0-substrate-c2c2de2dae78", false},
	} {
		if got := poorTitle(c.title, c.branch); got != c.poor {
			t.Errorf("%q on %q: poor %v", c.title, c.branch, got)
		}
	}
}

func TestParseTitle(t *testing.T) {
	reply := func(s string) []byte {
		return []byte(`{"type":"result","is_error":false,"result":"` + s + `","total_cost_usd":0.01}`)
	}
	for in, want := range map[string]string{
		"ACT-1921 Activation Email for New Users":    "ACT-1921 Activation Email for New Users",
		`\"ACT-1921  Activation Email\"\n`:           "ACT-1921 Activation Email",
		"**Fix the Flaky Checkout Test.**":           "Fix the Flaky Checkout Test",
		`Here is a name:\nACT-1921 Activation Email`: "",
		"/clear": "",
		"":       "",
		"A title that goes on and on " + strings.Repeat("and on ", 8): "",
		// An escape sequence never gets as far as the agent it's typed into.
		`Fix \u001b]52;c;aGk=\u0007Login`: "Fix Login",
	} {
		got, cost, err := parseTitle(reply(in))
		if got != want || (want == "") != (err != nil) || cost != 0.01 {
			t.Errorf("%q: %q %v %v, want %q", in, got, cost, err, want)
		}
	}
	if _, _, err := parseTitle([]byte(`{"is_error":true,"result":"Not logged in"}`)); err == nil {
		t.Error("an error was taken for a title")
	}
}

// withTitles stands in a claude that answers a recap with okReply and a
// title with reply, and counts each.
func withTitles(t *testing.T, reply string) (recaps, titles *int) {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	var mu sync.Mutex
	recaps, titles = new(int), new(int)
	old := runCommand
	runCommand = func(_ context.Context, _ config, _ claudeSession, args []string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if args[len(args)-1] == "/recap" {
			*recaps++
			return []byte(okReply), nil
		}
		*titles++
		return []byte(reply), nil
	}
	t.Cleanup(func() { runCommand = old })
	return recaps, titles
}

const titleReply = `{"type":"result","is_error":false,"result":"ACT-12 Fix the Login Bug","total_cost_usd":0.004}`

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestAPoorTitleGetsABetterOneWithItsRecapOnce(t *testing.T) {
	recaps, titles := withTitles(t, titleReply)
	s := testSessionWith(t, "one\n")
	cfg := withDefaults(config{})
	r, _, err := ensureRecap(context.Background(), cfg, s, "fix-login-bug", false)
	if err != nil || r.Title != "ACT-12 Fix the Login Bug" || r.Text == "" || *titles != 1 {
		t.Fatalf("%+v %v, %d titles", r, err, *titles)
	}
	if got, want := r.CostUSD, 0.0166+0.004; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("cost %v", got)
	}
	// The conversation moves on: a new recap, the same suggestion.
	appendTo(t, s.Transcript, "two\n")
	r, _, _ = ensureRecap(context.Background(), cfg, s, "fix-login-bug", false)
	if *recaps != 2 || *titles != 1 || r.Title != "ACT-12 Fix the Login Bug" {
		t.Errorf("%d recaps, %d titles, %+v", *recaps, *titles, r)
	}
	// Turned down, it stays turned down.
	if err := settleTitle(s.ID); err != nil {
		t.Fatal(err)
	}
	appendTo(t, s.Transcript, "three\n")
	if r, _, _ = ensureRecap(context.Background(), cfg, s, "fix-login-bug", false); !r.Settled || *titles != 1 {
		t.Errorf("%+v, %d titles", r, *titles)
	}
}

func TestAGoodTitleIsLeftAlone(t *testing.T) {
	_, titles := withTitles(t, titleReply)
	s := testSessionWith(t, "one\n")
	r, _, err := ensureRecap(context.Background(), withDefaults(config{}), s, "Fix the login bug", false)
	if err != nil || r.Title != "" || *titles != 0 {
		t.Errorf("%+v %v, %d titles", r, err, *titles)
	}
}

func TestARecapDoesWithoutATitleThatFailed(t *testing.T) {
	_, titles := withTitles(t, `{"is_error":false,"result":"Sure! Here are a few ideas:\n1. Fix Login"}`)
	s := testSessionWith(t, "one\n")
	cfg := withDefaults(config{})
	r, ran, err := ensureRecap(context.Background(), cfg, s, "", false)
	if err != nil || !ran || r.Text == "" || r.Title != "" {
		t.Fatalf("%+v %v", r, err)
	}
	// Asked for again with the next recap.
	appendTo(t, s.Transcript, "two\n")
	if _, _, _ = ensureRecap(context.Background(), cfg, s, "", false); *titles != 2 {
		t.Errorf("%d titles", *titles)
	}
}

func TestARenameDoesntPutARecapOutOfDate(t *testing.T) {
	recaps, _ := withTitles(t, titleReply)
	said := `{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}` + "\n"
	s := testSessionWith(t, said)
	cfg := withDefaults(config{})
	if _, _, err := ensureRecap(context.Background(), cfg, s, "Fix login", false); err != nil {
		t.Fatal(err)
	}
	// What /rename writes, and Claude's note to itself about it.
	appendTo(t, s.Transcript, `{"type":"custom-title","customTitle":"ACT-12 Fix the Login Bug","sessionId":"x"}
{"type":"agent-name","agentName":"ACT-12 Fix the Login Bug","sessionId":"x"}
{"type":"system","subtype":"local_command","content":"<command-name>/rename</command-name>"}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<system-reminder>The user named this session</system-reminder>"}}
`)
	if _, current := (liveSource{cfg}).cached(s); !current {
		t.Error("a rename put the recap out of date")
	}
	if _, ran, _ := ensureRecap(context.Background(), cfg, s, "ACT-12 Fix the Login Bug", false); ran || *recaps != 1 {
		t.Errorf("recapped again after a rename: %d", *recaps)
	}
	// Something said is another matter.
	appendTo(t, s.Transcript, `{"type":"user","message":{"role":"user","content":"and the tests?"}}`+"\n")
	if _, current := (liveSource{cfg}).cached(s); current {
		t.Error("a prompt left the recap current")
	}
}

func TestATitleAskedForReplacesOneTurnedDown(t *testing.T) {
	_, titles := withTitles(t, titleReply)
	s := testSessionWith(t, "one\n")
	cfg := withDefaults(config{})
	// With no recap yet, the title is kept for the one to come.
	r, err := ensureTitle(context.Background(), cfg, s, "Fix login")
	if err != nil || r.Title != "ACT-12 Fix the Login Bug" || r.Text != "" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, current := (liveSource{cfg}).cached(s); current {
		t.Error("a title alone counts as a recap")
	}
	if r, _, _ := ensureRecap(context.Background(), cfg, s, "Fix login", false); r.Title != "ACT-12 Fix the Login Bug" || r.Text == "" {
		t.Errorf("%+v", r)
	}
	_ = settleTitle(s.ID)
	if r, _ := ensureTitle(context.Background(), cfg, s, "Fix login"); r.Settled || r.Text == "" || *titles != 2 {
		t.Errorf("%+v, %d titles", r, *titles)
	}
	if _, err := ensureTitle(context.Background(), cfg, claudeSession{ID: testSession}, ""); err != errNothingYet {
		t.Errorf("no conversation: %v", err)
	}
}

func TestARenameAskedOfABusyAgentIsGivenWhenItsDone(t *testing.T) {
	withTitles(t, titleReply)
	s := testSessionWith(t, "one\n")
	cfg := withDefaults(config{})
	withHerdr(t, nil, s)
	now := func(status string, seq int64) {
		agentsNow = func() ([]agentInfo, error) { return []agentInfo{claudeAgent("w1:p1", status, seq)}, nil }
	}
	if _, _, err := ensureRecap(context.Background(), cfg, s, "fix-login-bug", false); err != nil {
		t.Fatal(err)
	}
	var typed []string
	old := promptAgent
	promptAgent = func(pane, text string) error { typed = append(typed, pane+": "+text); return nil }
	t.Cleanup(func() { promptAgent = old })

	// Nothing asked for: nothing typed.
	now("idle", 6)
	if _ = tick(cfg); len(typed) != 0 {
		t.Fatalf("typed %q", typed)
	}
	if err := askRename("w1:p1", s.ID); err != nil {
		t.Fatal(err)
	}
	// Still at work: it waits. The recap written meanwhile keeps it waiting.
	now("working", 7)
	_ = tick(cfg)
	appendTo(t, s.Transcript, "two\n")
	if r, _, _ := ensureRecap(context.Background(), cfg, s, "fix-login-bug", false); !r.Renaming || len(typed) != 0 {
		t.Fatalf("%+v, typed %q", r, typed)
	}
	// Done: typed in, once.
	now("done", 8)
	_ = tick(cfg)
	_ = tick(cfg)
	if len(typed) != 1 || typed[0] != "w1:p1: /rename ACT-12 Fix the Login Bug" {
		t.Errorf("typed %q", typed)
	}
	if r := loadRecap(s.ID); r.Renaming || !r.Settled || renamePending("w1:p1") {
		t.Errorf("%+v", r)
	}
}
