package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestAnAgentsStageComesFromItsStatusThenItsPR(t *testing.T) {
	pr := func(kv ...string) map[string]string {
		t := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			t[kv[i]] = kv[i+1]
		}
		return t
	}
	for _, c := range []struct {
		name    string
		status  string
		tokens  map[string]string
		changes gitChanges
		want    stage
	}{
		{"asking", "blocked", pr("pr_state", "merged"), gitChanges{}, stageNeedsYou},
		{"a turn you haven't seen", "done", pr("pr_state", "merged"), gitChanges{}, stageDone},
		{"on the next thing after a merge", "working", pr("pr_state", "merged"), gitChanges{}, stageWorking},
		{"no PR", "idle", nil, gitChanges{}, stageIdle},
		{"a PR with no state (not herdr-github's)", "idle", pr("pr", "#415"), gitChanges{}, stageIdle},
		{"a draft", "idle", pr("pr_state", "draft", "pr_checks", "✓"), gitChanges{}, stageIdle},
		{"a failing draft", "idle", pr("pr_state", "draft", "pr_checks", "✗"), gitChanges{}, stageFixing},
		{"open, checks running", "idle", pr("pr_state", "open", "pr_checks", "●"), gitChanges{}, stageReview},
		{"open, with work to push", "idle", pr("pr_state", "open"), gitChanges{Files: 2, Ahead: 1}, stageReview},
		{"open, failing", "idle", pr("pr_state", "open", "pr_checks", "✗"), gitChanges{}, stageFixing},
		{"open, sent back", "idle", pr("pr_state", "open", "pr_review", "changes requested"), gitChanges{}, stageFixing},
		{"open, in conflict", "idle", pr("pr_state", "open", "pr_badge", "#9 ✓ conflicts"), gitChanges{}, stageFixing},
		{"merged", "idle", pr("pr_state", "merged"), gitChanges{}, stageMerged},
		{"merged, main has moved on", "idle", pr("pr_state", "merged"), gitChanges{Behind: 4}, stageMerged},
		{"closed", "idle", pr("pr_state", "closed"), gitChanges{}, stageMerged},
		{"merged, then edited", "idle", pr("pr_state", "merged"), gitChanges{Files: 1}, stageIdle},
		{"merged, then committed to", "idle", pr("pr_state", "merged"), gitChanges{Ahead: 1}, stageIdle},
	} {
		e := &entry{agent: agentInfo{Status: c.status, Tokens: c.tokens}, changes: c.changes}
		if got := stageOf(e); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got.label(), c.want.label())
		}
	}
}

func TestNewWorkAfterAMergePutsAnAgentBackInProgress(t *testing.T) {
	m, _ := demoModel(t)
	e := m.entries["w1:p4"] // idle, its PR open
	e.agent.Tokens = map[string]string{"prs": "#431 merged", "pr_state": "merged"}
	m.resort("w1:p4")
	if last := len(m.order) - 1; m.order[last] != "w1:p4" || m.cursor != last {
		t.Fatalf("merged isn't last, selected: %s, cursor %d", paneOrder(m), m.cursor)
	}
	// It starts on the next thing: git says there's work in its worktree.
	next, _ := m.Update(changesMsg{pane: "w1:p4", at: e.branchAt, changes: gitChanges{Files: 3}})
	m = next.(model)
	if stageOf(m.entries["w1:p4"]) != stageIdle || m.order[len(m.order)-1] == "w1:p4" {
		t.Errorf("still listed as merged: %s", paneOrder(m))
	}
	if m.selectedPane() != "w1:p4" {
		t.Errorf("the selection moved to %s", m.selectedPane())
	}
}

func TestTheTopOfAScrolledListSaysItsStage(t *testing.T) {
	m, _ := demoModel(t)
	m.height = 14             // a few entries at a time
	m.cursor, m.offset = 5, 5 // the second of the two working
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if got := strings.TrimSpace(lines[listTop]); got != "Working 2" {
		t.Errorf("the list starts with %q", got)
	}
	if lines[listTop+1] != "" {
		t.Errorf("under the heading: %q", lines[listTop+1])
	}
	if i, ok := m.entryAt(listTop + 2); !ok || i != 5 {
		t.Errorf("under the heading is entry %d, %v", i, ok)
	}
}

