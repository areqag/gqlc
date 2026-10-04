#!/usr/bin/env python3
"""
Re-ask the ordinal question for every open PR when master moves (bd gqlc-4plwf).

Usage: check-open-pr-ordinals.py DIR [DIR ...]
       check-open-pr-ordinals.py --self-test

THE HALF check-doc-ordinals.py CANNOT SEE. That gate reads a TREE, and on a pull
request the tree it reads is the merge ref, which GitHub assembles when the
author last pushed. The collision this file exists for arrives afterwards, from
the OTHER side: master takes the ordinal while the PR sits still. Nothing about
the PR changes, so no `pull_request` event fires, so the merge ref is never
rebuilt and the green check keeps standing for a number that is now taken.

Measured window, 2026-09-02 (bd gqlc-4plwf): between 16:49 and 20:12 a PR held
an ordinal that master had already adopted, and every gate the repository has
was green the whole time.

WHY THIS SHAPE. No event exists for "your base moved" -- except that the base
moves BY a master push, which is an event we already receive. So the master
push re-asks the question for every PR still open and delivers the answer as a
commit status on each PR's head SHA, where its checks list already is. The
collision reads as a red X within minutes, with no author push and no cron.
Alternatives and why they lost are in gqlc-4plwf's design note.

WHAT IS COMPARED, and this is the subtle part. Per PR, the files the PR ADDS
(status added / renamed / copied) under an enrolled directory, read from
`repos/{owner}/{repo}/pulls/{n}/files` -- the PR's own three-dot diff, merge
base to head. Not the union of the two trees: a stale head still carrying an
old master file whose ordinal master has since renumbered-and-reused collides
in a union, but is absent from the tree the squash merge actually produces, so
a union would report a collision that cannot happen. `renamed` is included
because a renumber IS a rename and claims its new ordinal; the endpoint names
a rename by its NEW path in `filename` (old one in `previous_filename`).

The verdict itself is delegated to check-doc-ordinals.py rather than
reimplemented, by materializing a scratch directory of EMPTY files -- master's
filenames plus the PR's additions, deduped by name. That reuses one copy of the
ordinal regex, the collision logic, the mixed-directory refusal and the remedy
text; two copies of a series convention drift silently. Empty files are enough
because that checker's offenders() stats filenames and never opens one.

THIS RUN GOES GREEN ON A COLLISION IT FINDS. The defect belongs to the PR and
is reported on the PR; master is fine, and a collision that actually lands on
master is caught by the unconditional tree check on ci.yml's push arm. What
DOES fail this run is a broken instrument: an API call that errors, or a PR
whose file list reaches the cap below. A refused PR is posted an `error`
status on its own head and the loop goes on, so one unreadable PR costs no
other PR its verdict; the run reds once every PR has been examined.

WHY pulls/files AND NOT THE COMPARE IT REPLACED (bd gqlc-rs3j). Until
2026-10-04 this read `repos/{owner}/{repo}/compare/{master}...{head}`. On
2026-09-02 that endpoint returned exactly 300 entries in `files` for a range
whose `total_commits` was 850, sent NO Link header, and ignored pagination
(`?per_page=100&page=2` returned 0), so the 300 was a silent truncation with no
rest to ask for. PR #2978, a golden regeneration touching 303 files, hit it on
2026-10-04 and was refused -- correctly, but the refusal exited the loop and
silenced every PR after it. pulls/files paginates instead: for #2978 at
5c257959 it sent `Link: rel="next"` and returned pages of 100, 100, 100 and 3.

The swap is measured, not assumed, 2026-10-04 at master 43422c03, against
`git diff --name-status --find-renames $(git merge-base origin/master H) H`
for each head H: the full (status, filename, previous_filename) set from
pulls/files equalled git's for open PRs #2990, #2985, #2978 and merged #2987,
#2986, #2983, #2981, #2975, #2974; the compare equalled both for every one of
those except #2978, where it stopped at 300 of 303. Renames were checked on
merged #2844 and #2592, one each, where `filename` was git's new path. That is
eleven PRs and three renames, a floor and not a census; a PR whose two
endpoints disagree on the claiming set falsifies it.

Two known differences, neither able to turn a collision green. (1) The
endpoint diffs against the PR's own BASE branch, where the compare named
master: a PR stacked on another PR lists only its own changes, and the
parent's additions are checked on the parent. Every PR measured above targets
master. (2) Its merge base is computed against GitHub's `base.sha`, which it
refreshes lazily, not against the just-pushed GITHUB_SHA. For a head that has
merged or rebased onto master past a stale `base.sha`, it can list master's
own documents as additions. Those dedupe by name against master's listing,
unless master has since renumbered one and reused its ordinal: then the stale
extra collides, which is a FALSE FAILURE, never a lost one. Unwitnessed here:
on 2026-10-04 a review re-measured five open PRs (#2999 #2998 #2996 #2994
#2990), and merge-base(H, base.sha) equalled merge-base(H, origin/master) on
all five, with pulls/files again equal to git's set. With the eleven above,
that is sixteen PRs, a floor. None of the latest 400 PR heads held a merge
commit.

A third difference is closed in code. The compare named both SHAs, but
pulls/files names neither and answers for whatever head the PR has when it
is read. So after reading the files, the head is read again
(current_head()); if it moved since the list, no status is posted.

THE pulls/files CAP. GitHub's REST documentation gives that endpoint a
3000-file maximum. That is documented, not measured -- no PR here has come
near it -- and what the endpoint does past it is unwitnessed. So a PR at
PULL_FILES_CAP is refused loudly rather than under-checked: under-checking
here means a green status on a question nobody answered, which is the exact
failure this file exists to remove.
"""

