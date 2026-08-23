// bdguard fails when `.beads/issues.jsonl` regresses vs a base ref: dropping
// an issue or reopening one that was closed at base. Additions and new
// closures pass, so `chore(bd)` sync PRs need no exemption. Motivated by
// bd gqlc-v2p (PR #422 pre-fix carried a stale export that would have
// reopened two closed beads and dropped one).
//
// Deleting a bead on purpose (bd gqlc-w29n: a sync bug minted a duplicate
// ghost bead) is declared by listing its id in `.beads/allowed-drops.txt`,
// which exempts that id from the dropped arm alone. That file is read at the
// PR head, so the exemption arrives in the diff a reviewer already reads.
//
// Correcting a close that was never earned (bd gqlc-npyp: a branch-close hook
// closed gqlc-rz0l against a SHA a rebase then discarded) is declared in
// `.beads/allowed-reopens.txt` as `<bead-id> <closing-sha>`, or — for a close
// whose reason cites no SHA at all — as `<bead-id> <base-updated-at>`, an
// RFC3339 timestamp that must name the same instant as the base record's
// `updated_at`. Both shapes quote a fact that is already committed on the base
// ref, which is the property that stops this being the bare-id allowlist
// gqlc-npyp exists to avoid; the second shape exists because the first reaches
// only closes whose reason happens to cite a SHA (bd gqlc-j068 — measured on
// the export at 92ebb9a7, that was 86 of 290 closed beads, so roughly seven in
// ten hand-written closes had no declaration bdguard would grant).
//
// The two shapes are kept disjoint on purpose. The SHA shape carries a veto —
// if a ref contains the SHA, the close was earned and the declaration is
// refused — and the timestamp shape has nothing to veto against, so a
// declaration could otherwise dodge the veto by naming the timestamp instead.
// A timestamp declaration is therefore refused outright when the base close
// reason cites a SHA some ref does contain, with a message pointing at the SHA
// shape. What that leaves, stated rather than left to be found: a reason
// citing an ORPHANED SHA accepts either shape, and the two agree there, since
// the veto would have stayed silent anyway.
//
// That arm has to separate two shapes that look identical in the status
// columns:
//
//   - REVERT — an older export overwriting a newer close. This is the defect
//     bdguard exists for.
//   - CORRECTION — a close the work never earned, undone in bd afterwards.
//
// The separator is recency, not reachability. A reverting record is a copy
// taken before the close, so its `updated_at` is at or behind base's; a
// correction is a write made after the close, so its `updated_at` is ahead.
// Base is read with `git show <ref>:...`, so the PR's own tree does not
// restate it — the declaration has to quote a fact already committed on the
// base ref. Head's `updated_at` is a field in the file the PR is editing, so
// it is a shape check, not an anti-forgery control (bd gqlc-ukgs).
//
// Reachability of the closing SHA — the first evidence considered — is kept
// only as a veto, for two reasons. gqlc squash-merges and deletes the branch,
// so an earned close's cited SHA is orphaned as surely as an unearned one's;
// and the CI checkout is depth-1, where objects outside the fetched pack are
// absent whatever their history. Read as authorisation it would pass
// declarations in CI it should refuse. Read as a veto it still refuses the
// case where the SHA is sitting on a ref, and stays silent when it has
// nothing to say.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	exportPath         = ".beads/issues.jsonl"
	allowedDropsPath   = ".beads/allowed-drops.txt"
	allowedReopensPath = ".beads/allowed-reopens.txt"
)

type record struct {
	Type        string `json:"_type"`
	ID          string `json:"id"`
	Status      string `json:"status"`
	UpdatedAt   string `json:"updated_at"`
	CloseReason string `json:"close_reason"`
}

// exemptions carries everything that can subtract from a regression finding.
// refContaining names a ref containing the given SHA, or "" when it found none
// and when it could not tell; the two are deliberately one value, since only a
// positive answer is used. A nil refContaining with a non-empty reopens map is
// an error rather than a disabled veto, so losing the wiring is loud.
type exemptions struct {
	drops         map[string]bool
	reopens       map[string]declaration
	refContaining func(sha string) string
}

// declaration is one entry of allowed-reopens.txt: which fact about the BASE
// record the entry quotes. Exactly one field is set, and which one is decided
// by the token's own shape, not by a keyword the author picks — a hex token is
// a sha, an RFC3339 token is a timestamp, and nothing else parses.
type declaration struct {
	sha      string // a sha the base close_reason cites, verbatim
	closedAt string // base's updated_at, for a close whose reason cites no sha
}