func TestTheFilterMatchesAStage(t *testing.T) {
	m, _ := demoModel(t)
	// A map of its own: the demo's agents share theirs between tests.
	m.entries["w1:p4"].agent.Tokens = map[string]string{"pr_state": "open", "pr_checks": "✗"}
	if got := paneOrder(typeText(m, "needs fixing")); got != "w1:p4" {
		t.Errorf("lists %s", got)
	}
}

func TestAStagesTabListsOnlyItsAgents(t *testing.T) {
	m, _ := demoModel(t)
	m.cursor = 4 // the first of the two working
	for range 4 {
		m = press(m, "right") // All, Needs you, Done, Idle, Working
	}
	if m.show != stageWorking || paneOrder(m) != "w1:p1 w2:p3" || m.selectedPane() != "w1:p1" {
		t.Fatalf("showing %s: %s, on %s", m.show.label(), paneOrder(m), m.selectedPane())
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(lines[0], "Working 2") || !strings.Contains(lines[listTop], "Dark mode") {
		t.Errorf("no heading wanted under a stage's tab:\n%s", strings.Join(lines[:listTop+2], "\n"))
	}
	if !strings.Contains(lines[len(lines)-1], "esc all") {
		t.Errorf("footer %q", lines[len(lines)-1])
	}
	// The filter narrows within the stage, and the counts follow it.
	m = typeText(m, "codex")
	if paneOrder(m) != "w2:p3" || !strings.Contains(ansi.Strip(m.View()), "All 1  Working 1") {
		t.Errorf("filtered: %s", paneOrder(m))
	}
	// esc: the filter, then the stage, then it closes.
	m = press(m, "esc")
	m = press(m, "esc")
	if m.show != stageAll || len(m.order) != len(m.entries) {
		t.Errorf("after esc twice: showing %d, %s", m.show, paneOrder(m))
	}
	// Left from All goes round to the last tab.
	if m = press(m, "left"); m.show != stageReview {
		t.Errorf("left of All is %s", m.show.label())
	}
}

func TestClickingATabShowsItsStage(t *testing.T) {
	m, _ := demoModel(t)
	_, tabs := m.tabBar()
	line := strings.Split(ansi.Strip(m.View()), "\n")[0]
	for _, tab := range tabs {
		want := "All"
		if tab.stage != stageAll {
			want = tab.stage.label()
		}
		if got := strings.TrimSpace(line[tab.x0:tab.x1]); !strings.HasPrefix(got, want+" ") {
			t.Errorf("cells %d-%d hold %q, want %s", tab.x0, tab.x1, got, want)
		}
	}
	review := tabs[5]
	next, _ := m.Update(tea.MouseMsg{X: review.x0 + 1, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m = next.(model); m.show != stageReview || paneOrder(m) != "w1:p4" {
		t.Errorf("showing %s: %s", m.show.label(), paneOrder(m))
	}
}

func TestAnEmptiedStageKeepsItsTab(t *testing.T) {
	m, _ := demoModel(t)
	m.showStage(stageReview)
	m.entries["w1:p4"].agent.Status = "working" // you set it going again
	m.resort("")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "In review 0") || !strings.Contains(v, "No agents at this stage.") {
		t.Errorf("view:\n%s", v)
	}
}

func TestTheTabsFitANarrowPopup(t *testing.T) {
	m, _ := demoModel(t)
	for _, w := range []int{100, 70, 50} {
		m.width = w
		line, tabs := m.tabBar()
		if got := ansi.StringWidth(line); got > w || tabs[len(tabs)-1].x1 != got {
			t.Errorf("at %d wide the tabs take %d, the last ending at %d", w, got, tabs[len(tabs)-1].x1)
		}
	}
	if line, _ := m.tabBar(); !strings.Contains(ansi.Strip(line), "Review 1") {
		t.Errorf("not shortened: %q", ansi.Strip(line))
	}
}
