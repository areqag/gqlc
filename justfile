# single source of truth for the linter toolchain version. Every lint/fmt
# recipe self-heals: it verifies the pinned version in .bin/ and reinstalls on
# mismatch, so a version bump here is a one-line change and nobody ever
# installs or upgrades the linter by hand.
golangci_version := "v2.13.1"
golangci := justfile_directory() + "/.bin/golangci-lint"
# Wraps a `golangci-lint run` in a bounded wait on the machine-wide lock the
# linter takes (bd gqlc-49pc). Only `run` takes it — `fmt` was measured under a
# held lock and proceeds — so `fmt` and `fmt-check` below call the binary
# directly and this prefix appears on the `run` sites alone.
lint_lock := justfile_directory() + "/.githooks/lint-lock.sh"
# Per-worktree cache. golangci-lint caches analyzer facts and per-package issue
# records keyed on module path + relative file path + content hash — the absolute
# worktree path is NOT in the key. A shared default cache at ~/.cache/golangci-lint
# therefore returns issues carrying the absolute Pos.Filename of whichever
# worktree first computed them; when that worktree is removed, subsequent lints
# in a sibling report phantom paths (bd gqlc-6rv / gqlc-we8). CLAUDE.md mandates
# a fresh sibling worktree per session, so this is a routine hazard.
# Colocating the cache under .bin/ (already gitignored) makes `git worktree
# remove` delete it, and costs one cold lint (~80s) per fresh session.
export GOLANGCI_LINT_CACHE := justfile_directory() + "/.bin/golangci-cache"
actionlint_version := "v1.7.7"
# Same self-heal contract as golangci-lint above, for the hooks tree.
shellcheck_version := "v0.10.0"
shellcheck := justfile_directory() + "/.bin/shellcheck"
# Same self-heal contract again, for the Python that runs inside required CI
# contexts. Upstream tags releases without a leading `v`, so this pin is written
# without one and `ruff --version` prints it back verbatim (bd gqlc-tqi4).
ruff_version := "0.16.4"
ruff := justfile_directory() + "/.bin/ruff"

# Single source of truth for the discovery-probe names. Three recipes mktemp a
# throwaway module under test/data to witness that the module set is read off
# the tree, and sweep-discovery-probes globs those names to clear one a killed
# run left behind. The name therefore has to be the same string in two places
# that cannot see each other — the recipe that CREATES the probe and the recipe
# that REMOVES it — and a fact spelled twice is a fact that can disagree with
# itself (bd gqlc-oxne). Renaming a probe here moves both at once; renaming one
# literal in place is no longer possible, because there are no literals.
#
# .gitignore is the third place, and it cannot read these. What holds it to them
# is a `git check-ignore -v` witness inside the sweep that requires the matching
# rule to come from this repo's own .gitignore, so a rename reddens even in a clone
# whose own .git/info/exclude or core.excludesFile happens to hide the old name.
vuln_probe := "vulnprobe"
fence_probe := "fenceprobe"
xtest_probe := "xtestprobe"
discovery_probes := vuln_probe + " " + fence_probe + " " + xtest_probe

# The scratch filesystem every agent working on this repo shares, and where the
# Go toolchain is told to put its work directories while it reports on it.
#
# `go run` writes its work directory under os.TempDir(), so the tool that
# diagnoses a full /tmp could not be built at the one moment anyone wants it —
# and neither could `go build`, which is what "build failed with no error text
# on a different package set each run" actually was (bd gqlc-osuz). Pointing
# GOTMPDIR at .bin/ (already gitignored, and not on the tmpfs) makes the
# diagnostic survive the condition it diagnoses. Set per recipe rather than
# exported here: the go command refuses a GOTMPDIR that does not exist, and a
# global export would break every recipe in a fresh clone until something
# created it.
scratch_root := "/tmp"
gotmpdir := justfile_directory() + "/.bin/gotmp"

# The pressure at which the unattended cadence (`tmp-reap-cadence`, invoked by
# km's guard sweep) starts deleting, in whichever of bytes and inodes is fuller.
#
# 75 and not 95. The `check-tmp` gate warns at 85 and fails at 95, and those are
# thresholds for a HUMAN who is present: a suite that stops and names the cause
# is a good outcome at 95%. The cadence exists to stop anyone reaching them, so
# it has to act below the warning — a reaper whose trigger is the same number as
# the alarm has conceded the incident before it starts. It is also the number
# with headroom: the reap itself writes an archive, and 2026-08-22's incident
# went from comfortable to town-wide ENOSPC inside one working night with
# sixteen seats allocating scratch (bd gqlc-vze6).
reap_threshold := "75"

# Configures local git settings required after a fresh clone.
# Idempotent: safe to run multiple times.
#
# Two halves. The first wires core.hooksPath at .githooks — that is what makes
# this repo's hooks run. The second installs .githooks/hooks-drift-tripwire into
# the DEFAULT hooks directory, which is where git falls back when the first half
# is undone; that copy is what refuses a commit or a push while the drift stands.
# The tripwire's own header argues why it has to live there.
#
# The tripwire is copied, not symlinked. A symlink would point back into the
# working tree — the carrier that goes stale with the parked branch, which is why
# bd gqlc-pyk2's detector was inert — and at a commit predating that file it
# would dangle. A copy under the git common dir is branch-independent, which is
# the whole point of it.
#
# The install itself lives in .githooks/install-hooks-drift-tripwire, called both
# from here and from check-hooks' self-heal arm, so the two agree about the
# CONTENT of what lands. They still spell the destination and the marker
# separately — check-hooks recomputes both — and what holds those in step is the
# suite, which drives the real recipe against a fixture: moving either literal in
# one place alone reddens a named row (measured). The installer refuses to
# overwrite a hook it did not write, classifies all five names before writing any,
# and writes through a temp name and a rename rather than over the live path.
#
# The third half, added later and wired as a dependency rather than inline:
# check-push-keepalive puts ssh keepalives in core.sshCommand so a push whose
# pre-push run outlasts GitHub's idle timeout is not lost (bd gqlc-ehgg). It is
# a dependency because `just test` and `just doctor` need it too — this recipe
# is run once after a clone, and the worktrees that predate it never run it
# again.
#
# THE SUCCESS MESSAGE IS PART OF THE CONTRACT (bd gqlc-o13d). This recipe used to
# print "git hooks activated" straight after the write, from the WRITE, and that
# claim is not the write's to make. Measured 2026-08-23 on a throwaway repo: with
# GIT_CONFIG_PARAMETERS="'core.hooksPath=/dev/null'" exported, `git config
# core.hooksPath .githooks` writes .git/config, the file reads back '.githooks',
# and a real `git commit` with a refusing pre-commit in .githooks/ LANDS. Every
# string in sight is correct and nothing is gated. So a developer who ran the
# remedy the drift detector prints, saw success, and carried on was working
# unhooked — the state this bead was raised to P0 for. Success is now printed from
# a behavioural reading: git is asked to RUN a hook, or nothing is claimed.
#
# That reading is behavioural for a second reason now. This recipe writes the
# shared config twice — core.hooksPath here, core.sshCommand through the
# dependency above — and `just doctor` writes it too. A repair that verified
# itself by re-reading the file it had just written would be one concurrent
# `git config` away from reporting on somebody else's update; running a hook
# asks the question no writer of that file can answer for git.
init: check-push-keepalive
    #!/usr/bin/env bash
    set -euo pipefail
    git config core.hooksPath .githooks
    .githooks/install-hooks-drift-tripwire
    if ! .githooks/verify-hooks-live; then
        echo "" >&2
        echo "error: 'just init' wrote core.hooksPath = .githooks and git STILL does not run" >&2
        echo "       this repository's hooks. The write is not the repair — the lines above" >&2
        echo "       say what is actually in the way. Hooks are NOT active; do not proceed as" >&2
        echo "       though this recipe had succeeded (bd gqlc-o13d)." >&2
        exit 1
    fi
    echo "git hooks active — git ran .githooks/gqlc-liveness-probe just now (core.hooksPath = .githooks)"
    echo "hooksPath drift tripwire installed in $(git rev-parse --git-common-dir)/hooks (shared by every linked worktree)"
    echo "NOTE: that is a reading of this instant, not a promise. core.hooksPath lives in the"
    echo "      config every linked worktree shares, so any other live session can revert it"
    echo "      after this line prints — a worktree-isolated agent spawn is one measured way"
    echo "      (bd gqlc-o13d). 'just check-hooks' re-reads it; 'I ran just init' does not."

# fails when core.hooksPath drifts from .githooks, which silently kills every
# local pre-commit/pre-push gate at once (CI cannot see local git config).
# Sub-ms; wired into `test` so developers hit it naturally.
#
# This recipe is what a plain terminal has, but it only runs when someone runs
# it. .githooks/claude-pre-bash runs a superset of it on every Bash tool call
# inside a Claude Code session, which is what closes the window between the
# drifting write and the next `just test` (bd gqlc-nzwa). A superset in latency,
# no longer in coverage: this recipe compared the configured value and nothing
# else, so with core.hooksPath = .githooks but .githooks/ holding only *.sample
# files, or a hook file left non-executable, it exited 0 without a word (and
# `doctor`, which depends on it, printed "ok") where claude-pre-bash refuses.
# Both states are fixtures in .githooks/tests/claude-pre-bash-test.sh. The
# verify-hooks-live arm below closes them by running a hook rather than reading
# about one, and closes the environment-override state neither of them had.
#
# Skipped under CI, which has no local hooks by design and runs the equivalent
# gates as workflow jobs; without the skip this would fail every CI `just test`.
#
# Compares the configured value rather than testing the directory: the drift
# that actually occurred (bd gqlc-5fm) pointed at .git/hooks, which exists but
# holds only .sample files that git ignores, so an existence test passes while
# every hook is dead.
#
# The second arm installs the drift tripwire into the default hooks directory
# when it is absent, and holds it to its behaviour when it is not. Without it the
# value check above is the only thing standing between a drift and an ungated
# commit, and it only speaks when invoked — which is how the window in bd
# gqlc-4thl stayed open. The two arms cover different halves and
# neither subsumes the other: this one catches ANY spelling of the drifted value
# but only on demand, while the tripwire catches only the default-directory
# spelling and does it at commit and push time without being asked.
#
# NOT checked by comparing bytes with .githooks/hooks-drift-tripwire. The install
# is shared by every linked worktree while each worktree's copy of the source is
# at its own parked commit, so byte-equality would have two worktrees on
# different branches each declaring the other's install wrong and reinstalling
# over it on every `just init`.
#
# A marker line alone certifies PRESENCE, and presence is not what is owed. Measured:
# a three-line file carrying `#!/usr/bin/env bash`, the marker, and `exit 0`,
# installed as all five names, made `just doctor` print "ok" while a drifted
# commit LANDED. So did any `cp`-truncated prefix between 47 and 2707 bytes — the
# marker sits on line 2 and everything after it up to the case statement is
# comment, so a prefix parses and exits 0. Both bounds bisected: at 47 the marker
# first completes, and 2708 is the first prefix that stops exiting 0.
#
# What executing the installed hooks buys is those two shapes and the family they
# belong to: a stub or a truncated prefix exits 0 whatever it is handed, so one
# run exposes it. It does not reach a disarm written against this check. The
# hooks are run here with no arguments and without GIT_INDEX_FILE, so a copy
# branching on that variable refuses when this recipe runs it and permits when
# git runs it as pre-commit or commit-msg. Measured on this branch: five lines,
# check-hooks silent at rc=0, `just doctor` printing ok, drifted commit landed.
#
# NOT "without the variables git sets for a hook", which was measured false:
# GIT_EXEC_PATH and GIT_PREFIX are set for all five names and reach this recipe
# when it runs from .githooks/pre-push, and GIT_INDEX_FILE is one git sets for
# pre-commit and commit-msg but not for pre-push, post-checkout or post-merge.
# The suite unsets `${!GIT_@}` for exactly that reason. So GIT_INDEX_FILE is one
# usable key among several rather than the only door — $#, stdin, GIT_EDITOR and
# GIT_AUTHOR_* are equally usable. Whoever can write that directory can write
# that file; this raises the price of a deliberate disarm, it does not remove it.
#
# So after the marker grep, the installed hooks are EXECUTED and held to their
# exit codes: the three blocking arms must refuse, the two warn arms must not.
# That is still behaviour rather than bytes, so the parked-branch property
# survives intact — an older copy that still refuses still passes. Executed only
# after the marker grep, so this never runs a file it does not recognise, and with
# stderr discarded, because the real tripwire prints its whole ERROR block when it
# is reached and `just test` would be unreadable.
#
# The missing-install arm SELF-HEALS rather than refusing, following
# ensure-golangci above (which reinstalls the pinned linter rather than failing
# the push over it). This recipe is a dependency of `test`, which is what
# .githooks/pre-push runs, so refusing here would have made `just init` a
# precondition for every push in every registered worktree on the day this
# landed — and the obvious answer to a push refused for a reason unrelated to the
# commits is `git push --no-verify`, which skips .githooks/pre-push WHOLESALE and
# takes `just test` and `just lint-new` with it. Trading a hypothetical future
# ungated commit for an actual present untested, unlinted push is a bad trade. An
# absent hook file is unambiguous and the repair is one file copy, so it is
# repaired. A marker-bearing copy that FAILS the behavioural check below is not:
# that is tamper or corruption rather than absence, and it refuses.
#
# The hooksPath arms stay hard refusals: self-healing core.hooksPath would
# rewrite the very drift this recipe exists to report, and the shared config is
# where the damage lives.
[private]
check-hooks:
    #!/usr/bin/env bash
    [ -n "${CI:-}" ] && exit 0
    # The behavioural arm, and it runs FIRST (bd gqlc-o13d). It asks git to RUN a
    # hook, so it answers the two states the value comparison below exits 0 on —
    # .githooks/ holding only *.sample files, and a hook left non-executable — and
    # the state where the value is overridden from the ENVIRONMENT rather than
    # written to a file. Ordering is what that last one costs: `git config --get`
    # reports an environment override, so the value arm would refuse it and print
    # "Run 'just init' to fix", which for that shape rewrites a file that was
    # never wrong and changes nothing. Measured as a red row before this was
    # reordered. verify-hooks-live reads --show-origin and names the variables
    # instead.
    if ! .githooks/verify-hooks-live; then
        echo "error: git did not run a hook when asked, so local hooks are inactive." >&2
        echo "       The lines above say what is in the way; repair as they say —" >&2
        echo "       'just init' is not the answer to every one of these." >&2
        exit 1
    fi
    # Still reached, and not redundant. A hook ran, so the lookup works, but the
    # value can still be a spelling this repository does not use — an absolute
    # path at its own .githooks runs every hook and is drift worth naming, and it
    # is the shape that will not survive the directory being moved. This arm is
    # also the only one left on a git older than 2.36, where verify-hooks-live has
    # no `git hook run` to use and says so instead of refusing.
    got="$(git config --get core.hooksPath || true)"
    if [ "$got" != ".githooks" ]; then
        echo "error: core.hooksPath is '${got:-<unset>}', expected '.githooks' — local hooks are inactive." >&2
        echo "       Run 'just init' to fix." >&2
        exit 1
    fi
    dest_dir="$(git rev-parse --git-common-dir)/hooks"
    blocking=(pre-commit commit-msg pre-push)
    warning=(post-checkout post-merge)

    absent=()
    for name in "${blocking[@]}" "${warning[@]}"; do
        target="$dest_dir/$name"
        if [ ! -x "$target" ] || ! grep -q 'gqlc-hooks-drift-tripwire' "$target" 2>/dev/null; then
            absent+=("$name")
        fi
    done
    if [ "${#absent[@]}" -ne 0 ]; then
        # A foreign hook squatting one of the names is the one ambiguous case, and
        # the installer refuses it rather than clobbering — for the whole set, not
        # just that name.
        if ! .githooks/install-hooks-drift-tripwire --missing-only; then
            echo "error: the core.hooksPath drift tripwire could not be installed into $dest_dir." >&2
            echo "       core.hooksPath is correct right now, so hooks run — but if it drifts" >&2
            echo "       to the default directory nothing will refuse the ungated commits." >&2
            exit 1
        fi
        echo "check-hooks: self-healed the hooksPath drift tripwire (${absent[*]})." >&2
        echo "             It lives under the git common dir, so this armed every linked" >&2
        echo "             worktree at once; no per-worktree install is needed." >&2
    fi

    for name in "${blocking[@]}"; do
        if "$dest_dir/$name" >/dev/null 2>&1; then
            echo "error: $dest_dir/$name carries the drift tripwire marker but exits 0 when run." >&2
            echo "       It would certify itself as installed and then let every commit and" >&2
            echo "       push through while core.hooksPath is drifted — a truncated copy or a" >&2
            echo "       disarmed one. Delete it and run 'just init' to reinstall." >&2
            exit 1
        fi
    done
    for name in "${warning[@]}"; do
        if ! "$dest_dir/$name" >/dev/null 2>&1; then
            echo "error: $dest_dir/$name exits non-zero when run, and the post-* arms must not." >&2
            echo "       post-checkout's exit status BECOMES the exit status of git checkout" >&2
            echo "       and git switch, so a blocking copy there fails every branch switch in" >&2
            echo "       every drifted worktree; post-merge's status is ignored by git, so a" >&2
            echo "       non-zero code there is a copy that is not this tripwire at all." >&2
            echo "       Delete it and run 'just init' to reinstall." >&2
            exit 1
        fi
    done

# fails when this worktree's branch tracks master, which is the state
# `git worktree add -b <branch> origin/master` leaves behind (bd gqlc-tfh1).
# In it, a bare `git push` resolves to master and `git pull` merges master into
# the branch — neither says so, and `git status` reports ahead/behind against
# master as if that were the branch's home.
#
# Distinct from the .githooks/guard-push-destination backstop, which sees a
# push and nothing else: this names the misconfiguration while it is still
# latent, and it is the arm that reaches the worktrees that already exist —
# 4 of the 21 alive on 2026-08-19 tracked origin/master. Wired into `test` for
# the same reason check-hooks is: a check nobody invokes is not a check.
#
# The directory is an argument so the recipe can be exercised over a throwaway
# tree (.githooks/tests/worktree-upstream-test.sh); developers and CI take the
# default.
#
# Not skipped under CI, unlike check-hooks. actions/checkout leaves a
# pull_request run on a detached HEAD (no upstream, nothing to say) and a
# master push on master itself (allowed below), so there is no state CI is
# expected to be in that this would fail — and skipping would mean the only
# thing that ever runs it is a developer's machine.
[private]
check-worktree-upstream dir=".":
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{ dir }}"
    branch="$(git -C "$dir" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    # Detached HEAD — how a reviewer checks out a SHA — prints the literal
    # "HEAD" and has no upstream.
    if [ -z "$branch" ] || [ "$branch" = "HEAD" ] || [ "$branch" = "master" ] || [ "$branch" = "main" ]; then
        exit 0
    fi
    upstream="$(git -C "$dir" rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
    if [ -z "$upstream" ]; then
        exit 0
    fi
    # Compared against the branch's own remote rather than pattern-matched on
    # */master: a remote-tracking ref named origin/topic/master is not this bug.
    remote="$(git -C "$dir" config "branch.$branch.remote" || true)"
    case "$remote" in
        "")  exit 0 ;;
        ".") prefix="" ;;
        *)   prefix="$remote/" ;;
    esac
    if [ "$upstream" != "${prefix}master" ] && [ "$upstream" != "${prefix}main" ]; then
        exit 0
    fi
    echo "error: branch '$branch' tracks '$upstream', so a bare 'git push' here targets" >&2
    echo "       ${upstream#"$prefix"} and 'git pull' merges it in (bd gqlc-tfh1)." >&2
    echo "       Drop the tracking, and set it from the first push instead:" >&2
    echo "         git -C '$dir' branch --unset-upstream" >&2
    echo "         git -C '$dir' push -u origin HEAD" >&2
    exit 1

# fails when a key in the shared .git/config holds a value that breaks the MAIN
# worktree, which is the state observed 2026-08-22 with `core.bare = true`
# (bd gqlc-qhno): every git command in the shared repo cwd died with "fatal:
# this operation must be run in a work tree", and that is the directory CLAUDE.md
# designates for read-only research.
#
# WHY NOBODY NOTICED, and why this reads the config rather than asking git what
# it is. core.bare and core.worktree disable the main worktree ONLY; every
# linked worktree keeps working. Measured on a throwaway repo with both keys, in
# turn, set in the shared config:
#
#     main worktree     git status -> fatal, rc=128
#     linked worktree   git status -> rc=0, clean
#
# So every seat is green while the shared cwd is bricked. The probe has to be
# chosen with that in mind: from the linked worktree
# `git rev-parse --is-bare-repository` answers FALSE while
# `git config --get core.bare` answers TRUE. A detector built on the former is
# blind from every worktree except the one that is already broken — and `just
# test` runs in the seats. The config read is the arm that reaches.
#
# The set is named rather than swept. Both keys here are legitimate git
# configuration in other repositories, so there is no general rule to apply; a
# sweep would have to enumerate anyway, and enumerating in the open says which
# keys are claimed. Add to it when a new key is found to have this shape.
#
# core.hooksPath has the same blast radius and is NOT in this set: check-hooks
# above owns it, with behavioural arms this recipe has no equivalent of. That
# recipe must skip CI, because a CI checkout legitimately has no hooksPath — and
# that skip is the reason not to fold the keys together. core.bare and
# core.worktree are wrong in CI too, so this recipe runs there.
#
# The directory is an argument so the recipe can be exercised over a throwaway
# tree (.githooks/tests/shared-config-drift-test.sh).
[private]
check-shared-config dir=".":
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{ dir }}"
    # key|allowed|allowed... — <unset> is the sentinel for "not present at all".
    specs=(
        "core.bare|<unset>|false"
        "core.worktree|<unset>"
    )
    rc=0
    for spec in "${specs[@]}"; do
        key="${spec%%|*}"
        allowed="${spec#*|}"
        # --get-all, not --get. Measured: on a key set twice git resolves to the
        # LAST value, and --get reports that one. So `bare=true` followed by
        # `bare=false` reads clean through --get, and the repository does still
        # work. It is refused anyway: a drifted value sitting in the shared
        # config is one write-ordering away from being the live one, and the
        # writer that put it there has not been identified (bd gqlc-qhno item 2).
        # Every value present is judged, not the winning one.
        mapfile -t values < <(git -C "$dir" config --get-all "$key" 2>/dev/null || true)
        if [ "${#values[@]}" -eq 0 ]; then
            values=("<unset>")
        fi
        for value in "${values[@]}"; do
            ok=0
            while IFS= read -r candidate; do
                [ "$value" = "$candidate" ] && ok=1
            done < <(printf '%s\n' "$allowed" | tr '|' '\n')
            [ "$ok" -eq 1 ] && continue
            rc=1
            origin="$(git -C "$dir" config --show-origin --get-all "$key" 2>/dev/null \
                | grep -F "	$value" | head -1 | cut -f1 | sed 's/^file://')"
            echo "error: $key is '$value' in the shared git config (bd gqlc-qhno)." >&2
            echo "       Allowed: ${allowed//|/, }." >&2
            [ -n "$origin" ] && echo "       Set in: $origin" >&2
            echo "       This disables the MAIN worktree only — every linked worktree, and so" >&2
            echo "       every seat, keeps working while the shared cwd answers 'fatal: this" >&2
            echo "       operation must be run in a work tree' to every command." >&2
            echo "       Repair: git -C '$dir' config --unset-all $key" >&2
        done
    done
    exit "$rc"

