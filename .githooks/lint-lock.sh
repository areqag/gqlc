#!/usr/bin/env bash
# A bounded wait on another worker's golangci-lint lock (bd gqlc-hg61, gqlc-49pc).
#
#     lint-lock.sh <command> [args...]
#
# Runs the command and exits with its status. The command's merged output is
# streamed as it arrives and captured, because these runs are slow and a
# developer watching one should not be handed a blank terminal so that this
# script can grep afterwards.
#
# WHY THIS IS SHARED RATHER THAN LIVING IN THE HOOK. golangci-lint takes ONE
# lock per machine and refuses to start while another instance holds it. The
# lock is the file /tmp/golangci-lint.lock and it is NOT per-cache-directory:
# measured 2026-08-29 against the pinned 2.13.1, a second run under a different
# GOLANGCI_LINT_CACHE is refused just the same. That is why a neighbouring
# WORKTREE contends at all, since every worktree has its own cache by design
# (justfile). With one worker this is unreachable; with several sibling
# worktrees linting at once it is routine, and it gets worse the more of them
# are running.
#
# The wait used to live only in .githooks/pre-push, which is the wrong way
# round: CLAUDE.md's session-completion workflow puts the quality gates before
# the push, so a hand-run `just lint` is the FIRST place anyone meets the lock
# and the hook was the second. Three concurrent workers were observed on
# 2026-08-29 each spinning a private `for i in 1..6; sleep 40` loop around
# `just lint`.
#
# THE RETRY IS NARROW IN BOTH DIRECTIONS, which is the whole safety argument:
#   - it requires the lock's own sentence in the output, AND
#   - it requires an exit code that is not 1. golangci-lint's --issues-exit-code
#     is 1 and neither the justfile nor .golangci.yml overrides it, so 1 is the
#     only code meaning "I graded the code and found issues". A real finding in
#     the caller's own diff is therefore never retried, whatever it prints.
# On exhaustion the command's failure is passed through. A gate that yields
# under load is not a gate.
#
# THE BUDGET BELOW IS DERIVED, NOT CHOSEN (bd gqlc-6ypo). Measured 2026-09-11 on
# this machine, worktree gqlc-6ypo, against the pinned golangci-lint 2.13.1:
#
#   a. A REFUSED ATTEMPT COSTS 5.06s, not nothing. Three consecutive runs against
#      a lock held by `flock /tmp/golangci-lint.lock -c 'sleep 30'` took 5059ms,
#      5064ms and 5058ms, each exit 3. That is golangci-lint's own wait, not
#      process startup: at the deployed commit 6d2288e0, run.go's acquireFileLock
#      gives TryLockContext a `totalTimeout = 5 * time.Second` with a 1s
#      retryDelay. So a cycle costs 5s PLUS the sleep, and the old error message's
#      "12 attempts (10s apart)" understated the wall clock it had spent by 40%.
#   b. THE SLOWEST SINGLE HOLD IS ~96s, not four minutes. Sampling
#      `ps -eo pid,etimes,args=` every 2s across a genuinely cold `just lint` in a
#      fresh worktree (no .bin/golangci-cache, GOCACHE a fresh mktemp -d), the root
#      `golangci-lint run` lived 96s; a later warm root run was 88s.
#   c. A COLD `just lint` IS 161s WALL, AND THAT IS THE NUMBER NOT TO USE. It is
#      FOUR separate acquisitions — three inside check-golangci-formatters-report's
#      probe plus the root run — so a waiter never queues behind all of it at once.
#      gqlc-6ypo was filed against 4m2.6s of `just lint`, which is this quantity.
#   d. FIFTEEN worktrees were registered that day (1 main + 14 siblings), and six
#      were observed contending inside one 30s window.
#
# So: cycle = 5s refused attempt + jittered sleep, mean 5 + 15 = 20s. Thirty
# retries is 31 attempts: 455s at the jitter's floor, ~605s at its mean, 755s at
# its ceiling. That covers SIX 96s holds ahead of you (576s) — the worst
# contention this fleet has actually been seen in. The old twelve covered 185s,
# which is one hold and not two.
#
# WHAT IT DOES NOT COVER, said plainly because the alternative is a number that
# implies a guarantee it has not got: fourteen peers each holding a cold lock is
# ~22 minutes, and this budget will refuse long before that. That is the right
# trade in this direction. The refusal below is loud, names its cause and names
# the command that finds the holder; an unbounded wait is silent, and a silent
# wait on this fleet is taken for a wedged session by watchdogs that kill it
# without printing anything at all.
#
# THE SLEEP IS JITTERED FOR A SECOND REASON, independent of the budget's size. A
# fixed delay makes waiters synchronise: every refused worker sleeps exactly the
# same 10s, so they all retry on the same boundary, one wins the flock and the
# rest are refused again having each spent an attempt to learn nothing. Observed
# 2026-09-11 reaching "attempt 7 of 13" with `ps` showing NO holder at the instant
# it was sampled — the budget was going on the race rather than on anyone useful.
# Raising the count alone fixes one of the two mechanisms.
#
# NOT `--allow-serial-runners`, and it is worth saying why since it is the
# built-in answer and it does work. Measured the same day: with the lock held for
# 40s, `golangci-lint run --allow-serial-runners` waited it out and then ran, exit
# 0 — and at 6d2288e0 that path passes `context.Background()` to TryLockContext,
# i.e. it polls every second FOREVER. It would delete this loop and the herd with
# it, at the cost of the bound and of every diagnostic in this file, and it prints
# nothing at all while it waits. (It is also not the flag gqlc-6ypo rules out:
# --allow-parallel-runners lets N linters run at once and is a memory bill;
# --allow-serial-runners still serialises.) Revisit if the refusal below starts
# firing on real queues rather than on the race.
set -uo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: lint-lock.sh <command> [args...]" >&2
  exit 2
