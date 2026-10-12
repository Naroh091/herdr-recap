package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// seekFor types a filter and lets the search it sets off finish.
func seekFor(t *testing.T, m model, filter string) model {
	t.Helper()
	delay := seekDelay
	seekDelay = 0
	t.Cleanup(func() { seekDelay = delay })
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(filter)})
	return settle(t, next.(model), cmd)
}

func TestAFilterNoRowMatchesIsLookedForInTheConversations(t *testing.T) {
	m, _ := demoModel(t)
	// On no row: only in what the agent said.
	m = seekFor(t, m, "Playwright")
	if got := paneOrder(m); got != "w1:p3" {
		t.Fatalf("lists %q", got)
	}
	v := ansi.Strip(m.View())
	for _, want := range []string{"found in these conversations", "Flaky checkout e2e test", "order total with Playwright's toHaveText"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	// What was said is there in place of the recap, the row no taller.
	if strings.Contains(v, "it raced the cart animation, now") {
		t.Errorf("the recap is still there:\n%s", v)
	}
	lines := strings.Split(v, "\n")
	if i, ok := m.entryAt(listTop + 2); !ok || i != 0 || !strings.Contains(lines[listTop+2], "Flaky checkout") {
		t.Errorf("the row isn't where the mouse takes it to be: %q", lines[listTop+2])
	}

	// Every word, wherever in the conversation.
	m = press(m, "esc")
	if len(m.order) != len(m.entries) || m.found != nil {
		t.Fatalf("esc left %s", paneOrder(m))
	}
	if m = seekFor(t, m, "signature upgrade"); paneOrder(m) != "w3:p2" {
		t.Errorf("lists %q", paneOrder(m))
	}
}

func TestTheConversationsAreOnlySearchedWhenNoRowMatches(t *testing.T) {
	m, _ := demoModel(t)
	// "checkout" is on a row, and "animation" in more than its recap.
	if m = seekFor(t, m, "checkout"); m.found != nil || m.seeking || paneOrder(m) != "w1:p3" {
		t.Errorf("searched for a filter a row matches: %v, lists %s", m.found, paneOrder(m))
	}
	m = press(m, "esc")
	// Too short to be worth it.
	if m = seekFor(t, m, "qz"); m.seeking || m.foundFor != "" {
		t.Errorf("searched for two letters")
	}
	m = press(m, "esc")
	m = seekFor(t, m, "nobody said this")
	if v := ansi.Strip(m.View()); len(m.order) != 0 || !strings.Contains(v, "in the agents' conversations either") {
		t.Errorf("view:\n%s", v)
	}
}

func TestATranscriptIsSearchedForWhatWasSaid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"why does the Importer drop rows with an empty SKU?"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"the quux flag"},{"type":"text","text":"It skips them in normalize(): an empty SKU fails the uniqueness check, so the row is dropped silently."},{"type":"tool_use","name":"Read","input":{"file_path":"/src/xylophone.go"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"package xylophone // marimba"}]}}`,
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"caveat: glockenspiel"}}`,
		// Codex's records.
		`{"type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"sandbox: vibraphone"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Added a warning to the import log for each dropped row."}]}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","output":"celesta"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	find := func(filter string) string {
		return searchTranscript(context.Background(), path, searchWords(filter))
	}
	for filter, want := range map[string]string{
		"uniqueness":       "an empty SKU fails the uniqueness check",
		"IMPORTER sku":     "why does the Importer drop rows",
		"import log":       "Added a warning to the import log",
		"silently warning": "", // each in a message, and both wanted: the one with the later word
	} {
		got := find(filter)
		if want == "" {
			if got == "" {
				t.Errorf("%q: not found, though every word was said", filter)
			}
		} else if !strings.Contains(got, want) {
			t.Errorf("%q: %q", filter, got)
		}
	}
	// What tools read and printed, what was thought, and what neither of you
	// said isn't searched.
	for _, filter := range []string{"xylophone", "marimba", "quux", "glockenspiel", "vibraphone", "celesta", "sku nowhere"} {
		if got := find(filter); got != "" {
			t.Errorf("%q found: %q", filter, got)
		}
	}
	if got := searchTranscript(context.Background(), filepath.Join(t.TempDir(), "none.jsonl"), []string{"sku"}); got != "" {
		t.Errorf("a missing transcript: %q", got)
	}
}

func TestWhatWasSaidIsShownAroundWhatMatched(t *testing.T) {
	long := strings.Repeat("before ", 60) + "the Needle is here " + strings.Repeat("after ", 60)
	got := around(long, []string{"needle"}, 80)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") || !strings.Contains(got, "the Needle is here") || len([]rune(got)) > 82 {
		t.Errorf("%q", got)
	}
	if got := around("short\nand  sweet", []string{"sweet"}, 80); got != "short and sweet" {
		t.Errorf("%q", got)
	}
	// A line longer than is read is passed over, not the rest of the file.
	path := filepath.Join(t.TempDir(), "long.jsonl")
	huge := `{"type":"user","message":{"role":"user","content":"` + strings.Repeat("needle ", searchLineMax/7+1) + `"}}`
	after := `{"type":"user","message":{"role":"user","content":"the needle after it"}}`
	if err := os.WriteFile(path, []byte(huge+"\n"+after+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := searchTranscript(context.Background(), path, []string{"needle"}); got != "the needle after it" {
		t.Errorf("%.80q", got)
	}
}
