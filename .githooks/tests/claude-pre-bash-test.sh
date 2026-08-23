#!/usr/bin/env bash
# Unit tests for .githooks/claude-pre-bash (master guard + unpushed-close guard).
#
# Builds throwaway git repos under mktemp and feeds the hook the same JSON
# shape Claude Code sends on stdin, asserting allow/deny per case. The hook
# must resolve the branch at the command's EFFECTIVE target directory
# (leading `cd` chains, `git -C <path>`), not the hook's own pwd, and must
# not match `git commit` inside quoted literals or in the plain prose of a
# heredoc body — while
# still matching the `$(...)` and `` `...` `` forms substitution_bodies()
# extracts from an UNQUOTED heredoc body (bd gqlc-wa8c). That extraction is
# textual, so the set it matches is not the set the shell would run, and it
# misses on both sides: an escaped `\$(...)` is matched and denied although the
# shell leaves it inert, while a spelling it does not reach runs unseen.
# Both directions are disclosed in the hook's header and pinned by the contrast
# rows below.
#
# The `bd close` half (bd gqlc-90vt) asserts on the VERDICT NAME as well as on
# allow/deny, because THREE of its outcomes are allows: allow-no-reason (a close
# carrying no reason flag), allow-no-sha (a reason citing nothing this repo
# resolves to a commit) and allow-reachable (a cited sha a remote ref contains).
# Pinned by allow/deny alone the first two would be indistinguishable from the
# guard having silently stopped finding shas at all, and the third from either.
#
# Run via: just test-hooks
set -u

# When run under a git hook (pre-push via `just test`), GIT_DIR etc. leak in
# and redirect every git call — repo setup would re-init the parent repo and
# the hook under test would resolve the wrong branch. Isolate completely.
unset "${!GIT_@}"

# The bd-close rows import the hook as a module to read its verdict names.
# SourceFileLoader.exec_module caches bytecode NEXT TO THE SOURCE, i.e. inside
# .githooks/ — a test writing into the tree it is testing. `just lint-hooks`
# then fails on the .pyc, because a file with no recognised shebang is fatal
# there by design (bd gqlc-jhi2), so this test would redden a different gate.
# Belt and braces: the env var covers the interpreters this script spawns, and
# each loader sets the flag itself for a caller that does not inherit it.
export PYTHONDONTWRITEBYTECODE=1

HOOK="$(cd "$(dirname "$0")/.." && pwd)/claude-pre-bash"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mkrepo() { # $1=path $2=branch
  git init -q -b "$2" "$1"
  git -C "$1" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m init
}

MASTER_REPO="$TMP/on-master";   mkrepo "$MASTER_REPO" master
MAIN_REPO="$TMP/on-main";       mkrepo "$MAIN_REPO" main
FEATURE_REPO="$TMP/on-feature"; mkrepo "$FEATURE_REPO" feature-x
git init -q -b master "$TMP/unborn"   # repo with zero commits, HEAD unborn on master
DETACHED_REPO="$TMP/detached"; mkrepo "$DETACHED_REPO" master
git -C "$DETACHED_REPO" checkout -q --detach   # at master's tip, but not ON master

