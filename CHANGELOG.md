# Changelog

## 0.6.0 - 2026-10-11

Recaps for other agents. `agents` in `config.json` lists the ones recapped,
`["claude", "codex"]` unless you say otherwise.

- **Codex agents are recapped, with nothing to set up.** If you run Codex
  in herdr, its agents now get recaps as Claude's do, each one a short
  request against your Codex plan or key. `"agents": ["claude"]` turns that
  off.
- **Codex recaps itself**, as Claude does: a fork of the pane's session that
  is never saved (`codex exec fork --ephemeral`, read-only), so the
  conversation goes nowhere it hadn't been. The session is found from the
  pane's Codex process; herdr's Codex integration isn't needed.
- **OpenCode and Hermes, through Memex, when you add them to `agents`**, by
  [@saforem2](https://github.com/saforem2) (#1): Memex reads the end of the
  session, and the user and assistant text is summarized by Claude Code
  (`haiku` by default, or your own `summarizer` command). Sessions are
  matched by the exact ID herdr reports, and the transcript is passed as
  data, with no tools, so it can't instruct the summarizer.
- **Better titles for Codex agents too**: asked for in a fork of its own,
  and given with Codex's `/rename`. Not for agents read through Memex.
## 0.5.2 - 2026-10-11

- **Pull requests and Linear issues open in their plugins.** With
  herdr-github installed, an agent's pull request is underlined: a click on
  it, or `ctrl+g`, opens it in the GitHub popup. With herdr-linear, the same
  for its Linear issue (`ctrl+l`), which is now shown as one piece with its
  status (`ENG-142 In Progress`). `esc` there comes back to Recap, on the
  agent you left. The plugins are found by themselves; without them nothing
  changes.

## 0.5.1 - 2026-10-11

- **A recap runs the width of the popup**, where it stopped at 80 columns:
  on a wide terminal it's a line or two, and more agents fit. It stops short
  of the right edge by a margin that grows with the popup, 3 cells on a
  narrow one and up to 8 on a wide one.

## 0.5.0 - 2026-10-10

- **Better titles, suggested with the recap.** An agent with no title, a slug
  for one (`fix-channelless-assignment-notify`) or one that leaves out its
  branch's ticket has a better one on the line under it, such as *ACT-1921
  Activation Email for New Users*, followed by *Rename · Ignore*. *Rename*
  (a click, or `ctrl+r`) types `/rename` into the agent, once it's between
  turns, and the row says *Renaming to …* until its title shows it. *Ignore*
  (a click, or `ctrl+x`) drops the suggestion for good. It's asked for once
  per session, at the same time as the recap.
- **`ctrl+r` renames; it no longer writes the recap again.** Out-of-date
  recaps are rewritten on opening the popup as before. With no title
  suggested, `ctrl+r` asks for one.
- A rename asked of an agent that's working or waiting on you is kept, and
  given when it's next between turns, with the popup open or not.
- **A rename doesn't put a recap out of date.** Nor does anything else Claude
  writes to a transcript that isn't conversation: only an agent's message or
  one of yours does.

## 0.4.2 - 2026-10-10

- **Idle is said only of the Idle stage.** An idle agent whose PR puts it in
  *In review*, *Needs fixing* or *Merged* says *for 3h* on the right, not
  *idle 3h*, and typing `idle` lists the Idle stage alone.

## 0.4.1 - 2026-10-10

- **Two stages renamed.** *Finished* is *Done*, herdr's own word for it and
  the one on the row; *In progress* is *Idle*: nothing is in progress there,
  it's waiting for you to set it going.
- **The stages are in the order of whose move it is.** *Needs you*, *Done*,
  *Needs fixing* and *Idle* are yours; then *Working*, *In review* and
  *Merged*. *Idle* was after *In review*.

## 0.4.0 - 2026-10-10

- **The list is grouped by stage.** Under a heading with a count each: *Needs
  you*, *Finished*, *Needs fixing*, *Working*, *In review*, *In progress*,
  *Merged*. The first, second and fourth are the agent's status, as before;
  an idle agent is placed by its pull request (herdr-github's tokens), so the
  one whose PR is failing is above the ones waiting on a review, and the
  merged ones are at the bottom. An agent started on something new after a
  merge is back in *Working*, or *In progress* between turns. A stage's name
  filters the list.
- **The stages are tabs along the top**, each in its colour with its count,
  in place of the counts by status. `←` / `→` or a click shows one stage
  alone; `esc` goes back to *All* before it closes.

## 0.3.0 - 2026-10-06

- **One look, shared with herdr-github and herdr-linear.** A title, a
  branch, a pull request, an agent and a key are drawn the same in all three
  (see *Colours* in the README). Here: a PR's badge is coloured as
  herdr-github colours a PR (draft peach, open green, merged mauve, closed
  red, with its checks and review in their own colours) instead of all
  green, and the count that needs you is a pill.
- **Keys are pills.** The keys along the bottom are solid chips, coloured by
  what they do; the one under the pointer underlines. Too many for the
  popup's width, and labels shorten, then the least missed keys are left out.
- **The `terminal` theme keeps its hierarchy.** Branches and the dim details
  on the right no longer share the recap's grey.

## 0.2.1 - 2026-10-06

- A PR from herdr-github is on a row once (*#148 merged*), not twice
  (*#148 · #148 merged*): its `prs` token stands for `pr` as well as the
  `pr_*` details.

## 0.2.0 - 2026-10-06

- Type to filter the list, as in herdr-github and herdr-linear: every word
  must match somewhere in an agent's row (its title, status, space, branch,
  recap, what it's waiting for or a token). `esc` clears the filter, then
  closes.
- With letters going to the filter, reply is `tab` (was `r`), and `k` `j`,
  `g` `G` and `q` no longer move or close: use the arrows or `ctrl+p`
  `ctrl+n`, `home` `end`, and `esc`.

## 0.1.2 — 2026-09-28

- Rows keep their height when selected: the selected agent's last prompt,
  model and mode are on the line above the footer instead of in its row, so
  moving down the list no longer shifts everything under it.
- The recap comes right under the title (after what a blocked agent is
  asking), with branch, changes and tasks below it, and on the selected row
  it's drawn in the title's colour so it stays readable on the highlight.
- Recap lines wrap at 80 columns at most, however wide the popup.

## 0.1.1 — 2026-09-25

- Opening the popup while an earlier one is still up (left on another
  client, where you can't see it) closes that one and opens it here,
  instead of failing with *a popup pane is already open*. Another plugin's
  popup is left alone, with a toast saying so.
- The popup's size is in the manifest, so it's the same however it's opened.

## 0.1.0 — 2026-09-25

- A popup with every agent in herdr, in the sidebar's status glyphs and
  colours, what needs you first and within a status what has waited longest.
  Enter or a click goes to the agent.
- For Claude agents, a recap of where each got to (Claude Code's own
  `/recap`), written ahead of time: an agent that finishes, or stops to ask
  you something, and that you don't look at within `recap_after_seconds`
  (3 minutes) gets its recap then, while its prompt cache is still warm.
  Opening the popup rewrites any that are out of date.
- What a blocked agent is waiting for, in red: its question, or the
  command or edit it wants approved.
- Under each title: git branch, uncommitted and unpushed work (*6 files
  +214 −58 ↑2*), task progress (*4/7 tasks*) and the pane's tokens (a PR
  badge from herdr-github…; `tokens` in config.json picks which). The
  selected agent also shows your last prompt, its model and its permission
  mode. Right of the title, how long it's been in its state (*waiting 3m*).
- `r` replies to the selected agent without leaving the popup, on as many
  lines as you like; `ctrl+r` writes its recap again.
- In herdr's theme colours: the title in its brightest text, each detail in
  its own colour. Glyphs are ones common monospace fonts have.
- Sessions are found through the agent's own process, so profile switchers
  that set `CLAUDE_CONFIG_DIR` (clauth and the like) work, and recaps run the
  agent's own `claude` with its PATH: herdr's server often has a bare PATH.
- `herdr-recap list` shows what it reads from each agent; a demo on
  fictional agents, and `scripts/screenshots.sh` for the README's images.
