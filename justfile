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

# Where recipes that need a scratch directory allocate one.
scratch_root := "/tmp"

# Compares the just on PATH against the pin CI installs
# (.github/actions/setup-just/just-version, read here at run time so the number
# lives in one file), and refuses on mismatch with the install remedy. just's
# dump format is not a stability contract across releases, so a version nobody
# states is a red-in-CI-green-here nobody can reproduce (bd gqlc-rnyit). Not
# wired into any gate: repinning every host is a rollout, so this answers when
# asked rather than reddening checkouts whose host is not the pin yet.
check-just-version:
    #!/usr/bin/env bash
    set -euo pipefail
    pin_file="{{ justfile_directory() }}/.github/actions/setup-just/just-version"
    if [ ! -f "$pin_file" ]; then
        echo "error: $pin_file is absent, so there is no pin to check against." >&2
        exit 1
    fi
    want="$(tr -d '[:space:]' < "$pin_file")"
    if ! printf '%s' "$want" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
        echo "error: $pin_file does not hold a just version (got '$want')." >&2
        exit 1
    fi
    have="$(just --version | sed -n 's/^just //p')"
    if [ "$have" = "$want" ]; then
        echo "just $have matches the pin ($pin_file)"
        exit 0
    fi
    echo "error: just $have is on PATH and this tree pins $want ($pin_file)." >&2
    echo "       Install the pin:" >&2
    echo "         curl --proto '=https' --tlsv1.2 -sSfL https://just.systems/install.sh \\" >&2
    echo "           | bash -s -- --tag $want --to \"\$HOME/.local/bin\"" >&2
    exit 1

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
# This recipe only runs when someone runs it, and NOTHING now closes the window
# between the drifting write and the next `just test`. A Claude Code bash hook
# used to run a superset of this check on every tool call (bd gqlc-nzwa); it has
# been deleted, so the window is open again and `.githooks/hooks-drift-tripwire`
# — which fires only once the drift has already redirected git's hook lookup — is
# the only thing left that catches drift you did not go looking for.
#
# What this recipe compares is the configured VALUE and nothing else, so with
# core.hooksPath = .githooks but .githooks/ holding only *.sample files, or a
# hook file left non-executable, it exits 0 without a word and `doctor`, which
# depends on it, prints "ok". The verify-hooks-live arm below closes both states
# by running a hook rather than reading about one, and closes the
# environment-override state as well.
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
# tree; developers and CI take the default. The parameter is live and currently
# has no caller but a human.
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
# So every linked worktree is green while the shared cwd is bricked. The probe
# has to be chosen with that in mind: from the linked worktree
# `git rev-parse --is-bare-repository` answers FALSE while
# `git config --get core.bare` answers TRUE. A detector built on the former is
# blind from every worktree except the one that is already broken — and `just
# test` usually runs in a linked worktree. The config read is the arm that
# reaches.
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
# tree. The parameter is live and currently has no caller but a human.
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
            echo "       every session in one, keeps working while the shared cwd answers" >&2
            echo "       'fatal: this operation must be run in a work tree' to every command." >&2
            echo "       Repair: git -C '$dir' config --unset-all $key" >&2
        done
    done
    exit "$rc"

