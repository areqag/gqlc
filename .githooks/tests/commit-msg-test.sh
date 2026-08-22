#!/usr/bin/env bash
# Unit tests for .githooks/commit-msg (AI-attribution guard).
#
# Feeds crafted commit messages via a temp file (git's commit-msg contract)
# and asserts accept/reject. Verifies both the trailer patterns we want to
# block (Claude in value, @anthropic.com in email, any case for the key) and
# the escape hatches (no trailer, human co-author, merge in progress).
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR etc. leak in
# and would redirect the throwaway repo's git commands to the parent repo.
# Isolate completely.
unset "${!GIT_@}"

HOOK="$(cd "$(dirname "$0")/.." && pwd)/commit-msg"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# The hook shells out to `git rev-parse --verify MERGE_HEAD`; that call needs
# a working tree with an OID database. Give every test the same throwaway
# repo — the hook only cares about MERGE_HEAD existence, not history shape.
REPO="$TMP/repo"
git init -q -b master "$REPO"
git -C "$REPO" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m init

pass=0
fail=0

# $1=name $2=expected(reject|accept) $3=msg-body [$4=merge]
run_case() {
    local name="$1" expected="$2" msg="$3" merge="${4:-}"
    local msg_file="$TMP/msg.$$"
    printf '%s' "$msg" >"$msg_file"

    if [ "$merge" = "merge" ]; then
        # Fabricate MERGE_HEAD so the hook's early-exit branch fires.
        printf '%s\n' "$(git -C "$REPO" rev-parse HEAD)" >"$REPO/.git/MERGE_HEAD"
    else
        rm -f "$REPO/.git/MERGE_HEAD"
    fi

    local decision
    if (cd "$REPO" && "$HOOK" "$msg_file") >/dev/null 2>&1; then
        decision=accept
    else
        decision=reject
    fi

    if [ "$decision" = "$expected" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$decision"
    fi

    rm -f "$msg_file" "$REPO/.git/MERGE_HEAD"
}

# --- must reject ------------------------------------------------------------
run_case "canonical claude trailer (mixed-case key)" reject "\
subject line

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
"

run_case "canonical claude trailer (git's own case)" reject "\
subject line

Co-authored-by: Claude Opus 4.7 <noreply@anthropic.com>
"

run_case "non-claude name but anthropic.com email" reject "\
subject line

Co-Authored-By: Some Human <claude-bot@anthropic.com>
"

run_case "fully lowercase trailer" reject "\
subject line

co-authored-by: claude opus 4.7 <noreply@anthropic.com>
"

# --- must accept ------------------------------------------------------------
run_case "no trailer at all" accept "\
subject line

body paragraph, nothing else.
"

run_case "human co-author trailer" accept "\
subject line

Co-Authored-By: Jane Doe <jane@example.com>
"

# --- merge escape hatch -----------------------------------------------------
# Even if the merged message would otherwise trip the guard, the hook must
# bail early when MERGE_HEAD exists — commits on the source branch are the
# right place to catch this, not on the merger's machine.
run_case "merge commit with claude trailer" accept "\
Merge branch 'foo'

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
" merge

# --- author / committer identity (gqlc-n8n0) --------------------------------
# The hook's other half, which had no rows at all before this section. That is
# why an enumeration one name short stayed green: `fixture@example.invalid` is
# one word from the denylisted `test@example.invalid` and reached two seats'
# commits.
#
# The identity comes from GIT_AUTHOR_* / GIT_COMMITTER_*, which `git var`
# prefers over config. Each row varies ONE side and leaves the other
# deliberately good, so a rejection names which check fired instead of leaving
# the two indistinguishable.
GOOD="areqag@proton.me"

# $1=name $2=expected(reject|accept) $3=author-email $4=committer-email
# $5=optional substring the refusal must contain.
#
# $5 is not decoration. A dotless domain is refused by the SHAPE check whatever
# the reserved-name rule says, so a row that only asserts "rejected" cannot see
# whether the reserved-name rule fired at all: deleting `localhost` from that
# rule left such a row green (mutation M5). Naming the reason is what separates
# the two refusals.
run_identity_case() {
    local name="$1" expected="$2" author="$3" committer="$4" reason="${5:-}"
    local msg_file="$TMP/msg.$$"
    printf 'subject line\n' >"$msg_file"
    rm -f "$REPO/.git/MERGE_HEAD"

    local decision out
    if out=$( (cd "$REPO" \
        && GIT_AUTHOR_NAME=a GIT_AUTHOR_EMAIL="$author" \
           GIT_COMMITTER_NAME=c GIT_COMMITTER_EMAIL="$committer" \
           "$HOOK" "$msg_file") 2>&1 ); then
        decision=accept
    else
        decision=reject
    fi

    if [ "$decision" != "$expected" ]; then
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$decision"
    elif [ -n "$reason" ] && ! printf '%s' "$out" | grep -qF "$reason"; then
        fail=$((fail + 1)); printf 'FAIL - %s (rejected, but not for %s: %s)\n' "$name" "$reason" "$out"
    else
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    fi
    rm -f "$msg_file"
}

# RFC 2606 s.2 reserved TLDs, each with a local part that is NOT `test` — the
# point of the bead. A rule over the reserved TLD cannot be walked around by
# inventing a new local part; an enumeration of addresses can.
run_identity_case "author under .invalid, the address that reached master"  reject "fixture@example.invalid"  "$GOOD"
run_identity_case "author under .test"                                      reject "ci@build.test"            "$GOOD"
run_identity_case "author under .example"                                   reject "bot@corp.example"         "$GOOD"
run_identity_case "author under .localhost"                                 reject "dev@my.localhost"         "$GOOD"
run_identity_case "author at bare localhost"                                reject "root@localhost"           "$GOOD" "RFC 2606"

# The shape check, which had no row either. It is the reason the bare-localhost
# row above has to name RFC 2606: without that, both refusals look alike.
run_identity_case "author at a dotless domain"                              reject "dev@nodots"               "$GOOD" "Implausible"
# An empty domain, and a domain that is nothing but dots. Both name no host,
# so both are the shape check's business rather than RFC 2606's — asserting
# the reason is what holds that split in place.
run_identity_case "author with an empty domain"                             reject "dev@"                     "$GOOD" "Implausible"
run_identity_case "author at a domain of nothing but dots"                  reject "dev@..."                  "$GOOD" "Implausible"
# The domain is what follows the LAST @, so a second @ cannot smuggle a
# reserved name past as part of the local part.
run_identity_case "author whose address carries a second @"                 reject "x@y@localhost"            "$GOOD" "RFC 2606"

# RFC 2606 s.3 reserved second-level names. example.net was absent from the old
# denylist entirely, and subdomains of all three were reachable.
run_identity_case "author at example.net, absent from the old denylist"     reject "someone@example.net"      "$GOOD"
run_identity_case "author at a subdomain of example.com"                    reject "someone@mail.example.com" "$GOOD"
run_identity_case "author at a subdomain of example.net"                    reject "someone@mail.example.net" "$GOOD"
run_identity_case "author at a subdomain of example.org"                    reject "someone@mail.example.org" "$GOOD"

# Domains are case-insensitive; the guard must not be case-sensitive where DNS
# is not.
run_identity_case "author under .INVALID in capitals"                       reject "FIXTURE@EXAMPLE.INVALID"  "$GOOD"

# A trailing dot is the DNS root: the same name, spelled as an FQDN. Both of
# these were ACCEPTED before the strip — a one-character walk around a rule
# whose entire claim is that respelling cannot get past it.
run_identity_case "author under .invalid spelled as an FQDN"                reject "dev@example.invalid."     "$GOOD" "RFC 2606"
run_identity_case "author at localhost spelled as an FQDN"                  reject "dev@localhost."           "$GOOD" "RFC 2606"

# Stripping ONE dot is walked around by writing two, which is why the strip is
# a loop. Measured accepted while it was a single `${host%.}`.
run_identity_case "author under .invalid with a run of trailing dots"       reject "dev@example.invalid.."    "$GOOD" "RFC 2606"
# The strip must not change the answer for a real domain carrying the root
# dot: an FQDN is a legal spelling for a contributor too.
run_identity_case "a real domain spelled as an FQDN"                        accept "dev@notinvalid.com."      "$GOOD"

# The committer is checked as well as the author, and independently of it.
run_identity_case "committer under .invalid while the author is good"       reject "$GOOD" "fixture@example.invalid"

# Regressions: what the old enumeration did catch must stay caught.
run_identity_case "author at the old denylist's exact string"               reject "test@example.invalid"     "$GOOD"
run_identity_case "author at example.com"                                   reject "jane@example.com"         "$GOOD"
run_identity_case "author at example.org"                                   reject "jane@example.org"         "$GOOD"

# Must still accept. These are the over-match rows: a reserved name appearing
# as a SUBSTRING of a real domain is a real address, and rejecting it would
# lock a contributor out of the repo.
run_identity_case "an ordinary address"                                     accept "$GOOD"                    "$GOOD"
run_identity_case "reserved TLD as a label inside a real domain"            accept "dev@example.invalid.io"   "$GOOD"
run_identity_case "a real domain merely ending in the reserved word"        accept "dev@notinvalid.com"       "$GOOD"
run_identity_case "a real domain merely prefixed by the reserved name"      accept "dev@myexample.com"        "$GOOD"

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
