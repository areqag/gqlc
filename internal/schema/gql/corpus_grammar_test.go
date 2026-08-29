package gql_test

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/require"

	"github.com/areqag/gqlc/internal/grammar/gql/gen"
)

const grammarPath = "../../grammar/gql/GQL.g4"

// grammarSHA256 is the vendored grammar's hash, recorded in
// internal/grammar/gql/SOURCE.md as byte-identical to upstream
// antlr/grammars-v4. Every claim in that file — the artefact list, the ISO
// production-name comparison, the reasons the word "faithful" was withdrawn —
// is about the upstream file, so an in-place edit here would silently make the
// note describe a grammar we no longer have.
const grammarSHA256 = "e1b4a24c6b88dedddc0a1fff97df0fc30bf118cea51539e26d71c717cb737bbf"

// TestVendoredGrammarIsUnmodified fails on any edit to GQL.g4. Editing it is not
// forbidden — but the change belongs upstream first, and re-pinning here without
// re-reading SOURCE.md is the failure mode this catches. It cannot see upstream
// drift; that is a network check, and gqlc-4jm's scope.
func TestVendoredGrammarIsUnmodified(t *testing.T) {
	src, err := os.ReadFile(grammarPath)
	require.NoError(t, err)
	require.Equal(t, grammarSHA256, fmt.Sprintf("%x", sha256.Sum256(src)),
		"GQL.g4 no longer matches the hash internal/grammar/gql/SOURCE.md vendors it under: "+
			"if the edit is intended, re-verify that file's provenance section and re-pin grammarSHA256")
}

// coverageRoots is the one rule every corpus file must enter, and the corpus'
// obligation is its transitive closure — so the obligation tracks the grammar
// rather than a hand-maintained list of constructs.
//
// Naming graphTypeSource and nestedGraphTypeSpecification as further roots looks
// harmless and is not: it lets the invisible-alternative criterion suppose a file
// could enter a body directly, when measureCoverage requires every file to come in
// through the statement. With the extra roots, deleting `graphTypeSource#3` still
// reached everything via the body root and so scored as invisible; from the real
// single entry point it loses 110 of the 133 rules, which is the rule gate
// demanding it as loudly as anything in the grammar.
var coverageRoots = []string{statementRule}

// coverageCuts are frontier rules: still required to be covered, but not
// descended into.
//
// graphExpression is reached by `graphTypeLikeGraph : LIKE graphExpression` and
// drags in 545 of the grammar's 574 rules — the whole query and expression
// surface, which is out of scope here and has its own corpus (the vendored
// openCypher TCK).
//
// nonReservedWords is reached by `regularIdentifier : REGULAR_IDENTIFIER |
// nonReservedWords`: ~200 keyword tokens usable as an identifier. That is
// identifier lexis, not a schema construct.
//
// A cut still has to be entered by some corpus file, so a cut that stops being
// reachable fails rather than silently masking a subtree. Adding a third cut
// needs the grammar path that justifies it.
//
// That brake is not proportional, which is the thing to keep in view when reading
// the numbers below: with no cuts the closure is 545 of the grammar's 574 rules,
// and graphExpression alone erases 412 rules and 186 tokens for the price of one
// three-word file. nonReservedWords erases 0 rules and 30 tokens, so the
// identifier-lexis cut is genuinely cheap — it names ~200 keyword tokens but only
// 30 are reachable nowhere else in the schema grammar. The erasure is not pinned
// separately: wantObligationRules and wantObligationTokens already carry it, and
// being membership goldens rather than counts, they fail a third cut by naming the
// rules and tokens it erased rather than by reporting a smaller number.
var coverageCuts = []string{"graphExpression", "nonReservedWords"}