import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

# The context these statuses are posted under. It is deliberately NOT a required
# check: required demands a green status on every head SHA at every event, so a
# PR opened after the last master push would wait forever on a context nobody
# will post. The acceptance for this bead asks for RED, not BLOCKED.
CONTEXT = "ordinal-recheck"

# GitHub truncates a commit-status description past 140 characters, so a
# description assembled from the checker's own words is cut here rather than by
# the API, which would drop the remedy silently mid-word.
DESCRIPTION_LIMIT = 140

# The most files the pull request files endpoint will list, by GitHub's REST
# documentation. Documented, not measured: no PR here has come near it. At it,
# the list is refused rather than trusted, as the compare's 300 used to be.
PULL_FILES_CAP = 3000

# A file the PR ADDS under an enrolled directory claims an ordinal. A modified
# or deleted file does not: modifying 0012 leaves 0012 claimed once, and
# deleting it frees the number rather than taking it.
CLAIMING_STATUSES = frozenset({"added", "renamed", "copied"})

CHECKER = Path(__file__).resolve().parent / "check-doc-ordinals.py"


class Refused(Exception):
    """This PR's question cannot be fully asked; no verdict may be posted on it.

    An exception rather than an exit because the refusal belongs to ONE PR. The
    caller reports it on that PR's head and moves on to the next: exiting here
    left every PR listed after it with no verdict at all (bd gqlc-rs3j).
    """


def run(argv, check=True):
    """Run a command, returning it complete. Failure is fatal by default.

    No `|| true` anywhere in this file: an instrument that cannot read its
    input has cleared nothing, and a swallowed API error would leave every PR
    silently unchecked while the run stayed green -- which is the shape of the
    defect this gate exists to catch, reproduced in the gate itself.
    """
    result = subprocess.run(argv, capture_output=True, text=True)
    if check and result.returncode != 0:
        # Name the endpoint, not a fixed slice: for a POST the third token
        # is the verb flag ("gh api -X"), which says nothing about what was
        # being posted or where.
        endpoint = next((a for a in argv if a.startswith("repos/")), None)
        if endpoint is not None:
            prefix = f"{' '.join(argv[:2])} {endpoint}"
        else:
            prefix = " ".join(argv)
        sys.exit(
            f"error: {prefix} failed (rc={result.returncode}):\n"
            f"{result.stderr.strip()}"
        )
    return result


