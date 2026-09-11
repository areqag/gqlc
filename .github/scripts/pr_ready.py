#!/usr/bin/env python3
"""
Answer "are this PR's required checks green at its current head" (bd gqlc-xf0v).

Usage: pr_ready.py ROLLUP_JSON REQUIRED_JSON
       pr_ready.py --self-test
       pr_ready.py --check-required LIVE_CONTEXTS_JSON

ROLLUP_JSON is the array `gh pr view --json statusCheckRollup` yields;
REQUIRED_JSON is the array branch protection yields at
`required_status_checks.contexts`. Both arrive as files so this tool never
touches the network: the just recipe fetches, this tool judges. That split is
what lets --self-test drive the whole matrix offline (the shape
next-doc-ordinal.py's rows use: decision core minus the fetch).

THE REDUCTION, and why it is newest-per-context. statusCheckRollup returns an
entry per workflow RUN, not per context, so a superseded FAILURE never leaves
the array. Judging any entry (the query anybody would write) reads a green PR
as red: measured on PR #1236, tidy FAILURE 13:57:12 beside tidy SUCCESS
13:59:45 at the same head, and on PR #1069, lint/test/codegen-fence CANCELLED
05:39 beside the same three SUCCESS 05:40. Group by name, take max startedAt,
and both PRs read green, which is what they are.

THE OTHER DIRECTION is why SKIPPED on a required context is its own verdict,
not a pass and not a failure. A skipped check run reads as a PASS to branch
protection: measured on PR #1015, four required contexts newest-and-skipped
with mergeStateStatus CLEAN, because lint/test/codegen-fence declare
needs: [actionlint, tidy] and a tidy failure skips all three instead of
failing them. Collapsing skipped into green repeats that false green;
collapsing it into failure sends the author after a regression that does not
exist (the cause is upstream of the skipped job, with a different fix). So the
exit code is one bit — 0 READY, 1 anything else — and the lines say which kind
of not-ready each context is.

READY is fail-closed by construction: it requires a newest entry PRESENT with
conclusion SUCCESS for EVERY required context. A missing context, a pending
run (null conclusion), and any other conclusion all refuse. There is no
empty-required short-circuit either: --check-required and the required-unanimity
row below both refuse an empty set, because zero required contexts would make
every PR READY.

Non-required entries (nightly-alert's perpetual SKIPPED on a pull request) are
not named in any output. A reader who has to discount noise they were never
told about is the defect's starting shape. That exemplar was live-smoke-age
until bd gqlc-ezwae put the AGE arm on pull requests: it runs there now instead
of skipping, and joins the required set, so the fixture below moved to a job
that really does skip on every PR.
"""

import json
import sys


def entry_name(entry):
    # CheckRun carries name; StatusContext carries context.
    return entry.get("name") or entry.get("context") or ""


def entry_result(entry):
    # CheckRun carries conclusion (null while running); StatusContext carries
    # state. Either may be absent on a shape GitHub adds tomorrow.
    return entry.get("conclusion") or entry.get("state") or ""


def entry_stamp(entry):
    # ISO-8601 UTC in both shapes, so lexicographic order is newest-last.
    return entry.get("startedAt") or entry.get("createdAt") or ""


def newest_per_context(rollup):
    """Map each context name to its newest rollup entry."""
    newest = {}
    for entry in rollup:
        name = entry_name(entry)
        if not name:
            continue
        if name not in newest or entry_stamp(entry) >= entry_stamp(newest[name]):
            newest[name] = entry
    return newest


def verdict(rollup, required):
    """Judge newest-per-context against the required set.

    Returns (ready, rows) where each row is (context, kind, detail) and kind
    is one of ok, skipped, failed, missing. ready is True only when every row
    is ok.
    """
    newest = newest_per_context(rollup)
    rows = []
    for context in required:
        entry = newest.get(context)
        if entry is None:
            rows.append((context, "missing", "no entry in rollup"))
            continue
        result = entry_result(entry)
        if result == "SUCCESS":
            rows.append((context, "ok", result))
        elif result == "SKIPPED":
            rows.append((context, "skipped", result))
        else:
            rows.append((context, "failed", result or "pending"))
    ready = all(kind == "ok" for _, kind, _ in rows)
    return ready, rows


def report(ready, rows, out):
    n_ok = sum(1 for _, kind, _ in rows if kind == "ok")
    head = "READY" if ready else "NOT-READY"
    out.append(f"{head}: {n_ok}/{len(rows)} required contexts SUCCESS")
    for context, kind, detail in rows:
        out.append(f"  {kind.upper():8} {context} {detail}")
    return ready


def load_json(path):
    with open(path, encoding="utf-8") as handle:
        return json.load(handle)