# the checked-in project settings must carry permissions.defaultMode, because a
# seat resumed by hand gets no launch flags: `claude --resume <uuid>` replays
# none of them, and a running session's permission mode cannot be changed by any
# means. Settings FILES are re-read at every launch, so the project file is the
# only carrier that reaches a launch nobody configured (bd gqlc-keaz).
#
# MEASURED 2026-08-29 over four throwaway trees, each asked for one Write:
#   A  no permissions key                          → DENIED, no file
#   B  permissions.defaultMode=bypassPermissions   → allowed
#   C  B plus a settings.local.json of the shape every seat here has (an
#      allow list, no defaultMode)                 → allowed
#   D  C's local file, project key removed         → DENIED
# B against A is that project scope carries the mode at all; C against D is that
# the per-seat settings.local.json MERGES field-wise rather than replacing the
# permissions object. Without C this fix would have been inert in the only trees
# it exists for, and green here regardless.
#
# It does not weaken the hooks: under bypassPermissions a PreToolUse hook still
# runs and still blocks on exit 2 (measured the same day — the model issued the
# Write, the hook fired, the tool_result read `PreToolUse:Write hook error`, no
# file appeared). claude-pre-ask's refusal of interactive tools in an unattended
# seat stands.
#
# A PARSE FAILURE IS REFUSED, NOT SKIPPED. `claude -p` silently ignores a
# settings file that fails validation — no error and no dialog, as its own
# --help says. A malformed file is therefore indistinguishable at launch from an
# absent key, so treating it as anything but a failure would hide exactly the
# regression this guard is for.
#
# Its limit: it judges the CHECKED-IN file. A settings.local.json is untracked
# and per-seat, so a citizen who sets a weaker defaultMode in theirs is not seen
# here, and deliberately — that file is where a local choice belongs.
#
# The directory is an argument so a mutation can be run over a throwaway copy
# rather than the tree under test.
[private]
check-claude-permission-mode dir=".":
    #!/usr/bin/env bash
    set -uo pipefail
    settings="{{ dir }}/.claude/settings.json"
    want="bypassPermissions"
    if [ ! -f "$settings" ]; then
        echo "error: $settings does not exist, so no seat launched without an explicit" >&2
        echo "       --permission-mode gets one at all (bd gqlc-keaz)." >&2
        exit 1
    fi
    # one line because an unindented continuation is parsed as justfile syntax,
    # not as recipe body. A parse failure arrives as a non-zero exit with the
    # traceback on stdout, which is the branch below.
    if ! got="$(python3 -c 'import json,sys;v=json.load(open(sys.argv[1])).get("permissions",{}).get("defaultMode");print("ABSENT" if v is None else v)' "$settings" 2>&1)"; then
        echo "error: $settings is not valid JSON, so Claude Code ignores it in full and" >&2
        echo "       silently — a seat then comes up permission-gated with no diagnostic" >&2
        echo "       anywhere (bd gqlc-keaz). python said:" >&2
        printf '%s\n' "$got" | sed 's/^/       /' >&2
        exit 1
    fi
    case "$got" in
        "$want") exit 0 ;;
        ABSENT)
            echo "error: $settings carries no permissions.defaultMode (bd gqlc-keaz)." >&2
            echo "       A seat resumed by hand — 'claude --resume <uuid>', which replays no" >&2
            echo "       launch flags — then comes up gated on a human who is not there, and" >&2
            echo "       waits until someone kills the session." >&2
            echo "       Repair: set permissions.defaultMode to \"${want}\" in that file." >&2
            exit 1 ;;
        *)
            echo "error: $settings sets permissions.defaultMode to '${got}', not '${want}'" >&2
            echo "       (bd gqlc-keaz). Only bypassPermissions leaves an unattended seat able" >&2
            echo "       to act; every other mode prompts for a human at some tool." >&2
            exit 1 ;;
    esac

# puts ssh keepalives in the repository's own git config, so an ordinary
# `git push` survives .githooks/pre-push — bd gqlc-ehgg / GH #1414.
#
# THE DEFECT. git opens the transport to the remote BEFORE it runs pre-push,
# then this repository's pre-push holds it idle for the whole gate chain: the
# full go suite, every shell suite, golangci-lint. Twelve to fifteen minutes on
# a loaded machine, and longer the more seats are pushing. GitHub's server-side
# idle timeout closes the connection during that window, so git exits 141
# (SIGPIPE, "Connection to github.com closed by remote host") or 143 AFTER every
# gate has passed. Four independent lanes hit it in one night on 2026-08-23.
#
# It is vicious in three directions. The tail of the hook output is all `ok`, so
# a lane that tails its push log reads a green run and the one line that matters
# is a page above it. The objects sometimes ARRIVE ANYWAY — measured: a lane
# reported five consecutive rc=141 failures while `git ls-remote` showed its
# branch already on origin at the local SHA — so two lanes abandoned work that
# had landed. And the gate is what is running when the connection dies, so the
# gate looks like the obstacle: one lane reached for `git push --no-verify` on
# its fourth attempt, which is the one thing this repository never does.
#
# WHY THE CONFIG AND NOT THE ENV VAR. `GIT_SSH_COMMAND='ssh -o
# ServerAliveInterval=15 ...' git push` is measured to work and is what got
# every lane through that night, but it only helps someone who already knows to
# type it. core.sshCommand is read by every `git push` from every worktree
# sharing this repository, including one a human runs without having read the
# bead. That is the difference between a workaround and a fix.
#
# WHAT IT DOES NOT COVER, stated because a partial fix read as a complete one is
# how this defect keeps costing hours. core.sshCommand governs ssh and nothing
# else, and rc=141 was measured over HTTPS too on 2026-08-23 by a second lane —
# git spawns no ssh there, so there is nothing for the keepalive to attach to
# and no equivalent key to set. It covers every push from this repository as it
# is configured: all 17 seat worktrees and the shared checkout resolve origin to
# git@github.com:areqag/gqlc.git, with no url.insteadOf rewrite, and remotes live
# in the shared config so a per-worktree difference is not reachable (measured
# 2026-08-23). A clone made over https is the uncovered case, and
# .githooks/push-transport-notice tells that operator so on every push rather
# than leaving the silence to be read as cover.
#
# WHY IT SELF-HEALS RATHER THAN REFUSING, unlike check-hooks above. The key is
# absent on every fresh clone and in every worktree registered before this
# landed, so a refusal would have failed every push in the town on the day it
# shipped — and the obvious answer to a push refused for a reason unrelated to
# the commits is `git push --no-verify`, which is the exact behaviour this whole
# recipe exists to remove the pressure for. This follows check-hooks' tripwire
# arm and ensure-golangci: absence is unambiguous and the repair is one write.
#
# WHAT IT WILL NOT DO. It writes core.sshCommand and nothing else. The shared
# config is where the damage lives — bd gqlc-qhno's core.bare, and the spawns
# that rewrote core.hooksPath to /dev/null and disabled every hook repo-wide —
# so an existing value is REPORTED and never overwritten. `ssh -i ~/.ssh/id_foo`
# is legitimate configuration that belongs to whoever wrote it, and clobbering
# it would lock its author out of the remote entirely; a lost push is a smaller
# harm than a broken key. That arm warns and exits 0 for the same reason.
#
# It reads --get, not --get-all, which is the opposite of check-shared-config
# above, and the two questions are why. That recipe asks "is a dangerous value
# PRESENT", where any occurrence convicts. This one asks "is the keepalive IN
# EFFECT", and what is in effect is the value git resolves — the last one. A
# keepalive-bearing value masked by a later plain `ssh` is not in effect and
# must be reported, which --get-all would miss by finding the good one.
#
# An empty value is treated as absent: git reads `[core]\n\tsshCommand =` back
# as a present empty string and then falls through to plain ssh, so it carries
# no keepalive and is nobody's deliberate configuration.
#
# Skipped under CI, which pushes over https with no local hooks and would only
# be writing a key into a checkout thrown away at the end of the job.
[private]
check-push-keepalive dir=".":
    #!/usr/bin/env bash
    set -uo pipefail
    dir="{{ dir }}"
    if [ -n "${CI:-}" ]; then
        exit 0
    fi
    want='ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=120'
    have="$(git -C "$dir" config --get core.sshCommand 2>/dev/null || true)"
    if [ -z "$have" ]; then
        if git -C "$dir" config core.sshCommand "$want" 2>/dev/null; then
            echo "note: core.sshCommand set to '$want' — an ordinary 'git push' now keepalives" >&2
            echo "      the transport while .githooks/pre-push runs (bd gqlc-ehgg)." >&2
        else
            echo "warn: could not write core.sshCommand in '$dir', so a push whose pre-push run" >&2
            echo "      outlasts GitHub's ssh idle timeout will still die at rc=141 with every" >&2
            echo "      gate passed (bd gqlc-ehgg). Push under this instead:" >&2
            echo "        GIT_SSH_COMMAND='$want' git push ..." >&2
        fi
        exit 0
    fi
    case "$have" in
        *ServerAliveInterval=*) exit 0 ;;
    esac
    echo "warn: core.sshCommand is '$have', which sets no ServerAliveInterval, and it is NOT" >&2
    echo "      overwritten here — it is someone's deliberate configuration (bd gqlc-ehgg)." >&2
    echo "      A push whose pre-push run outlasts GitHub's ssh idle timeout will die at" >&2
    echo "      rc=141 with every gate passed. Add the keepalive to your own value:" >&2
    echo "        git -C '$dir' config core.sshCommand '$have -o ServerAliveInterval=15 -o ServerAliveCountMax=120'" >&2
    exit 0

# answers whether a push that reported failure actually landed — bd gqlc-ehgg.
#
# The rc=141 above does not mean the objects stayed home. Measured 2026-08-23: a
# lane reported five consecutive push failures and stalled, while its branch was
# already on origin at the same SHA as local. Two lanes abandoned landed work
# that night, and the alternative to this recipe is a hand investigation by
# someone who has just watched fifteen minutes of green gates end in a failure.
#
# READ-ONLY, and deliberately not a retry. Pushing again into a remote state
# nobody has looked at is how a force-push argument gets made at 3am; this
# reports the three states and stops.
push-landed branch="":
    #!/usr/bin/env bash
    set -uo pipefail
    branch="{{ branch }}"
    [ -n "$branch" ] || branch="$(git rev-parse --abbrev-ref HEAD)"
    local_sha="$(git rev-parse --verify "$branch" 2>/dev/null || true)"
    if [ -z "$local_sha" ]; then
        echo "error: '$branch' is not a branch in this worktree." >&2
        exit 1
    fi
    remote_sha="$(git ls-remote origin "refs/heads/$branch" 2>/dev/null | awk 'NR==1 { print $1 }')"
    if [ -z "$remote_sha" ]; then
        echo "NOT LANDED: origin has no refs/heads/$branch. Local is $local_sha."
        echo "            The push did fail. Retry it; nothing on the remote is at stake."
        exit 1
    fi
    if [ "$remote_sha" = "$local_sha" ]; then
        echo "LANDED: origin/$branch is $remote_sha, the same commit as local."
        echo "        The push SUCCEEDED whatever rc git reported (bd gqlc-ehgg). Do not retry;"
        echo "        do not abandon this work as unpushed."
        exit 0
    fi
    echo "DIVERGED: origin/$branch is $remote_sha, local is $local_sha."
    echo "          Something landed, but not this commit. Look before pushing again:"
    echo "            git fetch origin '$branch' && git log --oneline FETCH_HEAD...$branch"
    exit 1

# refuses to run when bd's auto-export cannot stage its own output — bd
# gqlc-c2ch / GH #1170.
#
# The defect, measured 2026-08-22 from a seat worktree: every `bd update` ended
# with
#
#     ✓ Updated issue: gqlc-cn8e — ...
#     Warning: auto-export: git add failed: exit status 128: fatal: this
#     operation must be run in a work tree
#
# The DB write succeeded, bd printed ✓ and exited 0, and `.beads/issues.jsonl`
# silently stopped tracking the ledger. Reproduced here on a throwaway repo
# 2026-08-23: the cause is `core.bare = true` in the SHARED config, which the
# check above now refuses by name. This recipe exists because the state check
# and the behavioural one answer different questions. check-shared-config
# enumerates two keys known to have this blast radius; nobody has identified the
# writer that sets them (gqlc-qhno item 2), and the failing operation has other
# ways to break — a core.worktree redirect, a permission wall, a GIT_DIR that
# points somewhere else. This recipe asks the operation itself.
#
# WHY IT PROBES $root AND NOT $dir. bd resolves one beads directory per
# repository, at the MAIN worktree's root, and stages the export THERE no matter
# which worktree you invoke it from. Measured on the live repo 2026-08-23: after
# a bd write from the shared checkout the export was rewritten and staged there,
# while gqlc-seat-sedrak's checked-out copy still carried its worktree-creation
# mtime, 1.9 MB smaller. So a seat's own `.beads/issues.jsonl` never moves, and
# a seat looking at its own tree can see neither the staleness nor the failure.
# The probe has to reach across into the main checkout to see anything at all —
# which is the same asymmetry that made check-shared-config read the config.
#
# TWO ARMS, and they are not redundant:
#   A. `git add --dry-run` over the export path — the operation bd actually
#      runs. Measured: rc=128 with the reported message under core.bare=true.
#   B. the main checkout's toplevel is where we think it is. Measured: under
#      `core.worktree = /tmp`, arm A exits 0 while staging a path under /tmp —
#      a pass that stages the wrong file. Arm A alone cannot see that.
#
# --dry-run never writes the index, so this is read-only against the repository
# it judges.
[private]
check-beads-export dir=".":
    #!/usr/bin/env bash
    set -uo pipefail
    # git exports GIT_DIR / GIT_WORK_TREE to every hook, `just test` runs from
    # .githooks/pre-push, and those variables beat `git -C`. Without the scrub
    # the probe judges the hook's repository rather than the one at $dir — it
    # would report on a healthy tree while a bricked one went unread. Through
    # the shared file rather than a private copy of the line (bd gqlc-o9wz);
    # $0 here is just's temp script, so the path comes from the justfile.
    sandbox={{ quote(justfile_directory() + "/.githooks/git-env-sandbox.sh") }}
    if [ ! -f "$sandbox" ]; then
        echo "error: $sandbox is missing, so the git environment cannot be scrubbed and" >&2
        echo "       this probe would judge whatever repository a hook was running in." >&2
        exit 1
    fi
    # shellcheck source=.githooks/git-env-sandbox.sh disable=SC1091
    source "$sandbox"
    dir="{{ dir }}"
    common="$(git -C "$dir" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
    if [ -z "$common" ]; then
        echo "error: '$dir' is not a git repository, so bd's export target cannot be" >&2
        echo "       resolved (bd gqlc-c2ch)." >&2
        exit 1
    fi
    # --git-common-dir, not --git-dir: from a linked worktree the latter answers
    # <main>/.git/worktrees/<name>, whose parent is not a checkout at all.
    root="$(cd "$(dirname "$common")" 2>/dev/null && pwd -P)"
    rc=0
    if [ -z "$root" ]; then
        echo "error: the main checkout for '$dir' resolves to '$(dirname "$common")'," >&2
        echo "       which is not a directory (bd gqlc-c2ch)." >&2
        exit 1
    fi
    if [ ! -f "$root/.beads/issues.jsonl" ]; then
        echo "error: bd's export target $root/.beads/issues.jsonl does not exist, so the" >&2
        echo "       in-tree export is not being written at all (bd gqlc-c2ch)." >&2
        rc=1
    elif ! out="$(git -C "$root" add --dry-run -- .beads/issues.jsonl 2>&1)"; then
        echo "error: bd's auto-export cannot stage .beads/issues.jsonl in $root" >&2
        echo "       (bd gqlc-c2ch). git said: $out" >&2
        echo "       bd hits this on every write, prints the warning AFTER its ✓, and exits" >&2
        echo "       0 — so the export silently stops tracking the ledger. Run" >&2
        echo "       'just check-shared-config' next: core.bare=true in the shared config is" >&2
        echo "       the cause measured on 2026-08-22." >&2
        rc=1
    fi
    top="$(git -C "$root" rev-parse --show-toplevel 2>/dev/null)"
    [ -n "$top" ] && top="$(cd "$top" 2>/dev/null && pwd -P)"
    if [ "$top" != "$root" ]; then
        echo "error: the main checkout $root reports its work tree as '${top:-<none>}'" >&2
        echo "       (bd gqlc-c2ch). bd's export would be staged against that tree instead," >&2
        echo "       which 'git add' does without failing. Check core.worktree." >&2
        rc=1
    fi
    exit "$rc"

# The single entry point to internal/tools/tmpreap, so GOTMPDIR and the raw-df
# fallback below are spelled once rather than once per caller.
#
# The fallback is the point. Every way this tool can fail — a full scratch
# filesystem the Go toolchain cannot build in, a permission wall, a bug in the
# tool — ends with the two `df` invocations that answer the question anyway, and
# with `-i` beside `-h` because inodes are the currency that ran out and `df -h`
# is green while they do (bd gqlc-osuz).
[private]
tmpreap root *args:
    #!/usr/bin/env bash
    set -uo pipefail
    mkdir -p {{quote(gotmpdir)}}
    rc=0
    GOTMPDIR={{quote(gotmpdir)}} go run ./internal/tools/tmpreap \
        -root {{quote(root)}} -repo {{quote(justfile_directory())}} {{args}} || rc=$?
    if [ "$rc" -ne 0 ]; then
        echo >&2
        echo "tmpreap exited $rc over {{root}}. The raw numbers, in case it was the scratch" >&2
        echo "filesystem that stopped it — a full one takes the Go toolchain down with it:" >&2
        df -h {{quote(root)}} >&2 || true
        df -i {{quote(root)}} >&2 || true
        exit "$rc"
    fi

# refuses to run the suite over a scratch filesystem that is about to make it
# lie. Wired into `test` and `doctor`; ~50ms warm.
#
# It is a GATE and not a warning because of what the failure looks like from the
# other side: `just test` reporting a build failure with no error text, on a
# DIFFERENT package set each run, because the build harness could not write its
# work directory. One author read that as a real test failure, and two others
# read `git worktree add` failing "No space left on device" with gigabytes free
# as a broken tree (bd gqlc-osuz). A run that stops here with the real cause
# named costs less than any of those.
#
# Skipped under CI: a hosted runner's /tmp is the root disk of a preinstalled
# image, routinely past these thresholds and reset for every job, so the local
# multi-agent exhaustion this guards against cannot happen there and the
# thresholds would only fail honest runs. GQLC_SKIP_TMP_CHECK is the local
# escape hatch, for the developer who has decided the pressure is fine.
[private]
check-tmp root=scratch_root:
    #!/usr/bin/env bash
    set -uo pipefail
    if [ -n "${CI:-}" ] || [ -n "${GQLC_SKIP_TMP_CHECK:-}" ]; then
        exit 0
    fi
    just tmpreap {{quote(root)}} -check

# health check for local dev environment; extend as new drift modes emerge
doctor: check-hooks check-worktree-upstream check-shared-config check-beads-export check-tmp check-push-keepalive
    @echo "ok"

# what is holding the shared scratch filesystem, in bytes AND inodes, with the
# decision this tool would take over every top-level entry and the reason for it.
#
# Read-only by construction: it cannot be handed -apply. The reporting half is
# the half that pays, because the recurring cost of this failure is that it gets
# MISDIAGNOSED — three agents, three different wrong diagnoses, before anyone
# looked at `df -i` (bd gqlc-osuz).
tmp-report root=scratch_root:
    @just tmpreap {{quote(root)}}

# reclaims the entries `just tmp-report` proved abandoned. Dry run by default;
# `just tmp-reap apply` is what deletes.
#
# What it will not touch, each for its own reason: a worktree with uncommitted or
# untracked changes, a worktree whose content is not already equal on origin/master,
# anything a live process has as its cwd or holds an fd on, anything written to
# inside the age threshold, anything holding a git repository this repo does not
# track, and anything belonging to the machine. Under `apply` the text artifacts
# under -archive-max-file that it is about to destroy are tarred to a file outside
# the scan root first, up to a total of -archive-max-total — 690 MiB of agent logs
# came to 43 MiB in the manual remediation this replaces — and what it could NOT
# archive is reported on stdout as unrecoverable, because those files are deleted
# all the same: every one of them counted under a reason, and the first few of
# each reason named. Hitting the total instead refuses the deletion outright,
# since the record it would delete over is incomplete.
#
# The root is positional, so `apply ~` is one typo away from the home directory.
# `apply` is refused over any root outside /tmp and /var/tmp — $TMPDIR is not
# consulted, because an environment variable on that list is an authorisation to
# delete that anything in the shell can set — and over anything on the same path
# chain as $HOME. A dry run, which is read-only, stays available over any
# directory.
#
# The mode is refused rather than defaulted when it is neither of the two: a typo
# that silently dry-runs is a reap somebody thinks they performed.
tmp-reap mode="dry-run" root=scratch_root:
    #!/usr/bin/env bash
    set -uo pipefail
    # Shell-quoted into one variable rather than pasted raw into the case head
    # and the message: `{{{{mode}}}}` interpolates verbatim, so a value carrying a
    # quote or a `)` is shell syntax there rather than data (bd gqlc-4seg).
    mode={{quote(mode)}}
    case "$mode" in
        dry-run) just tmpreap {{quote(root)}} ;;
        apply)   just tmpreap {{quote(root)}} -apply ;;
        *)
            echo "error: unknown mode '$mode' — expected 'dry-run' or 'apply'." >&2
            exit 1
            ;;
    esac

# the unattended half: what km's guard sweep runs once per cadence, and the only
# thing in this town that reclaims scratch without a person typing (bd gqlc-u078).
#
# Everything above this line is a REMEDY — a report someone reads, a gate that
# stops a suite, an apply someone types. Each one needs a citizen to already
# suspect the filesystem, and the state that motivated the whole tool is the one
# where nobody does: /tmp reached 99% of its inode cap overnight and began
# refusing writes town-wide while `df -h` still showed 5.9G free, so the error
# and the obvious diagnostic pointed at different resources (bd gqlc-vze6). The
# gqlc-vze6 close said it plainly: "A reaper nobody invokes is the state we were
# already in."
#
# It is `-apply` with a threshold rather than a conditional around `tmp-reap`,
# so the decision to delete is taken by the process that took the measurement,
# in the same run. A shell that measured with one invocation and deleted with a
# second would have to decide what a non-zero exit meant, and the only
# unrecoverable way to be wrong here is to delete because you could not measure.
# tmpreap returns an error and touches nothing on every unmeasurable filesystem.
#
# Under the threshold the tool stops at the statfs, so a tick costs microseconds
# and does not walk half a million inodes to conclude it has nothing to do.
tmp-reap-cadence root=scratch_root threshold=reap_threshold:
    @just tmpreap {{quote(root)}} -apply -apply-above {{quote(threshold)}}