pass=0
fail=0
# Every row's name, in run order, for the membership pin at the end of the file.
ROWS=""
note_row() { ROWS="$ROWS$1
"; }

run_hook() { # $1=cwd-for-hook $2=command-string -> hook stdout
  (
    cd "$1" || exit 1
    python3 -c 'import json,sys; print(json.dumps({"tool_name":"Bash","tool_input":{"command":sys.argv[1]}}))' "$2" | "$HOOK" 2>/dev/null
  )
}

record() { # $1=name $2=expected $3=actual
  note_row "$1"
  if [ "$3" = "$2" ]; then
    pass=$((pass + 1)); printf 'ok   - %s\n' "$1"
  else
    fail=$((fail + 1)); printf 'FAIL - %s (expected %s, got %s)\n' "$1" "$2" "$3"
  fi
}

run_case() { # $1=name $2=expected(deny|allow) $3=cwd-for-hook $4=command-string
  local out decision
  out="$(run_hook "$3" "$4")"
  decision=allow
  if printf '%s' "$out" | grep -q '"permissionDecision": *"deny"'; then decision=deny; fi
  record "$1" "$2" "$decision"
}

# run_case reports a bare `deny`, which cannot say WHICH guard refused — a row
# that began denying for a neighbouring reason would still read as a pass.
# classify names the guard by its own sentence: deny-master is the master
# guard's refusal, deny-hooks the core.hooksPath one, and a deny carrying
# neither is deny-other rather than being folded into either. Matching the
# master sentence positively (not "not the hooks one") is what makes a third
# message fail here instead of passing as deny-master.
classify() { # $1=hook stdout -> deny-master|deny-hooks|deny-other|warn|silent|unrecognized(...)
  if printf '%s' "$1" | grep -q '"permissionDecision": *"deny"'; then
    if printf '%s' "$1" | grep -q 'Direct commits to .* are blocked'; then
      printf 'deny-master'
    elif printf '%s' "$1" | grep -q 'core\.hooksPath'; then
      printf 'deny-hooks'
    else
      printf 'deny-other'
    fi
  elif printf '%s' "$1" | grep -q '"systemMessage"'; then
    printf 'warn'
  elif [ -z "$1" ]; then
    printf 'silent'
  else
    printf 'unrecognized(%s)' "$1"
  fi
}

run_guard_case() { # $1=name $2=expected(classify verdict) $3=cwd $4=command
  record "$1" "$2" "$(classify "$(run_hook "$3" "$4")")"
}

# --- baseline behavior that must be preserved -------------------------------
run_case "bare form, cwd on master"            deny  "$MASTER_REPO"  'git commit -m x'
run_case "bare form, cwd on main"              deny  "$MAIN_REPO"    'git commit -m x'
run_case "bare form, cwd on feature branch"    allow "$FEATURE_REPO" 'git commit -m x'
run_case "commit-tree is not commit"           allow "$MASTER_REPO"  'git commit-tree HEAD^{tree} -m x'
run_case "unrelated git subcommand"            allow "$MASTER_REPO"  'git status'
run_case "second position in && chain"         deny  "$MASTER_REPO"  'go test ./... && git commit -m x'
run_case "env-assignment prefix"               deny  "$MASTER_REPO"  'GIT_AUTHOR_NAME=x git commit -m y'

# --- bug 1: false negative — `git -C <path>` form was never branch-checked --
run_case "-C to master repo from feature cwd"  deny  "$FEATURE_REPO" "git -C $MASTER_REPO commit -m x"
run_case "-C relative path to master repo"     deny  "$TMP"          'git -C on-master commit -m x'
run_case "-C to unborn master repo"            deny  "$TMP"          "git -C $TMP/unborn commit -m x"
run_case "cd to master repo from feature cwd"  deny  "$FEATURE_REPO" "cd $MASTER_REPO && git commit -m x"

# --- bug 2: false positive — target dir ignored, hook pwd checked instead ---
run_case "-C to feature repo from master cwd"  allow "$MASTER_REPO"  "git -C $FEATURE_REPO commit -m x"
run_case "cd to feature repo from master cwd"  allow "$MASTER_REPO"  "cd $FEATURE_REPO && git commit -m x"
run_case "cd then -C overrides tracked cwd"    allow "$MASTER_REPO"  "cd $TMP && git -C $FEATURE_REPO commit -m x"

# --- command substitution executes for real: must still be checked ----------
# The three below are the exception SC2016 exists to flag: the substitution is
# the fixture, and it has to reach the hook unevaluated or the case tests
# nothing. Taken one line at a time rather than file-wide, so a *stray*
# unexpanded expansion in a later case is still reported.
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_case "commit inside \$() on master cwd"    deny  "$MASTER_REPO"  'X="$(git commit -m x)" && echo "$X"'
# shellcheck disable=SC2016 # ditto
run_case "commit inside \$() on feature cwd"   allow "$FEATURE_REPO" 'X="$(git commit -m x)" && echo "$X"'
# shellcheck disable=SC2016 # ditto
run_case "commit inside backticks on master"   deny  "$MASTER_REPO"  'X=`git commit -m x`'
run_case "-c option consumes its arg"          deny  "$MASTER_REPO"  'git -c user.email=x@x commit -m y'

# --- newline-separated commands (no && between them) must still be seen -----
run_case "newline-separated cd then commit"    deny  "$FEATURE_REPO" "$(printf 'cd %s\ngit commit -m x' "$MASTER_REPO")"
run_case "multi-line quoted commit message"    deny  "$MASTER_REPO"  "$(printf 'git commit -m "l1\nl2"')"

# --- quoted << is prose, not a heredoc: must not swallow following lines ----
run_case "quoted heredoc-lookalike then commit" deny "$MASTER_REPO"  "$(printf 'echo "usage: cmd << EOF"\ngit commit -m x')"

# --- pushd tracks like cd ---------------------------------------------------
run_case "pushd to feature repo from master"   allow "$MASTER_REPO"  "pushd $FEATURE_REPO && git commit -m x"
run_case "pushd to master repo from feature"   deny  "$FEATURE_REPO" "pushd $MASTER_REPO && git commit -m x"

# --- branch resolution edges ------------------------------------------------
run_case "detached HEAD is not master"         allow "$DETACHED_REPO" 'git commit -m x'
run_case "unbalanced quote falls back, master" deny  "$MASTER_REPO"  "echo 'oops && git commit -m x"
run_case "unbalanced quote falls back, feature" allow "$FEATURE_REPO" "echo 'oops && git commit -m x"

# --- bug 3: false positive — quoted literals and heredoc prose matched ------
run_case "quoted literal in echo"              allow "$MASTER_REPO"  'echo "git commit"'
run_case "prose argument (bd-comment shape)"   allow "$MASTER_REPO"  'bd comment gqlc-xyz "next step: git commit the export"'
run_case "heredoc body with prose+apostrophe"  allow "$MASTER_REPO"  "$(printf 'cat <<%s > /dev/null\ndo not ever run git commit here, it won'\''t fly\nEOF\n' "'EOF'")"
# The limit of the row above it: quoting collapses a run into one token only
# while the run is one quoted string. Quoted word by word the tokens survive
# and the guard fires. Pinned deny because that is the behaviour today and it
# errs closed; widening the row above to cover it would be the fail-open half.
run_case "word-by-word quoting still denies"   deny  "$MASTER_REPO"  'echo "git" "commit" -m x'

# --- bd gqlc-wa8c: an unquoted heredoc EXPANDS its body ---------------------
# The delimiter's quoting decides whether the body is inert. Measured with a
# touch-marker fixture rather than reasoned: `$(touch MARK)` under <<EOF
# created the file, the identical line under <<'EOF' and <<"EOF" did not.
# In THIS block a deny row is normally a command that really runs and a silent
# row normally text that really does not; the silent rows further down, under
# "limits this leaves open", are a different thing — commands that really run
# and are missed anyway, which is what makes them limits. Where a row breaks
# that reading it says so above the row: the feature-cwd row really runs and is
# silent because of the branch rather than the spelling, and of the escaped
# rows at the end the two inert ones deny text the shell does not expand, while
# the third really runs like the rows above it.
# Rows assert deny-master, not a bare deny: the refusal has to be the master
# guard's, or a fixture failing for a neighbouring reason reads as a pass for
# the wrong cause.
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_guard_case "unquoted heredoc expands \$( )"  deny-master "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n$(git commit -m x)\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "unquoted heredoc expands backticks" deny-master "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n`git commit -m x`\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "unquoted heredoc, <<- tab-strip" deny-master "$MASTER_REPO" "$(printf 'cat <<-EOF > /dev/null\n\t$(git commit -m x)\n\tEOF\n')"
# The branch, not the shape, is what decides: same spelling on a feature cwd.
# shellcheck disable=SC2016 # ditto
run_guard_case "unquoted heredoc on feature cwd" silent "$FEATURE_REPO" "$(printf 'cat <<EOF > /dev/null\n$(git commit -m x)\nEOF\n')"
# ...and the effective-target resolution still reaches through the body.
# shellcheck disable=SC2016 # ditto
run_guard_case "-C to master from inside a heredoc" deny-master "$FEATURE_REPO" "$(printf 'cat <<EOF > /dev/null\n$(git -C %s commit -m x)\nEOF\n' "$MASTER_REPO")"
# A quoted delimiter expands nothing, so the same body is data both ways.
# shellcheck disable=SC2016 # ditto
run_guard_case "quoted heredoc \$( ) is prose"   silent "$MASTER_REPO" "$(printf 'cat <<%s > /dev/null\n$(git commit -m x)\nEOF\n' "'EOF'")"
# shellcheck disable=SC2016 # ditto
run_guard_case "dquoted heredoc \$( ) is prose"  silent "$MASTER_REPO" "$(printf 'cat <<"EOF" > /dev/null\n$(git commit -m x)\nEOF\n')"
# The half that rules out "strip only quoted heredocs": an unquoted delimiter
# expands substitutions but does not execute bare words, so prose in one stays
# an allow. Leaving the unquoted body in the token stream would deny this.
run_guard_case "unquoted heredoc bare prose"     silent "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\ndo not run git commit here\nEOF\n')"
# The escaped rows, which is where the deny/really-runs reading above stops
# holding. A backslash in front of the $ or the backtick suppresses the
# expansion, so the first two rows are PROSE and the hook denies them anyway:
# the scan keys on the literal `$(` and `` ` `` and never looks
# at what precedes them. Measured on bash 5.3.15 with a positive-content
# witness rather than an absent marker — the heredoc's target file still held
# the literal `$(touch MARK)` text and no marker appeared.
# The third row is why the fix is not "skip a $( with a backslash before it":
# `\\` is itself an escape yielding a literal backslash, so the substitution
# after it is LIVE and its deny is correct. Across 0 to 4 backslashes measured,
# an odd count suppressed the expansion and an even count did not. Mutating the
# extraction toward either candidate reddens rows in this block, and this third
# row is what separates them: it goes red when the skip keys on a backslash
# being present, and stays green when it keys on the run being odd, its own run
# being even. Which other rows move depends on whether the skip is wired into
# the `$(` arm of the scan alone or into its backtick arm too, since the `$(`
# arm cannot reach the backtick row — so what this block pins is that
# separation, not a redden-set. Measured by building both extractions out of
# tree and running this suite under each.
# shellcheck disable=SC2016 # ditto
run_guard_case "escaped \$( ) denies, inert"     deny-master "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n\\$(git commit -m x)\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "escaped backtick denies, inert"  deny-master "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n\\`git commit -m x\\`\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "escaped backslash, \$( ) is live" deny-master "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n\\\\$(git commit -m x)\nEOF\n')"

# --- a substitution body may hold parentheses (bd gqlc-90vt round 6) --------
# The `$( )` scan counts paren depth instead of excluding parens, so a body
# holding a balanced pair is read. One row per spelling: bare, where the
# tokenizer caught the call anyway; in a heredoc body, where nothing did; and
# inside DOUBLE QUOTES, where nothing did either — shlex collapses a quoted run
# to one opaque token, so the tokenizer is not a backstop there. That last
# spelling was witnessed writing a real commit onto master through this guard.
# shellcheck disable=SC2016 # ditto
run_guard_case "paren in a bare \$( ) body"      deny-master "$MASTER_REPO" 'echo $(git commit -m "(x)")'
# shellcheck disable=SC2016 # ditto
run_guard_case "paren in a heredoc \$( ) body"   deny-master "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n$(git commit -m "(x)")\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "paren in a quoted \$( ) body"    deny-master "$MASTER_REPO" 'echo "$(git commit -m "(x)")"'

# --- limits this leaves open: measured, disclosed, not fixed ----------------
# Inside a stripped heredoc body the substitution extractor is the only thing
# looking, because the surrounding prose must stay data. So a real command it
# does not match is not seen at all. Each limit gets THREE rows, because the
# bare spelling denying does not mean the heredoc is what suppresses it: the
# bare row denies with the tokenizer having split the text into words, and the
# DOUBLE-QUOTED row at top level, with no heredoc anywhere, is silent again —
# shlex collapses the whole run to one token there, so the tokenizer is a
# backstop for unquoted text and nothing else. The <<EOF.txt rows at the end
# are about the DELIMITER instead, so their partner shows the opposite — the
# same delimiter word, quoted, arms nothing and therefore denies.
# bash 5.3 funsub. All three rows assert what the HOOK decides about the text,
# so none depends on the shell running this file; the claim that the heredoc
# form executes was measured separately on bash 5.3.15.
# shellcheck disable=SC2016 # ditto
run_guard_case "funsub \${ ; } in heredoc"       silent "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n${ git commit -m x; }\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "funsub \${ ; } bare"             deny-master "$MASTER_REPO" 'echo ${ git commit -m x; }'
# shellcheck disable=SC2016 # ditto
run_guard_case "funsub \${ ; } double-quoted"    silent "$MASTER_REPO" 'echo "${ git commit -m x; }"'
# A limit with a different mechanism: the command shape is ordinary, but a
# backslash-newline splits the `$(` token itself. The shell rejoins the line
# and runs the substitution — measured by letting it commit for real rather
# than by reading a marker, and the fixture's HEAD moved — while the extractor
# needs a literal `$(` and finds none, so the heredoc row is silent.
# shellcheck disable=SC2016 # ditto
run_guard_case "cont-split \$( in heredoc"       silent "$MASTER_REPO" "$(printf 'cat <<EOF > /dev/null\n$\\\n(git commit -m x)\nEOF\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "cont-split \$( bare"             deny-master "$MASTER_REPO" "$(printf 'echo $\\\n(git commit -m x)')"
# shellcheck disable=SC2016 # ditto
run_guard_case "cont-split \$( double-quoted"    silent "$MASTER_REPO" "$(printf 'echo "$\\\n(git commit -m x)"')"
# A third mechanism, and the only one of the three where the extractor DOES
# match: a `case` pattern's `)` is an ordinary closing paren to the depth count,
# so the span ends at the pattern and the body handed on is `case x in a`. The
# commit is left in the surrounding text, double-quoted, where shlex collapses
# it — silent. The contrast row is the same line with the case removed, so what
# the pair pins is the case pattern doing it and not the quoting: parens that
# balance for the shell do not balance for this scan. The silent row alone
# cannot be killed — a mutation that breaks the scan makes it silent too, which
# is what it already asserts — so the contrast is the half that discriminates,
# and it is the half that reddens (measured: stop counting `)` at all and the
# contrast row goes silent, in a 150/14 run).
# shellcheck disable=SC2016 # ditto
run_guard_case "case pattern ) hides a later commit" silent "$MASTER_REPO" 'echo "$(case x in a) git commit -m y;; esac)"'
# shellcheck disable=SC2016 # ditto
run_guard_case "case pattern ) contrast, no case"    deny-master "$MASTER_REPO" 'echo "$(git commit -m y)"'
# HEREDOC_RE reads a \w+ delimiter, so <<EOF.txt arms on the prefix `EOF`; no
# line ever equals that, the body swallows the rest of the command, and the
# real commit after it is not seen. The quoted spelling of the same delimiter
# does not match the regex at all, so nothing is stripped and it denies.
run_guard_case "<<EOF.txt swallows what follows" silent "$MASTER_REPO" "$(printf 'cat <<EOF.txt > /dev/null\ndata\nEOF.txt\ngit commit -m x\n')"
run_guard_case "<<'EOF.txt' strips nothing"      deny-master "$MASTER_REPO" "$(printf 'cat <<%s > /dev/null\ndata\nEOF.txt\ngit commit -m x\n' "'EOF.txt'")"
# Which way <<EOF.txt fails depends on the body, not just the delimiter: put a
# bare `EOF` line in it and the swallow ends there, so the rest is tokenized
# and denies. Measured PROSE — the commit is still inside the real heredoc —
# so this row is a false deny, and it is pinned because it errs closed.
run_guard_case "<<EOF.txt, bare EOF ends swallow" deny-master "$MASTER_REPO" "$(printf 'cat <<EOF.txt > /dev/null\ndata\nEOF\ngit commit -m x\nEOF.txt\n')"
# The same bound the other way: <<\EOF is a QUOTED form the regex cannot see,
# so its body is tokenized and prose there denies though it expands nothing.
run_guard_case "<<\\EOF prose denies, fail-closed" deny-master "$MASTER_REPO" "$(printf 'cat <<\\EOF > /dev/null\ngit commit -m x\nEOF\n')"

# --- a heredoc inside "$( )" is a heredoc (bd gqlc-gtxl) --------------------
# `--body "$(cat <<'BODY' ... BODY)"` is how a PR body is written here, and the
# quote parity scan used to read that `<<` as being inside the double quotes and
# therefore as prose. Nothing armed, nothing was stripped, and the substitution
# extractor then pulled a `git commit` QUOTED IN THE PR BODY out of the
# unstripped text and refused it as a commit onto master. Measured twice in one
# night against two different agents, both of whom lost time and worked around
# it with --body-file.
# A command substitution reopens an UNQUOTED context, so under the fix the
# heredoc arms and its body is stripped. Each allow row is paired with the
# dangerous spelling it must not have loosened: the same shape with a delimiter
# the shell EXPANDS is still refused, in both its $( ) and backtick forms, and a
# real commit written after the heredoc is still refused.
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_guard_case "PR body quotes git commit in <<'BODY'" silent "$MASTER_REPO" \
  "$(printf 'gh pr create --title t --body "$(cat <<%s\nMutant 2 (`./usr/bin/git commit`, history not config): caught.\nBODY\n)"\n' "'BODY'")"
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_guard_case "same shape, <<BODY expands \$( )"      deny-master "$MASTER_REPO" \
  "$(printf 'gh pr create --title t --body "$(cat <<BODY\nx $(git commit -m sneaky) y\nBODY\n)"\n')"
# shellcheck disable=SC2016 # ditto
run_guard_case "same shape, <<BODY expands backticks"  deny-master "$MASTER_REPO" \
  "$(printf 'gh pr create --title t --body "$(cat <<BODY\nx `git commit -m sneaky` y\nBODY\n)"\n')"
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_guard_case "a real commit after the PR body"       deny-master "$MASTER_REPO" \
  "$(printf 'gh pr create --title t --body "$(cat <<%s\nprose\nBODY\n)" && git commit -m real\n' "'BODY'")"

# --- a substitution body is analyzed at the command's cwd, not the tool's ---
# The second half of bd gqlc-gtxl, and the one that made the first bite: the
# recursion handed every substitution body the directory the HOOK was invoked
# from. In this repo's mandated workflow that is the shared checkout, parked on
# master, while the command's own first clause is `cd <sibling worktree>` — so
# the refusal named a directory the command never touched.
# The fix walks only the cd chain that textually PRECEDES the span in its own
# scope, and the three deny rows are what stop that from becoming a hole: a cd
# that has not run yet, and a cd that ran in an earlier subshell, must not move
# the answer, or a real commit onto master would be resolved against some other
# repository and allowed.
# shellcheck disable=SC2016 # ditto
run_guard_case "\$( ) body honours a leading cd"      silent      "$MASTER_REPO"  "cd $FEATURE_REPO && echo \"\$(git commit -m x)\""
# shellcheck disable=SC2016 # ditto
run_guard_case "\$( ) body, leading cd onto master"   deny-master "$FEATURE_REPO" "cd $MASTER_REPO && echo \"\$(git commit -m x)\""
# shellcheck disable=SC2016 # ditto
run_guard_case "a cd AFTER the span does not apply"   deny-master "$MASTER_REPO"  "echo \"\$(git commit -m x)\" && cd $FEATURE_REPO"
# shellcheck disable=SC2016 # ditto
run_guard_case "a cd in a sibling span does not leak" deny-master "$MASTER_REPO"  "echo \"\$(cd $FEATURE_REPO)\" && echo \"\$(git commit -m x)\""
# shellcheck disable=SC2016 # ditto
run_guard_case "a cd INSIDE the span applies"         silent      "$MASTER_REPO"  "echo \"\$(cd $FEATURE_REPO && git commit -m x)\""
# The two halves composed, which is the shape that was measured: a PR body
# quoting `git commit`, written from a sibling worktree.
# shellcheck disable=SC2016 # the substitution is the hook's input, not this file's code
run_guard_case "PR body from a sibling worktree"      silent      "$MASTER_REPO" \
  "$(printf 'cd %s && gh pr create --title t --body "$(cat <<%s\nsee `git commit` in the note\nBODY\n)"\n' "$FEATURE_REPO" "'BODY'")"

# --- a redirect does not end a command (bd gqlc-vmlp) -----------------------
# The shell accepts a redirection anywhere in a simple command, including
# before the subcommand. The git walk stopped at the operator token, so it
# never reached `commit` — a bypass of this guard, not a nuisance. The fd
# prefix needs its own row because shlex splits it off as an ordinary word
# (`2>&1` comes back as `2`, `>&`, `1`), and the walk read that `2` as the
# subcommand and stopped there.
run_guard_case "redirect before the subcommand"       deny-master "$MASTER_REPO"  'git >/dev/null commit -m x'
run_guard_case "fd-prefixed redirect before it"       deny-master "$MASTER_REPO"  'git 2>&1 commit -m x'
run_guard_case "redirect before it, feature cwd"      silent      "$FEATURE_REPO" 'git >/dev/null commit -m x'
run_guard_case "-C survives a redirect"               deny-master "$FEATURE_REPO" "git -C $MASTER_REPO >/dev/null commit -m x"
# A terminator must still terminate, or the walk would read the next command's
# words as this one's: `commit` here belongs to no git invocation.
run_guard_case "a semicolon still ends the command"   silent      "$MASTER_REPO"  'git ; commit -m x'

# --- core.hooksPath drift (bd gqlc-nzwa) ------------------------------------
# Four config states, per bd gqlc-5fm. The fourth — a path that exists and is
# full of executable *.sample files — is the one that actually occurred twice,
# and the one an "is it set?" or "does the directory exist?" test passes over.
#
# Every fixture is a throwaway repo under mktemp and every `git config` here
# is pinned with `git -C`: an unpinned `git config` run from the wrong cwd is
# the documented root cause of drift occurrence #1 (bd gqlc-r41), so a test
# for this defect that could write the real repo's config would be the defect.
#
# The drift fixtures defined in this block sit on a feature branch so a deny
# cannot come from the master guard — the MASTER_DRIFT fixture further down is
# on master on purpose, to pin which guard wins, and its row says so. That is
# why these rows assert the classify() verdict (defined next to run_case,
# above) rather than a bare "deny".
run_drift_case() { # $1=name $2=expected(deny-hooks|deny-master|warn|silent) $3=cwd $4=command
  record "$1" "$2" "$(classify "$(run_hook "$3" "$4")")"
}

mkhookrepo() { # $1=path — repo on a feature branch shipping one live .githooks hook
  mkrepo "$1" drift-branch
  mkdir -p "$1/.githooks"
  printf '#!/bin/sh\nexit 0\n' > "$1/.githooks/commit-msg"
  chmod +x "$1/.githooks/commit-msg"
}

# state 1: the correct value, hooks present and executable
OK_REPO="$TMP/hooks-ok"; mkhookrepo "$OK_REPO"
git -C "$OK_REPO" config core.hooksPath .githooks

# state 2: unset — git silently falls back to $GIT_DIR/hooks
UNSET_REPO="$TMP/hooks-unset"; mkhookrepo "$UNSET_REPO"

# state 3: a path that is not this repo's hook tree
WRONG_REPO="$TMP/hooks-wrong"; mkhookrepo "$WRONG_REPO"
git -C "$WRONG_REPO" config core.hooksPath "$TMP/nowhere"

# state 4: THE RECORDED DRIFT — absolute path at <gitdir>/hooks, which exists
# and holds executable files, every one of them a *.sample git will not run.
SAMPLE_REPO="$TMP/hooks-samples"; mkhookrepo "$SAMPLE_REPO"
find "$SAMPLE_REPO/.git/hooks" -type f ! -name '*.sample' -delete
printf '#!/bin/sh\nexit 0\n' > "$SAMPLE_REPO/.git/hooks/pre-commit.sample"
chmod +x "$SAMPLE_REPO/.git/hooks/pre-commit.sample"
git -C "$SAMPLE_REPO" config core.hooksPath "$SAMPLE_REPO/.git/hooks"

# state 5: value correct, but .githooks/ holds nothing git will execute. The
# value check cannot see this one; it is what separates "points somewhere"
# from "points at hooks that exist".
DEAD_REPO="$TMP/hooks-dead"; mkrepo "$DEAD_REPO" drift-branch
mkdir -p "$DEAD_REPO/.githooks"
printf '#!/bin/sh\nexit 0\n' > "$DEAD_REPO/.githooks/pre-commit.sample"
chmod +x "$DEAD_REPO/.githooks/pre-commit.sample"
git -C "$DEAD_REPO" config core.hooksPath .githooks

# state 5b: the other half of "hooks that exist" — right name, right path, but
# the file is not executable, which git skips as silently as a wrong path does.
NOEXEC_REPO="$TMP/hooks-noexec"; mkrepo "$NOEXEC_REPO" drift-branch
mkdir -p "$NOEXEC_REPO/.githooks"
printf '#!/bin/sh\nexit 0\n' > "$NOEXEC_REPO/.githooks/commit-msg"
chmod -x "$NOEXEC_REPO/.githooks/commit-msg"
git -C "$NOEXEC_REPO" config core.hooksPath .githooks

# out of scope: a repo that ships no .githooks/ at all is indistinguishable
# from any unrelated repo on the machine, so the check stays silent there.
BARE_REPO="$TMP/hooks-none"; mkrepo "$BARE_REPO" drift-branch

run_drift_case "correct value, live hooks: commit"   silent     "$OK_REPO"     'git commit -m x'
run_drift_case "correct value, live hooks: push"     silent     "$OK_REPO"     'git push'
run_drift_case "correct value, live hooks: merge"    silent     "$OK_REPO"     'git merge --no-ff side'
run_drift_case "correct value, live hooks: pull"     silent     "$OK_REPO"     'git pull'
run_drift_case "correct value, live hooks: ls"       silent     "$OK_REPO"     'ls -la'
run_drift_case "unset: commit refused"               deny-hooks "$UNSET_REPO"  'git commit -m x'
run_drift_case "unset: push refused"                 deny-hooks "$UNSET_REPO"  'git push'
# A merge or pull is refused because post-merge runs bd-gh-sync. It used to be
# the only thing a drifted merge lost: commit-msg fires on a merge, but its
# MERGE_HEAD guard exited 0 ahead of both the identity checks and the
# Co-Authored-By scan, so a merge commit was unscreened whether the hooks were
# healthy or dead (measured rc=0 with hooksPath correct, against a plain-commit
# control rejected rc=1). That was bd gqlc-7y7e and it is fixed — the guard is
# split, the trailer scan runs on a merge message — so a drifted merge now loses
# the AI-attribution gate too. Cited by construct rather than by the line range
# this comment used to carry, which had already gone stale unnoticed
# (bd gqlc-m83s).
# Other shapes fire less: `pull --rebase` on the row below fired post-merge
# while the branch was still fast-forwardable and none of the four once it had
# diverged. It is refused anyway because HOOK_GATED keys on the subcommand,
# which is all this hook can see before the fact — and the subcommand does not
# determine the shape. revert, cherry-pick (with and without -e), rebase and am
# fired none of the four in the same measurement, so they stay outside
# HOOK_GATED; the rows in this file pin that boundary for the subcommands they
# name, and only for those.
run_drift_case "unset: merge refused"                deny-hooks "$UNSET_REPO"  'git merge --no-ff side'
run_drift_case "unset: pull refused"                 deny-hooks "$UNSET_REPO"  'git pull --rebase'
run_drift_case "unset: revert only warns"            warn       "$UNSET_REPO"  'git revert --no-edit HEAD'
run_drift_case "unset: cherry-pick only warns"       warn       "$UNSET_REPO"  'git cherry-pick HEAD'
run_drift_case "unset: rebase only warns"            warn       "$UNSET_REPO"  'git rebase origin/master'
run_drift_case "unset: am only warns"                warn       "$UNSET_REPO"  'git am /tmp/x.patch'
# stash, tag and fetch were measured firing none of the four as well, so they
# take the non-gated path, which warns here because the cwd is drifted. These
# rows exist because without them, adding all three to HOOK_GATED turned no row
# red: membership is pinned only for the subcommands some row names.
run_drift_case "unset: stash only warns"             warn       "$UNSET_REPO"  'git stash push -u'
run_drift_case "unset: tag only warns"               warn       "$UNSET_REPO"  'git tag -a v1 -m x'
run_drift_case "unset: fetch only warns"             warn       "$UNSET_REPO"  'git fetch'
run_drift_case "unset: innocuous command warns"      warn       "$UNSET_REPO"  'ls -la'
run_drift_case "wrong path: commit refused"          deny-hooks "$WRONG_REPO"  'git commit -m x'
run_drift_case "wrong path: push refused"            deny-hooks "$WRONG_REPO"  'git push'
run_drift_case "wrong path: innocuous command warns" warn       "$WRONG_REPO"  'ls -la'
run_drift_case "sample-only dir: commit refused"     deny-hooks "$SAMPLE_REPO" 'git commit -m x'
run_drift_case "sample-only dir: push refused"       deny-hooks "$SAMPLE_REPO" 'git push'
run_drift_case "sample-only dir: innocuous warns"    warn       "$SAMPLE_REPO" 'ls -la'
run_drift_case "value ok but no runnable hook"       deny-hooks "$DEAD_REPO"   'git commit -m x'
run_drift_case "value ok but hook not executable"    deny-hooks "$NOEXEC_REPO" 'git commit -m x'
# BARE_REPO is a plain `git init`, so its core.hooksPath is UNSET — drift by the
# value rule the rows above use — and it is still silent, because hooks_drift()
# reads the config only for repos that ship .githooks/. So "drifted" means both
# conditions, which is why CONTRIBUTING.md defines the word before using it.
run_drift_case "no .githooks/ in repo: silent"       silent     "$BARE_REPO"   'git commit -m x'
run_drift_case "non-repo cwd never fires"            silent     "$TMP"         'git commit -m x'

# --- what the refusal SAYS, not just that it refuses (bd gqlc-tr3c) ---------
# The message used to end "commit-msg, pre-commit, pre-push and post-merge are
# all dead" — a four-way universal asserted at the moment of refusal, and false
# in exactly the fail-closed case two blocks down, where core.hooksPath holds an
# absolute spelling of a directory whose hooks are live and firing. A human
# being refused is then told not to bother checking a hook that is in fact
# running, and debugs the wrong thing.
# Two rows, because either half alone is satisfiable by a bad fix: the first
# would pass on a message stripped to nothing, the second on one that kept the
# false list. A SHORTER list of hook names is the same defect with a smaller
# number, so no hook name may appear at all — that is what the alternation
# below asserts, rather than the phrase "are all dead".
DRIFT_MSG="$(run_hook "$UNSET_REPO" 'git commit -m x')"
record "the drift refusal names no hook as dead" "clean" \
  "$(printf '%s' "$DRIFT_MSG" | grep -qE 'commit-msg|pre-commit|pre-push|post-merge|all dead' \
     && echo CLAIMS || echo clean)"
record "the drift refusal still says what and how to repair" "explains" \
  "$(printf '%s' "$DRIFT_MSG" | grep -q 'core\.hooksPath' \
     && printf '%s' "$DRIFT_MSG" | grep -q 'Repair:' && echo explains || echo THIN)"

# --- drift carried by ENV rather than by the config file (bd gqlc-o13d) ------
#
# `git -c core.hooksPath=X ...` reaches every descendant through GIT_CONFIG_*,
# so a repo whose .git/config still says '.githooks' can run with hooks dead and
# leave no writer for anyone to find. Every row below is anchored to what git
# ACTUALLY did with the hook under that env, measured on git 2.55.0 against a
# commit-msg that exits 1 — not to what the docs say the variable means.
#
#   GIT_CONFIG_PARAMETERS      hook did NOT run, commit landed   -> must fire
#   GIT_CONFIG_COUNT/KEY/VALUE hook did NOT run, commit landed   -> must fire
#   GIT_CONFIG (no underscore) hook DID run, commit refused      -> must stay silent
#
# That last one is the reason the rule is `keep GIT_CONFIG_*` and not `keep
# anything with CONFIG in it`: git honours bare GIT_CONFIG for the `git config`
# command and not for the config it consults when deciding to run a hook, so
# reading it here would report dead hooks that are alive.
run_hook_env() { # $1=cwd $2=command $3..=VAR=VAL for the hook process
  cwd="$1"; cmd="$2"; shift 2
  (
    cd "$cwd" || exit 1
    python3 -c 'import json,sys; print(json.dumps({"tool_name":"Bash","tool_input":{"command":sys.argv[1]}}))' "$cmd" \
      | env "$@" "$HOOK" 2>/dev/null
  )
}

run_env_drift_case() { # $1=name $2=expected $3=cwd $4=command $5..=VAR=VAL
  name="$1"; want="$2"; cwd="$3"; cmd="$4"; shift 4
  record "$name" "$want" "$(classify "$(run_hook_env "$cwd" "$cmd" "$@")")"
}

printf '[core]\n\thooksPath = /dev/null\n' > "$TMP/alt-config"

run_env_drift_case "env -c hooksPath: commit refused" deny-hooks "$OK_REPO" 'git commit -m x' \
  "GIT_CONFIG_PARAMETERS='core.hooksPath=/dev/null'"
run_env_drift_case "env -c hooksPath: push refused"   deny-hooks "$OK_REPO" 'git push' \
  "GIT_CONFIG_PARAMETERS='core.hooksPath=/dev/null'"
run_env_drift_case "env KEY/VALUE triple refused"     deny-hooks "$OK_REPO" 'git commit -m x' \
  GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null
run_env_drift_case "bare GIT_CONFIG is not drift"     silent     "$OK_REPO" 'git commit -m x' \
  "GIT_CONFIG=$TMP/alt-config"

# The discovery half of the same filter is pinned by run_gitdir_case below,
# which predates these rows and covers the identical case; a second copy here
# would only be a second thing to keep true.

# --- the message has to name WHERE the value came from ----------------------
# The rows above prove both drift shapes are refused. They do not distinguish
# them, and the two need OPPOSITE repairs: a value written into .git/config is
# what `just init` exists to undo, while a value arriving through
# GIT_CONFIG_PARAMETERS lives in this process's environment, leaves .git/config
# already correct, and survives `just init` untouched. Told only the value, a
# reader runs the repair, watches it report success, and is refused again.
#
# So these assert on the reason TEXT rather than classify()'s verdict. That is a
# deliberate exception to the note above about asserting verdicts: the claim
# here IS the wording, because the wording is what a human acts on.
reason_of() { # $1=hook stdout -> permissionDecisionReason, or empty
  printf '%s' "$1" | python3 -c 'import json,sys
raw = sys.stdin.read().strip()
try:
    d = json.loads(raw)
except Exception:
    print("")
    raise SystemExit
print((d.get("hookSpecificOutput") or {}).get("permissionDecisionReason") or "")'
}

run_reason_case() { # $1=name $2=substring the reason must contain $3=cwd $4=cmd $5..=VAR=VAL
  name="$1"; want="$2"; cwd="$3"; cmd="$4"; shift 4
  got="$(reason_of "$(run_hook_env "$cwd" "$cmd" "$@")")"
  case "$got" in
    *"$want"*) record "$name" "reason names [$want]" "reason names [$want]" ;;
    *)         record "$name" "reason names [$want]" "ABSENT, reason was: ${got:-<none>}" ;;
  esac
}

