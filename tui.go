package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ── messages ──────────────────────────────────────────────────────────────────

type agentsMsg struct {
	agents     []agentInfo
	workspaces []workspaceInfo
	err        error
}

// pollMsg re-reads the agents, so statuses stay live while the popup is open.
type pollMsg struct{}

// sessionMsg is a pane's Claude session, found for the status change seq.
type sessionMsg struct {
	pane    string
	seq     int64
	session claudeSession
	cached  *recap
	current bool
	err     error
}

type recapMsg struct {
	pane    string
	session string
	recap   recap
	err     error
}

type focusedMsg struct{ err error }

// changesMsg is git's answer about an agent's worktree, read at `at`.
type changesMsg struct {
	pane, at string
	changes  gitChanges
}

// repliedMsg is how sending a reply went.
type repliedMsg struct {
	pane, title string
	err         error
}

// pollEvery is how often the open popup re-reads herdr's agents.
const pollEvery = time.Second

// maxReplyLines is as tall as the reply box grows before it scrolls.
const maxReplyLines = 6

// maxRecapping bounds how many recaps run at once: each is a claude process.
const maxRecapping = 6

// recapLines is the most lines a recap is given; Claude keeps them under 40
// words, which is two or three lines in a popup.
const recapLines = 3

// ── model ─────────────────────────────────────────────────────────────────────

// entry is one agent's row.
type entry struct {
	agent       agentInfo
	session     *claudeSession
	recap       *recap
	current     bool   // the recap describes the conversation as it is now
	resolvedSeq int64  // the status change the session was last looked up at
	resolving   bool   // looking up the session
	recapping   bool   // a recap is being written
	triedRecap  bool   // one has been asked for since the popup opened
	again       bool   // the conversation moved on during a recap: write another after it
	note        string // why there's no recap: not Claude, nothing said yet, an error
	branch      string // the git branch in the agent's folder
	branchAt    string // the folder and status change branch was read at
	changes     gitChanges
}

type model struct {
	ctx        context.Context
	src        source
	tokens     []string // config.json's tokens: which to show, in order
	width      int
	height     int
	entries    map[string]*entry
	show       stage    // the tab: one stage's agents, or stageAll
	order      []string // pane IDs, as listed: the ones the filter matches
	filter     textinput.Model
	workspaces []workspaceInfo
	cursor     int
	offset     int // first entry shown
	loaded     bool
	spin       spinner.Model
	err        string
	flash      string
	sem        chan struct{} // bounds the recaps running at once
	now        func() time.Time

	mouseX, mouseY int // last pointer position; -1 until the mouse moves

	// A reply being typed to the selected agent (r), sent with agent.prompt.
	replying  bool
	replyTo   string // its pane
	replyText textarea.Model
}

func newModel(ctx context.Context, src source) model {
	sp := spinner.New()
	// A pie filling and emptying: in progress, and unlike braille or ◐◓◑◒
	// it's in common monospace fonts such as JetBrains Mono (a glyph from a
	// fallback font can come out the wrong width). ● stays done's.
	sp.Spinner = spinner.Spinner{Frames: []string{"◔", "◕"}, FPS: time.Second / 3}
	sp.Style = lipgloss.NewStyle()
	ti := textarea.New()
	// A chevron to type after on the first line; the rest line up with it.
	ti.SetPromptFunc(4, func(line int) string {
		if line == 0 {
			return " ›  "
		}
		return "    "
	})
	ti.Placeholder = "yes, go ahead"
	ti.ShowLineNumbers = false
	ti.CharLimit = 8000
	ti.MaxHeight = maxReplyLines
	// Enter sends; these start a new line. (Shift+Enter looks like Enter
	// in most terminals.)
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.SetHeight(1)
	// Typing filters the list. Its text lines up with the titles.
	fi := textinput.New()
	fi.Prompt = " › "
	fi.Placeholder = "Type to filter… ← → to change tabs"
	fi.PromptStyle, fi.PlaceholderStyle = styleDim, styleDim
	fi.Focus()
	return model{
		ctx: ctx, src: src, entries: map[string]*entry{}, spin: sp, filter: fi,
		sem: make(chan struct{}, maxRecapping), now: time.Now, mouseX: -1, mouseY: -1, replyText: ti,
		show: stageAll,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.loadAgents())
}

func (m model) loadAgents() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		agents, workspaces, err := src.agents()
		return agentsMsg{agents, workspaces, err}
	}
}

func (m model) resolve(a agentInfo) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		s, err := src.session(a.PaneID)
		msg := sessionMsg{pane: a.PaneID, seq: a.StateChangeSeq, session: s, err: err}
		if err == nil {
			msg.cached, msg.current = src.cached(s)
		}
		return msg
	}
}