# The required set is spelled in each fixture rather than once here, so no
# constant in this file can drift from what the rows assert — and the
# unanimity row below holds the fixtures to each other while --check-required
# holds them to the live protection config.
def fixture_superseded_failure():
    """PR #1236 / #1069 shape: stale FAILURE/CANCELLED beside newer SUCCESS."""
    required = [
        "lint",
        "test",
        "tidy",
        "actionlint",
        "govulncheck",
        "live-smoke",
        "live-smoke-age",
        "codegen-fence",
    ]
    rollup = [
        {"name": "tidy", "conclusion": "FAILURE", "startedAt": "2026-08-22T13:57:12Z"},
        {"name": "tidy", "conclusion": "SUCCESS", "startedAt": "2026-08-22T13:59:45Z"},
        {"name": "lint", "conclusion": "CANCELLED", "startedAt": "2026-08-22T05:39:02Z"},
        {"name": "lint", "conclusion": "SUCCESS", "startedAt": "2026-08-22T05:40:01Z"},
        {"name": "test", "conclusion": "CANCELLED", "startedAt": "2026-08-22T05:39:02Z"},
        {"name": "test", "conclusion": "SUCCESS", "startedAt": "2026-08-22T05:40:01Z"},
        {
            "name": "codegen-fence",
            "conclusion": "CANCELLED",
            "startedAt": "2026-08-22T05:39:02Z",
        },
        {
            "name": "codegen-fence",
            "conclusion": "SUCCESS",
            "startedAt": "2026-08-22T05:40:01Z",
        },
        {"name": "actionlint", "conclusion": "SUCCESS", "startedAt": "2026-08-22T05:40:02Z"},
        {"name": "govulncheck", "conclusion": "SUCCESS", "startedAt": "2026-08-22T05:40:03Z"},
        {"name": "live-smoke", "conclusion": "SUCCESS", "startedAt": "2026-08-22T05:40:04Z"},
        {
            "name": "live-smoke-age",
            "conclusion": "SUCCESS",
            "startedAt": "2026-08-22T05:40:04Z",
        },
        # Non-required noise: silent in every output, present so the row
        # proves it. nightly-alert skips on every pull request by its own
        # `if:` (schedule only), which is what keeps this entry faithful to a
        # real PR rollup rather than to a job that has since started running.
        {"name": "nightly-alert", "conclusion": "SKIPPED", "startedAt": "2026-08-22T05:40:05Z"},
    ]
    return required, rollup


def fixture_required_skipped():
    """PR #1015 shape: required contexts newest-and-skipped after a tidy failure."""
    required = [
        "lint",
        "test",
        "tidy",
        "actionlint",
        "govulncheck",
        "live-smoke",
        "live-smoke-age",
        "codegen-fence",
    ]
    rollup = [
        {"name": "tidy", "conclusion": "FAILURE", "startedAt": "2026-08-22T06:10:00Z"},
        {"name": "lint", "conclusion": "SKIPPED", "startedAt": "2026-08-22T06:11:00Z"},
        {"name": "test", "conclusion": "SKIPPED", "startedAt": "2026-08-22T06:11:00Z"},
        {"name": "codegen-fence", "conclusion": "SKIPPED", "startedAt": "2026-08-22T06:11:00Z"},
        {"name": "actionlint", "conclusion": "SUCCESS", "startedAt": "2026-08-22T06:09:00Z"},
        {"name": "govulncheck", "conclusion": "SUCCESS", "startedAt": "2026-08-22T06:09:00Z"},
        {"name": "live-smoke", "conclusion": "SUCCESS", "startedAt": "2026-08-22T06:09:00Z"},
        {
            "name": "live-smoke-age",
            "conclusion": "SUCCESS",
            "startedAt": "2026-08-22T06:09:00Z",
        },
    ]
    return required, rollup


def naive_verdict(rollup):
    """The query anybody would write: red on any non-SUCCESS entry anywhere."""
    return not any(entry_result(entry) != "SUCCESS" for entry in rollup)


def skipped_as_pass_verdict(rollup, required):
    """The mergeStateStatus reading: skipped counts as green."""
    newest = newest_per_context(rollup)
    return all(
        newest.get(context) is not None
        and entry_result(newest[context]) in ("SUCCESS", "SKIPPED")
        for context in required
    )


