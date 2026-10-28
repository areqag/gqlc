package lexfold

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"reflect"
	"sort"
	"testing"
	"unsafe"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	cyphergen "github.com/areqag/gqlc/internal/grammar/cypher/gen"
	gqlgen "github.com/areqag/gqlc/internal/grammar/gql/gen"
)

var update = flag.Bool("update", false, "regenerate tables_gen.go from the generated lexers")

const tablesFile = "tables_gen.go"

// The ATN's states and each transition's label are unexported in antlr4-go, so
// the derivation reads them by reflection. That ties this test, and only this
// test, to the runtime version in go.mod; a rename there fails it loudly.
func unexported(v reflect.Value, name string) reflect.Value {
	f := v.FieldByName(name)
	return reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
}

func field[T any](v reflect.Value, name string) T {
	x, ok := unexported(v, name).Interface().(T)
	if !ok {
		panic(fmt.Sprintf("antlr runtime: field %s is not a %T", name, x))
	}
	return x
}

// lexerATN is what the derivation needs from one lexer: its reachable states,
// every symbol-consuming transition among them, and the rule names.
type lexerATN struct {
	states    []antlr.ATNState
	trans     []antlr.Transition
	from      []antlr.ATNState // from[i] holds trans[i]
	ruleNames []string
}

func readATN(lex antlr.Lexer) *lexerATN {
	atn := reflect.ValueOf(lex.GetATN()).Elem()
	starts := field[[]*antlr.TokensStartState](atn, "modeToStartState")
	a := &lexerATN{ruleNames: lex.GetRuleNames()}
	seen := map[antlr.ATNState]bool{}
	var stack []antlr.ATNState
	for _, s := range starts {
		stack = append(stack, s)
	}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[s] {
			continue
		}
		seen[s] = true
		a.states = append(a.states, s)
		for _, tr := range s.GetTransitions() {
			tv := reflect.ValueOf(tr).Elem()
			stack = append(stack, field[antlr.ATNState](tv, "target"))
			if _, ok := tr.(*antlr.RuleTransition); ok {
				stack = append(stack, field[antlr.ATNState](tv, "followState"))
			}
			if !unexported(tv, "isEpsilon").Bool() {
				a.trans = append(a.trans, tr)
				a.from = append(a.from, s)
			}
		}
	}
	return a
}

func target(tr antlr.Transition) antlr.ATNState {
	return field[antlr.ATNState](reflect.ValueOf(tr).Elem(), "target")
}

// boundaries returns every code point at which some transition's label starts
// or stops, so that each half-open span between two consecutive ones is matched
// by the same transitions throughout.
func (a *lexerATN) boundaries() []int {
	set := map[int]bool{MaxDFAEdge + 1: true, antlr.LexerMaxCharValue + 1: true}
	for _, tr := range a.trans {
		label := field[*antlr.IntervalSet](reflect.ValueOf(tr).Elem(), "intervalSet")
		if label == nil {
			continue
		}
		for _, iv := range label.GetIntervals() {
			set[iv.Start] = true
			set[iv.Stop] = true // exclusive
		}
	}
	var out []int
	for b := range set {
		if b > MaxDFAEdge && b <= antlr.LexerMaxCharValue+1 {
			out = append(out, b)
		}
	}
	sort.Ints(out)
	return out
}

// signature is the set of transitions that consume c. Two code points with the
// same signature are indistinguishable to the lexer.
func (a *lexerATN) signature(c int) string {
	b := make([]byte, len(a.trans))
	for i, tr := range a.trans {
		b[i] = '0'
		if tr.Matches(c, 0, antlr.LexerMaxCharValue) {
			b[i] = '1'
		}
	}
	return string(b)
}

// interchangeable reports whether c and r, whose signatures differ, still
// produce the same tokens. That holds in one shape, the GQL and Cypher
// identifier start `ID_Start | Pc`: every transition that tells them apart
// is the single transition of one of two fragment rules, each fragment is
// invoked exactly once, as one alternative of the same block, the two
// alternatives rejoin at once, and c and r each pass one of them.
func (a *lexerATN) interchangeable(c, r int) bool {
	rules := map[int]bool{}
	for i, tr := range a.trans {
		if tr.Matches(c, 0, antlr.LexerMaxCharValue) != tr.Matches(r, 0, antlr.LexerMaxCharValue) {
			rules[a.from[i].GetRuleIndex()] = true
		}
	}
	if len(rules) != 2 {
		return false
	}
	passes := func(x int) bool {
		for i, tr := range a.trans {
			if rules[a.from[i].GetRuleIndex()] && tr.Matches(x, 0, antlr.LexerMaxCharValue) {
				return true
			}
		}
		return false
	}
	return a.singleTransitionRules(rules) && a.siblingAlternatives(rules) && passes(c) && passes(r)
}

func (a *lexerATN) singleTransitionRules(rules map[int]bool) bool {
	n := map[int]int{}
	for i := range a.trans {
		n[a.from[i].GetRuleIndex()]++
	}
	for rule := range rules {
		if n[rule] != 1 {
			return false
		}
	}
	return true
}

