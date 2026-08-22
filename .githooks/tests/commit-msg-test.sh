#!/usr/bin/env bash
# Unit tests for .githooks/commit-msg — the AI-attribution guard and the
# author/committer identity guard.
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
# The hook reads `git var GIT_AUTHOR_IDENT`, which falls back to the ambient
# identity when the repo declares none — so without this the identity guard
# below would be judging whoever runs the suite. A machine configured with
# jane@example.com would redden the accept rows on untouched code.
git -C "$REPO" config user.name "Jane Doe"
git -C "$REPO" config user.email "jane@doe.dev"

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
# The other half of the hook (cd10da3e, gqlc-r41), which had no row at all
# until gqlc-7iea. It was written to stop a test fixture's identity reaching
# the history, and `fixture <fixture@example.invalid>` — the identity that in
# fact reached two citizens' commits — passed it: the denylist spelled one
# address in .invalid rather than the reserved TLD, and a fixture address
# clears the shape check below it, having both an @ and a dot in its domain.
#
# The identity is set per row through GIT_AUTHOR_* / GIT_COMMITTER_*, which is
# what `git var` reads first. Per row rather than in the repo's config so that
# author and committer can differ, which is the case the hook checks twice and
# no row here would otherwise separate.
run_identity_case() { # $1=name $2=expected(reject|accept) $3=author email [$4=committer email]
    local name="$1" expected="$2" author="$3" committer="${4:-$3}"
    local msg_file="$TMP/ident.$$"
    printf 'subject line\n' >"$msg_file"
    rm -f "$REPO/.git/MERGE_HEAD"

    local decision
    if (cd "$REPO" \
        && GIT_AUTHOR_NAME="Author" GIT_AUTHOR_EMAIL="$author" \
           GIT_COMMITTER_NAME="Committer" GIT_COMMITTER_EMAIL="$committer" \
           "$HOOK" "$msg_file") >/dev/null 2>&1; then
        decision=accept
    else
        decision=reject
    fi

    if [ "$decision" = "$expected" ]; then
        pass=$((pass + 1)); printf 'ok   - %s\n' "$name"
    else
        fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$name" "$expected" "$decision"
    fi

    rm -f "$msg_file"
}

# The address from the incident, and the one every suite in this directory
# spells its own fixtures with. Both must be refused by the same rule.
run_identity_case "author fixture@example.invalid"  reject "fixture@example.invalid"
run_identity_case "author t@t.invalid"              reject "t@t.invalid"
run_identity_case "author someone@example.test"     reject "someone@example.test"
run_identity_case "author dev@my.localhost"         reject "dev@my.localhost"
run_identity_case "author jane@example.com"         reject "jane@example.com"
run_identity_case "author root@localhost"           reject "root@localhost"
run_identity_case "author with no domain"           reject "nobody"
run_identity_case "author with dotless domain"      reject "nobody@host"
# `git var` answers `A <>` for an empty address rather than failing, so this
# reaches the guard rather than the skip. It is refused by the shape check
# below the reserved-domain loop, not by the loop: an empty address has no
# domain to match. The old denylist named "" explicitly and the shape check
# made that arm dead — measured, removing it fails no row here.
run_identity_case "author with empty address"       reject ""
# Two claims the rule's comment makes, each with a row rather than a promise:
# the match folds case, and it reaches subdomains of a reserved name.
run_identity_case "uppercased fixture address"      reject "FIXTURE@EXAMPLE.INVALID"
run_identity_case "subdomain of a reserved name"    reject "ci@mail.example.invalid"

# The FQDN root dot. `example.invalid.` and `example.invalid` are the SAME
# domain — the trailing dot only says the name is already absolute — but as
# text they are not equal and neither is a suffix of the other, so an
# unnormalised rule misses both of its arms and the shape check below waves the
# address through (it has an @, and its domain has a dot). Found by Միհր on
# PR #1195; it committed.
run_identity_case "fixture address, root dot"       reject "fixture@example.invalid."
# More than one trailing dot is not a legal name, but nothing validates that
# before this hook: stripping exactly ONE dot leaves `example.invalid.`, which
# is the bypass again. The rule strips them all, so this row is what stops the
# fix from being one character short of the defect it repairs.
run_identity_case "fixture address, two root dots"  reject "fixture@example.invalid.."
# Both normalisations at once, so neither can be removed while the other hides
# it: case-folding alone leaves the dot, dot-stripping alone leaves the case.
run_identity_case "uppercased address, root dot"    reject "FIXTURE@EXAMPLE.INVALID."
# A BARE reserved TLD reaches the reserved loop only once the dot is stripped.
# Without stripping, `invalid.` has a dot, so the shape check accepts it and
# the loop never matches — the one spelling where the trailing dot turns a
# refusal into an acceptance rather than merely evading one (cf. gqlc-76gk,
# which is about the dotless spelling being caught by the shape rule instead).
run_identity_case "bare reserved TLD, root dot"     reject "ci@invalid."

# The committer is checked separately from the author, and a rebase or an
# --amend is exactly how a fixture identity ends up on one and not the other.
run_identity_case "plausible author, fixture committer" reject "jane@doe.dev" "fixture@example.invalid"
run_identity_case "fixture author, plausible committer" reject "fixture@example.invalid" "jane@doe.dev"

# The falsifier for an over-broad rule: a citizen's own address, and one whose
# domain merely CONTAINS a reserved name, still commit.
run_identity_case "citizen address"                 accept "jane@doe.dev"
run_identity_case "address on proton.me"            accept "antranig.yeretzian@proton.me"
run_identity_case "domain containing 'invalid'"     accept "ops@invalid-arguments.io"
run_identity_case "domain containing 'example'"     accept "ops@example-corp.io"
# The falsifier for the dot-stripping specifically: a real address written as
# an absolute name is still a real address, so normalising the dot must not
# make the rule refuse anyone. Without this row, "strip trailing dots" and
# "refuse anything ending in a dot" both pass the reject rows above.
run_identity_case "citizen address, root dot"       accept "jane@doe.dev."

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
