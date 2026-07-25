package gql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// alternativeIndex turns a parse tree node into a `rule#N` tag. That is what makes
// the third obligation set mechanical: an author declaring which alternatives a
// file exercises would drift from the grammar the same way the listener did, which
// is the defect this corpus exists to catch.
//
// Two mechanisms, because neither covers the other's rules. A rule whose
// alternatives carry `# AltLabel` is identified by the context struct ANTLR
// generates per label — the struct name is the alternative, so nothing is inferred,
// and it holds even for left-recursive rules where the tree is rewritten. Every
// other rule is identified by its ordered direct-child sequence, matched against
// each alternative's expression, taking the lowest-numbered match because that is
// the one ALL(*) prefers.
//
// Taking the lowest-numbered match is exact only because no alternative this gate
// requires is shadowed by a lower-numbered sibling that accepts every child sequence
// it can produce; such an alternative could never be tagged and would sit red on
// every spelling. All 47 were checked, none is shadowed: see bd memory
// m2-lowest-numbered-match-unshadowed for the method and the result. It is a fact
// about today's grammar, not an invariant enforced here, and what guards it is
// wantInvisibleAlternatives: shadowing can only arrive with a grammar change, and a
// grammar change to the alternative set trips that pin first. Re-run the check before
// repinning it.
type alternativeIndex struct {
	// byRule holds one entry per rule with more than one alternative. A rule with
	// one alternative has nothing to tell apart and is deliberately absent, so a
	// lookup miss means "nothing to attribute" rather than "could not attribute".
	byRule map[string]alternativeSet
}

// alternativeSet is how one rule's alternatives are told apart. contexts is set
// for a labelled rule and shapes for an unlabelled one, never both; blocked is the
// reason neither could be built, which callers turn into a failure rather than
// silently attributing nothing.
type alternativeSet struct {
	contexts []string
	shapes   []ebnf
	blocked  string
}

func newAlternativeIndex(parserRules map[string]string) *alternativeIndex {
	idx := &alternativeIndex{byRule: make(map[string]alternativeSet, len(parserRules))}
	for rule, body := range parserRules {
		elements, labels := ruleAlternatives(body)
		if len(elements) < 2 {
			continue
		}
		if labels[0] != "" {
			idx.byRule[rule] = labelledAlternatives(labels)
			continue
		}
		idx.byRule[rule] = shapedAlternatives(elements)
	}
	return idx
}

// labelledAlternatives maps each alternative to the Go struct ANTLR generates for
// its label. ANTLR requires a rule's alternatives to be all labelled or none, so a
// gap means the label scan is misreading the grammar, not that the grammar is odd.
func labelledAlternatives(labels []string) alternativeSet {
	contexts := make([]string, len(labels))
	for i, label := range labels {
		if label == "" {
			return alternativeSet{blocked: fmt.Sprintf(
				"alternative %d carries no # label while alternative 1 does", i+1)}
		}
		contexts[i] = strings.ToUpper(label[:1]) + label[1:] + "Context"
	}
	return alternativeSet{contexts: contexts}
}

func shapedAlternatives(elements []string) alternativeSet {
	shapes := make([]ebnf, len(elements))
	for i, alt := range elements {
		expr, err := parseEBNF(alt)
		if err != nil {
			return alternativeSet{blocked: fmt.Sprintf("alternative %d: %v", i+1, err)}
		}
		shapes[i] = expr
	}
	return alternativeSet{shapes: shapes}
}

// tag returns the `rule#N` tag a parse tree node took. It returns an empty tag and
// no error only when the rule has a single alternative, so a caller that treats
// every error as a failure cannot end up recording nothing for a rule that had a
// choice to make.
func (idx *alternativeIndex) tag(rule, contextType string, children []string) (string, error) {
	set, ok := idx.byRule[rule]
	switch {
	case !ok:
		return "", nil
	case set.blocked != "":
		return "", fmt.Errorf("%s: %s", rule, set.blocked)
	}

	for i, name := range set.contexts {
		if name == contextType {
			return fmt.Sprintf("%s#%d", rule, i+1), nil
		}
	}
	for i, shape := range set.shapes {
		if shape.matches(children) {
			return fmt.Sprintf("%s#%d", rule, i+1), nil
		}
	}

	if set.contexts != nil {
		return "", fmt.Errorf("%s parsed into %s, which is none of its %d labelled alternative contexts",
			rule, contextType, len(set.contexts))
	}
	return "", fmt.Errorf("%s with children [%s] matched none of its %d alternatives",
		rule, strings.Join(children, " "), len(set.shapes))
}

// blocked returns why the index cannot attribute a rule, or "" if it can.
func (idx *alternativeIndex) blocked(rule string) string {
	return idx.byRule[rule].blocked
}