# env-borne: git reports origin 'command line:' even though nothing is on a
# command line here — it is the origin git uses for GIT_CONFIG_PARAMETERS too.
run_reason_case "env drift names the environment" "came from the ENVIRONMENT" \
  "$OK_REPO" 'git commit -m x' "GIT_CONFIG_PARAMETERS='core.hooksPath=/dev/null'"
run_reason_case "env drift does not say just init" "will not help" \
  "$OK_REPO" 'git commit -m x' "GIT_CONFIG_PARAMETERS='core.hooksPath=/dev/null'"
# file-borne: the recorded gqlc-nzwa shape, where just init IS the repair.
run_reason_case "file drift names the config file" "from file:" \
  "$SAMPLE_REPO" 'git commit -m x'
run_reason_case "file drift still says just init"  "just init" \
  "$SAMPLE_REPO" 'git commit -m x'

# the repair has to stay runnable, or the guard wedges the session that has to
# fix it. `just init` and a direct git config write are the two documented forms.
run_drift_case "just init still runs while drifted"  warn       "$UNSET_REPO"  'just init'
run_drift_case "config repair still runs"            warn       "$UNSET_REPO"  'git config core.hooksPath .githooks'

# the master guard keeps precedence, so its message is not replaced by drift's
MASTER_DRIFT="$TMP/hooks-master"; mkrepo "$MASTER_DRIFT" master
mkdir -p "$MASTER_DRIFT/.githooks"
printf '#!/bin/sh\nexit 0\n' > "$MASTER_DRIFT/.githooks/commit-msg"
chmod +x "$MASTER_DRIFT/.githooks/commit-msg"
run_drift_case "master guard wins over drift"        deny-master "$MASTER_DRIFT" 'git commit -m x'

