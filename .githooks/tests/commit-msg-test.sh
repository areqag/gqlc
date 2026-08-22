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

# --- author / committer identity --------------------------------------------
# Until gqlc-s989 this half of the hook had NO rows at all. Every case above
# inherits the machine's real global identity, so all of them satisfy the
# identity check incidentally, and a hole in its denylist could not redden
# anything. It did not: `test@example.invalid` was spelled out while only
# `.com` and `.org` were globbed, so `fixture@example.invalid` committed
# cleanly, and one did (42191059, a real feature commit).
#
# Identity comes from GIT_AUTHOR_EMAIL / GIT_COMMITTER_EMAIL here because
# that is what `git var GIT_{AUTHOR,COMMITTER}_IDENT` reads first, and it
# leaves the fixture repo's config untouched.
# The expectation is not accept/reject but WHICH GUARD rejected, spelled
# `denylist:<label>` or `shape:<label>`. Both guards reject, so a plain
# accept/reject row cannot tell them apart — and under that coarser
# expectation every dotless reserved name (`ci@invalid`, `root@localhost`)
# is carried by the shape check's dot requirement while appearing to
# witness its denylist arm. Deleting `*@invalid` or the `.localhost` arm
# then changes nothing observable and the suite stays green. Mutation
# screen, 2026-08-22: that is 2 of 8 survivors; naming the guard kills
# both, and the message the committer reads ("reserved by RFC 2606" vs
# "Implausible") is the thing that differs, so it is worth asserting anyway.
run_ident_case() { # $1=name $2=expected $3=author-email [$4=committer-email]
    local name="$1" expected="$2" email="$3" cemail="${4:-$3}"
    local msg_file="$TMP/msg.$$"
    printf 'a subject line\n' >"$msg_file"
    rm -f "$REPO/.git/MERGE_HEAD"

    local err decision
    if err="$(cd "$REPO" && env \
            GIT_AUTHOR_NAME=fixture    GIT_AUTHOR_EMAIL="$email" \
            GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL="$cemail" \
            "$HOOK" "$msg_file" 2>&1 >/dev/null)"; then
        decision=accept
    else
        case "$err" in
            *"Bogus author identity"*)       decision=denylist:author ;;
            *"Bogus committer identity"*)    decision=denylist:committer ;;
            *"Implausible author email"*)    decision=shape:author ;;
            *"Implausible committer email"*) decision=shape:committer ;;
            *) decision="reject:unrecognised" ;;
        esac
    fi

    if [ "$decision" = "$expected" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$decision"
    fi
    rm -f "$msg_file"
}

# RFC 2606 reserves example.{com,net,org} and the .invalid / .test / .example
# TLDs; RFC 6761 adds .localhost. One row per reserved name, and each with a
# local-part that is NOT 'test', because a denylist that enumerates local-parts
# is the exact defect this section exists to pin.
run_ident_case "the local-part that was hardcoded"   denylist:author test@example.invalid
run_ident_case "the address that actually leaked"    denylist:author fixture@example.invalid
run_ident_case "another local-part at example.invalid" denylist:author stub@example.invalid
run_ident_case "any host under .invalid"             denylist:author ci@build.invalid
run_ident_case "the bare .invalid TLD"               denylist:author ci@invalid
run_ident_case "example.com"                         denylist:author someone@example.com
run_ident_case "example.org"                         denylist:author someone@example.org
run_ident_case "example.net"                         denylist:author someone@example.net
run_ident_case "a host under .test"                  denylist:author runner@ci.test
run_ident_case "a host under .example"               denylist:author runner@ci.example
run_ident_case "root@localhost"                      denylist:author root@localhost
run_ident_case "any user at localhost"               denylist:author build@localhost
# The empty-email row below reaches the `""` denylist arm only if git hands an
# empty GIT_AUTHOR_EMAIL to `git var` rather than treating it as unset and
# falling back to config. Measured true on git 2.55; NOT promised by git's
# docs. Were it to change, the row would quietly exercise whatever real
# address the machine is configured with, still print `ok`, and leave the
# `""` arm unwitnessed — the same read-the-machine defect this section exists
# to remove. So assert the precondition by name instead of assuming it.
ident_email="$(cd "$REPO" && env GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL= \
    git var GIT_AUTHOR_IDENT 2>/dev/null | sed 's/.*<\(.*\)>.*/\1/')"
if [ -z "$ident_email" ]; then
    pass=$((pass + 1)); printf 'ok   - precondition: git passes an empty author email through\n'
else
    fail=$((fail + 1)); printf 'FAIL - precondition: git passes an empty author email through (git var gave <%s>, so the empty-email row below tests that address and not the "" arm)\n' "$ident_email"
fi

run_ident_case "an empty email"                      denylist:author ""

# The shape check is the second guard and the only one covering addresses no
# denylist can enumerate. Each row below is rejected by shape and by nothing
# else, so each pins one clause of `[[ $email != *@* ]] || [[ $domain != *.* ]]`.
# `plainaddress.com` is the one that needs the `*@*` clause specifically: with
# no @, `${email##*@}` is the whole string, so the dot clause is satisfied and
# only the @ clause is left to reject it.
run_ident_case "no @ and no dot"                     shape:author nobody
run_ident_case "no @ but a dotted domain"            shape:author plainaddress.com
run_ident_case "an @ but an undotted domain"         shape:author user@nodot

# Author and committer are separate calls and diverge in real life — rebases,
# `--author`, and patch application all set one without the other. With the
# same address in both slots the author call rejects first, so no row above can
# tell whether the committer is checked at all.
run_ident_case "bogus committer, real author"        denylist:committer \
    antranig.yeretzian@proton.me fixture@example.invalid
run_ident_case "bogus author, real committer"        denylist:author \
    fixture@example.invalid antranig.yeretzian@proton.me

# The other direction matters as much: a gate that rejects real contributors
# stops the town rather than protecting it.
run_ident_case "a real address is accepted"          accept antranig.yeretzian@proton.me
run_ident_case "a noreply github address"            accept 12345+someone@users.noreply.github.com
run_ident_case "a plausible corporate address"       accept dev@invalidate.example-host.co.uk

# Over-breadth is the failure mode that stops the town rather than protecting
# it, and the globs are one stray `*` away from it. Each domain below CONTAINS
# a reserved label without BEING one — the label is a component inside a real
# TLD, which is registrable and ordinary. `*@*.invalid` widened to `*@*invalid*`
# would take all three.
run_ident_case "a real domain named invalid.com"     accept dev@invalid.com
run_ident_case "a real domain named test.com"        accept dev@test.com
run_ident_case "a reserved label mid-domain"         accept dev@localhost.example-corp.io

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
