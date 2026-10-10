package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// settle runs cmd and everything it leads to, feeding each message to the
// model, until nothing more comes within a moment. Timers (the poll, the
// spinner) don't fire that fast, so they're left behind.
func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		got := make(chan tea.Msg, 1)
		go func() { got <- c() }()
		var msg tea.Msg
		select {
		case msg = <-got:
		case <-time.After(100 * time.Millisecond):
			continue
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
			continue
		case nil, pollMsg:
			continue
		}
		if msg == tea.Quit() {
			continue
		}
		next, more := m.Update(msg)
		m = next.(model)
		queue = append(queue, more)
	}
	return m
}

// demoModel is the popup on the demo's agents, loaded and settled.
func demoModel(t *testing.T) (model, *demoSource) {
	t.Helper()
	src := newDemoSource()
	src.delay = 0
	m := newModel(context.Background(), src)
	m.width, m.height = 100, 40
	m.now = func() time.Time { return src.started }
	return settle(t, m, m.loadAgents()), src
}

func paneOrder(m model) string { return strings.Join(m.order, " ") }

func TestWhatNeedsYouComesFirst(t *testing.T) {
	m, _ := demoModel(t)
	// blocked, done, the idle ones with no PR out, working (two, in sidebar
	// order: storefront, billing), then the one in review
	if got := paneOrder(m); got != "w2:p1 w1:p3 w3:p2 w3:p4 w1:p1 w2:p3 w1:p4" {
		t.Errorf("order %s", got)
	}
}

func TestOutOfDateRecapsAreRewrittenOnOpen(t *testing.T) {
	m, _ := demoModel(t)
	e := m.entries["w1:p1"] // working, with a recap from before it carried on
	if e.recap == nil || !e.current || !strings.Contains(e.recap.Text, "contrast in the billing table") {
		t.Errorf("%+v", e.recap)
	}
	if e := m.entries["w2:p1"]; e.recap == nil || !strings.Contains(e.recap.Text, "approve running the migration") {
		t.Error("a current recap was replaced")
	}
}

func TestRowsSayWhyThereIsNoRecap(t *testing.T) {
	m, _ := demoModel(t)
	if e := m.entries["w2:p3"]; e.note != "Recaps aren't available for this agent." {
		t.Errorf("codex: %q", e.note)
	}
	if e := m.entries["w3:p4"]; e.note != "Nothing to recap yet." || e.recapping {
		t.Errorf("an empty conversation: %q, recapping %v", e.note, e.recapping)
	}
}

