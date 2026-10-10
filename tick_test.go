package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

// withHerdr stands in herdr's agent list and the panes' sessions, and records
// what tick spawns instead of spawning it.
func withHerdr(t *testing.T, agents []agentInfo, s claudeSession) *[][]string {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", "")
	oldAgents, oldSession, oldSpawn := agentsNow, sessionOf, spawn
	agentsNow = func() ([]agentInfo, error) { return agents, nil }
	sessionOf = func(context.Context, config, agentInfo) (claudeSession, error) { return s, nil }
	var spawned [][]string
	spawn = func(args ...string) error { spawned = append(spawned, args); return nil }
	t.Cleanup(func() { agentsNow, sessionOf, spawn = oldAgents, oldSession, oldSpawn })
	return &spawned
}

func claudeAgent(pane, status string, seq int64) agentInfo {
	return agentInfo{PaneID: pane, Agent: "claude", Status: status, StateChangeSeq: seq, Title: "Fix login"}
}

func TestTickSchedulesAgentsWaitingOnYou(t *testing.T) {
	spawned := withHerdr(t, []agentInfo{
		claudeAgent("w1:p1", "done", 5),
		claudeAgent("w1:p2", "blocked", 2),
		claudeAgent("w1:p3", "working", 9),
		claudeAgent("w1:p4", "idle", 4),
		{PaneID: "w1:p5", Agent: "codex", Status: "done", StateChangeSeq: 1, AgentSession: agentSession{Kind: "id", Value: "codex-session"}},
	}, claudeSession{})
	if err := tick(withDefaults(config{RecapAfterSeconds: 240, MemexAgents: []string{"codex"}})); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"recap", "--after", "240", "--seq", "5", "w1:p1"},
		{"recap", "--after", "240", "--seq", "2", "w1:p2"},
		{"recap", "--after", "240", "--seq", "1", "w1:p5"},
	}
	if !slices.EqualFunc(*spawned, want, slices.Equal) {
		t.Errorf("spawned %v", *spawned)
	}
}

func TestTickSchedulesAStatusChangeOnce(t *testing.T) {
	// herdr can send several events for one change; the recap is one.
	agents := []agentInfo{claudeAgent("w1:p1", "done", 5)}
	spawned := withHerdr(t, agents, claudeSession{})
	cfg := withDefaults(config{})
	for range 3 {
		if err := tick(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if len(*spawned) != 1 {
		t.Errorf("spawned %v", *spawned)
	}
	// A later status change is another recap.
	agents[0].StateChangeSeq = 7
	if err := tick(cfg); err != nil || len(*spawned) != 2 {
		t.Errorf("spawned %v, %v", *spawned, err)
	}
}

func TestTickOnlyLooksAtTheEventsPane(t *testing.T) {
	spawned := withHerdr(t, []agentInfo{claudeAgent("w1:p1", "done", 5), claudeAgent("w1:p2", "done", 3)}, claudeSession{})
	t.Setenv("HERDR_PLUGIN_EVENT_JSON", `{"type":"pane.agent_status_changed","pane_id":"w1:p2"}`)
	if err := tick(withDefaults(config{})); err != nil {
		t.Fatal(err)
	}
	if len(*spawned) != 1 || (*spawned)[0][5] != "w1:p2" {
		t.Errorf("spawned %v", *spawned)
	}
}

func TestAFailedSpawnCanBeRetried(t *testing.T) {
	spawned := withHerdr(t, []agentInfo{claudeAgent("w1:p1", "done", 5)}, claudeSession{})
	spawn = func(...string) error { return errors.New("no") }
	cfg := withDefaults(config{})
	_ = tick(cfg)
	spawn = func(args ...string) error { *spawned = append(*spawned, args); return nil }
	_ = tick(cfg)
	if len(*spawned) != 1 {
		t.Errorf("spawned %v", *spawned)
	}
}

func TestALaterRecapOnlyIfYouStillHaventLooked(t *testing.T) {
	cfg := withDefaults(config{})
	for _, c := range []struct {
		name  string
		now   []agentInfo
		recap bool
	}{
		{"still done from the same change", []agentInfo{claudeAgent("w1:p1", "done", 5)}, true},
		{"still blocked", []agentInfo{claudeAgent("w1:p1", "blocked", 5)}, true},
		{"you looked: idle", []agentInfo{claudeAgent("w1:p1", "idle", 6)}, false},
		{"you answered and it went on", []agentInfo{claudeAgent("w1:p1", "working", 6)}, false},
		{"done again, from a later change", []agentInfo{claudeAgent("w1:p1", "done", 8)}, false},
		{"the pane closed", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			withHerdr(t, c.now, claudeSession{})
			s := testSessionWith(t, "one\n")
			sessionOf = func(context.Context, config, agentInfo) (claudeSession, error) { return s, nil }
			calls := withClaude(t, okReply)
			if err := recapLater(context.Background(), cfg, 0, 5, "w1:p1"); err != nil {
				t.Fatal(err)
			}
			if got := calls.Load() == 1; got != c.recap {
				t.Errorf("recapped: %v", got)
			}
		})
	}
}

func TestALaterRecapClearsItsMark(t *testing.T) {
	a := claudeAgent("w1:p1", "done", 5)
	withHerdr(t, []agentInfo{a}, claudeSession{})
	cfg := withDefaults(config{})
	if err := tick(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scheduledPath(a)); err != nil {
		t.Fatal("not marked as scheduled")
	}
	withClaude(t, okReply)
	_ = recapLater(context.Background(), cfg, 0, 5, "w1:p1")
	if _, err := os.Stat(scheduledPath(a)); err == nil {
		t.Error("the mark outlived the recap")
	}
}

func TestHookPane(t *testing.T) {
	for in, want := range map[string]string{
		`{"type":"pane.agent_status_changed","pane_id":"w1:p2"}`:           "w1:p2",
		`{"event":"pane.agent_status_changed","data":{"pane_id":"w3:p1"}}`: "w3:p1",
		``:         "",
		`garbage`:  "",
		`{"x": 1}`: "",
	} {
		t.Setenv("HERDR_PLUGIN_EVENT_JSON", in)
		if got := hookPane(); got != want {
			t.Errorf("%s: got %q", in, got)
		}
	}
}