// The obligation sets are pinned because the gate below can only catch the closure
// growing (a new rule is uncovered until a corpus file enters it). A closure that
// shrinks — a rule deleted, a reference dropped, a cut swallowing more than it used
// to — would otherwise pass silently while testing less. A deliberate grammar change
// updates these and re-reads the uncovered list.
//
// Membership rather than the size, for the same reason as
// wantInvisibleAlternatives: a size pin passes for any wrong 133, and it cannot
// answer the question the phase-2 area split asks of it. That split partitions these
// two sets across five authors, and a partition that names a rule outside the closure
// while dropping one inside it sums to 133 either way — so every count-based check of
// the split passes while an author chases a name that does not exist and a real
// obligation goes unowned. Only the sets themselves can be diffed against it.
//
// Sorted, unlike wantInvisibleAlternatives, which is ordered as its gate emits it.
// These two are collected into maps, so there is no emission order to preserve and
// Go randomises what is left. ElementsMatch does not care, but the failure message
// lists the differing names in the order it was handed them, and a diff that reads
// differently on every run is harder to act on than one that does not.
var (
	wantObligationRules = []string{
		"absoluteCatalogSchemaReference",
		"absoluteDirectoryPath",
		"approximateNumericType",
		"arcTypePointingLeft",
		"arcTypePointingRight",
		"arcTypeUndirected",
		"binaryExactNumericType",
		"bindingTableReferenceValueType",
		"bindingTableType",
		"booleanType",
		"byteStringType",
		"catalogGraphTypeParentAndName",
		"catalogObjectParentReference",
		"characterStringType",
		"closedEdgeReferenceValueType",
		"closedGraphReferenceValueType",
		"closedNodeReferenceValueType",
		"connectorPointingRight",
		"connectorUndirected",
		"copyOfGraphType",
		"createGraphTypeStatement",
		"datetimeType",
		"dateType",
		"decimalExactNumericType",
		"destinationNodeTypeAlias",
		"destinationNodeTypeReference",
		"directoryName",
		"edgeKind",
		"edgeReferenceValueType",
		"edgeSynonym",
		"edgeTypeFiller",
		"edgeTypeImpliedContent",
		"edgeTypeKeyLabelSet",
		"edgeTypeLabelSet",
		"edgeTypeName",
		"edgeTypePattern",
		"edgeTypePatternDirected",
		"edgeTypePatternPointingLeft",
		"edgeTypePatternPointingRight",
		"edgeTypePatternUndirected",
		"edgeTypePhrase",
		"edgeTypePhraseFiller",
		"edgeTypePropertyTypes",
		"edgeTypeSpecification",
		"elementTypeList",
		"elementTypeSpecification",
		"emptyType",
		"endpointPair",
		"endpointPairDirected",
		"endpointPairPhrase",
		"endpointPairPointingLeft",
		"endpointPairPointingRight",
		"endpointPairUndirected",
		"exactNumericType",
		"fieldName",
		"fieldType",
		"fieldTypeList",
		"fieldTypesSpecification",
		"fixedLength",
		"graphExpression",
		"graphReferenceValueType",
		"graphTypeLikeGraph",
		"graphTypeName",
		"graphTypeReference",
		"graphTypeSource",
		"graphTypeSpecificationBody",
		"identifier",
		"immaterialValueType",
		"isOrColon",
		"labelName",
		"labelSetPhrase",
		"labelSetSpecification",
		"listValueTypeName",
		"listValueTypeNameSynonym",
		"localdatetimeType",
		"localNodeTypeAlias",
		"localtimeType",
		"maxLength",
		"minLength",
		"nestedGraphTypeSpecification",
		"nodeReferenceValueType",
		"nodeSynonym",
		"nodeTypeFiller",
		"nodeTypeImpliedContent",
		"nodeTypeKeyLabelSet",
		"nodeTypeLabelSet",
		"nodeTypeName",
		"nodeTypePattern",
		"nodeTypePhrase",
		"nodeTypePhraseFiller",
		"nodeTypePropertyTypes",
		"nodeTypeSpecification",
		"nonReservedWords",
		"notNull",
		"nullType",
		"numericType",
		"objectName",
		"openEdgeReferenceValueType",
		"openGraphReferenceValueType",
		"openNodeReferenceValueType",
		"pathValueType",
		"precision",
		"predefinedSchemaReference",
		"predefinedType",
		"propertyName",
		"propertyType",
		"propertyTypeList",
		"propertyTypesSpecification",
		"propertyValueType",
		"recordType",
		"referenceParameterSpecification",
		"referenceValueType",
		"regularIdentifier",
		"relativeCatalogSchemaReference",
		"relativeDirectoryPath",
		"scale",
		"schemaName",
		"schemaReference",
		"signedBinaryExactNumericType",
		"simpleDirectoryPath",
		"sourceNodeTypeAlias",
		"sourceNodeTypeReference",
		"temporalDurationQualifier",
		"temporalDurationType",
		"temporalInstantType",
		"temporalType",
		"timeType",
		"typed",
		"unsignedBinaryExactNumericType",
		"unsignedDecimalInteger",
		"unsignedInteger",
		"valueType",
		"verboseBinaryExactNumericType",
	}
	wantObligationTokens = []string{
		"ACCENT_QUOTED_CHARACTER_SEQUENCE",
		"AMPERSAND",
		"ANY",
		"ARRAY",
		"AS",
		"BIG",
		"BIGINT",
		"BINARY",
		"BINDING",
		"BOOL",
		"BOOLEAN",
		"BRACKET_RIGHT_ARROW",
		"BYTES",
		"CHAR",
		"COLON",
		"COMMA",
		"CONNECTING",
		"COPY",
		"CREATE",
		"CURRENT_SCHEMA",
		"DATE",
		"DATETIME",
		"DAY",
		"DEC",
		"DECIMAL",
		"DIRECTED",
		"DOUBLE",
		"DOUBLE_COLON",
		"DOUBLE_PERIOD",
		"DOUBLE_QUOTED_CHARACTER_SEQUENCE",
		"DURATION",
		"EDGE",
		"EXISTS",
		"FLOAT",
		"FLOAT128",
		"FLOAT16",
		"FLOAT256",
		"FLOAT32",
		"FLOAT64",
		"GRAPH",
		"HOME_SCHEMA",
		"IF",
		"IMPLIES",
		"INT",
		"INT128",
		"INT16",
		"INT256",
		"INT32",
		"INT64",
		"INT8",
		"INTEGER",
		"INTEGER128",
		"INTEGER16",
		"INTEGER256",
		"INTEGER32",
		"INTEGER64",
		"INTEGER8",
		"IS",
		"LABEL",
		"LABELS",
		"LEFT_ANGLE_BRACKET",
		"LEFT_ARROW",
		"LEFT_ARROW_BRACKET",
		"LEFT_BRACE",
		"LEFT_BRACKET",
		"LEFT_PAREN",
		"LIKE",
		"LIST",
		"LOCAL",
		"MINUS_LEFT_BRACKET",
		"MONTH",
		"NODE",
		"NOT",
		"NOTHING",
		"NULL_KW",
		"OF",
		"OR",
		"PATH",
		"PERIOD",
		"PRECISION",
		"PROPERTY",
		"REAL",
		"RECORD",
		"REGULAR_IDENTIFIER",
		"RELATIONSHIP",
		"REPLACE",
		"RIGHT_ANGLE_BRACKET",
		"RIGHT_ARROW",
		"RIGHT_BRACE",
		"RIGHT_BRACKET",
		"RIGHT_BRACKET_MINUS",
		"RIGHT_BRACKET_TILDE",
		"RIGHT_PAREN",
		"SECOND",
		"SIGNED",
		"SMALL",
		"SMALLINT",
		"SOLIDUS",
		"STRING",
		"SUBSTITUTED_PARAMETER_REFERENCE",
		"TABLE",
		"TILDE",
		"TILDE_LEFT_BRACKET",
		"TIME",
		"TIMESTAMP",
		"TO",
		"TYPE",
		"TYPED",
		"UBIGINT",
		"UINT",
		"UINT128",
		"UINT16",
		"UINT256",
		"UINT32",
		"UINT64",
		"UINT8",
		"UNDIRECTED",
		"UNSIGNED",
		"UNSIGNED_BINARY_INTEGER",
		"UNSIGNED_DECIMAL_INTEGER",
		"UNSIGNED_HEXADECIMAL_INTEGER",
		"UNSIGNED_OCTAL_INTEGER",
		"USMALLINT",
		"VALUE",
		"VARBINARY",
		"VARCHAR",
		"VERTEX",
		"VERTICAL_BAR",
		"WITH",
		"WITHOUT",
		"YEAR",
		"ZONE",
		"ZONED",
	}
)