func TestTheViewShowsStatusTitleAndRecap(t *testing.T) {
	m, _ := demoModel(t)
	m.height = 50 // room for every agent and its stage's heading
	v := ansi.Strip(m.View())
	for _, want := range []string{
		"Recap   All 7  Needs you 1  Done 1  Idle 2  Working 2  In review 1",
		"Needs you 1", "Done 1", "Idle 2", "Working 2", "In review 1",
		"◉ Move billing webhooks to v2 events", "billing · waiting 2m",
		"billing/webhooks-v2 · 6 files +214 −58 ↑2 · 4/5 tasks · #412",
		"? Run npm run migrate -- --env staging",
		"storefront · done 12m", "storefront · working 18m", "docs · idle 2h",
		"settings-dark-mode · 14 files +530 −121 · 3/7 tasks", "main · ↑1", "codex · #415",
		"opus 5.5 · acceptEdits",
		"● Flaky checkout e2e test", "✓ Release notes for 2.4",
		"scratch", "Nothing to recap yet.", "Recaps aren't available for this agent.",
		"enter go to agent   tab reply   ^r suggest title   ←→ stage   esc close", // pills: a cell either side, one between
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if lines := strings.Split(v, "\n"); len(lines) != m.height {
		t.Errorf("%d lines for a %d-line popup", len(lines), m.height)
	}
}

func TestTheCursorFollowsItsAgentWhenTheOrderChanges(t *testing.T) {
	m, _ := demoModel(t)
	m.cursor = 4 // w1:p1, working
	agents, ws, _ := m.src.agents()
	for i := range agents {
		if agents[i].PaneID == "w1:p1" {
			agents[i].Status, agents[i].StateChangeSeq = "done", 13
		}
	}
	m = settle(t, m, func() tea.Msg { return agentsMsg{agents: agents, workspaces: ws} })
	if m.selectedPane() != "w1:p1" {
		t.Errorf("cursor on %s", m.selectedPane())
	}
}

func TestAClosedPaneLeavesTheList(t *testing.T) {
	m, _ := demoModel(t)
	agents, ws, _ := m.src.agents()
	m = settle(t, m, func() tea.Msg { return agentsMsg{agents: agents[1:], workspaces: ws} })
	if _, ok := m.entries[agents[0].PaneID]; ok || len(m.order) != len(agents)-1 {
		t.Errorf("order %s", paneOrder(m))
	}
}

// focusSource records where the popup sent you.
type focusSource struct {
	*demoSource
	mu      sync.Mutex
	focused []string
	err     error
}

func (f *focusSource) focus(pane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = append(f.focused, pane)
	return f.err
}

func focusModel(t *testing.T) (model, *focusSource) {
	t.Helper()
	d := newDemoSource()
	d.delay = 0
	src := &focusSource{demoSource: d}
	m := newModel(context.Background(), src)
	m.width, m.height = 100, 40
	return settle(t, m, m.loadAgents()), src
}

func TestEnterGoesToTheAgentAndCloses(t *testing.T) {
	m, src := focusModel(t)
	m.cursor = 1
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	if len(src.focused) != 1 || src.focused[0] != "w1:p3" {
		t.Fatalf("focused %v", src.focused)
	}
	if _, quit := next.(model).Update(msg); quit == nil || quit() != tea.Quit() {
		t.Error("didn't close after going to the agent")
	}
}

func TestAFailedJumpSaysSoAndStaysOpen(t *testing.T) {
	m, src := focusModel(t)
	src.err = errors.New("agent_not_found: gone")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, quit := next.(model).Update(cmd())
	if quit != nil || !strings.Contains(next.(model).err, "agent_not_found") {
		t.Errorf("err %q", next.(model).err)
	}
}

func TestClickingAnAgentGoesToIt(t *testing.T) {
	m, src := focusModel(t)
	// The first entry is its stage's heading, its title and recap from
	// listTop, then a blank: the second entry's heading and title follow.
	for y := listTop; y < listTop+m.headingHeight(0); y++ {
		if _, ok := m.entryAt(y); ok {
			t.Error("a stage's heading is an entry")
		}
	}
	y := listTop + m.headingHeight(0) + m.entryHeight(0) + m.headingHeight(1)
	if i, ok := m.entryAt(y); !ok || i != 1 {
		t.Fatalf("line %d is entry %d, %v", y, i, ok)
	}
	if _, ok := m.entryAt(y - 1); ok {
		t.Error("the blank between entries is an entry")
	}
	_, cmd := m.Update(tea.MouseMsg{X: 10, Y: y + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil {
		t.Fatal("click did nothing")
	}
	cmd()
	if len(src.focused) != 1 || src.focused[0] != m.order[1] {
		t.Errorf("focused %v", src.focused)
	}
}

func TestClickingAHintPressesIt(t *testing.T) {
	m, _ := focusModel(t)
	x := strings.Index(ansi.Strip(m.renderFooter(m.footer())), "esc close")
	_, cmd := m.Update(tea.MouseMsg{X: x, Y: m.height - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if cmd == nil || cmd() != tea.Quit() {
		t.Error("esc close didn't close")
	}
}

func TestTheDemoGoesNowhere(t *testing.T) {
	m, _ := demoModel(t)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, quit := next.(model).Update(cmd())
	if quit != nil || next.(model).flash == "" {
		t.Error("the demo should stay open and say why")
	}
}

// renameSource records what's typed into agents and what's settled, and has
// the agent take a /rename as Claude would, unless it's held back.
type renameSource struct {
	*demoSource
	mu    sync.Mutex
	sent  []string
	done  []string // sessions whose suggestion was settled
	asked int
	hold  bool // the agent's title doesn't change
}

func (r *renameSource) give(pane string, s claudeSession) error {
	c, _ := r.cached(s)
	r.mu.Lock()
	r.sent = append(r.sent, pane+": /rename "+c.Title)
	hold := r.hold
	r.mu.Unlock()
	if hold {
		return nil
	}
	return r.demoSource.give(pane, s)
}

func (r *renameSource) settled(s claudeSession) error {
	r.mu.Lock()
	r.done = append(r.done, s.ID)
	r.mu.Unlock()
	return r.demoSource.settled(s)
}

func (r *renameSource) suggest(ctx context.Context, s claudeSession, title string) (recap, error) {
	r.mu.Lock()
	r.asked++
	r.mu.Unlock()
	return r.demoSource.suggest(ctx, s, title)
}

func renameModel(t *testing.T) (model, *renameSource) {
	t.Helper()
	d := newDemoSource()
	d.delay = 0
	src := &renameSource{demoSource: d}
	m := newModel(context.Background(), src)
	m.width, m.height = 100, 40
	m.now = func() time.Time { return d.started }
	m = settle(t, m, m.loadAgents())
	return selectPane(m, "w1:p4"), src // the one with a slug for a title
}

// selectPane moves the selection to a pane's row, scrolled into view.
func selectPane(m model, pane string) model {
	for i, p := range m.order {
		if p == pane {
			m.cursor = i
		}
	}
	m.scrollTo()
	return m
}

func TestAPoorTitleHasABetterOneUnderIt(t *testing.T) {
	m, _ := renameModel(t)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "✓ search-pagination") || !strings.Contains(v, "→ WEB-431 Paginate the Search Results  Rename · Ignore") {
		t.Errorf("no suggestion under the poor title:\n%s", v)
	}
	if strings.Count(v, "Rename · Ignore") != 1 {
		t.Error("a good title has a suggestion under it")
	}
	if !strings.Contains(v, "^r rename   ^x ignore") {
		t.Error("the footer doesn't offer the rename")
	}
	// The suggestion is found by the filter: a ticket's key, say.
	if got := paneOrder(typeText(m, "web-431")); got != "w1:p4" {
		t.Errorf("filtered to %q", got)
	}
}

func TestCtrlRGivesTheAgentItsSuggestedTitle(t *testing.T) {
	m, src := renameModel(t)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(model)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Renaming to WEB-431 Paginate the Search Results.") || strings.Contains(v, "Rename · Ignore") {
		t.Errorf("not shown as being renamed:\n%s", v)
	}
	m = settle(t, m, cmd)
	if len(src.sent) != 1 || src.sent[0] != "w1:p4: /rename WEB-431 Paginate the Search Results" {
		t.Fatalf("typed %q", src.sent)
	}
	// Its title says so on the next look: the row is as any other.
	m = settle(t, m, m.loadAgents())
	e := m.entries["w1:p4"]
	if e.renaming != "" || titled(e) || title(e.agent) != "WEB-431 Paginate the Search Results" {
		t.Errorf("renaming %q, title %q", e.renaming, title(e.agent))
	}
	if len(src.done) != 1 {
		t.Errorf("settled %v", src.done)
	}
}

func TestABusyAgentIsRenamedWhenItsDone(t *testing.T) {
	m, src := renameModel(t)
	e := m.entries["w1:p4"]
	e.agent.Status = "working"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = settle(t, next.(model), cmd)
	if len(src.sent) != 0 || e.renaming == "" {
		t.Fatalf("typed %q into an agent at work, renaming %q", src.sent, e.renaming)
	}
	// The demo's agent is idle: the next look finds it free, and it's sent once.
	src.hold = true
	m = settle(t, m, m.loadAgents())
	m = settle(t, m, m.loadAgents())
	if len(src.sent) != 1 || e.renaming == "" {
		t.Fatalf("typed %q, renaming %q", src.sent, e.renaming)
	}
	// Sent, and a while later still not in its title: the row stops saying so.
	sent := m.now()
	m.now = func() time.Time { return sent.Add(renameWait + time.Second) }
	m = settle(t, m, m.loadAgents())
	if e.renaming != "" || titled(e) {
		t.Errorf("still renaming %q", e.renaming)
	}
}

func TestARenameOutlivesThePopup(t *testing.T) {
	m, src := renameModel(t)
	m.entries["w1:p4"].agent.Status = "working"
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	settle(t, next.(model), cmd)
	// Closed and opened again while the agent is still at work: the row
	// says it's being renamed, not that it could be.
	m = newModel(context.Background(), src)
	m.width, m.height = 100, 40
	m.now = func() time.Time { return src.started }
	src.hold = true
	m = selectPane(settle(t, m, m.loadAgents()), "w1:p4")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Renaming to WEB-431 Paginate the Search Results.") || strings.Contains(v, "Rename · Ignore") {
		t.Errorf("a rename asked for was forgotten:\n%s", v)
	}
	// The agent is free by the next look: it's typed in.
	if settle(t, m, m.loadAgents()); len(src.sent) != 1 {
		t.Errorf("typed %q", src.sent)
	}
}

func TestIgnoreTurnsASuggestionDown(t *testing.T) {
	m, src := renameModel(t)
	before := m.entryHeight(m.cursor)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = settle(t, next.(model), cmd)
	if e := m.entries["w1:p4"]; titled(e) || m.entryHeight(m.cursor) != before-1 {
		t.Error("the suggestion is still there")
	}
	if len(src.sent) != 0 || len(src.done) != 1 {
		t.Errorf("typed %q, settled %v", src.sent, src.done)
	}
	// A recap that was being written meanwhile, or read before the note of
	// it landed, doesn't bring it back.
	e := m.entries["w1:p4"]
	stale := recap{Session: e.session.ID, Text: "Later.", Title: "WEB-431 Paginate the Search Results"}
	next, _ = m.Update(recapMsg{pane: "w1:p4", session: e.session.ID, recap: stale})
	next, _ = next.(model).Update(sessionMsg{pane: "w1:p4", seq: e.agent.StateChangeSeq, session: *e.session, cached: &stale, current: true})
	if m = next.(model); titled(m.entries["w1:p4"]) {
		t.Error("an older recap brought the suggestion back")
	}
	// Turned down for good: it's not back on the next open.
	if r, _ := src.cached(*m.entries["w1:p4"].session); r == nil || !r.Settled {
		t.Errorf("%+v", r)
	}
}

func TestTheButtonsAfterASuggestionAreClickable(t *testing.T) {
	for _, c := range []struct {
		button  string
		sent    int
		settled int
	}{{"Rename", 1, 1}, {"Ignore", 0, 1}} {
		m, src := renameModel(t)
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		y, x := -1, -1
		for i, l := range lines {
			if j := strings.Index(l, c.button+" "); strings.Contains(l, "→ WEB-431") && j >= 0 {
				y, x = i, ansi.StringWidth(l[:j])
			}
			if j := strings.Index(l, "· "+c.button); strings.Contains(l, "→ WEB-431") && j >= 0 {
				y, x = i, ansi.StringWidth(l[:j])+2
			}
		}
		if y < 0 {
			t.Fatalf("%s isn't on screen", c.button)
		}
		next, cmd := m.Update(tea.MouseMsg{X: x + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		m = settle(t, next.(model), cmd)
		m = settle(t, m, m.loadAgents())
		if len(src.sent) != c.sent || len(src.done) != c.settled {
			t.Errorf("%s: typed %q, settled %v", c.button, src.sent, src.done)
		}
	}
	// Elsewhere on that line goes to the agent, as the rest of the row does.
	m, src := renameModel(t)
	y := 0
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "→ WEB-431") {
			y = i
		}
	}
	_, cmd := m.Update(tea.MouseMsg{X: 8, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if msg, ok := cmd().(focusedMsg); !ok || !errors.Is(msg.err, errDemo) || len(src.sent) != 0 {
		t.Errorf("a click on the suggestion itself: %v, typed %q", cmd(), src.sent)
	}
}

func TestCtrlRAsksForATitleWhenNoneIsSuggested(t *testing.T) {
	m, src := renameModel(t)
	m = selectPane(m, "w1:p3")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(model)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Finding a title.") {
		t.Errorf("not shown as finding one:\n%s", v)
	}
	m = settle(t, m, cmd)
	if v := ansi.Strip(m.View()); src.asked != 1 || !strings.Contains(v, "→ WEB-512 Fix the Flaky Checkout Test  Rename · Ignore") {
		t.Errorf("asked %d:\n%s", src.asked, v)
	}
	if len(src.sent) != 0 {
		t.Errorf("renamed without being asked: %q", src.sent)
	}
	// One that would keep its title says so, and nothing is offered.
	m = selectPane(m, "w3:p2")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = settle(t, next.(model), cmd)
	if e := m.entries["w3:p2"]; titled(e) || !strings.Contains(m.flash, "would keep the title it has") {
		t.Errorf("flash %q", m.flash)
	}
	// Nothing said yet: nothing to go by.
	m = selectPane(m, "w3:p4")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd != nil || next.(model).flash != "Nothing to recap yet." {
		t.Errorf("flash %q", next.(model).flash)
	}
}

func TestAWorkingAgentIsRecappedOnceWhileOpen(t *testing.T) {
	// Its conversation keeps changing: rewriting its recap on every status
	// change would cost for nothing.
	d := newDemoSource()
	d.delay = 0
	m := newModel(context.Background(), d)
	m.width, m.height = 100, 40
	m = settle(t, m, m.loadAgents())
	e := m.entries["w1:p1"]
	if !e.triedRecap {
		t.Fatal("not recapped on open")
	}
	d.current[e.session.ID] = false // it carried on
	next, cmd := m.Update(sessionMsg{pane: "w1:p1", seq: e.agent.StateChangeSeq, session: *e.session, cached: e.recap})
	if cmd != nil || next.(model).entries["w1:p1"].recapping {
		t.Error("recapped a working agent again")
	}
}

type flakySessionSource struct {
	*demoSource
	calls int
}

func (f *flakySessionSource) session(context.Context, agentInfo) (claudeSession, error) {
	f.calls++
	if f.calls == 1 {
		return claudeSession{}, errSessionNotIndexed
	}
	return claudeSession{Agent: "codex", ID: codexSession, Transcript: "/x", Stamp: "v1"}, nil
}

func TestFailedMemexLookupRetriesWithBoundedBackoff(t *testing.T) {
	src := &flakySessionSource{demoSource: newDemoSource()}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), src)
	m.now = func() time.Time { return now }
	a := agentInfo{PaneID: "w9:p1", Agent: "codex", Status: "done", StateChangeSeq: 1, AgentSession: agentSession{Kind: "id", Value: codexSession}}
	m = settle(t, m, m.updateAgents([]agentInfo{a}, nil))
	if src.calls != 1 {
		t.Fatalf("initial calls: %d", src.calls)
	}
	m.updateAgents([]agentInfo{a}, nil)
	if src.calls != 1 {
		t.Fatalf("retried without backoff: %d", src.calls)
	}
	now = now.Add(resolveRetryBase)
	m = settle(t, m, m.updateAgents([]agentInfo{a}, nil))
	if src.calls != 2 || m.entries[a.PaneID].session == nil {
		t.Fatalf("calls %d, entry %+v", src.calls, m.entries[a.PaneID])
	}
}

type refreshingSessionSource struct {
	*demoSource
	calls int
}

func (r *refreshingSessionSource) session(context.Context, agentInfo) (claudeSession, error) {
	r.calls++
	return claudeSession{Agent: "opencode", ID: openCodeSession, Transcript: "/x", Stamp: fmt.Sprintf("v%d", r.calls)}, nil
}

func TestMemexSessionRefreshesWhileStateIsUnchanged(t *testing.T) {
	src := &refreshingSessionSource{demoSource: newDemoSource()}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	m := newModel(context.Background(), src)
	m.now = func() time.Time { return now }
	a := agentInfo{PaneID: "w9:p2", Agent: "opencode", Status: "done", StateChangeSeq: 1, AgentSession: agentSession{Kind: "id", Value: openCodeSession}}
	m = settle(t, m, m.updateAgents([]agentInfo{a}, nil))
	if got := m.entries[a.PaneID].session.Stamp; got != "v1" {
		t.Fatalf("stamp %q", got)
	}
	now = now.Add(memexRefreshEvery)
	m = settle(t, m, m.updateAgents([]agentInfo{a}, nil))
	if src.calls != 2 || m.entries[a.PaneID].session.Stamp != "v2" {
		t.Fatalf("calls %d, session %+v", src.calls, m.entries[a.PaneID].session)
	}
}

func TestScrollingKeepsTheCursorInView(t *testing.T) {
	m, _ := demoModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m = next.(model)
	for range len(m.order) {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
	}
	if m.cursor != len(m.order)-1 {
		t.Fatalf("cursor %d", m.cursor)
	}
	v := ansi.Strip(m.View())
	if !strings.Contains(v, title(m.entries[m.order[m.cursor]].agent)) {
		t.Errorf("the selected agent is off screen:\n%s", v)
	}
	if lines := strings.Split(v, "\n"); len(lines) != 12 {
		t.Errorf("%d lines", len(lines))
	}
}

func TestWrap(t *testing.T) {
	text := "Adding dark mode to settings: tokens and form fields are done, and it's now fixing contrast."
	lines := wrap(text, 30, 3)
	if len(lines) != 3 {
		t.Fatalf("%q", lines)
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 30 {
			t.Errorf("%q is wider than 30", l)
		}
	}
	if got := wrap(text, 30, 2); len(got) != 2 || !strings.HasSuffix(got[1], "…") {
		t.Errorf("cut to two lines: %q", got)
	}
	if got := wrap("short", 30, 3); len(got) != 1 || got[0] != "short" {
		t.Errorf("%q", got)
	}
	if got := wrap("", 30, 3); len(got) != 1 {
		t.Errorf("%q", got)
	}
	if got := wrap("supercalifragilisticexpialidocious", 10, 3); ansi.StringWidth(got[0]) > 10 {
		t.Errorf("a long word overflows: %q", got)
	}
}

func TestTitle(t *testing.T) {
	for _, c := range []struct {
		a    agentInfo
		want string
	}{
		{agentInfo{Title: "Fix login", Name: "app", Cwd: "/x/app"}, "Fix login"},
		{agentInfo{Title: "Claude Code", Name: "app"}, "app"},
		{agentInfo{Cwd: "/home/me/shop"}, "shop"},
		{agentInfo{Agent: "claude"}, "claude"},
	} {
		if got := title(c.a); got != c.want {
			t.Errorf("%+v: %q", c.a, got)
		}
	}
}

func TestAgo(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 4 * time.Minute: "4m ago", 3 * time.Hour: "3h ago", 72 * time.Hour: "3d ago",
	} {
		if got := ago(now, now.Add(-d)); got != want {
			t.Errorf("%v: %q", d, got)
		}
	}
}

