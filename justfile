# single source of truth for the linter toolchain version. Every lint/fmt
# recipe self-heals: it verifies the pinned version in .bin/ and reinstalls on
# mismatch, so a version bump here is a one-line change and nobody ever
# installs or upgrades the linter by hand.
golangci_version := "v2.12.2"
golangci := justfile_directory() + "/.bin/golangci-lint"
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

# Configures local git settings required after a fresh clone.
# Idempotent: safe to run multiple times.
init:
    git config core.hooksPath .githooks
    @echo "git hooks activated (core.hooksPath = .githooks)"

# fails when core.hooksPath drifts from .githooks — the only way local
# pre-commit/pre-push gates can silently die (CI cannot see local git config).
# Sub-ms; wired into `test` so developers hit it naturally.
#
# Skipped under CI, which has no local hooks by design and runs the equivalent
# gates as workflow jobs; without the skip this would fail every CI `just test`.
#
# Compares the configured value rather than testing the directory: the drift
# that actually occurred (bd gqlc-5fm) pointed at .git/hooks, which exists but
# holds only .sample files that git ignores, so an existence test passes while
# every hook is dead.
[private]
check-hooks:
    #!/usr/bin/env bash
    [ -n "${CI:-}" ] && exit 0
    got="$(git config --get core.hooksPath || true)"
    if [ "$got" != ".githooks" ]; then
        echo "error: core.hooksPath is '${got:-<unset>}', expected '.githooks' — local hooks are inactive." >&2
        echo "       Run 'just init' to fix." >&2
        exit 1
    fi

# health check for local dev environment; extend as new drift modes emerge
doctor: check-hooks
    @echo "ok"

# provisions the pinned golangci-lint into the gitignored .bin/ when missing
# or version-mismatched (~3s; official release binary — golangci-lint does not
# support builds from source). The happy path is a ~30ms version check, cheap
# enough to run before every lint/fmt invocation, in hooks included.
[private]
ensure-golangci:
    #!/usr/bin/env bash
    set -euo pipefail
    want="{{golangci_version}}"
    if [ "$({{quote(golangci)}} version --short 2>/dev/null || true)" = "${want#v}" ]; then
        exit 0
    fi
    echo "provisioning golangci-lint $want into .bin/" >&2
    curl --proto '=https' --tlsv1.2 -sSfL \
        "https://raw.githubusercontent.com/golangci/golangci-lint/$want/install.sh" \
        | sh -s -- -b {{quote(justfile_directory() + "/.bin")}} "$want"

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
    curl --proto '=https' --tlsv1.2 -sSfL --retry 5 --retry-all-errors --retry-delay 2 \
        "https://github.com/koalaman/shellcheck/releases/download/$want/shellcheck-$want.linux.$(uname -m).tar.xz" \
        | tar -xJ -C "$stage"
    install -m 0755 "$stage/shellcheck-$want/shellcheck" {{quote(shellcheck)}}

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
            # shellcheck only supports shell; SC1071 on a python hook would have
            # to be silenced globally, which turns it off for the shell files too.
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
# config.
[private]
check-golangci-build-tags:
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

# full static analysis: golangci-lint over the Go tree (.golangci.yml) and
# shellcheck over the hooks tree, as linters + formatter diffs as issues
lint: ensure-golangci lint-hooks check-golangci-build-tags
    {{golangci}} run

# Guard: the golangci-lint analysis cache must be non-empty after lint.
# Fails if GOLANGCI_LINT_CACHE in the justfile diverges from the path: in ci.yml (gqlc-b63).
lint-cache-check:
    @test -d .bin/golangci-cache && test -n "$(ls -A .bin/golangci-cache 2>/dev/null)" \
        || { echo "error: GOLANGCI_LINT_CACHE (.bin/golangci-cache) is empty or missing — justfile and ci.yml paths diverged"; exit 1; }

# lints only lines changed since the given rev — the fast pre-push variant
lint-new rev="origin/master": ensure-golangci
    {{golangci}} run --new-from-rev {{rev}}

# rewrites formatting in place (gofumpt + gci, both bundled in golangci-lint)
fmt: ensure-golangci
    {{golangci}} fmt

# formatting check without writing; fails with a diff when unformatted
fmt-check: ensure-golangci
    {{golangci}} fmt --diff

# unit tests for the master-guard PreToolUse hook (throwaway git repos, ~1s)
test-hooks:
    bash .githooks/tests/claude-pre-bash-test.sh
    bash .githooks/tests/commit-msg-test.sh
    bash .githooks/tests/bd-gh-sync-test.sh
    bash .githooks/tests/lint-hooks-test.sh