# the drift check must follow the command's effective target, like the master
# guard does: a healthy cwd must not excuse a push into a drifted repo, and a
# drifted cwd must not condemn a commit aimed at a healthy one.
run_drift_case "-C into drifted repo from healthy"   deny-hooks "$OK_REPO"     "git -C $UNSET_REPO push"
run_drift_case "-C into healthy repo from drifted"   warn       "$UNSET_REPO"  "git -C $OK_REPO commit -m x"

# ...but the WARN half does not: it keys on the hook's own directory, resolved
# to that directory's repo root. So a non-gated command aimed at a drifted repo
# from a healthy cwd is silent, and a drifted cwd warns from a subdirectory as
# well as from the root. CONTRIBUTING.md documents both.
mkdir -p "$UNSET_REPO/sub"
run_drift_case "-C into drifted, non-gated: silent"  silent     "$OK_REPO"     "git -C $UNSET_REPO status"
run_drift_case "subdir of a drifted repo warns"      warn       "$UNSET_REPO/sub" 'ls -la'

# hooks_drift() strips GIT_* before shelling out, because repo-discovery env
# redirects `git -C <root> config --get` at whichever repo exported it — a
# drifted repo would then read a healthy repo's config and fall silent. The
# `unset "${!GIT_@}"` at the top of this file means no row above can reach that
# guard, so this one puts GIT_DIR back for a single call, pointed at the healthy
# repo while the cwd is the drifted one. Measured without the strip: the answer
# is `silent`, not merely a downgraded `warn` — GIT_DIR masks the drift outright.
run_gitdir_case() { # $1=name $2=expected $3=cwd $4=command $5=GIT_DIR
  local out
  out="$(
    cd "$3" || exit 1
    export GIT_DIR="$5"
    python3 -c 'import json,sys; print(json.dumps({"tool_name":"Bash","tool_input":{"command":sys.argv[1]}}))' "$4" | "$HOOK" 2>/dev/null
  )"
  record "$1" "$2" "$(classify "$out")"
}
run_gitdir_case "GIT_DIR cannot mask drift" deny-hooks "$UNSET_REPO" 'git commit -m x' "$OK_REPO/.git"
# branch_of() carries a second, distinct GIT_* strip for the same reason, which
# the row above executes but cannot detect: its two repos share a branch name,
# so branch_of returns a non-protected branch either way. Here GIT_DIR points at
# a feature-branch repo while the target is on master, so without the strip
# GIT_DIR beats `git -C` and the master guard reads the feature branch.
# MEASURED without it: silent, not a downgraded warn.
run_gitdir_case "GIT_DIR cannot mask the master guard" deny-master "$MASTER_REPO" 'git commit -m x' "$FEATURE_REPO/.git"

# An internal error must not read as a silent allow: this hook exists to refuse
# when the git hooks are dead, so exiting 0 with no output on its own bug is the
# defect it guards against. Malformed stdin is the reachable trigger — json.load
# raises before any check runs. The fixture is the HEALTHY repo on purpose: a
# drift warn is impossible there (the row above it is `silent`), so a warn here
# can only have come from the top-level handler.
run_raw_case() { # $1=name $2=expected $3=cwd $4=raw stdin
  local out
  out="$(cd "$3" && printf '%s' "$4" | "$HOOK" 2>/dev/null)"
  record "$1" "$2" "$(classify "$out")"
}
run_raw_case "healthy repo, valid stdin: silent"  silent "$OK_REPO" '{"tool_name":"Bash","tool_input":{"command":"ls"}}'
run_raw_case "internal error warns, not silent"   warn   "$OK_REPO" 'not json at all'