// witness returns a child sequence that identifies tag's alternative and no
// earlier one, or nil if every sequence it can produce is also matched by a
// lower-numbered alternative. A tag with no witness cannot be discharged: an author
// could write the file the worklist asks for and still see the gate demand it,
// because the index would credit the earlier alternative.
func (idx *alternativeIndex) witness(rule string, alt int) []string {
	shapes := idx.byRule[rule].shapes
	for _, candidate := range shapes[alt-1].expand() {
		stolen := false
		for _, earlier := range shapes[:alt-1] {
			if earlier.matches(candidate) {
				stolen = true
				break
			}
		}
		if !stolen {
			return candidate
		}
	}
	return nil
}

// TestAlternativeDistinguishability proves the third gate is closeable at all: for
// every alternative the corpus owes, some child sequence identifies it and no
// earlier alternative of the same rule. A labelled rule is exempt because the
// generated context type identifies it exactly, with no shape to confuse.
func TestAlternativeDistinguishability(t *testing.T) {
	want := grammarObligation(t)

	for _, tag := range want.required {
		rule, alt := parseTag(t, tag)
		if want.alternatives.byRule[rule].shapes == nil {
			continue
		}
		require.NotNil(t, want.alternatives.witness(rule, alt),
			"every child sequence %s can produce is also matched by an earlier alternative, so no corpus file can discharge it", tag)
	}
}

