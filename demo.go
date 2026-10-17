package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

// demoSource is a fictional set of agents: the popup without herdr, Claude or
// any real work on screen, for trying it out and for screenshots.
type demoSource struct {
	mu      sync.Mutex
	started time.Time
	recaps  map[string]recap
	current map[string]bool
	titles  map[string]string // by pane: the titles agents were renamed to
	spaces  map[string]string // by workspace: the names spaces were renamed to
	delay   time.Duration     // how long a demo recap "takes"
	woken   map[string]bool   // by pane: snoozed agents woken in the demo
}

// errDemo: going to an agent, in the demo, where there's nowhere to go.
var errDemo = errors.New("the demo's agents aren't real, so there's nowhere to go")

type demoAgent struct {
	agent   agentInfo
	session string // "" for an agent that isn't Claude
	recap   string // "" for a session with nothing said yet
	age     time.Duration
	current bool
	later   string // the recap written when it's out of date
	said    string // something from its conversation that no recap mentions
	suggest string // a better title than the one it has
	meta    sessionMeta
	active  time.Duration // when it last did anything, from the demo's start
	changes gitChanges
}

var demoWorkspaces = []workspaceInfo{
	{WorkspaceID: "w1", Label: "storefront"},
	{WorkspaceID: "w2", Label: "billing"},
	{WorkspaceID: "w3", Label: "docs"},
}

var demoAgents = []demoAgent{
	{
		agent: agentInfo{PaneID: "w2:p1", Cwd: "/demo/w2-p1", WorkspaceID: "w2", Agent: "claude", Status: "blocked", Title: "Move billing webhooks to v2 events", StateChangeSeq: 4,
			Tokens: map[string]string{"pr": "#412", "pr_checks": "passing", "issue": "BIL-207", "issue_state": "In Progress"}},
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e01", age: 3 * time.Minute, current: true,
		meta: sessionMeta{Branch: "billing/webhooks-v2", Model: "claude-opus-5-5", Mode: "acceptEdits",
			LastPrompt: "run the migration against staging when the tests pass",
			Pending:    "Run npm run migrate -- --env staging", TasksDone: 4, TasksTotal: 5},
		changes: gitChanges{Files: 6, Added: 214, Deleted: 58, Ahead: 2},
		active:  -2 * time.Minute,
		recap:   "Moving the billing webhooks to v2 events: handlers and tests are done. Waiting on you to approve running the migration against staging.",
	},
	{
		// Snoozed till it was done, and just woke: herdr-snooze's tokens.
		agent: agentInfo{PaneID: "w1:p3", Cwd: "/demo/w1-p3", WorkspaceID: "w1", Agent: "claude", Status: "done", Title: "Flaky checkout e2e test", StateChangeSeq: 9,
			Tokens: map[string]string{snoozeWoke: "16:32 · it's done", snoozeWokeBadge: "⏰"}},
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e02", age: 12 * time.Minute, current: true,
		meta: sessionMeta{Branch: "fix/checkout-flake", Model: "claude-sonnet-5",
			LastPrompt: "the checkout e2e test fails about one run in five, find out why"},
		changes: gitChanges{Files: 1, Added: 12, Deleted: 3},
		active:  -12 * time.Minute,
		suggest: "WEB-512 Fix the Flaky Checkout Test",
		recap:   "Fixed the flaky checkout test: it raced the cart animation, now it waits for the total. Next, push the branch and open the PR.",
		said:    "The cart animation takes 300ms, so the test slept for 250 and usually lost. It now waits on the order total with Playwright's toHaveText, which retries until it's there, so there's no sleep left to tune.",
	},
	{
		agent:   agentInfo{PaneID: "w1:p1", Cwd: "/demo/w1-p1", WorkspaceID: "w1", Agent: "claude", Status: "working", Title: "Dark mode for the settings page", StateChangeSeq: 12},
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e03", age: 41 * time.Minute,
		meta: sessionMeta{Branch: "settings-dark-mode", Model: "claude-opus-5-5", Mode: "auto",
			LastPrompt: "dark mode for the whole settings page, match the design file", TasksDone: 3, TasksTotal: 7},
		changes: gitChanges{Files: 14, Added: 530, Deleted: 121},
		active:  0,
		recap:   "Adding dark mode to settings: colour tokens are in, the form fields are next.",
		later:   "Adding dark mode to settings: tokens and form fields are done, and it's now fixing contrast in the billing table. Next, screenshots for review.",
	},
	{
		agent: agentInfo{PaneID: "w1:p4", Cwd: "/demo/w1-p4", WorkspaceID: "w1", Agent: "claude", Status: "idle", Title: "search-pagination", StateChangeSeq: 5,
			Tokens: map[string]string{"prs": "#431 ✓ approved", "pr_state": "open", "pr_checks": "✓", "pr_review": "approved"}},
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e06", age: 3 * time.Hour, current: true,
		meta: sessionMeta{Branch: "search-pagination", Model: "claude-sonnet-5-5",
			LastPrompt: "open the PR and ask the search team for a review"},
		active:  -3 * time.Hour,
		suggest: "WEB-431 Paginate the Search Results",
		recap:   "Paginated the search results with a cursor instead of an offset, and opened the PR. Next, merge it once the search team has looked.",
	},
	{
		// Snoozed till the platform team is in: herdr-snooze's tokens.
		agent: agentInfo{PaneID: "w3:p2", Cwd: "/demo/w3-p2", WorkspaceID: "w3", Agent: "claude", Status: "idle", Title: "Release notes for 2.4", StateChangeSeq: 3,
			Tokens: map[string]string{snoozeMark: "1", snoozeBadge: "💤  Tue 09:00"}},
		changes: gitChanges{Ahead: 1},
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e04", age: 2 * time.Hour, current: true,
		meta: sessionMeta{Branch: "main", Model: "claude-haiku-4-5-20251001",
			LastPrompt: "draft release notes for 2.4 from the merged PRs"},
		active: -2 * time.Hour,
		recap:  "Drafted the 2.4 release notes from the merged PRs and grouped them by area. Next, check the upgrade section with the platform team.",
		said:   "I've put the one breaking change first in the upgrade section: the webhook signature header is renamed, and the old name stops working in 2.5, so there's a note on sending both until then.",
	},
	{
		agent:   agentInfo{PaneID: "w3:p4", Cwd: "/demo/w3-p4", WorkspaceID: "w3", Agent: "claude", Status: "idle", Title: "", Name: "scratch", StateChangeSeq: 1},
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e05",
	},
	{
		agent: agentInfo{PaneID: "w2:p3", Cwd: "/demo/w2-p3", WorkspaceID: "w2", Agent: "codex", Status: "working", Title: "Rate limiter for the public API", StateChangeSeq: 7,
			Tokens: map[string]string{"pr": "#415"}},
		// Codex recaps itself, as Claude does; its session says less about it.
		session: "0b3c9a51-5d7e-4f7a-9c35-1a2b3c4d5e07", age: 6 * time.Minute, current: true,
		recap: "Adding a rate limiter to the public API: the token bucket and its tests are in. Next, read each key's limits from config.",
	},
}