func main() {
	base := flag.String("base", "", "git ref to compare against (e.g. origin/master or a merge-base sha)")
	flag.Parse()
	if *base == "" {
		fmt.Fprintln(os.Stderr, "bdguard: -base is required")
		os.Exit(2)
	}
	if err := run(context.Background(), *base); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, base string) error {
	headBytes, err := os.ReadFile(exportPath)
	if err != nil {
		return fmt.Errorf("read head %s: %w", exportPath, err)
	}
	baseBytes, err := showAtRef(ctx, base, exportPath)
	if err != nil {
		if errors.Is(err, errPathAbsentAtRef) {
			// Missing at base is legitimate (repo before bd landed) — empty.
			baseBytes = nil
		} else {
			return fmt.Errorf("read base %s@%s: %w", exportPath, base, err)
		}
	}
	allowedDrops, err := readAllowedDrops(allowedDropsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", allowedDropsPath, err)
	}
	allowedReopens, err := readAllowedReopens(allowedReopensPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", allowedReopensPath, err)
	}
	return check(headBytes, baseBytes, base, exemptions{
		drops:   allowedDrops,
		reopens: allowedReopens,
		refContaining: func(sha string) string {
			return gitRefContaining(ctx, sha)
		},
	})
}

// readAllowedDrops treats an absent file as an empty allowlist: most branches
// delete no bead and carry no such file. Any other read failure is returned
// rather than folded into "absent", so a file that exists but cannot be read
// surfaces as itself instead of as an unexplained drop.
func readAllowedDrops(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return parseAllowedDrops(data), nil
}

// parseAllowedDrops reads one bead id per line, ignoring blank lines and lines
// whose first non-blank byte is '#'. Ids are matched exactly: an entry exempts
// that one id, with no wildcard or prefix form that could exempt a set.
func parseAllowedDrops(data []byte) map[string]bool {
	allowed := make(map[string]bool)
	for _, line := range bytes.Split(data, []byte("\n")) {
		id := bytes.TrimSpace(line)
		if len(id) == 0 || id[0] == '#' {
			continue
		}
		allowed[string(id)] = true
	}
	return allowed
}

// readAllowedReopens shares readAllowedDrops' absent-is-empty treatment, and
// its reasons.
func readAllowedReopens(path string) (map[string]declaration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return parseAllowedReopens(data)
}

// parseAllowedReopens reads `<bead-id> <closing-sha>` or `<bead-id>
// <base-updated-at>` per line, ignoring blank lines and lines whose first
// non-blank byte is '#'. A malformed line is an error: an id on its own is the
// shape allowed-drops.txt uses, and accepting it here would reduce this arm to
// a bare-id allowlist. A second entry for an id already seen is an error too,
// since the two would disagree about which close is being disputed.
func parseAllowedReopens(data []byte) (map[string]declaration, error) {
	allowed := make(map[string]declaration)
	for i, raw := range bytes.Split(data, []byte("\n")) {
		line := string(bytes.TrimSpace(raw))
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: want `<bead-id> <closing-sha>` or `<bead-id> <base-updated-at>`, got %d field(s): %q", i+1, len(fields), line)
		}
		id, token := fields[0], fields[1]
		var decl declaration
		switch {
		case isSHAToken(token):
			decl.sha = token
		case isRFC3339Token(token):
			decl.closedAt = token
		default:
			return nil, fmt.Errorf("line %d: %q is neither a 7-40 character lowercase hex sha nor an RFC3339 timestamp", i+1, token)
		}
		if prev, dup := allowed[id]; dup {
			return nil, fmt.Errorf("line %d: %s declared twice (%s then %s)", i+1, id, prev.token(), decl.token())
		}
		allowed[id] = decl
	}
	return allowed, nil
}

// token renders the declaration as the second field that produced it, so a
// message can quote what the file says rather than a reconstruction of it.
func (d declaration) token() string {
	if d.sha != "" {
		return d.sha
	}
	return d.closedAt
}

