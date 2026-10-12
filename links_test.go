package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// linkSource records the popups this one was closed for.
type linkSource struct {
	*demoSource
	mu      sync.Mutex
	opened  []popupRef
	without bool // herdr-github, herdr-linear and herdr-snooze aren't installed
}

func (l *linkSource) plugins() map[string]popupRef {
	if l.without {
		return nil
	}
	return map[string]popupRef{
		githubPlugin: {PluginID: githubPlugin, Entrypoint: "picker", Width: "85%", Height: "80%"},
		linearPlugin: {PluginID: linearPlugin, Entrypoint: "picker", Width: "80%", Height: "70%"},
		snoozePlugin: {PluginID: snoozePlugin, Entrypoint: "picker", Width: "44", Height: "16"},
	}
}

func (l *linkSource) open(p popupRef) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, p)
	return nil
}

func linkModel(t *testing.T, without bool) (model, *linkSource) {
	t.Helper()
	d := newDemoSource()
	d.delay = 0
	src := &linkSource{demoSource: d, without: without}
	m := newModel(context.Background(), src)
	m.width, m.height = 100, 40
	return selectPane(settle(t, m, m.Init()), "w1:p4"), src // the one with a PR
}

func TestAPullRequestOpensInTheGitHubPlugin(t *testing.T) {
	m, src := linkModel(t, false)
	if !strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^g pull request") {
		t.Error("no key for it in the footer")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	msg := cmd()
	if len(src.opened) != 1 {
		t.Fatalf("opened %v", src.opened)
	}
	p := src.opened[0]
	if p.PluginID != githubPlugin || p.Width != "85%" || p.Env["HERDR_GITHUB_PANE"] != "w1:p4" ||
		p.Env["HERDR_GITHUB_SPACE"] != "w1" || p.Env["HERDR_GITHUB_CWD"] != "/demo/w1-p4" {
		t.Errorf("%+v", p)
	}
	// It's told where to come back to: this popup, on this agent.
	var back popupRef
	if err := json.Unmarshal([]byte(p.Env[returnEnv]), &back); err != nil || back.PluginID != "herdr-recap" ||
		back.Entrypoint != "picker" || back.Env[selectEnv] != "w1:p4" {
		t.Errorf("back %+v %v", back, err)
	}
	if _, quit := next.(model).Update(msg); quit == nil || quit() != tea.Quit() {
		t.Error("didn't close for the other popup")
	}
}

func TestAClickOnAPullRequestOpensIt(t *testing.T) {
	m, src := linkModel(t, false)
	e := m.selected()
	_, spans := m.detailLine(e, &e.session.Meta)
	if len(spans) != 1 || spans[0].plugin != githubPlugin {
		t.Fatalf("nowhere to click: %v", spans)
	}
	x0, x1 := spans[0].x0, spans[0].x1
	y := -1
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "#431") {
			y = i
			if got := ansi.Cut(l, x0, x1); got != "#431 ✓ approved" {
				t.Errorf("the cells are %q", got)
			}
		}
	}
	click := func(x int) int {
		_, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if cmd != nil {
			cmd()
		}
		return len(src.opened)
	}
	// Beside it is the row: that goes to the agent, as ever.
	if click(x0-2) != 0 || click(x1) != 0 {
		t.Error("a click beside the pull request opened it")
	}
	if click(x0) != 1 || click(x1-1) != 2 {
		t.Errorf("opened %d", len(src.opened))
	}
}

func TestWithoutTheGitHubPluginAPullRequestIsJustText(t *testing.T) {
	m, src := linkModel(t, true)
	e := m.selected()
	if _, spans := m.detailLine(e, &e.session.Meta); len(spans) != 0 {
		t.Errorf("something to click: %v", spans)
	}
	if strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "pull request") {
		t.Error("a key for it in the footer")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd != nil || len(src.opened) != 0 || !strings.Contains(next.(model).flash, "herdr-github") {
		t.Errorf("flash %q, opened %v", next.(model).flash, src.opened)
	}
	// And an agent with none says so.
	m = selectPane(m, "w3:p1")
	if m.selected() != nil && linkToken(m.selected().agent, githubPlugin) == "" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
		if !strings.Contains(next.(model).flash, "no pull request") {
			t.Errorf("flash %q", next.(model).flash)
		}
	}
}