// wantInvisibleAlternatives is the third obligation set's *candidate* set (see
// invisibleAlternatives). What the corpus owes is this minus
// alternativeExemptions, because being invisible to the other two gates and being
// reachable at all are different questions.
//
// Membership rather than the size, because a size pin passes for any wrong 47, and
// the failure it is blind to is the one parallel authoring produces: one candidate
// gained and one lost in the same merge. Ordered as the gate emits it — rule name,
// then alternative number — so a diff here reads in the order the worklist does.
var wantInvisibleAlternatives = []string{
	"absoluteCatalogSchemaReference#1",
	"catalogObjectParentReference#2",
	"connectorPointingRight#1",
	"connectorUndirected#1",
	"datetimeType#1",
	"datetimeType#2",
	"destinationNodeTypeReference#1",
	"destinationNodeTypeReference#2",
	"edgeTypeFiller#2",
	"edgeTypeImpliedContent#1",
	"edgeTypeImpliedContent#2",
	"edgeTypeImpliedContent#3",
	"edgeTypePhraseFiller#1",
	"edgeTypePhraseFiller#2",
	"elementTypeSpecification#1",
	"elementTypeSpecification#2",
	"emptyType#1",
	"graphTypeReference#1",
	"graphTypeReference#2",
	"identifier#1",
	"localdatetimeType#1",
	"localdatetimeType#2",
	"localtimeType#1",
	"localtimeType#2",
	"nodeTypeFiller#2",
	"nodeTypeImpliedContent#1",
	"nodeTypeImpliedContent#2",
	"nodeTypeImpliedContent#3",
	"nodeTypePhraseFiller#1",
	"nodeTypePhraseFiller#2",
	"predefinedSchemaReference#3",
	"recordType#1",
	"recordType#2",
	"schemaReference#3",
	"sourceNodeTypeReference#1",
	"sourceNodeTypeReference#2",
	"timeType#1",
	"timeType#2",
	"unsignedInteger#1",
	"valueType#3",
	"valueType#4",
	"valueType#5",
	"valueType#7",
	"valueType#8",
	"valueType#9",
	"valueType#10",
	"verboseBinaryExactNumericType#8",
}