func isRFC3339Token(s string) bool {
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

// isSHAToken accepts an all-decimal token, deliberately and unlike the bd-close
// hook, which was taught to reject one (bd gqlc-7fn3: a close reason saying
// "1048576" was read as citing a sha and refused). About four in a hundred
// seven-character abbreviations are all-decimal, so rejecting the shape here
// would refuse real declarations. It is safe in this direction because a
// declaration is not granted for LOOKING like a sha: citesSHA still requires
// the base close_reason to quote the same token, and gitRefContaining still
// gets its veto. A declaration naming a decimal token resolves to no object, so
// the veto stays silent — the same outcome as any orphaned sha.
func isSHAToken(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// citesSHA reports whether text contains sha delimited by non-alphanumeric
// bytes on both sides. The trailing bound is what stops a declared `3dd322dc`
// from matching the `3dd322dcab…` of a different commit; the leading bound
// stops a longer token that ends in those characters from counting. An
// abbreviation of a cited sha is not a match — the token is copied verbatim or
// it is refused.
func citesSHA(text, sha string) bool {
	for i := 0; i+len(sha) <= len(text); i++ {
		if text[i:i+len(sha)] != sha {
			continue
		}
		if i > 0 && isAlnum(text[i-1]) {
			continue
		}
		if end := i + len(sha); end < len(text) && isAlnum(text[end]) {
			continue
		}
		return true
	}
	return false
}

func isAlnum(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// check is the comparator: given both files' bytes, decide whether the head
// regressed vs the base. Its only I/O is exemptions.refContaining, injected so
// its branches are reachable with no `git` shellout. Measured with
// `go test ./internal/tools/bdguard -coverprofile=c.out && go tool cover
// -func=c.out`: check and parse are at 100.0% of statements.
//
// The uncovered remainder in that same profile is main(), run(), showAtRef and
// gitRefContaining, all at 0.0% — the flag entrypoint and the git boundary. No
// automated test reaches them (bd gqlc-drvx); they were exercised only by a
// manual incident replay against a scratch repo outside the worktree.
//
// exemptions.drops exempts ids from the dropped arm only: a bead still present
// but no longer closed is a status regression, not the deletion the list
// declares. A listed id that was not dropped is inert rather than an error —
// once the deletion lands on master the id is absent at base and head alike,
// so entries go stale by design, and failing on them would fail every later PR
// until someone pruned the file. exemptions.reopens goes stale the same way,
// and additionally goes inert when a later close cites a different SHA.
func check(headBytes, baseBytes []byte, baseLabel string, ex exemptions) error {
	head, headLines, err := parse(headBytes)
	if err != nil {
		return fmt.Errorf("parse head %s: %w", exportPath, err)
	}
	if headLines > 0 && len(head) == 0 {
		return fmt.Errorf("bdguard: head %s has %d non-blank lines but zero issue records — suspected bd format drift (rename of _type or of the \"issue\" tag). Refusing to run: a silent pass here would be indistinguishable from a clean export", exportPath, headLines)
	}
	baseIssues, baseLines, err := parse(baseBytes)
	if err != nil {
		return fmt.Errorf("parse base %s@%s: %w", exportPath, baseLabel, err)
	}
	if baseLines > 0 && len(baseIssues) == 0 {
		return fmt.Errorf("bdguard: base %s@%s has %d non-blank lines but zero issue records — suspected bd format drift (rename of _type or of the \"issue\" tag). Refusing to run: a silent pass here would be indistinguishable from a clean export", exportPath, baseLabel, baseLines)
	}
	if len(ex.reopens) > 0 && ex.refContaining == nil {
		return fmt.Errorf("bdguard: %s declares %d reopen(s) but no ref probe was wired; refusing to grant an exemption whose veto is missing", allowedReopensPath, len(ex.reopens))
	}

	var dropped, reopened []string
	for id, baseRec := range baseIssues {
		headRec, ok := head[id]
		if !ok {
			if !ex.drops[id] {
				dropped = append(dropped, id)
			}
			continue
		}
		if baseRec.Status != "closed" || headRec.Status == "closed" {
			continue
		}
		line := fmt.Sprintf("%s (base=closed, head=%s)", id, headRec.Status)
		decl, declared := ex.reopens[id]
		if !declared {
			reopened = append(reopened, line)
			continue
		}
		if refusal := refuseReopen(baseRec, headRec, decl, ex.refContaining); refusal != "" {
			reopened = append(reopened, line+"\n      declaration `"+id+" "+decl.token()+"` refused: "+refusal)
		}
	}
	if len(dropped) == 0 && len(reopened) == 0 {
		return nil
	}

	sort.Strings(dropped)
	sort.Strings(reopened)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "bdguard: %s regressed against %s\n", exportPath, baseLabel)
	if len(dropped) > 0 {
		fmt.Fprintf(&buf, "  dropped issues (present at base, absent at head):\n")
		for _, id := range dropped {
			fmt.Fprintf(&buf, "    - %s\n", id)
		}
	}
	if len(reopened) > 0 {
		fmt.Fprintf(&buf, "  reopened issues (closed at base, not closed at head):\n")
		for _, id := range reopened {
			fmt.Fprintf(&buf, "    - %s\n", id)
		}
	}
	fmt.Fprintf(&buf, "hint: %s is a passive bd export; if a real sync, run bd commands and stage only that file", exportPath)
	if len(dropped) > 0 {
		fmt.Fprintf(&buf, "\nhint: a deliberate deletion is declared by listing the exact id in %s (drops only — that file does not reach the reopened arm)", allowedDropsPath)
	}
	if len(reopened) > 0 {
		fmt.Fprintf(&buf, "\nhint: an unearned close is corrected by adding `<bead-id> <closing-sha>` to %s, where the sha is one the base record's close_reason cites, and the head record's updated_at is later than base's. When the base close_reason cites no sha at all, quote base's updated_at instead: `<bead-id> <base-updated-at>` in RFC3339", allowedReopensPath)
	}
	return errors.New(buf.String())
}

// refuseReopen returns why a declared reopen is refused, or "" to grant it.
// Both timestamps must parse: an unreadable one is refused rather than assumed
// in either direction, since the whole discrimination rests on their order.
func refuseReopen(baseRec, headRec record, decl declaration, refContaining func(string) string) string {
	if refusal := refuseDeclaredFact(baseRec, decl, refContaining); refusal != "" {
		return refusal
	}
	baseAt, err := time.Parse(time.RFC3339, baseRec.UpdatedAt)
	if err != nil {
		return fmt.Sprintf("base updated_at %q is not RFC3339: %v", baseRec.UpdatedAt, err)
	}
	headAt, err := time.Parse(time.RFC3339, headRec.UpdatedAt)
	if err != nil {
		return fmt.Sprintf("head updated_at %q is not RFC3339: %v", headRec.UpdatedAt, err)
	}
	if !headAt.After(baseAt) {
		return fmt.Sprintf("head updated_at %s is not after base %s, which is the shape of an older export overwriting a newer close, not of a correction", headRec.UpdatedAt, baseRec.UpdatedAt)
	}
	if decl.sha != "" {
		if ref := refContaining(decl.sha); ref != "" {
			return fmt.Sprintf("%s contains %s, so the close cites work that is on a ref", ref, decl.sha)
		}
	}
	return ""
}

// refuseDeclaredFact checks the half that differs between the two shapes: that
// the declaration quotes something the BASE record actually says. The recency
// rule and the reachability veto are shared and live in the caller.
func refuseDeclaredFact(baseRec record, decl declaration, refContaining func(string) string) string {
	if decl.sha != "" {
		if !citesSHA(baseRec.CloseReason, decl.sha) {
			return fmt.Sprintf("the close_reason recorded at base cites no such sha, so the declaration names a close this base does not show (base close_reason %d bytes)", len(baseRec.CloseReason))
		}
		return ""
	}

	// The timestamp shape. It has no reachability veto of its own, so it is
	// refused whenever the sha shape WOULD have had one to apply: otherwise a
	// declaration could escape the veto by quoting the timestamp instead of the
	// sha the reason already carries. An orphaned sha is not a bar, because
	// there the veto would have stayed silent and the two shapes agree.
	for _, cited := range citedSHATokens(baseRec.CloseReason) {
		if ref := refContaining(cited); ref != "" {
			return fmt.Sprintf("the base close_reason cites %s and %s contains it, so this close is on a ref; a timestamp declaration cannot be granted over that — declare the sha instead and see the refusal for what it is", cited, ref)
		}
	}

	declaredAt, err := time.Parse(time.RFC3339, decl.closedAt)
	if err != nil {
		// Unreachable via parseAllowedReopens, which only stores a token that
		// already parsed. Kept because refuseReopen is also called directly by
		// tests, where a hand-built declaration can carry anything.
		return fmt.Sprintf("declared timestamp %q is not RFC3339: %v", decl.closedAt, err)
	}
	baseAt, err := time.Parse(time.RFC3339, baseRec.UpdatedAt)
	if err != nil {
		return fmt.Sprintf("base updated_at %q is not RFC3339: %v", baseRec.UpdatedAt, err)
	}
	if !declaredAt.Equal(baseAt) {
		return fmt.Sprintf("the declared timestamp %s is not base's updated_at %s, so the declaration names a close this base does not show", decl.closedAt, baseRec.UpdatedAt)
	}
	return ""
}

// citedSHATokens returns the sha-shaped tokens in text, delimited the same way
// citesSHA requires. Used only to decide whether a timestamp declaration is
// dodging the sha shape's veto, so a false positive costs a refusal with a
// message naming the token, not a silent grant.
func citedSHATokens(text string) []string {
	var out []string
	start := -1
	for i := 0; i <= len(text); i++ {
		if i < len(text) && isAlnum(text[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if tok := text[start:i]; isSHAToken(tok) {
				out = append(out, tok)
			}
			start = -1
		}
	}
	return out
}

// gitRefContaining names a ref whose history contains sha, or "" when it finds
// none. Two different states collapse to "": the object is absent (outside the
// fetched pack), and the object is present but no ref reaches it. See the
// package comment for why that collapse is safe here.
//
// Shallowness alone does not silence the probe, and pack membership alone does
// not make it speak: this answers positively only when the sha came down in
// the fetched pack AND a ref reaches it. Measured in `git clone --depth=1`:
// for the tip that came down in the pack, `git for-each-ref --contains=<tip>`
// prints `refs/heads/master` and `git merge-base --is-ancestor <tip> HEAD`
// exits 0, so this returns a ref there. For a commit outside the pack,
// `git cat-file -e <sha>^{commit}` exits 128 and this short-circuits to "".
// For a commit fetched into that same clone by name — `git fetch --depth=1
// origin <sha>`, the shape ci.yml uses to place the base commit — the object
// is there (`cat-file -e` exits 0) yet `for-each-ref --contains` prints
// nothing and `merge-base --is-ancestor` exits 1: in the pack and still
// silent, because the graft truncates every ref's history walk. That is the
// second collapse state above.
func gitRefContaining(ctx context.Context, sha string) string {
	if err := exec.CommandContext(ctx, "git", "cat-file", "-e", sha+"^{commit}").Run(); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "git", "for-each-ref", "--contains="+sha, "--count=1", "--format=%(refname)").Output()
	if err == nil {
		if ref := strings.TrimSpace(string(out)); ref != "" {
			return ref
		}
	}
	if err := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", sha, "HEAD").Run(); err == nil {
		return "HEAD"
	}
	return ""
}

// parse extracts the records for _type=="issue" lines, keyed by id. Non-issue
// record types (bd may emit others) are ignored. Blank lines are skipped.
// A non-empty input yielding zero issue records is reported via nonZeroLines
// so the caller can fail loud on format drift (bd renames _type or "issue"),
// rather than silently returning "no regressions" — this whole tool exists to
// catch silently-passing gates.
func parse(data []byte) (issues map[string]record, nonZeroLines int, err error) {
	issues = make(map[string]record)
	for i, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		nonZeroLines++
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, 0, fmt.Errorf("line %d: %w", i+1, err)
		}
		if r.Type != "issue" {
			continue
		}
		if r.ID == "" {
			return nil, 0, fmt.Errorf("line %d: issue record with empty id", i+1)
		}
		issues[r.ID] = r
	}
	return issues, nonZeroLines, nil
}

