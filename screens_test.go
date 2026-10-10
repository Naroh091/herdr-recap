package main

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestWriteDemoScreens saves each README screen, as the popup draws it, from
// the demo's agents. It only runs for scripts/screenshots.sh, which sets
// SCREENS_DIR and turns the files into PNGs with freeze.
func TestWriteDemoScreens(t *testing.T) {
	dir := os.Getenv("SCREENS_DIR")
	if dir == "" {
		t.Skip("set SCREENS_DIR to write the README screenshots")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.ANSI256) })
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, m model) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(m.View()+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// herdr's default theme, which freeze's background matches.
	catppuccin := herdrPalettes["catppuccin"]
	useTheme(&catppuccin)
	t.Cleanup(func() { useTheme(nil) })
	m, _ := demoModel(t)
	m.width, m.height = 100, 50

	// Coming back: what needs you first, what each agent got to.
	save("agents", m)

	// Answering one without leaving the list.
	reply := press(press(m, "down"), "tab")
	reply = typeText(reply, "looks good, but before you push:")
	next, _ := reply.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	reply = typeText(next.(model), "squash the fixup commits and open the PR as a draft")
	save("reply", reply)
}

func press(m model, k string) model {
	next, _ := m.Update(keyMsg(k))
	return next.(model)
}
