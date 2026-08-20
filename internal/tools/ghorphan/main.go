// ghorphan reconciles the GitHub issues that .githooks/bd-gh-sync's push pass
// minted twice for one bead. Two concurrent runs each snapshot the ledger, each
// see the same bead as unmirrored, and each open an issue for it; the bead ends
// up holding whichever `bd github push` wrote external_ref last. The other
// number is an orphan — no bead points at it, and the sync's close pass keys on
// external_ref, so nothing can ever reach it. It stays open regardless of
// whether the work lands. Measured 2026-08-18: 18 of 223 open issues (bd
// gqlc-mmej, this tool is gqlc-mb8v).
//
// The lock that stops NEW orphans being minted is a separate change (gqlc-mmej
// scope item 1). This is item 3: the ones that already exist.
//
// THE PREDICATE, and why each of its three parts is required. An open issue O
// with no bead pointing at it is closed in favour of a bound issue C only when
// all of:
//
//   - O.Title == C.Title, byte for byte. This is the trigger, not the evidence.
//     Two genuinely distinct beads may legitimately share a title, so on its own
//     it decides nothing.
//   - O.Body == C.Body, byte for byte. `bd github push` writes the issue body
//     from the bead description, so two issues minted for the same bead carry
//     the same bytes. This is the part that separates a duplicate from a
//     coincidence: distinct beads that share a title do not also share a
//     description. Compared as bytes rather than as an md5 — byte equality
//     implies digest equality and has no collision surface — while the report
//     prints digests, which is the form bd gqlc-mmej's measurement is stated in.
//   - |O.CreatedAt - C.CreatedAt| <= window. The race mints both issues inside
//     one read-modify-write, so they are seconds apart; two issues about the
//     same subject filed independently are not. Symmetric deliberately: 17 of
//     the 18 known pairs have the orphan created first, by 0-6s, but the
//     mechanism does not fix that order — whichever run's `bd github push`
//     writes external_ref LAST wins the bead, and that is not necessarily the
//     run that created its issue second. An asymmetric rule would refuse a
//     genuine race orphan. The default window is an order of magnitude above
//     the 6s maximum measured across those 18 pairs, and it is also the
//     ceiling: -window takes a smaller value and refuses a larger one.
//
// WHAT -window CAN DO TO A VERDICT. Both directions can turn a REFUSE into a
// CLOSE, which is why the ceiling is a refusal rather than a warning and why
// nothing printed by this tool tells an operator that a refusal is beyond the
// reach of a flag (bd gqlc-mb8v, review round 1 — the earlier wording claimed
// exactly that and this package's own rows falsify it):
//
//   - Wider loosens adjacency, so a pair that missed it starts matching. 814
//     and 816 sit 61s apart: REFUSE at 60s, CLOSE at 120s. This direction is
//     refused outright in plan(). It buys nothing measurable — a one-year
//     window over the reconstructed 2026-08-18 board yields the same 16 closes
//     and 2 refusals — and a candidate's evidence line already carries its Δt
//     and which side of the window it fell on, so "how far apart were they" is
//     answered without moving the flag. The Δt there is rounded to the second
//     and the side is not, so a 60.4s gap prints as "1m0s apart (outside the
//     window)": read the parenthetical, not the number.
//   - Narrower tightens adjacency, which is the safe direction for a single
//     candidate and not for two: a refusal that reads "2 bound issues match" is
//     the tool declining to pick a canonical, and dropping one of the two out
//     of the window makes the pick for it. Still available, because the
//     operator asking for a stricter run is asking a coherent question, and
//     pinned as a behaviour rather than left to be discovered.
//
// and exactly one bound issue satisfies all three. Two would mean the tool
// cannot say which mirror is canonical, so it refuses rather than picking.
//
// WHAT IT WILL NOT DO. An issue that any bead names in external_ref is never
// closed, whatever its title says — checked when the plan is built and again at
// the write boundary, because that is the one property whose violation is not
// recoverable. Closing is opt-in: the default invocation reports and mutates
// nothing, because closing issues is visible, shared and awkward to undo.
// Anything the predicate does not fully satisfy is reported with the specific
// part that failed and skipped; the operator gets the evidence and makes the
// call. The two pairs bd gqlc-mmej flags as not byte-identical (505/503,
// 895/899) land in that bucket by design — see main_test.go's corpus test,
// which pins both refusals against the real captured bodies.
//
// Idempotence falls out of the candidate filter: only OPEN unbound issues are
// considered, so a second run over a reconciled corpus plans nothing.
package main

