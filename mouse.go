package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// renderFooter draws the keys along the bottom, the one under the pointer
// lit so it reads as clickable.
func (m model) renderFooter(hs []hint) string {
	hot := ""
	if m.mouseY == m.height-1 {
		hot = hintAt(hs, m.mouseX, m.width)
	}
	return footerLine(hs, hot, m.width)
}

// handleMouse: hovering highlights, one click goes to the agent, the wheel
// scrolls. Footer hints are buttons, and so are a suggested title's.
func (m model) handleMouse(ev tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.mouseX, m.mouseY = ev.X, ev.Y
	if ev.Action == tea.MouseActionMotion {
		// Hover highlights, like a launcher: the click then opens what you
		// see. Not while replying: the reply is to the selected agent.
		if i, ok := m.entryAt(ev.Y); ok && !m.replying && i != m.cursor {
			m.cursor = i
			m.scrollTo() // selected, it grows: keep it all on screen
		}
		return m, nil
	}
	if ev.Action != tea.MouseActionPress {
		return m, nil
	}
	switch ev.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		if !m.replying {
			if ev.Button == tea.MouseButtonWheelUp {
				m.move(-1)
			} else {
				m.move(1)
			}
		}
		return m, nil
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}
	if ev.Y == m.height-1 {
		if k := hintAt(m.footer(), ev.X, m.width); k != "" {
			return m.handleKey(keyMsg(k))
		}
		return m, nil
	}
	// A stage's tab. Not while replying: the reply is to an agent listed now.
	if ev.Y == 0 && !m.replying {
		_, tabs := m.tabBar()
		for _, t := range tabs {
			if ev.X >= t.x0 && ev.X < t.x1 {
				m.flash = ""
				m.showStage(t.stage)
			}
		}
		return m, nil
	}
	// A click in the list while replying would leave, losing the reply.
	if i, row, ok := m.entryLineAt(ev.Y); ok && !m.replying {
		m.cursor, m.flash = i, ""
		// The buttons after a suggested title.
		if e := m.selected(); row == 1 && e.renaming == "" && suggestion(e) != "" {
			_, rename, ignore := m.suggestionLine(e)
			switch {
			case ev.X >= rename[0]-1 && ev.X <= rename[1]:
				return m.rename()
			case ev.X > ignore[0]-2 && ev.X <= ignore[1]:
				return m.ignore()
			}
		}
		return m.activate()
	}
	return m, nil
}
