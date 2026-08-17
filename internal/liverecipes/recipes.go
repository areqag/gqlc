// Package liverecipes reads the two artefacts the live battery is split
// across — the justfile recipes that invoke it and the Go sources that
// declare it — and reports where they disagree.
//
// The command-line reader here is the lower half. It answers what a recipe
// body runs and which flags each command carries, and it is separate from
// the questions asked of those answers: the AGE witness sweep asks whether a
// recipe runs one witness at all, and the arm split in split.go asks which
// top-level tests each half selects. Both read the same recipes, and a second
// reader would be a second answer to drift from.
package liverecipes

import (
	"regexp"
	"strings"
	"unicode"
)

// Commands is every command a line CONTAINS in command position, each as its
// own fields — up to the operator that ends it, and not one field further.
// unterminated says the line's quoting never closed, which makes every field
// of it a guess.
//
// A command starts the line or follows `&&`, `||`, `;` or `|`. An operator
// not surrounded by spaces (`a&&go test`) does not end one: the fields are
// split on whitespace, so `a&&go` is one field and no command starts there.
func Commands(line string) (commands [][]string, unterminated bool) {
	fields, quoted := Fields(line)
	start := 0
	for i := 0; i <= len(fields); i++ {
		if i < len(fields) {
			switch fields[i] {
			case "&&", "||", ";", "|":
			default:
				continue
			}
		}
		if command := fields[start:i]; len(command) > 0 {
			commands = append(commands, command)
		}
		start = i + 1
	}
	return commands, quoted
}

// runs reports whether a command invokes prog — its first field being prog,
// or a path ending in one.
//
// Command position is what stops `echo go test -run W` from counting:
// searching every argument for `go` beside `test` counts a line that only
// prints the words.
//
// Three things are consequently read as invoking nothing: a compiled binary
// invoked directly, a script that runs the program inside itself, and an
// environment prefix (`GOFLAGS=… go test`), which no live recipe writes today.
func runs(command []string, prog ...string) bool {
	if len(command) < len(prog) {
		return false
	}
	if command[0] != prog[0] && !strings.HasSuffix(command[0], "/"+prog[0]) {
		return false
	}
	for i, want := range prog[1:] {
		if command[i+1] != want {
			return false
		}
	}
	return true
}

// GoTestInvocations is every `go test` command a recipe body CONTAINS in
// command position, each as the fields of that command ALONE. unterminated
// says some line's quoting never closed.
//
// Contains, not runs: this is a SUPERSET of what the body executes, because
// a command after `||` runs only when the one before it failed, and a command
// before `&&` can leave the rest of the line unreached. Which direction that
// fails in belongs to the caller and is not one safe direction:
//
//   - A caller requiring a flag of EVERY invocation gets a requirement from a
//     command that is counted and not run. Complaint.
//   - A caller satisfied by SOME invocation selecting a test can be told yes
//     for a body that runs it only on another command's failure. Silence,
//     measured: review mutation P3 put
//     `go test -run 'TestLiveSmoke' … || go test -run '<the full set>' …` in
//     the live recipe and the AGE sweep stayed green over a body whose witness
//     invocation runs only on the smoke battery's failure.
//
// Running is also not gating. `… || true` appended to a real recipe (review
// mutation P4; `|| true` is one of this justfile's own idioms) leaves the
// tests running and the recipe failing on nothing, and this reader says
// nothing about it, because its claim is about what runs.
//
// Reading flags from the command that runs them is the whole of this
// function, and it is why callers get segments rather than a yes/no. The
// predecessor pair — "does a `go test` appear anywhere" plus "does a -tags
// appear anywhere" — is satisfied by a body where those are two different
// commands (review mutations POOLTAG2, POOLTAG, CNT1, CNT2, POOLRUN2), and
// every one of them is silence.
func GoTestInvocations(cmds string) (invocations [][]string, unterminated bool) {
	for _, line := range strings.Split(cmds, "\n") {
		commands, quoted := Commands(line)
		if quoted {
			unterminated = true
		}
		for _, command := range commands {
			if runs(command, "go", "test") {
				invocations = append(invocations, command)
			}
		}
	}
	return invocations, unterminated
}