def verdict(directory, master_names, added_names):
    """Decide one series for one PR, as (ok, description).

    The decision core, kept free of the network so --self-test can drive it
    with a reconstructed window. `directory` is named only so the delegated
    checker's messages read as they do everywhere else; nothing is read from it.
    """
    names = sorted(set(master_names) | set(added_names))
    with tempfile.TemporaryDirectory() as scratch:
        series = Path(scratch) / Path(directory).name
        series.mkdir()
        for name in names:
            # Empty is sufficient: the delegated checker stats filenames and
            # never opens a file. Asserted by its own docstring and by reading
            # offenders(); if that ever stops being true this materialization
            # is what has to change, not the call site.
            (series / name).touch()
        result = run([sys.executable, str(CHECKER), str(series)], check=False)

    if result.returncode == 0:
        return True, ""

    # The checker's first error line already names the ordinal and the count.
    # Reusing its sentence keeps one wording for one defect wherever it is
    # reported; a description written here would drift from the one the author
    # sees in CI.
    first = next(
        (line for line in result.stderr.splitlines() if line.startswith("error:")),
        "",
    )
    detail = first[len("error:") :].strip().rstrip(":")
    detail = detail.replace(str(series), directory)
    return False, detail[:DESCRIPTION_LIMIT]


def claimed_by(files, directories):
    """The enrolled files a PR's file entries claim, as {directory: [name, ...]}.

    The second decision core, kept free of the network for the reason verdict()
    is: which entry claims an ordinal is a judgement, and a test that can only
    see the final verdict cannot see it being made. Refuses at the cap here
    rather than in the caller because the cap is a property of this list.
    """
    if len(files) >= PULL_FILES_CAP:
        # The first sentence is the status description, so it stands alone
        # inside DESCRIPTION_LIMIT; the rest reaches the run log.
        raise Refused(
            f"{len(files)} changed files reach the API's {PULL_FILES_CAP}-file cap. "
            "Past it the list is cut with nothing in the response to say so, and "
            "a truncated list is indistinguishable from a complete one. Refusing "
            "to post a verdict on a question that was not fully asked."
        )

    claimed = {directory: [] for directory in directories}
    for entry in files:
        if entry.get("status") not in CLAIMING_STATUSES:
            continue
        path = Path(entry["filename"])
        for directory in directories:
            # Equality, not a prefix test: an enrolled series is one flat
            # directory, and a document in a subdirectory of it is not part of
            # the numbering these two gates share.
            if path.parent == Path(directory):
                claimed[directory].append(path.name)
    return claimed


def added_under(repo, number, directories):
    """The enrolled files PR `number` adds, as {directory: [basename, ...]}."""
    pages = json.loads(
        run(
            [
                "gh",
                "api",
                "--paginate",
                # The page shape is pinned rather than left to gh: without it
                # gh 2.102.0 merges array pages into one array, and its own
                # help text says each page is printed separately.
                "--slurp",
                f"repos/{repo}/pulls/{number}/files?per_page=100",
            ]
        ).stdout
    )
    return claimed_by([entry for page in pages for entry in page], directories)


def current_head(repo, number):
    """PR `number`'s head SHA as GitHub reports it now."""
    return json.loads(run(["gh", "api", f"repos/{repo}/pulls/{number}"]).stdout)["head"]["sha"]


def post_status(repo, head_sha, state, description, target_url):
    run(
        [
            "gh",
            "api",
            "-X",
            "POST",
            f"repos/{repo}/statuses/{head_sha}",
            "-f",
            f"state={state}",
            "-f",
            f"context={CONTEXT}",
            "-f",
            f"description={description}",
            "-f",
            f"target_url={target_url}",
        ]
    )


