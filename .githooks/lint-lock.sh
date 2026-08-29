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
# GOLANGCI_LINT_CACHE is refused just the same. That is why a neighbouring SEAT
# contends at all, since every worktree has its own cache by design (justfile).
# With one worker this is unreachable; the town runs sixteen seats, so it is
# routine and gets worse as the town gets busier.
#
# The wait used to live only in .githooks/pre-push, which is the wrong way
# round: CLAUDE.md and citizen-protocol.md both tell a citizen to run the gates
# BEFORE opening a PR, so a hand-run `just lint` is the FIRST place anyone meets
# the lock and the hook was the second. Three seats were observed on 2026-08-29
# each spinning a private `for i in 1..6; sleep 40` loop around `just lint`.
#
# THE RETRY IS NARROW IN BOTH DIRECTIONS, which is the whole safety argument:
#   - it requires the lock's own sentence in the output, AND
#   - it requires an exit code that is not 1. golangci-lint's --issues-exit-code
#     is 1 and neither the justfile nor .golangci.yml overrides it, so 1 is the
#     only code meaning "I graded the code and found issues". A real finding in
#     the caller's own diff is therefore never retried, whatever it prints.
# On exhaustion the command's failure is passed through. A gate that yields
# under load is not a gate.
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

attempts=$((1 + ${GQLC_LINT_LOCK_RETRIES:-12}))
delay="${GQLC_LINT_LOCK_DELAY:-10}"

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
    echo "       it for all ${attempts} attempts (${delay}s apart) while running: $*" >&2
    echo "       THIS IS ALMOST CERTAINLY NOT YOUR CODE. The linter refused to START;" >&2
    echo "       it did not grade your changes and found nothing wrong with them. Do not" >&2
    echo "       go debugging a tree this run never looked at." >&2
    echo "       Correct response: wait for the other lint to finish and run again." >&2
    echo "       Who holds it:  ps -eo pid,etimes,args= | grep '[g]olangci-lint'" >&2
    echo "       Do NOT push with --no-verify (Constitution IV.4) and do NOT sleep with" >&2
    echo "       work unpushed — unpushed work here is lost work (bd gqlc-hg61)." >&2
    exit "${rc}"
  fi

  echo "NOTE: another worker holds the golangci-lint lock; this is not your code." >&2
  echo "      Waiting ${delay}s and retrying (attempt ${attempt} of ${attempts}) — bd gqlc-49pc." >&2
  sleep "${delay}"
done