// The grammar is scanned as text rather than through the generated parser's ATN
// because the ATN has no notion of the source-level rule references this closure
// walks: it is a flattened state machine, and reconstructing "rule x names rule
// y" from it is strictly harder than reading the .g4.
var (
	reLineComment  = regexp.MustCompile(`//[^\n]*`)
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reRuleHead     = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(options\s*\{[^}]*\})?\s*$`)
	reRuleEnd      = regexp.MustCompile(`^\s*;\s*$`)
	reLiteral      = regexp.MustCompile(`'(\\.|[^'])*'`)
	reAltLabel     = regexp.MustCompile(`#\s*[A-Za-z_][A-Za-z0-9_]*`)
	reOptions      = regexp.MustCompile(`\boptions\s*\{[^}]*\}`)
	reElementLabel = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	reIdentifier   = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
)

// scanGrammarRules returns every parser rule in GQL.g4 as name -> body text. A
// rule block is a line holding nothing but the rule name, then body lines up to
// a line holding nothing but a semicolon.
func scanGrammarRules() (map[string]string, error) {
	src, err := os.ReadFile(grammarPath)
	if err != nil {
		return nil, err
	}

	text := reLineComment.ReplaceAllString(string(src), "")
	text = reBlockComment.ReplaceAllString(text, "")

	rules := make(map[string]string)
	name := ""
	var body []string
	for _, line := range strings.Split(text, "\n") {
		if name == "" {
			if m := reRuleHead.FindStringSubmatch(line); m != nil {
				name = m[1]
				body = nil
			}
			continue
		}
		body = append(body, line)
		if reRuleEnd.MatchString(line) {
			rules[name] = strings.Join(body, "\n")
			name = ""
		}
	}

	parserRules := make(map[string]string, len(rules))
	for rule, ruleBody := range rules {
		if isRuleName(rule) {
			parserRules[rule] = ruleBody
		}
	}
	if len(parserRules) == 0 {
		return nil, fmt.Errorf("%s holds no parser rule; the rule-block scan is broken", grammarPath)
	}
	return parserRules, nil
}

// isRuleName reports whether an identifier is an ANTLR parser rule rather than a
// token: parser rules start lower-case, tokens upper-case.
func isRuleName(id string) bool {
	return id[0] >= 'a' && id[0] <= 'z'
}

// cleanRuleBody drops everything in a rule body that looks like a reference but
// is not: quoted literals, alternative labels (`# Alt`) and options blocks. It
// runs before the body is split on `|` so that a literal `'|'` cannot be mistaken
// for an alternative separator.
func cleanRuleBody(body string) string {
	return reAltLabel.ReplaceAllString(withoutLiterals(body), " ")
}

func withoutLiterals(body string) string {
	return reOptions.ReplaceAllString(reLiteral.ReplaceAllString(body, " "), " ")
}

// ruleAlternatives splits a rule body into its alternatives' element text and
// their `# AltLabel`s, one entry each per alternative. The two come from a single
// split so they cannot fall out of step: a label's position in the rule is the
// alternative number, and that is not derivable from the label text —
// `#listValueTypeAlt1` is alternative 3.
func ruleAlternatives(body string) (elements, labels []string) {
	for _, alt := range splitAlternatives(withoutLiterals(body)) {
		label := ""
		if m := reAltLabel.FindString(alt); m != "" {
			label = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m), "#"))
			alt = reAltLabel.ReplaceAllString(alt, " ")
		}
		elements = append(elements, alt)
		labels = append(labels, label)
	}
	return elements, labels
}

