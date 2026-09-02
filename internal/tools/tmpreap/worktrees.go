package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The -worktrees mode. It answers a different question from the scratch reaper
// beside it: not "what is holding this filesystem" but "which of the worktrees
// this repository has REGISTERED can be removed".
//
// It exists because the only worktree sweep this town ever ran deleted a live
// agent's working directory while every safety question it asked answered safe
// (bd gqlc-24wf). Every one of those questions was about the CONTENT of the
// tree; the harm was to the OCCUPANT. So this mode asks both, and asks the
// content question in a form the candidate cannot answer about itself:
// landedOn compares the branch's touched paths against -base, so a tree's own
// ref can never make its content "already elsewhere".
//
// The population is read from the registry rather than from a scan root's
// children because the seat worktrees live beside the main checkout under
// $HOME, where the scratch guard rightly refuses -apply. That is also why this
// mode never deletes: it removes through `git worktree remove`, which re-asks
// git's own dirt question, and never with --force.

// wtVerdict is what the decision table concluded. Every registration gets one,
// with a reason, because a sweep whose report a stranger cannot check is a
// sweep nobody can safely authorise.
type wtVerdict string

const (
	// wtExcluded is never a candidate: the main checkout and the permanent seats.
	wtExcluded wtVerdict = "EXCLUDED"
	// wtPrune is a registration whose checkout git already calls stale. Nothing
	// on disk is removed for it; `git worktree prune` retires the bookkeeping.
	wtPrune wtVerdict = "PRUNE"
	// wtHold is every failure direction in this file.
	wtHold wtVerdict = "HOLD"
	wtReap wtVerdict = "REAP"
)

type wtEntry struct {
	path    string
	at      string
	verdict wtVerdict
	reason  string
}

type wtConfig struct {
	// seatPrefix is the basename prefix marking a permanent seat worktree.
	seatPrefix string
	base       string
	maxAge     time.Duration
	now        time.Time
}

// planWorktrees runs every registration through the table, first match wins.
//
// The order is the safety argument. Exclusions come first so no gate below can
// ever be asked about the main checkout or a seat. The two attribute gates come
// next because they are answers git has already computed. The oracle's
// questions follow, and the filesystem walk is last — it is the only gate that
// costs a traversal, and by then it runs only over trees that are already
// clean, landed and unoccupied.
//
// Every gate fails toward HOLD. There is no input on which an unanswered
// question produces a REAP.
func planWorktrees(ctx context.Context, cfg wtConfig, regs []worktreeReg, oracle worktreeOracle, held map[string]string) []wtEntry {
	out := make([]wtEntry, 0, len(regs))
	for i, reg := range regs {
		out = append(out, decideWorktree(ctx, cfg, i, reg, oracle, held))
	}
	return out
}

func decideWorktree(ctx context.Context, cfg wtConfig, index int, reg worktreeReg, oracle worktreeOracle, held map[string]string) wtEntry {
	e := wtEntry{path: reg.path, at: reg.at(), verdict: wtHold}
	hold := func(reason string) wtEntry { e.reason = reason; return e }

	// git documents the main worktree as the first stanza of the porcelain
	// list, and refuses to remove it in any case; this is belt and braces.
	if index == 0 {
		e.verdict, e.reason = wtExcluded, "main checkout"
		return e
	}
	// By NAME, deliberately, and not by the kingdom.toml roster. The two read
	// the same intent and fail in opposite directions: a name pattern that is
	// wrong leaks (an ad-hoc tree named like a seat is never reaped), while a
	// roster that has drifted DELETES — a sleeping seat's tree is clean and
	// landed, and a sleeping seat has no process standing in it, so occupancy
	// would not save it either. Over-exclusion is the direction to be wrong in.
	if strings.HasPrefix(filepath.Base(reg.path), cfg.seatPrefix) {
		e.verdict, e.reason = wtExcluded, "seat worktree, permanent (citizen-protocol)"
		return e
	}
	if reg.isPrunable {
		e.verdict, e.reason = wtPrune, "registration stale: "+reasonOr(reg.prunable, "git gave no reason")
		return e
	}
	if reg.isLocked {
		return hold("locked: " + reasonOr(reg.locked, "git gave no reason"))
	}

	info, err := os.Stat(reg.path)
	if err != nil {
		// Not prunable and not on disk. git may simply not have noticed yet;
		// removing the registration is `git worktree prune`'s call to make, and
		// this mode does not make it on git's behalf.
		return hold("path gone; git does not yet call it prunable: " + err.Error())
	}

	st, err := oracle(ctx, reg.path)
	switch {
	case err != nil:
		return hold("git could not answer for it: " + err.Error())
	case st.dirty:
		return hold("uncommitted or untracked changes: " + st.dirtyDetail)
	case !st.landed:
		// The row the whole mode exists for. A stale local -base fails toward
		// HOLD, and this tool deliberately does not fetch: a reaper that
		// updates its own notion of what has landed is a reaper that can talk
		// itself into a deletion.
		return hold("content is not on " + cfg.base + ": " + st.landedDetail)
	}

	if who, ok := held[reg.path]; ok {
		return hold("in use: " + who)
	}
	u := measure(reg.path, info)
	if u.unreadable != "" {
		return hold("not fully readable (" + u.unreadable + "), so its age was never established")
	}
	if age := cfg.now.Sub(u.newest); age < cfg.maxAge {
		return hold(fmt.Sprintf("modified %s ago, inside the %s threshold", age.Round(time.Second), cfg.maxAge))
	}

	e.verdict = wtReap
	e.reason = fmt.Sprintf("untouched for %s, no live process inside, clean, content on %s",
		cfg.now.Sub(u.newest).Round(time.Minute), cfg.base)
	return e
}

