# Changelog

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