func TestPaneOrderIsNumeric(t *testing.T) {
	if !paneLess("w1:p2", "w1:p10") || paneLess("w1:p10", "w1:p2") {
		t.Error("w1:p2 comes before w1:p10")
	}
}

func TestEscapesInTitlesNeverReachTheScreen(t *testing.T) {
	m, _ := demoModel(t)
	e := m.entries[m.order[0]]
	e.agent.Title = "evil \x1b]52;c;aGk=\x07title \x1b[2J"
	if v := m.View(); strings.Contains(v, "\x1b]") || strings.Contains(v, "\x1b[2J") {
		t.Error("an escape sequence got through")
	}
}

func TestOnlyTheSelectedAgentShowsYourLastPrompt(t *testing.T) {
	m, _ := demoModel(t)
	prompt := "› run the migration against staging when the tests pass"
	if v := ansi.Strip(m.View()); !strings.Contains(v, prompt) {
		t.Errorf("the selected agent's last prompt is missing:\n%s", v)
	}
	m.cursor = 1
	if v := ansi.Strip(m.View()); strings.Contains(v, prompt) || !strings.Contains(v, "› the checkout e2e test fails") {
		t.Errorf("the prompt didn't follow the selection:\n%s", v)
	}
	if v := ansi.Strip(m.View()); strings.Contains(v, "opus 5.5 · acceptEdits") || !strings.Contains(v, "sonnet 5") {
		t.Errorf("model and mode should follow the selection:\n%s", v)
	}
	// They're on the status line, so selecting a row doesn't change its height.
	selected := m.entryHeight(1)
	m.cursor = 0
	if selected != m.entryHeight(1) {
		t.Errorf("selected %d lines, unselected %d", selected, m.entryHeight(1))
	}
}