# puts ssh keepalives in the repository's own git config, so an ordinary
# `git push` survives .githooks/pre-push — bd gqlc-ehgg / GH #1414.
#
# THE DEFECT. git opens the transport to the remote BEFORE it runs pre-push,
# then this repository's pre-push holds it idle for the whole gate chain: the
# full go suite, every shell suite, golangci-lint. Twelve to fifteen minutes on
# a loaded machine, and longer the more sessions are pushing. GitHub's server-side
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
# else — git spawns no ssh over HTTPS, so there is nothing for the keepalive to
# attach to and no equivalent key to set. (An HTTPS rc=141 was reported on
# 2026-08-23 by a second lane, but the transport-shape measurement in
# bd gqlc-01pw shows no outstanding HTTPS request spans the hook window, so that
# report is unattributed rather than evidence against this fix.) It covers every push from this repository as it
# is configured: every worktree and the shared checkout resolve origin to
# git@github.com:areqag/gqlc.git, with no url.insteadOf rewrite, and remotes live
# in the shared config so a per-worktree difference is not reachable (measured
# 2026-08-23). A clone made over https is the uncovered case, and
# .githooks/push-transport-notice tells that operator so on every push rather
# than leaving the silence to be read as cover.
#
# WHY IT SELF-HEALS RATHER THAN REFUSING, unlike check-hooks above. The key is
# absent on every fresh clone and in every worktree registered before this
# landed, so a refusal would have failed every push in the repository on the day it
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
# reports the state it can establish and stops.
#
# AN ABSENT REMOTE HEAD HAS TWO CAUSES, and until bd gqlc-97rxk this recipe
# knew only one of them. A merge DELETES the head branch here — the repository
# sets delete_branch_on_merge, which is what makes the claim below a fact
# rather than a habit: `gh api repos/areqag/gqlc --jq .delete_branch_on_merge`
# answered true on 2026-09-02. So absence is the ordinary successful end state
# as well as the never-arrived one — and absence is what an author meets at
# session close, which is the one moment this recipe is advertised. Measured
# the same day: three branches, all pushed, all merged, all told their push had
# failed and to retry it. Re-pushing there recreates a head branch for a merged
# PR.
#
# Git cannot tell the two apart. `git merge-base --is-ancestor` calls a merged
# branch unmerged, because a squash leaves no tip of it on master; CLAUDE.md
# records the same trap for remote-branch pruning. GitHub can, so the absent
# arm asks it — and only that arm, because the state this recipe was written
# for (bd gqlc-ehgg) is answered by `git ls-remote` alone and must not cost a
# round trip.
#
# THE GITHUB CALL IS AN ADDED DEPENDENCY, so it degrades rather than fails
# closed: an absent or erroring `gh` yields UNKNOWN naming which of the two it
# hit, never the old verdict. Only GitHub answering "no PR from that head, in
# any state" now licenses NOT LANDED.
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
    if [ -n "$remote_sha" ] && [ "$remote_sha" = "$local_sha" ]; then
        echo "LANDED: origin/$branch is $remote_sha, the same commit as local."
        echo "        The push SUCCEEDED whatever rc git reported (bd gqlc-ehgg). Do not retry;"
        echo "        do not abandon this work as unpushed."
        exit 0
    fi
    if [ -n "$remote_sha" ]; then
        echo "DIVERGED: origin/$branch is $remote_sha, local is $local_sha."
        echo "          Something landed, but not this commit. Look before pushing again:"
        echo "            git fetch origin '$branch' && git log --oneline FETCH_HEAD...$branch"
        exit 1
    fi
    if ! command -v gh >/dev/null 2>&1; then
        echo "UNKNOWN: origin has no refs/heads/$branch, and gh is not installed here, so"
        echo "         the second cause of that absence cannot be ruled out: a squash merge"
        echo "         DELETES the head branch. Local is $local_sha. Do not conclude the push"
        echo "         failed, and do not re-push, before asking:"
        echo "           gh pr list --head '$branch' --state all"
        exit 1
    fi
    gh_err="$(mktemp)"
    trap 'rm -f "$gh_err"' EXIT
    if ! prs="$(gh pr list --head "$branch" --state all --json number,state,mergeCommit \
            --jq '.[] | "\(.state) \(.number) \(.mergeCommit.oid // "-")"' 2>"$gh_err")"; then
        echo "UNKNOWN: origin has no refs/heads/$branch, and GitHub could not be asked, so"
        echo "         the second cause of that absence cannot be ruled out: a squash merge"
        echo "         DELETES the head branch. Local is $local_sha. gh said:"
        sed 's/^/           /' "$gh_err"
        echo "         Do not conclude the push failed on this. Retry the question, not the push."
        exit 1
    fi
    merged="$(printf '%s\n' "$prs" | awk '$1 == "MERGED" { print; exit }')"
    if [ -n "$merged" ]; then
        read -r _ number oid <<<"$merged"
        echo "LANDED AND MERGED: PR #$number merged as $oid."
        echo "                   origin has no refs/heads/$branch because the merge DELETED"
        echo "                   it. That is the ordinary end state, not a failed push."
        echo "                   Local is $local_sha. Do NOT re-push: that recreates a head"
        echo "                   branch for a merged PR. To confirm by content rather than by"
        echo "                   this report, read the merged file on master:"
        echo "                     git show origin/master:<path>"
        exit 0
    fi
    if [ -n "$prs" ]; then
        echo "ARRIVED, NOT MERGED: origin has no refs/heads/$branch, but GitHub has a PR from"
        echo "                     that head, and a PR cannot be opened from a head that never"
        echo "                     arrived. So the push did not fail. Local is $local_sha."
        printf '%s\n' "$prs" | sed 's/^/                       /'
        echo "                     Nothing of this branch is on master. Read the PR before"
        echo "                     re-pushing; re-pushing is how a closed PR's head comes back."
        exit 1
    fi
    echo "NOT LANDED: origin has no refs/heads/$branch, and GitHub has no PR from that head"
    echo "            in any state. Local is $local_sha."
    echo "            The push did fail. Retry it; nothing on the remote is at stake."
    exit 1

# refuses to run when bd's auto-export cannot stage its own output — bd
# gqlc-c2ch / GH #1170.
#
# The defect, measured 2026-08-22 from a linked worktree: every `bd update` ended
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
# while a linked worktree's checked-out copy still carried its worktree-creation
# mtime, 1.9 MB smaller. So a linked worktree's own `.beads/issues.jsonl` never
# moves, and a session looking at its own tree can see neither the staleness nor
# the failure.
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

# This check is the acceptance of bd gqlc-bc26w, not a new instrument — it
# verifies the fix itself and does not surveil for anything else: a gate freeze
# in force at the time was ruled, 2026-09-01, not to reach a regression test for
# a defect being shipped.
#
# What it holds: bd-gh-sync's push selection asked only whether a bead already
# had a mirror, never what its status was, so a bead that CLOSED before it was
# ever mirrored was offered to GitHub on every push, by everyone, forever.
# The fix is one `continue`. This is the row that reddens when it is edited away.
#
# It runs the REAL selection, cut out of .githooks/bd-gh-sync between its own
# invocation line and the heredoc terminator, rather than a restatement of the
# rule. A restatement keeps passing after the hook stops agreeing with it, which
# is the failure this exists to notice. Every way the cut can come back with the
# wrong bytes — anchor missing, anchor duplicated, heredoc unterminated, block
# empty — is fatal here, because an empty selection emits an empty plan and an
# empty plan is the shape of a healthy run.
#
# The comparator is FALSIFIED in band on every run: it is re-driven with a plan
# the selection never produced, one offering the closed bead, and this fails if
# it accepts it. A comparator that cannot fail is a row that witnesses nothing.
#
# ~100ms: one python3 start, no network, no git and no bd. Wired into `test`,
# which is also what puts it in .githooks/pre-push — that hook's gate is
# `just test`, so a second call site there would only run it twice.
[private]
check-bd-gh-sync-selection:
    #!/usr/bin/env bash
    set -uo pipefail
    hook={{ quote(justfile_directory() + "/.githooks/bd-gh-sync") }}
    label_gate={{ quote(justfile_directory() + "/.github/scripts/check-label-lengths.py") }}
    for f in "$hook" "$label_gate"; do
        if [ ! -f "$f" ]; then
            echo "error: $f is missing, so bd-gh-sync's push selection cannot be run and" >&2
            echo "       this check would report a pass over nothing (bd gqlc-bc26w)." >&2
            exit 1
        fi
    done
    scratch="$(mktemp -d)" || exit 1
    trap 'rm -rf "$scratch"' EXIT

    # The anchor is the start of the selection's own invocation line. The file
    # carries six `PYEOF` heredocs and this names the one that decides what is
    # offered to GitHub; the other five answer different questions.
    #
    # It stops before that line's trailing `\` continuation and carries no
    # backslash at all, which is load-bearing. awk performs escape-sequence
    # processing on a `-v` assignment, and a lone trailing backslash is where
    # the awks disagree: gawk 5.4 keeps it, mawk — /usr/bin/awk on the CI
    # runners — does not. Measured on this branch: with the backslash the anchor
    # matched once in every local worktree and 0 times on the runner. That was
    # the fail-closed refusal below rather than a false pass, but it is still a
    # red no local checkout can reproduce. With no backslash the escape processing is a
    # no-op by construction rather than by luck, and ENVIRON does none at all.
    anchor='python3 - "$_tmp/beads.json" "$_label_gate"'
    erc=0
    ANCHOR="$anchor" awk '
        index($0, ENVIRON["ANCHOR"]) == 1 { anchors++; armed = 1; next }
        armed && index($0, "<<") && index($0, "PYEOF") { armed = 0; inblock = 1; opened++; next }
        inblock && $0 == "PYEOF" { inblock = 0; closed++; next }
        inblock { print }
        END {
            if (anchors != 1) { printf "the anchor line matched %d time(s), want exactly 1\n", anchors + 0 >"/dev/stderr"; exit 3 }
            if (opened != 1) { printf "the heredoc after the anchor opened %d time(s), want 1\n", opened + 0 >"/dev/stderr"; exit 3 }
            if (closed != 1) { printf "that heredoc closed %d time(s), want 1\n", closed + 0 >"/dev/stderr"; exit 3 }
        }
    ' "$hook" >"$scratch/selection.py" 2>"$scratch/extract.err" || erc=$?
    if [ "$erc" -ne 0 ] || [ ! -s "$scratch/selection.py" ]; then
        echo "error: could not cut bd-gh-sync's push selection out of $hook (bd gqlc-bc26w)." >&2
        sed 's/^/       /' "$scratch/extract.err" >&2
        echo "       Refusing rather than judging a block nobody read. If the invocation was" >&2
        echo "       deliberately reshaped, update the anchor in this recipe to match it." >&2
        exit 1
    fi

    # Seven beads, one per cell of the decision the fix changed. The over-cap
    # label is 52 characters against the gate's cap of 50, and nothing asserts
    # that separately: were it short, its row would read NEW and the comparison
    # below would fail.
    cat >"$scratch/beads.json" <<'BEADS'
    [
      {"id": "probe-closed-plain",    "status": "closed",   "external_ref": null, "labels": ["subject:.githooks"]},
      {"id": "probe-closed-overcap",  "status": "closed",   "external_ref": null, "labels": ["subject:.githooks/probe/deliberately/over/the/cap.sh"]},
      {"id": "probe-open-plain",      "status": "open",     "external_ref": null, "labels": ["subject:.githooks"]},
      {"id": "probe-open-overcap",    "status": "open",     "external_ref": null, "labels": ["subject:.githooks/probe/deliberately/over/the/cap.sh"]},
      {"id": "probe-open-mirrored",   "status": "open",     "external_ref": "https://github.com/areqag/gqlc/issues/1", "labels": ["subject:.githooks"]},
      {"id": "probe-blocked-plain",   "status": "blocked",  "external_ref": null, "labels": ["subject:.githooks"]},
      {"id": "probe-deferred-plain",  "status": "deferred", "external_ref": null, "labels": ["subject:.githooks"]}
    ]
    BEADS

    # Both closed beads are absent, which is the fix. blocked and deferred are
    # present, which is its narrowness: they are live work parked and still want
    # a board row. COUNT proves the selection walked the whole fixture rather
    # than stopping early, which would also produce an absence.
    cat >"$scratch/expected.txt" <<'PLAN'
    NEW probe-open-plain
    UNMIRRORABLE probe-open-overcap
    NOTE probe-open-overcap
    NEW probe-blocked-plain
    NEW probe-deferred-plain
    COUNT 7
    DONE
    PLAN

    # The NOTE line's tail is the label gate's remedy wording, which this check
    # does not own and which gqlc-uy7j moved once already. The id on it is what
    # the selection decided, so that is what is compared.
    compare() {
        sed -E 's/^(NOTE [^ ]+) .*/\1/' "$1" | diff -u "$scratch/expected.txt" -
    }

    prc=0
    python3 "$scratch/selection.py" "$scratch/beads.json" "$label_gate" \
        >"$scratch/plan.txt" 2>"$scratch/plan.err" || prc=$?
    if [ "$prc" -ne 0 ]; then
        echo "error: bd-gh-sync's push selection exited $prc on a seven-bead fixture, so" >&2
        echo "       what it decides cannot be read at all (bd gqlc-bc26w). It said:" >&2
        sed 's/^/       /' "$scratch/plan.err" >&2
        exit 1
    fi
    if ! compare "$scratch/plan.txt" >"$scratch/verdict.diff" 2>&1; then
        echo "error: bd-gh-sync's push selection no longer decides what gqlc-bc26w fixed." >&2
        echo "       Expected on the left, what the live hook produced on the right:" >&2
        sed 's/^/       /' "$scratch/verdict.diff" >&2
        if [ -s "$scratch/plan.err" ]; then
            echo "       The selection also wrote to stderr:" >&2
            sed 's/^/       /' "$scratch/plan.err" >&2
        fi
        echo "       A closed bead reappearing here is offered to GitHub on every push by" >&2
        echo "       everyone, forever, and mints an issue for finished work." >&2
        exit 1
    fi

    # The falsifier, in band: a plan the run never produced, differing only by
    # offering the bead the fix withholds.
    { echo "NEW probe-closed-plain"; cat "$scratch/plan.txt"; } >"$scratch/synthetic.txt"
    if compare "$scratch/synthetic.txt" >/dev/null 2>&1; then
        echo "error: the comparator accepted a plan offering probe-closed-plain — a bead" >&2
        echo "       closed before it was ever mirrored — so the row above passed without" >&2
        echo "       being able to fail (bd gqlc-bc26w)." >&2
        exit 1
    fi

# This check is the acceptance of bd gqlc-xpgdc, and like the recipe above it
# verifies that fix and surveils for nothing else.
#
# What it holds: every "the bead list came back empty" line bd-gh-sync prints is
# equally true of a repository with no beads and of a bd aimed at a ledger
# nobody meant, and none of them named the path. Measured 2026-09-02 on the push
# arm: an empty ledger and BEADS_DIR pointed at another checkout's .beads
# produced BYTE-IDENTICAL output. That ambiguity ran four hours across ten
# concurrent sessions with this script's own empty-list line as the only tell
# (gqlc-zpjuc). `_ledger` is what
# separates them, and these are the rows that redden when it is edited away.
#
# It runs the REAL `_ledger`, cut out of .githooks/bd-gh-sync, rather than a
# restatement of it — a restatement keeps passing after the hook stops agreeing
# with it. No bd, no gh, no git, no network: the helper reads the environment
# and nothing else, which is the property that makes it cheap to run here and
# is itself asserted below.
#
# WHAT IT DOES NOT WITNESS, said plainly. It does not run the hook end to end,
# so it cannot see the arms that CALL `_ledger` actually being reached; the call
# sites are pinned textually instead, with comments stripped so a commented-out
# call cannot vouch for a live one. The end-to-end run was measured by hand
# against stubbed bd/gh while fixing gqlc-xpgdc and is deliberately not
# committed: PR #1595 deleted the stub-driven hook suite because a stub encodes
# belief rather than witness, and `just test` is also the pre-push hook, where
# wall-time is paid by everyone on every push.
[private]
check-bd-gh-sync-ledger:
    #!/usr/bin/env bash
    set -uo pipefail
    hook={{ quote(justfile_directory() + "/.githooks/bd-gh-sync") }}
    if [ ! -f "$hook" ]; then
        echo "error: $hook is missing, so bd-gh-sync's ledger naming cannot be run and" >&2
        echo "       this check would report a pass over nothing (bd gqlc-xpgdc)." >&2
        exit 1
    fi
    scratch="$(mktemp -d)" || exit 1
    trap 'rm -rf "$scratch"' EXIT

    # Cut the helper out by its definition line. Every way the cut can come back
    # wrong — anchor missing, anchor duplicated, body unterminated, body empty —
    # is fatal, because an empty file sources cleanly and would leave `_ledger`
    # undefined while every row below still "passed".
    erc=0
    awk '
        $0 == "_ledger() {" { anchors++; inblock = 1; print; next }
        inblock && $0 == "}" { inblock = 0; closed++; print; next }
        inblock { print }
        END {
            if (anchors != 1) { printf "the _ledger definition matched %d time(s), want exactly 1\n", anchors + 0 >"/dev/stderr"; exit 3 }
            if (closed != 1) { printf "that definition closed %d time(s), want 1\n", closed + 0 >"/dev/stderr"; exit 3 }
        }
    ' "$hook" >"$scratch/ledger.sh" 2>"$scratch/extract.err" || erc=$?
    if [ "$erc" -ne 0 ] || [ ! -s "$scratch/ledger.sh" ]; then
        echo "error: could not cut _ledger out of $hook (bd gqlc-xpgdc)." >&2
        sed 's/^/       /' "$scratch/extract.err" >&2
        echo "       Refusing rather than judging a helper nobody read. If it was" >&2
        echo "       deliberately reshaped, update the anchor in this recipe to match." >&2
        exit 1
    fi

    # Driven under the hook's own shell options, because `set -u` is what makes
    # the unset arm a real question: an unguarded $BEADS_DIR would abort there.
    probe="/probe/gqlc-xpgdc/not-a-real-ledger"
    drive='set -Eeuo pipefail; . "$0"; _ledger'
    named=$(BEADS_DIR="$probe" bash -c "$drive" "$scratch/ledger.sh" 2>"$scratch/named.err"); nrc=$?
    unset_out=$(env -u BEADS_DIR bash -c "$drive" "$scratch/ledger.sh" 2>"$scratch/unset.err"); urc=$?
    if [ "$nrc" -ne 0 ] || [ "$urc" -ne 0 ]; then
        echo "error: _ledger exited non-zero ($nrc named, $urc unset), so what bd-gh-sync" >&2
        echo "       reports its ledger to be cannot be read at all (bd gqlc-xpgdc)." >&2
        sed 's/^/       /' "$scratch/named.err" "$scratch/unset.err" >&2
        exit 1
    fi

    # Row 1: a named ledger is quoted verbatim. This is the byte that was
    # missing, and the whole of the fix.
    case "$named" in
        *"$probe"*) ;;
        *) echo "error: _ledger did not name the BEADS_DIR it was given (bd gqlc-xpgdc)." >&2
           echo "       BEADS_DIR=$probe produced: $named" >&2
           echo "       A diagnosis that omits the path cannot tell an empty ledger from" >&2
           echo "       a bd aimed somewhere nobody meant; that is the defect this fixes." >&2
           exit 1 ;;
    esac

    # Row 2: the two states must DIFFER. Naming the path is only useful if the
    # genuinely-empty case says something else, and a helper hard-coded to any
    # constant would satisfy row 1 alone.
    if [ "$named" = "$unset_out" ]; then
        echo "error: _ledger answered identically with BEADS_DIR set and unset, so the" >&2
        echo "       two states bd-gh-sync must distinguish are again the same bytes" >&2
        echo "       (bd gqlc-xpgdc). Both said: $named" >&2
        exit 1
    fi

    # Row 3: the unset arm says so rather than printing an empty path, and does
    # not smuggle the probe in from the ambient environment.
    case "$unset_out" in
        *"$probe"*) echo "error: _ledger named $probe with BEADS_DIR unset — it is reading" >&2
                    echo "       something other than its own environment (bd gqlc-xpgdc)." >&2
                    exit 1 ;;
        *unset*) ;;
        *) echo "error: with BEADS_DIR unset _ledger did not say so; it said: $unset_out" >&2
           echo "       An empty or silent path reads as a ledger nobody chose (bd gqlc-xpgdc)." >&2
           exit 1 ;;
    esac

    # The falsifier, in band. Row 2 is the load-bearing row — it is what a
    # helper hard-coded to any constant would fail — so it is the one that has
    # to be shown capable of failing. Its predicate is re-run here against a
    # deliberately environment-blind _ledger, and this refuses unless that
    # predicate FIRES on it. Row 1 satisfies the same fixture (the constant is
    # the probe path), which is precisely why row 1 alone would witness nothing.
    #
    # Not a second assertion about the real helper: by this point row 2 has
    # already exited on `named = unset_out`, so any further test phrased over
    # those two values is unreachable and would vouch for nothing.
    cat >"$scratch/inert.sh" <<'INERT'
    _ledger() { echo "BEADS_DIR=/probe/gqlc-xpgdc/not-a-real-ledger"; }
    INERT
    f_named=$(BEADS_DIR="$probe" bash -c ". '$scratch/inert.sh'; _ledger" 2>/dev/null)
    f_unset=$(env -u BEADS_DIR bash -c ". '$scratch/inert.sh'; _ledger" 2>/dev/null)
    case "$f_named" in
        *"$probe"*) ;;
        *) echo "error: the falsifier fixture does not satisfy row 1, so it does not" >&2
           echo "       isolate row 2 as the row under test (bd gqlc-xpgdc)." >&2
           exit 1 ;;
    esac
    if [ "$f_named" != "$f_unset" ]; then
        echo "error: row 2's predicate did not fire on a _ledger that ignores its" >&2
        echo "       environment, so row 2 above passed without being able to fail" >&2
        echo "       (bd gqlc-xpgdc). The fixture answered '$f_named' set and" >&2
        echo "       '$f_unset' unset; an environment-blind helper must answer both" >&2
        echo "       alike, which is exactly what row 2 rejects." >&2
        exit 1
    fi

    # The call sites. Comments are stripped first: a `$(_ledger)` inside the
    # block comment above the helper would otherwise vouch for a live call that
    # had been deleted (bd gqlc-xpgdc, and the same trap as a raw-bytes grep
    # accepting commented-out evidence).
    sed 's/[[:space:]]*#.*$//' "$hook" >"$scratch/nocomments.sh"
    missing=0
    while IFS='|' read -r anchor what; do
        if ! awk -v a="$anchor" '
            index($0, a) { armed = NR }
            armed && NR > armed && NR <= armed + 4 && index($0, "$(_ledger)") { found = 1 }
            END { exit found ? 0 : 1 }
        ' "$scratch/nocomments.sh"; then
            echo "error: the $what diagnosis no longer names its ledger within 4 lines of" >&2
            echo "       \"$anchor\" (bd gqlc-xpgdc). That diagnosis is now true of an empty" >&2
            echo "       ledger and of a bd aimed at the wrong ledger alike, which is exactly" >&2
            echo "       the four-hour ambiguity of gqlc-zpjuc." >&2
            missing=1
        fi
    done <<'ANCHORS'
    the bead list was empty when this run chose what to push|push-side empty-list
    the bead list came back empty when this run chose what to|pull-side empty-list
    ANCHORS
    [ "$missing" -eq 0 ] || exit 1

