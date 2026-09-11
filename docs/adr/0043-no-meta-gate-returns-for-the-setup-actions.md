# No meta-gate returns for the setup-* actions

`internal/tools/ciguard` does not come back. The coverage it held over
`.github/actions/setup-golangci` and `.github/actions/setup-shellcheck` is
replaced by one assert step inside each action, and the residue it does not
cover is an acknowledged gap rather than an open question.

This answers `gqlc-pz5jw`, which is part (b) of `gqlc-chep`. Part (a) landed as
PR #1803 and corrected four comments that named ciguard as live coverage after
PR #1595 deleted it. Part (b) was the decision those comments deferred, and it
is recorded here because it has now been asked twice: once when #1595 deleted
the tool, and once when #1803 found the comments still claiming it.

## What was unguarded

ciguard held three properties over those two action files. None of the three has
had anything holding it since PR #1595 (f6dc4c7b):

1. **Path agreement** — `path: .bin/golangci-lint` in the action against the
   justfile's `golangci` variable, and the same for `shellcheck`.
2. **The errexit spelling** — `version="$(just --evaluate ...)"` as a bare
   assignment on its own line, rather than `echo "version=$(just ...)"`. Under
   `set -e` the second form takes `echo`'s exit status, so a failed read leaves
   the step GREEN with the cache key emptied to `golangci-bin-Linux-`.
3. **No `if:` or `continue-on-error:`** on either action's steps.

Property 2 is the one that has actually cost something. It was not hypothetical:
commit 93a42972 (2026-08-17) fixed it after the step went green and keyed the
cache on the empty string.

## Evidence

Measured 2026-09-11, on `origin/master` at e5d7cdfa unless stated.

**The exposure is not live.** `GET /repos/areqag/gqlc/actions/caches` returns 49
entries. The two this decision is about read `golangci-bin-Linux-v2.13.1` and
`shellcheck-bin-Linux-v0.10.0`; no key in the repository ends in `-`. Both
`path:` values equal their justfile variable, and neither action's steps carry
an `if:` or a `continue-on-error:`.

**The files barely move.** Over the last 1000 commits on master (2026-07-04
onward) `.github/actions` was touched by 12; over the last 400 (2026-08-29
onward) by 8. `setup-golangci/action.yml` has TWO commits in its whole history
and the later one is prose. So the rate at which a guard here would be exercised
is roughly one substantive edit per quarter.

**A CI failure tally cannot settle it, and that matters more than the tally
itself.** Over the 144 failed `ci.yml` runs between 2026-07-11 and 2026-09-10,
read step by step from the Actions API, no failure is attributable to either
action. That is not evidence of absence: the defect's whole signature is a GREEN
step over a wrong cache key. It fails silently and costs re-downloads, so it
would never appear in a failure count however long the window. Any argument from
"it has never reddened" is void here, and this paragraph exists so the next
person does not make it.

**actionlint does not cover these files and cannot be made to.** `actionlint
v1.7.7 -shellcheck` over `.github/actions/setup-golangci/action.yml` reports
`"on" section is missing in workflow` — it parses an action file as a workflow.
Its default glob reaches `.github/workflows/` only, so the four setup-* actions
are graded by nothing today. Worth recording because it is the first thing that
looks like a free answer: shellcheck's optional `check-extra-masked-returns`
(SC2312) does flag the bad spelling exactly, measured on the pinned v0.10.0
against the real run block — but nothing hands it these files, and building
something that would is the meta-gate this decision declines.

## Decision

**(c) REBUILD is refused.** A tool that stubs `just` and parses action YAML is
an order of magnitude more mechanism than the exposure justifies at one
substantive edit per quarter, and PR #1595 deleted exactly that mechanism on
purpose as part of removing 28k lines of test scaffolding. Refusing it also
settles `gqlc-gu7ao` in the direction that bead already took: `lint-just`'s
`file` parameter was vestigial because ciguard was its only exerciser, and PR
#2722 dropped it. Nothing here asks for it back.

**(b) AS STATED is refused too.** `gqlc-pz5jw` offered "one test asserting the
two `path:` values equal `just --evaluate <var>`". That covers property 1, which
has never broken, and not property 2, which has. Its literal form is also wrong:
`just --evaluate golangci` returns an absolute path
(`justfile_directory() + "/.bin/golangci-lint"`) and the action's `path:` is
workspace-relative, so the equality it names is false today on a correct tree.

**What was taken instead** is an assert step in each of the two actions, placed
between the read step and the cache step. It reads no YAML and stubs nothing. It
checks that the emitted pin is non-empty, that it equals the justfile's version
variable, and that the justfile's install path is the path the cache saves.

That is a different kind of coverage from ciguard's and deliberately so. ciguard
graded the SPELLING; this grades what the spelling exists to produce. Every
cause of the August outcome arrives at it — the `echo` form, a stale literal
restated in the action, an `if:` that skips the read, a renamed justfile
variable — whereas ciguard caught one of those four. The pattern is not new
here: `.github/actions/setup-go` already asserts its own provisioning and its
own cache paths in-band, for the same class of green-but-wrong defect
(`gqlc-skwh7`, `gqlc-flko`).

## What stays an acknowledged gap

Three things, none of them measured to have happened:

- an `if:` or a `continue-on-error:` on the CACHE step. A skipped restore leaves
  the assertion true.
- a `continue-on-error:` on the assert step itself, which would print its
  refusal and let the job continue.
- an edit that moves `.bin/golangci-lint` in the justfile, in the `path:` and in
  the assertion together. The literal appears three times, so the assertion
  catches one side drifting and not a coordinated move. `setup-go` states the
  same limit about its own cache-path assertion and for the same structural
  reason: `actions/cache`'s `with:` block cannot be fed from a step output,
  because it runs before any step of the action could emit one.

An acknowledged gap gets re-examined where a false claim of coverage does not,
which is the whole argument of `gqlc-chep`. These three are listed in each
action file as well, beside the steps they are about.

## Cost

The assert step adds one composite step to two actions, used by five job
invocations across `ci.yml` (`setup-golangci` in `lint` and `codegen-fence`;
`setup-shellcheck` in `lint`, `tidy` and `actionlint`). On run 34544728807 the
whole of `setup-shellcheck` reports 2s and the whole of `setup-golangci` 1s at
the Actions API's one-second granularity, and every trivial step in the `tidy`
job reports 0s, so the addition is not resolvable from here. It costs two `just
--evaluate` calls, 7ms each locally. None of the five jobs is the critical path:
`test` is, at 137s against `lint` 29s, `codegen-fence` 30s, `tidy` 24s and
`actionlint` 24s on that run.

## Do not re-propose this

If the question comes back, it needs a NEW fact, not a re-reading of the ones
above. The two that would be new facts:

- a cache key in this repository observed carrying an empty or wrong version
  AFTER the assert steps landed, which would mean the assertion is in the wrong
  place rather than that a bigger one is needed; or
- one of the three acknowledged gaps actually firing.

"Nothing guards the spelling" is not one: the spelling is no longer the thing
worth guarding, for the reason under **Decision** above.
