package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// waiting is a status that leaves an agent sitting until you come back to it:
// finished and not yet looked at (done), or asking you something (blocked).
func waiting(a agentInfo) bool {
	return recapCapable(a) && (a.Status == "done" || a.Status == "blocked")
}

// tick runs on every agent status change (and at startup). It only reads the
// agent list: for each supported agent that is now waiting on you it starts a
// detached `recap --after` to write its recap later, if you haven't looked by
// then. An agent with a rename waiting for it to be between turns is given it. A status change is one agent's, so only that agent is considered;
// with no event (startup, by hand), every agent is.
func tick(cfg config) error {
	agents, err := agentsNow()
	if err != nil {
		return err
	}
	only := hookPane()
	pruneScheduled()
	for _, a := range agents {
		if only != "" && a.PaneID != only {
			continue
		}
		// A rename asked for while it was busy, now that it's between turns.
		if a.Agent == "claude" && (a.Status == "idle" || a.Status == "done") && renamePending(a.PaneID) {
			if s, err := sessionOf(context.Background(), cfg, a); err != nil {
				fmt.Fprintln(os.Stderr, "herdr-recap: renaming", a.PaneID+":", err)
			} else if err := giveTitle(a.PaneID, s.ID); err != nil {
				fmt.Fprintln(os.Stderr, "herdr-recap: renaming", a.PaneID+":", err)
			}
		}
		if !waiting(a) {
			continue
		}
		if err := schedule(cfg, a); err != nil {
			fmt.Fprintln(os.Stderr, "herdr-recap: scheduling", a.PaneID+":", err)
		}
	}
	return nil
}

func scheduledDir() string { return filepath.Join(stateDir(), "scheduled") }

// scheduledPath marks a recap as scheduled for one status change of one pane,
// so the flurry of events herdr can send for it schedules it once.
func scheduledPath(a agentInfo) string {
	return filepath.Join(scheduledDir(), notAlnum.ReplaceAllString(a.PaneID, "_")+"-"+strconv.FormatInt(a.StateChangeSeq, 10))
}

func schedule(cfg config, a agentInfo) error {
	if err := os.MkdirAll(scheduledDir(), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(scheduledPath(a), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	} else if err != nil {
		return err
	}
	f.Close()
	err = spawn("recap", "--after", strconv.Itoa(cfg.RecapAfterSeconds),
		"--seq", strconv.FormatInt(a.StateChangeSeq, 10), a.PaneID)
	if err != nil {
		os.Remove(scheduledPath(a))
	}
	return err
}

// spawn starts this program again, detached, logging to recap.log. A
// variable so tests can stand one in.
var spawn = func(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(stateDir(), "recap.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(self, args...)
	cmd.Stdout, cmd.Stderr = log, log
	// Its own session, so it outlives the hook: herdr may end the hook's
	// process group once tick returns.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// The worker's view of herdr, variables so tests can stand them in.
var (
	agentsNow = listAgents
	sessionOf = func(ctx context.Context, cfg config, a agentInfo) (claudeSession, error) {
		return resolveAgentSession(ctx, cfg, a)
	}
)

// pruneScheduled forgets marks old enough that their recap has long since run
// or died with the machine.
func pruneScheduled() {
	entries, _ := os.ReadDir(scheduledDir())
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(scheduledDir(), e.Name()))
		}
	}
}

// recapLater is `recap --after S --seq N PANE`: wait S seconds, then write the
// pane's recap if its agent is still waiting on you from the same status
// change. If the status moved in between (you looked, so done became idle, or
// you answered and it went back to work), nothing is spent.
func recapLater(ctx context.Context, cfg config, after time.Duration, seq int64, paneID string) error {
	a := agentInfo{PaneID: paneID, StateChangeSeq: seq}
	defer os.Remove(scheduledPath(a))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(after):
	}
	agents, err := agentsNow()
	if err != nil {
		return err
	}
	var now *agentInfo
	for i := range agents {
		if agents[i].PaneID == paneID {
			now = &agents[i]
		}
	}
	if now == nil || now.StateChangeSeq != seq || !waiting(*now) {
		return nil
	}
	ctx, cancel := recapContext(ctx, cfg)
	defer cancel()
	s, err := sessionOf(ctx, cfg, *now)
	if err != nil {
		return err
	}
	r, ran, err := ensureRecap(ctx, cfg, s, now.Title, false)
	if errors.Is(err, errNothingYet) || (err == nil && !ran) {
		return nil
	} else if err != nil {
		return err
	}
	fmt.Printf("%s %s %s ($%.4f): %s\n", time.Now().Format(time.RFC3339), paneID, s.ID, r.CostUSD, r.Text)
	return nil
}
