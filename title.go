package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A better title for an agent whose own is poor, asked for alongside its
// recap: the same fork of the conversation, so it knows the work and the
// ticket it's for. It's only suggested: the popup shows it under the title,
// and renames the agent (Claude's /rename) when you say so, or drops it.

// ticketPattern is a ticket's key in a branch name: act-1921 in
// mrolafsson-act-1921-users-get-no-email.
var ticketPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([a-z][a-z0-9]{1,9})-([1-9][0-9]{0,5})(?:[^a-z0-9]|$)`)

// notTickets are words a number follows in a branch name without making a
// ticket of it: phase-2, release-3.
var notTickets = map[string]bool{
	"phase": true, "step": true, "part": true, "pr": true, "v": true, "version": true, "release": true,
	"issue": true, "fix": true, "bug": true, "test": true, "try": true, "attempt": true, "wave": true,
	"stage": true, "sprint": true, "week": true, "day": true, "round": true, "batch": true, "task": true,
	"milestone": true, "iteration": true, "option": true, "plan": true, "draft": true, "rev": true,
}

// ticketKey is the ticket a branch is for, as its tracker writes it
// (ACT-1921), or "".
func ticketKey(branch string) string {
	for rest := branch; rest != ""; {
		m := ticketPattern.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		if team := rest[m[2]:m[3]]; !notTickets[strings.ToLower(team)] {
			return strings.ToUpper(team) + "-" + rest[m[4]:m[5]]
		}
		rest = rest[m[5]:]
	}
	return ""
}

// poorTitle: a title worth replacing. None at all (Claude Code's own name
// for a session it hasn't titled), a slug (what /rename makes up:
// fix-channelless-assignment-notify), or one that doesn't say the ticket its
// branch is for.
func poorTitle(title, branch string) bool {
	t := strings.TrimSpace(title)
	if t == "" || strings.EqualFold(t, "claude code") {
		return true
	}
	if !strings.Contains(t, " ") && strings.ContainsAny(t, "-_") {
		return true
	}
	key := ticketKey(branch)
	return key != "" && !strings.Contains(strings.ToUpper(t), key)
}

// maxTitleWidth is as long as a suggested title may be: it has to fit a
// terminal title and a sidebar.
const maxTitleWidth = 50

func titlePrompt(title, branch string) string {
	current := "It has no name yet."
	if t := strings.TrimSpace(title); t != "" && !strings.EqualFold(t, "claude code") {
		current = fmt.Sprintf("Its current name is %q.", t)
	}
	on := ""
	if branch != "" {
		on = fmt.Sprintf(" The git branch is %q: a ticket's key may be in it, or in the conversation.", branch)
	}
	return "Suggest a name for this session, for a list of sessions. " + current +
		" Answer with the name alone on one line: no quotes, no explanation, and don't use any tools." +
		" At most six words and " + fmt.Sprint(maxTitleWidth) + " characters, in Title Case, saying what the work is as a whole rather than its latest step." +
		" If the work is for a ticket, start with the ticket's key in capitals, like ACT-1921." + on +
		" If the current name is already right, answer with it unchanged."
}

// titleArgs asks the session for a title, in a fork that is never saved, as
// a recap is.
func titleArgs(id, prompt string) []string {
	return []string{"-p", "--resume", id, "--fork-session", "--no-session-persistence", "--output-format", "json", prompt}
}

// suggestTitle asks for a better title than the one the agent has. An
// answer that isn't a title (several lines, a sentence, too long) is an error.
func suggestTitle(ctx context.Context, cfg config, s claudeSession, title string) (string, float64, error) {
	prompt := titlePrompt(title, gitBranch(s.Cwd))
	if s.Native {
		// Codex, asked the same in a fork of its own.
		out, err := runCodex(ctx, cfg, s, prompt)
		if err != nil {
			return "", 0, fmt.Errorf("codex: %w", err)
		}
		t, err := cleanTitle(clean(out, true))
		return t, 0, err
	}
	out, err := runCommand(ctx, cfg, s, titleArgs(s.ID, prompt))
	if err != nil {
		return "", 0, err
	}
	return parseTitle(out)
}

// titleable: the session's agent can be asked for a title and given one:
// Claude's and Codex's, each forked for the asking and with a /rename.
func titleable(s claudeSession) bool { return claudes(s) || s.Native }

func parseTitle(out []byte) (string, float64, error) {
	text, cost, isError, err := claudeResult(out, true)
	switch {
	case err != nil:
		return "", 0, err
	case isError:
		return "", cost, fmt.Errorf("claude: %s", shorten(text, 200))
	}
	t, err := cleanTitle(text)
	return t, cost, err
}

// cleanTitle is an answer as a title, or an error when it isn't one: several
// lines, a sentence, too long.
func cleanTitle(text string) (string, error) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "\n") {
		return "", errors.New("the answer was more than a title")
	}
	// It's typed into the agent as /rename's argument: plain words only.
	t := strings.Join(strings.Fields(strings.Trim(text, "\"'`*_ ")), " ")
	t = strings.TrimRight(t, ".")
	switch {
	case t == "":
		return "", errors.New("the answer was an empty title")
	case strings.HasPrefix(t, "/"), ansi.StringWidth(t) > maxTitleWidth+10, len(strings.Fields(t)) > 9:
		return "", errors.New("the answer was more than a title")
	}
	return t, nil
}

