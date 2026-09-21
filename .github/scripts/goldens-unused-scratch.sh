#!/usr/bin/env bash
# Makes and reaps the scratch copy `just check-goldens-unused` lints (bd
# gqlc-7hyt). That recipe removes its copy in an EXIT trap, and a trap cannot
# run under SIGKILL, which is how a session on the dev host ends when a quota
# wall or a stall watchdog takes it. Measured 2026-09-20: SIGKILL of the recipe's
# shell, and separately of its whole process group, mid-lint left the copy
# behind holding 2644 and 2639 inodes; killed before the first lint, 2381.
# CLAUDE.md's "Scratch space" has what that costs on a tmpfs capped in inodes.
#
#   new <root> <owner-pid>   mktemp the copy's directory under <root>, record
#                            its owner in it, print its path
#   sweep <root>             remove the copies under <root> whose owner is gone
#
# Either may be preceded by `--test-proc-root <dir>`, which is for the rows
# alone: see THE PROC ROOT below.
#
# Both halves are here so that the name, the owner record and the reading of
# /proc are each spelled once. The rows beside this file build every fixture
# through `new`, so they do not spell them either.
#
# A COPY IS REMOVED ONLY IF ALL OF THESE HOLD. Anything unreadable keeps it:
# what is being avoided is deleting the copy a live gate is linting, which a
# sweep keyed on name and age has done to a live worktree on this host before.
#
#   - it is directly under <root>, named by `new`'s template, and not a
#     symlink: one of that name is neither removed nor read through;
#   - it holds the owner record, so a directory someone else made under that
#     name is left alone. No line below asks that on its own: a copy with no
#     record has no age to read and no owner to call dead, and either keeps it;
#   - its owner is DEAD: no process has that pid, or the one that has it was not
#     started at the recorded tick (field 22 of /proc/<pid>/stat), which is what
#     tells a reused pid from the owner. A record is `<pid> <tick>`, each a
#     decimal number written the one way the kernel writes it: no leading zero,
#     a pid that is not 0 and no longer than pid_max's hard limit (4194304,
#     seven digits), a tick no longer than a u64. ANYTHING ELSE READS AS ALIVE —
#     an empty or garbled field, `0<pid>` beside that pid's right tick — because
#     a record this script cannot have written says nothing about a death. So
#     does an owner that is a zombie, until its parent reaps it; so does an
#     owner whose stat file is THERE and has no number in field 22, since only
#     a stat that cannot be read at all says the pid is gone; and so does every
#     owner on a host with no /proc: there `new` writes `-` for the tick, and
#     `sweep` removes nothing at all;
#   - the record is OLDER than stale_minutes. The owner test reads this host's
#     process table, so it calls dead an owner it cannot see: one in another pid
#     namespace sharing <root>, or on another host sharing it. Age is what is
#     left for those, so the threshold has to outlast a live run. The recipe
#     makes four golangci-lint runs and each may wait out .githooks/lint-lock.sh's
#     whole budget, 755 s at its ceiling: 50 minutes of waiting, against ~3 s of
#     work on the dev host. 90 is that with room. ASSUMED, and held by nothing
#     (2026-09-20): ONE fenced module, test/data/codegen. The recipe makes the
#     four runs once PER module `modscope modules` names besides the root, so a
#     second one takes a live run's ceiling to 100 minutes, past this number:
#     re-derive it then.
#
# <root> must be absolute, and both halves refuse one that is not: a relative
# root beginning with `-` reads to find as an option, and the sweep would go
# quiet over it rather than wrong.
#
# ONE SWEEPER REMOVES A COPY, AND A REMOVAL CUT SHORT IS FINISHED LATER. Before
# it removes anything the sweeper makes the record young again and renames the
# copy, still inside the template. The rename is what arbitrates: of sweepers
# that meet over a copy one wins it, and only the winner counts it. The young
# record is what keeps a claimed copy from being claimed a second time while it
# is being removed, and the record is the last thing unlinked, so that a
# removal that dies part-way leaves a copy the rule above still reads — 90
# minutes later, when the record is old again.
#
# WHAT STILL LEAKS, neither measured: a run killed between `new`'s mktemp and
# its write of the record leaves one empty directory, and a sweeper killed
# between unlinking the record and the rmdir after it leaves another. Both are
# unmarked, which the second rule protects.
#
# THE PROC ROOT. `--test-proc-root <dir>` makes both halves read `<dir>/<pid>/stat`
# where they would read /proc's, so that the rows can stand an EMPTY directory
# in for a host with no /proc, or write a stat file no live process has, on a
# runner that cannot mask /proc (bd gqlc-qbah: CI's cannot, and the rows that
# needed it said SKIP in the one job that gates a merge). Told the wrong
# directory a sweeper calls every owner dead, so:
#
#   - it is an ARGUMENT, and nothing is read from the environment. What a real
#     run inherits is its environment; its argument list is the two words the
#     recipe spells. A row runs the real recipe with an empty directory
#     exported as GQLC_GOLDENS_UNUSED_PROC_ROOT, the name a shortcut would give
#     it, and requires the sweep to have read /proc all the same;
#   - <dir> must be an absolute path to a directory that is there, or rc=2;
#   - it is said on stderr, in one line, every time it is in effect.
#
# NOT RUN ANYWHERE: a host that really has no /proc (darwin). What the rows run
# is this host told to read an empty directory. `mv -T`, `find -mmin` and
# `touch -h` are GNU's as well; a sweep whose mv refuses -T removes nothing.
set -euo pipefail