func TestTokenValues(t *testing.T) {
	tokens := map[string]string{"pr": "#1044", "pr_state": "open", "pr_checks": "passing", "clauth": "me", "empty": " "}
	if got := strings.Join(tokenValues(tokens, nil), " "); got != "me #1044" {
		t.Errorf("all: %q", got)
	}
	if got := strings.Join(tokenValues(tokens, []string{"pr_checks", "pr", "missing"}), " "); got != "passing #1044" {
		t.Errorf("named: %q", got)
	}
	if got := tokenValues(map[string]string{"pr_state": "open"}, nil); len(got) != 1 {
		t.Errorf("pr details without a pr are shown: %q", got)
	}
	// herdr-github's prs has the PR, its state and the others: it's said once.
	tokens = map[string]string{"pr": "#148", "pr_badge": "#148 merged", "pr_state": "merged", "prs": "#148 merged", "clauth": "tvh"}
	if got := strings.Join(tokenValues(tokens, nil), " · "); got != "tvh · #148 merged" {
		t.Errorf("a PR is there twice: %q", got)
	}
}

// replySource records replies, and refuses them for a blocked agent.
type replySource struct {
	*demoSource
	mu   sync.Mutex
	sent []string
}

func (r *replySource) prompt(pane, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pane == "w2:p1" {
		return &herdrError{"agent_blocked", "agent is blocked"}
	}
	r.sent = append(r.sent, pane+": "+text)
	return nil
}