def check_open_prs(directories):
    repo = os.environ["GITHUB_REPOSITORY"]
    base_sha = os.environ["GITHUB_SHA"]
    target_url = (
        f"{os.environ['GITHUB_SERVER_URL']}/{repo}/actions/runs"
        f"/{os.environ['GITHUB_RUN_ID']}"
    )

    master = {
        directory: [path.name for path in Path(directory).iterdir() if path.is_file()]
        for directory in directories
    }

    prs = json.loads(
        run(
            [
                "gh",
                "pr",
                "list",
                "--state",
                "open",
                "--limit",
                "1000",
                "--json",
                "number,headRefOid",
            ]
        ).stdout
    )

    refused = []
    for pr in prs:
        head = pr["headRefOid"]
        try:
            added = added_under(repo, pr["number"], directories)
        except Refused as refusal:
            # An error, not a failure: nothing is known to collide, but nothing
            # is known not to, and a stale green on this head must not stand.
            description = f"not checked: {refusal}"[:DESCRIPTION_LIMIT]
            post_status(repo, head, "error", description, target_url)
            print(f"error: PR #{pr['number']}: {refusal}", file=sys.stderr)
            refused.append(pr["number"])
            continue
        # pulls/files names no SHA, so the files are pinned to the listed head
        # by asking again AFTER reading them. On a mismatch nothing is posted:
        # the push that moved the head rebuilt its merge ref, which ci.yml's
        # tree check reads, and the next master push asks this again.
        now = current_head(repo, pr["number"])
        if now != head:
            print(
                f"PR #{pr['number']}: head moved from {head} to {now} during the "
                "read, no status posted"
            )
            continue
        if not any(added.values()):
            # Silent by design. Most PRs touch no enrolled series, and a
            # success status on every one of them would put a context on every
            # PR in the repository to say nothing happened.
            print(f"PR #{pr['number']}: adds no enrolled document, no status posted")
            continue

        failures = []
        for directory in directories:
            if not added[directory]:
                continue
            ok, description = verdict(directory, master[directory], added[directory])
            if not ok:
                failures.append(description)

        if failures:
            post_status(repo, head, "failure", failures[0], target_url)
            print(f"PR #{pr['number']}: FAILURE posted on {head}: {failures[0]}")
        else:
            description = f"no ordinal taken by base @{base_sha[:7]}"
            post_status(repo, head, "success", description, target_url)
            print(f"PR #{pr['number']}: success posted on {head}")

    if refused:
        # Red AFTER the loop: the run is a broken instrument for these PRs, and
        # every other PR has already received its verdict.
        listed = ", ".join(f"#{number}" for number in refused)
        print(f"error: no verdict could be reached for PR {listed}", file=sys.stderr)
        return 1
    return 0


def self_test_claimed_by():
    """Drive the status filter: which file entry claims an ordinal.

    Split out from the verdict rows because a mutation narrowing
    CLAIMING_STATUSES survives every one of them -- the verdict never sees a
    file the filter dropped, so it cannot report the collision it hides.
    """
    enrolled = ["docs/adr"]
    doc = "0012-something.md"
    rows = [
        ("an added document claims its ordinal", "added", f"docs/adr/{doc}", True),
        ("a renumber arrives as a RENAME and claims", "renamed", f"docs/adr/{doc}", True),
        ("a copy claims the ordinal it lands on", "copied", f"docs/adr/{doc}", True),
        ("modifying 0012 leaves it claimed once, not twice", "modified", f"docs/adr/{doc}", False),
        ("deleting 0012 frees the number, it does not take it", "removed", f"docs/adr/{doc}", False),
        (
            "a directory outside the enrolled series claims nothing",
            "added",
            f"docs/postmortems/{doc}",
            False,
        ),
        (
            "a document one level below an enrolled directory is outside its numbering",
            "added",
            f"docs/adr/sub/{doc}",
            False,
        ),
    ]

    failed = False
    for name, status, filename, want_claimed in rows:
        claimed = claimed_by([{"status": status, "filename": filename}], enrolled)
        got_claimed = any(claimed.values())
        if got_claimed != want_claimed:
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  wanted claimed={want_claimed}, got {claimed!r}",
                file=sys.stderr,
            )
            continue
        print(f"self-test ok: {name}")

    # The cap refusal, driven rather than trusted. It is the one place this file
    # chooses to fail loudly instead of answering, so a mutation that deletes it
    # buys a green status on a question that was never fully asked -- the exact
    # shape of the defect this gate exists to remove.
    name = "a file list at the API's cap is refused, not under-checked"
    at_cap = [
        {"status": "added", "filename": f"docs/adr/{i:04d}-x.md"}
        for i in range(PULL_FILES_CAP)
    ]
    try:
        claimed_by(at_cap, ["docs/adr"])
    except Refused as refusal:
        if str(PULL_FILES_CAP) not in str(refusal):
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  the refusal does not name the cap: {refusal!s:.120}",
                file=sys.stderr,
            )
        else:
            print(f"self-test ok: {name}")
    else:
        failed = True
        print(
            f"self-test FAILED: {name}\n"
            f"  {PULL_FILES_CAP} entries were answered rather than refused",
            file=sys.stderr,
        )

    return failed