// grammarRefs splits a cleaned rule body into the parser rules and the tokens it
// names. Element labels (`name=rule`) name a tree field, not a reference.
func grammarRefs(body string, parserRules map[string]string) (rules, tokens map[string]bool) {
	labels := make(map[string]bool)
	for _, m := range reElementLabel.FindAllStringSubmatch(body, -1) {
		labels[m[1]] = true
	}

	rules, tokens = make(map[string]bool), make(map[string]bool)
	for _, id := range reIdentifier.FindAllString(body, -1) {
		switch {
		case labels[id]:
		case isRuleName(id):
			if _, ok := parserRules[id]; ok {
				rules[id] = true
			}
		default:
			tokens[id] = true
		}
	}
	return rules, tokens
}

// obligation is what the corpus must enter: every reachable parser rule, every
// token named by a reachable non-frontier rule, and every required alternative.
type obligation struct {
	rules  map[string]bool
	tokens map[string]bool
	// tokenRefs maps a token to the reachable rules naming it, which is how an
	// uncovered token is reported against a rule an author can write a file for.
	tokenRefs map[string][]string
	// invisible are the tags of alternatives the rule and token gates cannot
	// demand, grouped by rule in nonFrontier's order and ascending by alternative
	// within a rule. It is the candidate set, not the obligation.
	invisible []string
	// required is invisible minus alternativeExemptions, which is what the corpus
	// actually owes: being invisible to the other two gates and being reachable at
	// all are different questions.
	required []string
	// alternatives attributes a parse tree node to one of these tags.
	alternatives *alternativeIndex
	// nonFrontier are the reachable rules that were descended into, so the
	// alternative scan reads the same closure the rule and token sets came from.
	nonFrontier []string
}

// alternativeExemption records a candidate alternative no corpus file can take,
// because ANTLR's ALL(*) hands its input to a lower-numbered alternative at every
// position it could appear. Ordering makes such an alternative dead, not merely
// uncovered, so demanding a file for it would make the gate uncloseable.
//
// stolenBy names the alternative that wins instead, and joins the required set so
// that TestCorpusGrammarCoverage demands it by name and the worklist asks an author
// for it. That is what stops an exemption from excusing an untested construct: if
// the thief is uncovered too, nothing exercises the spelling at all. The pair is
// swept in the other direction by TestAlternativeExemptions — an exemption whose tag
// becomes covered is stale, which is how a grammar fix that revives the alternative
// gets noticed.
//
// Requiring the thief is a necessary condition on an exemption, not a sufficient
// one: it would be satisfied by naming any alternative the corpus happens to cover.
// What makes an entry answerable is why, which is prose a reviewer reads, so expect
// this list to be reviewed rather than merely to pass.
//
// The list is written by hand and stays so; gqlc-h9n.14 asked whether the partition
// could be derived instead, and closed on the finding that a sound and complete
// derivation cannot exist. An exemption is a universal — every input that would take
// the alternative is taken by another — and deciding it for alternatives whose
// sub-languages are context-free is CFL inclusion, which is undecidable. The reduction
// is direct: put two grammars in Greibach normal form, which is left-recursion-free
// and so within what ANTLR accepts, as alternatives 1 and 2 of a rule anchored at EOF.
// Prediction then selects alternative 2 on exactly the inputs in L2 \ L1, so the
// alternative is dead iff L2 is contained in L1.
//
// Both mechanisms that were proposed sit on the wrong side of that bound rather than
// near it. PredictionModeLLExactAmbigDetection reports that two alternatives were
// ambiguous on one input at one position, which is the existential of the universal an
// exemption claims, and closing the gap is the undecidable step rather than an
// engineering one — the ground amendment A10 rejected it on. Graph reachability over
// the ATN is silent by construction: an alternative's entry state is an epsilon
// successor of its decision state, so it is reachable exactly when its rule is, and
// connectorUndirected is in wantObligationRules. It answers "is this alternative in
// the grammar", which was never in doubt.
//
// So what the hand-list gives up is completeness, not honesty. It cannot over-claim,
// TestAlternativeExemptions failing on an exemption the corpus covers. Under-claiming
// is the benign direction and announces itself: a dead alternative nobody has exempted
// is a demand no file satisfies, and authoringGuidance sends the author who hits it to
// read the grammar rather than widen the harness. That path has since found a second
// one — optionalityExemptions' entry in corpus_branch_test.go was written after an
// author's spelling kept landing on the branch already covered.
type alternativeExemption struct {
	tag      string
	stolenBy string
	bead     string
	why      string
}