func typeText(m model, s string) model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(model)
}

func TestReplyToAnAgent(t *testing.T) {
	d := newDemoSource()
	d.delay = 0
	src := &replySource{demoSource: d}
	m := newModel(context.Background(), src)
	m.width, m.height = 100, 40
	m = settle(t, m, m.loadAgents())
	m.cursor = 1 // done: Flaky checkout e2e test
	m = press(m, "tab")
	if v := ansi.Strip(m.View()); !m.replying || !strings.Contains(v, "Reply to Flaky checkout e2e test:") || !strings.Contains(v, " ›  ") {
		t.Fatal("tab didn't open the reply line")
	}
	// Keys go to the reply, not the filter.
	m = typeText(m, "just push it")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = settle(t, next.(model), cmd)
	if len(src.sent) != 1 || src.sent[0] != "w1:p3: just push it" || m.replying {
		t.Fatalf("sent %v", src.sent)
	}
	if !strings.Contains(m.flash, "Sent to Flaky checkout e2e test") {
		t.Errorf("flash %q", m.flash)
	}
	// Esc drops a reply unsent.
	m = press(m, "tab")
	m = typeText(m, "never mind")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m = next.(model); m.replying || len(src.sent) != 1 {
		t.Error("esc sent the reply or kept the line open")
	}
	// A blocked agent can't take a prompt: it says to go and answer.
	m.cursor = 0
	m = press(m, "tab")
	m = typeText(m, "yes")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = settle(t, next.(model), cmd)
	if !strings.Contains(m.err, "waiting on a question or approval") {
		t.Errorf("err %q", m.err)
	}
}