func (m model) recapCmd(pane string, s claudeSession, force bool) tea.Cmd {
	src, ctx, sem := m.src, m.ctx, m.sem
	return func() tea.Msg {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return recapMsg{pane: pane, session: s.ID, err: ctx.Err()}
		}
		defer func() { <-sem }()
		r, err := src.recap(ctx, s, force)
		return recapMsg{pane: pane, session: s.ID, recap: r, err: err}
	}
}

// ── update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.filter.Width = max(10, msg.Width-6)
		m.fitReply()
		m.scrollTo()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case pollMsg:
		return m, m.loadAgents()

	case agentsMsg:
		next := tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} })
		if msg.err != nil {
			m.err = "Couldn't list herdr's agents: " + msg.err.Error()
			return m, next
		}
		if strings.HasPrefix(m.err, "Couldn't list herdr's agents") {
			m.err = ""
		}
		return m, tea.Batch(next, m.updateAgents(msg.agents, msg.workspaces))

	case sessionMsg:
		e := m.entries[msg.pane]
		if e == nil {
			return m, nil
		}
		e.resolving = false
		if msg.err != nil {
			e.session, e.note = nil, sessionNote(msg.err)
			if msg.seq != e.agent.StateChangeSeq {
				// It changed while being looked up: look again.
				return m, m.startResolve(e)
			}
			return m, nil
		}
		if e.session == nil || e.session.ID != msg.session.ID {
			e.recap = nil // a different conversation now: the old recap isn't its
		}
		s := msg.session
		e.session, e.note = &s, ""
		if msg.cached != nil {
			e.recap = msg.cached
		}
		e.current = msg.current
		if s.Transcript == "" {
			e.note = "Nothing to recap yet."
			return m, nil
		}
		// An out-of-date recap is rewritten, but only once for an agent
		// that's working: it'd be out of date again straight away. One
		// already running describes the conversation as it was: another
		// follows it.
		if !e.current && (e.agent.Status != "working" || !e.triedRecap) {
			if e.recapping {
				e.again = true
				return m, nil
			}
			return m, m.startRecap(e, false)
		}
		return m, nil

	case recapMsg:
		e := m.entries[msg.pane]
		if e == nil {
			return m, nil
		}
		e.recapping = false
		again := e.again
		e.again = false
		if e.session != nil && e.session.ID == msg.session {
			switch {
			case errors.Is(msg.err, errNothingYet):
				e.note = "Nothing to recap yet."
			case errors.Is(msg.err, context.Canceled):
			case msg.err != nil:
				e.note = "Couldn't recap: " + msg.err.Error()
			default:
				r := msg.recap
				// Current only if the conversation didn't move on meanwhile.
				e.recap, e.current, e.note = &r, !again, ""
			}
		}
		if again && e.session != nil && e.session.Transcript != "" {
			return m, m.startRecap(e, false)
		}
		return m, nil

	case changesMsg:
		if e := m.entries[msg.pane]; e != nil && e.branchAt == msg.at {
			was := stageOf(e)
			e.changes = msg.changes
			// New work in a merged PR's worktree makes it idle, not merged.
			if stageOf(e) != was {
				m.resort(m.selectedPane())
			}
		}
		return m, nil

	case repliedMsg:
		switch {
		case isHerdrCode(msg.err, "agent_blocked"):
			m.err = shorten(msg.title, 40) + " is waiting on a question or approval: go to it (enter) to answer."
		case msg.err != nil:
			m.err = "Couldn't send the reply: " + msg.err.Error()
		default:
			m.flash = "Sent to " + shorten(msg.title, 60) + "."
		}
		return m, nil

	case focusedMsg:
		if errors.Is(msg.err, errDemo) {
			m.flash = "In the demo that's as far as it goes: the agents are made up."
			return m, nil
		}
		if msg.err != nil {
			m.err = "Couldn't go to that agent: " + msg.err.Error()
			return m, nil
		}
		return m, tea.Quit

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

// updateAgents takes a fresh agent list: new agents get a row, gone ones lose
// theirs, and a Claude agent whose status changed has its session looked up
// again (it may be a new conversation, and its recap out of date).
func (m *model) updateAgents(agents []agentInfo, workspaces []workspaceInfo) tea.Cmd {
	selected := m.selectedPane()
	m.loaded, m.workspaces = true, workspaces
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, a := range agents {
		seen[a.PaneID] = true
		e := m.entries[a.PaneID]
		if e == nil {
			e = &entry{resolvedSeq: -1}
			m.entries[a.PaneID] = e
		}
		kindChanged := e.agent.Agent != a.Agent
		e.agent = a
		// The branch can change with the folder or on any turn: read it again
		// then (a few small files), not every second.
		if at := a.Cwd + "\x00" + strconv.FormatInt(a.StateChangeSeq, 10); a.Cwd != "" && e.branchAt != at {
			e.branch, e.branchAt = gitBranch(a.Cwd), at
			src, pane, cwd := m.src, a.PaneID, a.Cwd
			cmds = append(cmds, func() tea.Msg { return changesMsg{pane, at, src.changes(cwd)} })
		}
		if a.Agent != "claude" {
			e.session, e.recap, e.note = nil, nil, "Recaps are for Claude agents."
			continue
		}
		if kindChanged && e.note == "Recaps are for Claude agents." {
			e.note = ""
		}
		if !e.resolving && e.resolvedSeq != a.StateChangeSeq {
			cmds = append(cmds, m.startResolve(e))
		}
	}
	for pane := range m.entries {
		if !seen[pane] {
			delete(m.entries, pane)
		}
	}
	m.resort(selected)
	return tea.Batch(cmds...)
}