func newDemoSource() *demoSource {
	d := &demoSource{started: time.Now(), recaps: map[string]recap{}, current: map[string]bool{}, titles: map[string]string{}, spaces: map[string]string{}, delay: 1500 * time.Millisecond}
	for _, a := range demoAgents {
		if a.session != "" && a.recap != "" {
			d.recaps[a.session] = recap{Session: a.session, Text: a.recap, At: d.started.Add(-a.age)}
			if poorTitle(a.agent.Title, "") {
				r := d.recaps[a.session]
				r.Title = a.suggest
				d.recaps[a.session] = r
			}
			d.current[a.session] = a.current
		}
	}
	return d
}

func (d *demoSource) agents() ([]agentInfo, []workspaceInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	agents := make([]agentInfo, len(demoAgents))
	for i, a := range demoAgents {
		agents[i] = a.agent
		if t, ok := d.titles[a.agent.PaneID]; ok {
			agents[i].Title = t
		}
		if d.woken[a.agent.PaneID] {
			tokens := map[string]string{}
			for k, v := range a.agent.Tokens {
				if k != snoozeMark && k != snoozeBadge {
					tokens[k] = v
				}
			}
			agents[i].Tokens = tokens
		}
	}
	workspaces := make([]workspaceInfo, len(demoWorkspaces))
	for i, w := range demoWorkspaces {
		workspaces[i] = w
		if l, ok := d.spaces[w.WorkspaceID]; ok {
			workspaces[i].Label = l
		}
	}
	return agents, workspaces, nil
}

func (d *demoSource) find(paneID string) *demoAgent {
	for i := range demoAgents {
		if demoAgents[i].agent.PaneID == paneID {
			return &demoAgents[i]
		}
	}
	return nil
}