func TestTypingFiltersTheList(t *testing.T) {
	m, _ := demoModel(t)
	all := paneOrder(m)
	m.cursor = 2
	// Every word must be somewhere in the row: here the title and the status.
	m = typeText(m, "dark working")
	if got := paneOrder(m); got != "w1:p1" || m.cursor != 0 {
		t.Fatalf("order %s, cursor %d", got, m.cursor)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, " › dark working") || strings.Contains(v, "Release notes") || !strings.Contains(v, "esc clear") {
		t.Errorf("view:\n%s", v)
	}
	// It lasts across herdr's next list of agents.
	if m = settle(t, m, m.loadAgents()); paneOrder(m) != "w1:p1" {
		t.Errorf("after a poll: %s", paneOrder(m))
	}
	m = typeText(m, " nope")
	if v := ansi.Strip(m.View()); len(m.order) != 0 || !strings.Contains(v, "Nothing matches.") {
		t.Errorf("view:\n%s", v)
	}
	// Esc clears the filter first, and only then closes.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m = next.(model); cmd != nil || m.filter.Value() != "" || paneOrder(m) != all {
		t.Fatalf("esc: filter %q, order %s", m.filter.Value(), paneOrder(m))
	}
	if _, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc}); cmd == nil || cmd() != tea.Quit() {
		t.Error("esc with no filter didn't close")
	}
}