// JustRecipes is every recipe name a shell fragment invokes `just` with. A
// flag is not a recipe name; everything else after `just` is taken as one,
// because `just a b` runs both a and b whenever neither takes parameters.
//
// A fragment whose quoting never closes yields the recipes it named before
// the open quote and nothing after it. The caller's own emptiness guard is
// what that has to walk into.
func JustRecipes(script string) []string {
	var names []string
	for _, line := range strings.Split(script, "\n") {
		commands, _ := Commands(StripComment(line))
		for _, command := range commands {
			if !runs(command, "just") {
				continue
			}
			for _, arg := range command[1:] {
				if !strings.HasPrefix(arg, "-") {
					names = append(names, arg)
				}
			}
		}
	}
	return names
}

// Selects reports how a `go test -run` / `-skip` pattern reaches the
// TOP-LEVEL test named name: reaches is whether some alternative matches it
// at all, wholly is whether some alternative matches it with no further
// elements. The two differ exactly when a pattern narrows to subtests, and
// that difference is what a caller reading a -skip is built on — a -skip that
// reaches a subtest of a test still runs the test, a -skip that matches it
// wholly removes it.
//
// go test splits a pattern on top-level `|` into alternatives first and each
// alternative on `/` into elements, then matches element i against the i'th
// part of a test's name with an UNANCHORED regexp match (testing/match.go:
// splitRegexp, alternationMatch.matches, simpleMatch.matches). So a pattern
// reaches a top-level test when some alternative's first element matches it as
// a regexp: `TestAGERefuses` selects TestAGERefusesTheFunctionsItDoesNotDefine,
// and string equality is the wrong question in both directions. Verified
// against go1.26.5 rather than read off the docs — `-skip 'TestLiveSmoke/neo4j|W'`
// skips W outright, because the appended text is a second alternative and not a
// second element; and `-run 'TestLiveSmoke|W/x'` leaves W selected, running none
// of its subtests, at exit 0.
//
// Every alternative is read even after one matches, because a narrowed
// alternative can be followed by a whole one. A pattern with an uncompilable
// alternative is therefore refused even when an earlier alternative already
// matched: doubt REACHES everything and selects nothing whole, which is the
// refusal both readings want — a -run this reader cannot compile does not
// select the test, and a -skip it cannot compile might drop it. A complaint
// either way, never silence.
//
// That is why there is no error return. With one, only a -skip reading could
// act on it: an error comes back with reaches false, which is the answer that
// LETS a -skip through, while for -run wholly is false already. The -run arm's
// err branch is then something nothing can make true (review mutation G3b,
// dead) and the -skip arm carries the only live one (G3). Two truths in one
// value make one of them unreachable.
//
// The split here is naive where go test's is bracket-aware: a `|` or `/`
// inside `[...]` or `(...)` is top-level to this function and is not to go
// test. That direction is deliberate. The pieces a naive split makes of
// `TestFoo(A|B)` do not compile.
func Selects(pattern, name string) (reaches, wholly bool) {
	for _, alt := range strings.Split(pattern, "|") {
		head, _, narrowed := strings.Cut(alt, "/")
		re, err := regexp.Compile(head)
		if err != nil {
			return true, false
		}
		if !re.MatchString(name) {
			continue
		}
		reaches = true
		if !narrowed {
			wholly = true
		}
	}
	return reaches, wholly
}

// FlagValues is every value a command line gives one go test flag, in any of
// the spellings go's flag package accepts: `-flag value`, `-flag=value`,
// `--flag`, and the `-test.flag` form a compiled binary takes.
func FlagValues(fields []string, flag string) []string {
	var values []string
	for i := 0; i < len(fields); i++ {
		name, value, assigned := strings.Cut(fields[i], "=")
		if strings.TrimPrefix(strings.TrimLeft(name, "-"), "test.") != flag {
			continue
		}
		switch {
		case assigned:
			values = append(values, value)
		case i+1 < len(fields):
			values = append(values, fields[i+1])
			i++
		default:
			// A flag with nothing after it. go test rejects the command
			// line, but this reader still has to answer, and the empty
			// pattern is the one that selects everything — which for a
			// -skip means the test is skipped and complained about.
			values = append(values, "")
		}
	}
	return values
}