# provisions the pinned golangci-lint into the gitignored .bin/ when missing
# or version-mismatched (~3s; official release binary — golangci-lint does not
# support builds from source). The happy path is a ~30ms version check, cheap
# enough to run before every lint/fmt invocation, in hooks included.
#
# The download is TWO hops: raw.githubusercontent.com for install.sh, then the
# release asset install.sh fetches for itself. curl's own --retry covers the
# first hop only, which is why the retry here is a loop around the whole
# pipeline rather than a flag (ensure-shellcheck below is a single hop, so the
# flags suffice there). Measured on 2026-08-17 (bd gqlc-l45j): GitHub returned
# HTTP 429 on this download while ~8 of this repo's PRs had CI in flight, and
# with no retry that killed a required context in setup, before the change was
# read. The failure message names provisioning rather than lint so a reader can
# tell a setup death from a real finding without opening the log.
#
# GQLC_PROVISION_ATTEMPTS / GQLC_PROVISION_DELAY size the budget; the retry
# tests in .githooks/tests/tool-gate-test.sh set them to keep the failing case
# fast. An attempts value below 1 runs the loop zero times and falls through to
# the error, so a malformed budget blocks rather than passes.
#
# AND THEN REFUSES A LINTER OLDER THAN THE TOOLCHAIN, by name (bd gqlc-6rf3).
# just reads the pin from the justfile of the tree you are standing in, so a
# branch based before the last pin bump provisions the OLD linter — and a
# go1.N-built golangci-lint cannot load go1.(N+1) source. It dies inside
# go/types with a stack trace that names neither the pin nor the branch base,
# and nothing anywhere says "your base is old". Measured by Նուարդ across three
# trees: same branch, panic before `git merge origin/master` and green after.
# It cost her most of a session and cost the mayor two wrong town-wide
# broadcasts — the second of which told seats to declare a WORKING gate unrun in
# their PR bodies, which is the real loss, because a gate everyone ritually
# disclaims can no longer be told apart from one that genuinely did not run.
#
# The pin is deliberately read from the tree under test rather than from
# origin/master, so that a branch can test a linter change; that is why this
# reports rather than repairs. What it owes is a cause, and the message carries
# both the pin and the remedy.
#
# The comparison is on the Go MINOR version the linter binary was built with, as
# `golangci-lint version` reports it, against `go env GOVERSION`. Older only: a
# linter built with a NEWER Go than the local toolchain loads older source
# fine, so refusing that direction would redden a working tree.
[private]
ensure-golangci:
    #!/usr/bin/env bash
    set -euo pipefail
    want="{{golangci_version}}"
    if [ "$({{quote(golangci)}} version --short 2>/dev/null || true)" != "${want#v}" ]; then
        echo "provisioning golangci-lint $want into .bin/" >&2
        attempts="${GQLC_PROVISION_ATTEMPTS:-4}"
        delay="${GQLC_PROVISION_DELAY:-2}"
        attempt=1
        installed=0
        while [ "$attempt" -le "$attempts" ]; do
            if curl --proto '=https' --tlsv1.2 -sSfL \
                    "https://raw.githubusercontent.com/golangci/golangci-lint/$want/install.sh" \
                | sh -s -- -b {{quote(justfile_directory() + "/.bin")}} "$want"; then
                installed=1
                break
            fi
            echo "ensure-golangci: provisioning attempt $attempt of $attempts failed" >&2
            if [ "$attempt" -lt "$attempts" ]; then
                sleep "$delay"
                delay=$((delay * 2))
            fi
            attempt=$((attempt + 1))
        done
        if [ "$installed" -ne 1 ]; then
            echo "error: could not provision golangci-lint $want after $attempts attempt(s)." >&2
            echo "       This is a tool-download failure, not a lint finding." >&2
            exit 1
        fi
    fi

    # Both fields are "<major> <minor>", or empty when the shape was not
    # recognised. Unrecognised means silent rather than refusing: this is a
    # diagnosis attached to a provisioning step, and a linter that runs must not
    # be blocked because upstream reworded its version banner.
    #
    # `|| true` on both, because these run under `set -euo pipefail` and either
    # side can be absent: tool-gate-test.sh drives this recipe with a stub
    # install and no `go` on PATH, where the substitution exits 127 and takes
    # the whole recipe with it. A missing tool is the same case as an
    # unrecognised banner — no comparison to make, not an accusation.
    built="$({{quote(golangci)}} version 2>/dev/null | sed -n 's/.*built with go\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p' | head -n 1 || true)"
    here="$(go env GOVERSION 2>/dev/null | sed -n 's/^go\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p' || true)"
    if [ -n "$built" ] && [ -n "$here" ]; then
        built_major="${built%% *}"; built_minor="${built##* }"
        here_major="${here%% *}"; here_minor="${here##* }"
        if [ "$built_major" -lt "$here_major" ] \
            || { [ "$built_major" -eq "$here_major" ] && [ "$built_minor" -lt "$here_minor" ]; }; then
            echo "error: golangci-lint $want is built with go${built_major}.${built_minor}, and this" >&2
            echo "       machine's toolchain is go${here_major}.${here_minor}. A linter built with an older Go" >&2
            echo "       cannot load newer source; it panics inside go/types, and that stack" >&2
            echo "       trace names neither the pin nor the cause." >&2
            echo "" >&2
            echo "       The cause is almost always a STALE BRANCH BASE. just reads the pin" >&2
            echo "       'golangci_version' from the justfile of the tree you are standing in," >&2
            echo "       and this tree pins $want. A branch based before the commit that last" >&2
            echo "       bumped that line still provisions the older linter." >&2
            echo "" >&2
            echo "       Remedy: git fetch origin && git merge origin/master" >&2
            echo "       If this branch genuinely cannot take master yet, the run of record is" >&2
            echo "         GOTOOLCHAIN=go${built_major}.${built_minor} just lint-new" >&2
            echo "       which is a real run and not a bypass (bd gqlc-6rf3)." >&2
            exit 1
        fi
    fi

# provisions the pinned shellcheck into the gitignored .bin/ when missing or
# version-mismatched, exactly as ensure-golangci does: the happy path is a
# ~10ms version check and nobody installs the linter by hand. Upstream ships
# only release binaries, so this is a download rather than a build.
[private]
ensure-shellcheck:
    #!/usr/bin/env bash
    set -euo pipefail
    want="{{shellcheck_version}}"
    have="$({{quote(shellcheck)}} --version 2>/dev/null | sed -n 's/^version: //p' || true)"
    if [ "$have" = "${want#v}" ]; then
        exit 0
    fi
    echo "provisioning shellcheck $want into .bin/" >&2
    mkdir -p {{quote(justfile_directory() + "/.bin")}}
    stage="$(mktemp -d)"
    trap 'rm -rf "$stage"' EXIT
    # Upstream releases name the OS with the kernel's own spelling lowercased —
    # `linux`, `darwin`. Deriving it from `uname -s` rather than hardcoding
    # `linux` lets macOS provision the same way; an unsupported kernel is named
    # and refused rather than downloaded blindly to fail on extract.
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        linux|darwin) ;;
        *)
            echo "error: ensure-shellcheck has no release mapping for kernel '$os'." >&2
            echo "       Supported: linux, darwin. Install shellcheck $want by hand or extend this recipe." >&2
            exit 1
            ;;
    esac
    curl --proto '=https' --tlsv1.2 -sSfL --retry 5 --retry-all-errors --retry-delay 2 \
        "https://github.com/koalaman/shellcheck/releases/download/$want/shellcheck-$want.$os.$(uname -m).tar.xz" \
        | tar -xJ -C "$stage"
    install -m 0755 "$stage/shellcheck-$want/shellcheck" {{quote(shellcheck)}}

# provisions the pinned ruff into the gitignored .bin/, exactly as
# ensure-shellcheck does. Upstream ships a static binary per target triple, so
# this is a download and not a pip install: there is no virtualenv here, nothing
# resolves a dependency graph, and the happy path is a ~10ms version check.
#
# The asset name embeds `uname -m` unchanged because ruff's triples use the same
# spellings the kernel does (x86_64, aarch64). shellcheck's asset does too, so
# the two lines are deliberately the same shape.
[private]
ensure-ruff:
    #!/usr/bin/env bash
    set -euo pipefail
    want="{{ruff_version}}"
    have="$({{quote(ruff)}} --version 2>/dev/null | sed -n 's/^ruff //p' || true)"
    if [ "$have" = "$want" ]; then
        exit 0
    fi
    echo "provisioning ruff $want into .bin/" >&2
    mkdir -p {{quote(justfile_directory() + "/.bin")}}
    stage="$(mktemp -d)"
    trap 'rm -rf "$stage"' EXIT
    curl --proto '=https' --tlsv1.2 -sSfL --retry 5 --retry-all-errors --retry-delay 2 \
        "https://github.com/astral-sh/ruff/releases/download/$want/ruff-$(uname -m)-unknown-linux-gnu.tar.gz" \
        | tar -xz -C "$stage"
    install -m 0755 "$stage/ruff-$(uname -m)-unknown-linux-gnu/ruff" {{quote(ruff)}}

# ruff over the Python in .github/scripts (bd gqlc-tqi4).
#
# Until this recipe existed, NO linter, formatter or syntax check ran over any
# Python in this tree. Not ruff, not flake8, not pylint, not mypy, not black,
# not `py_compile`. Measured by grepping the justfile and every workflow for
# each of those names on 2026-08-23: nothing. `lint-hooks` reaches these two
# files and skips them by design, because shellcheck only supports shell.
#
# What that left unchecked is not incidental code. `check-pr-closes.py` is the
# gate deciding what a pull request may claim to close, run from the `tidy` job,
# which is a required status context — and since ADR 0003 a green CI is the only
# merge gate this repository has. Its own suite was the whole of its cover, so a
# NameError on a branch no row reaches, an unused import, or a shadowed builtin
# all shipped green. FALSIFIED before this landed and again after: an undefined
# name added to a function nothing calls in check-pr-closes.py leaves the tree
# green at HEAD~ and reports `F821 Undefined name` at rc=1 here.
#
# The rule set is pinned in .github/ruff.toml and passed with `--config`, so no
# config anywhere else in this tree or on the machine can widen or narrow it.
# That file argues its own location and its own `select`.
#
# Directory taken as a parameter for the same reason lint-hooks takes one: so a
# second Python directory can be linted without this recipe learning its name,
# and so the empty case below can be reached from a test.
#
# The empty case FAILS. A glob that matches nothing lints nothing and exits 0,
# which on every dashboard is the shape of a clean tree — the same fail-open
# refusal test-hooks and lint-hooks make.
lint-python dir=".github/scripts": ensure-ruff
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{dir}}"
    if [ ! -d "$dir" ]; then
        echo "error: '$dir' is not a directory, so ruff has nothing to lint" >&2
        exit 1
    fi
    shopt -s nullglob globstar
    files=("$dir"/**/*.py)
    if [ "${#files[@]}" -eq 0 ]; then
        echo "error: no .py file found under $dir, so ruff ran over nothing and exited 0" >&2
        echo "       over it — indistinguishable from every file passing. Either the" >&2
        echo "       scripts moved, or this is not the repository root (bd gqlc-tqi4)." >&2
        exit 1
    fi
    echo "ruff {{ruff_version}} over ${#files[@]} python file(s) under $dir:"
    printf '  %s\n' "${files[@]}"
    {{ruff}} check --no-cache \
        --config {{quote(justfile_directory() + "/.github/ruff.toml")}} -- "${files[@]}"

# shellcheck over the hooks tree (bd gqlc-jhi2). The hooks carry `# shellcheck
# disable=` directives over deliberate exceptions — the SC2086 disable in
# .githooks/bd-gh-sync's _push_batch, over the unquoted `bd github push $1` that
# splits a bead id list into argv words on purpose. Named rather than cited by
# line: `grep -n SC2086 .githooks/bd-gh-sync` finds it after any edit, and the
# line number this comment used to carry had already rotted twice. With no
# linter in the tree those directives read as enforced and are comments, and
# every SC-class defect the exception is carved out of goes unchecked with
# them. This repo has shipped three of that class.
#
# Files are selected by shebang rather than by a list, so a hook added tomorrow
# is linted without anyone remembering to name it here — and the two ways that
# selection can quietly shrink are both fatal rather than silent: a tree that
# yields no shell script at all, and a file whose shebang the test does not
# recognise. Skipping the latter is how a gate ends up green over a set nobody
# looked at, which is the defect this recipe exists to close.
#
# The directory is an argument so the recipe can be exercised over a throwaway
# tree (.githooks/tests/lint-hooks-test.sh); CI and developers take the default.
lint-hooks dir=".githooks": ensure-shellcheck
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{dir}}"
    if [ ! -d "$dir" ]; then
        echo "error: '$dir' is not a directory, so shellcheck has nothing to lint" >&2
        echo "       and this gate is watching nothing (bd gqlc-jhi2)." >&2
        exit 1
    fi

    scripts=()
    unclassified=()
    while IFS= read -r f; do
        head=""
        IFS= read -r head <"$f" || true
        case "$head" in
            "#!"*[\ /]sh | "#!"*[\ /]bash | "#!"*[\ /]dash | "#!"*[\ /]ksh)
                scripts+=("$f") ;;
            # Python is skipped because the linter only supports shell: SC1071
            # on a python hook would have to be silenced globally, which turns
            # it off for the shell files too.
            #
            # This comment does not open with the tool's name on purpose. A
            # comment beginning that word inside a case branch is read as a
            # directive in the wrong place (SC1124, an ERROR), and this recipe
            # body is itself linted now (bd gqlc-wprl).
            "#!"*python*) ;;
            *) unclassified+=("$f") ;;
        esac
    done < <(find "$dir" -type f | sort)

    if [ "${#unclassified[@]}" -ne 0 ]; then
        echo "error: these files under $dir carry no shebang this recipe recognises, so it" >&2
        echo "       cannot say whether shellcheck should be watching them (bd gqlc-jhi2):" >&2
        printf '         %s\n' "${unclassified[@]}" >&2
        echo "       Give the file a shell or python shebang, or teach the case above about it." >&2
        exit 1
    fi
    if [ "${#scripts[@]}" -eq 0 ]; then
        echo "error: no shell script found under $dir, so shellcheck ran over nothing and" >&2
        echo "       this gate is watching nothing (bd gqlc-jhi2)." >&2
        exit 1
    fi

    # Printed, not just counted: the standing evidence in a CI log that the set
    # under the gate is the set anyone reviewing it expects.
    echo "shellcheck {{shellcheck_version}} over ${#scripts[@]} shell script(s) under $dir:"
    printf '  %s\n' "${scripts[@]}"
    {{shellcheck}} -- "${scripts[@]}"

# shellcheck over the justfile's OWN recipe bodies (bd gqlc-wprl).
#
# A shebang recipe body is a bash script with `set -euo pipefail`, several of
# them 60+ lines long and carrying this repo's gate logic, and until now no
# linter read a line of it. The bug class that motivated this is not exotic: a
# collision guard shipped matching newline-delimited `sort -u` output with the
# space-delimited `case " $a $b " in *" $x "*)` idiom, so it could not fire, and
# it shipped in the commit whose job was to close a no-coverage finding. Scalar
# versus array is exactly what a linter tracks and a reader does not.
#
# Bodies are taken from `just --dump --dump-format json` rather than from a
# reader of my own, so the set under the gate is just's own parse. Only bodies
# whose first line is a shell shebang are enrolled: a non-shebang recipe is a
# sequence of independent one-line shell invocations, not a script, and
# concatenating those lines into one file would invent both a scope and a
# control flow that never exist at runtime.
#
# `{{{{...}}}}` becomes the literal token INTERP. That is the one place this
# gate reads something other than what runs, and it is why severity stops at
# `warning`: the info and style bands are dense with quoting advice about that
# token. Errors and warnings are not artefacts of the substitution.
#
# No check is excluded. SC2194 (a constant case word) was the one candidate,
# raised by `case "{{{{mode}}}}" in` in tmp-reap — and that reading was right: the
# value was pasted in raw, so a mode carrying a `)` was shell syntax there
# rather than data. Binding it through `{{{{quote(mode)}}}}` fixed the recipe and
# retired the exclusion with it (bd gqlc-4seg).
#
# The justfile read is an argument so the recipe can be exercised over a
# throwaway one, the same way lint-hooks takes its directory. The half about
# lint-hooks still holds — `lint` calls it three times with three directories.
# This parameter's own exerciser was internal/tools/ciguard/justbodies_test.go,
# deleted with the CI scaffolding in PR #1595, and nothing passes an argument
# here today: `lint` is the only caller and it takes the default. So the
# parameter is vestigial rather than load-bearing, and whether it goes or gains
# an exerciser is bd gqlc-gu7ao.
[private]
lint-just file=justfile(): ensure-shellcheck
    #!/usr/bin/env bash
    set -euo pipefail
    file="{{file}}"
    if [ ! -f "$file" ]; then
        echo "error: '$file' is not a file, so there are no recipe bodies to lint and this" >&2
        echo "       gate is watching nothing (bd gqlc-wprl)." >&2
        exit 1
    fi
    work="$(mktemp -d)"
    trap 'rm -rf "$work"' EXIT

    # `just --dump` resolves the whole file, so a justfile that stopped parsing
    # dies here rather than yielding an empty recipe set that lints clean.
    just --justfile "$file" --working-directory "$(dirname -- "$file")" \
        --dump --dump-format json >"$work/dump.json"

    jq -r '
        def flat: map(if type == "string" then . else "INTERP" end) | join("");
        .recipes | to_entries[]
        | select((.value.body | length) > 0)
        | select((.value.body[0] | flat) | test("^#!.*(bash|sh)$"))
        | .key + "\t" + ((.value.body | map(flat) | join("\n")) | @base64)
    ' "$work/dump.json" >"$work/index"

    bodies=()
    while IFS="$(printf '\t')" read -r name encoded; do
        printf '%s' "$encoded" | base64 -d >"$work/$name.sh"
        bodies+=("$work/$name.sh")
    done <"$work/index"

    if [ "${#bodies[@]}" -eq 0 ]; then
        echo "error: no shebang recipe body was extracted from the justfile, so this gate" >&2
        echo "       ran shellcheck over nothing and exited 0 over it — indistinguishable" >&2
        echo "       from every body being clean. Either every recipe lost its shebang, or" >&2
        echo "       just's json dump changed shape (bd gqlc-wprl)." >&2
        exit 1
    fi

    # Printed by recipe name rather than by temp path, because the path is a
    # throwaway and the name is what a reader has to go and open.
    echo "shellcheck {{shellcheck_version}} over ${#bodies[@]} justfile recipe body/bodies:"
    cut -f1 <"$work/index" | sed 's/^/  /'
    {{shellcheck}} --severity=warning -- "${bodies[@]}"

# .golangci.yml's run.build-tags list must be the tags this tree actually uses.
#
# The list is hand-maintained and the linter needs it: a file behind a build tag
# the config does not name is a file golangci-lint never loads, and it reports
# success over code it has not read — green because it was looking at less. It
# grew to two entries the day test/data/tagblind landed, and it grew by one every
# time a constrained directory was added, which made it a manual mirror of a set
# `just vuln` derives from a filesystem walk (bd gqlc-oxne) — two derivations of
# one fact, one automatic and one remembered.
#
# That key is now the VOCABULARY, not a mirror of one. The derivation reads it
# and refuses any constraint term it cannot place — not a platform value, not a
# go1.N tag, not toolchain-owned, not declared there — so a tag in the tree but
# not in the config never reaches a comparison here: `scope tags` fails first and
# names the file carrying it (bd gqlc-e7oq).
#
# This recipe therefore compares in ONE direction, because the other one cannot
# report anything. `derived` holds the terms classify placed as classCustom, and
# it places a term as classCustom only if `declared` holds it; `configured` IS
# `declared`, read by the same function on the same root. derived is a subset of
# configured on every tree, so `comm -23 derived configured` was empty by
# construction rather than by corpus. The clause that read it claimed to fire
# "the day something reinstates a default case in the classification", and it
# does not: with main.go's `return classUnknown` changed to `return classCustom`
# this recipe exited 0. Nor can any tree witness it, because the undeclared term
# that would fill the set makes the unmutated derivation refuse the file carrying
# it — a `//go:build zzmystery` file exits 1 at `scope tags`, measured. The
# default case is held out by `just test` instead, on
# TestConstraintTagsRefusesATermItCannotPlace and
# TestAnEmptyVocabularyFailsClosedWithoutAGradingClause, both of which redden
# under that mutation.
#
# The direction that remains is the live one: a tag in the config but not in the
# tree is a line describing nothing, which pre-accepts whichever constrained
# directory is added under that spelling next without anyone deciding it should
# be linted. It is also the backstop the tag derivation leans on for a GOOS
# landing in run.build-tags, so it carries a witness of its own below — this
# tree has nothing stale in it, and a clause whose only observable behaviour is
# saying nothing is one that survives being flipped or deleted.
#
# One reader for the key — `scope declared` — and no emptiness clause guarding
# it here, because a reader that goes quiet fails closed on its own: an empty
# vocabulary places nothing, so the derivation stops on the first constrained
# file in the tree rather than producing an empty set that agrees with an empty
# config — which would be green because both sides went missing at once.
# `golangci-lint run` must keep reporting the `formatters:` block as issues.
#
# This is the only server-side enforcement of gofumpt and gci in the whole
# repository, and it is not obvious that it is. The formatters have recipes of
# their own (`just fmt`, `just fmt-check`), they have their own block in
# .golangci.yml, and NO workflow calls fmt-check — `fmt-check` is not a required
# status context. What makes unformatted Go unmergeable is a property of a
# pinned third-party binary: `run` reports `formatters:` entries as ordinary
# issues, so the required `lint` context reddens on them.
#
# Measured at the pin, v2.13.1 (bd gqlc-lsku, 2026-08-23). Nothing held it. A
# golangci-lint bump that stops reporting formatters through `run`, or an edit
# moving gofumpt and gci out of `formatters:`, takes that enforcement away and
# every check stays green — the tell would be unformatted Go reaching master,
# noticed by a human. This recipe is bd gqlc-sh4j, which is that property made
# to fail.
#
# It asserts by DOING, not by reading the config: a throwaway module outside the
# tree, this repository's own .golangci.yml copied into it, and three runs — a
# pristine control that must exit 0, a gofumpt violation that must be named, and
# a gci violation that must be named. The control is what stops the two
# violations passing for the wrong reason; without it a linter that refuses
# everything, or a config that fails to load, reads exactly like a working gate.
#
# Outside the tree deliberately. A probe module under test/data would be seen by
# `go list ./...`, by modscope's module walk and by the discovery-probe sweep,
# and the sweep's own comment explains what one leaked probe costs.
#
# ~1s measured locally with a cold cache, one tiny package.
[private]
check-golangci-formatters-report: ensure-golangci
    #!/usr/bin/env bash
    set -euo pipefail
    probe="$(mktemp -d "{{scratch_root}}/gqlc-fmtprobe-XXXXXX")"
    trap 'rm -rf "${probe}"' EXIT
    cp {{quote(justfile_directory() + "/.golangci.yml")}} "${probe}/.golangci.yml"
    printf 'module gqlcfmtprobe\n\ngo 1.25\n' >"${probe}/go.mod"

    # The pristine file. It carries a package comment and a doc comment on the
    # one exported symbol because .golangci.yml runs revive, and a control that
    # exits 1 for an unrelated reason witnesses nothing.
    cat >"${probe}/probe.go" <<'PROBE'
    // Package gqlcfmtprobe is written by a gate, not by a person.
    package gqlcfmtprobe

    import (
    	"fmt"
    	"os"
    )

    // Probe returns a string so that the imports above are used.
    func Probe() string {
    	return fmt.Sprint(len(os.Args))
    }
    PROBE
    # just indents a shebang recipe's body, heredoc included, so the leading four
    # spaces come back off here. A tab-indented line inside the Go source would
    # be mangled by a blanket strip, so only the four spaces just added are cut.
    sed -i 's/^    //' "${probe}/probe.go"
    cp "${probe}/probe.go" "${probe}/probe.clean"

    # Cache under the probe so a formatter verdict is never served out of this
    # repository's cache, and so the directory is removed with the trap.
    run_probe() {
        ( cd "${probe}" && GOLANGCI_LINT_CACHE="${probe}/cache" \
            {{quote(lint_lock)}} {{quote(golangci)}} run ./... 2>&1 ) || return $?
    }

    if ! control="$(run_probe)"; then
        echo "error: golangci-lint refused a pristine probe package, so the two violation" >&2
        echo "       runs below would exit 1 for a reason that has nothing to do with the" >&2
        echo "       formatters and this gate would pass while asserting nothing" >&2
        echo "       (bd gqlc-sh4j). What it said:" >&2
        printf '%s\n' "${control}" | sed 's/^/         /' >&2
        exit 1
    fi

    # Each violation is introduced on its own, from the clean file, so a run
    # names one formatter and the other's silence is visible.
    check_formatter() {
        local formatter="${1}" out
        if out="$(run_probe)"; then
            echo "error: a ${formatter} violation was written into the probe package and" >&2
            echo "       'golangci-lint run' exited 0 over it. That command is the ONLY" >&2
            echo "       server-side enforcement of gofumpt and gci in this repository:" >&2
            echo "       fmt-check is not a required status context, and no workflow calls" >&2
            echo "       it. Unformatted Go is now mergeable (bd gqlc-sh4j)." >&2
            exit 1
        fi
        case "${out}" in
            *"(${formatter})"*) ;;
            *)  echo "error: 'golangci-lint run' refused the probe package, but did not name" >&2
                echo "       ${formatter} in what it printed — so whatever reddened it, it was" >&2
                echo "       not the formatter this row is about (bd gqlc-sh4j):" >&2
                printf '%s\n' "${out}" | sed 's/^/         /' >&2
                exit 1
                ;;
        esac
    }

    # gofumpt: a blank line straight after a function's opening brace.
    sed 's/^func Probe() string {$/func Probe() string {\n/' \
        "${probe}/probe.clean" >"${probe}/probe.go"
    check_formatter gofumpt

    # gci: the import block reordered into two sections.
    printf '%s\n' '/^\t"fmt"$/{N;s/.*/\t"os"\n\n\t"fmt"/}' >"${probe}/gci.sed"
    sed -f "${probe}/gci.sed" "${probe}/probe.clean" >"${probe}/probe.go"
    check_formatter gci

