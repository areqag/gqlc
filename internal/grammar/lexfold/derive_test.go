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

func derive(lex antlr.Lexer) Table {
	a := readATN(lex)
	standIns := a.standIns()
	other := standIns[a.signature(0xFFFD)]
	bounds := a.boundaries()
	var t Table
	t.Other = other
	add := func(lo, hi, to rune) {
		if to == other {
			return
		}
		if n := len(t.Ranges); n > 0 && t.Ranges[n-1].Hi == lo-1 && t.Ranges[n-1].To == to {
			t.Ranges[n-1].Hi = hi
			return
		}
		t.Ranges = append(t.Ranges, Range{Lo: lo, Hi: hi, To: to})
	}
	for i := 0; i+1 < len(bounds); i++ {
		lo, hi := bounds[i], bounds[i+1]-1
		if a.signature(lo) != a.signature(hi) {
			panic(fmt.Sprintf("lexer classes are not constant over %U..%U", lo, hi))
		}
		to, ok := standIns[a.signature(lo)]
		if !ok {
			to = Keep
			for c := 0x80; c <= MaxDFAEdge && to == Keep; c++ {
				if a.interchangeable(lo, c) {
					to = rune(c)
				}
			}
		}
		add(rune(lo), rune(hi), to)
	}
	return t
}

func render(tables map[string]Table) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by TestTablesAreCurrent -update; DO NOT EDIT.\n\n")
	b.WriteString("package lexfold\n")
	for _, name := range []string{"GQL", "Cypher"} {
		t := tables[name]
		fmt.Fprintf(&b, "\n// %s is the fold for internal/grammar/%s/gen.\n", name, map[string]string{"GQL": "gql", "Cypher": "cypher"}[name])
		fmt.Fprintf(&b, "var %s = Table{\n\tOther: %#x,\n\tRanges: []Range{\n", name, t.Other)
		for _, r := range t.Ranges {
			to := fmt.Sprintf("%#x", r.To)
			if r.To == Keep {
				to = "Keep"
			}
			fmt.Fprintf(&b, "\t\t{%#x, %#x, %s},\n", r.Lo, r.Hi, to)
		}
		b.WriteString("\t},\n}\n")
	}
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
	derived := map[string]Table{
		"GQL":    derive(gqlgen.NewGQLLexer(antlr.NewInputStream(""))),
		"Cypher": derive(cyphergen.NewCypherLexer(antlr.NewInputStream(""))),
	}
	want := render(derived)
	if *update {
		require.NoError(t, os.WriteFile(tablesFile, want, 0o644))
		return
	}
	got, err := os.ReadFile(tablesFile)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got), "%s is stale: run go test ./internal/grammar/lexfold -run TestTablesAreCurrent -update", tablesFile)
	require.Equal(t, derived["GQL"], GQL)
	require.Equal(t, derived["Cypher"], Cypher)
}