# ============================================================================
# bd close must not record work against a sha no remote ref contains (gqlc-90vt)
# ============================================================================
#
# One repo with a real remote, carrying three shas that a naive check cannot
# tell apart: one pushed, one on a local branch only, one orphaned by a reset
# (the shape a rebase leaves behind, and the shape of the witnessed incident).
BD_REPO="$TMP/bd"
git init -q --bare "$TMP/bd-origin.git"
mkrepo "$BD_REPO" master
git -C "$BD_REPO" remote add origin "$TMP/bd-origin.git"
git -C "$BD_REPO" push -q -u origin master
PUSHED_SHA="$(git -C "$BD_REPO" rev-parse --short=8 HEAD)"
git -C "$BD_REPO" checkout -q -b localonly
git -C "$BD_REPO" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m local
LOCAL_SHA="$(git -C "$BD_REPO" rev-parse --short=8 HEAD)"
git -C "$BD_REPO" checkout -q master
git -C "$BD_REPO" checkout -q -b doomed
git -C "$BD_REPO" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m doomed
ORPHAN_SHA="$(git -C "$BD_REPO" rev-parse --short=8 HEAD)"
# The uppercase rows need a sha whose uppercase form is a DIFFERENT string, and
# an 8-char abbreviation is all-digits about once in 43 fixtures — which would
# make those rows silently re-run their lowercase siblings and still print ok.
# The full 40 puts that near one in 10^8, and the fixture row below turns even
# that into a loud failure rather than a quiet tautology.
ORPHAN_SHA_FULL="$(git -C "$BD_REPO" rev-parse HEAD)"
ORPHAN_UPPER="$(printf '%s' "$ORPHAN_SHA_FULL" | tr 'a-f' 'A-F')"
ORPHAN_MIXED="${ORPHAN_SHA_FULL:0:20}$(printf '%s' "${ORPHAN_SHA_FULL:20}" | tr 'a-f' 'A-F')"
git -C "$BD_REPO" checkout -q master
git -C "$BD_REPO" branch -q -D doomed
ABSENT_SHA=deadbeefdeadbeef
ABSENT_UPPER=DEADBEEFDEADBEEF
ABSENT_MIXED=deadBEEFdeadBEEF
# An ALL-DECIMAL abbreviated sha that git really resolves, for bd gqlc-7fn3.
# It cannot be a literal: it has to name an object in this fixture, and it has
# to be unpushed, or the row it carries would pass for the wrong reason. About
# 10% of commits have an all-decimal prefix at some length from 7 to 12
# ((10/16)^7 and down), so this loop takes ~10 tries; the cap keeps a hostile
# hash from hanging the suite and the fixture row below turns a miss into a
# loud failure rather than a skipped assertion. The branch is LOCAL and never
# pushed, so the sha it holds is the deny-unpushed-sha shape.
git -C "$BD_REPO" checkout -q -b decimal
DECIMAL_SHA=""
for _ in $(seq 400); do
  git -C "$BD_REPO" -c user.email=t@t.invalid -c user.name=t commit -q --allow-empty -m dec
  DECIMAL_FULL="$(git -C "$BD_REPO" rev-parse HEAD)"
  for len in 7 8 9 10 11 12; do
    cand="${DECIMAL_FULL:0:len}"
    case "$cand" in
      *[!0-9]*) continue ;;
    esac
    if [ "$(git -C "$BD_REPO" cat-file -t "$cand" 2>/dev/null)" = commit ]; then
      DECIMAL_SHA="$cand"
      break
    fi
  done
  if [ -n "$DECIMAL_SHA" ]; then break; fi
done
git -C "$BD_REPO" checkout -q master
# A number no repository will ever hold, and the one that was actually refused:
# the tmpfs inode budget of /tmp, quoted from a citizen's own incident report.
DECIMAL_PROSE=1048576
# Six characters: one under the floor both regexes impose, and a real prefix of
# a real commit, so a floor that slipped to 5 or 6 would promote it.
ORPHAN_SHORT6="${ORPHAN_SHA_FULL:0:6}"
printf 'Closed by branch doomed at %s (not yet pushed).\n' "$ORPHAN_SHA" > "$TMP/reason.txt"
printf 'Closed by branch master at %s.\n' "$PUSHED_SHA" > "$TMP/reason-ok.txt"

# The fixture only means something if the three shas really are distinguishable
# only by the ref set. Asserted, not assumed: without this, a fixture that
# failed to push (or failed to orphan) would make every row below pass for the
# wrong reason.
fixture_check() { # $1=label $2=expected $3=actual
  note_row "fixture: $1"
  if [ "$2" = "$3" ]; then
    pass=$((pass + 1)); printf 'ok   - fixture: %s\n' "$1"
  else
    fail=$((fail + 1)); printf 'FAIL - fixture: %s (expected %s, got %s)\n' "$1" "$2" "$3"
  fi
}
fixture_check "pushed sha is on a remote ref" \
  "origin/master" "$(git -C "$BD_REPO" branch -r --contains "$PUSHED_SHA" | tr -d ' \n')"
fixture_check "local-only sha is on NO remote ref" \
  "" "$(git -C "$BD_REPO" branch -r --contains "$LOCAL_SHA" | tr -d ' \n')"
fixture_check "local-only sha IS on a local ref (so -r is what discriminates)" \
  "localonly" "$(git -C "$BD_REPO" branch --contains "$LOCAL_SHA" --format='%(refname:short)' | tr -d ' \n')"
fixture_check "orphan sha is on no ref at all" \
  "" "$(git -C "$BD_REPO" for-each-ref --contains="$ORPHAN_SHA" --format='%(refname)' | tr -d ' \n')"
fixture_check "orphan sha object still exists locally" \
  "commit" "$(git -C "$BD_REPO" cat-file -t "$ORPHAN_SHA" 2>&1)"
fixture_check "absent sha resolves to nothing" \
  "missing" "$(printf '%s\n' "$ABSENT_SHA" | git -C "$BD_REPO" cat-file --batch-check | awk '{print $2}')"
# The uppercase rows assert that git and the hook agree on a sha the hook used
# to miss. Both halves of that need saying out loud: the uppercase spelling must
# be a different STRING (else the row is its lowercase sibling wearing a hat),
# and git must still RESOLVE it (else the row would pass against a fixed hook
# for the same reason it passed against the broken one — nothing there to find).
fixture_check "uppercase orphan sha is a different string" \
  "differs" "$([ "$ORPHAN_UPPER" != "$ORPHAN_SHA_FULL" ] && echo differs || echo IDENTICAL)"
fixture_check "mixed-case orphan sha differs from both" \
  "differs" "$([ "$ORPHAN_MIXED" != "$ORPHAN_SHA_FULL" ] && [ "$ORPHAN_MIXED" != "$ORPHAN_UPPER" ] && echo differs || echo IDENTICAL)"
fixture_check "git resolves the UPPERCASE orphan sha" \
  "commit" "$(git -C "$BD_REPO" cat-file -t "$ORPHAN_UPPER" 2>&1)"
fixture_check "git resolves the mixed-case orphan sha" \
  "commit" "$(git -C "$BD_REPO" cat-file -t "$ORPHAN_MIXED" 2>&1)"
fixture_check "uppercase orphan sha is still on NO remote ref" \
  "" "$(git -C "$BD_REPO" branch -r --contains "$ORPHAN_UPPER" | tr -d ' \n')"
# ABSENT_UPPER is a literal, so nothing derives it and nothing would notice it
# being retyped in lowercase — at which point the three rows that carry the
# anchored tier's hex class silently become their lowercase siblings and still
# print ok. Found by mutation, 2026-08-22: lowercasing this literal left the
# suite at 177 passed, 0 failed.
fixture_check "uppercase absent sha is the uppercase of the absent one" \
  "differs" "$([ "$ABSENT_UPPER" = "$(printf '%s' "$ABSENT_SHA" | tr 'a-f' 'A-F')" ] && [ "$ABSENT_UPPER" != "$ABSENT_SHA" ] && echo differs || echo IDENTICAL)"
fixture_check "mixed-case absent sha differs from both cases" \
  "differs" "$([ "$ABSENT_MIXED" != "$ABSENT_SHA" ] && [ "$ABSENT_MIXED" != "$ABSENT_UPPER" ] && echo differs || echo IDENTICAL)"
fixture_check "mixed-case absent sha resolves to nothing" \
  "missing" "$(printf '%s\n' "$ABSENT_MIXED" | git -C "$BD_REPO" cat-file --batch-check | awk '{print $2}')"
# The 6-char row is only a floor test if git WOULD resolve it — otherwise a
# lowered floor promotes a token git drops anyway and the row stays green for
# the wrong reason.
fixture_check "the 6-char prefix does resolve to the orphan commit" \
  "commit" "$(git -C "$BD_REPO" cat-file -t "$ORPHAN_SHORT6" 2>&1)"

# run_verdict asserts the NAME close_verdict() returned, by importing the hook
# as a module. An allow asserted only as "not denied" cannot tell an allow that
# was decided from one that was reached because sha extraction stopped working.
run_verdict() { # $1=name $2=expected-verdict $3=cwd $4=command
  local got
  got="$(
    cd "$3" || exit 1
    python3 - "$HOOK" "$4" <<'PY'
import importlib.machinery, importlib.util, json, sys
sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader("hook", sys.argv[1])
spec = importlib.util.spec_from_loader("hook", loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
closes = []
mod.git_targets(sys.argv[2], __import__("os").getcwd(), mod.HOOK_GATED, closes=closes)
print(",".join(mod.close_verdict(c)[0] for c in closes) or "no-close-seen")
PY
  )"
  note_row "$1"
  if [ "$got" = "$2" ]; then
    pass=$((pass + 1)); printf 'ok   - %s\n' "$1"
  else
    fail=$((fail + 1)); printf 'FAIL - %s (expected verdict %s, got %s)\n' "$1" "$2" "$got"
  fi
}

# run_case reads allow/deny and run_verdict reads the NAME close_verdict()
# returned. Neither reads the deny MESSAGE, and main() reads nothing else — so
# nulling a message turns a refusal into a silent allow that both of them pass.
# Measured on this file's parent revision: doing that at each of the three
# `return "deny-unverifiable-repo"` sites left the suite at 146 passed, 0 failed,
# while the same mutation on deny-absent-object reddened one row and on
# deny-unreadable-reason three. close_refusal reads the message, naming each of
# the three sites by its own sentence so one site's row cannot be satisfied by
# another site's refusal, and falls through to classify() so a mutant's allow
# reads as `silent` rather than as some other deny.
# shellcheck disable=SC2016 # the patterns are the hook's literal message text
close_refusal() { # $1=hook stdout -> the site's name, else classify()'s verdict
  if printf '%s' "$1" | grep -q 'could not resolve the repository the command runs in'; then
    printf 'deny-unresolvable-dir'
  elif printf '%s' "$1" | grep -q 'could not get an answer from `git cat-file --batch-check`'; then
    printf 'deny-unanswerable-objects'
  elif printf '%s' "$1" | grep -q 'gave no answer, so reachability could not be checked'; then
    printf 'deny-unanswerable-reachability'
  else
    classify "$1"
  fi
}

run_close_case() { # $1=name $2=expected $3=cwd $4=command
  record "$1" "$2" "$(close_refusal "$(run_hook "$3" "$4")")"
}

# The other two sites are the answers of two subprocess probes, and no fixture
# reaches them: object_types() and remote_refs_containing() catch OSError and
# TimeoutExpired themselves, and the GIT_* env that could redirect them is unset
# at the top of this file. So the probe is stubbed to the "could not tell" value
# its own docstring names, and the hook still runs through its REAL __main__ —
# same shape as the exception row further down, so main(), the close arm and
# deny() are all the shipped ones.
run_unanswerable_case() { # $1=name $2=expected $3=cwd $4=command $5=probe to silence
  local got
  got="$(
    cd "$3" || exit 1
    python3 - "$HOOK" "$4" "$5" <<'PY' 2>/dev/null
import io, json, sys
sys.dont_write_bytecode = True

hook, command, victim = sys.argv[1:4]
unanswerable = {"object_types": None, "remote_refs_containing": (False, [])}[victim]


class Swap(dict):
    def __setitem__(self, key, value):
        if key == victim:
            def silenced(*args, **kwargs):
                return unanswerable

            value = silenced
        super().__setitem__(key, value)


glb = Swap({"__name__": "__main__", "__file__": hook, "__builtins__": __builtins__})
sys.stdin = io.StringIO(json.dumps(
    {"tool_name": "Bash", "tool_input": {"command": command}}))
out, sys.stdout = sys.stdout, io.StringIO()
try:
    exec(compile(open(hook).read(), hook, "exec"), glb)  # noqa: S102 - the artifact under test
except SystemExit:
    pass
captured, sys.stdout = sys.stdout.getvalue(), out
sys.stdout.write(captured)
PY
  )"
  record "$1" "$2" "$(close_refusal "$got")"
}

# --- state 1: reachable from a remote ref -> permitted ----------------------
run_case    "sha on a remote ref"              allow "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b at $PUSHED_SHA (1 commit).\""
run_verdict "sha on a remote ref (verdict)"    allow-reachable "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b at $PUSHED_SHA (1 commit).\""