# This check is the acceptance of bd gqlc-o22k.
#
# What it holds: the pull's append-only rule cannot tell a block APPENDED on
# GitHub from the same block CUT in bd — the two leave identical bodies — so an
# edit-time ordering is the only thing separating them. It used to read GitHub's
# `updatedAt`, which is not an edit time: it moves on a comment and on a close,
# and bd-gh-sync's own push path writes both when it auto-closes a mirror. So a
# bead whose description was cut locally and then closed was pulled back with
# the cut block restored, silently, by this file acting on its own echo. The fix
# reads `lastEditedAt` (coalesced to `createdAt`, null for a body never edited).
#
# It runs the REAL selection, cut out of .githooks/bd-gh-sync, rather than a
# restatement of the rule — a restatement keeps passing after the hook stops
# agreeing with it. Every way the cut can come back wrong (anchor missing or
# duplicated, heredoc unterminated, block empty) is fatal, because an empty
# selection emits an empty plan and an empty plan is the shape of a healthy run.
#
# ~100ms: one python3 start, no network, no git, no bd and no gh.
[private]
check-bd-gh-sync-pull-tiebreak:
    #!/usr/bin/env bash
    set -uo pipefail
    hook={{ quote(justfile_directory() + "/.githooks/bd-gh-sync") }}
    if [ ! -f "$hook" ]; then
        echo "error: $hook is missing, so bd-gh-sync's pull selection cannot be run and" >&2
        echo "       this check would report a pass over nothing (bd gqlc-o22k)." >&2
        exit 1
    fi
    scratch="$(mktemp -d)" || exit 1
    trap 'rm -rf "$scratch"' EXIT

    # Names the PULL selection. The file carries six PYEOF heredocs; the push
    # one begins `python3 - "$_tmp/beads.json" "$_label_gate"` and this is the
    # only other that opens on beads.json. Matched anywhere in the line rather
    # than at column 1 because this invocation is nested in an `if` block, and
    # carries no trailing backslash: awk processes escapes in a -v assignment
    # and the awks disagree about a lone trailing one, so ENVIRON is used.
    anchor='python3 - "$_tmp/beads.json" "$_tmp/gh.json"'
    erc=0
    ANCHOR="$anchor" awk '
        index($0, ENVIRON["ANCHOR"]) > 0 && !armed && !seen { anchors++; armed = 1; next }
        armed && index($0, "<<") && index($0, "PYEOF") { armed = 0; inblock = 1; opened++; next }
        inblock && $0 == "PYEOF" { inblock = 0; closed++; seen = 1; next }
        inblock { print }
        END {
            if (anchors != 1) { printf "the anchor line matched %d time(s), want exactly 1\n", anchors + 0 >"/dev/stderr"; exit 3 }
            if (opened != 1) { printf "the heredoc after the anchor opened %d time(s), want 1\n", opened + 0 >"/dev/stderr"; exit 3 }
            if (closed != 1) { printf "that heredoc closed %d time(s), want 1\n", closed + 0 >"/dev/stderr"; exit 3 }
        }
    ' "$hook" >"$scratch/selection.py" 2>"$scratch/extract.err" || erc=$?
    if [ "$erc" -ne 0 ] || [ ! -s "$scratch/selection.py" ]; then
        echo "error: could not cut bd-gh-sync's pull selection out of $hook (bd gqlc-o22k)." >&2
        sed 's/^/       /' "$scratch/extract.err" >&2
        echo "       Refusing rather than judging a block nobody read. If the invocation was" >&2
        echo "       deliberately reshaped, update the anchor in this recipe to match it." >&2
        exit 1
    fi

    # Four beads, each with the SAME local description and the same
    # `updated_at`, so the edit time is the only input that separates them.
    cat >"$scratch/beads.json" <<'BEADS'
    [
     {"id":"probe-cut-then-autoclose","status":"open",
      "external_ref":"https://github.com/areqag/gqlc/issues/1",
      "description":"line1\nline2","updated_at":"2026-09-10T12:00:00Z"},
     {"id":"probe-genuine-gh-append","status":"open",
      "external_ref":"https://github.com/areqag/gqlc/issues/2",
      "description":"line1\nline2","updated_at":"2026-09-10T12:00:00Z"},
     {"id":"probe-edit-time-missing","status":"open",
      "external_ref":"https://github.com/areqag/gqlc/issues/3",
      "description":"line1\nline2","updated_at":"2026-09-10T12:00:00Z"},
     {"id":"probe-never-edited","status":"open",
      "external_ref":"https://github.com/areqag/gqlc/issues/4",
      "description":"line1\nline2","updated_at":"2026-09-10T12:00:00Z"}
    ]
    BEADS

    # Every body properly extends its bead description, so all four clear the
    # prefix test and reach the tiebreak. That is the point: the prefix test
    # cannot separate #1 from #2.
    cat >"$scratch/gh.json" <<'GH'
    [
     {"number":1,"state":"open","body":"line1\nline2\nBLOCK CUT IN BD"},
     {"number":2,"state":"open","body":"line1\nline2\nAPPENDED ON GITHUB"},
     {"number":3,"state":"open","body":"line1\nline2\nNO EDIT TIME KNOWN"},
     {"number":4,"state":"open","body":"line1\nline2\nAPPENDED AT CREATION"}
    ]
    GH

    # TWO concatenated documents with no enclosing array — the shape
    # `gh api graphql --paginate` actually writes. json.load reads the first and
    # raises on the second, so #4 being decided at all is what witnesses the
    # stream decoder. #1 was last body-edited BEFORE the bead's cut; #2 after
    # it; #3 is absent from the map entirely; #4 has never been edited, so its
    # createdAt is the coalesce.
    cat >"$scratch/ghedit.json" <<'EDIT'
    {"data":{"repository":
      {"issues":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},
       "nodes":[
        {"number":1,"lastEditedAt":"2026-09-10T11:00:00Z","createdAt":"2026-09-10T10:00:00Z"},
        {"number":2,"lastEditedAt":"2026-09-10T14:00:00Z","createdAt":"2026-09-10T10:00:00Z"}]}}}}
    {"data":{"repository":
      {"issues":{"pageInfo":{"hasNextPage":false,"endCursor":null},
       "nodes":[
        {"number":4,"lastEditedAt":null,"createdAt":"2026-09-10T14:00:00Z"}]}}}}
    EDIT

    cat >"$scratch/expected.txt" <<'PLAN'
    HOLD probe-cut-then-autoclose 1 gh-not-newer-than-bd
    ALLOW probe-genuine-gh-append
    HOLD probe-edit-time-missing 3 gh-body-edit-time-unavailable
    ALLOW probe-never-edited
    COUNT 4
    DONE
    PLAN

    run() {
        python3 "$scratch/selection.py" "$scratch/beads.json" "$scratch/gh.json" \
            999 areqag/gqlc "$1" 2>"$scratch/plan.err"
    }

    prc=0
    run "$scratch/ghedit.json" >"$scratch/plan.txt" || prc=$?
    if [ "$prc" -ne 0 ]; then
        echo "error: bd-gh-sync's pull selection exited $prc on a four-bead fixture, so" >&2
        echo "       what it decides cannot be read at all (bd gqlc-o22k). It said:" >&2
        sed 's/^/       /' "$scratch/plan.err" >&2
        exit 1
    fi
    if ! diff -u "$scratch/expected.txt" "$scratch/plan.txt" >"$scratch/verdict.diff" 2>&1; then
        echo "error: bd-gh-sync's pull tiebreak no longer decides what gqlc-o22k fixed." >&2
        echo "       Expected on the left, what the live hook produced on the right:" >&2
        sed 's/^/       /' "$scratch/verdict.diff" >&2
        echo "       probe-cut-then-autoclose turning to ALLOW is the silent one: it writes" >&2
        echo "       a locally-deleted block back over the bead, triggered by this hook's" >&2
        echo "       own auto-close comment." >&2
        exit 1
    fi

    # The falsifier, in band. Same selection, same beads, same bodies — the ONE
    # difference is that #1 carries the time GitHub's `updatedAt` would have
    # reported, the auto-close comment at 13:00 rather than the body edit at
    # 11:00. That is the pre-fix input, and it must flip #1 to ALLOW. If it does
    # not, the tiebreak is no longer reading the edit time at all and the row
    # above passed without being able to fail.
    cat >"$scratch/ghedit.pre.json" <<'PRE'
    {"data":{"repository":
      {"issues":{"pageInfo":{"hasNextPage":false,"endCursor":null},
       "nodes":[
        {"number":1,"lastEditedAt":"2026-09-10T13:00:00Z","createdAt":"2026-09-10T10:00:00Z"},
        {"number":2,"lastEditedAt":"2026-09-10T14:00:00Z","createdAt":"2026-09-10T10:00:00Z"},
        {"number":4,"lastEditedAt":null,"createdAt":"2026-09-10T14:00:00Z"}]}}}}
    PRE
    frc=0
    run "$scratch/ghedit.pre.json" >"$scratch/pre.txt" || frc=$?
    if [ "$frc" -ne 0 ] || ! command grep -qx "ALLOW probe-cut-then-autoclose" "$scratch/pre.txt"; then
        echo "error: feeding the pull selection the timestamp \`updatedAt\` would have" >&2
        echo "       reported did NOT flip probe-cut-then-autoclose to ALLOW (exit $frc), so" >&2
        echo "       the comparison above is not reading the body-edit time and cannot fail" >&2
        echo "       (bd gqlc-o22k). It produced:" >&2
        sed 's/^/       /' "$scratch/pre.txt" >&2
        exit 1
    fi