// quietSince: nothing has been said in the transcript past offset from.
// Claude also writes what isn't conversation there (a rename, a title, a
// mode), and a recap is as good after those as before.
func quietSince(path string, from int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() <= from || fi.Size()-from > metaTail {
		return false
	}
	if _, err := f.Seek(from, 0); err != nil {
		return false
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), metaTail)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r struct {
			Type   string `json:"type"`
			IsMeta bool   `json:"isMeta"`
		}
		if json.Unmarshal(line, &r) != nil || r.Type == "" || r.Type == "assistant" || (r.Type == "user" && !r.IsMeta) {
			return false
		}
	}
	return sc.Err() == nil
}

// ensureTitle asks for a title now, whatever the agent's own is like (the
// popup's ^r), and keeps it with the session's recap as a new suggestion.
func ensureTitle(ctx context.Context, cfg config, s claudeSession, title string) (recap, error) {
	if !titleable(s) {
		return recap{}, errors.New("titles are suggested for Claude and Codex agents")
	}
	if s.Transcript == "" {
		return recap{}, errNothingYet
	}
	// Before the lock: a recap being written needn't wait for it, or it for one.
	t, cost, err := suggestTitle(ctx, cfg, s, title)
	if err != nil {
		return recap{}, err
	}
	unlock, err := lockSession(ctx, s.ID)
	if err != nil {
		return recap{}, err
	}
	defer unlock()
	r := loadRecap(s.ID)
	if r == nil {
		// No recap yet: the title is kept for the one to come.
		r = &recap{Session: s.ID}
	}
	r.Title, r.Renaming, r.Settled, r.CostUSD = t, false, false, r.CostUSD+cost
	return *r, saveRecap(*r)
}

// A rename waits for its agent to be between turns, which may be after the
// popup that asked for it has closed. So it's kept with the session's recap
// (Renaming), and whoever next finds the agent free types it in: the popup,
// or tick on the status change. A mark by pane tells tick there's one to look
// for, without it reading every session on every change.

func renamingPath(paneID string) string {
	return filepath.Join(stateDir(), "renaming", notAlnum.ReplaceAllString(paneID, "_"))
}

// renamePending: a rename may be waiting for this pane's agent.
func renamePending(paneID string) bool {
	_, err := os.Stat(renamingPath(paneID))
	return err == nil
}

// askRename notes that the session is to be given its suggested title.
func askRename(paneID, sessionID string) error {
	unlock, err := lockSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	defer unlock()
	r := loadRecap(sessionID)
	if r == nil || r.Title == "" {
		return errors.New("no title is suggested for it")
	}
	r.Renaming = true
	if err := saveRecap(*r); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(renamingPath(paneID)), 0o700); err != nil {
		return err
	}
	return os.WriteFile(renamingPath(paneID), []byte(sessionID), 0o600)
}

// promptAgent types text into an agent and enters it; a variable so tests
// can stand one in.
var promptAgent = func(paneID, text string) error {
	return herdrCall("agent.prompt", map[string]any{"target": paneID, "text": text}, nil)
}

// giveTitle types the session's pending rename into its agent (Claude's
// /rename), once: with none pending it does nothing. The caller has found
// the agent between turns.
func giveTitle(paneID, sessionID string) error {
	unlock, err := lockSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	defer unlock()
	r := loadRecap(sessionID)
	if r == nil || !r.Renaming || r.Title == "" {
		os.Remove(renamingPath(paneID)) // another conversation's, or done already
		return nil
	}
	if err := promptAgent(paneID, "/rename "+r.Title); err != nil {
		return err
	}
	os.Remove(renamingPath(paneID))
	r.Renaming, r.Settled = false, true
	return saveRecap(*r)
}

// settleTitle notes that the session's suggested title was taken or turned
// down, so it isn't offered again.
func settleTitle(sessionID string) error {
	unlock, err := lockSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	defer unlock()
	r := loadRecap(sessionID)
	if r == nil {
		return nil
	}
	r.Renaming, r.Settled = false, true
	return saveRecap(*r)
}