// resort orders the list again, keeping the selection on the agent it was on.
func (m *model) resort(selected string) {
	m.sortEntries()
	m.cursor = 0
	for i, p := range m.order {
		if p == selected {
			m.cursor = i
		}
	}
	m.scrollTo()
}

func (m *model) startResolve(e *entry) tea.Cmd {
	e.resolving, e.resolvedSeq = true, e.agent.StateChangeSeq
	return m.resolve(e.agent)
}

func (m *model) startRecap(e *entry, force bool) tea.Cmd {
	e.recapping, e.triedRecap = true, true
	if e.note != "" && strings.HasPrefix(e.note, "Couldn't recap") {
		e.note = ""
	}
	return m.recapCmd(e.agent.PaneID, *e.session, force)
}

// sessionNote says why an agent's session couldn't be found.
func sessionNote(err error) string {
	if errors.Is(err, errNotClaude) {
		return "Claude isn't running in this pane any more."
	}
	return "Couldn't find its conversation: " + err.Error()
}

func (m *model) sortEntries() {
	wsIndex := map[string]int{}
	for i, w := range m.workspaces {
		wsIndex[w.WorkspaceID] = i
	}
	m.order = m.order[:0]
	for p, e := range m.entries {
		staged := m.show == stageAll || stageOf(e) == m.show || (m.replying && p == m.replyTo)
		if staged && m.matches(e) {
			m.order = append(m.order, p)
		}
	}
	sort.Slice(m.order, func(i, j int) bool {
		a, b := m.entries[m.order[i]].agent, m.entries[m.order[j]].agent
		if ra, rb := stageOf(m.entries[m.order[i]]), stageOf(m.entries[m.order[j]]); ra != rb {
			return ra < rb
		}
		// Within a stage, what has waited longest first.
		sa, sb := m.inStateSince(m.entries[m.order[i]]), m.inStateSince(m.entries[m.order[j]])
		if !sa.Equal(sb) && !sa.IsZero() && !sb.IsZero() {
			return sa.Before(sb)
		}
		if sa.IsZero() != sb.IsZero() {
			return !sa.IsZero()
		}
		wa, oka := wsIndex[a.WorkspaceID]
		wb, okb := wsIndex[b.WorkspaceID]
		if oka != okb {
			return oka
		}
		if wa != wb {
			return wa < wb
		}
		return paneLess(a.PaneID, b.PaneID)
	})
}

// matches says whether every word of the filter is somewhere in what the
// agent's row shows: its title, status, stage, space, branch, what it's
// waiting for, its recap or its tokens. The agent being replied to stays listed
// whatever the filter says.
func (m model) matches(e *entry) bool {
	words := strings.Fields(strings.ToLower(m.filter.Value()))
	if len(words) == 0 || (m.replying && e.agent.PaneID == m.replyTo) {
		return true
	}
	hay := []string{title(e.agent), e.agent.Agent, stageOf(e).label(), e.branch}
	// Idle is a stage too: the word finds that stage's agents, not every
	// agent that's idle with a PR in review or merged.
	if e.agent.Status != "idle" {
		hay = append(hay, e.agent.Status)
	}
	if e.agent.Status == "blocked" {
		// As the header and the row put it.
		hay = append(hay, "needs you", "waiting")
	}
	for _, ws := range m.workspaces {
		if ws.WorkspaceID == e.agent.WorkspaceID {
			hay = append(hay, ws.Label)
		}
	}
	if e.session != nil {
		hay = append(hay, e.session.Meta.Branch)
		if e.agent.Status == "blocked" {
			hay = append(hay, e.session.Meta.Pending)
		}
	}
	if e.recap != nil {
		hay = append(hay, e.recap.Text)
	}
	hay = append(hay, tokenValues(e.agent.Tokens, m.tokens)...)
	all := strings.ToLower(strings.Join(hay, " "))
	for _, w := range words {
		if !strings.Contains(all, w) {
			return false
		}
	}
	return true
}

// refilter lists what a changed filter matches, from the top.
func (m *model) refilter() {
	m.sortEntries()
	m.cursor, m.offset = 0, 0
	m.scrollTo()
}