# The rows for .githooks/bd-prime-guarded, the wrapper the SessionStart and
# PreCompact hooks in .claude/settings.json run instead of a bare `bd prime`
# (bd gqlc-q2jb). ~2s, and it drives the REAL bd: the allow half's claim is that
# a fresh checkout still bootstraps, and a stub would only encode the belief.
#
# ENROLLED UNDER `tidy`, in ci.yml and in the `gates` recipe (bd gqlc-kip5). That
# job already carries the checks needing neither Docker nor a Go build, and it is
# already required on master, so this claimed a context that exists rather than a
# ninth one somebody would have to decide to require. What it costs tidy is a
# PINNED bd: ci.yml downloads the 1.0.4 release tarball, the version this fleet
# deploys, and asserts the version it got before running these rows against it.
# Deliberately not the latest release — bd-behaviour.yml takes the latest on
# purpose, because its job is to learn that a future bd breaks an assumption, and
# that is the right shape for an alarm and the wrong one for a merge gate.
#
# The rows needed one change to be runnable outside a primed workspace: a CI
# checkout carries .beads config with no database, the database being gitignored.
# The measurement is at A2 in the rows.
test-bd-prime-guard:
    @.githooks/bd-prime-guarded.rows .githooks/bd-prime-guarded

# The rows for `just complexity`'s EXIT CODE, which is the only thing
# .githooks/pre-commit has to tell "a function is over the gate" from "nothing
# was graded" (bd gqlc-f0x1, gqlc-i8j9, gqlc-9nj3). ~4s: each row is a real
# invocation of the real recipe over a probe package it writes, because the
# property under test is what golangci-lint does with a broken tree and a stub
# would only encode the belief about it.
#
# ENROLLED IN `just gates` AND IN ci.yml's lint job, the same way
# test-bd-prime-guard above is enrolled under `tidy`. It
# rides `lint` rather than taking a context of its own: the job already provides
# Go, just, the pinned golangci-lint and shellcheck, and a required context
# added here is one a repository admin has to enable by hand before it blocks
# anything.
test-complexity-exit-code: sweep-discovery-probes ensure-golangci
    @.githooks/complexity-exit-code.rows justfile

# health check for local dev environment; extend as new drift modes emerge
doctor: check-hooks check-worktree-upstream check-shared-config check-beads-export check-push-keepalive
    @echo "ok"

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
# GQLC_PROVISION_ATTEMPTS / GQLC_PROVISION_DELAY size the budget. The two
# variables are still read here, and nothing exercises them now. An attempts
# value below 1 runs the loop zero times and falls through to the error, so a
# malformed budget blocks rather than passes.
#
# AND THEN REFUSES A LINTER OLDER THAN THE TOOLCHAIN, by name (bd gqlc-6rf3).
# just reads the pin from the justfile of the tree you are standing in, so a
# branch based before the last pin bump provisions the OLD linter — and a
# go1.N-built golangci-lint cannot load go1.(N+1) source. It dies inside
# go/types with a stack trace that names neither the pin nor the branch base,
# and nothing anywhere says "your base is old". Measured across three trees:
# same branch, panic before `git merge origin/master` and green after. It cost
# most of a session, and then cost two wrong repository-wide announcements — the
# second of which told people to declare a WORKING gate unrun in their PR
# bodies, which is the real loss, because a gate everyone ritually disclaims can
# no longer be told apart from one that genuinely did not run.
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
    want="{{ golangci_version }}"
    if [ "$({{ quote(golangci) }} version --short 2>/dev/null || true)" != "${want#v}" ]; then
        echo "provisioning golangci-lint $want into .bin/" >&2
        attempts="${GQLC_PROVISION_ATTEMPTS:-4}"
        delay="${GQLC_PROVISION_DELAY:-2}"
        attempt=1
        installed=0
        while [ "$attempt" -le "$attempts" ]; do
            if curl --proto '=https' --tlsv1.2 -sSfL \
                    "https://raw.githubusercontent.com/golangci/golangci-lint/$want/install.sh" \
                | sh -s -- -b {{ quote(justfile_directory() + "/.bin") }} "$want"; then
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
    built="$({{ quote(golangci) }} version 2>/dev/null | sed -n 's/.*built with go\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p' | head -n 1 || true)"
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
    want="{{ shellcheck_version }}"
    have="$({{ quote(shellcheck) }} --version 2>/dev/null | sed -n 's/^version: //p' || true)"
    if [ "$have" = "${want#v}" ]; then
        exit 0
    fi
    echo "provisioning shellcheck $want into .bin/" >&2
    mkdir -p {{ quote(justfile_directory() + "/.bin") }}
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
    install -m 0755 "$stage/shellcheck-$want/shellcheck" {{ quote(shellcheck) }}

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
    want="{{ ruff_version }}"
    have="$({{ quote(ruff) }} --version 2>/dev/null | sed -n 's/^ruff //p' || true)"
    if [ "$have" = "$want" ]; then
        exit 0
    fi
    echo "provisioning ruff $want into .bin/" >&2
    mkdir -p {{ quote(justfile_directory() + "/.bin") }}
    stage="$(mktemp -d)"
    trap 'rm -rf "$stage"' EXIT
    curl --proto '=https' --tlsv1.2 -sSfL --retry 5 --retry-all-errors --retry-delay 2 \
        "https://github.com/astral-sh/ruff/releases/download/$want/ruff-$(uname -m)-unknown-linux-gnu.tar.gz" \
        | tar -xz -C "$stage"
    install -m 0755 "$stage/ruff-$(uname -m)-unknown-linux-gnu/ruff" {{ quote(ruff) }}

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
# test-hooks made the same fail-open refusal until PR #1595 (f6dc4c7b) deleted
# that recipe with the suites it ran, so lint-hooks is the only peer left.
#
# The empty case FAILS. A glob that matches nothing lints nothing and exits 0,
# which on every dashboard is the shape of a clean tree — the same fail-open
# refusal lint-hooks makes.
lint-python dir=".github/scripts": ensure-ruff
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{ dir }}"
    if [ ! -d "$dir" ]; then
        echo "error: '$dir' is not a directory, so ruff has nothing to lint" >&2
        exit 1
    fi
    # Selected by suffix OR by shebang, because a hook cannot carry a suffix:
    # git invokes .githooks entries by name. Suffix alone once left the largest
    # Python file in the repo read by no linter at all, while it sat inside a
    # directory lint-hooks already scans and in a language this recipe already
    # lints. It fell between the two selectors (gqlc-tmxex).
    #
    # Unclassified files are NOT refused here, unlike lint-hooks. That refusal
    # is already made over both directories this recipe is pointed at, so
    # repeating it would only give one missing shebang two voices.
    files=()
    while IFS= read -r f; do
        case "$f" in
            *.py) files+=("$f"); continue ;;
        esac
        head=""
        IFS= read -r head <"$f" || true
        case "$head" in
            "#!"*python*) files+=("$f") ;;
        esac
    done < <(find "$dir" -type f | sort)
    if [ "${#files[@]}" -eq 0 ]; then
        echo "error: no python file found under $dir — no .py suffix and no python" >&2
        echo "       shebang — so ruff ran over nothing and exited 0 over it," >&2
        echo "       indistinguishable from every file passing. Either the" >&2
        echo "       scripts moved, or this is not the repository root (bd gqlc-tqi4)." >&2
        exit 1
    fi
    echo "ruff {{ ruff_version }} over ${#files[@]} python file(s) under $dir:"
    printf '  %s\n' "${files[@]}"
    {{ ruff }} check --no-cache \
        --config {{ quote(justfile_directory() + "/.github/ruff.toml") }} -- "${files[@]}"

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
# The parameter is live and currently has no caller but a human.
#
# shellcheck every hook, selected by shebang; CI and developers take the default.
lint-hooks dir=".githooks": ensure-shellcheck
    #!/usr/bin/env bash
    set -euo pipefail
    dir="{{ dir }}"
    if [ ! -d "$dir" ]; then
        echo "error: '$dir' is not a directory, so shellcheck has nothing to lint" >&2
        echo "       and this gate is watching nothing (bd gqlc-jhi2)." >&2
        exit 1
    fi

    scripts=()
    unclassified=()
    while IFS= read -r f; do
        # Skip what git ignores: the sweep asks whether shellcheck should
        # watch a file, which is meaningless for a path that is not in the
        # repository. A __pycache__/ written by py_compile used to red the
        # arm with advice that cannot be followed on a .pyc (bd gqlc-5kuhz).
        # Outside a git tree check-ignore errors and the file stays swept,
        # so the refusal below cannot shrink to silence.
        if git check-ignore -q -- "$f" 2>/dev/null; then
            continue
        fi
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
    echo "shellcheck {{ shellcheck_version }} over ${#scripts[@]} shell script(s) under $dir:"
    printf '  %s\n' "${scripts[@]}"
    {{ shellcheck }} -- "${scripts[@]}"

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
# raised by a recipe that pasted a mode argument raw into its case head, so a
# value carrying a `)` was shell syntax there rather than data. Binding it
# through `{{{{quote(...)}}}}` fixed the recipe and retired the exclusion with it
# (bd gqlc-4seg).
#
# The justfile under the gate is justfile() itself: the path parameter this
# recipe once carried lost its only exerciser with internal/tools/ciguard in
# PR #1595 (bd gqlc-gu7ao).
[private]
lint-just: ensure-shellcheck
    #!/usr/bin/env bash
    set -euo pipefail
    file="{{ justfile() }}"
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
    echo "shellcheck {{ shellcheck_version }} over ${#bodies[@]} justfile recipe body/bodies:"
    cut -f1 <"$work/index" | sed 's/^/  /'
    {{ shellcheck }} --severity=warning -- "${bodies[@]}"

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
    probe="$(mktemp -d "{{ scratch_root }}/gqlc-fmtprobe-XXXXXX")"
    trap 'rm -rf "${probe}"' EXIT
    cp {{ quote(justfile_directory() + "/.golangci.yml") }} "${probe}/.golangci.yml"
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
            {{ quote(lint_lock) }} {{ quote(golangci) }} run ./... 2>&1 ) || return $?
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
    names=({{ discovery_probes }})
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

    evaluated="$('{{ just_executable() }}' --justfile '{{ justfile() }}' --evaluate)"
    dumped="$('{{ just_executable() }}' --justfile '{{ justfile() }}' --dump)"
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
# shellcheck over the hooks + CI-script trees, as linters + formatter
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
# It runs over .github/scripts alone. A second arm over .githooks was dropped
# with the last Python hook there (bd gqlc-7dkxl): lint-python REFUSES a
# directory holding no Python rather than passing vacuously, so the arm did not
# go quiet when its files left — it reddened `lint`, which is how we found it.
# Restore the arm if a .py or python-shebang hook is ever added back.
#
# check-golangci-formatters-report rides here rather than anywhere else because
# `lint` is what the required context runs, and the property it holds is a
# property of the very next line: that `golangci-lint run` reddens on gofumpt
# and gci. Ahead of the lint, so a tree whose formatter enforcement has gone
# quiet says so before spending eighty seconds.
lint: ensure-golangci lint-hooks (lint-hooks ".github/scripts") lint-python lint-just check-golangci-formatters-report check-golangci-build-tags
    {{ lint_lock }} {{ golangci }} run

