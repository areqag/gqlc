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
test-codegen-fence: ensure-golangci
    cd test/data/codegen && go build ./... && go vet -tags codegen_live ./...
    cd test/data/codegen && go mod tidy -diff
    cd test/data/codegen && {{golangci}} run

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
    cd test/data/codegen && go test -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation' ./...

# the neo4j half: both driver arms in parallel against one neo4j:5-community
# image. This is the half PR CI blocks on, so its wall time is a PR's wall time.
test-codegen-live-neo4j:
    cd test/data/codegen && go test -tags codegen_live -run TestLiveSmoke -skip 'TestLiveSmoke/apache-age' ./...

# the Apache AGE half: the smoke battery's AGE arm, the session-init contract
# and the dialect fact the AGE backend's edge-union refusal rests on, each on
# its own apache/age container. Nightly and manual only — these containers are
# cost this project does not charge to a pull request. -count=1 because this is
# the AGE arm's only gate and no pull request pays for it, so the run it reports
# on has to be a real one.
test-codegen-live-age:
    cd test/data/codegen && go test -count=1 -tags codegen_live -run 'TestLiveSmoke|TestAGESessionInit|TestAGERefusesRelationshipTypeAlternation' -skip 'TestLiveSmoke/neo4j' ./...

# call-graph-aware vulnerability scan; run on dependency changes and on the
# weekly CI schedule ("@latest" deliberate: the vuln DB matters more than
# tool-version reproducibility)
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...

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