func TestThePopupOpensOnThePaneItWasLeftOn(t *testing.T) {
	d := newDemoSource()
	d.delay = 0
	m := newModel(context.Background(), d)
	m.width, m.height = 100, 40
	m.startOn = "w1:p4"
	m = settle(t, m, m.loadAgents())
	if m.selectedPane() != "w1:p4" {
		t.Errorf("on %s", m.selectedPane())
	}
}

func TestAPopupFromTheEnvironmentIsHeldToAnID(t *testing.T) {
	for raw, want := range map[string]bool{
		`{"plugin_id":"herdr-recap","entrypoint":"picker"}`: true,
		`{"plugin_id":"../x","entrypoint":"picker"}`:        false,
		`{"plugin_id":"herdr-recap","entrypoint":"a b"}`:    false,
		`{"plugin_id":"herdr-recap"}`:                       false,
		``:                                                  false,
		`not json`:                                          false,
	} {
		t.Setenv(returnEnv, raw)
		if _, ok := cameFrom(); ok != want {
			t.Errorf("%q: %v", raw, ok)
		}
	}
}

// herdr-linear's issue is one piece, with its status, and opens in its popup.
func TestALinearIssueIsShownOnceAndOpensInTheLinearPlugin(t *testing.T) {
	m, src := linkModel(t, false)
	e := m.selected()
	e.agent.Tokens = map[string]string{
		"prs": "#431 ✓ approved", "issue": "ENG-142", "issue_state": "In Progress", "issue_title": "Fix the login redirect",
	}
	info, spans := m.detailLine(e, &e.session.Meta)
	line := ansi.Strip(strings.Join(info, " · "))
	if !strings.Contains(line, "ENG-142 In Progress") || strings.Count(line, "In Progress") != 1 || strings.Contains(line, "Fix the login") {
		t.Errorf("line %q", line)
	}
	var x0, x1 int
	for _, sp := range spans {
		if sp.plugin == linearPlugin {
			x0, x1 = sp.x0, sp.x1
		}
	}
	if got := ansi.Cut("   "+line, x0, x1); len(spans) != 2 || got != "ENG-142 In Progress" {
		t.Fatalf("spans %v, the cells are %q", spans, got)
	}
	if !strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^l issue") {
		t.Error("no key for it in the footer")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	cmd()
	if len(src.opened) != 1 {
		t.Fatalf("opened %v", src.opened)
	}
	p := src.opened[0]
	if p.PluginID != linearPlugin || p.Env["HERDR_LINEAR_ISSUE"] != "ENG-142" || p.Env["HERDR_LINEAR_CWD"] != "/demo/w1-p4" ||
		!strings.Contains(p.Env[returnEnv], `"HERDR_RECAP_PANE":"w1:p4"`) {
		t.Errorf("%+v", p)
	}
}