# Guard: the golangci-lint analysis cache must be non-empty after lint.
# Fails if GOLANGCI_LINT_CACHE in the justfile diverges from the path: in ci.yml (gqlc-b63).
lint-cache-check:
    @test -d .bin/golangci-cache && test -n "$(ls -A .bin/golangci-cache 2>/dev/null)" \
        || { echo "error: GOLANGCI_LINT_CACHE (.bin/golangci-cache) is empty or missing — justfile and ci.yml paths diverged"; exit 1; }

# lints only lines changed since the given rev — the fast pre-push variant
lint-new rev="origin/master": ensure-golangci
    {{ lint_lock }} {{ golangci }} run --new-from-rev {{ rev }}

# The function-complexity gate (gocyclo + gocognit) on its own, over the given
# packages — `just complexity ./internal/codegen/...`, or the whole tree when
# asked for nothing.
#
# CI does NOT call this: both linters are in `.golangci.yml`'s enable list, so
# the merge-blocking `lint` job already runs them and this recipe would be a
# second copy of that gate to keep in step. What calls it is .githooks/pre-commit,
# which needs the complexity findings WITHOUT the other twenty-odd linters —
# measured 2026-09-10, the full `golangci-lint run` is ~76 s warm over this tree
# and the hook's budget is sub-second, while these two alone over the packages a
# commit touches are a fraction of that.
#
# The thresholds are NOT written here. `--enable-only` selects which linters run
# and changes nothing else: `linters.settings` and `linters.exclusions` are both
# still read, so the numbers and the _test.go exemption still come from
# `.golangci.yml` and the hook cannot drift from CI. Both halves were measured
# on 2026-09-10 against a probe package — gocyclo fired at 28 (the configured
# 25, not gocyclo's own default of 30), and a copy of the same over-complex
# functions in a `_test.go` went unreported.
#
# `--enable-only`, NOT `--default none --enable gocyclo,gocognit`. That pairing
# reads like a restriction and is not one: `--enable` ADDS to the config's
# enable list, so all 28 configured linters still run. It looks correct on a
# clean tree — the extra linters find nothing, so the report contains only
# complexity rows — and the tell only appears once some other linter has
# something to say. Measured the same day: the probe drew two `revive` findings
# through that spelling, which is also ~76 s of hook nobody asked for.
#
# Restricting to the changed packages is exact rather than a sampling
# compromise: gocyclo and gocognit score one function from its own AST, so no
# edit can change the score of a function in a package it did not touch.
#
# The two --max flags UNCAP the report. golangci-lint defaults to 50 issues per
# linter and 3 sharing one message, and applies both silently — measured
# 2026-09-10, a capped run dropped build.go's gocyclo row from output that
# otherwise looked complete. It cannot turn a red run green, one surviving issue
# being enough to exit non-zero, so this is about what the person who has to fix
# it gets to see: a capped list sends them round the loop once per hidden
# function. The third suppressor in that family is `issues.uniq-by-line`, whose
# default keeps one issue per source line; it is turned off in `.golangci.yml`
# and not here, because these two linters both report at a function's
# DECLARATION line and CI's report is degraded by it identically (bd gqlc-5sfv).
#
# THE PATHS ARE GROUPED BY OWNING MODULE and the linter runs once per module,
# from inside that module. golangci-lint is module-scoped: a root-module run
# handed a path belonging to test/data/codegen exits 7 with "main module does
# not contain package" having graded nothing, and .githooks/pre-commit then
# reported that as a complexity finding — so every commit touching the live
# battery or a checked-in golden was refused for a reason that was not true
# (bd gqlc-f0x1, gqlc-i8j9). Running from the module root is what
# test-codegen-fence already does; the root `.golangci.yml` is found by
# golangci-lint's upward walk, so the thresholds and the _test.go exemption
# are still the ones CI enforces.
#
# The module set is DISCOVERED through internal/tools/modscope, the same
# derivation test-codegen-fence and `just vuln` read (bd gqlc-oxne). Naming
# `test/data/codegen` here would make this the third place a second nested
# module has to be remembered, and the two that already exist were generalised
# precisely because it was not.
#
# Reading modscope is what makes sweep-discovery-probes a dependency, and it is
# not optional: a probe module a killed run left under test/data stops the walk
# on goDirs' empty-walk refusal, so the gate would die over litter. The
# dependency is held by TestEveryRecipeRunningModscopeSweepsProbesFirst (bd
# gqlc-c7o7), which caught this recipe the first time it ran without it. It
# costs 0.1s measured, which is the reason it is affordable inside a hook.
#
# With no paths the default is every module's `./...`, not the root's. A
# root-only `./...` is what the comment above used to call "the whole tree"
# while the nested module went unmeasured, and a coverage claim wider than the
# run behind it is the failure class this gate exists to prevent.
#
# EXIT CODE IS LOAD-BEARING, and the hook's message depends on it: 1 is
# golangci-lint's --issues-exit-code and nothing overrides it, so 1 and only 1
# means "code was graded and found wanting". A structural code from any module
# therefore wins over a 1 from another — the commit author must not be told a
# function is over the threshold when a module failed to load.
#
# That contract is not golangci-lint's alone to keep, and this recipe holds the
# other half of it: a package-LOADING failure comes back from golangci-lint as a
# `typecheck` ISSUE and therefore as 1, so the per-module `go list` pre-flight
# below is what keeps 1 meaning graded (bd gqlc-9nj3). The rows in
# .githooks/complexity-exit-code.rows hold it, `just test-complexity-exit-code` runs
# them.
complexity *paths: sweep-discovery-probes ensure-golangci
    #!/usr/bin/env bash
    set -euo pipefail

    # `|| exit 1` rather than trusting errexit: it is suppressed inside a
    # command substitution (measured on bash 5.3), so a dead modscope would
    # otherwise read as a tree with no nested module and silently restore the
    # root-only behaviour this recipe exists to remove.
    modules_raw="$(go run ./internal/tools/modscope modules)" || exit 1
    nested=()
    while IFS= read -r module; do
        case "${module}" in ""|".") continue ;; esac
        nested+=("${module}")
    done <<<"${modules_raw}"

    # gofmt is the PARSE half of the pre-flight below. Resolved out of GOROOT
    # rather than off PATH so it is the one belonging to the toolchain that just
    # ran `go list`, with PATH as the fallback for a distribution that ships the
    # binary only as a symlink. Every Go distribution carries it, so this arm is
    # about saying which failure it is rather than about a case anyone expects:
    # an unresolvable gofmt exits 127 through the pre-flight, which this recipe
    # would otherwise report as "this file does not parse" — the same wrong-cause
    # message the pre-flight exists to stop printing (bd gqlc-3rgo).
    gofmt_bin="$(go env GOROOT)/bin/gofmt"
    if [ ! -x "${gofmt_bin}" ]; then
        gofmt_bin="$(command -v gofmt || true)"
    fi
    if [ -z "${gofmt_bin}" ]; then
        echo "complexity: no gofmt in \$(go env GOROOT)/bin or on PATH, so the PARSE" >&2
        echo "            pre-flight cannot run. This is a missing tool, NOT a finding" >&2
        echo "            about your code and NOT a complexity result." >&2
        exit 7
    fi

    # The Go files of a package, absolute, one per line. Test files are in the
    # list because golangci-lint lints them too (run.tests defaults to true), and
    # measured 2026-09-11 a `_test.go` that does not parse arrives from this
    # recipe as 1 exactly as a non-test file does. CgoFiles are separate from
    # GoFiles in `go list`'s output and would otherwise go unparsed.
    list_fmt='{{{{$d := .Dir}}{{{{range .GoFiles}}{{{{$d}}/{{{{.}}{{{{"\n"}}{{{{end}}{{{{range .CgoFiles}}{{{{$d}}/{{{{.}}{{{{"\n"}}{{{{end}}{{{{range .TestGoFiles}}{{{{$d}}/{{{{.}}{{{{"\n"}}{{{{end}}{{{{range .XTestGoFiles}}{{{{$d}}/{{{{.}}{{{{"\n"}}{{{{end}}'

    set -- {{ paths }}
    if [ "$#" -eq 0 ]; then
        set -- ./...
        for module in ${nested[@]+"${nested[@]}"}; do
            set -- "$@" "./${module}/..."
        done
    fi

    declare -A grouped=()
    for given in "$@"; do
        rel="${given#./}"
        recurse=""
        case "${rel}" in
            ...)   recurse="/..."; rel="" ;;
            */...) recurse="/..."; rel="${rel%/...}" ;;
        esac

        # Longest match wins, so a module nested inside another module would
        # be grouped under the one that actually owns the path.
        owner=""
        for module in ${nested[@]+"${nested[@]}"}; do
            case "${rel}" in
                "${module}"|"${module}"/*)
                    [ "${#module}" -gt "${#owner}" ] && owner="${module}" ;;
            esac
        done

        sub="${rel}"
        if [ -n "${owner}" ]; then
            sub="${rel#"${owner}"}"
            sub="${sub#/}"
        fi
        if [ -z "${sub}" ]; then
            pattern=".${recurse:+/...}"
        else
            pattern="./${sub}${recurse}"
        fi
        grouped["${owner:-.}"]+=" ${pattern}"
    done

    # Iterated over the ordered module list rather than over the associative
    # array's keys, whose order bash does not define: a gate whose report
    # changes order between runs is one nobody can diff.
    rc=0
    for module in . ${nested[@]+"${nested[@]}"}; do
        [ -n "${grouped[${module}]+set}" ] || continue
        read -r -a patterns <<<"${grouped[${module}]}"
        echo "complexity: ${module} (${patterns[*]})"
        module_rc=0
        # PRE-FLIGHT THE LOAD, because golangci-lint cannot report one as a
        # structural failure. A directory whose files disagree about their
        # package clause is a package-LOADING failure, but golangci-lint
        # surfaces it through `typecheck` — its always-on pseudo-linter, which
        # --enable-only does not suppress — as an ISSUE, so it takes
        # --issues-exit-code and arrives as 1. Measured 2026-09-11 on
        # test/data/codegen: `package codegen` staged beside `package fixtures`
        # exits 1 with "found packages fixtures ... and codegen ... (typecheck)"
        # and "1 issues: * typecheck: 1", having graded no function. The hook
        # reads 1 as "graded and found something" and printed the complexity
        # sentence over it (bd gqlc-9nj3) — the same lie gqlc-f0x1 and gqlc-i8j9
        # were about, reached past the exit-code test that closed them.
        #
        # `go list` discriminates exactly where this gate needs it to, which is
        # why it is the pre-flight rather than a parse of the report. Measured
        # the same day, all four against this tree: a package-clause clash exits
        # 1 and names both clauses; a directory holding no Go file exits 1; a
        # TYPE error (`var _ int = "not an int"`, a reference to an undefined
        # symbol) exits 0; and so does a file whose body does not PARSE, go/build
        # reading only as far as the imports. The type error is the one that must
        # not turn red here — gocyclo and gocognit are AST-only and grade that
        # package fine, so refusing mid-edit source would be worse than the bug
        # this closes.
        #
        # AND THEN PARSE, because `go list`'s blindness to a body is the gap it
        # left behind (bd gqlc-3rgo). go/build reads only as far as the imports,
        # so a file whose body is cut off mid-signature loads at rc=0 here and is
        # reported by golangci-lint through the same always-on `typecheck` — as
        # an ISSUE, so as 1, over a package in which nothing was graded. That is
        # the common member of the class rather than an exotic one: a half-typed
        # function is the normal state of a file at the moment someone reaches
        # for `git commit`, where a clause clash needs a deliberately broken
        # tree.
        #
        # gofmt is the parser, handed the EXACT file list `go list` just named
        # rather than a directory. gofmt walks a directory recursively — measured
        # 2026-09-11, `gofmt -l .` from the root descends into `_`-prefixed
        # directories that `go list ./...` skips — so a directory argument would
        # refuse a package over a parse error in some unrelated package below it,
        # which is the "refuses too much" lie in the other direction that the
        # graded rows exist to catch. `gofmt -l` exits 0 for a file that merely
        # needs formatting and 0 for one that does not type-check, and non-zero
        # only when it could not parse (all three measured), so its exit status
        # is the question this pre-flight is asking and nothing else.
        #
        # NOT the sentence the hook used to carry, which said an unparseable file
        # was "caught earlier still, by the repo-wide `just fmt-check`". It is
        # not: golangci-lint's formatters decline to format a file they cannot
        # parse, emit `level=warning msg="(gofumpt) formatting file ...: expected
        # ')', found '{'"` and exit 0 (measured 2026-09-11), and the hook calls
        # fmt-check with >/dev/null 2>&1 so the warning is not even seen.
        #
        # IT RUNS FROM THE MODULE ROOT, after the grouping above, and that is
        # load-bearing rather than incidental. `go list ./test/data/codegen`
        # from the root module fails with "main module does not contain package"
        # (measured) — so this same pre-flight spelled per staged DIRECTORY,
        # which is the obvious place for it, would call every commit touching
        # the live battery a load failure and re-open gqlc-f0x1 from the other
        # side.
        #
        # Cost, since this is on the path of every commit: the `-f` template
        # costs `go list` nothing measurable (9-15 ms per module warm either
        # way, 81-102 ms for the root module's whole-tree `./...`), and gofmt
        # adds 14-28 ms over one package's 16 files and 161-187 ms over the root
        # module's 267 — against ~300 ms for the cheapest real invocation of this
        # recipe. A commit stages one or two packages, so the added cost there is
        # the 14-28 ms figure, not the whole-tree one (measured 2026-09-11).
        #
        # 7 is not invented here: it is the code golangci-lint itself exits with
        # when it cannot load the packages it was handed, and it is not 1, which
        # is the whole of what the hook's reading needs. The parse arm shares it
        # for the same reason: the hook prints one message for every code that is
        # not 1, and both arms mean the same thing to it.
        module_go_files=()
        if ! module_go_list="$( cd "${module}" && go list -f "${list_fmt}" "${patterns[@]}" )"; then
            echo "complexity: ${module} could not be LOADED, so no function in it was graded" >&2
            module_rc=7
        else
            # `|| continue` and not `&& append`: a herestring over an EMPTY
            # variable still yields one empty line, so the `&&` form would leave
            # the loop — and with it the whole `if`, which is the last command in
            # this branch — at status 1, and errexit would kill the recipe with
            # exactly the code the hook reads as "graded, and found something".
            while IFS= read -r module_go_file; do
                [ -n "${module_go_file}" ] || continue
                module_go_files+=("${module_go_file}")
            done <<<"${module_go_list}"
        fi
        if [ "${module_rc}" -eq 0 ] && [ "${#module_go_files[@]}" -gt 0 ] \
            && ! module_parse_err="$("${gofmt_bin}" -l "${module_go_files[@]}" 2>&1 >/dev/null)"; then
            echo "complexity: ${module} holds a file that does not PARSE, so no function in it was graded" >&2
            printf '%s\n' "${module_parse_err}" >&2
            module_rc=7
        fi
        if [ "${module_rc}" -eq 0 ]; then
            ( cd "${module}" && {{ lint_lock }} {{ golangci }} run \
                --enable-only gocyclo,gocognit \
                --max-issues-per-linter 0 --max-same-issues 0 "${patterns[@]}" ) || module_rc=$?
        fi
        if [ "${module_rc}" -ne 0 ] && { [ "${rc}" -eq 0 ] || [ "${module_rc}" -ne 1 ]; }; then
            rc="${module_rc}"
        fi
    done
    exit "${rc}"

# rewrites formatting in place (gofumpt + gci, both bundled in golangci-lint)
fmt: ensure-golangci
    {{ golangci }} fmt

# formatting check without writing; fails with a diff when unformatted
fmt-check: ensure-golangci
    {{ golangci }} fmt --diff

# THE PRE-PR GATE SET: every required CI context that can run on this machine.
#
# It exists because a hand-written list of gates drifts and nothing tells you.
# The procedure everyone followed named three recipes — fmt-check, lint, test —
# while master required seven contexts, six of them reachable here across eleven
# arms. Anyone who ran the documented three and pushed then learned the rest one
# CI round trip at a time: measured on PR #1643, where all three were green and
# codegen-fence failed on three ireturn findings the root lint cannot reach by
# construction (bd gqlc-jq50, gqlc-s9bx). Naming ONE recipe moves that drift
# here, next to the recipes it is about and in front of everyone who edits them.
#
# That seven is the count as PR #1643 measured it, and the count has since
# moved: live-smoke-age is master's eighth required context as of 2026-09-10
# (bd gqlc-ezwae put the arm on pull requests, bd gqlc-f98s made it required),
# and it is reachable here no more than live-smoke's container half is — the
# NOT-covered summary below names it for that reason.
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
#   live-smoke-age
#                `just test-codegen-live-age` — the same trade on the AGE
#                side, and PR-blocking since bd gqlc-ezwae, so it is a
#                required context this recipe does not cover rather than a
#                nightly whose red arrives later. It carries -count=1, so it
#                is the slower of the two to re-run.
#   tidy (part)  three of that job's ten steps read state that does not exist
#                before the PR: check-pr-closes.py wants the body,
#                check-pr-authors.sh the commit list, check-cron-freshness.sh
#                the Actions API. Unrunnable here by construction, not by
#                choice. The other seven DO run — tidy-check and
#                check-doc-ordinals.py and
#                check-open-pr-ordinals.py --self-test and
#                next-doc-ordinal.py --self-test and
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
# Every arm here runs on a clean master. The `just vuln` arm used to be red on
# this project's own development machine, whose default Go is a distro build
# (`go1.27.0-X:nodwarf5`) that govulncheck cannot place a stdlib version on, so
# it scanned the largest attack surface in the binary and reported nothing (bd
# gqlc-u91z). CI never hit that, because .github/actions/setup-go exports
# GOTOOLCHAIN from go.mod. `just vuln` now pins the same toolchain from the same
# derivation, so the arm runs as CI runs it in the way that was actually meant
# (bd gqlc-irvs). The refusal it used to trip is untouched and still fires if
# the pinned toolchain is one govulncheck cannot place either.
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
    run lint           just test-complexity-exit-code
    run test           just test
    # Deliberately NOT the bare context "live-smoke". These tests run in CI only
    # inside that job, but this arm is its Docker-free slice and cannot boot a
    # container, so collecting "live-smoke" here would make the summary below
    # claim a required context this recipe does not cover. The suffixed name
    # sorts and prints beside it and cannot be mistaken for it (bd gqlc-lw1j8).
    run 'live-smoke[docker-free]' just test-codegen
    run codegen-fence  just test-codegen-fence
    run actionlint     just actionlint
    run tidy           just tidy-check
    run tidy           just bd-export-monotonic-local
    run tidy           python3 .github/scripts/check-label-lengths.py .beads/issues.jsonl
    # The enrolled series are listed HERE, in ci.yml and in ordinal-recheck.yml
    # rather than defaulted inside the checker: "is this an ordinal series" is a
    # fact about the directory, not one the script can infer, and a default
    # would let a new series be added with nobody deciding it should be covered.
    # All three lists must move together; the checker names them when it refuses.
    run tidy           python3 .github/scripts/check-doc-ordinals.py docs/adr
    # The moved-base half's decision core (bd gqlc-4plwf). Its own rows, not a
    # scan of the tree — the network path cannot run here, and it is the LOGIC
    # that would otherwise be exercised by nothing until a real collision. This
    # repository has no Python test runner, so this is where those rows live.
    #
    # ci.yml's tidy job runs the same command, unlike `fmt-check` above: an arm
    # that reds only here lets the break merge, and ordinal-recheck.yml's own
    # copy fires on master PUSH, which is after the merge it should have
    # stopped.
    run tidy           python3 .github/scripts/check-open-pr-ordinals.py --self-test
    # The allocator half's rows (bd gqlc-c30cl). Its decision core minus the
    # fetch: the tool refuses without a network by design, so the rows drive
    # the merge-and-offer logic with stubbed remotes and never fetch. Same
    # placement as the moved-base rows above: an arm that reds only here lets
    # the break merge.
    run tidy           python3 .github/scripts/next-doc-ordinal.py --self-test
    # The rows for .githooks/bd-prime-guarded (bd gqlc-kip5). ci.yml's tidy job
    # runs the same command against a PINNED bd; here it runs against whatever bd
    # this machine deploys, which is the binary the guard actually protects the
    # session hooks from. Those are different questions and both are wanted: a
    # local red says this host's bd has moved, a CI red says the guard has.
    run tidy           just test-bd-prime-guard
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
    echo "       live-smoke        its CONTAINER half only. The Docker-free half"
    echo "                         of that job ran above as live-smoke[docker-free];"
    echo "                         what is left needs Docker: just test-codegen-live-neo4j"
    echo "       live-smoke-age    entirely. It is PR-blocking too (bd gqlc-ezwae) and"
    echo "                         has no Docker-free half here; all of it needs"
    echo "                         Docker: just test-codegen-live-age"
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
test: check-hooks check-worktree-upstream check-shared-config check-beads-export check-bd-gh-sync-selection check-bd-gh-sync-ledger check-bd-gh-sync-pull-tiebreak check-pr-ready check-push-keepalive
    go build ./...
    go test ./...

# fails when go.mod/go.sum are not tidy
tidy-check:
    go mod tidy -diff

# the next free ordinal in a hand-numbered series, across origin as well as here.
#
# Run this BEFORE naming a new ADR or decision file. Reading master and adding
# one is the thing that creates duplicate ordinals: a number free on master can
# already be held by a branch in flight, and the two files differ in name, so
# nothing ever conflicts over it. check-doc-ordinals.py in `just gates` is the
# other half, and it can only catch this once both documents are in one tree —
# by then the number is spent and the fix is a rename plus every citation.
#
# Measured live 2026-09-01 (bd gqlc-dawo1): master's highest ADR was 0037, so
# the naive read offered 0038, which an open PR already held. Both enrolled
# series were carrying a trap that day.
#
# It fetches, and refuses if it cannot: an answer from stale refs is the defect
# wearing the fix's name. Series default to the two the gate enrols; pass a
# directory to ask about another.
adr-next *dirs="docs/adr":
    python3 .github/scripts/next-doc-ordinal.py {{ dirs }}

# fails when .beads/issues.jsonl regresses vs base (dropped or reopened issues).
# Motivated by bd gqlc-v2p: PR #422's blanket `git add -A` shipped a stale bd
# passive export that would have reopened two closed beads and dropped one.
# Deliberately NOT wired into `just test`: gate is PR-scoped, test runs on master
# pushes too (gqlc-5fm: a fatal recipe off master would break every CI run).
# Arg is passed through unchanged, so CI can hand it the exact base SHA and
# avoid a merge-base walk (which needs deep history).
bd-export-monotonic base:
    go run ./internal/tools/bdguard -base {{ base }}

# dev-local convenience: compare against the merge-base with origin/master.
#
# It requires full history, and it now CHECKS rather than assumes. Unquoted,
# `$(git merge-base ...)` collapses to zero words when the walk fails, and just
# then complains about its own arity:
#
#     error: recipe `bd-export-monotonic` got 0 positional arguments but takes 1
#
# which names neither history nor `origin/master` nor shallowness, and reads as
# a malformed recipe rather than as a fact about the repository. It reaches the
# operator under the arm name `tidy`, which says nothing either. Measured
# 2026-08-29 (bd gqlc-yzk1h): a shallow graft in the SHARED git dir put every
# linked worktree's `just gates` on this message at once.
#
# The causes are separated because their remedies are different commands, and an
# operator who hits this has no reason to suspect their git dir at all. A bare
# `${base:?}` would refuse without saying which of the three happened.
#
# The check lives here rather than as an up-front screen in `gates` because this
# recipe has callers `gates` does not see — it is run by hand, and a screen one
# level up would leave the direct run with the arity message. Measured the same
# day: of the eleven `gates` arms this is the only one that walks history, so a
# screen there would guard one arm and own a second copy of this diagnosis.
bd-export-monotonic-local:
    #!/usr/bin/env bash
    set -uo pipefail
    if ! git rev-parse --verify --quiet origin/master >/dev/null; then
        echo "error: no origin/master in this repository, so there is no base to compare against." >&2
        echo "       This recipe diffs the bd export against the merge-base with origin/master." >&2
        echo "       Repair:  git fetch origin master" >&2
        exit 1
    fi
    base=$(git merge-base HEAD origin/master 2>/dev/null) || base=""
    if [ -n "${base}" ]; then
        exec just bd-export-monotonic "${base}"
    fi
    echo "error: HEAD and origin/master have no common ancestor this repository can see," >&2
    echo "       so there is no merge-base to hand to bdguard. Nothing is wrong with the" >&2
    echo "       recipe or with your branch's contents (bd gqlc-yzk1h)." >&2
    if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = "true" ]; then
        echo "       CAUSE: this repository is SHALLOW, so the ancestor is simply not present." >&2
        echo "       Note that .git is shared by every worktree here, so this affects them all." >&2
        echo "       Repair:  git fetch --unshallow origin" >&2
    else
        echo "       The repository is not shallow, so the histories are genuinely unrelated —" >&2
        echo "       an orphan branch, or an origin/master rewritten out from under this HEAD." >&2
        echo "       Inspect:  git log --oneline -1 HEAD origin/master" >&2
    fi
    exit 1

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
    go run ./internal/tools/ghorphan -close {{ args }}

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
    probe="$(mktemp -d test/data/{{ fence_probe }}.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/{{ fence_probe }}\n\ngo 1.26.5\n' >"${probe}/go.mod"
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
        (cd "${m}" && {{ lint_lock }} {{ golangci }} run)
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
    probe="$(mktemp -d test/data/{{ xtest_probe }}.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/{{ xtest_probe }}\n\ngo 1.26.5\n' >"${probe}/go.mod"
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

# the codegen module's Docker-free tests. `just test` is the ROOT module and
# cannot reach a separate module at all, and every other recipe that reaches
# this one passes -tags codegen_live and boots containers — so until bd
# gqlc-lw1j8 the only way to run a Docker-free guard here was to have Docker.
#
# TestTxMethodSet is why that mattered: under bd gqlc-eunj4 it was one of only
# two witnesses of *Tx's promoted surface, and the one that caught the missing
# DropGraph row when the other did not. A witness costing a container boot is
# one a local loop skips.
#
# Selection is BY BUILD TAG and by nothing else. There is no -run allowlist
# here on purpose: every live file carries `//go:build codegen_live`, so an
# untagged run is exactly the Docker-free set, and it stays exact as the module
# grows without anyone maintaining a list (an allowlist is what let a live test
# run in no recipe at all — bd gqlc-df3d). A new test needing Docker but
# missing the tag fails HERE, loudly, which is the direction that costs least.
test-codegen:
    cd test/data/codegen && go test ./...

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
# reason given at that recipe instead, which bd gqlc-ezwae restated when the arm
# joined pull requests: a cached PASS is a weaker witness than a real run, and
# the AGE arm was measured able to afford a real one per PR. The asymmetry errs
# safe and is left standing.
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
# TestAGERefusesAUint64ParameterAboveMaxInt64 is the third of that kind, and the
# AGE in its name is about the emission it reads, not about a container: the
# refusal it executes is generated code that runs between cypherStmt and
# q.db.Query, so it drives a handle over a nil DBTX and starts nothing. It is in
# the PR-blocking half under the rule above, and hardest of the three: the
# mutation battery of bd gqlc-tzjqu deleted that guard from the emitted helper,
# regenerated the goldens, and every gate stayed green -- so the edit this row
# has to catch is the ordinary one, and catching it nightly is catching it after
# the merge (bd gqlc-l65y9).
#
# The alternation is a NAME LIST, not a pattern, for the reason the AGE recipe
# below spells out: -run is unanchored, so a name here silently claims every
# test that extends it. No name in EITHER recipe is a prefix of another test in
# the module -- re-derived 2026-09-05 over all 1028 test functions, not carried
# forward from the previous grep.
#
# TestNeo4jRefusesANestedListStoredProperty and
# TestNeo4jRefusesAMapValuedStoredProperty each start a container of their own,
# so the PARALLEL set here is FOUR rather than the three the paragraph used to
# describe. They boot concurrently and TestLiveSmoke's header measures three at
# ~4GB peak; a fourth of the same image is ~5.3GB by that arithmetic, inside a
# standard runner's 7GB. The docker.service that was disabled host-wide when
# the fourth landed (bd gqlc-p9g2i) is available again, and this half was run
# whole on 2026-09-10: 74 RUN / 74 PASS / 0 FAIL, 28.3s, exit 0.
#
# TestNeo4jRefusesAUint64ParameterAboveMaxInt64 and
# TestNeo4jNeverHandsBackANullValuedProperty each boot a container too but do
# NOT call t.Parallel, so go test runs them one at a time outside the set above
# and the PEAK is unchanged at four (bd gqlc-lr0v6, bd gqlc-wc5j). The price is
# serial wall time, measured alone on 2026-09-10 at 20.3s and 18.9s, most of it
# the boot -- the uint64 probe serves BOTH driver majors off its one container
# rather than taking a second. TestAGERefusesAUint64ParameterAboveMaxInt64 adds
# no container at all: it binds over a nil DBTX and asserts the value stopped
# before the send.
#
# They earn the PR-blocking half rather than the nightly one for one reason.
# Each is the tripwire under a claim that exists SOLELY because of what this
# server does -- nested lists for ADR 0035, map-valued properties for the record
# carriers' storage ruling, the driver's own overflow refusal for the uint64
# widen bd gqlc-tzjqu removed, and the absence of any null-valued property for
# the presence-only gate at writeShapelessFieldDecode -- and a pull request is
# where that had better still be true.
test-codegen-live-neo4j:
    cd test/data/codegen && go test -v -tags codegen_live -run 'TestLiveSmoke|TestEveryBatteryIsTheDeclaredSize|TestEveryBatteryIsNamedInScenarioTables|TestTxMethodSet|TestNeo4jRefusesANestedListStoredProperty|TestNeo4jRefusesAMapValuedStoredProperty|TestNeo4jRefusesAUint64ParameterAboveMaxInt64|TestNeo4jNeverHandsBackANullValuedProperty|TestAGERefusesAUint64ParameterAboveMaxInt64|TestEveryAgtypeCaptureIsWitnessedOrDeclaredSynthetic' -skip 'TestLiveSmoke/apache-age' ./...

# the Apache AGE half of the live battery: the smoke battery's AGE arm, the
# session-init contract, and the AGE-only probes. The -run alternation below is
# the source of truth for which probes those are — this sentence describes the
# shape, not the roster. Each runs on its own apache/age container. It runs on
# pull requests as well as on the nightly and on dispatch since bd gqlc-ezwae:
# the arm was measured at 55-58s against the neo4j arm's 73-116s on the SAME
# runs, in parallel, so charging a pull request for these containers costs no
# critical-path minute (bd gqlc-zase).
#
# -count=1 stays, and its reason is no longer "this is the AGE arm's only gate".
# A cached PASS is a weaker witness than a real run, and the measurement above
# is what says this arm can afford a real one on every pull request; dropping it
# would buy back seconds already shown not to be on the critical path, at the
# price of the nightly's freshness. Weighed and declined in gqlc-zase, rejected
# alternative 4.
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
    cd test/data/codegen && go test -v -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation|TestAGERefusesTheFunctionsItDoesNotDefine|TestAGERefusesTheSpatialConstructor|TestAGERefusesTheNamespaceItHasNoSchemaFor|TestAGEOffsetSidecar|TestAGEZonedTime|TestAGEStoresANestedListProperty|TestAGEStoresARecordProperty|TestAGEMatchesATemporalListParameter|TestAGENeverHandsBackANullValuedProperty|TestAGEKeepsAnExplicitNullAtARecordFieldButNotAtAProperty|TestAGEAgtypeCaptureMatchesTheServer|TestAGEAgtypeNullReachesPgxAsSQLNULL' -skip 'TestLiveSmoke/neo4j' ./...

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

    # Scan under the toolchain go.mod names, which is what CI already does via
    # .github/actions/setup-go. Without this the recipe was red on a clean
    # master on any box whose default Go is a distribution build:
    # govulncheck cannot match such a version to a released one, so it places no
    # version on the standard library and refuse_unplaced_stdlib below fires (bd
    # gqlc-irvs). That refusal is correct — it is the asymmetry that was the
    # defect, a gate red by default being a gate people learn to ignore.
    #
    # The derivation is the same script setup-go reads, not a second copy; see
    # the note in it for why that matters more than the four lines it saves.
    GOTOOLCHAIN="go$(./.github/scripts/go-toolchain-version.sh go.mod)"
    export GOTOOLCHAIN

    # Assert the pin TOOK, rather than trusting the export. The clause above is
    # the only thing standing between this gate and a silent stdlib-blind scan,
    # and on a box whose default Go is already a released one an export that
    # quietly did nothing is indistinguishable from one that worked — so
    # refuse_unplaced_stdlib would not catch this going wrong there. Same reason
    # setup-go asserts its own provisioning rather than reporting it.
    ran_under="$(go env GOVERSION)"
    if [ "${ran_under}" != "${GOTOOLCHAIN}" ]; then
        echo "error: this recipe pinned GOTOOLCHAIN=${GOTOOLCHAIN} from go.mod, but the go" >&2
        echo "       command reports GOVERSION=${ran_under}. The scan below would run under a" >&2
        echo "       toolchain nobody chose, and the advisory set is toolchain-dependent (bd" >&2
        echo "       gqlc-irvs)." >&2
        exit 1
    fi
    echo "vuln: scanning under ${ran_under}, the toolchain go.mod names"

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
    probe="$(mktemp -d test/data/{{ vuln_probe }}.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/{{ vuln_probe }}\n\ngo 1.26.5\n' >"${probe}/go.mod"
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
            # WHICH cause it was is derived here rather than left to the reader.
            # Measured 2026-09-02 (bd gqlc-y2dgv): a called vulnerability, a
            # module that will not load, and a `go run` that cannot fetch
            # govulncheck all exit 1, so the status separates none of them — and
            # the third is not a cause the old text named at all, so a tool
            # download that failed was reported as this tree calling something.
            #
            # The summary line is the discriminator because govulncheck prints
            # it only on a scan that ran to completion. Anchored, and matched on
            # the summary rather than on a `Vulnerability #` header: the headers
            # are printed by the package-results section before the scan has
            # finished, so keying on one would call a truncated run complete.
            if grep -qE '^Your code is affected by [0-9]+ vulnerabilit' <<<"${out}"; then
                echo "error: the scan of ${dir} exited ${rc} having run to completion, so this tree" >&2
                echo "       CALLS a known vulnerability. The output above names it. That does not" >&2
                echo "       belong in the register below, which is for advisories nothing calls" >&2
                echo "       (bd gqlc-k22l)." >&2
            else
                echo "error: the scan of ${dir} exited ${rc} without reporting: the output above" >&2
                echo "       carries no 'Your code is affected by' summary, so govulncheck did not" >&2
                echo "       finish a scan and this tree has NOT been shown to call anything." >&2
                echo "       Two causes reach here and the exit status separates neither — the" >&2
                echo "       module could not be loaded, or 'go run' could not fetch or build" >&2
                echo "       govulncheck itself. The output above is the only evidence which." >&2
                echo "       Do not re-run to decide: a red that clears on a re-run has been a" >&2
                echo "       real called vulnerability arriving in the advisory database mid-scan," >&2
                echo "       not a flake (bd gqlc-y2dgv, bd gqlc-4twyd)." >&2
            fi
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
    #
    # That argument is about an UNCALLED advisory and does not reach a called
    # one. x/crypto was pinned ahead of testcontainers-go in exactly the way
    # this paragraph declines, on 2026-09-02, because GO-2026-6355 and
    # GO-2026-6354 arrived with a call trace — startAGEContainer to
    # testcontainers.GenericContainer to ssh.NewClientConn — and a called
    # advisory is not registrable here at any size of diff (bd gqlc-4twyd).
    accepted="$(sed -e 's/#.*//' -e 's/[[:space:]]//g' -e '/^$/d' <<'ACCEPTED' | sort -u
    # go.opentelemetry.io/otel v1.41.0 in test/data/codegen: baggage parsing no
    # longer caps raw header length. Imported, not called. Fixed in v1.42.0, and
    # v1.45.0 is out, but it is testcontainers-go v0.43.0's indirect.
    GO-2026-5158
    # golang.org/x/crypto/openpgp: unmaintained and unsafe by design, no fix
    # available and none coming. Required, not imported. This one is permanent
    # unless x/crypto drops the package, and bumping x/crypto cannot clear it.
    GO-2026-5932
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
    # Run under the toolchain go.mod names, the same pin `just vuln` carries.
    # just runs each dependency in its own shell before the recipe body, so the
    # export there cannot reach this recipe, while in CI the whole job env is
    # pinned via .github/actions/setup-go. A `//go:build go1.N` constraint moves
    # what `go list` matches, so under two toolchains the two sides derive two
    # different blind sets — and the ratchet below fails in both directions,
    # tripping the gate for a reason its message does not describe (bd
    # gqlc-7qrk1). sweep-discovery-probes needs no pin: it invokes no `go`.
    GOTOOLCHAIN="go$(./.github/scripts/go-toolchain-version.sh go.mod)"
    export GOTOOLCHAIN
    ran_under="$(go env GOVERSION)"
    if [ "${ran_under}" != "${GOTOOLCHAIN}" ]; then
        echo "error: this recipe pinned GOTOOLCHAIN=${GOTOOLCHAIN} from go.mod, but the go" >&2
        echo "       command reports GOVERSION=${ran_under} (bd gqlc-7qrk1)." >&2
        exit 1
    fi
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
    internal/resolver
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
    sc={{ quote(shellcheck) }}
    if [ ! -x "${sc}" ]; then
        echo "error: actionlint's shellcheck is missing or not executable at ${sc}." >&2
        echo "       Refusing rather than running: actionlint would silently skip every" >&2
        echo "       run-block check and exit 0, which is indistinguishable from a pass" >&2
        echo "       (bd gqlc-68g9). Provision it with:  just ensure-shellcheck" >&2
        exit 1
    fi
    go run github.com/rhysd/actionlint/cmd/actionlint@{{ actionlint_version }} -shellcheck "${sc}"

