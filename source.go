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
	// session finds the Claude session in a pane.
	session(paneID string) (claudeSession, error)
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
}

type liveSource struct{ cfg config }

func (liveSource) agents() ([]agentInfo, []workspaceInfo, error) {
	agents, err := listAgents()
	if err != nil {
		return nil, nil, err
	}
	return agents, listWorkspaces(), nil
}

func (liveSource) session(paneID string) (claudeSession, error) {
	s, err := resolveSession(paneID)
	if err == nil {
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