[private]
check-golangci-build-tags: sweep-discovery-probes
    #!/usr/bin/env bash
    set -euo pipefail
    scope() { go run ./internal/tools/modscope "$@"; }
    lines() { [ -n "${1}" ] && printf '%s\n' "${1}" || true; }

    modules_raw="$(scope modules)" || exit 1
    mapfile -t modules <<<"${modules_raw}"
    derived=""
    for m in "${modules[@]}"; do
        [ -n "${m}" ] || continue
        tags="$(scope tags "${m}")" || exit 1
        derived+="${tags}"$'\n'
    done
    derived="$(lines "${derived}" | sed '/^$/d' | sort -u)"

    configured="$(scope declared)" || exit 1
    configured="$(lines "${configured}" | sed '/^$/d' | sort -u)"

    # The clause is a function so the witness below can RUN it. What covered it
    # before was a Go test recomputing `comm -13` over the same two sets, and a
    # recomputation cannot see the recipe: with the direction flipped to
    # `comm -23`, and separately with the refusal deleted, that test, `just lint`
    # and this recipe all stayed green. Measured, on this branch, which is where
    # this recipe was added — there is no older version of it to inherit the gap
    # from.
    refuse_stale() {
        local derived="${1}" configured="${2}" stale
        stale="$(comm -13 <(lines "${derived}") <(lines "${configured}") || true)"
        [ -n "${stale}" ] || return 0
        echo "error: these build tags are in .golangci.yml's run.build-tags but constrain no file" >&2
        echo "       in this tree, so the entries describe nothing and pre-accept whatever is" >&2
        echo "       added under that spelling next (bd gqlc-oxne):" >&2
        lines "${stale}" | sed 's/^/         /' >&2
        return 1
    }

    # WITNESS: the same shape as test-codegen-fence's and check-codegen-external-tests'
    # probe modules, and for the same reason. This clause is the single-fault
    # backstop the tag derivation's comments in internal/tools/modscope/main.go
    # lean on, and on a tree with nothing stale it prints nothing on every run —
    # a guard whose passing case is silence is one that nothing distinguishes
    # from a deleted one. So a term no file in this tree constrains is put into
    # the configured set on every invocation, CI included, and the clause must
    # both refuse it and name it.
    probe="zzstaleprobe"
    # Whole-line match, not the `case " ${arr[*]} "` idiom the rest of this file
    # uses: `derived` and `configured` are `sort -u` scalars delimited by
    # NEWLINES, so a space-delimited pattern only ever matches a set of exactly
    # one term, and this clause could not fire at all (bd gqlc-oxne).
    uses_term() { grep -qxF "${1}" <<<"${derived}"$'\n'"${configured}"; }

    # The collision arm's passing case is silence too, and on a tree that does
    # not use the probe spelling it is only ever run in the negative — which is
    # what let it ship dead. So every term the two sets do contain is looked up
    # here first and must be found; an empty union makes that vacuous and is
    # refused rather than passed.
    control=0
    while read -r term; do
        [ -n "${term}" ] || continue
        control=$((control + 1))
        if ! uses_term "${term}"; then
            echo "error: the probe-collision lookup cannot find ${term}, which was just read out" >&2
            echo "       of the two sets it searches. It would not find ${probe} either, so the" >&2
            echo "       collision arm below is dead and the witness after it is unprotected" >&2
            echo "       (bd gqlc-oxne)." >&2
            exit 1
        fi
    done < <(printf '%s\n%s\n' "${derived}" "${configured}" | sed '/^$/d')
    if [ "${control}" -eq 0 ]; then
        echo "error: both the derived and the configured tag sets are empty, so the" >&2
        echo "       probe-collision lookup was never exercised and this whole recipe" >&2
        echo "       compared nothing against nothing (bd gqlc-oxne)." >&2
        exit 1
    fi

    if uses_term "${probe}"; then
        echo "error: ${probe} is a term this tree really uses, so the witness below is" >&2
        echo "       measuring the ordinary case. Rename the probe." >&2
        exit 1
    fi

    probed="$(printf '%s\n%s\n' "${configured}" "${probe}" | sed '/^$/d' | sort -u)"
    if witness="$(refuse_stale "${derived}" "${probed}" 2>&1)"; then
        echo "error: ${probe} was put in the configured set, no file in this tree constrains it," >&2
        echo "       and the stale clause accepted it — so that clause is not comparing the two" >&2
        echo "       sets in the direction it claims to. derived is a subset of configured on" >&2
        echo "       every tree, so the other direction is empty by construction and exits 0" >&2
        echo "       over anything at all (bd gqlc-oxne)." >&2
        exit 1
    fi
    case "${witness}" in
        *"${probe}"*) ;;
        *)  echo "error: the stale clause refused, but did not name ${probe} in what it printed," >&2
            echo "       so a real stale entry is refused without anyone being told which one:" >&2
            printf '%s\n' "${witness}" | sed 's/^/         /' >&2
            exit 1
            ;;
    esac

    refuse_stale "${derived}" "${configured}" || exit 1

# Clears discovery probes a previous run could not clean up after itself.
#
# `just vuln`, test-codegen-fence and check-codegen-external-tests each mktemp a
# throwaway module under test/data to witness that the module set is read off
# the tree rather than remembered, and each removes its own on the way out. That
# cleanup is a shell trap, and a trap cannot run under SIGKILL — the routine end
# of a run killed for taking too long, or by a session that hit a quota.
#
# What a survivor costs is out of proportion to how it got there. A probe is a
# go.mod with no Go file beneath it, which is a module whose walk comes back
# empty, which modscope refuses by design (bd gqlc-s3lt) — so one leaked probe
# stops `just lint`, `just vuln`, test-codegen-fence and
# check-codegen-external-tests, and it stops them with a message about a broken
# walk rather than about itself.
#
# Covers every declared probe name, not the calling recipe's own. A leftover
# fence probe stops check-codegen-external-tests, which test-codegen-fence
# depends on, so the run that would have cleaned that probe up dies before it
# reaches its own trap.
#
# This recipe only clears what it is run before, and what runs it is four
# dependency edges in this file. internal/tools/modscope/justfile_test.go reads
# them off this file and refuses a recipe whose body spells modscope's package
# path and does not reach this one, directly or through another recipe. A
# recipe that runs modscope without spelling that path is outside what it reads
# (bd gqlc-wkio): a fifth caller introduced that way would not be required to
# reach this recipe. A recipe behind a header shape that file's reader reads
# differently from just is outside what it reads too — a parameter default
# spelling `:=` was one until that reader learned to find the colon outside a
# default, and a header continued with a trailing backslash was another until it
# learned to join the lines onto one. The shapes it is known to still read
# differently are listed there. That list is what has been looked for, not a
# boundary anyone has proved, so the check that does not rest on it is
# TestParseJustfileAgreesWithJustOnThisJustfile: it reads this file with just
# and with that reader and reports where the two disagree. Something is needed
# there, because that file's
# dangling-dependency control reaches a missed header only through the recipes
# that depend on that header. Measured
# before justfile_test.go existed: dropping the edge from
# check-golangci-build-tags left the tree green and silent, and the gate that
# lost the edge then failed on a leaked probe with a message about a broken
# walk (bd gqlc-c7o7).
#
# The limit is concurrency. Two of these recipes running against ONE worktree at
# the same time would clear each other's live probe, and the witness below plants
# under a fixed name both would collide on; they are not safe to run concurrently
# in a single tree. Separate worktrees, which is how this repo runs agents, are
# unaffected.
[private]
sweep-discovery-probes:
    #!/usr/bin/env bash
    set -euo pipefail
    names=({{discovery_probes}})
    trap 'rm -rf test/data/*.sweepwitness' EXIT

    # Three facts have to agree before the names below mean anything: the
    # `*_probe` variables, the concatenation this recipe reads them through, and
    # the mktemp sites that create the probes. discovery_probes is written by
    # hand, so a probe variable can be declared, interpolated at its site, and
    # left out of the concatenation — the probe is still created and this sweep
    # stops clearing it, which is the state this block refuses.
    #
    # Read back off just's own evaluation and dump rather than this file's bytes,
    # so a commented-out declaration is not a declaration. A commented-out mktemp
    # site does still count as a site; that direction asks for one more
    # declaration than the tree needs.
    if [ "${#names[@]}" -eq 0 ]; then
        echo "error: discovery_probes expands to no names at all, so this sweep globs nothing," >&2
        echo "       looks nothing up, and reports success against a tree where every recipe's" >&2
        echo "       probe leaked (bd gqlc-oxne)." >&2
        exit 1
    fi

    evaluated="$('{{just_executable()}}' --justfile '{{justfile()}}' --evaluate)"
    dumped="$('{{just_executable()}}' --justfile '{{justfile()}}' --dump)"
    pairs="$(printf '%s\n' "${evaluated}" \
        | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*_probe\)  *:= "\(.*\)"$/\1\t\2/p')"
    if [ -z "${pairs}" ]; then
        echo "error: this justfile declares no *_probe variable, so the set discovery_probes is" >&2
        echo "       compared against below is empty and agrees with whatever discovery_probes" >&2
        echo "       happens to say (bd gqlc-oxne)." >&2
        exit 1
    fi

    # Each comparison below owns its accumulator and asks membership one name at
    # a time, so an edit reaches one comparison.
    names_nl=""
    for n in "${names[@]}"; do
        names_nl="${names_nl}${n}"$'\n'
    done

    declared_vars=""
    declared=""
    unswept=""
    while IFS=$'\t' read -r var val; do
        if [ -z "${val}" ]; then
            echo "error: ${var} evaluates to the empty string, so the probe it names is created" >&2
            echo "       as test/data/.XXXXXX — a spelling .gitignore does not cover and the" >&2
            echo "       glob below does not reach (bd gqlc-oxne)." >&2
            exit 1
        fi
        declared_vars="${declared_vars}${var}"$'\n'
        declared="${declared}${val}"$'\n'
        case $'\n'"${names_nl}" in
            *$'\n'"${val}"$'\n'*) ;;
            *) unswept="${unswept} ${val}" ;;
        esac
    done <<<"${pairs}"

    unknown=""
    for n in "${names[@]}"; do
        case $'\n'"${declared}" in
            *$'\n'"${n}"$'\n'*) ;;
            *) unknown="${unknown} ${n}" ;;
        esac
    done

    # site_re is assembled from two pieces: the dump this searches includes this
    # recipe's own body, and a whole pattern written out here would match itself.
    #
    # One regex reads the sites, so what the refusal below accepts and what the
    # extraction below reads are the same shape by construction. The trailing dot
    # is part of that shape because the sweep glob is "test/data/${n}.*": a site
    # that interpolates its probe variable and then runs straight into the
    # mktemp template, with no dot between, makes a directory neither that glob
    # nor .gitignore's own dotted rules match.
    #
    # The allocators are enumerated rather than left at `mktemp -d`, which used
    # to be the only idiom this recognised: a probe put under test/data by
    # `mkdir -p` or `install -d` was a site to none of the checks here, so the
    # declared names were not held to it and the sweep glob did not clear it
    # (bd gqlc-lj9s).
    #
    # An enumeration is still a list, so it is backed by the refusal that
    # follows it rather than trusted: a line that names a *_probe variable
    # beside test/data and is not one of these shapes FAILS, instead of being
    # invisible the way an unlisted allocator was. What remains out of reach is
    # a probe created by something this justfile only calls — Go code, a script
    # — which names nothing here and appears in no dump.
    alloc_re="\(mktemp -d\|mkdir -p\|mkdir\|install -d\)"
    site_re="${alloc_re} ""test/data/"
    site_var_re="${site_re}[{][{] *\([A-Za-z_][A-Za-z0-9_]*\) *[}][}]\."

    # Split across two string literals for the same reason site_re is: this
    # recipe's body is inside the dump being searched, and a pattern written out
    # whole here would match the line it is written on.
    probe_ref_re="test/data/""[{][{] *[A-Za-z_][A-Za-z0-9_]*_pro""be"
    stray_sites="$(printf '%s\n' "${dumped}" | grep -e "${probe_ref_re}" \
        | grep -v -e "${site_re}" || true)"
    if [ -n "${stray_sites}" ]; then
        echo "error: these lines name a probe variable under test/data through a command this" >&2
        echo "       recipe does not recognise as an allocator:" >&2
        printf '%s\n' "${stray_sites}" | sed 's/^/         /' >&2
        echo "       Teach alloc_re about it, or route the creation through one of the shapes" >&2
        echo "       it lists. An unrecognised allocator makes a probe that no check here" >&2
        echo "       holds and no glob here clears (bd gqlc-lj9s)." >&2
        exit 1
    fi

    odd_sites="$(printf '%s\n' "${dumped}" | grep -e "${site_re}" \
        | grep -v -e "${site_var_re}" || true)"
    if [ -n "${odd_sites}" ]; then
        echo "error: a probe site under test/data does not interpolate a probe variable and" >&2
        echo "       follow it with a dot:" >&2
        printf '%s\n' "${odd_sites}" | sed 's/^/         /' >&2
        echo "       Only that form is compared against the declared names and swept by the" >&2
        echo "       glob below, so a site in any other shape can name a probe this recipe" >&2
        echo "       does not clear with nothing here objecting (bd gqlc-oxne)." >&2
        exit 1
    fi

    # \2, not \1: alloc_re is a group of its own inside site_var_re, so the
    # probe variable is the second capture. The s/// delimiter is % rather than
    # | because alloc_re is an alternation and | inside the pattern would end
    # the expression early — silently, as an empty match.
    site_vars="$(printf '%s\n' "${dumped}" \
        | sed -n "s%^.*${site_var_re}.*%\2%p")"
    if [ -z "${site_vars}" ]; then
        echo "error: no recipe in this justfile creates a probe under test/data, so the names" >&2
        echo "       checked below are held to nothing and this sweep clears a thing no run" >&2
        echo "       makes (bd gqlc-oxne)." >&2
        exit 1
    fi
    unheld=""
    while IFS= read -r v; do
        case $'\n'"${declared_vars}" in
            *$'\n'"${v}"$'\n'*) ;;
            *) unheld="${unheld} ${v}" ;;
        esac
    done <<<"${site_vars}"
    if [ -n "${unheld}" ]; then
        echo "error: these justfile variables name a probe at an allocator site and are not spelled" >&2
        echo "       *_probe:${unheld}" >&2
        echo "       The comparison below reads *_probe variables only, so a probe created" >&2
        echo "       through one of these is not reached by it (bd gqlc-oxne)." >&2
        exit 1
    fi

    if [ -n "${unswept}" ] || [ -n "${unknown}" ]; then
        echo "error: the probe names this justfile declares and the names discovery_probes hands" >&2
        echo "       this sweep are different sets." >&2
        if [ -n "${unswept}" ]; then
            echo "       declared, not swept:${unswept}" >&2
            echo "       — a declared name left out of discovery_probes still gets a probe made" >&2
            echo "         at its mktemp site, and this recipe no longer clears it (bd gqlc-oxne)." >&2
        fi
        if [ -n "${unknown}" ]; then
            echo "       swept, not declared:${unknown}" >&2
            echo "       — a name in discovery_probes with no variable behind it is a spelling no" >&2
            echo "         site creates, so the .gitignore rule it demands guards nothing." >&2
        fi
        exit 1
    fi

    # A function so the witness below can RUN it rather than recompute what it
    # believes it does. The names come from the variable block at the top of this
    # file, and the block above holds that block to the sites, so this glob and
    # the sites that create the probes read one spelling.
    sweep() {
        local n d
        for n in "${names[@]}"; do
            for d in "test/data/${n}".*; do
                [ -e "${d}" ] || continue
                rm -rf "${d}"
                printf '%s\n' "${d}"
            done
        done
    }

    # Audible when it fires. A cleanup that removes three modules and says
    # nothing leaves a CI log in which it is indistinguishable from a cleanup
    # that did not run, and a leaked probe is evidence a run was killed —
    # something the next reader of that log wants told, not silently repaired.
    leaked="$(sweep)"
    if [ -n "${leaked}" ]; then
        echo "swept discovery probe(s) an earlier run left behind:"
        printf '%s\n' "${leaked}" | sed 's/^/  /'
    fi

    # .gitignore is the one copy of these names that cannot read the variable
    # block, so it is the one that can drift. A lookup, not a text comparison:
    # what matters is whether git would hide a leaked probe under this name, and
    # only git can answer that.
    #
    # Two questions, and both have to be yes. -q answers whether git would hide
    # the path; -v names the file whose rule matched. Neither alone is the
    # question: git's hide answer covers .git/info/exclude and core.excludesFile,
    # per-clone files no commit carries, so -q alone stays green while this repo's
    # own .gitignore loses its probe rules in a clone that happens to hide them.
    # And -v exits 0 on a NEGATED rule — it reports that a pattern matched, not
    # that the path is hidden — so -v alone reads "!/test/data/vulnprobe.*/" as
    # coverage for a probe git would list as untracked.
    #
    # Each name is recorded as it is looked up so the refusal below can compare
    # the names that reached the lookup against the names declared, rather than
    # count calls: a lookup run three times against one name leaves the other two
    # free to drift out of .gitignore with this recipe green (bd gqlc-eo46).
    looked_up=""
    covered() {
        local src
        looked_up="${looked_up}${1}"$'\n'
        git check-ignore -q "test/data/${1}.sweepwitness" || return 1
        src="$(git check-ignore -v "test/data/${1}.sweepwitness" | cut -d: -f1)" || return 1
        [ "${src}" = ".gitignore" ]
    }

    # WITNESS: same shape and same reason as check-golangci-build-tags'
    # zzstaleprobe above. On the ordinary tree there is nothing to sweep, so
    # this recipe's whole observable behaviour is silence — which is what
    # survives being deleted. So one probe-shaped directory per declared name is
    # planted on every run, CI included, and the sweep must remove all of them.
    #
    # The witness carries no go.mod: a leaked WITNESS must not be able to become
    # the empty-module failure it exists to test for.
    for n in "${names[@]}"; do
        mkdir -p "test/data/${n}.sweepwitness"
        if ! covered "${n}"; then
            echo "error: test/data/${n}.* is a discovery-probe name this justfile creates, and the" >&2
            echo "       repo's own .gitignore does not cover it — so the .gitignore copy of that" >&2
            echo "       name has drifted from the one in the variable block (bd gqlc-oxne), and" >&2
            echo "       a probe a killed run leaves behind now reads as untracked work nobody" >&2
            echo "       wrote. Add /test/data/${n}.*/ to .gitignore." >&2
            exit 1
        fi
    done

    # No refusal in this recipe is itself asserted on. Some of them overlap, so
    # deleting one is sometimes caught by another, but nothing here makes that
    # so. The regress stops at this level rather than terminating inside the
    # script; closing it takes a test that runs this recipe against a fixture
    # tree from outside it (bd gqlc-eo46).
    missed=""
    for n in "${names[@]}"; do
        case $'\n'"${looked_up}" in
            *$'\n'"${n}"$'\n'*) ;;
            *) missed="${missed} ${n}" ;;
        esac
    done
    if [ -n "${missed}" ]; then
        echo "error: the .gitignore coverage lookup was not called for:${missed}" >&2
        echo "       Each of those is a declared discovery-probe name, so each can drift out of" >&2
        echo "       .gitignore with this recipe still green (bd gqlc-eo46)." >&2
        exit 1
    fi

    # The lookup's passing case is silence, and on a tree where nothing has
    # drifted every call above returns covered — so the refusing branch is not
    # taken on a green run, which is what let this file's probe-collision arm
    # ship dead once already. A spelling no recipe here creates goes through the
    # same lookup on every run and must come back uncovered, so a lookup stuck at
    # "yes" is refused rather than believed.
    #
    # Planted on disk first. The probe rules in .gitignore end in a slash, and
    # git matches a directory-only pattern against a directory that exists on
    # disk, not against an absent path — so an unplanted control comes back
    # uncovered for a reason that holds for a declared name too. The mkdir leaves
    # the spelling as the difference between the control and the lookups above.
    mkdir -p "test/data/zzuncoveredprobe.sweepwitness"
    if covered "zzuncoveredprobe"; then
        echo "error: this repo's own .gitignore reports test/data/zzuncoveredprobe.* as ignored, a" >&2
        echo "       spelling no recipe here creates. The coverage lookup above answers yes to a" >&2
        echo "       name that is not a declared probe, so it would answer yes to a renamed probe" >&2
        echo "       too and the drift it exists to catch would pass (bd gqlc-oxne)." >&2
        exit 1
    fi

    sweep >/dev/null
    for n in "${names[@]}"; do
        [ -e "test/data/${n}.sweepwitness" ] || continue
        echo "error: the sweep left test/data/${n}.sweepwitness on disk, so it does not cover" >&2
        echo "       ${n} at all. That name is declared as a discovery probe, and every recipe" >&2
        echo "       depending on this one would go on reporting success with a probe still" >&2
        echo "       there — or die on modscope's empty-walk refusal (bd gqlc-s3lt)." >&2
        exit 1
    done