# pinned openCypher release tag the TCK is vendored from; never "master" so the
# corpus is reproducible. Bump deliberately, then re-run fetch-tck and commit.
tck_tag := "2024.3"
tck_dir := "test/data/query/cypher/tck"

# vendors the whole openCypher tck/ subtree (features + LICENSE) at tck_tag into
# tck_dir via a shallow, sparse git checkout — no new deps, no extraction step
# (godog reads the .feature files directly). Run for initial population and
# deliberate version bumps; the result is committed.
fetch-tck:
    rm -rf {{ tck_dir }}
    mkdir -p {{ tck_dir }}
    rm -rf .tck-fetch
    git clone --depth 1 --branch {{ tck_tag }} --filter=blob:none --sparse \
        https://github.com/opencypher/openCypher.git .tck-fetch
    cd .tck-fetch && git sparse-checkout set tck
    cp -R .tck-fetch/tck/. {{ tck_dir }}/
    cp .tck-fetch/LICENSE {{ tck_dir }}/LICENSE
    rm -rf .tck-fetch
    @echo "vendored TCK {{ tck_tag }} into {{ tck_dir }}"

# builds the autogenerated code from the available, relevant ANTLR grammars
#
# No `sudo`. The three commands below carried one until 2026-09-11 and it was
# never a gate — it was a hazard: `sudo -n true` fails on the hosts this runs
# on, so a non-interactive session (CI, an agent) died on a password prompt
# nobody could answer, and the recipe read as "grammar regeneration needs root"
# when the invoking user is in the `docker` group and needs none (bd gqlc-eg4b,
# which regenerated GQL.g4 through it).
#
# `--user` is load-bearing and is the reason the sudo could not simply be
# deleted. The daemon runs a container as root whatever the client's identity,
# so the generated gen/*.go arrived root-owned into a bind mount and the next
# regeneration could not overwrite them. Passing the caller's uid:gid writes
# them as the caller.
build-grammar:
    docker build -q -t antlr-tool -f Dockerfile.grammar .
    @echo "Generating Go files from GQL.g4..."
    docker run --rm --user "$(id -u):$(id -g)" -v {{ invocation_directory() }}:/work -w /work/internal/grammar/gql antlr-tool -package gen -visitor -o gen GQL.g4
    @echo "Generating Go files from Cypher.g4..."
    docker run --rm --user "$(id -u):$(id -g)" -v {{ invocation_directory() }}:/work -w /work/internal/grammar/cypher antlr-tool -package gen -visitor -o gen Cypher.g4

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