def self_test_gh_failure():
    """A failed `gh` is a broken instrument, not an empty answer (bd gqlc-pju6h).

    Measured shape: `gh` prints its error body (`{"message": "Not Found"}`)
    to STDOUT and exits non-zero. That body parses as a successful lookup
    with no files, so a call site reading it with check=False reports "adds
    no enrolled document" -- a silent green in a gate whose whole job is to
    go red. This row puts a fake `gh` first on PATH and asserts the file
    read fails loudly instead.
    """
    name = "a gh failure whose error body goes to stdout fails loudly, not green"
    with tempfile.TemporaryDirectory() as tmp:
        fake = Path(tmp) / "gh"
        fake.write_text('#!/bin/sh\nprintf \'{"message": "Not Found"}\'\nexit 1\n')
        fake.chmod(0o755)
        old_path = os.environ.get("PATH", "")
        os.environ["PATH"] = f"{tmp}{os.pathsep}{old_path}"
        try:
            try:
                added_under("owner/repo", 1, ["docs/adr"])
            except SystemExit as failure:
                if failure.code == 0:
                    print(
                        f"self-test FAILED: {name}\n"
                        "  the failing gh exited the script with code 0",
                        file=sys.stderr,
                    )
                    return True
                print(f"self-test ok: {name}")
                return False
        finally:
            os.environ["PATH"] = old_path
    print(
        f"self-test FAILED: {name}\n"
        "  a failing gh was read as an empty file list, which the caller "
        'reports as "adds no enrolled document"',
        file=sys.stderr,
    )
    return True


# A stand-in for `gh`, put first on PATH by the rows that drive the network
# path. It serves a world read from WORLD and appends every status POST to
# POSTED as one JSON line. Anything it does not model exits 99, so a call the
# script newly makes is a loud row failure rather than an answer invented here.
FAKE_GH = r'''
import json, os, sys, urllib.parse
WORLD, POSTED = {world!r}, {posted!r}
args = sys.argv[1:]
world = json.load(open(WORLD))
if args[:2] == ["pr", "list"]:
    print(json.dumps(world["prs"]))
    sys.exit(0)
if args[:1] == ["api"] and "-X" in args:
    fields = dict(a.split("=", 1) for a in args if "=" in a and not a.startswith("repos/"))
    fields["endpoint"] = next(a for a in args if a.startswith("repos/"))
    with open(POSTED, "a") as out:
        out.write(json.dumps(fields) + "\n")
    print("{{}}")
    sys.exit(0)
endpoint = next((a for a in args if a.startswith("repos/")), "")
path, _, query = endpoint.partition("?")
parts = path.split("/")
if args[:1] == ["api"] and len(parts) == 5 and parts[3] == "pulls" and not query:
    # A head in "heads" moves only once its files have been read, so a check
    # made before the read sees the listed head and cannot pass the row.
    listed = next(pr["headRefOid"] for pr in world["prs"] if str(pr["number"]) == parts[4])
    moved = os.path.exists(WORLD + ".read." + parts[4])
    sha = world.get("heads", {{}}).get(parts[4], listed) if moved else listed
    print(json.dumps({{"head": {{"sha": sha}}}}))
    sys.exit(0)
if args[:1] == ["api"] and len(parts) == 6 and parts[3] == "pulls" and parts[5] == "files":
    files = world["files"][parts[4]]
    open(WORLD + ".read." + parts[4], "w").close()
    per_page = int(urllib.parse.parse_qs(query).get("per_page", ["30"])[0])
    pages = [files[i:i + per_page] for i in range(0, len(files), per_page)] or [[]]
    # Measured against gh 2.102.0, 2026-10-04: --slurp alone is refused;
    # without --paginate only the first page; with it, one merged array; with
    # --slurp too, the pages.
    if "--slurp" in args and "--paginate" not in args:
        sys.stderr.write("`--paginate` required when passing `--slurp`\n")
        sys.exit(1)
    if "--paginate" not in args:
        print(json.dumps(pages[0]))
    elif "--slurp" not in args:
        print(json.dumps(files))
    else:
        print(json.dumps(pages))
    sys.exit(0)
sys.stderr.write("fake gh: unmodelled call: %r\n" % (args,))
sys.exit(99)
'''