# --- state 2: the witnessed incident, replayed verbatim in shape ------------
run_case    "orphaned sha, 'not yet pushed'"   deny  "$BD_REPO" \
  "bd close gqlc-rz0l -r \"Closed by branch docs/c1-bare-arg-spec-drift at $ORPHAN_SHA (12 commits, not yet pushed).\""
run_verdict "orphaned sha (verdict)"           deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-rz0l -r \"Closed by branch docs/c1-bare-arg-spec-drift at $ORPHAN_SHA (12 commits, not yet pushed).\""

# --- state 3: the sha names nothing -> refused, and refused DIFFERENTLY -----
run_case    "sha that names no object"         deny  "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b at $ABSENT_SHA.\""
run_verdict "sha that names no object (verdict)" deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b at $ABSENT_SHA.\""

# --- state 4: on a LOCAL branch only. `cat-file -t` says commit and
#     `branch --contains` (no -r) names a branch, so both naive checks pass it.
run_case    "sha on a local branch only"       deny  "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch localonly at $LOCAL_SHA.\""
run_verdict "sha on a local branch only (verdict)" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch localonly at $LOCAL_SHA.\""

# --- state 5: empty / absent sha. `git branch -r --contains` with NO argument
#     defaults to HEAD and prints the remote refs containing it, so a dropped
#     argument reads as "reachable". Pinned three ways: the flag with its value
#     omitted, the shell-built and stdin reasons, and the probe called directly.
run_case    "reason flag with value omitted"   deny  "$BD_REPO" 'bd close gqlc-x -r'
run_verdict "reason flag with value omitted (verdict)" deny-unreadable-reason "$BD_REPO" 'bd close gqlc-x -r'
# shellcheck disable=SC2016 # the expansion is the hook's input, not this file's code
run_case    "reason built by the shell"        deny  "$BD_REPO" 'bd close gqlc-x -r "closed at $SHA"'
run_case    "reason read from stdin"           deny  "$BD_REPO" 'bd close gqlc-x --reason-file -'
# shellcheck disable=SC2016 # ditto
run_verdict "reason built by the shell (verdict)" deny-unreadable-reason "$BD_REPO" 'bd close gqlc-x -r "$(cat /tmp/r)"'
# The other half of UNEXPANDED_RE. A backtick is command substitution too, and
# it is the half with the reach: of the 7 corpus close reasons this arm refuses,
# 4 carry a backtick and no `$`. Deleting that clause left the suite green while
# a backtick reason fell to allow-no-sha — read as prose, and never checked.
# shellcheck disable=SC2016 # the backtick is the hook's input, not this file's code
run_verdict "backtick reason is unreadable"    deny-unreadable-reason "$BD_REPO" 'bd close gqlc-x -r "see `notes.md` for the sha"'
run_case    "empty reason string"              allow "$BD_REPO" 'bd close gqlc-x -r ""'
run_verdict "empty reason string (verdict)"    allow-no-sha "$BD_REPO" 'bd close gqlc-x -r ""'

fixture_check "argument-less probe defaults to HEAD, so a dropped arg reads reachable" \
  "origin/master" "$(git -C "$BD_REPO" branch -r --contains | tr -d ' \n')"
EMPTY_REV_PROBE="$(
  python3 - "$HOOK" "$BD_REPO" <<'PY'
import importlib.machinery, importlib.util, sys
sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader("hook", sys.argv[1])
spec = importlib.util.spec_from_loader("hook", loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
print("%s|%s" % (mod.remote_refs_containing(sys.argv[2], ""),
                 mod.remote_refs_containing(sys.argv[2], "HEAD")))
PY
)"
fixture_check "empty rev answers 'could not tell', where HEAD answers 'reachable'" \
  "(False, [])|(True, ['origin/master'])" "$EMPTY_REV_PROBE"

# --- closes that must NOT be denied ----------------------------------------
run_case    "close with no reason flag"        allow "$BD_REPO" 'bd close gqlc-x'
run_verdict "close with no reason flag (verdict)" allow-no-reason "$BD_REPO" 'bd close gqlc-x'
run_case    "reason citing no sha"             allow "$BD_REPO" 'bd close gqlc-x -r "landed via PR 42, reviewed"'
run_verdict "reason citing no sha (verdict)"   allow-no-sha "$BD_REPO" 'bd close gqlc-x -r "landed via PR 42, reviewed"'
# All-hex English words are why an unresolvable LOOSE token is not a citation.
run_case    "hex-looking prose, unanchored"    allow "$BD_REPO" 'bd close gqlc-x -r "the docs were defaced then effaced"'
run_verdict "hex-looking prose, unanchored (verdict)" allow-no-sha "$BD_REPO" 'bd close gqlc-x -r "the docs were defaced then effaced"'
run_case    "unrelated bd subcommand"          allow "$BD_REPO" "bd show gqlc-x --at $ORPHAN_SHA"

# --- the loose tier PROMOTES, not just declines ------------------------------
# The rows above are the negative half of the two-tier design: a loose token git
# cannot resolve stays prose. The positive half is the majority shape — 79 of
# the corpus's 110 sha-citing close reasons carry no at/@/commit anchor — and
# without these rows `citations = sorted(anchored)` (i.e. never promoting a
# loose token) left the whole suite green while three quarters of the corpus
# silently became allow-no-sha. Both directions, because a promotion that always
# denied would satisfy the deny row alone.
run_verdict "unanchored orphan sha is promoted (verdict)" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"$ORPHAN_SHA fixed the doomed thing\""
run_case    "unanchored orphan sha is promoted"           deny  "$BD_REPO" \
  "bd close gqlc-x -r \"$ORPHAN_SHA fixed the doomed thing\""
run_verdict "unanchored pushed sha is promoted, then allowed" allow-reachable "$BD_REPO" \
  "bd close gqlc-x -r \"$PUSHED_SHA landed on origin\""

# --- the two spellings of the subcommand this arm keys on --------------------
# `bd` reached by path and `bd done` (bd's documented alias for close, per
# `bd close --help`: "Aliases: close, done") are both live, and both survived
# deletion with the suite green before these rows existed.
run_verdict "bd invoked by path still scanned"  deny-unpushed-sha "$BD_REPO" \
  "/usr/local/bin/bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\""
run_verdict "the done alias is a close"         deny-unpushed-sha "$BD_REPO" \
  "bd done gqlc-x -r \"Closed at $ORPHAN_SHA.\""

# Three closes in one command, asserted as an ORDERED sequence rather than as a
# set: each `bd` invocation is scanned separately and keeps its own reason, so a
# scanner that stopped after the first, merged their reasons, or reordered them
# fails here. Three distinct verdicts, so no pair swap can go unseen.
run_verdict "three closes keep their own reasons, in order" \
  "deny-unpushed-sha,allow-reachable,deny-absent-object" "$BD_REPO" \
  "bd close a -r \"Closed at $ORPHAN_SHA.\" && bd close b -r \"Closed at $PUSHED_SHA.\" && bd close c -r \"Closed at $ABSENT_SHA.\""

# `commit <sha>` is an anchor too — 5 of the corpus's 110 sha-citing close
# reasons use it and no `at`/`@`. Pinned on the absent sha, which is the only
# state where being anchored changes the answer.
run_verdict "commit <sha> anchors an absent sha" deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Headline commit $ABSENT_SHA landed.\""
run_verdict "commit <sha> anchors an orphan sha" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Headline commit $ORPHAN_SHA landed.\""
# `@<sha>` is the third anchor spelling, and the one no row reached: deleting it
# from CITED_SHA_RE left the whole suite green while `@<absent sha>` fell from
# deny-absent-object to allow-no-sha, because unanchored it is a loose token git
# cannot resolve. It is the sole match for 2 of the 309 corpus close reasons.
run_verdict "@<sha> anchors an absent sha"       deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"landed @$ABSENT_SHA, review followed.\""

# --- case (gqlc-vzqi) -------------------------------------------------------
# git resolves hex in any case, and both regexes matched `[0-9a-f]` only, so
# `-r "Closed at 453E5D20."` named a real unpushed commit and the hook saw no
# sha at all. Every row above spells its sha lowercase, which is why this
# survived six rounds of adversarial review on this very arm.
#
# Two independent defects, so they need rows that fail independently:
#
#   the HEX CLASS   — split by tier, because widening one regex and not the
#                     other leaves half the bypass standing. An ABSENT sha can
#                     only be reached by the anchored tier (a loose token git
#                     cannot resolve is dropped as prose), and an UNANCHORED
#                     orphan can only be reached by the loose tier. So one row
#                     each, and neither can stand in for the other.
#
#   the ANCHOR WORD — pinned with LOWERCASE hex so the hex class is not in
#                     play; the only thing that can carry these is `(?i:)`.
#                     Absent shas again, that being the one state where being
#                     anchored changes the answer.
run_verdict "UPPERCASE hex, anchored, absent"    deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b at $ABSENT_UPPER.\""
run_verdict "UPPERCASE hex, loose, orphan"       deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"$ORPHAN_UPPER fixed the doomed thing\""
# Mixed case, because a fix that alternated `[0-9a-f]{7,40}|[0-9A-F]{7,40}`
# handles ALL CAPS and still drops `453E5d20`. Split by tier for the same reason
# the uppercase rows are: on an ORPHAN sha the two tiers rescue each other, and
# the orphan row below passed against exactly that mutant until the absent one
# joined it (mutation, 2026-08-22 — it survived on 179 green).
run_verdict "mixed-case hex, anchored, orphan"   deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $ORPHAN_MIXED.\""
run_verdict "mixed-case hex, anchored, absent"   deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $ABSENT_MIXED.\""
run_verdict "mixed-case hex, loose, orphan"      deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"$ORPHAN_MIXED fixed the doomed thing\""
run_verdict "the anchor word AT in capitals"     deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b AT $ABSENT_SHA.\""
# The corpus instance, not a hypothetical: gqlc-v23's close reason opens a
# sentence with `Commits 4491fb8(model)+...`, and the capital alone is what put
# it outside the anchor. Measured over .beads/issues.jsonl at eb28226b, it is
# the ONE field of 498 whose verdict this whole change moves — allow-no-sha to
# deny-absent-object, because that sha names no object in the repo.
run_verdict "sentence-initial Commit is an anchor" deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Commit $ABSENT_SHA(model) closed it.\""
run_verdict "@ with UPPERCASE hex"               deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"landed @$ABSENT_UPPER, review followed.\""
# main() reads the deny MESSAGE and run_verdict reads only the NAME, so without
# a run_case the uppercase refusal could be decided and never reach the caller.
run_case    "UPPERCASE hex is refused to the caller" deny "$BD_REPO" \
  "bd close gqlc-x -r \"Closed by branch b at $ABSENT_UPPER.\""
# The other direction. Widening the class puts all-hex English words in CAPS
# within reach of the loose tier, and a fix that answered this by refusing loose
# tokens outright would stop real closes — the majority corpus shape is
# unanchored. `DEFACED` is all-hex, resolves to nothing, and must stay prose.
run_verdict "an ALL-CAPS hex word is not a citation" allow-no-sha "$BD_REPO" \
  "bd close gqlc-x -r \"The docs were DEFACED and then restored.\""
# The 7-char floor is the other thing holding prose out, and widening the class
# leans on it harder. `beefed` is six hex characters and ordinary English, and
# it sits right after an anchor — so at a floor of 5 or 6 this reason becomes an
# anchored citation and the close is refused. Mutation, 2026-08-22: dropping the
# floor to 5 reddened nothing before this row existed.
run_verdict "a 6-char hex word after an anchor is not a citation" allow-no-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at beefed-up coverage.\""
# The loose tier's floor needs its own row, and it needs a token git RESOLVES —
# an unresolvable one is dropped whatever the floor, so it cannot tell 7 from 5.
# A 6-char abbreviation of a real commit is the case: humans do write shas that
# short, and this pins that the hook deliberately does not treat them as
# citations. Lowering the loose floor alone reddened nothing before this row.
run_verdict "a 6-char sha prefix is below the loose floor" allow-no-sha "$BD_REPO" \
  "bd close gqlc-x -r \"$ORPHAN_SHORT6 was the culprit\""

