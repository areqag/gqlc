#!/usr/bin/env bash
# The identity rules, in ONE place, because there are now two enforcers.
#
# .githooks/commit-msg applies them locally at commit time; that gate is
# advisory by construction — `git commit --no-verify` skips it, a clone that
# never ran `just init` has no core.hooksPath at all, and in this repo
# core.hooksPath has been observed rewritten to /dev/null in the SHARED config
# by worktree-isolated agent spawns, with no writer anyone can name and no
# tripwire (bd gqlc-r41). During such a window every rule in .githooks is a
# no-op and nothing says so.
#
# .github/scripts/check-pr-authors.sh applies the SAME rules in Actions, over
# the commits a pull request proposes, where the configuration is not the
# developer's. GitHub's squash merge is what makes the PR the only place to
# look: the squash commit is authored by whoever merges and committed by
# GitHub, so the original identities exist server-side only up to the merge.
#
# The rules are defined here and nowhere else on purpose (bd gqlc-gy3q). A
# second copy diverges the first time either side is edited, and it diverges
# silently in the direction that matters — the CI copy going stale means master
# accepts what commit-msg refuses. That is not hypothetical here: while this
# bead was open, two PRs (#1194, #1195) carried independent copies of this rule
# that had ALREADY disagreed on four inputs before any CI check existed.
#
# Sourced, not executed: this file defines functions and sets no traps.

# The rule is the DOMAIN, not a list of addresses. It used to be a list, and it
# read `test@example.invalid|*@example.com|*@example.org|root@localhost`:
# `fixture <fixture@example.invalid>` was one character class away from every
# entry, cleared the shape check below (it has an @, and its domain has a dot),
# and reached two citizens' commits (gqlc-7iea). RFC 2606 and RFC 6761 reserve
# these names so that nothing under them can ever be delivered to a person, so
# an address in one is a fixture's or a placeholder's by construction.
# NOT in this list, though both are special-use names too: `.local` (RFC 6762)
# and `.onion` (RFC 7686). The rule here is deliverability, not specialness —
# an address is refused because nothing sent there can reach a person. A .local
# address resolves on the local link and a .onion one over Tor, so both can
# reach somebody, and refusing them would refuse a real identity.
IMPLAUSIBLE_RESERVED_DOMAINS=(invalid test example localhost example.com example.net example.org)

# implausible_email_reason <email>
#
# Prints a one-line reason and returns 1 when the address cannot belong to a
# person. Prints nothing and returns 0 otherwise.
#
# A domain that merely CONTAINS a reserved name is a real domain and must be
# accepted: ops@example-corp.io and ops@invalid-arguments.io are the falsifiers
# this function is held to (.github/scripts/tests/ci-identity-gate-test.sh).
implausible_email_reason() {
    local email="$1"
    # Domains are case-insensitive; FIXTURE@EXAMPLE.INVALID is the same address.
    local domain="${email##*@}"
    domain="${domain,,}"
    # A trailing dot is the FQDN root: `example.invalid.` IS `example.invalid`.
    # As text it is neither equal to a reserved name nor a suffix ending in one,
    # so both arms below miss it — and the shape check cannot catch what they
    # miss, because the dot it demands is exactly the dot doing the evading.
    # ALL of them, not one: strip a single dot from `example.invalid..` and the
    # bypass is still there.
    while [ "$domain" != "${domain%.}" ]; do domain="${domain%.}"; done

    local reserved
    for reserved in "${IMPLAUSIBLE_RESERVED_DOMAINS[@]}"; do
        # The domain itself, or any subdomain of it: a@example.invalid and
        # a@mail.example.invalid are both unreachable.
        if [ "$domain" = "$reserved" ] || [ "$domain" != "${domain%".$reserved"}" ]; then
            printf 'its domain is reserved (RFC 2606 / RFC 6761), so no mail sent there reaches anyone'
            return 1
        fi
    done

    # Shape: must contain @ and the domain part must contain a dot. This is what
    # refuses an empty address, which has no domain for the loop above to match.
    if [ "${email#*@}" = "$email" ] || [ "${domain#*.}" = "$domain" ]; then
        printf 'it is not of the form user@host.tld'
        return 1
    fi
    return 0
}

# ai_attribution_trailer  (commit message on stdin)
#
# Prints the offending Co-Authored-By line and returns 1 when the message
# attributes authorship to Claude / Anthropic. See CLAUDE.md, "AI attribution".
#
# Case is folded on the whole line before matching, rather than with awk's
# IGNORECASE: IGNORECASE is a gawk extension and ubuntu-latest runners carry
# mawk, where a `/^co-authored-by:/` pattern silently fails to see the
# `Co-Authored-By:` spelling git actually writes — a gate that is green because
# it matched nothing.
ai_attribution_trailer() {
    local offender
    # `git interpret-trailers --parse` normalises folded and interleaved
    # trailers into one line each; without it a value continued on the next
    # line hides the offending name from a line-oriented match.
    offender="$(
        git interpret-trailers --parse \
            | awk '{
                l = tolower($0)
                if (l ~ /^co-authored-by:[[:space:]]/ && (l ~ /claude/ || l ~ /@anthropic\.com/)) {
                    print; exit
                }
            }'
    )"
    if [ -n "$offender" ]; then
        printf '%s' "$offender"
        return 1
    fi
    return 0
}