# full static analysis: golangci-lint over the Go tree (.golangci.yml) and
# shellcheck over the hooks + kingdom + CI-script trees, as linters + formatter
# diffs as issues
#
# .github/scripts is here because a developer must see the same verdict as CI
# does: those scripts run inside required contexts, and a tree where they are
# linted only on the runner is one where the first reader of a shellcheck
# finding is a red PR (bd gqlc-xqf6).
#
# lint-python is the Python half of that same argument, and it is newer: until
# bd gqlc-tqi4 no linter of any kind read the two .py files in .github/scripts,
# one of which is the PR-body merge gate.
#
# check-golangci-formatters-report rides here rather than anywhere else because
# `lint` is what the required context runs, and the property it holds is a
# property of the very next line: that `golangci-lint run` reddens on gofumpt
# and gci. Ahead of the lint, so a tree whose formatter enforcement has gone
# quiet says so before spending eighty seconds.
lint: ensure-golangci lint-hooks (lint-hooks "kingdom/bin") (lint-hooks ".github/scripts") lint-python lint-just check-golangci-formatters-report check-golangci-build-tags
    {{lint_lock}} {{golangci}} run

# Guard: the golangci-lint analysis cache must be non-empty after lint.
# Fails if GOLANGCI_LINT_CACHE in the justfile diverges from the path: in ci.yml (gqlc-b63).
lint-cache-check:
    @test -d .bin/golangci-cache && test -n "$(ls -A .bin/golangci-cache 2>/dev/null)" \
        || { echo "error: GOLANGCI_LINT_CACHE (.bin/golangci-cache) is empty or missing — justfile and ci.yml paths diverged"; exit 1; }

# lints only lines changed since the given rev — the fast pre-push variant
lint-new rev="origin/master": ensure-golangci
    {{lint_lock}} {{golangci}} run --new-from-rev {{rev}}

# rewrites formatting in place (gofumpt + gci, both bundled in golangci-lint)
fmt: ensure-golangci
    {{golangci}} fmt

# formatting check without writing; fails with a diff when unformatted
fmt-check: ensure-golangci
    {{golangci}} fmt --diff

# THE PRE-PR GATE SET: every required CI context that can run on this machine.
#
# It exists because a hand-written list of gates drifts and nothing tells you.
# citizen-protocol.md step 4 named three recipes — fmt-check, lint, test — while
# master required seven contexts, six of them reachable here across eleven arms.
# A citizen who ran the
# documented three and pushed then learned the rest one CI round trip at a time:
# measured on PR #1643, where all three were green and codegen-fence failed on
# three ireturn findings the root lint cannot reach by construction (bd
# gqlc-jq50, gqlc-s9bx). Naming ONE recipe in the playbook moves that drift here,
# next to the recipes it is about and in front of everyone who edits them.
#
# EVERY ARM RUNS EVEN AFTER ONE FAILS, and the failures are reported together at
# the end. Stopping at the first is precisely what a pre-PR check must not do:
# the cost this recipe exists to remove is the round trip, and three failures
# found one at a time are three round trips whether they are CI's or your own.
# `set -e` is therefore deliberately absent below.
#
# It is NOT a merge predicate and green here does not entitle anyone to skip CI.
# Some of what CI requires is not reachable from a developer's machine, and the
# summary says so on every run rather than leaving it to this comment:
#
#   live-smoke   `just test-codegen-live-neo4j` — needs Docker and pulls
#                container images. Runnable here (bd gqlc-tez0 measured the
#                live battery at ~30s), just not at the price the other arms
#                are; run it by hand when you touch the live battery.
#   tidy (part)  three of that job's seven steps read state that does not exist
#                before the PR: check-pr-closes.py wants the body,
#                check-pr-authors.sh the commit list, check-cron-freshness.sh
#                the Actions API. Unrunnable here by construction, not by
#                choice. The other four DO run — tidy-check and
#                bd-export-monotonic-local and check-label-lengths.py as their
#                own arms, and `just lint-hooks .github/scripts` because `just
#                lint` already depends on it.
#
# `just fmt-check` is an arm but is NOT a CI job: no workflow calls it. It is
# here because it prints a diff where `golangci-lint run` prints issues, and it
# costs a second. The enforcement of gofumpt and gci is `just lint` — which is
# a claim this repository gates rather than assumes, in
# check-golangci-formatters-report (bd gqlc-sh4j).
#
# On the town's own machine one arm is red on a clean master: `just vuln`
# refuses because this box's default Go is a distro build (`go1.27.0-X:nodwarf5`)
# that govulncheck cannot place a stdlib version on, so it would scan the
# largest attack surface in the binary and report nothing (bd gqlc-u91z). CI does
# not hit it because .github/actions/setup-go exports GOTOOLCHAIN from go.mod's
# own directive. Nothing here papers over that: the arm runs as CI runs it, the
# refusal names its own remedy, and the gap between the two is bd gqlc-irvs.
gates:
    #!/usr/bin/env bash
    set -uo pipefail
    failed=()
    contexts=()

    # $1 is the required CI context this arm stands for; the rest is the command.
    # The context is COLLECTED rather than restated in the summary below, because
    # a hardcoded coverage sentence is the same silent drift one level down: with
    # it, deleting the test-codegen-fence arm left this recipe green and still
    # claiming to cover codegen-fence (measured, bd gqlc-jq50).
    #
    # contexts is also the arm COUNT, rather than a second variable incremented
    # beside it. Two counters of one thing can disagree, and the disagreement is
    # exactly a run that grades fewer arms than it claims.
    run() {
        local ctx="$1"; shift
        contexts+=("${ctx}")
        echo ""
        echo "=== gates[${ctx}]: $*"
        if ! "$@"; then
            failed+=("$*")
        fi
    }

    run lint           just fmt-check
    run lint           just lint
    run lint           just lint-cache-check
    run lint           just vuln-root-residual
    run test           just test
    run codegen-fence  just test-codegen-fence
    run actionlint     just actionlint
    run tidy           just tidy-check
    run tidy           just bd-export-monotonic-local
    run tidy           python3 .github/scripts/check-label-lengths.py .beads/issues.jsonl
    run govulncheck    just vuln

    # Refuse BEFORE the summary, not after: the summary is a coverage claim, and
    # a run that graded nothing must not get to make one at all.
    ran="${#contexts[@]}"
    echo ""
    if [ "${ran}" -eq 0 ]; then
        echo "error: gates ran no arm at all, so it is green over nothing (bd gqlc-jq50)." >&2
        exit 1
    fi

    echo "gates: ran ${ran} arm(s) over required context(s):" \
         "$(printf '%s\n' "${contexts[@]}" | sort -u | tr '\n' ' ')"
    echo "gates: NOT covered, and CI still decides —"
    echo "       live-smoke        needs Docker: just test-codegen-live-neo4j"
    echo "       tidy (3 steps)    check-pr-closes.py, check-pr-authors.sh and"
    echo "                         check-cron-freshness.sh read a PR body, a PR's"
    echo "                         commit list and the Actions API. None exist here."

    if [ "${#failed[@]}" -ne 0 ]; then
        echo ""
        echo "gates: ${#failed[@]} of ${ran} FAILED — their output is above, in order:" >&2
        printf '  %s\n' "${failed[@]}" >&2
        exit 1
    fi
    echo "gates: all ${ran} passed."

# runs the whole suite (unit, golden snapshots, godog) in one shot. Independent
# of fetch-tck: the TCK is vendored, so there is no network at test time.
# go build link-checks package main, which has no tests and is otherwise
# only compile-checked by lint. -shuffle=on is deliberately absent: it defeats
# the test cache (the seed is regenerated per invocation and forms part of the
# test-binary args, so every run misses), and inter-test coupling in a codegen
# dev tool is a low-value gate relative to a ~2m40s tax on every push. Revisit
# if ordering coupling actually bites us.
test: check-hooks check-worktree-upstream check-shared-config check-claude-permission-mode check-beads-export check-tmp check-push-keepalive
    go build ./...
    go test ./...

# fails when go.mod/go.sum are not tidy
tidy-check:
    go mod tidy -diff

# fails when .beads/issues.jsonl regresses vs base (dropped or reopened issues).
# Motivated by bd gqlc-v2p: PR #422's blanket `git add -A` shipped a stale bd
# passive export that would have reopened two closed beads and dropped one.
# Deliberately NOT wired into `just test`: gate is PR-scoped, test runs on master
# pushes too (gqlc-5fm: a fatal recipe off master would break every CI run).
# Arg is passed through unchanged, so CI can hand it the exact base SHA and
# avoid a merge-base walk (which needs deep history).
bd-export-monotonic base:
    go run ./internal/tools/bdguard -base {{base}}

# dev-local convenience: compare against the merge-base with origin/master.
# Requires full history; assumes the dev worktree has it.
bd-export-monotonic-local:
    just bd-export-monotonic $(git merge-base HEAD origin/master)

# An orphan is an open GH issue no bead names, byte-identical in title AND body
# to an issue a bead does name, created seconds from it: .githooks/bd-gh-sync's
# push pass minted both for one bead and the ledger kept only one, so the close
# pass — which keys on external_ref — can never reach the other (bd gqlc-mmej,
# gqlc-mb8v). Deliberately not wired into `just test` or into any hook: it
# reaches the network, and the only reason to run it is that someone is about to
# read the answer.
#
# This recipe takes no parameters ON PURPOSE, and that is the whole of what
# keeps the reporting name off the write path. `-close` is a flag on the tool,
# so a recipe with a `*args` tail forwards it: this one carried one until bd
# gqlc-mb8v's review measured `just -n gh-orphans -close` rendering `go run
# ./internal/tools/ghorphan -close` while the `just --list` line beside it said
# "mutates nothing". With no parameter to take it, just reads a trailing
# `-close` as a second recipe name and stops at rc=1 before running anything.
# Pinned by TestTheReportingRecipeCannotBeHandedTheCloseFlag, which asks just
# rather than reading these lines.
#
# The cost is real and is paid on purpose: -window and -limit are now reachable
# through the acting recipe below or a direct `go run`, and not from this name.
# What each of them does to a verdict is written where they are read, in the
# tool's package comment — this line does not summarise it, because summarising
# it is how the claim this recipe used to carry got written.
#
# reports duplicate GH issues the bd↔GH sync minted twice; mutates nothing
gh-orphans:
    go run ./internal/tools/ghorphan

# Irreversible enough to be worth typing out: closing an issue is visible to
# everyone watching the repository, and a wrong close is undone by hand. Run
# `just gh-orphans` first and read every line, including the refusals — a
# refusal is a pair this tool will not decide.
#
# The body runs the tool directly. Spelling it `just gh-orphans -close` is what
# made the reporting name a write path, and it would need that recipe to take
# the parameter again.
#
# CLOSES the duplicates `just gh-orphans` reports, each pointing at its canonical
gh-orphans-close *args:
    go run ./internal/tools/ghorphan -close {{args}}

# The quality fence over every module in this tree that the root gates do not
# already cover: compile (go build), vet, module tidiness (go mod tidy -diff),
# and golangci-lint against the root config. Generated code must uphold the same
# linting + formatting standards as gqlc's own CI (owner directive, 2026-07-11);
# running golangci-lint from within a nested module discovers the root
# .golangci.yml via upward walk, giving parity for free. Used identically
# locally (post-generate) and in CI.
#
# The module set is DISCOVERED, not named (bd gqlc-oxne). It used to be the
# literal `test/data/codegen`, three times over, while `just vuln` beside it had
# already been taught to find its modules on disk — so a third module added to
# this tree got a vulnerability scan and no build, vet, tidy or lint at all. The
# weaker half of the pair was the generalised one, and adding a module was a
# silent downgrade with no gate reporting the asymmetry. Both halves now read
# internal/tools/modscope, so they cannot disagree about what a module is.
#
# The invariant being stated is "every module in this tree is built, vetted,
# tidied and linted", and it is split across recipes rather than duplicated: the
# root module is covered by `just test` (build), `just tidy-check` and `just
# lint`, and this covers every other. `.` is subtracted rather than skipped by
# name-matching so the subtraction is visible, and modscope refuses a checkout
# whose root is not a module, which is what makes the two halves a partition
# rather than two overlapping guesses.
#
# Each module's tags are derived too. `go vet` needs them or its analysers
# silently skip the constrained files (bd gqlc-3eyw); `go build` gets them as
# well, which is inert on today's corpus — test/data/codegen's tagged files are
# all _test.go and go build never compiles those — but reasoning from today's
# corpus is how bd gqlc-e7oq happened, and a nested module with constrained
# non-test files is exactly what this recipe now has to survive. golangci-lint
# reads its tags from .golangci.yml, which check-golangci-build-tags holds to
# the same derivation.
#
# check-codegen-external-tests runs ahead of the linter: both fail on the same
# regression, and only the guard names the scan it protects (ADR 0026).
test-codegen-fence: sweep-discovery-probes ensure-golangci check-codegen-external-tests
    #!/usr/bin/env bash
    set -euo pipefail
    scope() { go run ./internal/tools/modscope "$@"; }

    # The recipe's ONE derivation of "every module in this tree except the
    # root". A function rather than six lines inline, because the selftest below
    # has to run the same code over a changed tree — a set assembled at the only
    # place it is used can only ever be compared with itself. `|| return 1` on
    # the assignment because errexit is suppressed inside a command
    # substitution, measured on bash 5.3; see the note in `just vuln`.
    derive_fenced() {
        local raw m
        raw="$(scope modules)" || return 1
        fenced=()
        while IFS= read -r m; do
            case "${m}" in ""|".") continue ;; esac
            fenced+=("${m}")
        done <<<"${raw}"
    }

    # WITNESS: the fenced set is discovered, not named (bd gqlc-oxne).
    #
    # Nothing else here can say that. On today's tree the discovered set is one
    # module spelled `test/data/codegen`, which is the literal the discovery
    # replaced — so `fenced=("test/data/codegen")` written back over the
    # derivation fences the same module, prints the same line, and passes every
    # other assertion in this recipe. Two sets that agree on this tree are told
    # apart only by a tree they disagree on.
    #
    # So the tree is changed. A module is created on disk, the set is taken and
    # must hold it; the module is removed, the set is taken again and must not.
    # A hardcoded set fails the first clause, and a set measured once and cached
    # fails the second. Both run on every invocation, CI included, because a
    # witness that has to be remembered is not one.
    #
    # The probe carries a go.mod and nothing else, and it is gone before the
    # fencing loop below runs — which is also what the second clause checks.
    # Residue from a run this trap could not clean up is swept by the
    # sweep-discovery-probes dependency, not here.
    probe="$(mktemp -d test/data/{{fence_probe}}.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/{{fence_probe}}\n\ngo 1.26.5\n' >"${probe}/go.mod"
    derive_fenced || exit 1
    rm -rf "${probe}"
    trap - EXIT
    case " ${fenced[*]} " in
        *" ${probe} "*) ;;
        *)  echo "error: a module was created at ${probe} and the set this recipe fences did" >&2
            echo "       not change, so that set is not being read off the tree (bd gqlc-oxne)." >&2
            echo "       It was: ${fenced[*]}" >&2
            echo "       This is the bead's own failure and every other assertion here is blind" >&2
            echo "       to it: the discovered set and the literal it replaced are the same one" >&2
            echo "       module on this tree, so a named set fences the right thing by accident" >&2
            echo "       and goes on doing so until a second nested module is added — at which" >&2
            echo "       point that module is built, vetted, tidied and linted by nothing." >&2
            exit 1
            ;;
    esac
    derive_fenced || exit 1
    case " ${fenced[*]} " in
        *" ${probe} "*)
            echo "error: ${probe} is still in the fenced set after being removed from the tree," >&2
            echo "       so the set is a snapshot rather than a measurement (bd gqlc-oxne)." >&2
            echo "       Whatever cached it would cache a deleted module just as happily, and" >&2
            echo "       this recipe would then try to build a directory that is not there." >&2
            exit 1
            ;;
    esac

    if [ "${#fenced[@]}" -eq 0 ]; then
        echo "error: discovery found no module besides the root, so this recipe fenced nothing" >&2
        echo "       and would have exited 0 having built, vetted, tidied and linted no code" >&2
        echo "       (bd gqlc-oxne). Either discovery is broken, or the nested module was" >&2
        echo "       removed — in which case delete this recipe and its CI job together," >&2
        echo "       deliberately, rather than leaving a gate that watches an empty set." >&2
        exit 1
    fi

    # Printed rather than only counted: the standing evidence in a CI log that
    # the set under the fence is the set a reader expects, and the line that
    # changes the day a third module appears.
    echo "fencing ${#fenced[@]} nested module(s): ${fenced[*]}"
    for m in "${fenced[@]}"; do
        # `|| exit 1` rather than trusting `set -e`: errexit is suppressed inside
        # a command substitution, measured on bash 5.3, so a helper that died
        # here would otherwise read as a module asking for no tags — and a
        # module vetted without its tags is a module partly vetted (bd
        # gqlc-3eyw). The same rule governs `just vuln`; see the note there.
        tags_raw="$(scope tags "${m}")" || exit 1
        taglist="$(printf '%s\n' "${tags_raw}" | paste -sd,)"
        tagflag=()
        [ -z "${taglist}" ] || tagflag=(-tags "${taglist}")
        echo "fence: ${m}, tags [${taglist:-none}]"
        (cd "${m}" && go build "${tagflag[@]}" ./... && go vet "${tagflag[@]}" ./...)
        (cd "${m}" && go mod tidy -diff)
        (cd "${m}" && {{lint_lock}} {{golangci}} run)
    done