// TestAlternativeIndex pins the attribution against the real grammar, because the
// index is the single point of vacuity for the third obligation set: if it
// attributed nothing the gate would demand everything, and if it attributed
// generously the gate would demand nothing. Every case here is a shape observed in
// the probe that established the two mechanisms, not one the matcher was written
// against.
func TestAlternativeIndex(t *testing.T) {
	index := grammarObligation(t).alternatives

	for _, tc := range []struct {
		rule        string
		contextType string
		children    []string
		want        string
	}{
		{rule: "graphTypeSource", children: []string{"copyOfGraphType"}, want: "graphTypeSource#1"},
		{rule: "graphTypeSource", children: []string{"AS", "copyOfGraphType"}, want: "graphTypeSource#1"},
		{rule: "graphTypeSource", children: []string{"graphTypeLikeGraph"}, want: "graphTypeSource#2"},
		{rule: "graphTypeSource", children: []string{"AS", "nestedGraphTypeSpecification"}, want: "graphTypeSource#3"},

		// The `(objectName PERIOD)*` tail distinguishes nothing on its own, so these
		// two pin that the leading schemaReference is what decides the alternative.
		{rule: "catalogObjectParentReference", children: []string{"schemaReference", "SOLIDUS"}, want: "catalogObjectParentReference#1"},
		{rule: "catalogObjectParentReference", children: []string{"schemaReference", "objectName", "PERIOD", "objectName", "PERIOD"}, want: "catalogObjectParentReference#1"},
		{rule: "catalogObjectParentReference", children: []string{"objectName", "PERIOD"}, want: "catalogObjectParentReference#2"},

		// Alternative 3 is a superset of alternative 1's single symbol, so a matcher
		// that stopped at the first prefix match would report #1 here.
		{rule: "nodeTypeImpliedContent", children: []string{"nodeTypeLabelSet"}, want: "nodeTypeImpliedContent#1"},
		{rule: "nodeTypeImpliedContent", children: []string{"nodeTypeLabelSet", "nodeTypePropertyTypes"}, want: "nodeTypeImpliedContent#3"},

		{rule: "verboseBinaryExactNumericType", children: []string{"INTEGER", "LEFT_PAREN", "precision", "RIGHT_PAREN"}, want: "verboseBinaryExactNumericType#8"},
		{rule: "localdatetimeType", children: []string{"TIMESTAMP"}, want: "localdatetimeType#2"},
		{rule: "sourceNodeTypeReference", children: []string{"LEFT_PAREN", "sourceNodeTypeAlias", "RIGHT_PAREN"}, want: "sourceNodeTypeReference#1"},
		{rule: "sourceNodeTypeReference", children: []string{"LEFT_PAREN", "RIGHT_PAREN"}, want: "sourceNodeTypeReference#2"},

		// A label's name is not its position: #listValueTypeAlt1 is alternative 3, and
		// the last two carry the grammar's own `Atl` typo.
		{rule: "valueType", contextType: "PredefinedTypeLabelContext", want: "valueType#1"},
		{rule: "valueType", contextType: "ListValueTypeAlt1Context", want: "valueType#3"},
		{rule: "valueType", contextType: "OpenDynamicUnionTypeLabelContext", want: "valueType#7"},
		{rule: "valueType", contextType: "ClosedDynamicUnionTypeAtl1Context", want: "valueType#9"},
		{rule: "valueType", contextType: "ClosedDynamicUnionTypeAtl2Context", want: "valueType#10"},

		// A rule with one alternative has nothing to attribute.
		{rule: "nodeTypeLabelSet", children: []string{"labelSetPhrase"}},
	} {
		t.Run(tc.want+tc.rule, func(t *testing.T) {
			got, err := index.tag(tc.rule, tc.contextType, tc.children)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("a shape matching no alternative is an error", func(t *testing.T) {
		_, err := index.tag("graphTypeSource", "", []string{"AS"})
		require.Error(t, err)
	})

	t.Run("an unknown context for a labelled rule is an error", func(t *testing.T) {
		_, err := index.tag("valueType", "ValueTypeContext", []string{"predefinedType"})
		require.Error(t, err)
	})
}

// TestEBNFMatcher covers the two things the index cannot pin from the grammar: that
// an expression is matched at every length it admits rather than greedily, and that
// element syntax the compiler has never been taught is rejected instead of
// approximated.
func TestEBNFMatcher(t *testing.T) {
	t.Run("a repetition is not committed to greedily", func(t *testing.T) {
		expr, err := parseEBNF("a? a")
		require.NoError(t, err)
		require.True(t, expr.matches([]string{"a"}), "a greedy a? would consume the only child and leave nothing to match")
		require.True(t, expr.matches([]string{"a", "a"}))
		require.False(t, expr.matches([]string{"a", "a", "a"}))
	})

	t.Run("nested groups and choices", func(t *testing.T) {
		expr, err := parseEBNF("x (y | z w)+ q?")
		require.NoError(t, err)
		require.True(t, expr.matches([]string{"x", "y"}))
		require.True(t, expr.matches([]string{"x", "z", "w", "y", "q"}))
		require.False(t, expr.matches([]string{"x"}))
		require.False(t, expr.matches([]string{"x", "z", "q"}))
	})

	for _, alt := range []string{"a*?", "a ~b", "a .", "(a", "a)", "a {pred}?", "a='b'"} {
		t.Run("rejects "+alt, func(t *testing.T) {
			_, err := parseEBNF(alt)
			require.Error(t, err, "unknown element syntax must fail rather than compile to a shape that matches the wrong parses")
		})
	}
}

func parseTag(t *testing.T, tag string) (rule string, alt int) {
	t.Helper()

	rule, number, ok := strings.Cut(tag, "#")
	require.True(t, ok, "%q is not a rule#N tag", tag)
	alt, err := strconv.Atoi(number)
	require.NoError(t, err)
	return rule, alt
}

// ebnf is the fragment of ANTLR's element syntax the graph type rules of GQL.g4
// use: symbols, sequences, choices, and the three repetition suffixes. Anything
// else fails to compile rather than being approximated, because a wrong shape
// attributes a parse to the wrong alternative and so reports coverage that is not
// there.
type ebnf interface {
	// reach maps a set of positions in children to the positions reachable by
	// matching this expression from each of them. Position sets rather than a
	// single cursor, because `x? x` matches one child sequence at two lengths and a
	// cursor would have to guess which.
	reach(children []string, from map[int]bool) map[int]bool
	matches(children []string) bool
	// expand enumerates the child sequences this expression matches, taking `*` and
	// `+` at most twice. The cap can only lose sequences, never invent them, which
	// is the safe direction for its one caller: a witness it finds is real, and a
	// rule it fails to find one for may still have a longer one.
	expand() [][]string
}

type ebnfSymbol string

type ebnfSeq []ebnf

type ebnfChoice []ebnf

// ebnfRepeat is `?` (optional), `*` (optional and repeated) or `+` (repeated).
type ebnfRepeat struct {
	inner    ebnf
	optional bool
	repeated bool
}

func (s ebnfSymbol) reach(children []string, from map[int]bool) map[int]bool {
	to := make(map[int]bool, len(from))
	for pos := range from {
		if pos < len(children) && children[pos] == string(s) {
			to[pos+1] = true
		}
	}
	return to
}

func (s ebnfSeq) reach(children []string, from map[int]bool) map[int]bool {
	for _, item := range s {
		if len(from) == 0 {
			return from
		}
		from = item.reach(children, from)
	}
	return from
}

func (c ebnfChoice) reach(children []string, from map[int]bool) map[int]bool {
	to := make(map[int]bool)
	for _, item := range c {
		for pos := range item.reach(children, from) {
			to[pos] = true
		}
	}
	return to
}

func (r ebnfRepeat) reach(children []string, from map[int]bool) map[int]bool {
	to := make(map[int]bool, len(from))
	if r.optional {
		for pos := range from {
			to[pos] = true
		}
	}
	// Only positions not already reached are expanded again, which both terminates
	// (positions are bounded by len(children)) and tolerates an inner expression
	// that can match nothing.
	for frontier := from; len(frontier) > 0; {
		next := make(map[int]bool)
		for pos := range r.inner.reach(children, frontier) {
			if !to[pos] {
				to[pos] = true
				next[pos] = true
			}
		}
		if !r.repeated {
			break
		}
		frontier = next
	}
	return to
}

func (s ebnfSymbol) matches(children []string) bool { return matches(s, children) }
func (s ebnfSeq) matches(children []string) bool    { return matches(s, children) }
func (c ebnfChoice) matches(children []string) bool { return matches(c, children) }
func (r ebnfRepeat) matches(children []string) bool { return matches(r, children) }

// matches reports whether expr consumes children exactly.
func matches(expr ebnf, children []string) bool {
	return expr.reach(children, map[int]bool{0: true})[len(children)]
}

func (s ebnfSymbol) expand() [][]string { return [][]string{{string(s)}} }

func (s ebnfSeq) expand() [][]string {
	seqs := [][]string{nil}
	for _, item := range s {
		var next [][]string
		for _, prefix := range seqs {
			for _, suffix := range item.expand() {
				next = append(next, append(append([]string{}, prefix...), suffix...))
			}
		}
		seqs = next
	}
	return seqs
}

func (c ebnfChoice) expand() [][]string {
	var seqs [][]string
	for _, item := range c {
		seqs = append(seqs, item.expand()...)
	}
	return seqs
}

func (r ebnfRepeat) expand() [][]string {
	var seqs [][]string
	if r.optional {
		seqs = append(seqs, nil)
	}
	once := r.inner.expand()
	seqs = append(seqs, once...)
	if !r.repeated {
		return seqs
	}
	for _, first := range once {
		for _, second := range once {
			seqs = append(seqs, append(append([]string{}, first...), second...))
		}
	}
	return seqs
}

var (
	reEBNFToken       = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*|\S`)
	reWholeIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// parseEBNF compiles one alternative's element list. The text has already had
// literals, option blocks and the `# AltLabel` removed, so an operator this parser
// does not know is a grammar construct the harness has never been told how to
// attribute — an error, not something to skip.
func parseEBNF(alt string) (ebnf, error) {
	toks := reEBNFToken.FindAllString(alt, -1)
	for _, tok := range toks {
		if !reWholeIdentifier.MatchString(tok) && !isEBNFOperator(tok) {
			return nil, fmt.Errorf("unsupported element syntax %q", tok)
		}
	}

	p := &ebnfParser{toks: toks}
	expr, err := p.choice()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unconsumed %q", strings.Join(p.toks[p.pos:], " "))
	}
	return expr, nil
}

func isEBNFOperator(tok string) bool {
	return len(tok) == 1 && strings.ContainsAny(tok, "()|?*+")
}

type ebnfParser struct {
	toks []string
	pos  int
}

func (p *ebnfParser) peek() string {
	if p.pos >= len(p.toks) {
		return ""
	}
	return p.toks[p.pos]
}

func (p *ebnfParser) next() string {
	tok := p.peek()
	p.pos++
	return tok
}

func (p *ebnfParser) choice() (ebnf, error) {
	first, err := p.sequence()
	if err != nil {
		return nil, err
	}
	if p.peek() != "|" {
		return first, nil
	}

	alts := []ebnf{first}
	for p.peek() == "|" {
		p.pos++
		next, err := p.sequence()
		if err != nil {
			return nil, err
		}
		alts = append(alts, next)
	}
	return ebnfChoice(alts), nil
}

func (p *ebnfParser) sequence() (ebnf, error) {
	var items []ebnf
	for tok := p.peek(); tok != "" && tok != "|" && tok != ")"; tok = p.peek() {
		item, err := p.item()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 1 {
		return items[0], nil
	}
	// An empty sequence is an empty ebnfSeq, which reaches its start positions
	// unchanged; ANTLR does allow an empty alternative.
	return ebnfSeq(items), nil
}

func (p *ebnfParser) item() (ebnf, error) {
	var inner ebnf
	switch tok := p.next(); {
	case tok == "(":
		group, err := p.choice()
		if err != nil {
			return nil, err
		}
		if closer := p.next(); closer != ")" {
			return nil, fmt.Errorf("group is not closed by %q", closer)
		}
		inner = group
	case reWholeIdentifier.MatchString(tok):
		inner = ebnfSymbol(tok)
	default:
		return nil, fmt.Errorf("expected a symbol or a group, got %q", tok)
	}

	switch p.peek() {
	case "?":
		p.pos++
		return ebnfRepeat{inner: inner, optional: true}, nil
	case "*":
		p.pos++
		return ebnfRepeat{inner: inner, optional: true, repeated: true}, nil
	case "+":
		p.pos++
		return ebnfRepeat{inner: inner, repeated: true}, nil
	}
	return inner, nil
}
