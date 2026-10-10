// herdr-recap: every coding agent in a herdr popup, with a one-line recap of
// where it got to, written while you were away.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const usage = `herdr-recap — what each of your agents was doing, in herdr

  action open      what the herdr action runs: opens the popup
  action demo      what the herdr action runs: the popup on fictional agents
  picker [--demo]  the popup itself; --demo (or HERDR_RECAP_DEMO=1) shows
                   fictional agents: no herdr or Claude needed, safe to screenshot
  tick             what herdr runs on an agent status change: schedules a recap
                   for each supported agent now waiting on you
  recap PANE       write PANE's recap now, if it's out of date (--force: anyway)
  recap --after S --seq N PANE
                   (internal) wait S seconds, then recap PANE if its status is
                   still the one numbered N
  list             what the popup would show, as text: agents, sessions, recaps
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-recap:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	// A broken config.json mustn't quietly stop the recaps: say so (a toast
	// from an action, the plugin log from a hook) and carry on with the
	// defaults, which loadConfig returns with the error.
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-recap: using the defaults:", err)
		if args[0] == "action" {
			notify("Recap", "Using the defaults: "+err.Error())
		}
	}

	switch args[0] {
	case "action":
		if len(args) < 2 {
			return errors.New("action needs a name")
		}
		return runAction(args[1])
	case "picker":
		demo := os.Getenv("HERDR_RECAP_DEMO") == "1" || (len(args) > 1 && args[1] == "--demo")
		return runPicker(ctx, cfg, demo)
	case "tick":
		return tick(cfg)
	case "recap":
		return runRecapCommand(ctx, cfg, args[1:])
	case "list":
		return debugList(cfg)
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runAction(name string) error {
	switch name {
	case "open", "demo":
		env := map[string]string{}
		if name == "demo" {
			env["HERDR_RECAP_DEMO"] = "1"
		}
		err := openPopup("picker", "80%", "75%", env)
		if isHerdrCode(err, "ui_busy") {
			// herdr shows one popup at a time, open while its command runs.
			// If it's ours, left open (on another client, say, where you
			// can't see it), end it and open it here; someone else's is
			// theirs to close.
			if !endOwnPopup() {
				notify("Recap", "Another popup is open: close it (esc) and try again.")
				return err
			}
			err = openPopup("picker", "80%", "75%", env)
		}
		if err != nil {
			notify("Recap", "Couldn't open the popup: "+err.Error())
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown action %q", name)
}

func runRecapCommand(ctx context.Context, cfg config, args []string) error {
	fs := flag.NewFlagSet("recap", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	after := fs.Int("after", -1, "")
	seq := fs.Int64("seq", -1, "")
	force := fs.Bool("force", false, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("recap: %w", err)
	}
	if fs.NArg() != 1 {
		return errors.New("recap needs one pane ID")
	}
	pane := fs.Arg(0)
	if *after >= 0 {
		if *seq < 0 {
			return errors.New("recap --after needs --seq")
		}
		return recapLater(ctx, cfg, time.Duration(*after)*time.Second, *seq, pane)
	}
	ctx, cancel := recapContext(ctx, cfg)
	defer cancel()
	// Preserve Claude's process-based resolver. Only consult agent.list when
	// the pane is not Claude and needs Memex metadata.
	s, err := resolveSession(pane)
	if err != nil {
		var agent *agentInfo
		agents, listErr := listAgents()
		if listErr != nil {
			return listErr
		}
		for i := range agents {
			if agents[i].PaneID == pane {
				agent = &agents[i]
				break
			}
		}
		if agent == nil {
			return fmt.Errorf("pane %s has no agent", pane)
		}
		s, err = resolveAgentSession(ctx, cfg, *agent)
	}
	if err != nil {
		return err
	}
	// The pane's title, for whether to suggest a better one.
	title := ""
	if agents, err := listAgents(); err == nil {
		for _, a := range agents {
			if a.PaneID == pane {
				title = sessionTitle(a)
			}
		}
	}
	r, _, err := ensureRecap(ctx, cfg, s, title, *force)
	if err != nil {
		return err
	}
	fmt.Println(r.Text)
	return nil
}

// popupFile holds the running popup's process, so an open that finds one
// already up can end it. A popup lasts as long as its command.
func popupFile() string { return filepath.Join(stateDir(), "popup.pid") }

// notePopup records this process as the popup, and returns a func that
// forgets it again, unless a newer popup has replaced it.
func notePopup() func() {
	pid := strconv.Itoa(os.Getpid())
	if os.MkdirAll(stateDir(), 0o700) != nil || os.WriteFile(popupFile(), []byte(pid), 0o600) != nil {
		return func() {}
	}
	return func() {
		if data, err := os.ReadFile(popupFile()); err == nil && strings.TrimSpace(string(data)) == pid {
			os.Remove(popupFile())
		}
	}
}

// endOwnPopup ends this plugin's popup if one is running, and waits for it
// to go: whether it was ours to end.
func endOwnPopup() bool {
	data, err := os.ReadFile(popupFile())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 || !isOwnPopup(pid) {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil || p.Signal(syscall.SIGTERM) != nil {
		return false
	}
	for range 20 {
		if p.Signal(syscall.Signal(0)) != nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return p.Signal(syscall.Signal(0)) != nil
}

// isOwnPopup checks the process is this program, not a stranger that was
// given the pid after ours exited.
func isOwnPopup(pid int) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	exe, err := processExe(pid)
	return err == nil && sameFile(exe, self)
}

func sameFile(a, b string) bool {
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(fa, fb)
}
