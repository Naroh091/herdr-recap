package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	// RecapAfterSeconds is how long an agent has to sit finished (done) or
	// waiting on you (blocked) without you looking before its recap is
	// written. Short glances away cost nothing. Native Claude recaps can reuse
	// the session cache when this stays below that cache's lifetime.
	RecapAfterSeconds int `json:"recap_after_seconds"`
	// Claude is the claude binary recaps run with. Empty = the one the agent
	// runs, or "claude" on PATH.
	Claude string `json:"claude"`
	// MemexAgents names the other agents to recap, through Memex: any of
	// codex, opencode, hermes. Empty = Claude only. It's for you to turn on:
	// their conversations are read by Memex and sent to the summarizer, which
	// by default is Claude, and each recap is a request of its own to pay for.
	MemexAgents []string `json:"memex_agents"`
	// Memex is the memex binary. Empty = "memex" on PATH.
	Memex string `json:"memex"`
	// Summarizer is the command that writes a Memex-read conversation's
	// recap: the prompt on stdin, the recap alone on stdout. Empty = Claude
	// Code's print mode, with SummarizerModel (empty = haiku: a recap is a
	// small job, and this one has no prompt cache to reuse).
	Summarizer      []string `json:"summarizer"`
	SummarizerModel string   `json:"summarizer_model"`
	// TranscriptMaxChars is the most of a conversation's end the summarizer
	// is sent.
	TranscriptMaxChars int `json:"transcript_max_chars"`
	// TimeoutSeconds bounds one recap.
	TimeoutSeconds int `json:"timeout_seconds"`
	// Theme: "dark", "light", or empty to ask the terminal.
	Theme string `json:"theme"`
	// Tokens names the pane tokens to show under a title, in order. Empty =
	// all of them, with a PR said once: prs, or else pr, without the pr_*
	// details.
	Tokens []string `json:"tokens"`
}

func pluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return "herdr-recap"
}

func xdgDir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

func configDir() string {
	if d := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "herdr", "plugins", "config", pluginID())
}

func stateDir() string {
	if d := os.Getenv("HERDR_PLUGIN_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "herdr", "plugins", pluginID())
}

// loadConfig reads config.json. On error it still returns the defaults.
func loadConfig() (config, error) {
	cfg, err := readConfig()
	if err != nil {
		cfg = config{}
	}
	return withDefaults(cfg), err
}

func readConfig() (config, error) {
	cfg := config{}
	data, err := os.ReadFile(filepath.Join(configDir(), "config.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cfg, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, errors.New("config.json: " + err.Error())
		}
	}
	return cfg, nil
}

func withDefaults(cfg config) config {
	if cfg.RecapAfterSeconds <= 0 {
		cfg.RecapAfterSeconds = 180
	}
	cfg.Claude = expandHome(cfg.Claude)
	cfg.Memex = expandHome(cfg.Memex)
	if cfg.Memex == "" {
		cfg.Memex = "memex"
	}
	if cfg.SummarizerModel == "" {
		cfg.SummarizerModel = "haiku"
	}
	if cfg.TranscriptMaxChars <= 0 {
		cfg.TranscriptMaxChars = 24000
	}
	for i := range cfg.Summarizer {
		cfg.Summarizer[i] = expandHome(cfg.Summarizer[i])
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 120
	}
	return cfg
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}