# Holds every nested module to the packaging that keeps it inside govulncheck's
# call graph (ADR 0026, bd gqlc-rohp). The fence is the only always-run required
# gate over those modules, so it is the only place the convention can be held.
#
# Two assertions, because a guard that only pins today's spelling is not a
# guard. The first is the convention: every _test.go anywhere under a nested
# module declares an external test package. The second is the consequence that
# actually matters: the package closure govulncheck will load — the non-test
# deps plus every external test package's deps — still reaches
# testcontainers-go. It catches what the first cannot, a dropped codegen_live
# tag or a move of the container code behind a different one.
#
# Both are driven off discovery rather than off the literal `test/data/codegen`
# they used to name (bd gqlc-oxne), and the second finds ITS modules the same way:
# a go.mod requiring testcontainers-go is a module holding a live battery, so
# moving the battery moves this assertion with it instead of leaving it pointed
# at an empty directory. None means the battery is gone and this guard is
# checking nothing, so none is refused.
#
# EVERY module that requires it is asserted over, not one of them. A scalar here
# was the same defect as the literal path one paragraph up, one level along: the
# loop overwrote it, `scope modules` returns sorted paths, and the last match
# won. Measured on this tree — a second nested module requiring testcontainers-go
# moved the assertion onto it and left test/data/codegen, the module bd gqlc-rohp
# is about, unchecked with nothing saying so. Refusing the second module instead
# would reintroduce what this recipe just stopped doing: making a legitimate tree
# change a gate failure that only a gate edit can clear, while checking no more
# code than before. The modules checked are printed for the same reason
# lint-hooks prints its script list — a set that is only counted is a set nobody
# can see narrow.
[private]
check-codegen-external-tests: sweep-discovery-probes
    #!/usr/bin/env bash
    set -euo pipefail
    scope() { go run ./internal/tools/modscope "$@"; }

    derive_nested() {
        local raw m
        raw="$(scope modules)" || return 1
        nested=()
        while IFS= read -r m; do
            case "${m}" in ""|".") continue ;; esac
            nested+=("${m}")
        done <<<"${raw}"
    }

    # WITNESS: this set is discovered, not named — the same clause pair, and the
    # same argument, as in test-codegen-fence above; read it there. This guard
    # named `test/data/codegen` literally too (bd gqlc-oxne), and on a tree whose
    # discovered set is that one module, nothing but a changed tree can tell the
    # two apart. Residue this trap could not clean up is swept by the
    # sweep-discovery-probes dependency, not here.
    probe="$(mktemp -d test/data/{{xtest_probe}}.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/{{xtest_probe}}\n\ngo 1.26.5\n' >"${probe}/go.mod"
    derive_nested || exit 1
    rm -rf "${probe}"
    trap - EXIT
    case " ${nested[*]} " in
        *" ${probe} "*) ;;
        *)  echo "error: a module was created at ${probe} and the set this guard checks did not" >&2
            echo "       change, so that set is not being read off the tree (bd gqlc-oxne)." >&2
            echo "       It was: ${nested[*]}" >&2
            echo "       A second nested module would then keep its in-package tests, and the" >&2
            echo "       scan that drops them with everything only they import would report" >&2
            echo "       clean over the lot (bd gqlc-rohp)." >&2
            exit 1
            ;;
    esac
    derive_nested || exit 1
    case " ${nested[*]} " in
        *" ${probe} "*)
            echo "error: ${probe} is still in the set after being removed from the tree, so the" >&2
            echo "       set is a snapshot rather than a measurement (bd gqlc-oxne)." >&2
            exit 1
            ;;
    esac

    if [ "${#nested[@]}" -eq 0 ]; then
        echo "error: discovery found no nested module, so this guard checked nothing and would" >&2
        echo "       have exited 0 (bd gqlc-oxne, bd gqlc-rohp)." >&2
        exit 1
    fi

    inpackage=()
    batteries=()
    for m in "${nested[@]}"; do
        # Assignment rather than `mapfile -t tests < <(find ...)`, and for this
        # recipe's own reason rather than style — see the house rule in `just
        # vuln`. `find` exits 1 on a directory it cannot read and still prints
        # every file it did reach, so through a process substitution, whose
        # status is read by nobody, mapfile reports success over a walk that
        # skipped files and the emptiness clause below does not fire: it only
        # catches a walk that returned nothing at all (bd gqlc-s3lt).
        #
        # Not hypothetical, and nothing else here covers it. modscope's own walk
        # fails closed on an unreadable directory, but it SkipDirs testdata,
        # vendor and dot- or underscore-prefixed names before reading them — so
        # an unreadable directory with one of those names is invisible to
        # discovery above and to `go list` below, and this walk, which
        # deliberately has no such exclusions, is the only thing that looks
        # there. Measured: a `package blocked` test under an unreadable
        # test/data/codegen/testdata/blocked left this recipe exiting 0.
        tests_raw="$(find "${m}" -type f -name '*_test.go' | sort)" || {
            echo "error: the walk of ${m} for _test.go files failed, so this guard would have" >&2
            echo "       checked the files it managed to reach and exited 0 over the rest — a" >&2
            echo "       partially walked module reads exactly like a clean one here (bd" >&2
            echo "       gqlc-s3lt). The walk's own diagnostic is above." >&2
            exit 1
        }
        tests=()
        while IFS= read -r f; do
            case "${f}" in "") continue ;; esac
            tests+=("${f}")
        done <<<"${tests_raw}"
        if [ "${#tests[@]}" -eq 0 ]; then
            echo "error: no _test.go files found under ${m}." >&2
            echo "       The live battery has moved and this guard is checking nothing (bd gqlc-rohp)." >&2
            exit 1
        fi
        for f in "${tests[@]}"; do
            pkg="$(sed -n 's/^package[[:space:]]\{1,\}\([A-Za-z_][A-Za-z0-9_]*\).*$/\1/p' "$f" | head -1)"
            case "$pkg" in
                *_test) ;;
                "") inpackage+=("$f (no package clause)") ;;
                *)  inpackage+=("$f (package $pkg)") ;;
            esac
        done
        # Read then match, rather than `go mod edit -json … | grep -q`. That
        # pipeline is the condition of an `if`, so errexit is off for it and
        # pipefail has nobody to report to: a go.mod the go command cannot read
        # takes the same branch as a go.mod that does not name the battery, and
        # the refusal below then blames a dropped requirement for an unreadable
        # file.
        gomod_json="$(go mod edit -json "${m}/go.mod")" || {
            echo "error: go mod edit -json could not read ${m}/go.mod, so whether that module" >&2
            echo "       holds the live battery is unknown — and an unknown answer here reads" >&2
            echo "       exactly like 'this module is not the battery' (bd gqlc-rohp)." >&2
            exit 1
        }
        if grep -q '"Path": "github.com/testcontainers/testcontainers-go"' <<<"${gomod_json}"; then
            batteries+=("${m}")
        fi
    done
    if [ "${#inpackage[@]}" -ne 0 ]; then
        echo "error: every _test.go under a nested module must declare an external test package." >&2
        echo "       govulncheck drops the in-package test variant together with everything only it" >&2
        echo "       imports, so 'just vuln' goes green over the driver, testcontainers and docker" >&2
        echo "       trees it never loaded (bd gqlc-rohp). Offending files:" >&2
        printf '         %s\n' "${inpackage[@]}" >&2
        exit 1
    fi

    if [ "${#batteries[@]}" -eq 0 ]; then
        echo "error: no module in this tree requires github.com/testcontainers/testcontainers-go," >&2
        echo "       so the closure assertion below has nothing to assert over and this guard is" >&2
        echo "       half a guard (bd gqlc-rohp). Either the live battery has moved out of the" >&2
        echo "       tree — delete the assertion deliberately — or its go.mod requirement was" >&2
        echo "       dropped, which is the regression itself." >&2
        exit 1
    fi
    for battery in "${batteries[@]}"; do
        tags_raw="$(scope tags "${battery}")" || exit 1
        taglist="$(printf '%s\n' "${tags_raw}" | paste -sd,)"
        tagflag=()
        [ -z "${taglist}" ] || tagflag=(-tags "${taglist}")
        echo "closure: ${battery}, tags [${taglist:-none}]"
        xtest="$(cd "${battery}" && go list "${tagflag[@]}" -f '{{{{range .XTestImports}}{{{{println .}}{{{{end}}' ./... | sort -u)" || {
            echo "error: the external test imports of ${battery} could not be listed, so the" >&2
            echo "       closure below would be assembled from a short list and the assertion" >&2
            echo "       would be about less code than it names (bd gqlc-rohp)." >&2
            exit 1
        }
        # Loaded then matched, not piped into grep: through a pipe, a `go list`
        # that died reads as a closure that does not contain the battery, and
        # the message blames the packaging for a broken load.
        deps="$(cd "${battery}" && go list -deps "${tagflag[@]}" ./... ${xtest})" || {
            echo "error: the package closure of ${battery} could not be loaded under tags" >&2
            echo "       [${taglist:-none}], so whether it reaches testcontainers-go is unknown" >&2
            echo "       (bd gqlc-rohp). The load's own diagnostic is above." >&2
            exit 1
        }
        if ! grep -qx 'github.com/testcontainers/testcontainers-go' <<<"${deps}"; then
            echo "error: github.com/testcontainers/testcontainers-go is not in the package closure" >&2
            echo "       govulncheck will load for ${battery}, so the live battery's dependency tree is" >&2
            echo "       unscanned again (bd gqlc-rohp). The closure is the non-test deps plus every" >&2
            echo "       external test package's deps, taken under the tags derived for that module" >&2
            echo "       [${taglist:-none}]; check the tag still reaches the container code and that" >&2
            echo "       the battery is still an external test package." >&2
            exit 1
        fi
    done

# runs every live test in the codegen module against real testcontainers:
# the smoke battery on all three arms plus the AGE session-init contract.
# Opt-in: PR CI runs the fence recipe above; this recipe wires the docker-
# gated satellite (bd gqlc-73h, v6 arm added by bd gqlc-5gc, AGE arm by bd
# gqlc-35yu.8) that proves generated repositories actually query a live
# driver. Requires docker (or a compatible runtime honouring the DOCKER_HOST
# env var); set GQLC_SKIP_LIVE=1 to short-circuit on hosts without a
# container runtime.
#
# The two recipes below are the same battery split by backend, because CI runs
# the halves on different triggers. Arms are split by subtracting the other
# half's arms from TestLiveSmoke with -skip, so an arm added to the arms table
# runs in both halves until it is named there. Top-level tests are split by the
# halves' -run allowlists — TestAGESessionInit reaches only the AGE half because
# the neo4j half's -run omits it. Those allowlists are hand-written, and what
# keeps them exhaustive is TestEveryLiveTestIsRunByARecipeThatNamesIt
# (internal/liverecipes): it reads them against the top-level tests the codegen
# module declares and names the test no half runs (bd gqlc-df3d). Which half a
# test belongs in is still the author's call and is asserted nowhere.
#
# This recipe carries no -run. It is the whole battery on every arm, so it has
# no other half to subtract and selects by the build tag alone. No workflow
# reaches it, so the same guard holds it to a stricter rule than the halves': it
# has to run every declared test by itself. An allowlist here would have to name
# all of them, and would fail the moment a test was added.
#
# -count=1 so a developer asking for a live run gets containers, not the cache.
test-codegen-live:
    cd test/data/codegen && go test -count=1 -tags codegen_live ./...

# the neo4j half: both driver arms in parallel against one neo4j:5-community
# image. This is the half PR CI blocks on, so its wall time is a PR's wall time.
#
# No -count=1, unlike the two recipes either side, and that is what keeps the
# per-PR cost near zero (.github/workflows/codegen-live.yml). What a hit stands
# on is the test binary — the scenario bodies, the generated packages they
# drive, the driver dependencies, and the neo4j image, pinned by digest as a
# constant in live_neo4j_test.go rather than resolved at run time — and the
# cache key beside it: go records the environment variables and the files a run
# reads under the module root and keys the cached result on their values, so the
# GQLC_SKIP_LIVE=1 pass that starts no container is a separate entry from the run
# that starts one, and a third value is a third entry (measured on go1.26.6, bd
# gqlc-4int). A hit therefore replays a run of this binary under the same
# values, and any edit that could move either invalidates it; the server that
# run met, where it started one, came from the digest the binary carries. What
# is in neither the binary nor the key is not re-checked, the container runtime
# underneath included: it is not a property of this repo.
#
# That argument covers this half's server facts too, and it does have them —
# edgeUnionDispatch asserts what a live neo4j returns for a relationship type
# outside the candidate set. A digest-pinned image makes those as cacheable as
# anything else compiled in.
#
# It would equally cover the AGE half, whose image is pinned the same way in
# live_age_test.go, so that half's -count=1 does not follow from
# TestAGERefusesRelationshipTypeAlternation being a measurement. It rests on the
# reason given at that recipe instead: nightly and manual are its only runs. The
# asymmetry errs safe and is left standing.
#
# -v is not part of that asymmetry and does not disturb it: it joins the cache
# key, so the first run after this line misses and every later one replays the
# stored verbose output. Without it this arm prints one "ok" for the whole
# package, so a scenario that stops executing is indistinguishable from one
# that passes -- measured 2026-08-24 on bd gqlc-3d0l, where a mutation that
# survived here could not be told apart from a mutation whose row never ran.
#
# The two battery guards run here and not in the AGE half, though neither is
# about neo4j and neither starts a container. They belong to whichever half a
# PR blocks on, because what they catch is a scenario deleted by the PR in
# front of you: caught nightly it is caught after the merge (bd gqlc-8jfj).
# TestTxMethodSet is here for the same reason and is untagged too -- it reflects
# over the generated packages' method sets, so it needs no container. It is
# deliberately NOT added to the AGE recipe, which no pull request pays for and
# whose full battery already covers it.
#
# The alternation is a NAME LIST, not a pattern, for the reason the AGE recipe
# below spells out: -run is unanchored, so a name here silently claims every
# test that extends it. No name below is a prefix of another test in the module
# (grepped 2026-08-29).
test-codegen-live-neo4j:
    cd test/data/codegen && go test -v -tags codegen_live -run 'TestLiveSmoke|TestEveryBatteryIsTheDeclaredSize|TestEveryBatteryIsNamedInScenarioTables|TestTxMethodSet' -skip 'TestLiveSmoke/apache-age' ./...

# the Apache AGE half: the smoke battery's AGE arm, the session-init contract,
# the dialect fact the AGE backend's edge-union refusal rests on, the offset
# sidecar's two live branches, and the two constructs AGE refuses that no gap
# acts on yet — each on its own apache/age
# container. Nightly and manual only — these containers are cost this project
# does not charge to a pull request. -count=1 because this is the AGE arm's only
# gate and no pull request pays for it, so the run it reports on has to be a real
# one.
#
# The alternation is a NAME LIST, not a pattern: go test's -run is unanchored, so
# a prefix here would silently claim every test that extends it, and
# TestEveryLiveTestIsRunByARecipeThatNamesIt reads it as names for that reason. A
# live test added to the codegen module and not added here runs in no job at all.
#
# -v because a name list and a container are exactly the pair that hides a test
# not running. -run is matched, not verified: a name that matches nothing leaves
# `go test` printing ok and exiting 0, and Go DISCARDS a passing package's
# output without -v — not t.Log, not fmt.Println, not a direct write to
# os.Stdout. Measured on run 32634892211 (success, master, 19.7s): the log held
# ZERO lines from a test the -run list named and that had run green, so a
# container was paid for and its only product thrown away (bd gqlc-8cjn). With
# -v the per-test RUN/PASS lines are the evidence that the battery this project
# runs nowhere else actually executed. It goes on the whole recipe rather than a
# second `go test` invocation, which would start a second AGE container.
test-codegen-live-age:
    cd test/data/codegen && go test -v -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation|TestAGERefusesTheFunctionsItDoesNotDefine|TestAGEOffsetSidecar|TestAGEAnswersTheConstructsNoGapRefuses' -skip 'TestLiveSmoke/neo4j' ./...