fi

# The sentence golangci-lint itself prints. Verified against the real binary
# rather than transcribed: holding /tmp/golangci-lint.lock with flock and
# running .bin/golangci-lint 2.13.1 produces exactly
#   Error: parallel golangci-lint is running
#   The command is terminated due to an error: parallel golangci-lint is running
# and exits 3.
lock_sentence='parallel golangci-lint is running'

attempts=$((1 + ${GQLC_LINT_LOCK_RETRIES:-30}))
delay="${GQLC_LINT_LOCK_DELAY:-10}"

# Whole seconds, checked rather than assumed: the jitter below is integer
# arithmetic, and a fractional delay — which the old `sleep "${delay}"` accepted
# — would abort it with a bash syntax error mid-wait instead of waiting. Refused
# here, where the message can say so, rather than at the first contended run.
case "${delay}" in
  '' | *[!0-9]*)
    echo "lint-lock.sh: GQLC_LINT_LOCK_DELAY must be a whole number of seconds," >&2
    echo "              got '${delay}'." >&2
    exit 2
    ;;
esac

# Uniform over [delay, 2*delay). Full jitter down to zero is not wanted: the
# floor is what stops a refused worker from busy-spinning on a lock that is going
# to be held for another minute and a half, and the spread is what stops the
# refused workers retrying in lockstep. $RANDOM's modulo bias over a range this
# small is a fraction of a second and the point is to break the lockstep, not to
# be a good source of randomness.
jittered_delay() {
  if [ "${delay}" -eq 0 ]; then
    printf '0\n'
    return
  fi
  printf '%s\n' "$((delay + RANDOM % delay))"
}

waited=0

log="$(mktemp)"
trap 'rm -f "${log}"' EXIT

attempt=0
while :; do
  attempt=$((attempt + 1))
  rc=0
  # `set -o pipefail` above is what makes this read the command's status
  # rather than tee's.
  "$@" 2>&1 | tee "${log}" || rc=$?

  if [ "${rc}" -eq 0 ] || [ "${rc}" -eq 1 ] \
    || ! grep -qF "${lock_sentence}" "${log}"; then
    exit "${rc}"
  fi

  if [ "${attempt}" -ge "${attempts}" ]; then
    echo "ERROR: golangci-lint never got its lock: another worker on this machine held" >&2
    echo "       it for all ${attempts} attempts, over ${waited}s of sleeping plus ~5s of" >&2
    echo "       refused start per attempt, while running: $*" >&2
    echo "       THIS IS ALMOST CERTAINLY NOT YOUR CODE. The linter refused to START;" >&2
    echo "       it did not grade your changes and found nothing wrong with them. Do not" >&2
    echo "       go debugging a tree this run never looked at." >&2
    echo "       Correct response: wait for the other lint to finish and run again." >&2
    echo "       Who holds it:  ps -eo pid,etimes,args= | grep '[g]olangci-lint'" >&2
    echo "       Do NOT push with --no-verify, and do NOT end a session with work" >&2
    echo "       unpushed — unpushed work here is lost work (bd gqlc-hg61)." >&2
    exit "${rc}"
  fi

  this_delay="$(jittered_delay)"
  waited=$((waited + this_delay))
  echo "NOTE: another worker holds the golangci-lint lock; this is not your code." >&2
  echo "      Waiting ${this_delay}s and retrying (attempt ${attempt} of ${attempts}," >&2
  echo "      ${waited}s slept so far) — bd gqlc-49pc, gqlc-6ypo." >&2
  sleep "${this_delay}"
done
