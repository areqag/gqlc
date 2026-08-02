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

# full static analysis (.golangci.yml): linters + formatter diffs as issues
lint: ensure-golangci
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

# runs the codegen goldens' full quality fence inside the nested module:
# compile (go build), vet, module tidiness (go mod tidy -diff), and
# golangci-lint against the root config. Generated code must uphold the
# same linting + formatting standards as gqlc's own CI (owner directive,
# 2026-07-11); running golangci-lint from within the nested module
# discovers ../../../.golangci.yml via upward walk, giving parity for
# free. Used identically locally (post-generate) and in CI.
#
# go vet takes the codegen_live tag so its analysers reach the live battery;
# untagged it silently skips those files (bd gqlc-3eyw). go build does not,
# because the tagged files are all _test.go and go build never compiles those
# — the tag there would be inert, and vet already builds what it analyses.
# golangci-lint reads the tag from .golangci.yml.
#
# check-codegen-external-tests runs FIRST, before the linter. Ordering is
# load-bearing: reverting the battery to `package fixtures` also trips the
# ireturn allowlist in .golangci.yml (22 issues), so with the linter ahead of it
# the guard was never reached on the one regression it exists to catch, and the
# failure a developer saw named ireturn rather than the scan. The allowlist is
# doing that job by accident and only while those eight interface types exist;
# the guard has to be able to report on its own terms.
test-codegen-fence: ensure-golangci check-codegen-external-tests
    cd test/data/codegen && go build ./... && go vet -tags codegen_live ./...
    cd test/data/codegen && go mod tidy -diff
    cd test/data/codegen && {{golangci}} run

# Holds the nested module to the packaging that keeps it inside govulncheck's
# call graph. This is the only always-run required gate over that module, so it
# is the only place the convention can be held (bd gqlc-rohp).
#
# The mechanism, precisely, because a wrong account of it is how someone
# reasons their way back into the bug. govulncheck builds its package graph
# keyed by PkgPath and skips any package whose PkgPath is already present,
# *without descending into that package's imports* — PackageGraph.AddPackages,
# x/vuln internal/vulncheck/packages.go. The in-package test variant
# `p [p.test]` carries PkgPath `p`, the same key as the plain package `p`, which
# go/packages returns first; so the variant and everything only it imports are
# discarded with no diagnostic. It is not a contest that a richer package wins:
# a directory holding nothing but in-package _test.go files still produces a
# plain `p` entry with no Go files and no imports, and that empty entry takes
# the key just the same (measured). An external test package survives only
# because PkgPath `p_test` collides with nothing.
#
# Two assertions. The first is the convention; the second is the consequence
# that actually matters, because a guard that only pins today's spelling is not
# a guard:
#
#  1. every _test.go anywhere under the module declares an external test
#     package. Whole subtree rather than a top-level glob, and the package name
#     is parsed out of the clause rather than matched as a whole line — the
#     previous `grep -qx 'package fixtures'` form was defeated by a trailing
#     comment, by any file below the module root, and by matching nothing at all
#     (grep exits 2, `!` turns that into success). Empty file set is a failure
#     here, not a pass.
#  2. the package closure govulncheck will actually load still reaches
#     testcontainers-go. That closure is the non-test deps plus the deps of
#     every external test package — the set AddPackages ends up with — and
#     modelling it with `go list` reproduces govulncheck's own count exactly: 54
#     modules today, against 9 for the non-test build alone. This catches what
#     (1) cannot: a dropped codegen_live tag, a deleted or renamed battery, a
#     move of the container code behind a different tag.
[private]
check-codegen-external-tests:
    #!/usr/bin/env bash
    set -euo pipefail
    cd test/data/codegen

    mapfile -t tests < <(find . -type f -name '*_test.go' | sort)
    if [ "${#tests[@]}" -eq 0 ]; then
        echo "error: no _test.go files found under test/data/codegen." >&2
        echo "       The live battery has moved and this guard is checking nothing (bd gqlc-rohp)." >&2
        exit 1
    fi

    inpackage=()
    for f in "${tests[@]}"; do
        pkg="$(sed -n 's/^package[[:space:]]\{1,\}\([A-Za-z_][A-Za-z0-9_]*\).*$/\1/p' "$f" | head -1)"
        case "$pkg" in
            *_test) ;;
            "") inpackage+=("$f (no package clause)") ;;
            *)  inpackage+=("$f (package $pkg)") ;;
        esac
    done
    if [ "${#inpackage[@]}" -ne 0 ]; then
        echo "error: every _test.go under test/data/codegen must declare an external test package." >&2
        echo "       govulncheck drops the in-package test variant together with everything only it" >&2
        echo "       imports, so 'just vuln' goes green over the driver, testcontainers and docker" >&2
        echo "       trees it never loaded (bd gqlc-rohp). Offending files:" >&2
        printf '         %s\n' "${inpackage[@]}" >&2
        exit 1
    fi

    xtest="$(go list -tags codegen_live -f '{{{{range .XTestImports}}{{{{println .}}{{{{end}}' ./... | sort -u)"
    if ! go list -deps -tags codegen_live ./... ${xtest} \
        | grep -qx 'github.com/testcontainers/testcontainers-go'; then
        echo "error: github.com/testcontainers/testcontainers-go is not in the package closure" >&2
        echo "       govulncheck will load for test/data/codegen, so the live battery's dependency" >&2
        echo "       tree is unscanned again (bd gqlc-rohp). The closure is the non-test deps plus" >&2
        echo "       every external test package's deps; check the codegen_live tag still reaches" >&2
        echo "       the container code and that the battery is still an external test package." >&2
        exit 1
    fi

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
    cd test/data/codegen && go test -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit' ./...