func reasonOr(reason, fallback string) string {
	if strings.TrimSpace(reason) == "" {
		return fallback
	}
	return reason
}

// runWorktrees is the mode's whole entry point: plan, report, and under -apply
// act in the same process. One process measures and acts for the same reason
// the scratch reaper does — a plan handed to a second invocation is a plan
// describing a filesystem that has moved.
func runWorktrees(ctx context.Context, o options, out io.Writer) error {
	pr := &printer{w: out}
	regs, err := listWorktreeRegistrations(ctx, o.repo)
	if err != nil {
		return err
	}

	paths := make([]string, 0, len(regs))
	for _, reg := range regs {
		paths = append(paths, reg.path)
	}
	// This caller's OWN fail-closed clause. procRefsUnder refuses a process
	// table that does not name this process, but a seam being fail-closed does
	// not save a caller that swallows its error: both caller-side mutations of
	// the scratch path's equivalent measured SURVIVED once (bd gqlc-2459). An
	// unreadable /proc must stop the PLAN here, because an empty held set reads
	// exactly like "no worktree is occupied".
	refs, err := procRefsUnder(o.procDir, paths)
	if err != nil {
		return err
	}

	cfg := wtConfig{
		seatPrefix: filepath.Base(regs[0].path) + "-seat-",
		base:       o.base,
		maxAge:     o.maxAge,
		now:        time.Now(),
	}
	entries := planWorktrees(ctx, cfg, regs, gitOracle{base: o.base}.state, heldUnder(paths, refs))
	reportWorktrees(pr, cfg, entries)

	if !o.apply {
		pr.printf("\nnothing was removed. Re-run with -apply (or `just worktree-reap apply`) to remove.\n")
		return pr.err
	}
	return errors.Join(applyWorktrees(ctx, o.repo, entries, pr), pr.err)
}

func reportWorktrees(pr *printer, cfg wtConfig, entries []wtEntry) {
	counts := map[wtVerdict]int{}
	for _, e := range entries {
		counts[e.verdict]++
	}
	pr.printf("%d registered worktree(s): %d REAP, %d PRUNE, %d HOLD, %d EXCLUDED "+
		"(age threshold %s, content compared against %s)\n\n",
		len(entries), counts[wtReap], counts[wtPrune], counts[wtHold], counts[wtExcluded],
		cfg.maxAge, cfg.base)
	for _, e := range entries {
		pr.printf("%-8s %s (%s)\n         %s\n", e.verdict, e.path, e.at, e.reason)
	}
}

// applyWorktrees removes what the plan chose and then retires stale
// registrations. `git worktree remove` is the actuator rather than os.RemoveAll
// on purpose: it is a second wall that re-asks git's own dirt question at the
// moment of removal, which is the only thing standing between the plan and a
// tree that got dirty after it was classified. It is never given --force.
//
// A refusal from git is reported and the loop continues. The failure direction
// there is a leaked worktree, which is what this tool exists to reduce and not
// what it exists to prevent at any cost.
//
// Nothing is archived, deliberately. A REAP is clean by construction, so it
// holds no uncommitted content; what dies with the checkout is gitignored
// build output, which .gitignore is the assertion is regenerable.
func applyWorktrees(ctx context.Context, repo string, entries []wtEntry, pr *printer) error {
	removed, refused, pruning := 0, 0, false
	for _, e := range entries {
		switch e.verdict {
		case wtExcluded, wtHold:
			// Named rather than defaulted so that a verdict added later cannot
			// reach this loop without someone deciding what it actuates.
		case wtPrune:
			pruning = true
		case wtReap:
			if _, err := git(ctx, repo, "worktree", "remove", e.path); err != nil {
				refused++
				pr.printf("\nrefused by git, left in place: %s\n         %v\n", e.path, err)
				continue
			}
			removed++
			pruning = true
		}
	}
	pr.printf("\nremoved %d worktree(s); git refused %d.\n", removed, refused)
	if !pruning {
		return nil
	}
	if _, err := git(ctx, repo, "worktree", "prune"); err != nil {
		return fmt.Errorf("prune stale worktree registrations: %w", err)
	}
	pr.printf("stale registrations pruned.\n")
	return nil
}