def run_against_fake_gh(world, directories, master_files):
    """Drive check_open_prs() over `world` through FAKE_GH, as (rc, posts, out).

    `posts` is {head_sha: status fields} for every status POSTed, and `out` the
    lines printed, so a row can assert both what was posted and that a PR was
    reached at all. Runs in a scratch cwd holding `master_files` per enrolled
    directory, which is where check_open_prs() reads master's names from.
    """
    old_path, old_cwd = os.environ.get("PATH", ""), os.getcwd()
    old_env = {k: os.environ.get(k) for k in ("GITHUB_REPOSITORY", "GITHUB_SHA",
                                              "GITHUB_SERVER_URL", "GITHUB_RUN_ID")}
    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        (tmp / "world.json").write_text(json.dumps(world))
        posted = tmp / "posted.jsonl"
        posted.touch()
        bin_dir = tmp / "bin"
        bin_dir.mkdir()
        fake = bin_dir / "gh"
        fake.write_text(
            f"#!{sys.executable}\n"
            + FAKE_GH.format(world=str(tmp / "world.json"), posted=str(posted))
        )
        fake.chmod(0o755)
        tree = tmp / "tree"
        for directory in directories:
            (tree / directory).mkdir(parents=True)
            for name in master_files.get(directory, []):
                (tree / directory / name).touch()

        os.environ.update(
            PATH=f"{bin_dir}{os.pathsep}{old_path}",
            GITHUB_REPOSITORY="owner/repo",
            GITHUB_SHA="b" * 40,
            GITHUB_SERVER_URL="https://github.invalid",
            GITHUB_RUN_ID="1",
        )
        os.chdir(tree)
        out = io.StringIO()
        try:
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(out):
                try:
                    rc = check_open_prs(directories)
                except SystemExit as stop:
                    rc = f"SystemExit({stop.code!s:.200})"
                except Exception as crash:
                    rc = f"{type(crash).__name__}({crash!s:.200})"
        finally:
            os.chdir(old_cwd)
            os.environ["PATH"] = old_path
            for k, v in old_env.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        posts = {}
        for line in posted.read_text().splitlines():
            fields = json.loads(line)
            posts[fields["endpoint"].rsplit("/", 1)[1]] = fields
    return rc, posts, out.getvalue()


