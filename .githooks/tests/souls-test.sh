#!/usr/bin/env bash
# Tests for the two-part shape ADR 0003 change 5 gave the seat souls, which
# until now was recorded only as prose in that ADR:
#
#   A. Per-class `## Your duties` is BYTE-IDENTICAL across every seat of the
#      class. Specialisations were repealed; the duties section is the union of
#      what the class collectively knew, so a duty edited into one seat and not
#      its siblings is a specialisation reintroduced by accident.
#   B. `## Who you are` and `## How you work` are PERSONALITY and per-seat. The
#      cheap way to satisfy A is to paste a rule into every soul, and the way
#      that goes wrong is homogenising the prose the owner cares most about.
#      Two seats of a class sharing either section is that failure, visible.
#
# A alone is satisfied by sixteen identical souls. B is what says they must
# still be sixteen people. Neither is derivable from the other.
#
# Run via: just test-hooks
set -u

unset "${!GIT_@}"

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SEATS="$ROOT/kingdom/seats"

pass=0
fail=0
ok() {
    pass=$((pass + 1))
    printf 'ok   - %s\n' "$1"
}
bad() {
    fail=$((fail + 1))
    printf 'FAIL - %s\n' "$1"
}

# The section body between a `## <heading>` line and the next `## ` line, with
# the heading itself excluded so that renaming a heading is a separate failure
# from changing what is under it.
section() {
    awk -v want="## $2" '
        $0 == want { f = 1; next }
        /^## / { f = 0 }
        f { print }
    ' "$1"
}

seats=()
for d in "$SEATS"/*/; do
    [ -f "$d/soul.md" ] || continue
    seats+=("$(basename "$d")")
done

if [ "${#seats[@]}" -lt 2 ]; then
    bad "souls: found ${#seats[@]} seat(s) under kingdom/seats — nothing to compare"
    printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
    exit 1
fi
ok "souls: ${#seats[@]} seats found under kingdom/seats"

# Class is the word after the em dash on the H1: `# Միհր — Դատաւոր`. Read from
# the soul rather than from kingdom.toml deliberately: this suite is about what
# the documents say, and a soul whose own title does not name a class is a
# defect these comparisons would otherwise skip in silence.
declare -A class_of=()
for s in "${seats[@]}"; do
    cls="$(head -1 "$SEATS/$s/soul.md" | sed -n 's/^# .* — \(.*\)$/\1/p')"
    if [ -z "$cls" ]; then
        bad "souls: $s's H1 names no class (expected '# <name> — <class>')"
        continue
    fi
    class_of[$s]="$cls"
done
[ "${#class_of[@]}" -eq "${#seats[@]}" ] &&
    ok "souls: every seat's H1 names a class"

# --- Every soul carries the personality headings ----------------------------
for s in "${seats[@]}"; do
    missing=""
    for h in "Who you are" "How you work"; do
        grep -qxF "## $h" "$SEATS/$s/soul.md" || missing="$missing '$h'"
    done
    if [ -n "$missing" ]; then
        bad "souls: $s is missing heading(s):$missing"
    else
        ok "souls: $s carries 'Who you are' and 'How you work'"
    fi
done

classes="$(printf '%s\n' "${class_of[@]}" | sort -u)"

# --- A. duties are byte-identical within a class ----------------------------
while IFS= read -r cls; do
    [ -n "$cls" ] || continue
    members=()
    for s in "${seats[@]}"; do
        [ "${class_of[$s]:-}" = "$cls" ] && members+=("$s")
    done
    # A class of one has nothing to agree with; the two-part shape is a claim
    # about siblings. Said out loud so a class silently shrinking to one seat
    # does not read as a passing comparison.
    if [ "${#members[@]}" -lt 2 ]; then
        ok "duties: $cls has one seat (${members[0]}) — identity is vacuous, not checked"
        continue
    fi
    ref="${members[0]}"
    ref_body="$(section "$SEATS/$ref/soul.md" "Your duties")"
    if [ -z "$ref_body" ]; then
        bad "duties: $cls seat $ref has no '## Your duties' body to compare against"
        continue
    fi
    ref_hash="$(printf '%s' "$ref_body" | sha256sum | cut -c1-12)"
    differs=""
    for s in "${members[@]:1}"; do
        body="$(section "$SEATS/$s/soul.md" "Your duties")"
        [ "$body" = "$ref_body" ] || differs="$differs $s"
    done
    if [ -n "$differs" ]; then
        bad "duties: $cls seats differ from $ref ($ref_hash):$differs — ADR 0003 change 5 requires byte-identical duties across a class"
    else
        ok "duties: $cls — ${#members[@]} seats, all byte-identical ($ref_hash)"
    fi
done <<<"$classes"

# --- B. personality is per-seat ---------------------------------------------
while IFS= read -r cls; do
    [ -n "$cls" ] || continue
    members=()
    for s in "${seats[@]}"; do
        [ "${class_of[$s]:-}" = "$cls" ] && members+=("$s")
    done
    [ "${#members[@]}" -ge 2 ] || continue
    for h in "Who you are" "How you work"; do
        clashes=""
        n="${#members[@]}"
        for ((i = 0; i < n; i++)); do
            for ((j = i + 1; j < n; j++)); do
                a="$(section "$SEATS/${members[i]}/soul.md" "$h")"
                b="$(section "$SEATS/${members[j]}/soul.md" "$h")"
                [ "$a" = "$b" ] && clashes="$clashes ${members[i]}=${members[j]}"
            done
        done
        if [ -n "$clashes" ]; then
            bad "personality: $cls '$h' is shared verbatim by:$clashes — this section is per-seat and must not be homogenised"
        else
            ok "personality: $cls '$h' differs across all ${#members[@]} seats"
        fi
    done
done <<<"$classes"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
