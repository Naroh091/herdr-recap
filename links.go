package main

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"
)

// The plugins a row's tokens belong to: a click on a pull request opens
// herdr-github on it, and one on a Linear issue herdr-linear, when they're
// installed.
const githubPlugin, linearPlugin = "herdr-github", "herdr-linear"

// returnEnv is how a popup is told where it was opened from: a popupRef, as
// JSON, to open again when it's closed. herdr shows one popup at a time, so
// going from one plugin's to another's means closing the first.
const returnEnv = "HERDR_POPUP_RETURN"

// selectEnv is the pane this popup opens on: the agent it was left on for
// another plugin's popup.
const selectEnv = "HERDR_RECAP_PANE"

// popupRef is a plugin's popup, as plugin.pane.open takes it.
type popupRef struct {
	PluginID   string            `json:"plugin_id"`
	Entrypoint string            `json:"entrypoint"`
	Width      string            `json:"width,omitempty"`
	Height     string            `json:"height,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
}

// A plugin's or an entrypoint's ID, as herdr allows them: what a popupRef
// read from the environment is held to.
var refID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (p popupRef) valid() bool {
	return refID.MatchString(p.PluginID) && refID.MatchString(p.Entrypoint)
}

// siblings are the other plugins installed and enabled, each with its
// popup: the ones a row can link to. A variable so tests can stand one in.
var siblings = func() map[string]popupRef {
	var res struct {
		Plugins []struct {
			PluginID string `json:"plugin_id"`
			Enabled  bool   `json:"enabled"`
			Panes    []struct {
				ID        string `json:"id"`
				Placement string `json:"placement"`
				Width     string `json:"width"`
				Height    string `json:"height"`
			} `json:"panes"`
		} `json:"plugins"`
	}
	if herdrCall("plugin.list", nil, &res) != nil {
		return nil
	}
	out := map[string]popupRef{}
	for _, p := range res.Plugins {
		if !p.Enabled || p.PluginID == pluginID() {
			continue
		}
		for _, pane := range p.Panes {
			if pane.ID == "picker" && pane.Placement == "popup" {
				out[p.PluginID] = popupRef{PluginID: p.PluginID, Entrypoint: pane.ID, Width: pane.Width, Height: pane.Height}
			}
		}
	}
	return out
}

// handoffWait is how long a popup is given to close before the next one's
// open is given up on.
const handoffWait = 4 * time.Second

// handOver closes this popup for another: it's what this program becomes
// once the popup's own terminal is restored (swap.go).
func handOver(p popupRef) error {
	if !p.valid() {
		return errors.New("not a popup: " + p.PluginID + "/" + p.Entrypoint)
	}
	pendingSwap = &p
	return nil
}

// handOverDetached opens another popup once this one has closed: by this
// program again, detached, since the popup's own process ends with it. For
// when becoming it (swap.go) can't be done.
func handOverDetached(p popupRef) error {
	if !p.valid() {
		return errors.New("not a popup: " + p.PluginID + "/" + p.Entrypoint)
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return spawn("handoff", string(data))
}

// runHandoff is the detached half of handOver: it opens the popup as soon
// as herdr has room for it.
func runHandoff(arg string) error {
	var p popupRef
	if err := json.Unmarshal([]byte(arg), &p); err != nil {
		return err
	}
	if !p.valid() {
		return errors.New("not a popup: " + p.PluginID + "/" + p.Entrypoint)
	}
	params := map[string]any{
		"plugin_id": p.PluginID, "entrypoint": p.Entrypoint, "placement": "popup", "focus": true, "env": p.Env,
	}
	if p.Width != "" && p.Height != "" {
		params["width"], params["height"] = p.Width, p.Height
	}
	var err error
	for deadline := time.Now().Add(handoffWait); ; time.Sleep(60 * time.Millisecond) {
		if err = herdrCall("plugin.pane.open", params, nil); err == nil || !isHerdrCode(err, "ui_busy") || time.Now().After(deadline) {
			break
		}
	}
	if err != nil {
		notify("Recap", "Couldn't open "+p.PluginID+": "+err.Error())
	}
	return err
}

// here is this popup, to come back to with pane selected.
func here(pane string) popupRef {
	return popupRef{PluginID: pluginID(), Entrypoint: "picker", Width: popupWidth, Height: popupHeight,
		Env: map[string]string{selectEnv: pane}}
}

// githubFor is herdr-github's popup on an agent's pull request (its "pr"
// action's doing), with this popup to come back to.
func githubFor(to popupRef, a agentInfo) popupRef {
	back, _ := json.Marshal(here(a.PaneID))
	to.Env = map[string]string{
		"HERDR_GITHUB_CWD": a.Cwd, "HERDR_GITHUB_PANE": a.PaneID, "HERDR_GITHUB_SPACE": a.WorkspaceID,
		returnEnv: string(back),
	}
	return to
}

// linearFor is herdr-linear's popup on an agent's issue, with this popup to
// come back to.
func linearFor(to popupRef, a agentInfo) popupRef {
	back, _ := json.Marshal(here(a.PaneID))
	to.Env = map[string]string{
		"HERDR_LINEAR_CWD": a.Cwd, "HERDR_LINEAR_ISSUE": strings.TrimSpace(a.Tokens["issue"]),
		returnEnv: string(back),
	}
	return to
}

// cameFrom is the popup this one was opened from, if it was.
func cameFrom() (popupRef, bool) {
	var p popupRef
	if json.Unmarshal([]byte(os.Getenv(returnEnv)), &p) != nil || !p.valid() {
		return popupRef{}, false
	}
	return p, true
}
