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
set of packages whose in-package tests reach third-party code, and fails when
that set grows. The file counts only report: a new in-package test is this
repo's house style and failing on it would be churn with no risk behind it. The
blind set ratchets, because it grows only on the risk event itself — a package
acquiring a third-party import it cannot be scanned through. The baseline is the
set rather than its size so a trip can name the package that went blind, and it
is checked in both directions: a package that leaves the set has to leave the
baseline too, or the ratchet quietly regains the slack it just won.

**Reach is transitive through own-module packages.** What `AddPackages` loses is
the variant's outgoing edges, not any particular importer's, so an own-module
package that only the discarded variant pulls in is lost with everything *it*
imports. A test importing an own-module helper that imports third-party code is
therefore as blind as one importing that code directly, and a walk that stops at
the module boundary reports a residual smaller than the real one — in the
reassuring direction (bd gqlc-nsq4). No package in the tree has that shape
today, which is exactly why the walk carries a fixture it checks itself against
on every run: a recursion with nothing live to walk can be lost without any
measurement moving.

The recipe rides ci.yml's `lint` job rather than the `govulncheck` job. The
residual moves on any PR that adds a test file, and `lint` is the job that runs
on all of them unconditionally.

**The module set, each module's build tags, and the CI trigger are all derived,
not declared** (bd gqlc-pig9). `just vuln` scans every `go.mod` under the
checkout, with every build tag that module's own packages constrain themselves
by — a tag is where code enters the build, so a module scanned without its tags
is a module partly scanned, and `ignore` is excluded because that tag's whole
meaning is "not part of any build". The tags are read off the module's files on
disk rather than out of `go list ./...`, for the reason below.

Which tags a file *asks for* is derived; which spellings *are* tags is declared,
and the declaration is `.golangci.yml`'s `run.build-tags` (bd gqlc-e7oq). The
derivation classifies every term in a constraint against four vocabularies —
`go tool dist list`, `go1.N`, the toolchain's own terms, and that key — and a
term it can place in none of them is an error naming the file, never a `-tags`
argument. Making "custom build tag" the default case is what let a `go tool dist
list` that came back short turn the platform values it had lost back into tags,
silently; there is no default case now. A genuinely new custom tag therefore
goes into `.golangci.yml` first, which is where golangci-lint has always needed
it, and `check-golangci-build-tags` still holds that key and the tree to each
other in both directions.

vuln.yml arms on any `.go` file, any
`go.mod`/`go.sum` anywhere, the recipe, or itself. Each list this replaces was a
proxy for the real condition: a module list omits the third module the day it is
added, and a `go.mod`-only trigger misses a PR that newly imports or newly calls
an already-required module, which is the state `GO-2026-5841` sits in today
(bd gqlc-k22l).

**A derivation gets a postcondition, because a derivation can also come back
empty.** Deriving the tags rather than listing them removes one way to be green
over unscanned code and introduces another: a tag walk that silently found
nothing would produce a narrower build, a scan that ran, and an exit 0. So
before each module is scanned the recipe asserts that `go list`'s own
`IgnoredGoFiles` is empty under the derived tags. Measured: with the tag walk
broken, the nested module's advisory count goes from three to zero and the
recipe still exits 0 without this assertion, and fails naming the four excluded
files with it.

**That assertion had a blind spot of exactly the shape it guards against.**
`IgnoredGoFiles` is reported per package, so it can only speak for a directory
`go list` produced a package for — and `go list`'s `./...` wildcard does not
match a directory whose Go files are **all** excluded by build constraints. No
package, no error, no ignored files. A wholly tag-gated directory therefore
derived no tag from a `go list`-driven walk *and* reported nothing to a `go
list`-driven assertion. Measured end to end: a planted module whose only
constrained directory calls `yaml.Unmarshal` at `gopkg.in/yaml.v2 v2.2.1` behind
`//go:build revsecret` scanned as `tags [none]`, reported an empty
`IgnoredGoFiles`, and exited 0; the same module scanned with its tag exited 3
over three called advisories with a printed call trace. `test/data/codegen` does
not expose this, because its four `live_*_test.go` sit beside unconstrained
files, so the package is listed and `IgnoredGoFiles` is populated — moving the
live battery into a directory of its own would have opened it.

**So the tags are derived from a filesystem walk, and the postcondition has two
halves.** The walk enumerates the module's `.go` files on disk, bounded by the
nested-module roots discovery has already found, so it never crosses a module
boundary. The postcondition then asserts first that every directory holding a Go
file was matched by `go list ./...` under the derived tags — the only way to see
a directory the wildcard skipped — and second that no file of a matched package
was dropped, which is `IgnoredGoFiles` as before. Neither is a second copy of the
derivation, so neither can go stale with it. Both are scoped to what `./...`
matches, which is govulncheck's own scope: `testdata`, `vendor`, and dot- or
underscore-prefixed directories are outside both, and the walk applies the same
exclusions so the two sets answer the same question.

`test/data/tagblind` is the fixture — a directory whose every Go file is
constrained, and the only live exercise of either half. The recipe asserts its
shape before it scans: present, still unmatched by an untagged `go list ./...`,
and still contributing its tag to the walk. A fixture that quietly stopped
having that shape would take both guards out of service with nothing failing.
Reverting the walk to `go list ./...` fails on it. The tag is also in
`.golangci.yml`'s `build-tags`, so the one directory no default-configuration
tool loads is not also the one nothing lints.

The diagnostic when `IgnoredGoFiles` is non-empty names three causes, not two.
Besides a missed constraint, and something `-tags` cannot enable (a GOOS/GOARCH
filename suffix, `//go:build ignore`), the derivation can exclude a file
*itself*: `//go:build !windows` derives the tag `windows`, and `-tags windows`
then makes that constraint false. The gate goes red, which is correct and
fail-closed, but a reader sent after the wrong cause loses the time anyway. The
underlying derivation defect is bd gqlc-e7oq.