def self_test():
    failures = []

    def check(name, condition, detail=""):
        if condition:
            print(f"self-test ok: {name}")
        else:
            failures.append(name)
            print(f"self-test FAILED: {name}\n  {detail}")

    required_a, rollup_a = fixture_superseded_failure()
    required_b, rollup_b = fixture_required_skipped()

    ready_a, rows_a = verdict(rollup_a, required_a)
    check("superseded-failure-is-ready", ready_a, f"rows: {rows_a}")

    ready_b, rows_b = verdict(rollup_b, required_b)
    kinds_b = sorted(kind for _, kind, _ in rows_b)
    check(
        "required-skipped-is-not-ready",
        not ready_b and kinds_b.count("skipped") == 3 and kinds_b.count("failed") == 1,
        f"rows: {rows_b}",
    )

    missing_rollup = [e for e in rollup_a if entry_name(e) != "govulncheck"]
    ready_m, rows_m = verdict(missing_rollup, required_a)
    check(
        "missing-context-is-not-ready",
        not ready_m and ("govulncheck", "missing", "no entry in rollup") in rows_m,
        f"rows: {rows_m}",
    )

    pending_rollup = [
        dict(e, conclusion=None) if entry_name(e) == "lint" and entry_result(e) == "SUCCESS" else e
        for e in rollup_a
    ]
    ready_p, rows_p = verdict(pending_rollup, required_a)
    check(
        "pending-run-is-not-ready",
        not ready_p and ("lint", "failed", "pending") in rows_p,
        f"rows: {rows_p}",
    )

    noise_names = [entry_name(e) for e in rollup_a if entry_name(e) == "nightly-alert"]
    out_a = []
    report(ready_a, rows_a, out_a)
    check(
        "non-required-contexts-stay-silent",
        len(noise_names) == 1 and not any("nightly-alert" in line for line in out_a),
        f"report: {out_a}",
    )

    # The falsifiers, in band. Each wrong reduction is driven over the fixture
    # that exposes it and must MISJUDGE it there; a fixture the naive query
    # also gets right witnesses nothing about why the grouping exists.
    check(
        "naive-query-false-reds-on-superseded",
        not naive_verdict(rollup_a),
        "the naive reduction called the green fixture ready",
    )
    # The skipped-as-pass reading needs its cause removed to misjudge: with
    # tidy FAILURE present it refuses too, correctly by accident. Flip tidy to
    # SUCCESS and only the SKIPPED trio remains — the PR #1015 end state, a
    # mergeable PR whose lint and test never executed at that SHA.
    skipped_only = [
        dict(e, conclusion="SUCCESS") if entry_name(e) == "tidy" else e for e in rollup_b
    ]
    ready_s, rows_s = verdict(skipped_only, required_b)
    check(
        "skipped-without-failure-is-still-not-ready",
        not ready_s and all(kind in ("ok", "skipped") for _, kind, _ in rows_s),
        f"rows: {rows_s}",
    )
    check(
        "skipped-as-pass-false-greens-on-skipped",
        skipped_as_pass_verdict(skipped_only, required_b),
        "the skipped-as-pass reduction called the skipped fixture not-ready",
    )

    check(
        "fixtures-agree-on-required-set",
        set(required_a) == set(required_b) and len(required_a) == 8,
        f"{len(required_a)} {required_a} vs {len(required_b)} {required_b}",
    )

    if failures:
        print(f"self-test: {len(failures)} row(s) FAILED", file=sys.stderr)
        return 1
    print("self-test: all rows passed")
    return 0


def check_required(live):
    required_a, _ = fixture_superseded_failure()
    required_b, _ = fixture_required_skipped()
    if set(required_a) != set(required_b):
        print(
            "error: the fixtures disagree with each other on the required set, "
            "so there is nothing coherent to hold the live config to.",
            file=sys.stderr,
        )
        return 1
    wanted = set(required_a)
    if not live:
        print(
            "error: branch protection names no required contexts, so every PR "
            "would read READY. Refusing rather than judging against nothing.",
            file=sys.stderr,
        )
        return 1
    live_set = set(live)
    if live_set == wanted:
        print(f"required contexts match protection ({len(wanted)}): {sorted(wanted)}")
        return 0
    print("error: branch protection's required contexts moved out from under", file=sys.stderr)
    print(f"       the fixtures (bd gqlc-xf0v). Fixtures: {sorted(wanted)}", file=sys.stderr)
    print(f"       Live:       {sorted(live_set)}", file=sys.stderr)
    added = sorted(live_set - wanted)
    removed = sorted(wanted - live_set)
    if added:
        print(f"       New contexts a stale pr-ready would silently pass: {added}", file=sys.stderr)
    if removed:
        print(f"       Contexts the fixtures still demand: {removed}", file=sys.stderr)
    print("       Update the fixtures in pr_ready.py to match.", file=sys.stderr)
    return 1


def main(argv):
    if argv == ["--self-test"]:
        return self_test()
    if len(argv) == 2 and argv[0] == "--check-required":
        return check_required(load_json(argv[1]))
    if len(argv) != 2:
        print(
            f"usage: {sys.argv[0]} ROLLUP_JSON REQUIRED_JSON | --self-test "
            "| --check-required LIVE_CONTEXTS_JSON",
            file=sys.stderr,
        )
        return 2
    rollup = load_json(argv[0])
    required = load_json(argv[1])
    if not required:
        print(
            "error: no required contexts given, so every PR would read READY. "
            "Refusing rather than judging against nothing.",
            file=sys.stderr,
        )
        return 1
    ready, rows = verdict(rollup, required)
    out = []
    report(ready, rows, out)
    print("\n".join(out))
    return 0 if ready else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
