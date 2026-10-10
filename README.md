# herdr-recap

Where every agent got to, in one [herdr](https://herdr.dev) popup: what
needs you, what's done, what each one was doing, and a line to answer it
from without leaving the list.

![The popup on the demo's agents, grouped by stage under tabs: one that needs you, one finished, two working, one in review, two in progress](docs/images/agents.png)

## Why

One agent is a conversation. Five are a job. Each works in its own pane, on
its own branch, at its own pace, and the state of each lives only in that
pane's scrollback. While you're in one, the others carry on without you: one
finishes, one stops to ask permission for a command, one goes down a path
you'd have stopped.

Then you look away for a meeting or lunch, and come back to a sidebar that
says *done*, *done*, *working*, *needs you*. It tells you **that** each agent
moved, not **what** it did, what it's asking, or what's next. So you rebuild
it one pane at a time: open it, scroll up past the tool output, piece
together what you'd asked for and what it made of it, and move on to the
next. Meanwhile the agent that needed a yes has been waiting the whole time,
behind the ones that didn't.

Keeping all of that in your head is the hard part of running several agents
at once. herdr-recap keeps it for you: for every agent, a one-line recap of
where it got to, written while you were away, with the details that decide
what to look at first.

## Scan the list

Open it (`prefix+alt+r`) and read down. Each agent is a few lines:

- **Status first, what needs you at the top.** herdr's own glyphs and colours:
  `◉` needs you, `◔` working, `●` done, `✓` idle. Within a stage, whatever
  has waited longest comes first, and the right side says for how long:
  *waiting 2m*, *done 12m*, *working 18m*.
- **Grouped by where the work has got to.** Each group has a heading and a
  count, in this order:

  | Stage | Which agents |
  | --- | --- |
  | **Needs you** | stopped on a question or an approval |
  | **Finished** | finished a turn you haven't looked at |
  | **Needs fixing** | idle, its PR failing its checks, in conflict or with changes requested |
  | **Working** | at work |
  | **In review** | idle, its PR open with nothing wrong with it |
  | **In progress** | idle, with no PR out for review (none yet, or a draft) |
  | **Merged** | idle, its PR merged or closed and nothing new in its worktree |

  What an agent is doing right now wins over its PR: start one on the next
  thing after a merge and it's *Working*, and between turns *In progress*
  once it has edited or committed something, or moved to a new branch. The
  PR stages need [herdr-github](https://github.com/mrolafsson/herdr-github)'s
  tokens; without it, idle agents are all *In progress*.
- **The stages are tabs along the top.** *All*, then each stage that has
  agents, in its colour with its count. `←` / `→` or a click shows one
  stage's agents alone; `esc` goes back to *All*. A stage's name filters
  too: type `merged` for the agents you could close.
- **What it's waiting for.** An agent stopped on a question or an approval
  shows it in red: the question, or the command or edit it wants to run.
- **Where it got to.** Claude Code's own recap of the session: the goal, the
  current step, the next one. The same summary Claude shows when you come
  back to a session, for every session at once.
- **The state of its work.** Its branch; uncommitted and unpushed changes
  (*6 files +214 −58 ↑2*); its task list (*4/7 tasks*); and whatever the
  pane's other plugins report, such as its PR from herdr-github.
- **The selected agent's** last prompt, model, and permission mode (when
  it isn't the default) are on the line above the footer, so moving down the
  list doesn't reflow it.

`enter` (or a click) goes to the agent. Recaps are written ahead of time, so
the list is complete the moment it opens.

**Type to filter.** Every word must match somewhere in the row: its title,
status, space, branch, recap, what it's waiting for or a token. So `billing`
finds the agents on billing, `needs you` the ones stopped on a question, and
`dark working` the one still working on dark mode.

## Reply from the list

Most of what an agent needs from you is short: *yes, go ahead*, *push it*,
*try the other approach*. Press `tab` on it, type, and `enter` sends it, as if
you'd typed it in its pane. The popup stays open, so you can go down the list
answering each in turn.

![Replying to an agent from the list](docs/images/reply.png)

Replies can run to several lines (`alt+enter` or `ctrl+j` for a new one, or
paste). An agent stopped on a question or an approval can't take a typed
reply, so for those the popup says so, and `enter` takes you to answer it
there.

**Contents:** [Requirements](#requirements) · [Install](#install) ·
[Keys](#keys) · [When recaps are written](#when-recaps-are-written) ·
[What it costs](#what-it-costs) · [Configuration](#configuration) ·
[Privacy](#privacy) · [Troubleshooting](#troubleshooting) ·
[How it works](#how-it-works) · [Development](#development)

## Requirements

- **herdr 0.9.0** or later.
- **Claude Code** with `/recap` (2.1.x), signed in. Recaps and the details
  from the conversation are Claude-only; other agents are listed with their
  status, title, branch and changes.
- **Linux** or **macOS**, on arm64 or x86-64.

## Install

```sh
herdr plugin install mrolafsson/herdr-recap
```

The build step compiles it with Go if you have it, or downloads the release
binary for your platform and checks its SHA-256.

Bind it to a key in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+alt+r"
type = "plugin_action"
command = "herdr-recap.open"
```

On macOS, `alt` needs Option to send Alt in your terminal (Ghostty:
`macos-option-as-alt = true`).

Then `herdr server reload-config`, and **start a new herdr client** (detach
and run `herdr` again): a client that was already open keeps the key
bindings it started with.

`Recap: demo (fictional agents)` in the command palette shows it on made-up
agents: no Claude calls, safe to screenshot.

## Keys

| Key | Does |
| --- | --- |
| typing | filter |
| `↑` `↓`, `ctrl+p` `ctrl+n` | move |
| `pgup` / `pgdn`, `home` / `end` | page, first / last |
| `enter`, click | go to that agent and close |
| `tab` | reply to that agent: type, then `enter` to send, `esc` to drop it; `alt+enter` or `ctrl+j` for a new line |
| `ctrl+r` | write that agent's recap again, now |
| `←` / `→` | the stage before / after, round the ends; a click on a tab shows it |
| `esc` | clear the filter, then back to *All*, then close |

The keys along the bottom are pills, and buttons: the one under the pointer
underlines, and a click presses it.

A recap written before the agent carried on says how old it is (*recap from
20m ago*) and is drawn dimmer until it's rewritten, which opening the popup
does straight away.

## When recaps are written

Soon after a turn ends. When a Claude agent finishes (done) or stops to ask
you something (blocked), herdr tells the plugin, which waits
`recap_after_seconds` (3 minutes). If by then you still haven't looked at the
agent, its recap is written; if you have (done turns to idle) or you answered
it, nothing is spent. So the agents you're working with cost nothing, and the
ones you walked away from are recapped by the time you're back.

Opening the popup also rewrites any recap that no longer matches its
conversation, for example an agent that has carried on working, a few at a
time, in the background. The rows show what's already there meanwhile.

## What it costs

A recap is one short Claude request on the agent's own conversation, billed
like any other (or counted against your plan). Written soon after the turn,
it reuses the agent's prompt cache: in testing, about 2¢ at list price on a
~70k-token conversation, and a few cents on bigger ones. Once the cache has
expired (5 minutes by default, an hour on some plans) the whole conversation
is read again, which costs several times more. That's why the wait is 3
minutes: keep `recap_after_seconds` under your cache's lifetime.

At most one recap per agent each time you walk away from it: an agent
sitting in done starts no new turns.

Everything else on a row (the waiting question, tasks, branch, changes) is
read from files and git on your machine, and costs nothing.

## Configuration

Optional: `config.json` in the plugin's config directory
(`~/.config/herdr/plugins/config/herdr-recap/`). See
[config.example.json](config.example.json).

| Key | Default | Meaning |
| --- | --- | --- |
| `recap_after_seconds` | `180` | How long an agent waits on you, unlooked at, before its recap is written |
| `claude` | the agent's own | The Claude Code binary to run recaps with. By default the one the agent runs, found on the agent's PATH (herdr's own PATH often lacks `~/.local/bin`) |
| `timeout_seconds` | `120` | The longest one recap may take |
| `theme` | ask the terminal | `"dark"` or `"light"` background |
| `tokens` | all | Which pane tokens to show, in order, e.g. `["pr"]`. By default all, with a PR from herdr-github said once: its `prs` (state, checks and the agent's other PRs), or else its `pr`, without the `pr_*` details |

### Colours

Colours come from herdr's theme: the title in its brightest text, then the
recap, then the details, each kind in its own colour.

The herdr plugins (herdr-github, herdr-linear, herdr-recap) draw the same
thing the same way, from one shared file (`look.go`):

| what                 | how it's drawn |
|----------------------|----------------|
| a title              | the theme's brightest text; bold where it heads a card or a screen |
| prose                | a step quieter |
| where, when, who     | dim |
| a branch             | mauve |
| a pull request       | its state's colour: draft peach (work in progress), open green, merged mauve, closed red |
| an agent             | its state's mark and colour, as in herdr's sidebar: `◉` needs you, `◔` working, `●` done, `✓` idle |
| counts               | yellow; added green, removed red; tasks teal |
| keys                 | pills along the bottom, coloured by what they do: green goes somewhere, peach changes something, blue changes what you see, grey leaves |
| a screen's subject   | wears its state as a pill (`Open`, `Draft`, `In Review`) |

**The `terminal` theme.** herdr's `terminal` theme draws with your terminal's
16 colours, and gives several of these the same one (branches and dim text
the body's grey). The plugins spread them over the terminal's other colours
(branches on magenta, dim on bright black), so the list reads the same as on
a named theme. A colour you set yourself in `[theme.custom]` wins.

**A narrow popup.** When the keys don't fit, their labels shorten first (`^o
open in Linear` becomes `^o open`), then the keys you'd miss least are left
out. A key that isn't shown still works.

A pull request's badge from herdr-github (`#482 ✓ approved`, `#476 draft ●`)
is coloured piece by piece as that plugin colours a PR: the number and its
state in the state's colour, the checks and the review in theirs.

## Privacy

Recaps are made by the same Claude Code, account and settings the agent uses,
on its own conversation, so nothing goes anywhere it hadn't already. They're
kept in `~/.local/state/herdr/plugins/herdr-recap/recaps/`, one small file
per session, readable only by you.

The recap is a fork of the conversation that is never saved: the agent's own
session isn't written to, and it doesn't show up in `claude --resume`. It
does run with your Claude Code settings, so your own hooks (a notification on
stop, say) run for it too.

## Troubleshooting

- **`herdr-recap list`** shows, without drawing the popup, each agent, the
  Claude session it was found to be running, everything read from it
  (branch, tasks, the pending request, changes) and its cached recap.
- **`herdr-recap recap w1:p2`** writes one pane's recap now and prints it,
  with any error in full.
- **`recap.log`** in the state directory has every recap written in the
  background, with its cost, and any errors.
- *The key does nothing*: herdr clients read key bindings when they start, and
  `reload-config` doesn't reach one that's already open. Start a new client.
- *It opens somewhere else*: herdr shows one popup at a time, on whichever
  client counts as active, so with several attached (another terminal, a
  mosh session) it can open on one you're not looking at. Opening it again
  closes that one and opens it where you are. If another plugin's popup is
  in the way, a toast says so: close it first.
- *It lists only some of my agents*: it shows the herdr it's installed in.
  Agents on another machine you've connected in herdr's sidebar need the
  plugin installed there, and appear in that machine's popup.
- *Couldn't find its conversation*: the plugin reads Claude's process to find
  its session (its `CLAUDE_CONFIG_DIR` and `sessions/<pid>.json`). A Claude
  Code too old to write that file, or one running as another user, can't be
  read.

## How it works

The popup reads herdr's agent list every second over its socket, so statuses
stay live. For each Claude agent it finds the session through the pane's
`claude` process rather than herdr's reported session ID, which under a
profile switcher (clauth and the like) can name a different session: the
process's `CLAUDE_CONFIG_DIR` (from `/proc` on Linux, `ps` on macOS) leads
to `sessions/<pid>.json`, which names the session and where it was started.

The rest of a row comes from the end of the session's transcript: the last
tool call without a result (what a blocked agent is asking), its task list,
branch, model, mode, your last prompt, and when each happened. Changes come
from `git status` and `git diff --shortstat` in the agent's folder, read
again only when its status changes.

A recap runs

```sh
claude -p --resume <session> --fork-session --no-session-persistence --output-format json /recap
```

in the session's directory with the agent's `CLAUDE_CONFIG_DIR` and without
herdr's environment (so herdr's own Claude hooks don't mistake it for an
agent). It's cached with the transcript's size and time, and counts as
current until the transcript changes. One recap per session runs at a time:
another asking for the same one waits and takes its result.

The `pane.agent_status_changed` hook (`tick`) only reads the agent list and
starts a detached `recap --after` for an agent now waiting on you, once per
status change. That process sleeps, checks the agent's status hasn't moved
since, and writes the recap.

A reply is herdr's `agent.prompt`: the text is pasted into the agent's pane
and entered, as one prompt.

## Development

```sh
go test -race ./...
go run . picker --demo       # the popup on fictional agents, in any terminal
herdr plugin link .          # try it in herdr
sh scripts/screenshots.sh    # docs/images from the demo (needs freeze)
```

`themes_herdr.go` is generated from herdr's palettes by
`scripts/gen-themes.py` (see the script for how).