# --- the effective repo is resolved, like the master guard's --------------
#
# Asserted by VERDICT, not by deny alone. $TMP is not a git repository, so a
# hook that ignored `-C`/`cd` would still deny these — as deny-unverifiable-repo
# rather than deny-unpushed-sha. Pinned as deny they passed while the
# retargeting was mutated away; the paired allow rows are the other half.
run_verdict "bd -C retargets to the fixture repo"    deny-unpushed-sha "$TMP" \
  "bd -C $BD_REPO close gqlc-x -r \"Closed at $ORPHAN_SHA.\""
run_verdict "bd -C retargets, reachable sha allowed" allow-reachable "$TMP" \
  "bd -C $BD_REPO close gqlc-x -r \"Closed at $PUSHED_SHA.\""
run_verdict "cd chain retargets to the fixture repo" deny-unpushed-sha "$TMP" \
  "cd $BD_REPO && bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\""
run_verdict "cd chain retargets, reachable sha allowed" allow-reachable "$TMP" \
  "cd $BD_REPO && bd close gqlc-x -r \"Closed at $PUSHED_SHA.\""
run_case    "bd -C to the fixture repo"        deny  "$TMP" \
  "bd -C $BD_REPO close gqlc-x -r \"Closed at $ORPHAN_SHA.\""
run_verdict "unresolvable cwd is refused, not skipped" deny-unverifiable-repo "$TMP" \
  "cd \"\$WT\" && bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\""

# ...and the three rows that make that refusal REACH the caller. The row above
# reads close_verdict()'s name; main() reads only its message, so the name is
# not evidence the hook refuses. Each row below names one of the three
# `return "deny-unverifiable-repo"` sites by the sentence only that site emits;
# replace any one of those messages with None and its row, and only its row,
# goes red with `silent`.
run_close_case "unresolvable cwd refuses end-to-end" deny-unresolvable-dir "$TMP" \
  "cd \"\$WT\" && bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\""
run_unanswerable_case "an unanswerable object probe refuses end-to-end" \
  deny-unanswerable-objects "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\"" object_types
run_unanswerable_case "an unanswerable reachability probe refuses end-to-end" \
  deny-unanswerable-reachability "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\"" remote_refs_containing

# --- --reason-file is READ, not merely noticed ------------------------------
# Both halves, for the same reason as above: a hook that ignored the flag
# entirely would report deny-unreadable-reason and satisfy a deny-only row.
run_verdict "--reason-file citing an orphan sha"   deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x --reason-file $TMP/reason.txt"
run_verdict "--reason-file citing a pushed sha"    allow-reachable "$BD_REPO" \
  "bd close gqlc-x --reason-file $TMP/reason-ok.txt"
run_case    "--reason-file with a literal path"    deny "$BD_REPO" \
  "bd close gqlc-x --reason-file $TMP/reason.txt"

# --- the joined spelling of a flag, `--reason=VALUE` -------------------------
# Every row above passes the reason as two tokens. `bd close --help` (v1.0.4)
# documents `-r, --reason string`, so the joined form is real usage, and
# scan_bd's `"=" in opt` splitter is what reads it. Deleting that splitter makes
# the token an unrecognised flag, skipped whole: the close is then seen with NO
# reason flag and verdicts allow-no-reason — it disappears from this guard
# rather than merely mis-verdicting, with the suite green.
run_verdict "--reason=VALUE citing an orphan sha"  deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x --reason=\"Closed at $ORPHAN_SHA.\""

# --- the pflag shorthand spellings bd honours (bd gqlc-90vt round 6) ---------
# bd is cobra/pflag, so a shorthand may carry its value attached and shorthands
# may cluster. Measured against bd v1.0.4: `-rZZZ` and `-fr ZZZ` both parse,
# `-fr` with nothing after it answers "flag needs an argument: 'r'", and `-Z`
# answers "unknown shorthand flag", so the acceptance is real and not a silent
# swallow. Each spelling below verdicted allow-no-reason before scan_bd walked
# the cluster — a false positive on the record, saying "this close carries no
# reason flag" about a close carrying the incident's reason verbatim. `-f` is
# the ordinary spelling for force-closing a pinned bead, so `-fr` is reachable
# usage rather than a contrivance.
run_verdict "-rVALUE attached to the shorthand"    deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r\"Closed at $ORPHAN_SHA.\""
run_verdict "-fr VALUE, r last in the cluster"     deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -fr \"Closed at $ORPHAN_SHA.\""
run_verdict "-qr=VALUE, cluster with a joined value" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -qr=\"Closed at $ORPHAN_SHA.\""
# -C is the other value-taking shorthand, and it retargets rather than reads:
# without the cluster walk the -C is skipped whole, the effective directory
# stays $TMP, and the verdict is deny-unverifiable-repo instead.
run_verdict "-CPATH attached to the shorthand"     deny-unpushed-sha "$TMP" \
  "bd -C$BD_REPO close gqlc-x -r \"Closed at $ORPHAN_SHA.\""
# The other direction: a cluster of BOOLEAN shorthands takes no value, so it
# must not swallow the token after it. Treating any cluster as value-taking
# consumes `close` here and the close disappears from the guard entirely.
run_verdict "a boolean-only cluster consumes nothing" deny-unpushed-sha "$BD_REPO" \
  "bd -qv close gqlc-x -r \"Closed at $ORPHAN_SHA.\""

# --- a redirect between the subcommand and the flags (bd gqlc-vmlp) ---------
# The option walk stopped at the first operator token, and the shell accepts a
# redirection anywhere in a simple command — so a `>` written before the flags
# hid them, the close verdicted allow-no-reason, and an unpushed close went
# through. Every row here carries the SAME orphan sha as the trailing-redirect
# control below it, so what separates them is the position of the operator and
# nothing else. The `<` and fd-prefixed forms are separate rows because they
# tokenize differently: `2>&1` comes back as three tokens, the first of them an
# ordinary word.
run_verdict "redirect between subcommand and flags" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x > /dev/null -r \"Closed at $ORPHAN_SHA.\""
run_verdict "attached redirect, no space"           deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x >/dev/null -r \"Closed at $ORPHAN_SHA.\""
run_verdict "append redirect between them"          deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x >> $TMP/log -r \"Closed at $ORPHAN_SHA.\""
run_verdict "input redirect between them"           deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x < /dev/null -r \"Closed at $ORPHAN_SHA.\""
run_verdict "fd-prefixed redirect between them"     deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x 2>&1 -r \"Closed at $ORPHAN_SHA.\""
run_verdict "trailing redirect control"             deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\" > /dev/null"
# The other direction, and the reason a redirect is skipped by an explicit list
# rather than by "any token of operator characters": `|`, `;` and `&&` DO end
# the command, and the words after them belong to the next one. Both rows would
# verdict deny-unpushed-sha if the walk ran on through the terminator, because
# the `-r` after it carries the orphan sha — and neither `-r` is bd's.
run_verdict "a pipe still ends the command"         allow-no-reason "$BD_REPO" \
  "bd close gqlc-x | grep -r \"Closed at $ORPHAN_SHA.\""
run_verdict "a semicolon still ends the command"    allow-no-reason "$BD_REPO" \
  "bd close gqlc-x ; echo -r \"Closed at $ORPHAN_SHA.\""

# --- an all-decimal token is not a sha citation (bd gqlc-7fn3) --------------
# The anchored tier refuses on the anchor alone, without resolving anything, so
# `at 1048576` — an inode count quoted out of a citizen's own measurement — was
# refused as a close against a commit "this repository has never seen", three
# times in a row, on a close that was otherwise correct. The remedy on offer was
# to make the reason less precise, and this town's close discipline is to paste
# measured output into the reason, so the guard fired hardest on the closes
# carrying the most evidence.
# The fix DEMOTES an all-decimal token to the loose tier rather than dropping
# it, and that is what the second row pins: a decimal token git resolves to an
# unpushed commit here is still refused. About 3.7% of 7-character abbreviations
# are all-decimal, so a fix that excluded them from both tiers would have been a
# real hole rather than a nuisance removed.
fixture_check "the fixture found an all-decimal sha" \
  "found" "$([ -n "$DECIMAL_SHA" ] && echo found || echo NONE)"
fixture_check "the all-decimal sha resolves to a commit" \
  "commit" "$(git -C "$BD_REPO" cat-file -t "$DECIMAL_SHA" 2>&1)"
fixture_check "the all-decimal sha is on NO remote ref" \
  "" "$(git -C "$BD_REPO" branch -r --contains "$DECIMAL_SHA" | tr -d ' \n')"
fixture_check "the decimal prose token names no object" \
  "missing" "$(printf '%s\n' "$DECIMAL_PROSE" | git -C "$BD_REPO" cat-file --batch-check | awk '{print $2}')"
run_verdict "a measured number after an anchor is not a sha" allow-no-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Refused three times; /tmp holds at $DECIMAL_PROSE inodes.\""
run_verdict "an all-decimal UNPUSHED sha is still refused" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $DECIMAL_SHA.\""
# The control that keeps the row above from being read as "the anchored tier
# stopped working": a hex token after the same anchor, naming nothing here, is
# still refused on the anchor alone.
run_verdict "a hex token after the same anchor still denies" deny-absent-object "$BD_REPO" \
  "bd close gqlc-x -r \"Refused three times; /tmp holds at $ABSENT_SHA inodes.\""

# --- a close written inside a command substitution --------------------------
# git_targets threads `closes` into its own $( ) / backtick recursion, and this
# row is what pins that argument. The spelling is load-bearing: shlex(posix,
# punctuation_chars) splits a BARE $(...) into separate tokens, so `bd` reaches
# the outer token pass too and the same close is found TWICE — measured, a bare
# row verdicts deny-unpushed-sha,deny-unpushed-sha here — which leaves it denied
# with the recursion's argument dropped. Quoted, backticked or heredoc'd, shlex
# hands back one opaque token and the recursion is the only finder: drop the
# argument and this row's close verdicts no-close-seen. It disappears from the
# guard rather than mis-verdicting, and the rest of the suite stays green. The
# extractors themselves are already pinned by the master-guard rows above; what
# was unpinned is the threading, which only the close arm reaches.
run_verdict "close inside a quoted substitution"   deny-unpushed-sha "$BD_REPO" \
  "echo \"\$(bd close gqlc-x -r 'Closed at $ORPHAN_SHA.')\""
# The same position with the incident's own reason text. A parenthesised close
# reason is the majority shape here — 192 of the 309 in .beads/issues.jsonl at
# 15091c86 carry a paren and 106 of those carry a sha as well. Restore a scan
# whose body excludes parens and this row verdicts no-close-seen — the close
# vanishing from the guard rather than mis-verdicting.
run_verdict "quoted substitution, parenthesised reason" deny-unpushed-sha "$BD_REPO" \
  "echo \"\$(bd close gqlc-rz0l -r 'Closed by branch doomed at $ORPHAN_SHA (12 commits, not yet pushed).')\""
# ...and a reachable sha in that same position, so `quoted substitution,
# parenthesised reason` records a decision rather than a scan that now denies
# whatever is parenthesised.
run_verdict "quoted substitution, parenthesised reachable reason" allow-reachable "$BD_REPO" \
  "echo \"\$(bd close gqlc-x -r 'Closed at $PUSHED_SHA (1 commit, pushed).')\""