The same grading applies to vuln.yml's filter. `grep` exiting 1 means "no match"
and may be read as "nothing relevant changed"; any higher status is `grep`
failing, and the `|| true` that keeps `bash -e` from aborting on the first would
turn the second into a silent skip of the scan. The status is compared rather
than discarded.

**`just vuln` runs with `-show verbose`.** The scan is at symbol level, so
package- and module-level findings do not change its exit status; without
verbose they appear only as a count.

**The advisories the gate exits 0 over are a register, checked by set equality,
not a note.** Verbose naming them was originally so that a reader of a CI log
could see the set change — which is a guard nobody executes, because nobody
reads the log of a green job. The ids are now read back out of govulncheck's own
output and compared against a register kept in the recipe, in both directions
(bd gqlc-k22l). An advisory that appears without a recorded decision fails the
gate; an entry the scan no longer reports fails it too, until the line is
deleted. The second direction is what makes the first trustworthy: reading the
ids out of the output means that if the output format moves and the extraction
stops matching, the measured set empties and the register goes stale — the gate
fails rather than silently accepting everything.

**That last property is only unconditional because the extraction is checked
against the output too.** A stale register is a comparison against a *non-empty*
register; if the extraction broke while the register happened to be empty, both
halves would compare two empty sets and pass. So the extraction is graded
directly. govulncheck names every finding on its own `Vulnerability #N: <id>`
line, so per module a header line the id pattern cannot read fails the gate, and
across the run at least one such line must have been read at all — the latter
catching the case where the header itself moves and every count agrees at zero.
If the day comes that this tree genuinely has no findings, that check and the
register empty together, deliberately.

This refines rather than reverses the rejection of *fail the gate on
module-level findings* below. The objection there was blocking on advisories
nothing in the tree imports, one of them with no fix available; the register
does not block on those, it blocks on the arrival of a new one.

**The three registered advisories are accepted, not bumped, and the reason is a
version.** `GO-2026-5158` (otel) and `GO-2026-5841` (klauspost/compress) are
both indirect dependencies of `testcontainers-go`, whose latest published
release is `v0.43.0` — exactly what `test/data/codegen` already requires. There
is no upstream release to move to, and pinning an indirect dependency ahead of
the module that requires it is churn `go mod tidy` can undo, bought with no
reduction in exposure, since neither is called. `GO-2026-5932`
(`x/crypto/openpgp`) has no fix and never will; bumping `x/crypto` cannot clear
it. Revisit when `testcontainers-go` itself moves; `go list -m -u` inside
`test/data/codegen` is the check, and the register's stale half will announce it
anyway the moment an advisory clears.

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
matching nothing at all — `grep` exits 1 on no match, which `!` turns into
success. The guard parses the package name out of the clause and treats an empty
file set as a failure.

**Fail the gate on module-level findings.** Rejected: it would block on
advisories in modules nothing in the tree imports, including one with no fix
available. The register above is the narrower form that survives this objection
— it blocks on a *change* to that set, not on its existence.

**Suppress the three accepted advisories instead of registering them.**
Rejected. A suppression is one-directional: it silences an id whether or not the
scan still reports it, so it goes on describing a dependency long after that
dependency has moved, and it pre-accepts the id if it ever comes back. A
register compared in both directions is the same three lines with the failure
mode removed.

**Keep the module list literal and add a check that it matches discovery.**
Rejected as a fallback that was not needed: nothing about the scan requires a
static list — the recipe iterates and vuln.yml decides in-job rather than through
GitHub's static `paths:` filter, so both sides can discover directly. A
mismatch check would only reintroduce the list in order to guard it.

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
- A module added anywhere under the checkout is scanned, and its build tags are
  picked up, with no list to edit. A build tag added to an existing module widens
  that module's scan on the next run — including a tag whose files do not
  compile under it, which will fail the gate rather than be skipped.
- A Go file that `-tags` cannot reach fails `just vuln` rather than being quietly
  dropped from the scan: a GOOS/GOARCH filename suffix, or `//go:build ignore`.
  Neither exists in this tree today, and the first one to should be a decision
  recorded here, not a narrower scan nobody noticed. So does a negated constraint
  the derivation turns off itself (bd gqlc-e7oq).
- `test/data/tagblind` must keep holding nothing but build-constrained Go files.
  Deleting it, or adding an unconstrained file beside the one there, fails
  `just vuln` — it is the only exercise of the filesystem walk and of the
  directory-coverage half of the postcondition. Its tag is in `.golangci.yml`'s
  `build-tags`, so it is linted like anything else.
- A directory under a scanned module that `./...` does not match — `testdata`,
  `vendor`, or a dot- or underscore-prefixed name — is outside this gate, as it
  is outside govulncheck's own `./...`. Nothing in either module has that shape
  today.
- Nearly every pull request now runs the scan, because nearly every pull request
  touches a `.go` file. That is the intended cost: govulncheck answers at symbol
  level, so a call graph edge is as much an input to the answer as a version in a
  manifest.
- A newly published advisory against a required-but-uncalled dependency turns the
  weekly sweep red, and stays red until someone upgrades or writes a line in the
  register saying why not. That is the point — an advisory nobody has looked at
  is not the same state as one somebody accepted — but it does mean the sweep is
  a job with an owner rather than one that is green by default.
- `just vuln` output is longer, and includes the package and module inventory
  each scan matched. That inventory is the standing evidence that widening the
  scan to both modules is still in effect.