# the neo4j half: both driver arms in parallel against one neo4j:5-community
# image. This is the half PR CI blocks on, so its wall time is a PR's wall time.
test-codegen-live-neo4j:
    cd test/data/codegen && go test -tags codegen_live -run TestLiveSmoke -skip 'TestLiveSmoke/apache-age' ./...

# the Apache AGE half: the smoke battery's AGE arm and the session-init
# contract, each on its own apache/age container. Nightly and manual only —
# these containers are cost this project does not charge to a pull request.
# -count=1 because this is the AGE arm's only gate and no pull request pays for
# it, so the run it reports on has to be a real one.
test-codegen-live-age:
    cd test/data/codegen && go test -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit' -skip 'TestLiveSmoke/neo4j' ./...

# call-graph-aware vulnerability scan; run on dependency changes and on the
# weekly CI schedule ("@latest" deliberate: the vuln DB matters more than
# tool-version reproducibility)
#
# Two invocations, because a root-rooted untagged scan without -test misses
# three separate things (bd gqlc-rohp). -test: without it govulncheck loads no
# test files at all, so every test-only dependency is unscanned — godog and
# testify at root, and everything the live battery reaches in the nested module.
# The nested module needs its own invocation because it is a separate module:
# `go list ./...` at root emits none of its packages, so its driver,
# testcontainers and docker trees are outside a root-rooted scan by module
# boundary. It also needs the codegen_live tag, which is where the
# container-driving code enters the build.
#
# WHAT THIS GATE STILL DOES NOT SEE. govulncheck does not analyse the in-package
# test variant of a package: its graph is keyed by PkgPath and the variant
# `p [p.test]` shares PkgPath `p` with the plain package, which is added first,
# so the variant is skipped along with everything only it imports
# (PackageGraph.AddPackages, x/vuln internal/vulncheck/packages.go). rohp closed
# that in the nested module by making the battery an external test package. The
# ROOT module — the one that ships the compiler — still has it: 34 in-package
# test files against 16 external, with third-party imports in the in-package
# tests of ten packages, internal/cli, internal/codegen{,/age,/neo4j},
# internal/config, internal/queryfile, internal/resolver and
# internal/schema/gql{,/annexd,/isobnf}. A called vulnerability reachable only
# from one of those files exits 0 here. Measured, not assumed: planting a
# yaml.Unmarshal call into internal/schema/gql/propertytype_test.go
# (`package gql`) reports nothing; the identical call in a `package gql_test`
# file reports it with a trace.
#
# The trap is that -test DOES raise the root module count 31 → 42, and the
# module set is in fact complete — modelling the visible closure with `go list`
# and diffing it against the full -test closure leaves no residual module and no
# residual package. So every symptom you would think to look for says the gate
# is closed. What is missing is only call edges, which nothing in the output
# reports. bd gqlc-m5rc converts those files; vuln-root-residual below is the
# number in the meantime.
vuln: vuln-root-residual
    go run golang.org/x/vuln/cmd/govulncheck@latest -test ./...
    cd test/data/codegen && go run golang.org/x/vuln/cmd/govulncheck@latest -tags codegen_live -test ./...

# Prints the root module's residual blindness to `just vuln`, so it is a number
# measured on every scan rather than a claim in a comment that rots (bd
# gqlc-m5rc). Reports, does not fail: the residual is the repo's existing house
# style, and turning it into a ratchet would fail every branch that adds an
# in-package test — a policy call that belongs to the conversion bead, not to
# CI plumbing. Deliberately counts files and packages rather than modules: the
# module and package sets are already complete (verified by set-differencing the
# visible closure against the full -test closure), so a module-level metric here
# would report a reassuring zero over the real gap.
#
# "Third-party" is anything outside the main module whose first path element
# contains a dot, which is what puts a package in a vulnerability database at
# all; .TestImports is exactly the in-package test variant's import set.
vuln-root-residual:
    #!/usr/bin/env bash
    set -euo pipefail
    module="$(go list -m)"
    inpackage=0
    external=0
    while IFS= read -r f; do
        pkg="$(sed -n 's/^package[[:space:]]\{1,\}\([A-Za-z_][A-Za-z0-9_]*\).*$/\1/p' "$f" | head -1)"
        case "$pkg" in
            *_test) external=$((external + 1)) ;;
            *) inpackage=$((inpackage + 1)) ;;
        esac
    done < <(git ls-files '*_test.go' | grep -v '^test/data/codegen/')
    blind="$(go list -f '{{{{.ImportPath}} {{{{join .TestImports " "}}' ./... |
        while read -r pkg imports; do
            for i in ${imports}; do
                case "$i" in "$module" | "$module"/*) continue ;; esac
                case "${i%%/*}" in *.*)
                    echo "${pkg#"$module"/}"
                    break
                    ;;
                esac
            done
        done | sort -u)"
    echo "root module test-file packaging (bd gqlc-m5rc): ${inpackage} in-package, ${external} external"
    echo "  in-package tests import third-party code in $(echo "${blind}" | grep -c .) packages — those call edges are outside govulncheck's call graph:"
    echo "${blind}" | sed 's/^/    /'

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