var alternativeExemptions = []alternativeExemption{
	{
		tag:      "connectorUndirected#1",
		stolenBy: "connectorPointingRight#1",
		bead:     "gqlc-h9n.10",
		why:      "endpointPair lists endpointPairDirected first (GQL.g4:1637) and endpointPairPhrase is a sibling of edgeTypePhraseFiller, not a child, so prediction for endpointPair never sees the edge kind: `UNDIRECTED EDGE TYPE e ... CONNECTING (a TO b)` yields a directed endpoint pair, and no file can route a bare TO to connectorUndirected",
	},
}

// closure walks the reachable rules from coverageRoots, descending into every rule
// except cuts. cutsReached records which cuts were actually hit.
func closure(parserRules map[string]string, cuts map[string]bool) (rules, tokens map[string]bool, tokenRefs map[string][]string, nonFrontier []string, cutsReached map[string]bool) {
	rules = make(map[string]bool, len(coverageRoots))
	queue := make([]string, 0, len(coverageRoots))
	for _, root := range coverageRoots {
		rules[root] = true
		queue = append(queue, root)
	}

	tokens = make(map[string]bool)
	tokenRefs = make(map[string][]string)
	cutsReached = make(map[string]bool, len(cuts))
	for len(queue) > 0 {
		rule := queue[0]
		queue = queue[1:]
		if cuts[rule] {
			cutsReached[rule] = true
			continue
		}
		nonFrontier = append(nonFrontier, rule)

		refRules, refTokens := grammarRefs(cleanRuleBody(parserRules[rule]), parserRules)
		for token := range refTokens {
			tokens[token] = true
			tokenRefs[token] = append(tokenRefs[token], rule)
		}
		for ref := range refRules {
			if !rules[ref] {
				rules[ref] = true
				queue = append(queue, ref)
			}
		}
	}
	return rules, tokens, tokenRefs, nonFrontier, cutsReached
}

// loadObligation memoises the closure, which every corpus file's walk needs and
// which costs one recomputation per grammar alternative. It is a pure function of
// a checked-in file, so one computation per test binary is the whole story.
var loadObligation = sync.OnceValues(computeObligation)

// grammarObligation is the only way to obtain the obligation, so that the pinned
// sets hold for every caller. They are asserted here rather than in a test of
// their own because an empty obligation fails open: TestCorpusGrammarCoverage
// would report "rules 0/0" and pass, and a broken scanner would make every gate
// green.
func grammarObligation(t coverageT) obligation {
	t.Helper()

	got, err := loadObligation()
	require.NoError(t, err)
	require.ElementsMatch(t, wantObligationRules, slices.Sorted(maps.Keys(got.rules)),
		"the reachable rule set changed; re-read the grammar before repinning")
	require.ElementsMatch(t, wantObligationTokens, slices.Sorted(maps.Keys(got.tokens)),
		"the reachable token set changed; re-read the grammar before repinning")
	require.ElementsMatch(t, wantInvisibleAlternatives, got.invisible,
		"the set of alternatives the rule and token gates cannot demand changed:\n%s",
		strings.Join(got.invisible, "\n"))
	return got
}

// computeObligation walks the reachability closure from coverageRoots, stopping at
// coverageCuts, and subtracts the exemptions from the candidate alternatives.
func computeObligation() (obligation, error) {
	parserRules, err := scanGrammarRules()
	if err != nil {
		return obligation{}, err
	}

	for _, rule := range append(append([]string{}, coverageCuts...), coverageRoots...) {
		if _, ok := parserRules[rule]; !ok {
			return obligation{}, fmt.Errorf("%q is named as a cut or a root but is not a grammar rule", rule)
		}
	}

	rules, tokens, tokenRefs, nonFrontier, cutsReached := closure(parserRules, cutsSet())
	for _, cut := range coverageCuts {
		if !cutsReached[cut] {
			return obligation{}, fmt.Errorf("frontier cut %q is no longer reachable: drop it from coverageCuts", cut)
		}
	}
	for _, refs := range tokenRefs {
		sort.Strings(refs)
	}
	sort.Strings(nonFrontier)

	invisible := invisibleAlternatives(parserRules, nonFrontier, len(rules), len(tokens))
	index := newAlternativeIndex(parserRules)
	required, err := requiredAlternatives(invisible, index, alternativeExemptions)
	if err != nil {
		return obligation{}, err
	}

	return obligation{
		rules:        rules,
		tokens:       tokens,
		tokenRefs:    tokenRefs,
		invisible:    invisible,
		required:     required,
		alternatives: index,
		nonFrontier:  nonFrontier,
	}, nil
}