# answers "are this PR's required checks green at its current head" (bd gqlc-xf0v).
#
# The reduction is newest-per-context, not newest-run and not any-entry:
# statusCheckRollup carries an entry per workflow RUN, so a superseded FAILURE
# never leaves the array, and judging any entry reads a green PR as red
# (measured on PR #1236: tidy FAILURE beside tidy SUCCESS at the same head).
# The grouping lives in .github/scripts/pr_ready.py, which this recipe feeds
# but does not restate — two copies of a reduction drift silently.
#
# The required set is enumerated from the PR's BASE branch protection on every
# run, never from a list in this file, so a protection change cannot make a
# new required context silently pass. pr-ready-drift below is the backstop for
# the other direction: the script's canned fixtures spelling a set the live
# config has moved away from.
#
# SKIPPED on a required context is NOT-READY, distinctly from FAILURE, because
# those have different causes and fixes: a tidy failure skips lint/test/
# codegen-fence via needs rather than failing them (measured on PR #1015,
# mergeStateStatus CLEAN with four required contexts newest-and-skipped), so a
# failure verdict would send the author after a regression that does not exist
# while a green one repeats the false green. Non-required entries such as
# nightly-alert's perpetual SKIPPED on a pull request are silent in every
# output. (That exemplar was live-smoke-age until bd gqlc-ezwae put the AGE arm
# on pull requests, where it now runs rather than skipping and is required.)
#
# Exits non-zero when not ready: a detector that exits 0 is not a gate.
pr-ready n:
    #!/usr/bin/env bash
    set -euo pipefail
    n="{{ n }}"
    if ! printf '%s' "$n" | grep -Eq '^[0-9]+$'; then
        echo "error: '$n' is not a PR number, so there is no rollup to judge." >&2
        exit 1
    fi
    for tool in gh jq python3; do
        if ! command -v "$tool" >/dev/null 2>&1; then
            echo "error: '$tool' is not installed, so the rollup cannot be read." >&2
            exit 1
        fi
    done
    scratch="$(mktemp -d)"
    trap 'rm -rf "$scratch"' EXIT
    script={{ quote(justfile_directory() + "/.github/scripts/pr_ready.py") }}
    repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
    base="$(gh pr view "$n" --repo "$repo" --json baseRefName --jq .baseRefName)"
    head="$(gh pr view "$n" --repo "$repo" --json headRefOid --jq .headRefOid)"
    gh api "repos/$repo/branches/$base/protection" \
        --jq '.required_status_checks.contexts' >"$scratch/required.json"
    gh pr view "$n" --repo "$repo" --json statusCheckRollup \
        --jq '.statusCheckRollup' >"$scratch/rollup.json"
    echo "PR #$n at $head against $base:"
    python3 "$script" "$scratch/rollup.json" "$scratch/required.json"

