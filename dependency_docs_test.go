package main

import (
	"os"
	"strings"
	"testing"
)

func TestREADMEStatesHermesMemexDependencyAccurately(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	for _, want := range []string{
		"**Memex 0.24.0** or later for Codex and OpenCode recaps",
		"Hermes recaps\n  require [nicosuave/memex#220]",
		"https://github.com/nicosuave/memex/pull/220",
		"or a later Memex release containing it",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README does not contain %q", want)
		}
	}
	if strings.Contains(readme, "Memex 0.24.0 or later for Codex, OpenCode, and Hermes recaps") {
		t.Error("README incorrectly claims released Memex 0.24.0 supports Hermes recaps")
	}
}