# runs the whole suite (unit, golden snapshots, godog) in one shot. Independent
# of fetch-tck: the TCK is vendored, so there is no network at test time.
# -shuffle catches inter-test coupling; go build link-checks package main,
# which has no tests and is otherwise only compile-checked by lint.
test: check-hooks test-hooks
    go build ./...
    go test -shuffle=on ./...

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
test-codegen-fence: ensure-golangci check-codegen-external-tests
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
    probe="$(mktemp -d test/data/fenceprobe.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/fenceprobe\n\ngo 1.26.5\n' >"${probe}/go.mod"
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
        (cd "${m}" && {{golangci}} run)
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
check-codegen-external-tests:
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
    # two apart.
    probe="$(mktemp -d test/data/xtestprobe.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/xtestprobe\n\ngo 1.26.5\n' >"${probe}/go.mod"
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
# runs in both halves until it is named there. Top-level tests are split by each
# recipe's -run allowlist — TestAGESessionInit reaches only the AGE half because
# the neo4j half's -run omits it — so a new top-level live test runs in no half
# until the recipes name it.
#
# -count=1 so a developer asking for a live run gets containers, not the cache.
test-codegen-live:
    cd test/data/codegen && go test -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation|TestAGERefusesTheFunctionsItDoesNotDefine' ./...

# the neo4j half: both driver arms in parallel against one neo4j:5-community
# image. This is the half PR CI blocks on, so its wall time is a PR's wall time.
#
# No -count=1, unlike the two recipes either side, and that is what keeps the
# per-PR cost near zero (.github/workflows/codegen-live.yml). It is sound here
# because every input that could change this battery's answer is inside the
# test binary: the scenario bodies, the generated packages they drive, the
# driver dependencies, and the neo4j image — pinned by digest as a constant in
# live_neo4j_test.go, not resolved at run time. A cache hit therefore means
# this exact binary already passed against this exact server, and any edit that
# could move either invalidates it. What a cache hit cannot re-check is the
# container runtime underneath, which is not a property of this repo.
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
test-codegen-live-neo4j:
    cd test/data/codegen && go test -tags codegen_live -run TestLiveSmoke -skip 'TestLiveSmoke/apache-age' ./...

# the Apache AGE half: the smoke battery's AGE arm, the session-init contract
# and the dialect fact the AGE backend's edge-union refusal rests on, each on
# its own apache/age container. Nightly and manual only — these containers are
# cost this project does not charge to a pull request. -count=1 because this is
# the AGE arm's only gate and no pull request pays for it, so the run it reports
# on has to be a real one.
test-codegen-live-age:
    cd test/data/codegen && go test -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation|TestAGERefusesTheFunctionsItDoesNotDefine' -skip 'TestLiveSmoke/neo4j' ./...

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
vuln: vuln-root-residual
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
    # TWO HOUSE RULES for the helpers below, and neither is stylistic.
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
    probe="$(mktemp -d test/data/vulnprobe.XXXXXX)"
    trap 'rm -rf "${probe}"' EXIT
    printf 'module gqlc.invalid/vulnprobe\n\ngo 1.26.5\n' >"${probe}/go.mod"
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
        local want="tagblind" fixture root_dirs root_tags
        fixture="$(go list -m -f '{{{{.Dir}}')/test/data/${want}"
        root_dirs="$(module_dirs .)" || exit 1
        root_tags="$(module_tags .)" || exit 1
        if ! grep -qxF "${fixture}" <<<"${root_dirs}"; then
            echo "error: the tag-derivation fixture ${fixture}" >&2
            echo "       is gone, so nothing in this tree exercises the filesystem walk or the" >&2
            echo "       coverage assertion below (bd gqlc-pig9). Restore it." >&2
            exit 1
        fi
        if go list -e -f '{{{{.Dir}}' ./... | grep -qxF "${fixture}"; then
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
        local fixture root_dirs root_tags src
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
        if ! go list -e "${tagflag[@]}" -f '{{{{.Dir}}' ./... \
            | grep -qxF "${fixture}"; then
            echo "error: ${fixture} is not in the set 'go list ./...'" >&2
            echo "       matched under the derived tags [${root_tags:-none}], so the scan below" >&2
            echo "       would not compile a file that builds fine on this platform. The" >&2
            echo "       derivation has excluded the very file it read (bd gqlc-e7oq)." >&2
            exit 1
        fi
    }
    selftest_platformtag

    reported=""
    headers_total=0

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
    # None of the three below is bumped, and the reason is a version rather than
    # a preference. Both fixable ones are indirect dependencies of
    # testcontainers-go, whose latest published release is v0.43.0 — exactly what
    # test/data/codegen already requires. Pinning an indirect dependency ahead of
    # the module that requires it is churn `go mod tidy` can undo, bought with no
    # reduction in exposure, since none of the three is reachable. Revisit when
    # testcontainers-go itself moves; `go list -m -u` in test/data/codegen is the
    # check.
    accepted="$(sed -e 's/#.*//' -e 's/[[:space:]]//g' -e '/^$/d' <<'ACCEPTED' | sort -u
    # go.opentelemetry.io/otel v1.41.0 in test/data/codegen: baggage parsing no
    # longer caps raw header length. Imported, not called. Fixed in v1.42.0, and
    # v1.45.0 is out, but it is testcontainers-go v0.43.0's indirect.
    GO-2026-5158
    # github.com/klauspost/compress v1.18.5 in test/data/codegen: OOB read in
    # .../s2. Required, not imported — nothing in the tree names the package.
    # Fixed in v1.18.7; again testcontainers-go v0.43.0's indirect.
    GO-2026-5841
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

# lints the GitHub Actions workflow files
actionlint:
    go run github.com/rhysd/actionlint/cmd/actionlint@{{actionlint_version}}

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