# The limit the counting leaves: quoting is not tracked, so a paren the shell
# would read as a literal is counted too. The two unbalanced directions part
# company here. A surplus `(` never brings the depth back to zero, so nothing
# closes the span, it is not extracted at all, and quoted, shlex hands back one
# token — fail-OPEN, this row.
run_verdict "a surplus ( leaves the span unclosed and unread" no-close-seen "$BD_REPO" \
  "echo \"\$(bd close gqlc-x -r 'Closed at $ORPHAN_SHA (12 commits.')\""
# A surplus `)` brings the depth to zero EARLY, so the span is closed at that
# paren and the body does come back, truncated there. Written so the truncation
# falls outside the reason's quote, the shortened body still tokenizes and the
# close is read and verdicted on the sha it carries. Make paren_matches abandon
# the map when a `)` finds nothing open instead of dropping it — this row's
# trailing `)` is such a paren — and this row verdicts no-close-seen, because
# the span then has nothing closing it. The one unbalanced reason among the 192
# paren-bearing corpus reasons is this direction, not the row above: gqlc-oxne,
# 6 `(` to 7 `)`, depth first going negative 857 characters in on a
# `case " ${a} ${b} " in *" ${x} "*)` pattern, in a reason that is not truncated
# and runs on for another kilobyte.
run_verdict "a surplus ) closes the span early" deny-unpushed-sha "$BD_REPO" \
  "echo \"\$(bd close gqlc-x -r 'Closed at $ORPHAN_SHA.'; esac) tail)\""

# --- what the extractor hands back, for a body that nests -------------------
# Three decisions inside substitution_bodies() share one end-to-end verdict:
# that the outer `$( )` is matched by counting depth, that its nested span is
# blanked out of it, and that the nested body is returned alongside rather than
# left to a further recursion. Blanking is what stops a nested close being
# counted twice; returning it alongside is what keeps it at the same depth as
# one written bare, which is what the depth cap is measured against.
# Asserted on the extractor's own output because TWO of the three are invisible
# end to end: mutated on its own, not blanking runs 163/1 and not flattening
# runs 163/1, this fixture the only red in each. The depth count is NOT in that
# position — making the first `)` close the outer span runs 160/4, and three of
# those are verdict rows (`quoted substitution, parenthesised reason`, the
# `... reachable reason` beside it, and `a surplus ( leaves the span unclosed
# and unread`). So this fixture is the only guard for two of the decisions and
# a second witness for the third.
SUBST_PROBE="$(
  python3 - "$HOOK" <<'PY'
import importlib.machinery, importlib.util, sys
sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader("hook", sys.argv[1])
spec = importlib.util.spec_from_loader("hook", loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
print(mod.substitution_bodies("""echo "$(bd close x -r 'at (12 commits) $(date)')" """))
PY
)"
# Each entry is (body, directory), the directory being where the shell would
# run that span — None here, no cwd having been passed and no cd preceding
# either span. Entries are in OPENING order, parents before children: the frame
# stack that made the extraction linear (bd gqlc-hv81) reserves a slot when it
# opens a span and fills it when it closes, which is what keeps that order the
# same as the queue's was. Measured identical to the queue's output, body for
# body and in order, over all 97656 strings of `$`, `(`, `)`, backtick and `x`
# up to length 7, and over 200k random strings of shell metacharacters.
fixture_check "a nested paren-bearing body: counted, blanked, flattened" \
  "[(\"bd close x -r 'at (12 commits)  '\", None), ('date', None)]" "$SUBST_PROBE"

# --- object_types' own guards, driven with a stubbed subprocess -------------
# `git cat-file --batch-check` emits one line per input line and the mapping is
# positional, so a short or long reply must not be zipped anyway: a mis-paired
# type can call an orphan a commit or a commit an orphan. No git invocation can
# produce that, so the boundary is stubbed rather than staged.
OBJTYPES_PROBE="$(
  python3 - "$HOOK" "$BD_REPO" <<'PY'
import importlib.machinery, importlib.util, sys, types as _t
sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader("hook", sys.argv[1])
spec = importlib.util.spec_from_loader("hook", loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)


def stub(out, rc=0):
    return lambda *a, **k: _t.SimpleNamespace(stdout=out, stderr="", returncode=rc)


real = mod.subprocess.run
two = ["aaaaaaa", "bbbbbbb"]
mod.subprocess.run = stub("aaaaaaa missing\n")            # short reply
short = mod.object_types(sys.argv[2], two)
mod.subprocess.run = stub("a missing\nb missing\nc missing\n")  # long reply
long_ = mod.object_types(sys.argv[2], two)
mod.subprocess.run = stub("", 1)                          # git failed
failed = mod.object_types(sys.argv[2], two)
mod.subprocess.run = real
print("%s|%s|%s|%s" % (short, long_, failed, mod.object_types(sys.argv[2], [""])))
PY
)"
fixture_check "a short, long, failed or empty-rev batch-check answers 'could not tell'" \
  "None|None|None|None" "$OBJTYPES_PROBE"

# --- git_env() is what keeps a leaked GIT_DIR from answering for another repo -
# The `unset "${!GIT_@}"` at the top of this file means no row above can reach
# git_env() at all — dropping `env=git_env()` from both subprocess calls left the
# whole suite green. These two put GIT_DIR back for a single call each, and they
# are aimed at DIFFERENT call sites, because one row cannot separate them:
#   - bd-origin.git HOLDS the pushed object but has no refs/remotes, so only
#     remote_refs_containing's answer can change: leaked, it reports the sha as
#     contained by nothing and a reachable close becomes deny-unpushed-sha.
#   - the feature repo holds NEITHER object, so object_types' answer changes
#     first: leaked, the orphan resolves to missing and deny-unpushed-sha
#     becomes deny-absent-object.
# Fail direction is closed in both (a false refusal, not a false allow), which
# is why this is pinned rather than treated as a hole.
run_gitdir_verdict() { # $1=name $2=expected-verdict $3=cwd $4=command $5=GIT_DIR
  local got
  got="$(
    cd "$3" || exit 1
    GIT_DIR="$5" python3 - "$HOOK" "$4" <<'PY'
import importlib.machinery, importlib.util, sys
sys.dont_write_bytecode = True
loader = importlib.machinery.SourceFileLoader("hook", sys.argv[1])
spec = importlib.util.spec_from_loader("hook", loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
closes = []
mod.git_targets(sys.argv[2], __import__("os").getcwd(), mod.HOOK_GATED, closes=closes)
print(",".join(mod.close_verdict(c)[0] for c in closes) or "no-close-seen")
PY
  )"
  record "$1" "$2" "$got"
}
fixture_check "the bare origin holds the pushed object but no remote-tracking ref" \
  "commit|" "$(git --git-dir="$TMP/bd-origin.git" cat-file -t "$PUSHED_SHA")|$(git --git-dir="$TMP/bd-origin.git" branch -r --contains "$PUSHED_SHA" | tr -d ' \n')"
run_gitdir_verdict "GIT_DIR cannot answer the reachability probe" allow-reachable "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $PUSHED_SHA.\"" "$TMP/bd-origin.git"
run_gitdir_verdict "GIT_DIR cannot answer the object probe" deny-unpushed-sha "$BD_REPO" \
  "bd close gqlc-x -r \"Closed at $ORPHAN_SHA.\"" "$FEATURE_REPO/.git"

# --- an exception inside the close arm must DENY, not escape -----------------
# The close arm is the one place where "could not check" must not mean "let it
# through": __main__ catches everything else and WARNS (pinned by "internal
# error warns, not silent" above), and a warn is an allow — the close would run
# and the ledger would record work that may never have landed. So the arm's own
# `except Exception` is load-bearing, and nothing else in this suite can reach
# it: close_verdict() catches OSError and TimeoutExpired at every boundary it
# owns, so no fixture makes it raise. Stubbed instead, and stubbed into the REAL
# file executed through its REAL `__main__` guard: the source is compiled at its
# own path with a globals mapping that swaps close_verdict as the module binds
# it, so main(), the arm, deny() and the top-level handler are all the shipped
# ones. Narrow the arm to a subclass of Exception and this prints warn.
EXC_PROBE="$(
  cd "$BD_REPO" && python3 - "$HOOK" <<'PY'
import io, json, sys
sys.dont_write_bytecode = True


class Swap(dict):
    def __setitem__(self, key, value):
        if key == "close_verdict":
            def raiser(close):
                raise RuntimeError("stubbed")
            value = raiser
        super().__setitem__(key, value)


hook = sys.argv[1]
glb = Swap({"__name__": "__main__", "__file__": hook, "__builtins__": __builtins__})
sys.stdin = io.StringIO(json.dumps(
    {"tool_name": "Bash", "tool_input": {"command": 'bd close gqlc-x -r "landed"'}}))
out, sys.stdout = sys.stdout, io.StringIO()
try:
    exec(compile(open(hook).read(), hook, "exec"), glb)  # noqa: S102 - the artifact under test
except SystemExit:
    pass
captured, sys.stdout = sys.stdout.getvalue(), out
if '"permissionDecision": "deny"' in captured and "could not be checked" in captured:
    print("deny-uncheckable-close")
elif '"systemMessage"' in captured:
    print("warn-escaped-to-top-level")
else:
    print("other(%s)" % captured.strip()[:80])
PY
)"
record "an exception in the close arm denies" deny-uncheckable-close "$EXC_PROBE"

# This test writes nothing into the tree it tests. Asserted rather than
# assumed: the leak it guards against is silent here and fatal in lint-hooks,
# which is a different recipe on a different run.
fixture_check "the suite leaves no bytecode in the hooks tree" \
  "" "$(find "$(dirname "$HOOK")" -name '__pycache__' -o -name '*.pyc' | tr -d '\n')"

# A total pins suite SIZE, not membership — swapping a row for a different one
# leaves it green, which is why membership is pinned separately by the mutation
# battery, and only for the subcommands the rows above name: dropping commit,
# merge, pull or push from HOOK_GATED turns a named row red, and so does adding
# one a warn row names, but adding a subcommand no row mentions does not.
# What this total catches is the one thing that battery cannot see: rows
# silently disappearing. Without it, deleting every run_drift_case invocation
# exited 0, and so did deleting just the two escape-hatch rows. Both fail now.
# Counted at the END of the file rather than after the master-guard block, so
# the bd-close rows are inside it too.
EXPECTED_ROWS=225
if [ "$((pass + fail))" -ne "$EXPECTED_ROWS" ]; then
  printf 'FAIL - suite size drifted: expected %d rows, ran %d\n' "$EXPECTED_ROWS" "$((pass + fail))"
  fail=$((fail + 1))
fi

# The total above still leaves a RENAME or a SWAP invisible: retitle a row, or
# delete one and add an unrelated one, and the count is unmoved. This digest is
# over every row NAME in run order, so both move it. Row names are static
# strings — no path, sha or temp dir reaches one — so the digest is the same on
# every machine. Update it deliberately when a row is added, renamed or
# reordered; a drift you did not intend is the finding.
EXPECTED_ROW_DIGEST=3b76226a048d22be
ROW_DIGEST="$(printf '%s' "$ROWS" | python3 -c \
  'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest()[:16])')"
if [ "$ROW_DIGEST" != "$EXPECTED_ROW_DIGEST" ]; then
  printf 'FAIL - suite membership drifted: row-name digest %s, expected %s\n' \
    "$ROW_DIGEST" "$EXPECTED_ROW_DIGEST"
  printf 'the %d row names that ran, in order:\n' "$((pass + fail))"
  printf '%s' "$ROWS" | sed 's/^/    /'
  fail=$((fail + 1))
fi

printf -- '---\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
