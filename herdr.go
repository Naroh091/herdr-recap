package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// herdrError is a refusal from the herdr server, as opposed to a transport failure.
type herdrError struct {
	Code    string
	Message string
}

func (e *herdrError) Error() string { return e.Code + ": " + e.Message }

func socketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "herdr", "herdr.sock")
}

// herdrCall sends one request and decodes `result` into out. The server answers
// a single line per connection and hangs up.
func herdrCall(method string, params any, out any) error {
	conn, err := net.DialTimeout("unix", socketPath(), 3*time.Second)
	if err != nil {
		return fmt.Errorf("herdr socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if params == nil {
		params = map[string]any{}
	}
	req, err := json.Marshal(map[string]any{"id": "herdr-recap", "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReaderSize(conn, 1<<20).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("herdr %s: %w", method, err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return fmt.Errorf("herdr %s: bad reply: %w", method, err)
	}
	if reply.Error != nil {
		return &herdrError{reply.Error.Code, reply.Error.Message}
	}
	if out != nil && len(reply.Result) > 0 {
		return json.Unmarshal(reply.Result, out)
	}
	return nil
}

func isHerdrCode(err error, code string) bool {
	var he *herdrError
	return errors.As(err, &he) && he.Code == code
}

func notify(title, body string) {
	_ = herdrCall("notification.show", map[string]any{"title": title, "body": body}, nil)
}

// agentInfo is one entry of agent.list: a pane with a recognised agent in it.
type agentInfo struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Agent       string `json:"agent"`
	Name        string `json:"name"`
	Status      string `json:"agent_status"`
	Focused     bool   `json:"focused"`
	Cwd         string `json:"cwd"`
	Title       string `json:"terminal_title_stripped"`
	// Tokens are the pane's display values other plugins report (a PR badge,
	// a profile…): what the sidebar's $name row tokens show.
	Tokens map[string]string `json:"tokens"`
	// StateChangeSeq moves on every status change. A scheduled recap compares
	// it to what it saw when it was scheduled: if it moved, you've looked (done
	// became idle) or the agent carried on, and the recap is no longer wanted.
	StateChangeSeq int64        `json:"state_change_seq"`
	AgentSession   agentSession `json:"agent_session"`
}

type agentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

func listAgents() ([]agentInfo, error) {
	var res struct {
		Agents []agentInfo `json:"agents"`
	}
	err := herdrCall("agent.list", nil, &res)
	sanitize(&res.Agents)
	return res.Agents, err
}

type workspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

// listWorkspaces is the workspaces in sidebar order, for naming them on the
// rows and ordering the rows. Without it the rows go unnamed, nothing worse.
func listWorkspaces() []workspaceInfo {
	var res struct {
		Workspaces []workspaceInfo `json:"workspaces"`
	}
	if herdrCall("workspace.list", nil, &res) != nil {
		return nil
	}
	sanitize(&res.Workspaces)
	return res.Workspaces
}

// process is one of a pane's foreground processes.
type process struct {
	PID  int      `json:"pid"`
	Name string   `json:"name"`
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

func paneProcesses(paneID string) ([]process, error) {
	var res struct {
		ProcessInfo struct {
			Foreground []process `json:"foreground_processes"`
		} `json:"process_info"`
	}
	err := herdrCall("pane.process_info", map[string]any{"pane_id": paneID}, &res)
	return res.ProcessInfo.Foreground, err
}

func focusAgent(paneID string) error {
	return herdrCall("agent.focus", map[string]any{"target": paneID}, nil)
}

// openPopup opens one of this plugin's pane entrypoints as a popup. The menu the
// action was picked from may still be closing, so ui_busy is retried briefly.
func openPopup(entrypoint, width, height string, env map[string]string) error {
	params := map[string]any{
		"plugin_id": pluginID(), "entrypoint": entrypoint, "placement": "popup",
		"focus": true, "width": width, "height": height, "env": env,
	}
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if err = herdrCall("plugin.pane.open", params, nil); err == nil || !isHerdrCode(err, "ui_busy") {
			return err
		}
		time.Sleep(150 * time.Millisecond)
	}
	return err
}

// hookPane is the pane an event hook fired for, from HERDR_PLUGIN_EVENT_JSON;
// "" when there's no event (a startup hook, or run by hand).
func hookPane() string {
	var ev struct {
		PaneID string `json:"pane_id"`
		Data   struct {
			PaneID string `json:"pane_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_EVENT_JSON")), &ev)
	if ev.PaneID != "" {
		return ev.PaneID
	}
	return ev.Data.PaneID
}