func (d *demoSource) session(_ context.Context, info agentInfo) (claudeSession, error) {
	a := d.find(info.PaneID)
	if a == nil || a.session == "" {
		return claudeSession{}, errNotClaude
	}
	s := claudeSession{ID: a.session, Cwd: "/demo", Meta: a.meta}
	if a.agent.Agent == "codex" {
		s.Agent, s.Native = "codex", true
	}
	if s.Meta.Model != "" {
		s.Meta.Active = d.started.Add(a.active)
		s.Meta.PendingAt = s.Meta.Active
		// The working one has been at it since a while before its last message.
		s.Meta.PromptAt = s.Meta.Active.Add(-18 * time.Minute)
	}
	if a.recap != "" {
		s.Transcript = "/demo/" + a.session + ".jsonl"
	}
	return s, nil
}

func (d *demoSource) cached(s claudeSession) (*recap, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.recaps[s.ID]
	if !ok {
		return nil, false
	}
	return &r, d.current[s.ID]
}

func (d *demoSource) recap(ctx context.Context, s claudeSession, _ string, force bool) (recap, error) {
	select {
	case <-ctx.Done():
		return recap{}, ctx.Err()
	case <-time.After(d.delay):
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	text := d.recaps[s.ID].Text
	for _, a := range demoAgents {
		if a.session == s.ID && a.later != "" {
			text = a.later
		}
	}
	r := recap{Session: s.ID, Text: text, At: time.Now(), Title: d.recaps[s.ID].Title, Renaming: d.recaps[s.ID].Renaming, Settled: d.recaps[s.ID].Settled}
	d.recaps[s.ID], d.current[s.ID] = r, true
	return r, nil
}

func (d *demoSource) focus(string) error { return errDemo }

// changes are the demo's made-up worktrees, by the agent in them.
func (d *demoSource) changes(dir string) gitChanges {
	for _, a := range demoAgents {
		if a.agent.Cwd == dir {
			return a.changes
		}
	}
	return gitChanges{}
}

func (d *demoSource) prompt(string, string) error { return nil }

// search goes by what the demo's agents are made up to have said.
func (d *demoSource) search(_ context.Context, s claudeSession, words []string) string {
	f := newFinder(words)
	for _, a := range demoAgents {
		if a.session == s.ID {
			f.see(a.said)
		}
	}
	return f.snippet()
}

// The demo has no Memex: what's found is listed in the usual order.
func (d *demoSource) rank(context.Context, string, []string) map[string]float64 { return nil }

// The demo has herdr-github, herdr-linear and herdr-snooze, so its pull
// requests, issues and snoozing look as they would.
func (d *demoSource) plugins() map[string]popupRef {
	return map[string]popupRef{
		githubPlugin: {PluginID: githubPlugin, Entrypoint: "picker"},
		linearPlugin: {PluginID: linearPlugin, Entrypoint: "picker"},
		snoozePlugin: {PluginID: snoozePlugin, Entrypoint: "picker"},
	}
}

func (d *demoSource) open(popupRef) error { return errDemo }

// wake takes the snooze off a demo agent: what herdr-snooze would do.
func (d *demoSource) wake(paneID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.woken == nil {
		d.woken = map[string]bool{}
	}
	d.woken[paneID] = true
	return nil
}

func (d *demoSource) rename(_ string, s claudeSession) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.recaps[s.ID]
	r.Renaming = true
	d.recaps[s.ID] = r
	return nil
}

// give takes the rename as Claude would: the agent has the new title.
func (d *demoSource) give(paneID string, s claudeSession) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r := d.recaps[s.ID]; r.Renaming {
		d.titles[paneID] = r.Title
		r.Renaming, r.Settled = false, true
		d.recaps[s.ID] = r
	}
	return nil
}

func (d *demoSource) renameSpace(workspaceID, label string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.spaces[workspaceID] = label
	return nil
}

// suggest is the demo's better title for the agent, or the one it has.
func (d *demoSource) suggest(ctx context.Context, s claudeSession, title string) (recap, error) {
	select {
	case <-ctx.Done():
		return recap{}, ctx.Err()
	case <-time.After(d.delay):
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.recaps[s.ID]
	r.Session, r.Title, r.Renaming, r.Settled = s.ID, title, false, false
	for _, a := range demoAgents {
		if a.session == s.ID && a.suggest != "" {
			r.Title = a.suggest
		}
	}
	d.recaps[s.ID] = r
	return r, nil
}

func (d *demoSource) settled(s claudeSession) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.recaps[s.ID]
	r.Renaming, r.Settled = false, true
	d.recaps[s.ID] = r
	return nil
}

// capable: the demo's recaps are its Claude and Codex agents', as the
// plugin's are unless config.json says otherwise.
func (d *demoSource) capable(a agentInfo) bool { return a.Agent == "claude" || a.Agent == "codex" }
