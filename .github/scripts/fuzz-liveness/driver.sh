#!/usr/bin/env bash
# The liveness driver for fuzz targets (docs/specs/nightly-fuzzing-plan.md
# §10). Sourced by a target's <FuzzName>.rows, which calls, in order:
#
#   liveness_begin   NAME PKGDIR
#   liveness_control ID COMMAND
#   liveness_row     ID FILE SEDSCRIPT EXPECT MODE COMMAND
#   liveness_end
#
# What it guarantees about every row:
#   - The mutation lands in a `git archive HEAD` extract under mktemp -d, never
#     in a checkout: $PWD is asserted to be that copy before every sed.
#   - A row is VOID, not KILLED, when sed moved nothing (md5 unchanged), when
#     the mutant does not compile (`go test -c`, so a compiler kill cannot pass
#     for a harness kill), or — in MODE fuzz — when the target's committed
#     seeds already trip the mutant, which is the row claiming the fuzzer found
#     what the corpus held.
#   - KILLED means COMMAND exited non-zero AND its log matches the extended
#     regex EXPECT, so a red for some other reason does not count. In MODE fuzz
#     it also means the engine wrote a new crasher file: that is the mode for a
#     row whose claim is that the fuzzer, not the corpus, found the defect. MODE
#     seeds and MODE property ask for no crasher, because a defect the seed pass
#     reports is reported before the engine writes anything; a property row
#     killed there shows the property is checked, not that mutation reaches it.
#   - Restore is cp from a second, pristine extract, for FILE and for the
#     target's testdata/fuzz/NAME directory, so one row's crasher is not the
#     next row's seed. Each row also gets its own empty GQLC_FUZZ_CACHEDIR, so
#     no row inherits a grown corpus from the last or from the developer.
#
# It runs against HEAD, not the working tree: uncommitted edits are not in the
# copy, and liveness_begin says so when the package has any.
#
# COMMAND is a string run by `bash -c` in the copy, with LIVENESS_BUDGET (a Go
# duration, default 5m) in its environment.

LIVENESS_BUDGET="${LIVENESS_BUDGET:-5m}"
export LIVENESS_BUDGET

lv_name=""
lv_pkg=""
lv_scratch=""
lv_copy=""
lv_pristine=""
lv_failed=0
lv_table=()

lv_die() {
    echo "error: fuzz-liveness: $*" >&2
    exit 1
}

# liveness_begin NAME PKGDIR: extracts HEAD twice and enters the working copy.
liveness_begin() {
    lv_name="$1"
    lv_pkg="$2"
    local repo
    repo="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)" ||
        lv_die "not inside a git checkout, so there is no HEAD to archive"
    lv_scratch="$(mktemp -d)"
    trap 'rm -rf "$lv_scratch"' EXIT
    lv_copy="$lv_scratch/copy"
    lv_pristine="$lv_scratch/pristine"
    mkdir -p "$lv_copy" "$lv_pristine"
    git -C "$repo" archive HEAD | tar -x -C "$lv_copy"
    git -C "$repo" archive HEAD | tar -x -C "$lv_pristine"
    lv_copy="$(cd "$lv_copy" && pwd -P)"
    [ -d "$lv_copy/$lv_pkg" ] || lv_die "$lv_pkg is not a directory at HEAD"
    if ! git -C "$repo" diff --quiet HEAD -- "$lv_pkg"; then
        echo "note: $lv_pkg has uncommitted changes; the rows run against HEAD without them" >&2
    fi
    echo "== liveness for $lv_name ($lv_pkg) at $(git -C "$repo" rev-parse --short HEAD), budget $LIVENESS_BUDGET"
    cd "$lv_copy" || lv_die "cannot enter the copy"
}

lv_assert_in_copy() {
    [ "$(pwd -P)" = "$lv_copy" ] || lv_die "\$PWD is $(pwd -P), not the copy $lv_copy; refusing to mutate"
}

lv_restore() {
    local file="$1"
    lv_assert_in_copy
    cp "$lv_pristine/$file" "$lv_copy/$file"
    [ "$(md5sum <"$lv_copy/$file")" = "$(md5sum <"$lv_pristine/$file")" ] ||
        lv_die "$file differs from the pristine extract after restoring it"
    rm -rf "$lv_copy/$lv_pkg/testdata/fuzz/$lv_name"
    if [ -d "$lv_pristine/$lv_pkg/testdata/fuzz/$lv_name" ]; then
        cp -R "$lv_pristine/$lv_pkg/testdata/fuzz/$lv_name" "$lv_copy/$lv_pkg/testdata/fuzz/$lv_name"
    fi
}

# lv_run ID COMMAND: runs COMMAND in the copy under a fresh corpus root and
# leaves its exit status in lv_rc, its wall seconds in lv_secs and its log at
# $lv_scratch/ID.log.
lv_run() {
    local id="$1" cmd="$2" start
    mkdir -p "$lv_scratch/cache-$id"
    start="$(date +%s)"
    lv_rc=0
    GQLC_FUZZ_CACHEDIR="$lv_scratch/cache-$id" bash -c "$cmd" >"$lv_scratch/$id.log" 2>&1 || lv_rc=$?
    lv_secs=$(($(date +%s) - start))
}

