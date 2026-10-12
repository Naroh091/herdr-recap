package main

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestSwapEnvIsTheOtherPluginsEnvironment(t *testing.T) {
	env := []string{
		"PATH=/bin", "HERDR_SOCKET_PATH=/s", "HERDR_PANE_ID=w1:p1",
		"HERDR_PLUGIN_ID=herdr-recap", "HERDR_PLUGIN_ROOT=/plugins/herdr-recap", "HERDR_PLUGIN_ENTRYPOINT_ID=picker",
		"HERDR_PLUGIN_CONFIG_DIR=/cfg/plugins/config/herdr-recap", "HERDR_PLUGIN_STATE_DIR=/state/plugins/herdr-recap",
		"HERDR_PLUGIN_CONTEXT_JSON={}", "HERDR_RECAP_PANE=w1:p4", "HERDR_GITHUB_PANE=old", returnEnv + "=old",
		"HERDR_SNOOZE_PANE=w1:p2", "HERDR_LINEAR_ISSUE=ENG-1",
	}
	p := popupRef{PluginID: "herdr-github", Entrypoint: "picker", Env: map[string]string{"HERDR_GITHUB_PANE": "w1:p4", returnEnv: "{}"}}
	got := swapEnv(env, "herdr-recap", p, "/plugins/herdr-github")
	sort.Strings(got)
	want := []string{
		"HERDR_GITHUB_PANE=w1:p4", "HERDR_PANE_ID=w1:p1",
		"HERDR_PLUGIN_CONFIG_DIR=/cfg/plugins/config/herdr-github", "HERDR_PLUGIN_ENTRYPOINT_ID=picker",
		"HERDR_PLUGIN_ID=herdr-github", "HERDR_PLUGIN_ROOT=/plugins/herdr-github",
		"HERDR_PLUGIN_STATE_DIR=/state/plugins/herdr-github", returnEnv + "={}", "HERDR_SOCKET_PATH=/s", "PATH=/bin",
	}
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Directories that aren't named for this plugin aren't guessed at: the
	// other plugin falls back to its own defaults.
	got = swapEnv([]string{"HERDR_PLUGIN_STATE_DIR=/tmp/elsewhere"}, "herdr-recap", p, "/r")
	for _, kv := range got {
		if strings.HasPrefix(kv, "HERDR_PLUGIN_STATE_DIR=") {
			t.Errorf("guessed %s", kv)
		}
	}
}