// paneLess orders pane IDs by their numbers, so w1:p2 comes before w1:p10.
func paneLess(a, b string) bool {
	na, nb := paneNumber(a), paneNumber(b)
	if na != nb {
		return na < nb
	}
	return a < b
}

func paneNumber(id string) int {
	_, p, _ := strings.Cut(id, ":p")
	n, err := strconv.Atoi(p)
	if err != nil {
		return 1 << 30
	}
	return n
}

func (m model) selectedPane() string {
	if m.cursor >= 0 && m.cursor < len(m.order) {
		return m.order[m.cursor]
	}
	return ""
}

func (m model) selected() *entry {
	return m.entries[m.selectedPane()]
}

func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.replying {
		return m.handleReplyKey(k)
	}
	m.flash = ""
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.refilter()
			return m, nil
		}
		if m.show != stageAll {
			m.showStage(stageAll)
			return m, nil
		}
		return m, tea.Quit
	case "tab":
		return m.startReply()
	case "left":
		m.stepStage(-1)
		return m, nil
	case "right":
		m.stepStage(1)
		return m, nil
	case "up", "ctrl+p", "ctrl+k":
		m.move(-1)
		return m, nil
	case "down", "ctrl+n", "ctrl+j":
		m.move(1)
		return m, nil
	case "pgup":
		m.move(-max(1, m.visibleEntries()))
		return m, nil
	case "pgdown":
		m.move(max(1, m.visibleEntries()))
		return m, nil
	case "home":
		m.move(-len(m.order))
		return m, nil
	case "end":
		m.move(len(m.order))
		return m, nil
	case "enter":
		return m.activate()
	case "ctrl+r":
		return m.recapAgain()
	}

	// Anything else edits the filter.
	prev := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	if m.filter.Value() != prev {
		m.refilter()
	}
	return m, cmd
}

// startReply opens the reply line for the selected agent.
func (m model) startReply() (tea.Model, tea.Cmd) {
	e := m.selected()
	if e == nil {
		return m, nil
	}
	m.replying, m.replyTo, m.err = true, e.agent.PaneID, ""
	m.filter.Blur()
	m.replyText.SetValue("")
	m.fitReply()
	return m, m.replyText.Focus()
}

// handleReplyKey is typing a reply: enter sends it, esc drops it.
func (m model) handleReplyKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.endReply()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.replyText.Value())
		m.endReply()
		e := m.entries[m.replyTo]
		if text == "" || e == nil {
			return m, nil
		}
		src, pane, name := m.src, e.agent.PaneID, title(e.agent)
		return m, func() tea.Msg { return repliedMsg{pane, name, src.prompt(pane, text)} }
	}
	var cmd tea.Cmd
	m.replyText, cmd = m.replyText.Update(k)
	m.fitReply()
	m.scrollTo()
	return m, cmd
}

// endReply closes the reply line: typing filters the list again.
func (m *model) endReply() {
	m.replying = false
	m.replyText.Blur()
	m.filter.Focus()
}

// fitReply sizes the reply box to what's typed, up to maxReplyLines.
func (m *model) fitReply() {
	m.replyText.SetWidth(max(10, m.width-4))
	m.replyText.SetHeight(min(maxReplyLines, max(1, m.replyText.LineCount())))
}

// replyLines is the room the reply takes below the list: its heading and
// its box (0 when there's no reply being typed).
func (m model) replyLines() int {
	if !m.replying {
		return 0
	}
	return 1 + m.replyText.Height()
}

// activate goes to the selected agent, closing the popup once herdr has.
func (m model) activate() (tea.Model, tea.Cmd) {
	e := m.selected()
	if e == nil {
		return m, nil
	}
	src, pane := m.src, e.agent.PaneID
	return m, func() tea.Msg { return focusedMsg{src.focus(pane)} }
}

// recapAgain writes the selected agent's recap anew, current or not.
func (m model) recapAgain() (tea.Model, tea.Cmd) {
	e := m.selected()
	switch {
	case e == nil:
		return m, nil
	case e.session == nil || e.session.Transcript == "":
		if e.note != "" {
			m.flash = e.note
		}
		return m, nil
	case e.recapping:
		return m, nil
	}
	return m, m.startRecap(e, true)
}

func (m *model) move(delta int) {
	if len(m.order) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.order)-1)
	m.scrollTo()
}

// ── layout ────────────────────────────────────────────────────────────────────

// listTop is the first list line: under the tabs, a blank, the filter and
// another blank.
const listTop = 4

func (m model) listHeight() int {
	// tabs + blank + filter + blank above; status line + footer below, the footer
	// on the popup's last line (mouse.go counts on that).
	// A reply being typed takes the status line and more.
	return max(3, m.height-listTop-2-max(0, m.replyLines()-1))
}