# The detectors a red run can carry, named by the line each prints.
lv_detectors() {
    local log="$1" found=()
    command grep -q 'fuzzkit.Guard: slow input' "$log" && found+=(fuzzkit.Guard)
    command grep -q 'fuzzkit.Guard: watchdog' "$log" && found+=(Guard-watchdog)
    command grep -q 'hung or terminated unexpectedly' "$log" && found+=(engine-hang)
    command grep -q 'panic: test timed out' "$log" && found+=(test-timeout)
    command grep -q '^panic: \|panic: planted' "$log" && found+=(panic)
    command grep -q 'property ' "$log" && found+=(property)
    if [ "${#found[@]}" -eq 0 ]; then echo "none"; else (IFS=+; echo "${found[*]}"); fi
}

lv_crashers() {
    local dir="$lv_pkg/testdata/fuzz/$lv_name"
    comm -23 <(cd "$lv_copy/$dir" 2>/dev/null && find . -type f | sort) \
        <(cd "$lv_pristine/$dir" 2>/dev/null && find . -type f | sort) | wc -l
}

# liveness_control ID COMMAND: COMMAND on the unmutated copy must be green.
liveness_control() {
    local id="$1" cmd="$2"
    lv_assert_in_copy
    echo "-- $id (control): $cmd"
    lv_run "$id" "$cmd"
    local verdict=GREEN
    if [ "$lv_rc" -ne 0 ]; then
        verdict=RED
        lv_failed=1
        tail -n 40 "$lv_scratch/$id.log" >&2
    fi
    lv_table+=("$id|(none)|-|-|$verdict (want GREEN)|rc=$lv_rc|${lv_secs}s|-")
    echo "   $verdict rc=$lv_rc ${lv_secs}s"
}

# liveness_row ID FILE SEDSCRIPT EXPECT MODE COMMAND
liveness_row() {
    local id="$1" file="$2" script="$3" expect="$4" mode="$5" cmd="$6"
    local before after verdict detail crashers
    case "$mode" in
        seeds | fuzz | property) ;;
        *) lv_die "$id: MODE is '$mode', not seeds, fuzz or property" ;;
    esac
    lv_restore "$file"
    lv_assert_in_copy
    echo "-- $id: $file"
    before="$(md5sum <"$file" | cut -c1-8)"
    sed -i -e "$script" "$file"
    after="$(md5sum <"$file" | cut -c1-8)"
    diff -u --label "a/$file" --label "b/$file" "$lv_pristine/$file" "$file" || true
    if [ "$before" = "$after" ]; then
        verdict=VOID detail="sed moved nothing"
    elif ! go test -c -o /dev/null "./$lv_pkg" >"$lv_scratch/$id.screen" 2>&1; then
        verdict=VOID detail="mutant does not compile"
        cat "$lv_scratch/$id.screen" >&2
    elif [ "$mode" = fuzz ] && ! go test -count=1 -run "^$lv_name\$" "./$lv_pkg" >"$lv_scratch/$id.seeds" 2>&1; then
        verdict=VOID detail="a committed seed already trips the mutant"
        tail -n 20 "$lv_scratch/$id.seeds" >&2
    else
        lv_run "$id" "$cmd"
        crashers="$(lv_crashers)"
        detail="rc=$lv_rc detector=$(lv_detectors "$lv_scratch/$id.log") crashers=$crashers"
        if [ "$lv_rc" -ne 0 ] && command grep -Eq -- "$expect" "$lv_scratch/$id.log" &&
            { [ "$mode" != fuzz ] || [ "$crashers" -gt 0 ]; }; then
            verdict=KILLED
            command grep -Em1 -- "$expect" "$lv_scratch/$id.log" | sed 's/^/   evidence: /'
        else
            verdict=SURVIVED
            tail -n 40 "$lv_scratch/$id.log" >&2
        fi
    fi
    [ "$verdict" = KILLED ] || lv_failed=1
    lv_table+=("$id|$file|$before|$after|$verdict (want KILLED)|$detail|${lv_secs:-0}s|$cmd")
    echo "   $verdict $detail ${lv_secs:-0}s"
    lv_secs=0
    lv_restore "$file"
}

# liveness_end: prints the table and exits non-zero unless every control was
# green and every row KILLED.
liveness_end() {
    echo
    echo "| row | file | md5 before | md5 after | verdict | detail | wall | command |"
    echo "|---|---|---|---|---|---|---|---|"
    local r
    for r in "${lv_table[@]}"; do
        echo "| ${r//|/ | } |"
    done
    if [ "$lv_failed" -ne 0 ]; then
        echo "error: fuzz-liveness: $lv_name: a control was red or a row was not KILLED" >&2
        exit 1
    fi
    echo "fuzz-liveness: $lv_name: every control green, every row KILLED"
}
