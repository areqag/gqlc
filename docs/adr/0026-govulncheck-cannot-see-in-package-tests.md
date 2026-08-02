# ADR 0026 — govulncheck cannot see in-package tests

**Status:** Accepted
**Date:** 2026-08-02
**Beads:** gqlc-rohp, gqlc-m5rc, gqlc-k22l

## Context

`govulncheck` builds its package graph keyed by import path.
`PackageGraph.AddPackages` (`golang.org/x/vuln@v1.6.0`,
`internal/vulncheck/packages.go:144`) does:

```go
if _, found := g.packages[pkg.PkgPath]; found {
	continue
}
```

The `continue` skips the duplicate **and never descends into its imports**. The
in-package test variant `p [p.test]` carries `PkgPath == p`, the same key as the
plain package `p`, which `go/packages` returns first. So the variant, and
everything only it imports, is discarded with no diagnostic and no non-zero
exit. An external test package survives only because `PkgPath == p_test`
collides with nothing.

It is not a contest that the richer package wins. A directory holding nothing
but in-package `_test.go` files still produces a plain `p` entry with no Go
files and no imports, and that empty entry takes the key just the same
(measured).

The effect was proved with a single-variable A/B at the root module, only the
package clause differing: `golang.org/x/text` pinned to `v0.37.0` and a
`norm.NFC.String` call planted in `internal/schema/gql`. Under `package gql` the
scan printed "No vulnerabilities found" and exited 0; under `package gql_test`
it exited 3 reporting `GO-2026-5970` with the trace
`gql_test.TestRVPlant calls norm.Form.String`. The pin is what gives the A/B its
discriminating power — the `gopkg.in/yaml.v3 v3.0.1` this module already
requires carries no advisory at all, so an A/B built on it reports nothing in
either arm and would "pass" against a gate that saw everything.

Every symptom a reader would think to check says the gate is closed. `-test`
does raise the root module count, and set-differencing the visible closure
(non-test deps plus every external test package's deps — the set `AddPackages`
ends up with) against the full `-test` closure leaves no third-party module and
no third-party package missing. The only stdlib packages in the difference are
`internal/fuzz`, `runtime/pprof`, `testing/internal/testdeps` and
`testing/iotest`, and govulncheck reports the standard library as one unit. What
is missing is only **call edges**, and nothing in the output reports those.

The nested `test/data/codegen` module is the acute case: its live battery is the
only thing that imports the drivers, testcontainers and docker trees, so an
in-package battery leaves that whole dependency tree unanalysed.

## Decision

**The nested module's battery is an external test package**, held by
`just check-codegen-external-tests`, which the codegen fence runs. That is the
only always-run required gate over that module, so it is the only place the
convention can be held.

The guard asserts two things. The convention itself — every `_test.go` anywhere
under the module declares an external test package — and its consequence, that
the closure govulncheck will actually load still reaches
`testcontainers-go`. The second is what catches a dropped `codegen_live` tag, a
deleted or renamed battery, or a move of the container code behind a different
tag: failures the first cannot see. An empty file set is a failure, not a pass.

**The guard runs before the linter.** The `ireturn` allowlist in `.golangci.yml`
is keyed on the battery's external-test package path, so reverting the battery
to an in-package clause trips both the guard and the linter. With the linter
first, the failure a developer sees names `ireturn` rather than the scan, and
the allowlist ends up doing the guard's job by accident — only for as long as
those interface types exist.

**The root module's residual is measured and ratcheted, not fixed.**
`just vuln-root-residual` prints the in-package/external test-file split and the
set of packages whose in-package tests import third-party code, and fails when
that set grows. The file counts only report: a new in-package test is this
repo's house style and failing on it would be churn with no risk behind it. The
blind set ratchets, because it grows only on the risk event itself — a package
acquiring a third-party import it cannot be scanned through. The baseline is the
set rather than its size so a trip can name the package that went blind, and it
is checked in both directions: a package that leaves the set has to leave the
baseline too, or the ratchet quietly regains the slack it just won.

The recipe rides ci.yml's `lint` job rather than the `govulncheck` job, because
the vuln job scans only when a `go.mod` changes and the residual moves on PRs
that add a test file.

**`just vuln` runs with `-show verbose`.** The scan is at symbol level, so
package- and module-level findings do not change its exit status; without
verbose they appear only as a count. Naming them is what lets a reader of a CI
log see *which* advisories the gate is exiting 0 over, and notice when that set
changes. The three currently accepted are tracked on gqlc-k22l.

## Considered alternatives

**Convert every root in-package test to an external test package.** The correct
fix, and the reason gqlc-m5rc exists. Not done here: it touches every package,
and some of those tests want unexported state for good reasons. The goal is to
shrink the residual and know its size, not to reach zero by exporting things
that should stay private.

**Report a module-level metric instead of packages and files.** Rejected: the
module set is already complete, so it would report a reassuring zero over the
real gap.

**Grep for the expected package clause.** Rejected. A whole-line match is
defeated by a trailing comment, by any file below the module root, and by
matching nothing at all — `grep` exits 2 on no match, which `!` turns into
success. The guard parses the package name out of the clause and treats an empty
file set as a failure.

**Fail the gate on module-level findings.** Rejected: it would block on
advisories in modules nothing in the tree imports, including one with no fix
available.

## Consequences

- A `_test.go` added under `test/data/codegen` must declare an external test
  package, and the `ireturn` allowlist in `.golangci.yml` must keep matching the
  battery's `_test`-suffixed package path.
- A package whose in-package tests acquire a third-party import fails
  `just vuln-root-residual` on every PR. The fix is to move the importing test
  to an external test package; adding the package to the baseline requires a
  reason in the commit message.
- A test behind a non-default build tag is invisible to the ratchet, as it is to
  `go test ./...`. No root test file carries a build constraint today.
- `just vuln` output is longer, and includes the package and module inventory
  each scan matched. That inventory is the standing evidence that widening the
  scan to both modules is still in effect.