// requiredAlternatives subtracts each exemption's tag from the candidate set and
// adds the alternative that steals its input. An exemption naming a tag that is not
// a candidate is rejected rather than ignored: the two lists are registries of the
// same thing, and an exemption that matches nothing silently stops excusing
// whatever it was written for.
//
// exemptions is a parameter rather than a read of the package-level list so that
// TestRequiredAlternativesDemandsTheThief can supply one whose thief is outside the
// candidate set. Today's only exemption has a thief that is itself a candidate, so
// the append below is a no-op against the real list and unpinnable through it.
// Every caller outside that test passes alternativeExemptions.
func requiredAlternatives(invisible []string, index *alternativeIndex, exemptions []alternativeExemption) ([]string, error) {
	candidates := make(map[string]bool, len(invisible))
	for _, tag := range invisible {
		candidates[tag] = true
		if why := index.blocked(ruleOf(tag)); why != "" {
			return nil, fmt.Errorf("%s cannot be attributed mechanically, so no corpus file can discharge it: %s", tag, why)
		}
	}

	exempt := make(map[string]bool, len(exemptions))
	for _, ex := range exemptions {
		switch {
		case !candidates[ex.tag]:
			return nil, fmt.Errorf("exemption %q is not a candidate alternative; delete it", ex.tag)
		case exempt[ex.tag]:
			return nil, fmt.Errorf("duplicate exemption %q", ex.tag)
		case ex.stolenBy == "" || ex.bead == "" || ex.why == "":
			return nil, fmt.Errorf("exemption %q needs the alternative that takes its input, a bead, and why", ex.tag)
		case ex.stolenBy == ex.tag:
			return nil, fmt.Errorf("exemption %q cannot be stolen by itself", ex.tag)
		}
		exempt[ex.tag] = true
	}

	required := make([]string, 0, len(invisible)-len(exempt))
	demanded := make(map[string]bool, len(invisible))
	for _, tag := range invisible {
		if !exempt[tag] {
			required = append(required, tag)
			demanded[tag] = true
		}
	}
	// A stolenBy that is itself a candidate is already here, so this is a no-op in the
	// usual case. It is done anyway so that a grammar change moving the thief out of
	// the candidate set cannot quietly retire the demand along with it.
	for _, ex := range exemptions {
		if !demanded[ex.stolenBy] {
			required = append(required, ex.stolenBy)
			demanded[ex.stolenBy] = true
		}
	}
	return required, nil
}

// ruleOf splits the rule name off a `rule#N` tag.
func ruleOf(tag string) string {
	rule, _, _ := strings.Cut(tag, "#")
	return rule
}

// invisibleAlternatives returns the tags of reachable alternatives that the rule
// and token gates provably cannot demand, by those gates' own definition: delete
// the alternative from its rule, recompute the closure from the same roots and
// cuts, and flag it when every reachable rule and token is still reached. If they
// are, a corpus can satisfy both gates without any file ever taking it — and an
// alternative no gate can demand is exactly the blind spot that let the listener
// diverge from the grammar three times.
//
// The obligation is grammar-wide, so the test has to be. Comparing an
// alternative's symbols against its *siblings'* looks equivalent and is not: a
// symbol unique among siblings can still be reached through an unrelated rule.
// elementTypeSpecification#1 is the case that matters, and it is not a corner —
// nodeTypeSpecification is also referenced by the NODE(...) reference value type
// (GQL.g4:1944), so a corpus of edge declarations plus one NODE-typed property
// satisfies both gates with a node type declaration in an element list never
// parsed. The sibling test finds 17 of these; this one finds 48.
//
// Tags are `rule#N`, N counting alternatives from 1 in source order, emitted in
// nonFrontier's order. They are deliberately not sorted afterwards: `#10` sorts
// before `#3` as a string, and this list is read by hand.
func invisibleAlternatives(parserRules map[string]string, nonFrontier []string, wantRules, wantTokens int) []string {
	var invisible []string
	for _, rule := range nonFrontier {
		alts := splitAlternatives(cleanRuleBody(parserRules[rule]))
		if len(alts) < 2 {
			continue
		}

		for i := range alts {
			// Deleting an alternative can only shrink the closure, so counts settle it;
			// no set comparison is needed.
			rules, tokens, _, _, _ := closure(withoutAlternative(parserRules, rule, alts, i), cutsSet())
			if len(rules) == wantRules && len(tokens) == wantTokens {
				invisible = append(invisible, fmt.Sprintf("%s#%d", rule, i+1))
			}
		}
	}
	return invisible
}