import (
	"context"
	"crypto/md5" //nolint:gosec // digests are printed for a human to cross-check against bd gqlc-mmej's measurement; the verdict compares bytes
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// An order of magnitude above the 6s maximum separation measured over the
	// 18 known pairs (bd gqlc-mmej). Tight by intent: every second of slack is
	// slack in the only part of the predicate that distinguishes "minted by one
	// race" from "filed twice on purpose".
	//
	// -window's ceiling as well as its default: plan() refuses a larger value.
	// That bounds the adjacency clause; it does not make the flag verdict-safe,
	// and the package comment says which verdicts a narrower window still
	// moves.
	defaultWindow = 60 * time.Second
	// `gh issue list`'s cap. The repository held 223 open and ~1000 total
	// issues when this was written; the cap has to exceed the true count or the
	// run is refused, never quietly narrowed. Same reasoning, and the same
	// number, as .githooks/bd-gh-sync's _gh_limit_all.
	defaultLimit = 1000
)

// issueRefPat reads the issue number out of a bead's external_ref. Deliberately
// the same expression .githooks/bd-gh-sync uses, matched over any URL: a ref
// naming another repository still marks the local number as spoken for here,
// which can only ever protect an issue from being closed.
var issueRefPat = regexp.MustCompile(`/issues/(\d+)$`)

// issue is the subset of `gh issue list --json` this reads. CreatedAt stays a
// string through decoding so a value GitHub's API would not have produced is
// reported as the bytes that arrived rather than as a zero time.
type issue struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	State     string `json:"state"`
}

// bead is the subset of `bd list --json` this reads.
type bead struct {
	ID          string `json:"id"`
	ExternalRef string `json:"external_ref"`
}

const (
	verdictClose  = "CLOSE"
	verdictRefuse = "REFUSE"
)

// finding is one decision about one unbound open issue. Canonical and Beads are
// set only on a CLOSE; Reason only on a REFUSE. Evidence carries the
// per-candidate detail a human needs to overrule a refusal by hand, and is
// populated on both verdicts so a CLOSE is auditable from the report alone.
type finding struct {
	Orphan    int
	Verdict   string
	Canonical int
	Beads     []string
	Reason    string
	Evidence  []string
}