# call-graph-aware vulnerability scan; run on dependency changes and on the
# weekly CI schedule ("@latest" deliberate: the vuln DB matters more than
# tool-version reproducibility)
#
# One invocation per module, because govulncheck is scoped to a module: `go list
# ./...` at the root emits none of a nested module's packages, so the nested
# module's driver, testcontainers and docker trees are outside a root-rooted scan
# by module boundary (bd gqlc-rohp). -test as well, because without it
# govulncheck loads no test files at all and every test-only dependency is
# unscanned — godog and testify at root, and everything the live battery reaches
# in the nested module.
#
# The module set and each module's build tags are DISCOVERED, not declared (bd
# gqlc-pig9). A list written out here is a list a third module can be added
# without: it would go unscanned with no diagnostic and no failing gate, which is
# this gate's own defect class. Every go.mod under the checkout is scanned, with
# every build tag its own packages constrain themselves by — the tag is where
# code enters the build, so a module scanned without its tags is a module
# partly scanned. The tags are read off the module's files on disk rather than
# out of `go list ./...`, because the wildcard does not match a directory whose
# Go files are ALL constrained — it would derive none of the tags such a
# directory carries, and see nothing missing.
#
# Which TERMS of a constraint become tags is a judgement, not a transcription
# (bd gqlc-e7oq), and internal/tools/modscope makes it. Only a term that is
# custom and appears un-negated becomes one. A GOOS, GOARCH or go1.N term names
# a fact the toolchain owns, and `-tags` will happily assert it anyway: this
# recipe used to turn `//go:build !windows` into `-tags windows` and exclude the
# very file the term came from, and `//go:build windows` into a scan of
# Windows-only code on Linux. A negated term is dropped for a different reason —
# `-tags` can only make a term true, so on `!foo` it does the opposite of what
# was asked; foo's ABSENCE is what that file was written for, and that is the
# default. Where a custom tag is positive on one file and negated on another no
# single build covers both, and the postconditions below say so rather than this
# derivation picking a side quietly.
#
# Deriving the tags removes one way to be green over unscanned code and adds
# another — a tag walk that came back empty — so each module's scan is preceded
# by the derivation's postcondition, in two halves: every directory holding a Go
# file was matched by `go list`, and every file of every matched package is in
# the build. Both are scoped to what `./...` matches, which is govulncheck's own
# scope: testdata, vendor and dot- or underscore-prefixed directories are
# outside both.
#
# -show verbose because the scan runs at symbol level: package- and module-level
# findings never change its exit status, so without verbose they are only ever
# counted and the set this gate is exiting 0 over is never named. Naming them is
# not only for a reader — the ids are read back out of the output and checked
# against a register of accepted advisories, so "someone will notice the set
# change in the log" is a decision the recipe takes rather than a hope (bd
# gqlc-k22l). Verbose also prints the packages and modules each invocation
# matched, which is the standing evidence that the widened scan still covers
# every module.
#
# WHAT THIS GATE STILL DOES NOT SEE: the root module's in-package tests, whose
# imports govulncheck discards, so a called vulnerability reachable only from
# one of those files exits 0 here (ADR 0026). vuln-root-residual below measures
# and ratchets that blind spot; bd gqlc-m5rc closes it.
vuln: sweep-discovery-probes vuln-root-residual
    #!/usr/bin/env bash
    set -euo pipefail

    # comm needs a stream, and printf on an empty string still emits one empty
    # line, which comm would read as a member. Without this a trip could be
    # reported against a set that was never measured.
    lines() { [ -n "${1}" ] && printf '%s\n' "${1}" || true; }

    # The module set and each module's Go directories come from one place, and
    # every answer is graded before it is printed (bd gqlc-s3lt). An empty module
    # set, a checkout whose root is not a module, and a module whose walk found
    # no directory holding a Go file are all errors inside modscope rather than
    # empty lines out of it — see internal/tools/modscope, whose tests are the
    # regression for each. That grading is the point: the postcondition below is
    # a `comm` between two sets, and `comm` over two EMPTY sets reports no
    # difference. The walk is the measurement, the comparison is only as good as
    # it, and an unmeasured module used to read exactly like a covered one.
    #
    # THREE HOUSE RULES for the helpers below, and none is stylistic.
    #
    # (1) Never call one inside `<(...)`. A process substitution's exit status is
    # read by nobody, so `comm -13 <(...) <(scope dirs X)` hands comm an empty
    # stream when the helper dies and comm duly reports no difference — this
    # bead's failure mode reintroduced by punctuation.
    #
    # (2) Assignment position is NOT enough on its own. Measured on bash 5.3:
    # errexit is suppressed inside a command substitution, so a helper whose body
    # is `r="$(fail)"; printf ...` runs the printf anyway, exits 0, and the
    # caller's `dirs="$(helper)"` sees success and an empty string. Every helper
    # therefore returns its own status explicitly and every caller checks it,
    # which is why `|| return` and `|| exit` appear below on assignments that
    # `set -e` looks like it already covers. It does not.
    #
    # (3) Never pipe a listing into a consumer that exits on its first match.
    # `grep -q` closes the pipe the moment it matches, the producer takes
    # SIGPIPE on its next write and reports 141, and `pipefail` makes 141 the
    # pipeline's status — so a MATCH arrives as a failed pipeline. Measured on
    # the tree internal/tools/vulnguard builds, where the fixture is second of
    # three listed directories: `go list ./... | grep -qxF "${fixture}"` returns
    # 141 while `grep -qxF "${fixture}" <<<"${listed}"` over that same listing
    # returns 0 (bd gqlc-e53u). No `grep -q` or `grep -m1` in this recipe reads a
    # pipe: seven take a herestring — six of whose sources are `$(...)`
    # assignments and the seventh a parameter of one, so the producer has a
    # status of its own to be checked in every case, rule (2) again — and one
    # takes a file argument, which has no producer to lose.
    # What decides a piped site is a race: whether the producer still has a write
    # to make when `grep` exits. What settles it is the producer having none left
    # once `grep` can first match. The readable instance is a single write():
    # a match needs data, so that write lands with the reader alive and nothing
    # remains to take the signal (measured: match on
    # line 1, 50ms linger, rc 0 in 200 of 200; the same bytes in two writes with
    # the match in the FIRST fail 40 of 40 — the position matters, since two
    # writes with the match confined to the last pass 200 of 200, so single-write
    # is the readable safe case and not the only one). A `grep` that exits
    # without reading at all — invalid regex,
    # unreadable -f file — breaks that premise but loses no match. Nothing in
    # this recipe's TEXT settles the property for the producers this recipe has —
    # `go list`, `scope`, `sort` — whose write counts follow buffering inside
    # the producer rather than any line readable here. (Not libc: `go list` and
    # `scope` are static Go binaries and buffer through bufio; only `sort` links
    # libc.) Three cheap substitutes for settling it
    # are measured false. Sort order:
    # this site issues the TAGGED listing — 29 lines, fixture at 28 — and the
    # fixture is last only in the UNTAGGED listing, which this site never runs;
    # selftest_tagblind is not sorted into safety either, since on a healthy tree
    # its fixture is absent. Output size: fitting the buffer is neither
    # sufficient nor necessary. A 1262-byte listing, 52x INSIDE a 65536-byte
    # pipe buffer, returned 141 in 20 of 20 runs when emitted a line at a time
    # with a 20ms pause between lines; a 189019-byte payload, 2.9x OVER the
    # capacity, returned 0 in 60 of 60 when the match was on the last write.
    # Emission shape: that same
    # producer without the pause returned 0 in 200 of 200, so it is not
    # line-at-a-time that loses the race but slowness relative to grep's startup,
    # which nothing here bounds. The real `go list` won the race in 100 of 100
    # runs in this checkout; nothing makes that a guarantee, which is why the
    # rule bans the shape instead of offering a test to apply to it.
    scope() { go run ./internal/tools/modscope "$@"; }

    # Every directory of a module that holds a Go file, absolute, read off disk.
    # The walk is bounded by the modules discovered below, so it stops at a
    # nested module's root rather than filing that module's files under its
    # parent, and it applies go's own `./...` exclusions — names beginning with
    # '.' or '_', testdata, vendor — so this set and the set `go list ./...`
    # matches are answers to the same question and can be compared.
    #
    # Off disk rather than out of `go list`, because `go list`'s `./...` wildcard
    # does not match a directory whose Go files are ALL excluded by build
    # constraints: no package, no error, and no IgnoredGoFiles either. Deriving
    # the tags from a listing blind to such a directory derives none of the tags
    # only that directory carries, and the IgnoredGoFiles assertion below never
    # sees the files it dropped — the scan runs, reports on what was left, and
    # exits 0 (bd gqlc-pig9). The module root comes from `go list -m` rather than
    # `pwd` so it is the same path, symlinks and all, that `go list` prints.
    #
    # One module's Go directories, graded here as well as inside modscope. The
    # duplication is deliberate: modscope's grading is what makes the helper
    # honest, and this is what still holds the day the helper is replaced by
    # something that is not. Either alone reddens an emptied walk; the emptiness
    # test below also covers a helper that fails without saying so.
    module_dirs() {
        local raw
        raw="$(scope dirs "${1}")" || return 1
        if [ -z "${raw}" ]; then
            echo "error: the walk of ${1} came back with no directory holding a Go file, so the" >&2
            echo "       coverage postcondition below would compare two empty sets and pass over" >&2
            echo "       an unscanned module (bd gqlc-s3lt)." >&2
            return 1
        fi
        # Re-sorted rather than trusted sorted: `comm` compares this against a
        # set sorted by this shell, and two sorts under different collations
        # disagree about order without either being wrong.
        printf '%s\n' "${raw}" | sort -u
    }

    # One module's build tags, as a comma-separated -tags argument. Empty is a
    # legitimate answer here — a module whose files carry no constraints asks for
    # no tags — which is why the walk it is built on is graded and this is not.
    module_tags() {
        local raw
        raw="$(scope tags "${1}")" || return 1
        [ -n "${raw}" ] || return 0
        printf '%s\n' "${raw}" | paste -sd,
    }

    # The recipe's ONE derivation of the module set, taken through a plain
    # assignment rather than `mapfile < <(scope modules)`: a process
    # substitution's status is read by nobody, so mapfile would report its own
    # success and a helper that died would read as a checkout with no modules in
    # it. A function rather than two lines inline because the witness below has
    # to run the same code over a changed tree.
    derive_modules() {
        local raw m
        raw="$(scope modules)" || return 1
        modules=()
        while IFS= read -r m; do
            case "${m}" in "") continue ;; esac
            modules+=("${m}")
        done <<<"${raw}"
    }

    # WITNESS: the swept set is discovered, not named — the clause pair from
    # test-codegen-fence, where the argument is written out in full. It applies
    # here for the same reason: `modules=(. test/data/codegen)` written over the
    # derivation scans exactly what this tree scans today, passes every
    # postcondition below, and stops scanning the day a module is added.
    # Residue this trap could not clean up is swept by the
    # sweep-discovery-probes dependency, not here.
    probe="$(mktemp -d test/data/{{vuln_probe}}.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/{{vuln_probe}}\n\ngo 1.26.5\n' >"${probe}/go.mod"
    derive_modules || exit 1
    rm -rf "${probe}"
    trap - EXIT
    case " ${modules[*]} " in
        *" ${probe} "*) ;;
        *)  echo "error: a module was created at ${probe} and the set this recipe sweeps did not" >&2
            echo "       change, so that set is not being read off the tree (bd gqlc-oxne)." >&2
            echo "       It was: ${modules[*]}" >&2
            echo "       A module outside it is a module govulncheck is never pointed at, and" >&2
            echo "       nothing below reports on a module that was not swept — the sweep only" >&2
            echo "       grades what it looked at (bd gqlc-s3lt)." >&2
            exit 1
            ;;
    esac
    derive_modules || exit 1
    case " ${modules[*]} " in
        *" ${probe} "*)
            echo "error: ${probe} is still in the swept set after being removed from the tree," >&2
            echo "       so the set is a snapshot rather than a measurement (bd gqlc-oxne)." >&2
            exit 1
            ;;
    esac

    # Postconditions on the helper rather than a second derivation: modscope
    # already refuses both, and this is what still holds if it is ever replaced.
    if [ "${#modules[@]}" -eq 0 ] || [ -z "${modules[0]}" ]; then
        echo "error: module discovery came back empty, so this recipe would scan nothing" >&2
        echo "       and exit 0 (bd gqlc-pig9, bd gqlc-s3lt)." >&2
        exit 1
    fi
    case " ${modules[*]} " in
        *" . "*) ;;
        *)  echo "error: discovery did not find the root module's go.mod, so the main module" >&2
            echo "       is unscanned (bd gqlc-pig9). Found: ${modules[*]}" >&2
            exit 1
            ;;
    esac

    # The derivation's fixture. test/data/tagblind holds nothing but
    # build-constrained Go files, which is the one directory shape `go list
    # ./...` does not match at all — so it is the only thing in this tree that
    # exercises either the walk above or the coverage assertion below, and a
    # fixture that quietly stopped having that shape (deleted, or an
    # unconstrained file added beside it) would take both guards out of service
    # with nothing failing. The three clauses are the three ways that happens.
    selftest_tagblind() {
        local want="tagblind" fixture root_dirs root_tags untagged
        fixture="$(go list -m -f '{{{{.Dir}}')/test/data/${want}"
        root_dirs="$(module_dirs .)" || exit 1
        root_tags="$(module_tags .)" || exit 1
        if ! grep -qxF "${fixture}" <<<"${root_dirs}"; then
            echo "error: the tag-derivation fixture ${fixture}" >&2
            echo "       is gone, so nothing in this tree exercises the filesystem walk or the" >&2
            echo "       coverage assertion below (bd gqlc-pig9). Restore it." >&2
            exit 1
        fi
        untagged="$(go list -e -f '{{{{.Dir}}' ./...)" || exit 1
        if grep -qxF "${fixture}" <<<"${untagged}"; then
            echo "error: the tag-derivation fixture ${fixture}" >&2
            echo "       is now matched by an untagged 'go list ./...', so it no longer has the" >&2
            echo "       shape it exists to reproduce — a directory whose every Go file is build-" >&2
            echo "       constrained. An unconstrained .go file was added beside it (bd gqlc-pig9)." >&2
            exit 1
        fi
        case ",${root_tags}," in
            *",${want},"*) ;;
            *)  echo "error: the filesystem walk did not derive '${want}' from ${fixture}," >&2
                echo "       so it is not seeing wholly build-constrained directories — the exact" >&2
                echo "       blindness it replaced 'go list ./...' to fix (bd gqlc-pig9)." >&2
                exit 1
                ;;
        esac
    }
    selftest_tagblind

    # The classification's fixture, and the same argument one bead along.
    # test/data/platformtag holds one file behind `//go:build !windows` — a
    # NEGATED GOOS term, which is the shape the old derivation inverted: it read
    # the terms out with the punctuation stripped, emitted `windows`, and
    # `-tags windows` then falsified the file's own constraint (bd gqlc-e7oq).
    #
    # WHAT THIS CATCHES, stated exactly, because the honest scope is narrower
    # than "the derivation regressing". Four clauses; three are single-fault and
    # one is not.
    #
    #   (1) the fixture directory dropping out of the walk,
    #   (2) the file losing `//go:build !windows`, and
    #   (4) the derived tags no longer matching the fixture's directory
    #
    # each fail on their own. Clause (3) — `windows` in the derived tag set —
    # cannot, on THIS fixture: a negated GOOS term is suppressed twice over, once
    # by the polarity rule and once by the platform table, and either suppressor
    # regressing alone leaves the derived set unchanged and this recipe green. It
    # is a double-fault detector, and calling it a regression test for the
    # derivation would be claiming coverage that is not here.
    #
    # The single-fault coverage is in internal/tools/modscope's tests, where the
    # two suppressors can be separated because the corpus is synthetic:
    # TestConstraintTagsKeepsOnlyPositiveCustomTerms pins the polarity rule alone
    # through its negated-CUSTOM-tag cases (`!codegen_live` and the three
    # nested-negation cases, which no platform table touches) and the platform
    # table alone through its positive-GOOS and GOARCH cases (which no polarity
    # rule touches), and TestATruncatedDistListMakesUndeclaredPlatformTermsUnplaceableNotTags
    # pins the third suppressor, the refusal of a term that fits no vocabulary.
    #
    # There is no in-tree fixture that would separate them here, and this is a
    # property of the tree rather than an omission: the separating fixture is a
    # POSITIVE GOOS term, `//go:build windows`, and such a directory is excluded
    # from `go list ./...` on every machine this gate runs on — so the `unlisted`
    # postcondition below would redden on it permanently and correctly. The only
    # platform fixture that can live here is the negated one, and the negated one
    # is over-determined.
    selftest_platformtag() {
        local fixture root_dirs root_tags src listed
        fixture="$(go list -m -f '{{{{.Dir}}')/test/data/platformtag"
        src="${fixture}/platformtag.go"
        root_dirs="$(module_dirs .)" || exit 1
        root_tags="$(module_tags .)" || exit 1
        if ! grep -qxF "${fixture}" <<<"${root_dirs}"; then
            echo "error: the classification fixture ${fixture}" >&2
            echo "       is gone, so nothing in this tree witnesses what the tag derivation does" >&2
            echo "       with a platform term (bd gqlc-e7oq). Restore it." >&2
            exit 1
        fi
        if ! grep -qE '^//go:build[[:space:]]+!windows[[:space:]]*$' "${src}"; then
            echo "error: ${src} no longer carries '//go:build !windows'," >&2
            echo "       so it no longer has the shape it exists to reproduce and the assertion" >&2
            echo "       below passes over a file that could not fail it (bd gqlc-e7oq)." >&2
            exit 1
        fi
        case ",${root_tags}," in
            *",windows,"*)
                echo "error: the tag derivation emitted 'windows' from a tree whose only mention" >&2
                echo "       of it is the negated term in ${src}." >&2
                echo "       A negated term has produced its own positive, which is the inverse of" >&2
                echo "       what the file asks for: -tags windows satisfies 'windows', '!windows'" >&2
                echo "       goes false, and the scan below compiles this file nowhere while" >&2
                echo "       reporting clean over it (bd gqlc-e7oq)." >&2
                exit 1
                ;;
        esac
        # The consequence, not just the derivation: on this platform the file
        # builds, so `go list` under the derived tags must match its directory.
        # The general coverage postcondition below would also catch this, but
        # only as an unnamed directory in a list; here it names the bead.
        #
        # The flag goes in an array rather than through `${root_tags:+-tags}`.
        # That expansion is QUOTED, so an empty root_tags yields an empty
        # argument rather than no argument, `-f` stops being read as a flag, and
        # `go list` prints 28 lines of "." and "{{{{.Dir}}" instead of 25
        # directories — measured. The clause below then fails for a reason its
        # message does not describe, and only ever passes because tagblind keeps
        # root_tags non-empty. An assertion that is right by a coincidence
        # elsewhere in the tree is the shape this whole branch is about.
        local tagflag=()
        [ -z "${root_tags}" ] || tagflag=(-tags "${root_tags}")
        listed="$(go list -e "${tagflag[@]}" -f '{{{{.Dir}}' ./...)" || exit 1
        if ! grep -qxF "${fixture}" <<<"${listed}"; then
            echo "error: ${fixture} is not in the set 'go list ./...'" >&2
            echo "       matched under the derived tags [${root_tags:-none}], so the scan below" >&2
            echo "       would not compile a file that builds fine on this platform. The" >&2
            echo "       derivation has excluded the very file it read (bd gqlc-e7oq)." >&2
            exit 1
        fi
    }
    selftest_platformtag

    # govulncheck resolves the standard library's version by matching the
    # toolchain's `go env GOVERSION` against a tag pattern of its own, and a
    # version it cannot place is not an error there: the stdlib is looked up
    # under an empty version, matches no advisory, and every standard-library
    # finding disappears while the third-party half of the scan carries on
    # normally. Measured on this tree, one variable apart — under release
    # go1.26.5 the root module reports eight stdlib advisories, three of them
    # CALLED, and govulncheck exits 1; under the custom build
    # go1.26.5-X:nodwarf5 it reports none and exits 0 (bd gqlc-u91z). Nothing
    # else in this recipe can see that: the register below still balances,
    # because the third-party findings it holds are all in the nested module and
    # arrive either way.
    #
    # What is graded is govulncheck's own report of what it placed, on the line
    # it prints above the module list (ADR 0026). An empty version formats back
    # to the bare prefix, so `the go standard library` — the token `go` with
    # nothing after it — IS the unplaced rendering, and the pattern below asks
    # only whether anything follows. An absent line is refused for its own
    # reason: the header moving is this clause going quiet, and a quiet clause
    # accepts everything.
    #
    # On the accepting path the clause prints two lines: the name of what it
    # graded, then the header line it read out of the scan. The per-module tally
    # after the loop is the set of those names. The name alone cannot say which
    # output was graded — it is the caller's own argument, echoed — so the header
    # is what each call site matches back against the scan it handed in.
    refuse_unplaced_stdlib() {
        local scan="${1}" where="${2}" line token
        line="$(grep -m1 -E '^Govulncheck scanned the following [0-9]+ modules and the .*standard library:$' <<<"${scan}" || true)"
        if [ -z "${line}" ]; then
            echo "error: the scan of ${where} printed no line naming the standard library it" >&2
            echo "       resolved, so nothing here can tell a scan that covered the stdlib from" >&2
            echo "       one that silently did not (bd gqlc-u91z). govulncheck's output format" >&2
            echo "       moved; fix the match in this recipe." >&2
            return 1
        fi
        token="$(sed -E 's/^.*modules and the (.*) standard library:$/\1/' <<<"${line}")"
        case "${token}" in
            go?*) printf '%s\n%s\n' "${where}" "${line}"; return 0 ;;
        esac
        echo "error: the scan of ${where} placed no version on the standard library, so every" >&2
        echo "       stdlib advisory was looked up under an empty version and none of them" >&2
        echo "       could be reported. The scan exits 0 and names nothing, which is this" >&2
        echo "       gate green over the largest attack surface in the binary (bd gqlc-u91z)." >&2
        echo "       govulncheck said:" >&2
        echo "         ${line}" >&2
        echo "       That happens when the toolchain's version is one govulncheck cannot" >&2
        echo "       match — a distribution's custom build or a devel build; 'go env" >&2
        echo "       GOVERSION' shows which. Point GOTOOLCHAIN at a released toolchain for" >&2
        echo "       the scan; the go directive in go.mod names one." >&2
        return 1
    }

    # WITNESS: on a tree scanned by a release toolchain the clause above only
    # ever runs in the negative, and a guard whose passing case is silence is
    # one nothing distinguishes from a deleted one. Both refusing directions and
    # a positive control therefore run on every invocation of this recipe, local
    # or CI, against fabricated headers, before any scan (ADR 0026). How often
    # that is in CI is the `vuln` job's own path filter, not this line.
    unplaced_header="Govulncheck scanned the following 2 modules and the go standard library:"
    placed_header="Govulncheck scanned the following 2 modules and the go1.26.6 standard library:"
    witness_where="the unplaced-stdlib witness"

    # Which refusal fired is asserted, not just that one did. The two send a
    # reader to different places — a toolchain to point elsewhere, or this
    # recipe's own match to repair — and either reported as the other is a wrong
    # diagnosis on the single run where anyone is reading.
    expect_refusal() {
        local scan="${1}" marker="${2}" what="${3}" got
        if got="$(refuse_unplaced_stdlib "${scan}" "${witness_where}" 2>&1)"; then
            echo "error: ${what} was ACCEPTED, so the standard-library half of every scan" >&2
            echo "       below is unwatched (bd gqlc-u91z)." >&2
            return 1
        fi
        case "${got}" in
            *"${marker}"*) return 0 ;;
        esac
        echo "error: ${what} was refused, but the message does not say \"${marker}\", so a" >&2
        echo "       real trip names the wrong cause and sends whoever reads it to the wrong" >&2
        echo "       repair (bd gqlc-u91z):" >&2
        printf '%s\n' "${got}" | sed 's/^/         /' >&2
        return 1
    }
    expect_refusal "${unplaced_header}" "placed no version" \
        "a scan header naming a bare 'go' — what govulncheck prints when it could not place the toolchain" || exit 1
    expect_refusal "No vulnerabilities found." "printed no line" \
        "output carrying no scan header at all — the shape every scan takes once that line is renamed" || exit 1

    witness="$(refuse_unplaced_stdlib "${unplaced_header}" "${witness_where}" 2>&1 || true)"
    case "${witness}" in
        *"${unplaced_header}"*) ;;
        *)  echo "error: the unplaced-stdlib clause refused without quoting the header it" >&2
            echo "       refused, so a real trip carries no evidence of what it read" >&2
            echo "       (bd gqlc-u91z):" >&2
            printf '%s\n' "${witness}" | sed 's/^/         /' >&2
            exit 1
            ;;
    esac
    if ! accepted="$(refuse_unplaced_stdlib "${placed_header}" "${witness_where}" 2>/dev/null)"; then
        echo "error: a scan header that DOES place a standard-library version was refused, so" >&2
        echo "       this clause refuses every scan on a released toolchain too and is an" >&2
        echo "       outage rather than a gate (bd gqlc-u91z)." >&2
        exit 1
    fi
    # Both lines of the accepting arm are asserted, against the exact strings
    # handed in. The tally after the loop is built from the first, so an arm that
    # printed nothing would leave every module reading as ungraded. Each call
    # site matches the second back against its own scan, so an arm that dropped
    # it would be an acceptance no scan is tied to — and a `grep -qxF` on the
    # empty string matches any blank line, which govulncheck's output has.
    if [ "${accepted}" != "${witness_where}"$'\n'"${placed_header}" ]; then
        echo "error: the unplaced-stdlib clause accepted a placed header without echoing back" >&2
        echo "       the name it graded and the header it read (bd gqlc-u91z). Expected" >&2
        echo "       \"${witness_where}\" then the header it was handed; it said:" >&2
        printf '%s\n' "${accepted}" | sed 's/^/         /' >&2
        exit 1
    fi

    reported=""
    headers_total=0
    graded=""

    for dir in "${modules[@]}"; do
        # Taken once, here, into a variable — see the house rule beside scope().
        # The `comm` below reads this string rather than re-running the walk in
        # a process substitution whose exit status nothing would read.
        dirs="$(module_dirs "${dir}")" || exit 1
        tags="$(module_tags "${dir}")" || exit 1
        tagflag=()
        if [ -n "${tags}" ]; then tagflag=(-tags "${tags}"); fi

        # The derived tag set is CHECKED against the build it produces, not
        # trusted. A tag derivation that silently came back empty would leave
        # the tagged files out of the scan and change nothing else: the scan
        # would run, report on what was left, and exit 0 — green because it was
        # looking at less.
        #
        # There are two ways to be outside that build and they need separate
        # assertions, because `go list` reports only one of them. A directory
        # whose Go files are ALL excluded is not listed at all — no package, no
        # error, no ignored files — so it can only be caught by comparing the
        # directories the wildcard matched against the directories that hold Go
        # files (bd gqlc-pig9). Within a directory that WAS listed, `go list`
        # reports the drop itself, and that is the assertion below.
        matched="$(cd "${dir}" && go list -e "${tagflag[@]}" -f '{{{{.Dir}}' ./... | sort -u)"
        unlisted="$(comm -13 <(lines "${matched}") <(lines "${dirs}") || true)"
        if [ -n "${unlisted}" ]; then
            echo "error: these directories of ${dir} hold Go files that 'go list ./...' does not" >&2
            echo "       match, so govulncheck loads no package for them and the scan below would" >&2
            echo "       be green over unscanned code (bd gqlc-pig9):" >&2
            printf '%s\n' "${unlisted}" | sed 's/^/         /' >&2
            echo "       Derived tags were [${tags:-none}]. A directory whose every Go file is" >&2
            echo "       excluded by build constraints is skipped by the wildcard silently, with" >&2
            echo "       no package and no IgnoredGoFiles to report. Two causes: a custom tag these" >&2
            echo "       files need is one the derivation declined, because some other file negates" >&2
            echo "       it (an undeclared one cannot get this far — it is refused by name, with" >&2
            echo "       the file, in .golangci.yml's vocabulary); or these files are guarded by" >&2
            echo "       a GOOS/GOARCH or go1.N term, which the derivation deliberately does not" >&2
            echo "       pass as a tag (bd gqlc-e7oq) because doing so tells the compiler it is on" >&2
            echo "       a platform it is not. Code for another platform genuinely cannot be" >&2
            echo "       scanned from this one; that needs a deliberate decision here — a second" >&2
            echo "       GOOS-scoped invocation — not a silently narrower scan." >&2
            exit 1
        fi

        # `IgnoredGoFiles` is go/build's own list of the files it dropped for
        # build constraints, so an empty one says that no Go file of a package
        # `go list ./...` matched is outside the build govulncheck is about to
        # load. It says nothing about a directory the wildcard never matched;
        # that is the assertion above. The two together are the tag derivation's
        # postcondition rather than a second copy of it, so neither can go stale
        # with it. Both are scoped to what `./...` matches — testdata, vendor and
        # dot- or underscore-prefixed directories are outside it, and outside
        # govulncheck's own `./...` too.
        excluded="$(cd "${dir}" \
            && go list -e "${tagflag[@]}" \
                -f '{{{{if .IgnoredGoFiles}}{{{{.ImportPath}}: {{{{join .IgnoredGoFiles " "}}{{{{end}}' ./... \
            | sed '/^$/d')"
        if [ -n "${excluded}" ]; then
            echo "error: build constraints exclude these files from ${dir}, so govulncheck cannot" >&2
            echo "       see them and the scan below would be green over unscanned code (bd gqlc-pig9):" >&2
            printf '%s\n' "${excluded}" | sed 's/^/         /' >&2
            echo "       Derived tags were [${tags:-none}]. Two causes, and both are the corpus" >&2
            echo "       rather than the derivation — a tag the derivation does not know is" >&2
            echo "       refused by name, with its file, against .golangci.yml's vocabulary" >&2
            echo "       long before this line (bd gqlc-e7oq). (1) A custom tag appears both" >&2
            echo "       positively on one file and negated on these, so no single build covers" >&2
            echo "       both — the derivation takes the positive and these fall out; split the" >&2
            echo "       corpus or scan twice. (2) They are excluded by something -tags cannot" >&2
            echo "       enable at all: a GOOS/GOARCH filename suffix or constraint term, or" >&2
            echo "       //go:build ignore. test/data/platformtag carries a GOOS term today" >&2
            echo "       (//go:build !windows); it falls out only under GOOS=windows. A suffix" >&2
            echo "       and a //go:build ignore have not appeared here yet. Whichever of the" >&2
            echo "       three lands on this line needs a deliberate decision — a second" >&2
            echo "       GOOS-scoped invocation — not a silently narrower scan." >&2
            exit 1
        fi

        echo "govulncheck: $(cd "${dir}" && go list -m) at ${dir}, tags [${tags:-none}]"
        # Captured, not piped through `tee`. A pipeline's status is its LAST
        # command's, so piping would take the scan's exit — the single thing
        # this gate turns on — from `tee` unless `set -o pipefail` at the top of
        # the recipe is still in force three hundred lines away. This repo has
        # been bitten by that class three times; the status is read off the
        # command that produced it, which is action at no distance. The cost is
        # that a scan's output appears when it finishes rather than as it runs.
        rc=0
        out="$(cd "${dir}" && go run golang.org/x/vuln/cmd/govulncheck@latest "${tagflag[@]}" -test -show verbose ./... 2>&1)" || rc=$?
        printf '%s\n' "${out}"
        if [ "${rc}" -ne 0 ]; then
            echo "error: the scan of ${dir} exited ${rc}. govulncheck exits non-zero when this tree" >&2
            echo "       CALLS a known vulnerability, and when it cannot load the module at all —" >&2
            echo "       the output above says which. Neither belongs in the register below: that" >&2
            echo "       is for advisories nothing calls (bd gqlc-k22l)." >&2
            exit "${rc}"
        fi
        # What accumulates is the name the grading itself printed, taken in the
        # branch the grading's own status selected, so nothing here can record a
        # grading that did not run to acceptance.
        #
        # That name is this loop's own `${dir}`, echoed back, so it says the
        # grading ran ABOUT this module, not that it read this module's scan:
        # hand the clause any other string that parses — the witness's own
        # fabricated header is in scope — and every name still arrives. What
        # ties the two together is the header the grading reports, required
        # below to be a line of the output handed in. `${placed_header}` fails
        # that on the counts alone: it says 2 modules where these scans say 43
        # and 54.
        if stdlib_graded="$(refuse_unplaced_stdlib "${out}" "${dir}")"; then
            graded_where="$(sed -n '1p' <<<"${stdlib_graded}")"
            graded_line="$(sed -n '2p' <<<"${stdlib_graded}")"
            if ! grep -qxF -- "${graded_line}" <<<"${out}"; then
                echo "error: the standard-library grading of ${dir} reports a header the scan of" >&2
                echo "       ${dir} did not print, so it graded some other output and this" >&2
                echo "       module's stdlib half was accepted unexamined (bd gqlc-u91z)." >&2
                echo "       It graded:" >&2
                echo "         ${graded_line}" >&2
                echo "       The scan printed:" >&2
                grep -m1 -E '^Govulncheck scanned the following [0-9]+ modules and the .*standard library:$' <<<"${out}" \
                    | sed 's/^/         /' >&2
                exit 1
            fi
            graded+="${graded_where}"$'\n'
        else
            exit 1
        fi
        # The register below is fail-closed only while the extraction feeding it
        # still matches, so the extraction is checked against the output it
        # reads. govulncheck names every finding on its own `Vulnerability #N:
        # <id>` line; a header line the id pattern cannot read is the extraction
        # going quiet, and a quiet extraction empties `reported` — which reports
        # an unregistered advisory as nothing at all.
        headers="$(grep -cE '^Vulnerability #' <<<"${out}" || true)"
        ids="$(grep -cE '^Vulnerability #[0-9]+: GO-[0-9]{4}-[0-9]+$' <<<"${out}" || true)"
        if [ "${headers}" -ne "${ids}" ]; then
            echo "error: govulncheck named ${headers} advisories in ${dir} but this recipe could" >&2
            echo "       read an id out of only ${ids} of them, so the register below would be" >&2
            echo "       comparing against a set the scan did not produce (bd gqlc-k22l). The" >&2
            echo "       output format moved; fix the extraction in this recipe." >&2
            exit 1
        fi
        headers_total=$((headers_total + headers))
        reported+="$(grep -oE 'GO-[0-9]{4}-[0-9]+' <<<"${out}" || true)"$'\n'
    done
    reported="$(lines "${reported}" | sed '/^$/d' | sort -u)"

    # One name per grading that ran to acceptance, each printed by the grading
    # itself, compared against the modules actually scanned. A module missing
    # from the set had its standard-library half accepted unexamined, whether
    # the grading was skipped, downgraded or silenced.
    graded="$(lines "${graded}" | sed '/^$/d' | sort -u)"
    scanned="$(printf '%s\n' "${modules[@]}" | sort -u)"
    ungraded="$(comm -13 <(lines "${graded}") <(lines "${scanned}") || true)"
    if [ -n "${ungraded}" ]; then
        echo "error: these modules were scanned but not graded for whether govulncheck placed" >&2
        echo "       the standard library, so their stdlib half was accepted without being" >&2
        echo "       looked at (bd gqlc-u91z):" >&2
        printf '%s\n' "${ungraded}" | sed 's/^/         /' >&2
        exit 1
    fi

    # The per-module check above cannot see the header line itself moving: every
    # count would be 0, they would agree, `reported` would empty, and BOTH halves
    # of the register would go quiet together — comparing two empty sets, green
    # over whatever the scan actually said. That is the register's fail-closed
    # property depending on the register being non-empty, which is not a property
    # at all. This is what makes it hold unconditionally (bd gqlc-k22l).
    if [ "${headers_total}" -eq 0 ]; then
        echo "error: no scan above named a single advisory, so this recipe read nothing out of" >&2
        echo "       govulncheck's output and the register below would pass by comparing two" >&2
        echo "       empty sets (bd gqlc-k22l). Either the output format moved — fix the" >&2
        echo "       extraction — or every registered advisory has genuinely cleared, in which" >&2
        echo "       case delete the register and this check together, deliberately." >&2
        exit 1
    fi

    # The advisories this gate deliberately exits 0 over, and why (bd gqlc-k22l).
    # Every one is uncalled — govulncheck's symbol-level result is empty, which
    # is what keeps the exit status 0 and is re-established on every run above.
    #
    # This is a REGISTER, not a suppression list. It is compared against what the
    # scan actually reported, in both directions: an advisory that turns up
    # without a recorded decision fails the gate, and an entry the scan no longer
    # reports fails it too until the line is deleted. Both halves matter. Without
    # the first, "-show verbose so a reader can see the set change" is a guard
    # nobody executes, because nobody reads the log of a green job. Without the
    # second the register accumulates entries that describe nothing, and quietly
    # pre-accepts an id that comes back.
    #
    # The ids are read back out of govulncheck's own output rather than tracked
    # beside it, so the register cannot describe a scan that did not happen: if
    # the output format moves and the extraction stops matching, the measured set
    # empties and the second comparison fails rather than the first one passing.
    #
    # Neither of the two below is bumped, and the reason is a version rather
    # than a preference. The fixable one is an indirect dependency of
    # testcontainers-go, whose latest published release is v0.43.0 — exactly
    # what test/data/codegen already requires. Pinning an indirect dependency
    # ahead of the module that requires it is churn `go mod tidy` can undo,
    # bought with no reduction in exposure, since it is not called. Revisit
    # when testcontainers-go itself moves; `go list -m -u` in
    # test/data/codegen is the check.
    accepted="$(sed -e 's/#.*//' -e 's/[[:space:]]//g' -e '/^$/d' <<'ACCEPTED' | sort -u
    # go.opentelemetry.io/otel v1.41.0 in test/data/codegen: baggage parsing no
    # longer caps raw header length. Imported, not called. Fixed in v1.42.0, and
    # v1.45.0 is out, but it is testcontainers-go v0.43.0's indirect.
    GO-2026-5158
    # golang.org/x/crypto/openpgp: unmaintained and unsafe by design, no fix
    # available and none coming. Required, not imported. This one is permanent
    # unless x/crypto drops the package, and bumping x/crypto cannot clear it.
    GO-2026-5932
    # golang.org/x/crypto/ssh: source-address critical option not enforced for
    # non-public-key auth callbacks. Imported by an indirect dep, not called
    # by gqlc — no ssh server code here. Fixed in x/crypto v0.55.0; we are on
    # v0.52.0 via a transitive constraint. Registered rather than bumped to
    # keep the herdr-migration PR small; follow-up work should bump x/crypto.
    GO-2026-6303
    ACCEPTED
    )"

    unregistered="$(comm -23 <(lines "${reported}") <(lines "${accepted}") || true)"
    stale="$(comm -13 <(lines "${reported}") <(lines "${accepted}") || true)"
    if [ -n "${unregistered}" ]; then
        echo "error: the scan reported advisories this gate has no recorded decision about" >&2
        echo "       (bd gqlc-k22l):" >&2
        lines "${unregistered}" | sed 's|^|         https://pkg.go.dev/vuln/|' >&2
        echo "       They are uncalled, so govulncheck exited 0 and this gate would have gone" >&2
        echo "       green over them. Upgrade the dependency, or add the id to the register in" >&2
        echo "       this recipe with the reason it is being accepted." >&2
        exit 1
    fi
    if [ -n "${stale}" ]; then
        echo "error: these accepted advisories are no longer reported, so the register in this" >&2
        echo "       recipe is stale (bd gqlc-k22l):" >&2
        lines "${stale}" | sed 's|^|         https://pkg.go.dev/vuln/|' >&2
        echo "       Either a dependency moved and cleared them — delete the entries — or the" >&2
        echo "       scan narrowed and stopped seeing what it used to. A register left above the" >&2
        echo "       measured set pre-accepts whichever of these ids comes back." >&2
        exit 1
    fi
    echo "accepted and still uncalled (bd gqlc-k22l): $(lines "${reported}" | paste -sd' ')"