var errPathAbsentAtRef = errors.New("path absent at ref")

func showAtRef(ctx context.Context, ref, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", ref+":"+path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// `git show` prints either "fatal: path '...' does not exist in '<ref>'"
		// (path never existed at that commit) or "fatal: path '...' exists on
		// disk, but not in '<ref>'" (path exists in the worktree but not the
		// commit). Both mean "absent at ref" for our purposes; anything else
		// is bubbled up.
		//
		// What that split does NOT separate: a hex sha git cannot resolve.
		// Measured in a --depth=1 clone, `git show <unknown-40-hex>:<path>`
		// prints those same two messages, so it is classified absent here.
		// "fatal: invalid object name" — which is bubbled up — is what a
		// non-sha ref name prints (`git show nosuchbranch:p`). CI passes a
		// hex sha (ci.yml: `just bd-export-monotonic "$BASE_SHA"`), so the
		// fetch on the line above it is what stands between an unresolvable
		// base and a vacuous pass. See bd gqlc-z6xd.
		msg := stderr.Bytes()
		if bytes.Contains(msg, []byte("does not exist in")) || bytes.Contains(msg, []byte("exists on disk, but not in")) {
			return nil, errPathAbsentAtRef
		}
		return nil, fmt.Errorf("git show %s:%s: %w: %s", ref, path, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
