package main

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// stage is where an agent's work has got to, which the list is grouped by:
// its status while that says something (it's asking, it finished a turn,
// it's at work), and once it's idle, its pull request (herdr-github's pr_*
// tokens). In list order: whose move it is, yours first. Snoozed ones come
// last, whatever they're doing: you've said they can wait.
type stage int

const (
	stageNeedsYou stage = iota // blocked on a question or an approval
	stageDone                  // finished a turn you haven't looked at
	stageFixing                // idle, its PR failing, in conflict or sent back
	stageIdle                  // idle, with no PR out for review: yours to set going
	stageWorking               // at work
	stageReview                // idle, its PR open and nothing wrong with it: someone else's
	stageMerged                // idle, its PR merged or closed and nothing new since
	stageSnoozed               // hidden by herdr-snooze until a time
)

// stageOf derives an agent's stage. A status that says something wins over
// the PR: an agent started on the next thing after a merge is at work, not
// merged. So does new work in its worktree, between turns. (Its next branch
// has no PR, so herdr-github clears the tokens and it's idle anyway.)
func stageOf(e *entry) stage {
	if snoozed(e.agent) {
		return stageSnoozed
	}
	switch e.agent.Status {
	case "blocked":
		return stageNeedsYou
	case "done":
		return stageDone
	case "working":
		return stageWorking
	}
	t := e.agent.Tokens
	switch t["pr_state"] {
	case "merged", "closed":
		if e.changes.Files > 0 || e.changes.Ahead > 0 {
			return stageIdle
		}
		return stageMerged
	case "open", "draft":
		if t["pr_checks"] == "✗" || t["pr_review"] == "changes requested" ||
			strings.Contains(t["pr_badge"], "conflicts") {
			return stageFixing
		}
		if t["pr_state"] == "open" {
			return stageReview
		}
	}
	return stageIdle
}

// snoozeMark is herdr-snooze's token on a snoozed pane: there, it's
// snoozed. snoozeBadge is its token for showing: a badge, two spaces and
// when it wakes ("💤  17:30", "💤  Tue 09:00"). Neither is listed among a
// row's tokens: the row says it its own way.
const snoozeMark, snoozeBadge = "herdr_snooze", "snoozed"

// snoozed: herdr-snooze has hidden the agent until a time.
func snoozed(a agentInfo) bool { return a.Tokens[snoozeMark] != "" }

// wakeText is when a snoozed agent wakes, for its row: "until 17:30". The
// badge before the time is left out: the stage already says it's snoozed,
// and an emoji's width is the terminal's guess (and freeze's font has none).
// Just "snoozed" when it doesn't say when.
func wakeText(a agentInfo) string {
	v := strings.TrimSpace(a.Tokens[snoozeBadge])
	if _, at, ok := strings.Cut(v, "  "); ok {
		v = strings.TrimSpace(at)
	}
	if v == "" {
		return "snoozed"
	}
	return "until " + v
}

// stageAll is the tab that lists every stage, grouped under headings.
const stageAll stage = -1

func (s stage) label() string {
	switch s {
	case stageNeedsYou:
		return "Needs you"
	case stageDone:
		return "Done"
	case stageFixing:
		return "Needs fixing"
	case stageWorking:
		return "Working"
	case stageReview:
		return "In review"
	case stageMerged:
		return "Merged"
	case stageSnoozed:
		return "Snoozed"
	}
	return "Idle"
}

// short is the label for a popup too narrow for the tabs in full.
func (s stage) short() string {
	switch s {
	case stageAll:
		return "All"
	case stageNeedsYou:
		return "You"
	case stageDone:
		return "Done"
	case stageFixing:
		return "Fix"
	case stageWorking:
		return "Work"
	case stageReview:
		return "Review"
	case stageMerged:
		return "Merged"
	case stageSnoozed:
		return snoozeIcon
	}
	return "Idle"
}

func (s stage) style() lipgloss.Style {
	switch s {
	case stageNeedsYou:
		return styleBlocked
	case stageDone:
		return styleDone
	case stageFixing:
		return styleErr
	case stageWorking:
		return styleWorking
	case stageReview:
		return styleOK
	case stageMerged:
		return styleMerged
	case stageSnoozed:
		return snoozedStyle()
	}
	return styleIdle
}