// withoutAlternative copies parserRules with one rule's body replaced by its
// alternatives minus the i'th. The copy is cleaned text rather than grammar
// source, which closure tolerates because cleanRuleBody only ever removes.
func withoutAlternative(parserRules map[string]string, rule string, alts []string, i int) map[string]string {
	kept := make([]string, 0, len(alts)-1)
	for j, alt := range alts {
		if j != i {
			kept = append(kept, alt)
		}
	}

	without := make(map[string]string, len(parserRules))
	for r, body := range parserRules {
		without[r] = body
	}
	without[rule] = strings.Join(kept, " | ")
	return without
}

// splitAlternatives splits a cleaned rule body on its top-level `|`, dropping the
// leading `:` and trailing `;`. Nested `|` inside a group belongs to that group,
// not to the rule, so only depth zero separates.
func splitAlternatives(body string) []string {
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, ";")
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, ":")

	var alts []string
	depth, start := 0, 0
	for i, ch := range body {
		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '|':
			if depth == 0 {
				alts = append(alts, body[start:i])
				start = i + 1
			}
		}
	}
	return append(alts, body[start:])
}

var reSectionHeader = regexp.MustCompile(`^//\s*(\d+(?:\.\d+)?\s+\S.*?)\s*$`)

// scanRuleSections maps each parser rule to the ISO clause heading it is declared
// under (`// 18.3 <edge type specification>`). Unlike scanGrammarRules this reads
// the raw text, because the headings are comments; a commented-out rule block
// cannot be mistaken for a rule head since a `//` line matches neither pattern.
func scanRuleSections(t *testing.T) map[string]string {
	t.Helper()

	src, err := os.ReadFile(grammarPath)
	require.NoError(t, err)

	sections := make(map[string]string)
	heading := ""
	inRule := false
	for _, line := range strings.Split(string(src), "\n") {
		switch {
		case reSectionHeader.MatchString(line):
			heading = reSectionHeader.FindStringSubmatch(line)[1]
		case inRule:
			inRule = !reRuleEnd.MatchString(line)
		default:
			if m := reRuleHead.FindStringSubmatch(line); m != nil {
				sections[m[1]] = heading
				inRule = true
			}
		}
	}
	require.NotEmpty(t, sections)
	return sections
}

// TestGrammarObligation cross-checks every name in the closure against the
// generated parser's own tables. That is what proves the text scan reads the
// grammar the way ANTLR does: a scanner that picked up a stray identifier, or
// missed a rename, produces a name the parser does not know. The closure's sizes
// are pinned inside grammarObligation, so they hold for every caller.
func TestGrammarObligation(t *testing.T) {
	got := grammarObligation(t)

	ruleNames, symbolicNames := parserNameTables()
	known := make(map[string]bool, len(ruleNames)+len(symbolicNames))
	for _, name := range ruleNames {
		known[name] = true
	}
	for rule := range got.rules {
		require.True(t, known[rule], "scanned rule %q is not a rule of the generated parser", rule)
	}

	known = make(map[string]bool, len(symbolicNames))
	for _, name := range symbolicNames {
		known[name] = true
	}
	for token := range got.tokens {
		require.True(t, known[token], "scanned token %q is not a token of the generated parser", token)
	}
}

func cutsSet() map[string]bool {
	cuts := make(map[string]bool, len(coverageCuts))
	for _, cut := range coverageCuts {
		cuts[cut] = true
	}
	return cuts
}

// parserNameTables returns the generated parser's rule-index and token-type name
// tables, which the coverage collector maps parse-tree indices through.
func parserNameTables() (ruleNames, symbolicNames []string) {
	_, p := newGrammarParser("")
	return p.GetRuleNames(), p.GetSymbolicNames()
}

func newGrammarParser(src string) (*gen.GQLLexer, *gen.GQLParser) {
	lex := gen.NewGQLLexer(antlr.NewInputStream(src))
	return lex, gen.NewGQLParser(antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel))
}