// siblingAlternatives: the rules' only invocations sit alone in two
// alternatives entered from one state, and both return to one state.
func (a *lexerATN) siblingAlternatives(rules map[int]bool) bool {
	var invokers, rejoins []antlr.ATNState
	for _, s := range a.states {
		for _, tr := range s.GetTransitions() {
			rt, ok := tr.(*antlr.RuleTransition)
			if !ok || !rules[target(rt).GetRuleIndex()] {
				continue
			}
			follow := field[antlr.ATNState](reflect.ValueOf(rt).Elem(), "followState")
			if len(s.GetTransitions()) != 1 || len(follow.GetTransitions()) != 1 ||
				!unexported(reflect.ValueOf(follow.GetTransitions()[0]).Elem(), "isEpsilon").Bool() {
				return false
			}
			invokers = append(invokers, s)
			rejoins = append(rejoins, target(follow.GetTransitions()[0]))
		}
	}
	if len(invokers) != 2 || rejoins[0] != rejoins[1] {
		return false
	}
	for _, s := range a.states {
		ts := s.GetTransitions()
		if len(ts) == 2 && target(ts[0]) == invokers[0] && target(ts[1]) == invokers[1] ||
			len(ts) == 2 && target(ts[0]) == invokers[1] && target(ts[1]) == invokers[0] {
			return true
		}
	}
	return false
}

// standIns maps each signature to the code point that represents it: the
// lowest one in 0x80..MaxDFAEdge if the class has one there, else the lowest
// ASCII one.
func (a *lexerATN) standIns() map[string]rune {
	out := map[string]rune{}
	for c := MaxDFAEdge; c >= 0; c-- {
		sig := a.signature(c)
		if _, ok := out[sig]; !ok || c >= 0x80 {
			out[sig] = rune(c)
		}
	}
	return out
}

// standIn is the rune a lexer is shown for the span starting at lo.
func (a *lexerATN) standIn(standIns map[string]rune, lo int) rune {
	if to, ok := standIns[a.signature(lo)]; ok {
		return to
	}
	for c := 0x80; c <= MaxDFAEdge; c++ {
		if a.interchangeable(lo, c) {
			return rune(c)
		}
	}
	return Keep
}

// derive builds the shared ranges and one Table per lexer. A class is a
// distinct tuple of stand-ins across the lexers, numbered in order of first
// appearance after class 0, the tuple U+FFFD gets.
func derive(lexers ...antlr.Lexer) ([]Range, []Table) {
	atns := make([]*lexerATN, len(lexers))
	standIns := make([]map[string]rune, len(lexers))
	set := map[int]bool{}
	for i, lex := range lexers {
		atns[i] = readATN(lex)
		standIns[i] = atns[i].standIns()
		for _, b := range atns[i].boundaries() {
			set[b] = true
		}
	}
	var bounds []int
	for b := range set {
		bounds = append(bounds, b)
	}
	sort.Ints(bounds)

	tables := make([]Table, len(lexers))
	classes := map[string]uint8{}
	classOf := func(c int) uint8 {
		tuple := make([]rune, len(atns))
		for i, a := range atns {
			tuple[i] = a.standIn(standIns[i], c)
		}
		key := fmt.Sprint(tuple)
		id, ok := classes[key]
		if !ok {
			id = uint8(len(classes))
			classes[key] = id
			for i := range tables {
				tables[i] = append(tables[i], tuple[i])
			}
		}
		return id
	}
	classOf(0xFFFD)

	var ranges []Range
	for i := 0; i+1 < len(bounds); i++ {
		lo, hi := bounds[i], bounds[i+1]-1
		for _, a := range atns {
			if a.signature(lo) != a.signature(hi) {
				panic(fmt.Sprintf("lexer classes are not constant over %U..%U", lo, hi))
			}
		}
		class := classOf(lo)
		if n := len(ranges); n > 0 && ranges[n-1].Hi == rune(lo-1) && ranges[n-1].Class == class {
			ranges[n-1].Hi = rune(hi)
			continue
		}
		if class != 0 {
			ranges = append(ranges, Range{Lo: rune(lo), Hi: rune(hi), Class: class})
		}
	}
	return ranges, tables
}

func standInLiteral(r rune) string {
	if r == Keep {
		return "Keep"
	}
	return fmt.Sprintf("%#x", r)
}

func render(ranges []Range, gql, cypher Table) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by TestTablesAreCurrent -update; DO NOT EDIT.\n\npackage lexfold\n\n")
	for _, t := range []struct {
		name, pkg string
		table     Table
	}{{"GQL", "gql", gql}, {"Cypher", "cypher", cypher}} {
		fmt.Fprintf(&b, "// %s is the fold for internal/grammar/%s/gen.\nvar %s = Table{", t.name, t.pkg, t.name)
		for _, r := range t.table {
			fmt.Fprintf(&b, "%s, ", standInLiteral(r))
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("var ranges = []Range{\n")
	for _, r := range ranges {
		fmt.Fprintf(&b, "{%#x, %#x, %d},\n", r.Lo, r.Hi, r.Class)
	}
	b.WriteString("}\n")
	out, err := format.Source(b.Bytes())
	if err != nil {
		panic(err)
	}
	return out
}

// TestTablesAreCurrent is the fence between the grammars and this package: it
// re-derives both tables from the generated lexers and requires the checked-in
// ones to match, so a regenerated grammar that moves a lexer class fails here
// rather than lexing a rune as some other class.
func TestTablesAreCurrent(t *testing.T) {
	derivedRanges, tables := derive(
		gqlgen.NewGQLLexer(antlr.NewInputStream("")),
		cyphergen.NewCypherLexer(antlr.NewInputStream("")),
	)
	want := render(derivedRanges, tables[0], tables[1])
	if *update {
		require.NoError(t, os.WriteFile(tablesFile, want, 0o644))
		return
	}
	got, err := os.ReadFile(tablesFile)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "%s is stale: run go test ./internal/grammar/lexfold -run TestTablesAreCurrent -update", tablesFile)
	require.Equal(t, derivedRanges, ranges)
	require.Equal(t, tables[0], GQL)
	require.Equal(t, tables[1], Cypher)
}