// snoozeIcon is herdr-snooze's badge, on the Snoozed tab.
const snoozeIcon = "💤"

// snoozedStyle is what's snoozed, and when it wakes: blue, the colour of
// the 💤 that herdr-snooze puts on it, as herdr-snooze draws it too.
func snoozedStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(look.Blue) }

// stageAt is the stage of the i-th listed entry.
func (m model) stageAt(i int) stage { return stageOf(m.entries[m.order[i]]) }

// headed is whether the i-th listed entry has its stage's heading above it:
// the first of each stage, and whichever entry the list is scrolled to, so
// the top of the list always says what it's showing. Only under All.
func (m model) headed(i int) bool {
	if m.show != stageAll || m.ranked() {
		return false // the tab says it, or they're in order of how well they match
	}
	return i == 0 || i == m.offset || m.stageAt(i) != m.stageAt(i-1)
}

// headingHeight is the lines the i-th entry's heading takes: the heading and
// a blank under it, or none.
func (m model) headingHeight(i int) int {
	if m.headed(i) {
		return 2
	}
	return 0
}

// viewHeading is a stage's heading: its name, and how many are listed in it.
func (m model) viewHeading(s stage) string {
	n := 0
	for i := range m.order {
		if m.stageAt(i) == s {
			n++
		}
	}
	return " " + s.style().Bold(true).Render(s.label()) + styleDim.Render(" "+strconv.Itoa(n))
}

// stageTab is one tab along the top: a stage, how many agents the filter
// leaves in it, and the cells it's drawn in (x0 up to x1), for the mouse.
type stageTab struct {
	stage  stage
	n      int
	x0, x1 int
}

// stageTabs are All, then each stage that has agents, in list order. The
// stage being shown keeps its tab when its last agent leaves it.
func (m model) stageTabs() []stageTab {
	counts := map[stage]int{}
	total := 0
	for _, e := range m.entries {
		if m.matches(e) {
			counts[stageOf(e)]++
			total++
		}
	}
	tabs := []stageTab{{stage: stageAll, n: total}}
	for s := stageNeedsYou; s <= stageSnoozed; s++ {
		if counts[s] > 0 || s == m.show {
			tabs = append(tabs, stageTab{stage: s, n: counts[s]})
		}
	}
	return tabs
}

// tabBar is the popup's first line: the stages as tabs, each in its colour
// with its count, the one shown as a pill. Too wide for the popup, the
// labels shorten.
func (m model) tabBar() (string, []stageTab) {
	tabs := m.stageTabs()
	var line string
	// No name before them: the popup's border has it.
	for _, short := range []bool{false, true} {
		line = " "
		x := lipgloss.Width(line)
		for i, t := range tabs {
			label, style := t.stage.label(), t.stage.style()
			if t.stage == stageAll {
				label, style = "All", styleTabOn
			}
			if short {
				label = t.stage.short()
			} else if t.stage == stageSnoozed {
				label = snoozeIcon + " " + label
			}
			n := strconv.Itoa(t.n)
			var cell string
			switch {
			case t.stage != m.show:
				cell = " " + style.UnsetUnderline().Render(label) + styleDim.Render(" "+n) + " "
			case t.stage == stageAll:
				cell = pill(look.Accent, label+" "+n)
			default:
				cell = statePill(style, label+" "+n)
			}
			line += cell
			tabs[i].x0, tabs[i].x1 = x, x+lipgloss.Width(cell)
			x = tabs[i].x1
		}
		if x <= m.width {
			break
		}
	}
	return line, tabs
}

// showStage lists one stage (or all of them), staying on the selected agent
// when it's still listed.
func (m *model) showStage(s stage) {
	selected := m.selectedPane()
	m.show, m.offset = s, 0
	m.resort(selected)
}

// stepStage moves to the next tab (or the one before), round the ends.
func (m *model) stepStage(d int) {
	tabs := m.stageTabs()
	for i, t := range tabs {
		if t.stage == m.show {
			m.showStage(tabs[(i+d+len(tabs))%len(tabs)].stage)
			return
		}
	}
}