// Fields splits a recipe's command line into arguments, honouring single and
// double quotes so `-run 'A|B'` stays one argument. The second return says a
// quote was still open at the end — the line the caller handed over is not a
// line this reader finished.
//
// Not a shell: no escapes, no expansion, no operators, no substitution. It
// exists to find -run, -skip and -tags and the values beside them.
//
// The flags fail in OPPOSITE directions when it is defeated, so naming only
// the safe one states the limit wrongly. All three are below. Expansion is the
// case that matters, because a recipe may write
// `SKIP='TestLiveSmoke/neo4j|W' && go test … -skip "$SKIP"`, which is one
// executable line:
//
//   - For -run, an argument this reader takes literally selects nothing, and
//     a test reads as not running that does. Complaint.
//   - For -skip, the same literal argument SKIPS nothing, so the test reads as
//     running while the recipe skips it outright. Silence, and the shape this
//     package exists to refuse. Measured as review mutation V4b: `$SKIP` and
//     `${SKIP}` both compile as regexps and match no test name, `$` being the
//     end-of-text anchor.
//   - For -tags, a literal `$TAGS` carries no build tag, so the recipe reads
//     as building none of the live battery. Complaint.
//
// Closing that means being a shell, which is not a cost this check is worth;
// it is stated rather than fixed. No live recipe expands a variable into a
// test pattern today, and the justfile's own shell variables are elsewhere.
func Fields(s string) (fields []string, unterminated bool) {
	var (
		cur   strings.Builder
		quote rune
		open  bool
	)
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, open = r, true
		case unicode.IsSpace(r):
			if open {
				fields = append(fields, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if open {
		fields = append(fields, cur.String())
	}
	return fields, quote != 0
}

// StripComment drops a line's shell comment: everything from the first `#`
// that starts a word outside quotes, which is where sh starts one. This is the
// recipe artefact's half of the property a Go source reader holds by parsing:
// text that is spelled is not text that runs.
//
// Cutting at the FIRST `#` of any kind is not "the safe direction whose worst
// case is complaining about a test the recipe does run" — it fails toward
// silence. Review mutation V3:
// `go test -count=1 -tags codegen_live -ldflags '-X main.p=a#b' -run 'TestLiveSmoke' …`
// keeps -count=1 and loses -run, and the flagless remainder reads as selecting
// everything. Silence over a recipe that runs no witness — the exact shape of
// L20 with the cut moved.
//
// Word-start-outside-quotes is sh's own rule, so for the shapes this reader
// models the cut is where sh puts it — an unquoted `-X p=a#b` keeps its `#`,
// because that one does not start a word.
//
// Of those two halves only word-start carries the answer. Dropping it cuts a
// live -run away and the flagless remainder runs everything ("an unquoted #
// inside a word is not a comment either" — mutation G14w). Dropping the QUOTE
// half changes no answer, measured: a cut inside a quoted argument removes
// that argument's closing quote along with everything after it, so Fields
// reports unterminated and a caller reading the line refuses it anyway
// (mutation G14q survives). It is kept because it is sh's rule and because the
// redundancy belongs to the unterminated check rather than to this function —
// but it is redundancy, not a second guard, and both paths are complaints.
//
// What is NOT modelled is backslash escapes, command substitution, heredocs
// and here-strings. This justfile uses all four, so the bound is not "the
// artefact has none of them"; it is that this reader is pointed at the live
// recipes, and those use none of them. That is a property of single-line
// recipes today and not a law about the file, so it is held by
// TestTheRecipesThisReaderParsesStayInsideTheShellItModels rather than
// asserted here.
//
// Each cuts where sh would not: `\#` is a literal to sh, a `#` inside `$(…)`
// opens a comment in the substitution and not in the line around it, and every
// `#` in heredoc or here-string data is data. The remainder after a wrong cut
// either fails to close its quoting, which a caller refuses, or it parses with
// fewer flags than the shell runs — and the second is silence, not a
// complaint, when what was lost is a -run. That is V3's shape again, one layer
// down; it is bounded rather than closed.
func StripComment(line string) string {
	var quote rune
	startsWord := true
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && startsWord:
			return line[:i]
		}
		startsWord = quote == 0 && unicode.IsSpace(r)
	}
	return line
}

// RecipeBody is what one named justfile recipe runs, with its comments already
// stripped, so everything downstream reads what the shell executes rather than
// what the justfile spells. The second return is false when the file declares
// no such recipe, or declares it with no body — a caller that treats those as
// an empty body reports on a recipe it never found.
func RecipeBody(src, name string) (string, bool) {
	lines := strings.Split(src, "\n")
	var body []string
	for i, line := range lines {
		if line != name+":" {
			continue
		}
		for _, next := range lines[i+1:] {
			if next == "" || !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t") {
				break
			}
			body = append(body, StripComment(next))
		}
		break
	}
	if len(body) == 0 {
		return "", false
	}
	return strings.Join(body, "\n"), true
}