// ctrl+s hands the agent to herdr-snooze's popup: to snooze an awake one,
// or to wake a snoozed one or change when.
func TestCtrlSOpensTheSnoozePluginOnTheAgent(t *testing.T) {
	m, src := linkModel(t, false)
	if !strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^s snooze") {
		t.Error("no key for it in the footer")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	msg := cmd()
	if len(src.opened) != 1 {
		t.Fatalf("opened %v", src.opened)
	}
	// At the size it asks for, on this agent, told where to come back to.
	p := src.opened[0]
	if p.PluginID != snoozePlugin || p.Entrypoint != "picker" || p.Width != "44" || p.Height != "16" ||
		p.Env["HERDR_SNOOZE_PANE"] != "w1:p4" || len(p.Env) != 2 {
		t.Errorf("%+v", p)
	}
	var back popupRef
	if err := json.Unmarshal([]byte(p.Env[returnEnv]), &back); err != nil || back.PluginID != "herdr-recap" ||
		back.Env[selectEnv] != "w1:p4" {
		t.Errorf("back %+v %v", back, err)
	}
	if _, quit := next.(model).Update(msg); quit == nil || quit() != tea.Quit() {
		t.Error("didn't close for the other popup")
	}
	// A snoozed one is snoozed for a new time there.
	m = selectPane(m, "w3:p2")
	if !strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^w wake   ^s snooze") {
		t.Errorf("footer %q", ansi.Strip(footerLine(m.footer(), "", 200)))
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	cmd()
	if len(src.opened) != 2 || src.opened[1].Env["HERDR_SNOOZE_PANE"] != "w3:p2" {
		t.Errorf("opened %v", src.opened)
	}
}

func TestWithoutTheSnoozePluginCtrlSOnlySaysSo(t *testing.T) {
	m, src := linkModel(t, true)
	if strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^s") {
		t.Error("a key for it in the footer")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd != nil || len(src.opened) != 0 || !strings.Contains(next.(model).flash, snoozePlugin) {
		t.Errorf("flash %q, opened %v", next.(model).flash, src.opened)
	}
	// Nor does the filter take it.
	if next.(model).filter.Value() != "" {
		t.Errorf("filter %q", next.(model).filter.Value())
	}
}

// siblings stands in for plugin.list: herdr-snooze's popup comes from it,
// at the size it declares there.
func TestTheSnoozePopupIsTheOneSiblingsFinds(t *testing.T) {
	was := siblings
	t.Cleanup(func() { siblings = was })
	siblings = func() map[string]popupRef {
		return map[string]popupRef{snoozePlugin: {PluginID: snoozePlugin, Entrypoint: "picker", Width: "50", Height: "14"}}
	}
	to, ok := liveSource{}.plugins()[snoozePlugin]
	if !ok {
		t.Fatal("not found")
	}
	p := snoozeFor(to, agentInfo{PaneID: "w2:p5"})
	if p.Width != "50" || p.Height != "14" || p.Env["HERDR_SNOOZE_PANE"] != "w2:p5" ||
		!strings.Contains(p.Env[returnEnv], `"HERDR_RECAP_PANE":"w2:p5"`) {
		t.Errorf("%+v", p)
	}
}

func TestCtrlWWakesASnoozedAgentWithoutAPopup(t *testing.T) {
	m, src := linkModel(t, false)
	// On an awake agent it's the filter's, and there's no wake button.
	if strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^w") {
		t.Error("a wake key on an awake agent")
	}
	m.filter.SetValue("two words")
	m.filter.CursorEnd()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := next.(model).filter.Value(); got != "two " {
		t.Errorf("ctrl+w on an awake agent: filter %q", got)
	}
	m.filter.SetValue("")
	m.refilter()

	m = selectPane(m, "w3:p2")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if cmd == nil {
		t.Fatal("nothing to wake it")
	}
	m = settle(t, next.(model), cmd)
	if len(src.opened) != 0 {
		t.Errorf("opened a popup: %v", src.opened)
	}
	e := m.entries["w3:p2"]
	if snoozed(e.agent) || stageOf(e) == stageSnoozed || !strings.HasPrefix(m.flash, "Woke ") {
		t.Errorf("still snoozed: stage %v, flash %q", stageOf(e), m.flash)
	}
}

func TestWithoutTheSnoozePluginCtrlWIsTheFilters(t *testing.T) {
	m, _ := linkModel(t, true)
	m = selectPane(m, "w3:p2")
	if strings.Contains(ansi.Strip(footerLine(m.footer(), "", 200)), "^w") {
		t.Error("a wake key without snooze")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlW}); cmd != nil {
		if _, woke := cmd().(wokeMsg); woke {
			t.Error("woke it without snooze")
		}
	}
}