func main() {
	act := flag.Bool("close", false, "actually close the orphans on GitHub; without it nothing is mutated")
	window := flag.Duration("window", defaultWindow, "maximum creation-time separation between an orphan and its canonical; a value above the default is refused, so this narrows the predicate or leaves it alone")
	limit := flag.Int("limit", defaultLimit, "--limit handed to `gh issue list`; a listing that reaches it is refused as a page rather than read as a set")
	flag.Parse()

	src := source{
		issues:     ghIssues,
		beads:      bdBeads,
		closeIssue: ghCloseIssue,
	}
	if err := run(context.Background(), os.Stdout, config{act: *act, window: *window, limit: *limit}, src); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type config struct {
	act    bool
	window time.Duration
	limit  int
}

// source is the whole boundary with the outside world: two reads and one write.
// It is a struct of functions rather than an interface so a test can replace one
// leg and leave the others real, and so the write leg can be given a body that
// fails the test if it is ever called on a dry run.
type source struct {
	issues     func(ctx context.Context, limit int) ([]issue, error)
	beads      func(ctx context.Context) ([]bead, error)
	closeIssue func(ctx context.Context, number int, comment string) error
}

// reporter is the output side. It exists because apply() writes as it closes
// rather than at the end — a run that dies partway through an irreversible
// sequence has to have already said which issues it got to — and every one of
// those writes has an error nobody can usefully act on mid-sequence. The first
// one is kept and surfaced by the caller, so a report that went nowhere is not
// silently a report that had nothing to say.
type reporter struct {
	w   io.Writer
	err error
}

func (r *reporter) printf(format string, a ...any) {
	if r.err != nil {
		return
	}
	_, r.err = fmt.Fprintf(r.w, format, a...)
}

func run(ctx context.Context, out io.Writer, cfg config, src source) error {
	issues, err := src.issues(ctx, cfg.limit)
	if err != nil {
		return fmt.Errorf("ghorphan: read GitHub issues: %w", err)
	}
	beads, err := src.beads(ctx)
	if err != nil {
		return fmt.Errorf("ghorphan: read the bead ledger: %w", err)
	}
	findings, err := plan(issues, beads, cfg.window)
	if err != nil {
		return err
	}
	rep := &reporter{w: out}
	rep.printf("%s", render(findings))

	closes := 0
	for _, f := range findings {
		if f.Verdict == verdictClose {
			closes++
		}
	}
	refusals := len(findings) - closes

	if !cfg.act {
		// The parenthetical here used to read "a refusal needs a human, not a
		// wider flag". It was false when printed and it was printed at the
		// moment the operator decides what to do next: -window moves refusals
		// in both directions (see the package comment). Nothing replaced it —
		// the counts are the report, and the per-candidate evidence above them
		// is what a refusal is decided on.
		rep.printf("ghorphan: DRY RUN — nothing was mutated. %d issue(s) would be closed, %d refused. Re-run with -close to act.\n", closes, refusals)
		return rep.err
	}
	closed, applyErr := apply(ctx, rep, findings, boundIssues(beads), src.closeIssue)
	rep.printf("ghorphan: closed %d of %d planned issue(s), refused %d.\n", closed, closes, refusals)
	return errors.Join(applyErr, rep.err)
}

// plan is the comparator: given both listings, decide what to do. It performs no
// I/O, so every branch below is reachable from a table test. Measured with
// `go test ./internal/tools/ghorphan -coverprofile=c.out && go tool cover
// -func=c.out`: plan, apply, render, closeComment, candidateEvidence,
// bodyRelation, digest and checkTruncation are at 100.0% of statements.
//
// The uncovered remainder in that same profile is main(), ghIssues, bdBeads,
// ghCloseIssue and run3, all at 0.0% — the flag entrypoint and the process
// boundary, which no automated test may reach here: this tool's write is `gh
// issue close` against the live repository. Two statements more are unreachable
// rather than untested, and are named where they sit: boundIssues' strconv
// failure on a \d+ capture, and firstDifference's terminal return.
//
// Both inputs are refused when empty. An empty issue listing and a repository
// with nothing to reconcile produce the same empty plan, and only the second is
// a measurement; an empty bead ledger is worse, because every issue then looks
// unbound and the protection this tool leans on is the ledger.
func plan(issues []issue, beads []bead, window time.Duration) ([]finding, error) {
	if window < 0 {
		return nil, fmt.Errorf("ghorphan: -window is %s; a negative window makes every pair non-adjacent, which is a silent no-op rather than a stricter rule", window)
	}
	// The ceiling. Adjacency is one of the two clauses a REFUSE rests on, so a
	// window above the default rewrites refusals into closes: 814 and 816 sit
	// 61s apart and go REFUSE at 60s, CLOSE at 120s. Refused rather than warned
	// about, because the operator reading the warning would be reading it in
	// the report of the run that had already decided.
	if window > defaultWindow {
		return nil, fmt.Errorf("ghorphan: -window is %s, above the %s ceiling; a wider window loosens the adjacency clause and can turn a refusal into a close, which is an irreversible write. Every candidate's Δt is already in the report, so read that instead", window, defaultWindow)
	}
	if len(issues) == 0 {
		return nil, errors.New("ghorphan: the GitHub issue listing came back empty; refusing, because that is indistinguishable from a repository with nothing to reconcile")
	}
	if len(beads) == 0 {
		return nil, errors.New("ghorphan: the bead ledger came back empty; refusing, because with no bead pointing anywhere every issue reads as unbound and nothing is protected")
	}

	createdAt := make(map[int]time.Time, len(issues))
	seen := make(map[int]bool, len(issues))
	for _, i := range issues {
		if seen[i.Number] {
			return nil, fmt.Errorf("ghorphan: the issue listing holds #%d twice; refusing to reconcile a corpus this tool cannot key by number", i.Number)
		}
		seen[i.Number] = true
		t, err := time.Parse(time.RFC3339, i.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("ghorphan: issue #%d has createdAt %q, which is not RFC3339: %w — creation-time adjacency is a third of the predicate, so an unreadable timestamp is refused rather than treated as far apart", i.Number, i.CreatedAt, err)
		}
		createdAt[i.Number] = t
	}

	bound := boundIssues(beads)

	// Candidate canonicals are indexed by title over the BOUND issues only. An
	// unbound issue is never a canonical: "orphan" is defined relative to a
	// mirror the ledger actually tracks, and two unbound duplicates give no
	// reason to prefer either.
	//
	// A bound issue's own state is not consulted. 4 of the 18 known pairs have
	// a canonical that is already closed (its bead closed and the sync's close
	// pass reached it); requiring an open canonical would strand exactly the
	// orphans whose work has landed.
	byTitle := make(map[string][]issue)
	for _, i := range issues {
		if len(bound[i.Number]) > 0 {
			byTitle[i.Title] = append(byTitle[i.Title], i)
		}
	}

	var findings []finding
	for _, o := range issues {
		if !strings.EqualFold(o.State, "open") {
			continue
		}
		// The protection. Anything a bead names is out of scope whatever its
		// title says, and this is also what makes a second run a no-op on
		// everything the first one closed.
		if len(bound[o.Number]) > 0 {
			continue
		}
		cands := byTitle[o.Title]
		if len(cands) == 0 {
			// An unbound open issue with no title twin is an ordinary
			// hand-filed issue, not a race artefact. Reporting those would bury
			// the ones this tool is about under ~200 lines of noise.
			continue
		}

		// o is unbound and every candidate is bound, so o is not among them and
		// there is no self-pairing to exclude here. The check that o is not its
		// own canonical lives in apply(), at the write boundary, where it holds
		// however the plan was arrived at — a copy of it here would be a branch
		// no input can reach, and so a branch no test can hold.
		var matched []issue
		evidence := make([]string, 0, len(cands))
		for _, c := range cands {
			bodyOK := o.Body == c.Body
			gap := createdAt[o.Number].Sub(createdAt[c.Number])
			adjacent := gap.Abs() <= window
			if bodyOK && adjacent {
				matched = append(matched, c)
			}
			evidence = append(evidence, candidateEvidence(o, c, bodyOK, adjacent, gap))
		}

		f := finding{Orphan: o.Number, Evidence: evidence}
		switch len(matched) {
		case 1:
			f.Verdict = verdictClose
			f.Canonical = matched[0].Number
			f.Beads = bound[matched[0].Number]
		case 0:
			f.Verdict = verdictRefuse
			f.Reason = fmt.Sprintf("shares a title with %d bound issue(s), none of which matches on both body and creation time", len(cands))
		default:
			f.Verdict = verdictRefuse
			f.Reason = fmt.Sprintf("%d bound issues match on title, body and creation time; nothing here says which mirror is canonical", len(matched))
		}
		findings = append(findings, f)
	}
	slices.SortFunc(findings, func(a, b finding) int { return a.Orphan - b.Orphan })
	return findings, nil
}

// candidateEvidence states, for one (orphan, candidate) pair, what each half of
// the evidence said. The body relation is spelled out rather than reduced to a
// boolean because the operator's next move on a refusal depends on which shape
// it is: a byte-exact prefix is a mirror one side edited afterwards (895/899),
// while a divergence partway through is two different descriptions.
func candidateEvidence(o, c issue, bodyOK, adjacent bool, gap time.Duration) string {
	verdict := "match"
	if !bodyOK || !adjacent {
		verdict = "no match"
	}
	return fmt.Sprintf("#%d vs #%d: %s — body %s (md5 %s vs %s), created %s apart (%s)",
		o.Number, c.Number, verdict,
		bodyRelation(o.Number, o.Body, c.Number, c.Body),
		digest(o.Body), digest(c.Body),
		gap.Abs().Round(time.Second),
		map[bool]string{true: "within the window", false: "outside the window"}[adjacent])
}

// bodyRelation describes how two bodies differ, for the report only — no branch
// of it reaches a verdict.
func bodyRelation(an int, a string, bn int, b string) string {
	switch {
	case a == b:
		return "byte-identical"
	case strings.HasPrefix(b, a):
		return fmt.Sprintf("#%d's is a byte-exact prefix of #%d's, which carries %d more byte(s)", an, bn, len(b)-len(a))
	case strings.HasPrefix(a, b):
		return fmt.Sprintf("#%d's is a byte-exact prefix of #%d's, which carries %d more byte(s)", bn, an, len(a)-len(b))
	default:
		return fmt.Sprintf("differs, first at byte %d", firstDifference(a, b))
	}
}

// firstDifference is called only from bodyRelation's last arm, where neither
// string is a prefix of the other — so the loop always returns and the terminal
// return is the one Go requires rather than an answer. It is not a guard and
// nothing tests it.
func firstDifference(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func digest(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // reported for cross-checking against bd gqlc-mmej, never compared
	return hex.EncodeToString(sum[:])[:8]
}

// boundIssues maps an issue number to the bead ids naming it. A number named by
// more than one bead keeps all of them: the veto only asks whether the list is
// empty, and the report is more use naming every claimant than the first one.
func boundIssues(beads []bead) map[int][]string {
	bound := make(map[int][]string)
	for _, b := range beads {
		m := issueRefPat.FindStringSubmatch(b.ExternalRef)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			// The capture is \d+, so this is unreachable for anything short of
			// a number wider than int. Dropped rather than fatal: a ref this
			// tool cannot read must not remove protection from a different
			// issue, and it protects nothing on its own either way.
			continue
		}
		bound[n] = append(bound[n], b.ID)
	}
	for n := range bound {
		slices.Sort(bound[n])
	}
	return bound
}

func render(findings []finding) string {
	var sb strings.Builder
	for _, f := range findings {
		if f.Verdict == verdictClose {
			fmt.Fprintf(&sb, "CLOSE  #%d -> #%d (bead %s)\n", f.Orphan, f.Canonical, strings.Join(f.Beads, ", "))
		} else {
			fmt.Fprintf(&sb, "REFUSE #%d: %s\n", f.Orphan, f.Reason)
		}
		for _, e := range f.Evidence {
			fmt.Fprintf(&sb, "         %s\n", e)
		}
	}
	return sb.String()
}

// closeComment is what a closed orphan is left carrying. It names the canonical
// number twice on purpose: GitHub renders the first as a cross-reference on the
// canonical's timeline, so the pair is navigable from both ends, and the last
// line survives the truncation GitHub applies to long comments in list views.
func closeComment(f finding) string {
	return fmt.Sprintf(
		"Duplicate of #%d, which is the mirror the bead ledger tracks (bead %s).\n\n"+
			"This issue was minted by a second, concurrent `.githooks/bd-gh-sync push` "+
			"pass: both runs read the ledger before either had written to it, both saw "+
			"the same bead as unmirrored, and each opened an issue for it. No bead "+
			"carries this number in `external_ref`, and the sync's close pass keys on "+
			"`external_ref`, so nothing could ever close this issue — it would stay "+
			"open whether or not the work landed. See bd gqlc-mmej.\n\n"+
			"Closed by `internal/tools/ghorphan` (bd gqlc-mb8v). The work is tracked on #%d.",
		f.Canonical, strings.Join(f.Beads, ", "), f.Canonical)
}

// apply performs the closes. The bound check is a deliberate repeat of plan's:
// they read the same map, so it catches no state plan could not, but it is the
// last thing between a defect in this file and an irreversible write on shared
// state. It aborts rather than skipping, because a plan disagreeing with the
// ledger it was built from means the remaining entries were computed by the
// same broken logic.
//
// A close that GitHub rejects does not abort: the remaining orphans are
// independent, and stopping on the first would leave a partial reconciliation
// whose extent depends on issue number order. The failures are joined and
// returned, so the run still exits non-zero.
func apply(ctx context.Context, rep *reporter, findings []finding, bound map[int][]string, closeIssue func(ctx context.Context, number int, comment string) error) (int, error) {
	closed := 0
	var failures []error
	for _, f := range findings {
		if f.Verdict != verdictClose {
			continue
		}
		if ids := bound[f.Orphan]; len(ids) > 0 {
			return closed, fmt.Errorf("ghorphan: refusing to close #%d — bead(s) %s name it in external_ref. The plan and the ledger disagree, which is a defect in ghorphan rather than a state to act on; %d close(s) had already been made", f.Orphan, strings.Join(ids, ", "), closed)
		}
		if f.Canonical == f.Orphan {
			return closed, fmt.Errorf("ghorphan: refusing to close #%d as a duplicate of itself; %d close(s) had already been made", f.Orphan, closed)
		}
		if err := closeIssue(ctx, f.Orphan, closeComment(f)); err != nil {
			failures = append(failures, fmt.Errorf("close #%d (duplicate of #%d): %w", f.Orphan, f.Canonical, err))
			continue
		}
		closed++
		rep.printf("closed #%d as a duplicate of #%d\n", f.Orphan, f.Canonical)
	}
	return closed, errors.Join(failures...)
}

// checkTruncation refuses a listing that came back holding exactly what was
// asked for. `gh issue list` cuts its answer at --limit and says nothing about
// having done so, which leaves a full page and a complete set as the same bytes.
// A short read here does not merely lose orphans: it can hide the canonical of
// an orphan that IS in the page, and this tool would then see a title with no
// bound twin and report nothing at all.
func checkTruncation(n, limit int) error {
	if n < limit {
		return nil
	}
	return fmt.Errorf("`gh issue list` returned %d issue(s) against a --limit of %d, so the answer is a page rather than a set; re-run with a larger -limit", n, limit)
}

func ghIssues(ctx context.Context, limit int) ([]issue, error) {
	out, err := run3(ctx, "gh", "issue", "list", "--state", "all", "--limit", strconv.Itoa(limit),
		"--json", "number,title,body,createdAt,state")
	if err != nil {
		return nil, err
	}
	var issues []issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("decode `gh issue list` output: %w", err)
	}
	if err := checkTruncation(len(issues), limit); err != nil {
		return nil, err
	}
	return issues, nil
}

func bdBeads(ctx context.Context) ([]bead, error) {
	// --limit 0 is bd's "no cap"; the default caps at 50 and says nothing about
	// it, which would silently unprotect every issue past the cap.
	out, err := run3(ctx, "bd", "list", "--status", "all", "--limit", "0", "--json")
	if err != nil {
		return nil, err
	}
	var beads []bead
	if err := json.Unmarshal(out, &beads); err != nil {
		return nil, fmt.Errorf("decode `bd list` output: %w", err)
	}
	return beads, nil
}

func ghCloseIssue(ctx context.Context, number int, comment string) error {
	// `--reason "not planned"` rather than `gh issue close --duplicate-of`,
	// which would be the richer statement: the 18 orphans reconciled by hand on
	// 2026-08-18 were closed with this reason, and a corpus where the same
	// defect reads two ways in the GitHub UI is one nobody can count. The
	// duplicate pointer is in the comment, where it is also readable by anyone
	// without the API.
	_, err := run3(ctx, "gh", "issue", "close", strconv.Itoa(number), "--reason", "not planned", "--comment", comment)
	return err
}

// run3 runs a command and returns stdout, folding stderr into the error. The
// default exec error is "exit status 1", which names neither the command nor
// what it said.
func run3(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}