// maxRecapWidth keeps recap lines to a comfortable measure on a wide popup.
const maxRecapWidth = 80

// recapWidth is the room a recap line has: indented under the title, and no
// wider than maxRecapWidth.
func (m model) recapWidth() int { return max(10, min(maxRecapWidth, m.width-6)) }

// body is what goes under an agent's title: its recap wrapped, or why there
// isn't one.
func (m model) body(e *entry) (lines []string, dim bool) {
	w := m.recapWidth()
	switch {
	case e.recap != nil && e.recap.Text != "":
		return wrap(e.recap.Text, w, recapLines), !e.current
	case e.recapping:
		return []string{"Recapping…"}, true
	case e.resolving && e.note == "":
		return []string{"…"}, true
	case e.note != "":
		return wrap(e.note, w, 2), true
	}
	return []string{""}, true
}

// entryHeight is an entry's lines on screen, as viewEntry draws them,
// counted without drawing: scrolling and the mouse ask for it a lot. (A
// test holds the two to each other.)
func (m model) entryHeight(i int) int {
	e := m.entries[m.order[i]]
	var meta *sessionMeta
	if e.session != nil {
		meta = &e.session.Meta
	}
	body, _ := m.body(e)
	n := 1 + len(body) + 1 // title, recap, a blank after
	if m.hasDetails(e, meta) {
		n++
	}
	if e.agent.Status == "blocked" && meta != nil && meta.Pending != "" {
		n++
	}
	return n
}

// hasDetails is whether details has anything to say, cheaply.
func (m model) hasDetails(e *entry, meta *sessionMeta) bool {
	return e.branch != "" || (meta != nil && (meta.Branch != "" || meta.TasksTotal > 0)) ||
		!e.changes.empty() || (e.agent.Agent != "claude" && e.agent.Agent != "") ||
		len(tokenValues(e.agent.Tokens, m.tokens)) > 0 ||
		(e.recap != nil && (e.recapping || !e.current))
}

// unusualMode is a permission mode worth saying: anything but the default.
func unusualMode(mode string) bool { return mode != "" && mode != "default" }

// visibleEntries is how many whole entries fit from the offset.
func (m model) visibleEntries() int {
	n, used := 0, 0
	for i := m.offset; i < len(m.order); i++ {
		used += m.headingHeight(i) + m.entryHeight(i)
		if used > m.listHeight()+1 { // the last entry's trailing blank may not fit
			break
		}
		n++
	}
	return n
}

func (m *model) scrollTo() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.offset < m.cursor {
		used := 0
		for i := m.offset; i <= m.cursor; i++ {
			used += m.headingHeight(i) + m.entryHeight(i)
		}
		if used <= m.listHeight()+1 {
			break
		}
		m.offset++
	}
	m.offset = max(0, min(m.offset, len(m.order)-1))
}

// entryAt is the entry drawn on screen line y, if any.
func (m model) entryAt(y int) (int, bool) {
	line := listTop
	for i := m.offset; i < len(m.order); i++ {
		line += m.headingHeight(i) // a stage's heading isn't an entry
		h := m.entryHeight(i)
		if y >= line && y < line+h-1 { // not its trailing blank
			return i, true
		}
		line += h
		if line >= listTop+m.listHeight() {
			break
		}
	}
	return 0, false
}