def self_test_refusal_does_not_silence_later_prs():
    """One PR the instrument cannot read must not cost every PR after it.

    Observed 2026-10-04 (bd gqlc-rs3j): PR #2978 legitimately changed 303
    files, its read was refused at the cap, and the refusal exited the loop,
    so every open PR listed after it got no verdict on three master pushes.
    The world here is that order: a quiet PR, a colliding PR, the unreadable
    one, then a PR whose ordinal master has taken -- the one whose RED the
    abort used to eat. The colliding PR BEFORE the refused one is what pins
    the refusal's `continue`: without it the refused PR falls through holding
    the previous PR's additions, and a quiet predecessor hides that.
    """
    name = "a refused PR in the middle does not silence the PRs after it"
    adr = "docs/adr"
    taken = "0012-an-ordinal-master-already-holds.md"
    quiet, capped, colliding, earlier = "1" * 40, "2" * 40, "3" * 40, "5" * 40
    world = {
        "prs": [
            {"number": 1, "headRefOid": quiet},
            {"number": 5, "headRefOid": earlier},
            {"number": 2, "headRefOid": capped},
            {"number": 3, "headRefOid": colliding},
        ],
        "files": {
            "1": [{"status": "modified", "filename": "README.md"}],
            "5": [{"status": "added", "filename": f"{adr}/0012-an-earlier-claim.md"}],
            "2": [
                {"status": "added", "filename": f"test/data/g{i:04d}.golden"}
                for i in range(PULL_FILES_CAP)
            ],
            "3": [{"status": "added", "filename": f"{adr}/0012-a-different-document.md"}],
        },
    }
    rc, posts, out = run_against_fake_gh(world, [adr], {adr: [taken]})

    problems = []
    if rc != 1:
        problems.append(f"wanted rc=1 for the refused PR, got {rc!r}")
    if quiet in posts:
        problems.append(f"the quiet PR was posted a status: {posts[quiet]!r}")
    if posts.get(capped, {}).get("state") != "error":
        problems.append(f"the refused PR was not posted an error: {posts.get(capped)!r}")
    elif str(PULL_FILES_CAP) not in posts[capped]["description"]:
        problems.append(f"the error does not name the cap: {posts[capped]!r}")
    elif len(posts[capped]["description"]) > DESCRIPTION_LIMIT:
        problems.append(f"the error is past {DESCRIPTION_LIMIT} chars: {posts[capped]!r}")
    if posts.get(earlier, {}).get("state") != "failure":
        problems.append(f"the PR before it got no failure: {posts.get(earlier)!r}")
    if posts.get(colliding, {}).get("state") != "failure":
        problems.append(f"the PR after it got no failure: {posts.get(colliding)!r}")
    elif "0012" not in posts[colliding]["description"]:
        problems.append(f"the failure does not name 0012: {posts[colliding]!r}")
    if problems:
        print(f"self-test FAILED: {name}", file=sys.stderr)
        for problem in problems:
            print(f"  {problem}", file=sys.stderr)
        print("  output was:\n    " + out.replace("\n", "\n    "), file=sys.stderr)
        return True
    print(f"self-test ok: {name}")
    return False


def self_test_read_past_one_page():
    """A PR wider than one page is read on every page (bd gqlc-rs3j).

    PR #2978 changed 303 files; the compare it was read through returns 300
    and no way to ask for the rest. Two 303-file PRs here, each with its one
    enrolled addition at an opposite end: the last entry (past the compare's
    cap and three 100-entry pages) and the first. A read that keeps only the
    first page loses the one, a read that keeps only the last loses the
    other, and either reports "adds no enrolled document" for it.
    """
    name = "a PR wider than one page is read on every page"
    adr = "docs/adr"
    last, first = "4" * 40, "8" * 40
    padding = [{"status": "modified", "filename": f"test/data/g{i:04d}.golden"} for i in range(302)]
    claim = {"status": "added", "filename": f"{adr}/0012-a-claim-on-one-page.md"}
    world = {
        "prs": [{"number": 4, "headRefOid": last}, {"number": 8, "headRefOid": first}],
        "files": {"4": padding + [claim], "8": [claim] + padding},
    }
    rc, posts, out = run_against_fake_gh(
        world, [adr], {adr: ["0012-an-ordinal-master-already-holds.md"]}
    )
    states = {head: posts.get(head, {}).get("state") for head in (last, first)}
    if rc != 0 or set(states.values()) != {"failure"}:
        print(
            f"self-test FAILED: {name}\n"
            f"  wanted rc=0 and a failure on both heads, got rc={rc!r}, "
            f"states {states!r}\n"
            "  output was:\n    " + out.replace("\n", "\n    "),
            file=sys.stderr,
        )
        return True
    print(f"self-test ok: {name}")
    return False