func TestTheFilterMatchesWhatARowShows(t *testing.T) {
	for q, want := range map[string]string{
		"needs you":     "w2:p1",       // a blocked agent, as the header puts it
		"codex":         "w2:p3",       // the kind of agent
		"#415":          "w2:p3",       // a token
		"SETTINGS-dark": "w1:p1",       // the branch, in any case
		"upgrade":       "w3:p2",       // the recap
		"docs":          "w3:p2 w3:p4", // the space
	} {
		m, _ := demoModel(t)
		if got := paneOrder(typeText(m, q)); got != want {
			t.Errorf("%q lists %s, want %s", q, got, want)
		}
	}
}

func TestInState(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	m := model{now: func() time.Time { return now }}
	e := &entry{agent: agentInfo{Status: "blocked"}, session: &claudeSession{Meta: sessionMeta{
		Active: now.Add(-1 * time.Minute), PendingAt: now.Add(-3 * time.Minute), PromptAt: now.Add(-20 * time.Minute)}}}
	for status, want := range map[string]string{"blocked": "waiting 3m", "working": "working 20m", "done": "done 1m", "idle": "idle 1m", "unknown": ""} {
		e.agent.Status = status
		if got := m.inState(e); got != want {
			t.Errorf("%s: %q", status, got)
		}
	}
	if got := m.inState(&entry{agent: agentInfo{Status: "done"}}); got != "" {
		t.Errorf("no session: %q", got)
	}
}

