package main

import (
	"context"
)

// source is where the popup gets its agents and recaps: herdr and Claude, or
// the demo's fictional ones.
type source interface {
	// agents is every agent herdr knows, with its workspaces' names in
	// sidebar order.
	agents() ([]agentInfo, []workspaceInfo, error)
	// capable: the agent's conversation can be recapped.
	capable(a agentInfo) bool
	// session finds the coding agent session in a pane.
	session(context.Context, agentInfo) (claudeSession, error)
	// cached is the session's cached recap, if any, and whether it still
	// describes the conversation.
	cached(s claudeSession) (*recap, bool)
	// recap writes a current recap, or with force a new one regardless. A
	// poor title (the agent's, as herdr shows it) gets a better one suggested.
	recap(ctx context.Context, s claudeSession, title string, force bool) (recap, error)
	focus(paneID string) error
	// changes is git's view of a worktree: uncommitted and unpushed work.
	changes(dir string) gitChanges
	// prompt sends text to an agent, as if typed and entered.
	prompt(paneID, text string) error
	// suggest asks for a title now, whatever the agent's own is like: the
	// session's recap comes back with it.
	suggest(ctx context.Context, s claudeSession, title string) (recap, error)
	// rename notes that the session is to be given its suggested title, and
	// give types it into the agent, if it's still to be: once it's between turns.
	rename(paneID string, s claudeSession) error
	give(paneID string, s claudeSession) error
	// settled notes that the session's suggested title was taken or turned
	// down: it isn't offered again.
	settled(s claudeSession) error
	// search is where the session's conversation says every one of words:
	// some of what was said, or "" when it doesn't.
	search(ctx context.Context, s claudeSession, words []string) string
	// rank is how well each of the sessions (by ID) matches the filter, the
	// higher the better: empty when there's nothing to say so (no Memex).
	rank(ctx context.Context, filter string, ids []string) map[string]float64
	// plugins are the other plugins a row can link to, and open closes this
	// popup for one of theirs.
	plugins() map[string]popupRef
	open(popupRef) error
}

type liveSource struct{ cfg config }

func (liveSource) agents() ([]agentInfo, []workspaceInfo, error) {
	agents, err := listAgents()
	if err != nil {
		return nil, nil, err
	}
	return agents, listWorkspaces(), nil
}

func (l liveSource) session(ctx context.Context, a agentInfo) (claudeSession, error) {
	ctx, cancel := recapContext(ctx, l.cfg)
	defer cancel()
	s, err := resolveAgentSession(ctx, l.cfg, a)
	if err == nil && claudes(s) {
		s.Meta = readMeta(s.Transcript)
	}
	return s, err
}

func (liveSource) cached(s claudeSession) (*recap, bool) {
	r := loadRecap(s.ID)
	return r, fresh(r, s)
}

func (l liveSource) recap(ctx context.Context, s claudeSession, title string, force bool) (recap, error) {
	r, _, err := ensureRecap(ctx, l.cfg, s, title, force)
	return r, err
}

func (liveSource) focus(paneID string) error { return focusAgent(paneID) }

func (liveSource) changes(dir string) gitChanges { return readChanges(dir) }

func (liveSource) prompt(paneID, text string) error {
	return promptAgent(paneID, text)
}

func (liveSource) settled(s claudeSession) error { return settleTitle(s.ID) }

func (l liveSource) suggest(ctx context.Context, s claudeSession, title string) (recap, error) {
	return ensureTitle(ctx, l.cfg, s, title)
}

func (liveSource) rename(paneID string, s claudeSession) error { return askRename(paneID, s.ID) }

func (liveSource) give(paneID string, s claudeSession) error { return giveTitle(paneID, s.ID) }

func (l liveSource) capable(a agentInfo) bool { return recapCapable(l.cfg, a) }

func (l liveSource) search(ctx context.Context, s claudeSession, words []string) string {
	return searchSession(ctx, l.cfg, s, words)
}

func (l liveSource) rank(ctx context.Context, filter string, ids []string) map[string]float64 {
	return rankSessions(ctx, l.cfg, filter, ids)
}

func (liveSource) plugins() map[string]popupRef { return siblings() }

func (liveSource) open(p popupRef) error { return handOver(p) }