# fails when branch protection's required contexts move out from under the set
# pr_ready.py's canned fixtures spell (bd gqlc-xf0v).
#
# pr-ready itself cannot drift — it enumerates the required set live on every
# run — but the fixtures can, and that is measured rather than argued: on
# 2026-09-10 bd gqlc-f98s made live-smoke-age master's eighth required
# context, and --self-test over the then-unedited seven-context fixtures
# passed EVERY row. Nothing else in the tree was red, so the verdict rows had
# quietly stopped proving anything about the new member and only this recipe
# said so. It holds the fixture set to the live config precisely so that
# staleness reddens here instead of passing silently there.
#
# Deliberately NOT wired into `just test`: it reaches the network, and test
# runs offline the way gh-orphans stays out of it for the same reason. Run by
# hand when protection changes, and whenever the fixture set is edited.
pr-ready-drift:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v gh >/dev/null 2>&1; then
        echo "error: 'gh' is not installed, so the live protection config cannot be read." >&2
        exit 1
    fi
    scratch="$(mktemp -d)"
    trap 'rm -rf "$scratch"' EXIT
    script={{ quote(justfile_directory() + "/.github/scripts/pr_ready.py") }}
    repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
    gh api "repos/$repo/branches/master/protection" \
        --jq '.required_status_checks.contexts' >"$scratch/live.json"
    python3 "$script" --check-required "$scratch/live.json"

# runs pr_ready.py's canned-rollup matrix offline: the two measured
# misreadings (a superseded FAILURE the naive query false-reds, required
# SKIPPED the skipped-as-pass reading false-greens), the missing/pending
# refusals, and both falsifiers proving the fixtures discriminate. Wired into
# `test`, which is also what puts it in .githooks/pre-push.
[private]
check-pr-ready:
    python3 .github/scripts/pr_ready.py --self-test