def self_test_head_moved_during_read():
    """Files read from one head are not posted onto another (bd gqlc-rs3j).

    The PR list names each head SHA, but pulls/{n}/files names none: it
    answers for whatever the head is when it is read. A push between the two
    reads, or GitHub's own diff recompute after one, would attach a verdict
    from one head's files to the other head. Here the head moves from 6... to
    7... while the files are read, and the files collide; nothing may be
    posted on either SHA.
    """
    name = "a head that moved between the list and the read gets no verdict"
    adr = "docs/adr"
    listed, moved = "6" * 40, "7" * 40
    world = {
        "prs": [{"number": 6, "headRefOid": listed}],
        "heads": {"6": moved},
        "files": {"6": [{"status": "added", "filename": f"{adr}/0012-a-claim.md"}]},
    }
    rc, posts, out = run_against_fake_gh(
        world, [adr], {adr: ["0012-an-ordinal-master-already-holds.md"]}
    )
    if rc != 0 or posts:
        print(
            f"self-test FAILED: {name}\n"
            f"  wanted rc=0 and no status, got rc={rc!r}, posted {posts!r}\n"
            "  output was:\n    " + out.replace("\n", "\n    "),
            file=sys.stderr,
        )
        return True
    print(f"self-test ok: {name}")
    return False


def self_test():
    """Drive the decision cores over the window this gate was built from.

    Run by `just gates`, by ci.yml's tidy job on every PR, and by
    ordinal-recheck.yml before any PR is examined, because this repository has
    no Python test runner and a gate whose logic nothing exercises is a gate
    nobody has watched fail. The tidy step is the one that MATTERS for a break:
    the other two red only after the breaking change has merged. The rows are
    the ones the design named as owed, plus the status-filter rows a mutation
    battery showed were owed and missing.
    """
    taken = "0012-an-ordinal-master-already-holds.md"
    rows = [
        (
            "the reconstructed window: master took the ordinal after the last push",
            [taken],
            ["0012-a-different-document-under-the-same-number.md"],
            False,
        ),
        (
            "green control: the PR's ordinal is free",
            [taken],
            ["0014-something-else-entirely.md"],
            True,
        ),
        (
            "a renumber arrives as a RENAME claiming a taken ordinal",
            [taken, "0013-an-unrelated-decision.md"],
            ["0012-renumbered-into-a-collision.md"],
            False,
        ),
        (
            "the PR re-adds a file master already has under that exact name",
            [taken],
            [taken],
            True,
        ),
    ]

    failed = self_test_claimed_by()
    if self_test_gh_failure():
        failed = True
    if self_test_refusal_does_not_silence_later_prs():
        failed = True
    if self_test_read_past_one_page():
        failed = True
    if self_test_head_moved_during_read():
        failed = True
    for name, master_names, added_names, want_ok in rows:
        got_ok, description = verdict("docs/adr", master_names, added_names)
        if got_ok != want_ok:
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  wanted ok={want_ok}, got ok={got_ok} ({description!r})",
                file=sys.stderr,
            )
            continue
        if not want_ok and "0012" not in description:
            failed = True
            print(
                f"self-test FAILED: {name}\n"
                f"  the description does not name the colliding ordinal: {description!r}",
                file=sys.stderr,
            )
            continue
        print(f"self-test ok: {name}")

    if failed:
        print(
            "error: the decision core does not behave as designed, so no verdict "
            "it posts can be trusted.",
            file=sys.stderr,
        )
        return 1
    return 0


def main(argv):
    args = argv[1:]
    if args == ["--self-test"]:
        return self_test()
    if not args or any(arg.startswith("-") for arg in args):
        print(f"usage: {argv[0]} DIR [DIR ...]", file=sys.stderr)
        print(f"       {argv[0]} --self-test", file=sys.stderr)
        return 2

    for directory in args:
        if not Path(directory).is_dir():
            # The same refusal check-doc-ordinals.py makes, for the same
            # reason: a series renamed out from under this call site would
            # otherwise make the gate quietly stop checking it.
            print(
                f"error: {directory} is not a directory, so nothing was checked.",
                file=sys.stderr,
            )
            return 1

    return check_open_prs(args)


if __name__ == "__main__":
    sys.exit(main(sys.argv))