prefix="gqlc-goldens-unused"
record=".gqlc-goldens-unused-owner"
stale_minutes=90
proc_root="/proc"

# A decimal number of at most ${1} digits, written the way the kernel writes one.
canonical() {
    case "${2}" in "" | 0?* | *[!0-9]*) return 1 ;; esac
    [ "${#2}" -le "${1}" ]
}

# The tick <pid> was started at. Returns 1 if there is no stat file to read,
# which is what a pid nobody holds looks like, and 2 if there is one and no tick
# in it. The command name is field 2 and may itself hold spaces and parentheses,
# so the count starts after the LAST ") ": field 3 is then $1, and field 22 is
# $20.
proc_start() {
    local stat
    stat="$(cat "${proc_root}/${1}/stat" 2>/dev/null)" || return 1
    stat="${stat##*) }"
    # shellcheck disable=SC2086 # split into fields on purpose
    set -- ${stat}
    canonical 20 "${20:-}" || return 2
    printf '%s\n' "${20}"
}
is_pid() { canonical 7 "${1}" && [ "${1}" != 0 ]; }

absolute() {
    case "${1}" in
        /*) ;;
        *)
            echo "error: the scratch root '${1}' is not an absolute path, so it is not swept and nothing is made under it (bd gqlc-7hyt)." >&2
            exit 2
            ;;
    esac
}

owner_alive() {
    local pid="" start="" now unread=0
    read -r pid start <"${1}" 2>/dev/null || true
    is_pid "${pid}" || return 0
    canonical 20 "${start}" || return 0
    now="$(proc_start "${pid}")" || unread=$?
    [ "${unread}" -ne 2 ] || return 0
    [ "${unread}" -eq 0 ] && [ "${now}" = "${start}" ]
}

new() {
    local root="${1}" pid="${2}" start dir
    absolute "${root}"
    if ! is_pid "${pid}"; then
        echo "error: '${pid}' is not a pid, so no copy is made whose record would name it (bd gqlc-7hyt)." >&2
        exit 2
    fi
    start="$(proc_start "${pid}")" || start="-"
    dir="$(mktemp -d "${root}/${prefix}-XXXXXX")"
    printf '%s %s\n' "${pid}" "${start}" >"${dir}/${record}"
    printf '%s\n' "${dir}"
}

sweep() {
    local root="${1}" removed=0 freed=0 dir claimed inodes
    absolute "${root}"
    proc_start "$$" >/dev/null || return 0
    for dir in "${root}/${prefix}"-*; do
        [ ! -L "${dir}" ] || continue
        [ -n "$(find "${dir}/${record}" -maxdepth 0 -mmin "+${stale_minutes}" 2>/dev/null)" ] || continue
        ! owner_alive "${dir}/${record}" || continue
        claimed="${dir}.reap$$"
        touch -c -h "${dir}/${record}" 2>/dev/null || continue
        mv -T -- "${dir}" "${claimed}" 2>/dev/null || continue
        inodes="$(find "${claimed}" 2>/dev/null | wc -l)" || true
        find "${claimed}" -mindepth 1 -maxdepth 1 ! -name "${record}" -exec rm -rf -- {} + 2>/dev/null || true
        [ -z "$(find "${claimed}" -mindepth 1 -maxdepth 1 ! -name "${record}" 2>/dev/null)" ] || continue
        rm -rf -- "${claimed}" 2>/dev/null || true
        [ ! -e "${claimed}" ] || continue
        removed=$((removed + 1))
        freed=$((freed + inodes))
    done
    [ "${removed}" -eq 0 ] ||
        echo "goldens-unused scratch: removed ${removed} stale copies a killed run left under ${root}, freeing ${freed} inodes (bd gqlc-7hyt)"
}

if [ "${1:-}" = --test-proc-root ]; then
    proc_root="${2:-}"
    case "${proc_root}" in /*) ;; *) proc_root="" ;; esac
    if [ ! -d "${proc_root}" ]; then
        echo "error: --test-proc-root '${2:-}' is not an absolute path to a directory, so nothing is read from it, swept or made (bd gqlc-qbah)." >&2
        exit 2
    fi
    echo "goldens-unused scratch: TEST ONLY: process start ticks are read from ${proc_root} and not from /proc (bd gqlc-qbah)" >&2
    shift 2
fi

usage="usage: goldens-unused-scratch.sh new <root> <owner-pid> | sweep <root>"
case "${1:-}" in
    new) new "${2:-}" "${3:-}" ;;
    sweep) sweep "${2:-}" ;;
    *)
        echo "${usage}" >&2
        exit 2
        ;;
esac