// wrap breaks text into lines of at most w cells, at most n of them, the
// last ending in … if the text runs on.
func wrap(text string, w, n int) []string {
	words := strings.Fields(text)
	var lines []string
	cur := ""
	for i, word := range words {
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if ansi.StringWidth(next) <= w || cur == "" {
			cur = next
			continue
		}
		lines = append(lines, cur)
		cur = word
		if len(lines) == n {
			last := lines[n-1] + " " + strings.Join(words[i:], " ")
			lines[n-1] = shorten(last, w)
			return lines
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	for i, l := range lines {
		lines[i] = shorten(l, w)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

func shorten(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= n {
		return s
	}
	return strings.TrimRight(ansi.Truncate(s, n-1, ""), " ") + "…"
}

// ago is how long ago t was, briefly: "just now", "4m ago", "2h ago".
func ago(now, t time.Time) string {
	if d := now.Sub(t); d >= time.Minute {
		return duration(d) + " ago"
	}
	return "just now"
}

// ── view ──────────────────────────────────────────────────────────────────────

// View is what Bubble Tea writes to the terminal. Every frame passes through
// screenSafe, whatever path its text took to get there.
func (m model) View() string {
	return screenSafe(m.view())
}

func (m model) view() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.viewHeader() + "\n\n" + m.filter.View() + "\n\n")
	h := m.listHeight()
	used := 0
	switch {
	case !m.loaded:
		b.WriteString("  " + m.spin.View() + " Loading…\n")
		used++
	case len(m.order) == 0 && len(m.entries) > 0 && m.show != stageAll && m.filter.Value() == "":
		b.WriteString(styleDim.Render("  No agents at this stage.") + "\n")
		used++
	case len(m.order) == 0 && len(m.entries) > 0:
		b.WriteString(styleDim.Render("  Nothing matches.") + "\n")
		used++
	case len(m.order) == 0:
		b.WriteString(styleDim.Render("  No agents running.") + "\n")
		used++
	}
	for i := m.offset; i < len(m.order) && used < h; i++ {
		if m.headed(i) {
			// With the blank under it, when there's room for both.
			for _, l := range []string{m.viewHeading(m.stageAt(i)), ""}[:min(2, h-used)] {
				b.WriteString(l + "\n")
				used++
			}
		}
		for _, l := range m.viewEntry(m.entries[m.order[i]], i == m.cursor) {
			if used == h {
				break
			}
			b.WriteString(l + "\n")
			used++
		}
	}
	for ; used < h; used++ {
		b.WriteString("\n")
	}
	b.WriteString(m.statusLine())
	b.WriteString(m.renderFooter(m.footer()))
	return b.String()
}

// viewHeader is the stages as tabs, and how many recaps are being written.
func (m model) viewHeader() string {
	recapping := 0
	for _, e := range m.entries {
		if e.recapping {
			recapping++
		}
	}
	line, _ := m.tabBar()
	if recapping > 0 {
		right := m.spin.View() + styleDim.Render(fmt.Sprintf(" recapping %d ", recapping))
		if pad := m.width - lipgloss.Width(line) - lipgloss.Width(right); pad > 1 {
			line += strings.Repeat(" ", pad) + right
		}
	}
	return line
}

// title is what the row is called: the agent's own terminal title (Claude's
// title for the conversation), else its name, else its folder.
func title(a agentInfo) string {
	for _, t := range []string{a.Title, a.Name, filepath.Base(a.Cwd)} {
		if t = strings.TrimSpace(t); t != "" && t != "." && t != "/" && !strings.EqualFold(t, "claude code") {
			return t
		}
	}
	return a.Agent
}

func (m model) viewEntry(e *entry, selected bool) []string {
	w := m.width
	var meta *sessionMeta
	if e.session != nil {
		meta = &e.session.Meta
	}
	// Right of the title: its space, and when it last did anything.
	var right []string
	if len(m.workspaces) > 1 {
		for _, ws := range m.workspaces {
			if ws.WorkspaceID == e.agent.WorkspaceID && ws.Label != "" {
				right = append(right, shorten(ws.Label, 24))
			}
		}
	}
	if s := m.inState(e); s != "" {
		right = append(right, s)
	}
	left := " " + agentMark(e.agent.Status, m.spin.View()) + " "
	style := styleTitle
	if e.agent.Status == "working" {
		style = styleWorking.Bold(true)
	}
	rightText := styleDim.Render(strings.Join(right, " · "))
	room := max(1, w-lipgloss.Width(left)-lipgloss.Width(rightText)-2)
	lines := []string{fitRow(left, style.Render(shorten(title(e.agent), room)), rightText, w)}

	// What a blocked agent is waiting for: its pending question or request.
	if e.agent.Status == "blocked" && meta != nil && meta.Pending != "" {
		lines = append(lines, "   "+styleBlocked.Render(shorten("? "+meta.Pending, max(1, w-4))))
	}
	// The recap right under the title, where the eye lands; on the selected
	// row in the title's colour, to stand out against the highlight.
	recap := styleText
	if selected {
		recap = styleTitle.UnsetBold()
	}
	body, dim := m.body(e)
	for _, l := range body {
		if dim {
			l = styleDim.Render(l)
		} else {
			l = recap.Render(l)
		}
		lines = append(lines, "   "+l)
	}
	if info := m.details(e, meta); len(info) > 0 {
		lines = append(lines, "   "+shorten(strings.Join(info, styleDim.Render(" · ")), max(1, w-4)))
	}
	// The selected agent's model, mode and last prompt are on the status
	// line instead (about), so selecting a row doesn't change its height.
	if selected {
		for i, l := range lines {
			lines[i] = highlight(l, w)
		}
	}
	return append(lines, "")
}

// details is the line under a title, each piece in its own colour: branch,
// uncommitted and unpushed work, task progress, the pane's tokens, and how
// current the recap is. Model and mode are the selected row's, in about.
func (m model) details(e *entry, meta *sessionMeta) []string {
	var info []string
	// The folder's branch, read at the last status change, is the live one;
	// the transcript's is what it was at its last message.
	branch := e.branch
	if branch == "" && meta != nil {
		branch = meta.Branch
	}
	if branch != "" {
		// No branch glyph: ⎇ isn't in common monospace fonts. First, in its
		// own colour, it reads as the branch anyway.
		info = append(info, styleBranch.Render(shorten(branch, 40)))
	}
	if c := changesText(e.changes); c != "" {
		info = append(info, c)
	}
	if meta != nil && meta.TasksTotal > 0 {
		info = append(info, styleTasks.Render(fmt.Sprintf("%d/%d tasks", meta.TasksDone, meta.TasksTotal)))
	}
	if e.agent.Agent != "claude" && e.agent.Agent != "" {
		info = append(info, styleModel.Render(e.agent.Agent))
	}
	for _, v := range tokenValues(e.agent.Tokens, m.tokens) {
		info = append(info, tokenText(v))
	}
	switch {
	case e.recapping && e.recap != nil:
		info = append(info, styleDim.Render("recapping…"))
	case e.recap != nil && !e.current:
		info = append(info, styleDim.Render("recap from "+ago(m.now(), e.recap.At)))
	}
	return info
}

// changesText is a worktree's work in progress: "4 files +120 −30 ↑1 ↓2",
// the counts in the theme's colours; "" when there's none.
func changesText(c gitChanges) string {
	var parts []string
	if c.Files > 0 {
		unit := " files"
		if c.Files == 1 {
			unit = " file"
		}
		parts = append(parts, styleMode.Render(strconv.Itoa(c.Files)+unit))
	}
	if c.Added > 0 {
		parts = append(parts, styleOK.Render("+"+strconv.Itoa(c.Added)))
	}
	if c.Deleted > 0 {
		parts = append(parts, styleErr.Render("−"+strconv.Itoa(c.Deleted)))
	}
	if c.Ahead > 0 {
		parts = append(parts, styleMode.Render("↑"+strconv.Itoa(c.Ahead)))
	}
	if c.Behind > 0 {
		parts = append(parts, styleMode.Render("↓"+strconv.Itoa(c.Behind)))
	}
	return strings.Join(parts, " ")
}

// inStateSince is when the agent entered its status, as far as its
// conversation shows: when it started waiting on you (its pending request),
// when you last prompted it (working), or its last message (done, idle).
func (m model) inStateSince(e *entry) time.Time {
	if e == nil || e.session == nil {
		return time.Time{}
	}
	meta := e.session.Meta
	switch e.agent.Status {
	case "blocked":
		if !meta.PendingAt.IsZero() {
			return meta.PendingAt
		}
	case "working":
		if !meta.PromptAt.IsZero() {
			return meta.PromptAt
		}
	}
	return meta.Active
}

// inState is the status and how long it's lasted: "waiting 3m", "done 25m".
// An idle agent whose PR gives it another stage (in review, merged) is just
// "for 3h": idle is the stage of the ones with no PR out.
func (m model) inState(e *entry) string {
	since := m.inStateSince(e)
	if since.IsZero() {
		return ""
	}
	label := map[string]string{"blocked": "waiting", "working": "working", "done": "done", "idle": "idle"}[e.agent.Status]
	if label == "" {
		return ""
	}
	if e.agent.Status == "idle" && stageOf(e) != stageIdle {
		label = "for"
	}
	return label + " " + duration(m.now().Sub(since))
}

// duration is a span, briefly: "<1m", "4m", "2h", "3d".
func duration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// tokenValues are a pane's tokens to show, as values: the ones named in
// config.json in that order, or else all of them by name. herdr-github says
// a PR at several lengths, and the fullest there is enough: prs (the PR with
// its state and checks, and any others the agent opened) for pr and the pr_*
// details, or else pr for the details.
func tokenValues(tokens map[string]string, only []string) []string {
	var names []string
	if len(only) > 0 {
		names = only
	} else {
		pr, prs := tokens["pr"] != "", tokens["prs"] != ""
		for k := range tokens {
			if (k == "pr" && prs) || (strings.HasPrefix(k, "pr_") && (pr || prs)) {
				continue
			}
			names = append(names, k)
		}
		sort.Strings(names)
	}
	var vals []string
	seen := map[string]bool{}
	for _, k := range names {
		if v := strings.TrimSpace(tokens[k]); v != "" && !seen[v] {
			seen[v] = true
			vals = append(vals, shorten(v, 30))
		}
	}
	return vals
}

// fitRow lays out left + title + right-aligned meta in exactly w cells,
// truncating the title when space runs out.
func fitRow(left, title, right string, w int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	room := w - lw - rw - 2
	if room < 8 {
		right, rw = "", 0
		room = w - lw - 1
	}
	title = shorten(title, max(1, room))
	pad := max(1, w-lw-lipgloss.Width(title)-rw-1)
	return left + title + strings.Repeat(" ", pad) + right
}

// highlight gives a whole line the selection background, w cells wide. The
// line's own coloured pieces each end in a style reset, which would also end
// the background, so it's switched back on after every one.
func highlight(line string, w int) string {
	on := styleSelected.Render("x")
	on = on[:strings.Index(on, "x")] // the escape that turns the background on
	if on != "" {
		line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+on)
	}
	return styleSelected.Width(w).Render(line)
}

// statusLine is the one line above the footer: the last error, or a note.
func (m model) statusLine() string {
	if m.replying {
		name := "agent"
		if e := m.entries[m.replyTo]; e != nil {
			name = shorten(title(e.agent), 30)
		}
		return " " + styleTabOn.Render("Reply to "+name+":") + styleDim.Render("  alt+enter new line") + "\n" +
			m.replyText.View() + "\n"
	}
	switch {
	case m.err != "":
		return " " + styleErr.Render(shorten(clean(m.err, false), max(10, m.width-2))) + "\n"
	case m.flash != "":
		return " " + styleDim.Render(shorten(clean(m.flash, false), max(10, m.width-2))) + "\n"
	}
	return m.about() + "\n"
}

// about is the selected agent's last prompt, with its model and (an
// unusual) mode on the right, for the status line.
func (m model) about() string {
	if m.cursor >= len(m.order) {
		return ""
	}
	e := m.entries[m.order[m.cursor]]
	if e == nil || e.session == nil {
		return ""
	}
	meta := e.session.Meta
	var right []string
	if meta.Model != "" {
		right = append(right, styleModel.Render(shortModel(meta.Model)))
	}
	if unusualMode(meta.Mode) {
		right = append(right, styleMode.Render(meta.Mode))
	}
	rightText := strings.Join(right, styleDim.Render(" · "))
	if meta.LastPrompt == "" {
		if rightText == "" {
			return ""
		}
		return strings.Repeat(" ", max(1, m.width-lipgloss.Width(rightText)-1)) + rightText
	}
	return fitRow(" ", styleDim.Italic(true).Render("› "+meta.LastPrompt), rightText, m.width)
}

func (m model) footer() []hint {
	if m.replying {
		return []hint{{"enter send", "enter", hintAct}, {"esc cancel", "esc", hintQuiet}}
	}
	esc := "esc close"
	switch {
	case m.filter.Value() != "":
		esc = "esc clear"
	case m.show != stageAll:
		esc = "esc all"
	}
	// By kind, so the colours sit together: go, act, view, leave.
	return []hint{{"enter go to agent", "enter", hintGo}, {"tab reply", "tab", hintAct}, {"^r recap again", "ctrl+r", hintView}, {"←→ stage", "right", hintView}, {esc, "esc", hintQuiet}}
}

func runPicker(ctx context.Context, cfg config, demo bool) error {
	// Ask the terminal for its background now: once the program owns stdin,
	// the reply would arrive as stray input.
	if cfg.Theme != "dark" && cfg.Theme != "light" {
		cfg.Theme = "light"
		if lipgloss.HasDarkBackground() {
			cfg.Theme = "dark"
		}
	}
	darkTerminal = cfg.Theme == "dark"
	useTheme(pickerTheme(darkTerminal))
	if !demo {
		defer notePopup()()
	}
	var src source = liveSource{cfg}
	if demo {
		src = newDemoSource()
	}
	m := newModel(ctx, src)
	m.tokens = cfg.Tokens
	// WithContext: a SIGTERM (a newer open replacing this popup) ends it
	// cleanly, restoring the terminal.
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion(), tea.WithContext(ctx)).Run()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

// debugList prints the rows as the popup would find them, without drawing
// it: for checking what herdr and Claude report.
func debugList(cfg config) error {
	src := liveSource{cfg}
	agents, _, err := src.agents()
	if err != nil {
		return err
	}
	for _, a := range agents {
		fmt.Printf("%s  %-8s %-7s %s\n", a.PaneID, a.Status, a.Agent, title(a))
		if a.Agent != "claude" {
			continue
		}
		s, err := src.session(a.PaneID)
		if err != nil {
			fmt.Println("    session:", err)
			continue
		}
		fmt.Printf("    session %s  config %s\n    transcript %s\n", s.ID, s.ConfigDir, s.Transcript)
		mt := s.Meta
		c := readChanges(a.Cwd)
		fmt.Printf("    branch %q (folder %q)  model %s  mode %q  tasks %d/%d  changes %+v\n",
			mt.Branch, gitBranch(a.Cwd), mt.Model, mt.Mode, mt.TasksDone, mt.TasksTotal, c)
		fmt.Printf("    active %s  prompted %s  pending %q\n    last prompt %q\n",
			mt.Active.Format(time.RFC3339), mt.PromptAt.Format(time.RFC3339), mt.Pending, shorten(mt.LastPrompt, 80))
		if r, current := src.cached(s); r != nil {
			fmt.Printf("    recap (%s, current=%v): %s\n", r.At.Format(time.RFC3339), current, r.Text)
		}
	}
	fmt.Fprintln(os.Stderr, "state:", stateDir())
	return nil
}