func TestChangesText(t *testing.T) {
	for c, want := range map[gitChanges]string{
		{}:                                  "",
		{Files: 1, Added: 2}:                "1 file +2",
		{Files: 4, Added: 120, Deleted: 30}: "4 files +120 −30",
		{Ahead: 2, Behind: 1}:               "↑2 ↓1",
	} {
		if got := ansi.Strip(changesText(c)); got != want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestAReplyCanRunToSeveralLines(t *testing.T) {
	d := newDemoSource()
	d.delay = 0
	src := &replySource{demoSource: d}
	m := newModel(context.Background(), src)
	m.width, m.height = 100, 30
	m = settle(t, m, m.loadAgents())
	m.cursor = 1
	m = press(m, "tab")
	m = typeText(m, "looks good")
	for _, nl := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ}} {
		next, _ := m.Update(nl)
		m = next.(model)
		m = typeText(m, "more")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\nand pasted"), Paste: true})
	m = next.(model)
	if got := m.replyText.Value(); got != "looks good\nmore\nmore\nand pasted" {
		t.Fatalf("value %q", got)
	}
	v := ansi.Strip(m.View())
	if lines := strings.Split(v, "\n"); len(lines) != m.height {
		t.Errorf("%d lines for a %d-line popup: the list didn't make room for the reply\n%s", len(lines), m.height, v)
	}
	if !strings.Contains(v, "and pasted") || !strings.Contains(v, "looks good") {
		t.Errorf("the reply box doesn't show every line:\n%s", v)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	settle(t, next.(model), cmd)
	if len(src.sent) != 1 || src.sent[0] != "w1:p3: looks good\nmore\nmore\nand pasted" {
		t.Errorf("sent %q", src.sent)
	}
}

func TestEntryHeightMatchesWhatIsDrawn(t *testing.T) {
	m, _ := demoModel(t)
	check := func(when string) {
		t.Helper()
		for i, p := range m.order {
			for _, cursor := range []int{i, (i + 1) % len(m.order)} {
				m.cursor = cursor
				if got, want := m.entryHeight(i), len(m.viewEntry(m.entries[p], cursor == i)); got != want {
					t.Errorf("%s: %s (selected %v): counted %d lines, drew %d", when, p, cursor == i, got, want)
				}
			}
		}
	}
	check("settled")
	// Mid-recap and out of date, with and without the reply line open.
	for _, e := range m.entries {
		e.recapping, e.current = true, false
	}
	check("recapping")
}

func TestARecapFinishingAfterTheConversationMovedOnIsNotCurrent(t *testing.T) {
	d := newDemoSource()
	d.delay = 0
	m := newModel(context.Background(), d)
	m.width, m.height = 100, 40
	m = settle(t, m, m.loadAgents())
	e := m.entries["w1:p3"]
	s := *e.session
	// A recap is running when the agent's status changes.
	e.recapping = true
	next, cmd := m.Update(sessionMsg{pane: "w1:p3", seq: e.agent.StateChangeSeq, session: s, cached: e.recap, current: false})
	m = next.(model)
	if cmd != nil || !m.entries["w1:p3"].again {
		t.Fatal("the change wasn't kept for after the running recap")
	}
	// The running recap lands: not current, and another is asked for.
	next, cmd = m.Update(recapMsg{pane: "w1:p3", session: s.ID, recap: recap{Session: s.ID, Text: "old", At: time.Now()}})
	m = next.(model)
	if e := m.entries["w1:p3"]; e.current || cmd == nil || !e.recapping {
		t.Errorf("current %v, recapping %v, another asked for %v", e.current, e.recapping, cmd != nil)
	}
}

func TestTheMouseLeavesAReplyAlone(t *testing.T) {
	m, src := focusModel(t)
	m = press(m, "tab")
	m = typeText(m, "half a thought")
	next, cmd := m.Update(tea.MouseMsg{X: 10, Y: listTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = next.(model)
	if cmd != nil || len(src.focused) != 0 || !m.replying || m.replyText.Value() != "half a thought" {
		t.Error("a click in the list left the reply")
	}
	next, _ = m.Update(tea.MouseMsg{X: 10, Y: listTop + 12, Action: tea.MouseActionMotion})
	if next.(model).cursor != m.cursor {
		t.Error("hovering moved the selection away from the agent being replied to")
	}
}