# Measures the root module's residual blindness (ADR 0026), so it is a number
# taken on every run rather than a claim in a comment that rots (bd gqlc-m5rc).
# Wired into `just vuln` and, as a step in ci.yml's lint job, into every pull
# request. Reachability is the point: the residual moves on PRs that add a test
# file, and lint is the job that runs on all of them.
#
# The two halves are graded differently on purpose. The file counts REPORT — a
# new in-package test is this repo's house style, and failing on it would be
# churn with no risk behind it. The blind set RATCHETS: it grows only on the risk
# event itself, a package acquiring a third-party import it cannot be scanned
# through. The baseline is the set rather than the count so that a trip can name
# the package that went blind, and it is checked in both directions — a package
# that leaves the set has to leave the baseline too, or the ratchet quietly
# regains the slack it just won.
#
# Files and packages rather than modules: no third-party module or package is
# missing from the closure govulncheck loads, only call edges are, so a
# module-level metric here would report a reassuring zero over the real gap
# (ADR 0026).
#
# "Third-party" is anything outside the main module whose first path element
# contains a dot, which is what puts a package in a vulnerability database at
# all; .TestImports is exactly the in-package test variant's import set.
#
# Reaching third-party code is transitive through own-module packages, not
# direct (bd gqlc-nsq4). What govulncheck loses is the variant's outgoing edges,
# so an own-module package that only the variant pulls in is lost along with
# everything it imports; stopping the walk at the module boundary would report a
# residual smaller than the real one, in the reassuring direction.
vuln-root-residual:
    #!/usr/bin/env bash
    set -euo pipefail
    module="$(go list -m)"
    # Emits one line per entry, and nothing at all for an empty set — an empty
    # `echo` would feed `comm` a phantom entry and make the ratchet compare
    # against a set it never measured.
    lines() { [ -n "${1}" ] && printf '%s\n' "${1}" || true; }

    # Every own-module package's non-test imports, which is what makes the walk
    # in blind_packages transitive. `-deps -test` rather than a plain `./...`
    # listing so a helper under testdata — which `./...` never matches but an
    # in-package test can still import — has its imports on file too. Bracketed
    # test variants are dropped: their import path is the plain package's, so
    # keeping them would file the test build's imports under the non-test key
    # and mark packages blind through edges govulncheck has not lost.
    declare -A imports_of=()
    while IFS= read -r entry; do
        case "${entry}" in *'['*) continue ;; esac
        pkg="${entry%% *}"
        case "${pkg}" in "$module" | "$module"/*) imports_of["${pkg}"]="${entry#"${pkg}"}" ;; esac
    done < <(go list -deps -test -f '{{{{.ImportPath}} {{{{join .Imports " "}}' ./...)

    # Reads the listing below on stdin and names each package whose in-package
    # test variant reaches third-party code, walking own-module imports rather
    # than stopping at them. Reads imports_of from the caller's scope, which is
    # what lets selftest_blind_packages substitute a fixture for the real tree.
    blind_packages() {
        local module="$1" pkg intest xtest imports i
        local -A seen
        local -a frontier extra
        while read -r pkg intest xtest imports; do
            seen=()
            read -r -a frontier <<<"${imports}"
            while [ "${#frontier[@]}" -gt 0 ]; do
                i="${frontier[0]}"
                frontier=("${frontier[@]:1}")
                if [ -n "${seen[$i]+set}" ]; then continue; fi
                seen["$i"]=1
                case "$i" in "$module" | "$module"/*)
                    read -r -a extra <<<"${imports_of[$i]-}"
                    frontier+=("${extra[@]}")
                    continue
                    ;;
                esac
                case "${i%%/*}" in *.*)
                    printf '%s\n' "${pkg#"$module"/}"
                    break
                    ;;
                esac
            done
        done
    }

    # No package in this tree has the transitive shape today, so the recursion
    # above has nothing live to walk and could be lost without any measurement
    # moving. The fixture is the only thing that exercises it: m/indirect is
    # blind through two own-module hops, m/inert reaches only stdlib through
    # one, and m/direct pins the direct case the recursion must not break.
    selftest_blind_packages() {
        local -A imports_of=(
            [m/helper]="m/deeper"
            [m/deeper]="example.com/vuln/pkg"
            [m/leaf]="strings"
        )
        local want got
        want=$'direct\nindirect'
        got="$(blind_packages m <<'FIXTURE'
    m/direct 1 0 example.com/vuln/pkg testing
    m/indirect 1 0 m/helper testing
    m/inert 1 0 m/leaf fmt
    m/stdlib 1 0 testing
    m/notests 0 1
    FIXTURE
        )"
        if [ "${got}" != "${want}" ]; then
            echo "error: this recipe's own blind-package walk is broken, so every number" >&2
            echo "       below is unreliable (bd gqlc-nsq4). Fixture expected:" >&2
            lines "${want}" | sed 's/^/         /' >&2
            echo "       got:" >&2
            lines "${got}" | sed 's/^/         /' >&2
            exit 1
        fi
    }
    selftest_blind_packages

    # One `go list` feeds both halves, so the counts and the blind set can never
    # disagree about which files are in the tree, and both read the build
    # govulncheck itself loads rather than the checkout.
    listing="$(go list -f '{{{{.ImportPath}} {{{{len .TestGoFiles}} {{{{len .XTestGoFiles}} {{{{join .TestImports " "}}' ./...)"
    if [ -z "${listing}" ]; then
        echo "error: 'go list ./...' matched no packages in the root module, so this" >&2
        echo "       recipe is measuring nothing (bd gqlc-m5rc)." >&2
        exit 1
    fi

    inpackage=0
    external=0
    while read -r _ intest xtest _; do
        inpackage=$((inpackage + intest))
        external=$((external + xtest))
    done <<<"${listing}"
    blind="$(blind_packages "${module}" <<<"${listing}" | sort -u)"
    if [ "$((inpackage + external))" -eq 0 ]; then
        echo "error: the root module has no test files at all, so this recipe and the" >&2
        echo "       ratchet below are measuring nothing (bd gqlc-m5rc)." >&2
        exit 1
    fi
    echo "root module test-file packaging (bd gqlc-m5rc): ${inpackage} in-package, ${external} external"
    echo "  in-package tests import third-party code in $(lines "${blind}" | grep -c . || true) packages — those call edges are outside govulncheck's call graph:"
    lines "${blind}" | sed 's/^/    /'

    # The ratchet baseline. Every entry is a package whose in-package tests
    # already import third-party code; the list shrinks as bd gqlc-m5rc converts
    # them and must never grow.
    baseline="$(sort <<'BLIND'
    internal/cli
    internal/codegen
    internal/codegen/age
    internal/codegen/neo4j
    internal/config
    internal/queryfile
    internal/resolver
    internal/schema/gql
    internal/schema/gql/annexd
    internal/schema/gql/isobnf
    BLIND
    )"
    grew="$(comm -23 <(lines "${blind}") <(lines "${baseline}") || true)"
    shrank="$(comm -13 <(lines "${blind}") <(lines "${baseline}") || true)"
    if [ -n "${grew}" ]; then
        echo "error: a package just went blind to govulncheck (bd gqlc-m5rc):" >&2
        echo "${grew}" | sed 's/^/         /' >&2
        echo "       Its in-package tests now import third-party code, and govulncheck discards" >&2
        echo "       the in-package test variant together with everything only it imports — so a" >&2
        echo "       vulnerability called through that import reports nothing and 'just vuln'" >&2
        echo "       exits 0. Move the importing test to an external test package (package" >&2
        echo "       <pkg>_test); only add the package to the baseline in this recipe if the test" >&2
        echo "       genuinely needs unexported state, and say why in the commit message." >&2
        exit 1
    fi
    if [ -n "${shrank}" ]; then
        echo "error: these packages are no longer blind, so the baseline in this recipe is stale:" >&2
        echo "${shrank}" | sed 's/^/         /' >&2
        echo "       Delete them from it. A baseline left above the measured set is slack the" >&2
        echo "       ratchet will hand back to the next package that goes blind (bd gqlc-m5rc)." >&2
        exit 1
    fi

# lints the GitHub Actions workflow files.
#
# -shellcheck IS THE GATE, not a refinement of it. actionlint checks a `run:`
# block's shell only by handing it to shellcheck, and its default for this flag
# is the bare word `shellcheck` — a PATH lookup. With no shellcheck on PATH the
# integration is not weakened, it is DISABLED, and actionlint says nothing about
# having skipped it. Measured 2026-08-29 on a workflow whose only fault was
# inside a run block: bare actionlint exited 0 in silence; the same actionlint
# with this flag exited 1 and named the finding (bd gqlc-68g9). It had already
# cost a red CI job on PR #1533, where the local gate passed over the very file
# CI then refused.
#
# So the binary is a DEPENDENCY of this recipe rather than something the machine
# is assumed to have — the self-healing shape ensure-golangci already has.
#
# AND ITS PRESENCE IS THEN ASSERTED, because pointing the flag somewhere is not
# the same as pointing it at something. Measured 2026-08-29 over a workflow that
# the working binary reddens: `-shellcheck <path that does not exist>` exits 0 in
# silence, byte-identical in behaviour to `-shellcheck ''`, which is the spelling
# that means "disabled". actionlint offers no backstop and no complaint, so a
# provisioning step that half-succeeded would restore the exact silence this bead
# is about while the recipe still read as gated. ensure-shellcheck failing loudly
# is the first line of defence; this is the second, and it is here because the
# first one cannot see a binary that vanishes after it returns.
#
# Passing the path also pins the VERSION on both sides. CI runs this recipe, and
# a runner image shipping its own shellcheck would otherwise grade these
# workflows against whatever version it happened to carry, drifting from the
# v0.10.0 that `just lint` holds every other shell script in this repo to.
actionlint: ensure-shellcheck
    #!/usr/bin/env bash
    set -euo pipefail
    sc={{quote(shellcheck)}}
    if [ ! -x "${sc}" ]; then
        echo "error: actionlint's shellcheck is missing or not executable at ${sc}." >&2
        echo "       Refusing rather than running: actionlint would silently skip every" >&2
        echo "       run-block check and exit 0, which is indistinguishable from a pass" >&2
        echo "       (bd gqlc-68g9). Provision it with:  just ensure-shellcheck" >&2
        exit 1
    fi
    go run github.com/rhysd/actionlint/cmd/actionlint@{{actionlint_version}} -shellcheck "${sc}"

# pinned openCypher release tag the TCK is vendored from; never "master" so the
# corpus is reproducible. Bump deliberately, then re-run fetch-tck and commit.
tck_tag := "2024.3"
tck_dir := "test/data/query/cypher/tck"

# vendors the whole openCypher tck/ subtree (features + LICENSE) at tck_tag into
# tck_dir via a shallow, sparse git checkout — no new deps, no extraction step
# (godog reads the .feature files directly). Run for initial population and
# deliberate version bumps; the result is committed.
fetch-tck:
    rm -rf {{tck_dir}}
    mkdir -p {{tck_dir}}
    rm -rf .tck-fetch
    git clone --depth 1 --branch {{tck_tag}} --filter=blob:none --sparse \
        https://github.com/opencypher/openCypher.git .tck-fetch
    cd .tck-fetch && git sparse-checkout set tck
    cp -R .tck-fetch/tck/. {{tck_dir}}/
    cp .tck-fetch/LICENSE {{tck_dir}}/LICENSE
    rm -rf .tck-fetch
    @echo "vendored TCK {{tck_tag}} into {{tck_dir}}"

# builds the autogenerated code from the available, relevant ANTLR grammars
build-grammar:
    sudo docker build -q -t antlr-tool -f Dockerfile.grammar .
    @echo "Generating Go files from GQL.g4..."
    sudo docker run --rm -v {{invocation_directory()}}:/work -w /work/internal/grammar/gql antlr-tool -package gen -visitor -o gen GQL.g4
    @echo "Generating Go files from Cypher.g4..."
    sudo docker run --rm -v {{invocation_directory()}}:/work -w /work/internal/grammar/cypher antlr-tool -package gen -visitor -o gen Cypher.g4

# re-fetches both ISO/IEC 39075 free artefacts and compares their SHA-256
# against the values pinned in isobnf/productions.go and annexd/SOURCE.md.
# Fails loudly on mismatch — ISO published a new edition; re-vendor the
# snapshots and regenerate the derived files (see SOURCE.md in each package).
# Network required; not wired into `just test`.
iso-drift-check:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "fetch date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"

    BNF_URL='https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en).bnf.txt'
    FEAT_URL='https://standards.iso.org/iso-iec/39075/ed-1/en/ISO_IEC_39075(en)-features.xml'

    PINNED_BNF=$(grep -oP '(?<=SourceSHA256 = ")[0-9a-f]+' internal/schema/gql/isobnf/productions.go)
    PINNED_FEAT=$(grep -oP '(?<=`)[0-9a-f]{64}(?=`$)' internal/schema/gql/annexd/SOURCE.md | head -1)

    echo "pinned BNF SHA-256:      $PINNED_BNF"
    echo "pinned features SHA-256: $PINNED_FEAT"

    LIVE_BNF=$(curl -sSfL "$BNF_URL" | sha256sum | cut -d' ' -f1)
    LIVE_FEAT=$(curl -sSfL "$FEAT_URL" | sha256sum | cut -d' ' -f1)

    echo "live BNF SHA-256:        $LIVE_BNF"
    echo "live features SHA-256:   $LIVE_FEAT"

    fail=0
    if [ "$LIVE_BNF" != "$PINNED_BNF" ]; then
        echo "MISMATCH: BNF artefact has changed" >&2
        echo "  pinned: $PINNED_BNF" >&2
        echo "  live:   $LIVE_BNF" >&2
        fail=1
    fi
    if [ "$LIVE_FEAT" != "$PINNED_FEAT" ]; then
        echo "MISMATCH: features XML artefact has changed" >&2
        echo "  pinned: $PINNED_FEAT" >&2
        echo "  live:   $LIVE_FEAT" >&2
        fail=1
    fi
    if [ "$fail" -eq 0 ]; then
        echo "ok: both artefacts match their pinned checksums"
    fi
    exit "$fail"

# ---------- Թագաւորութիւն — the software factory (kingdom/README.md) ----------
#
# Herdr is the observation surface: `herdr` opens the town's TUI, with every
# citizen a tab in the "kingdom" workspace. `just kingdom`, `just herratsayn`
# and `just kingdom-attach` were tmux-era wrappers around `tmux
# attach-session`; they were deleted when herdr became HQ (bd gqlc-98br).
# Mechanical readout (mail counts, unroutable beads, unit health) still lives
# in `km status`.

# create state dir, seat worktrees, and the herdr workspace with its per-seat
# agents (all asleep). Requires the herdr server to be running.
kingdom-up:
    kingdom/bin/km up

# graceful stop: notice by mail, then close the herdr workspace
kingdom-down:
    kingdom/bin/km down

# mechanical readout: mail counts, unroutable beads, stalled P0s, unit health.
# For the pane view, open `herdr`.
kingdom-status:
    kingdom/bin/km status

# check deps, install+enable the systemd user timers, point bd mail at km
kingdom-install:
    kingdom/bin/km doctor
    kingdom/bin/km install-units

# the full off-switch: disable and remove the systemd user timers, so nothing
# fires again after logout or reboot. State, mail and seat worktrees are left
# alone, so kingdom-install puts it back.
#
# kingdom-halt is the SOFT stop — the timers keep firing, they just wake
# nobody, and Սեդրակ or Անդրանիկ can lower it (Constitution VI.4; twelve of
# the other citizens can raise a halt but none of them may resume). This is
# the hard one, and it
# is the half that had no recipe: turning the town on was a documented
# one-liner and turning it off was not (bd gqlc-yxnf).
kingdom-uninstall:
    kingdom/bin/km uninstall-units

# raise the halt flag: the dispatcher wakes nobody until kingdom-resume. The
# raise is recorded against whoever made it, so like kingdom-resume it needs an
# identity: KINGDOM_SEAT=andranik just kingdom-halt "reason"
kingdom-halt reason="":
    kingdom/bin/km halt {{reason}}

# lower the halt flag. Constitution VI.4 reserves this to Սեդրակ or Անդրանիկ, so
# it needs an identity: KINGDOM_SEAT=andranik just kingdom-resume
kingdom-resume:
    kingdom/bin/km resume

# health checks for the kingdom machinery
kingdom-doctor:
    kingdom/bin/km doctor

# fast-forward the checkout the systemd timers execute; merging a km fix does not deploy it
kingdom-deploy:
    kingdom/bin/km deploy

# who else has an open PR touching a file (bd gqlc-zgka). Bare: the census of
# every contested path. `path <PATH>` before routing a bead, `pr <N>` for your
# own branch.
#
# Exit 1 means overlap was FOUND, so just prints `error: recipe ... failed` on
# the ordinary result. That line is the finding, not a fault. The status is
# passed through rather than swallowed because the third state matters: 2 means
# the query could not run, and a recipe that flattened all three to 0 would
# answer "nothing touches this file" for "I never looked" — the fail-open this
# whole tool exists to refuse.
kingdom-overlap *args="census":
    kingdom/bin/km-overlap {{args}}
