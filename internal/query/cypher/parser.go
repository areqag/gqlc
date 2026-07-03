package cypher

import (
	"io"

	"github.com/antlr4-go/antlr/v4"

	"github.com/areqag/gqlc/internal/grammar/cypher/gen"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
)

// Option configures a cypher parser (Stage 14). The zero-value option set
// (cypher.New() with no arguments) is identical to the pre-Stage-14
// behaviour except at CALL clauses: an empty procsig.Registry misses on
// every lookup, so a CALL against a registry-less parser raises
// ErrUnknownProcedure at the fail-site. Every other clause is
// registry-independent.
type Option func(*config)

// config holds a parser's per-instance settings — today, the procedure
// signature registry.
type config struct {
	registry procsig.Registry
}

// WithRegistry supplies the procedure signature registry the parser
// consults at each CALL clause. Signatures are declared per-scenario in
// the TCK via the background step "there exists a procedure
// <name>(<params>) :: (<results>)", so the godog step definition builds
// a procsig.Registry from the parsed table and passes it here. Real
// callers supply their own registry from an external declaration file
// (deferred to a follow-up bead per Stage-14 spec §8).
func WithRegistry(r procsig.Registry) Option {
	return func(c *config) { c.registry = r }
}

type parser struct {
	cfg config
}

// New returns a query.Parser that lowers a single read query from openCypher
// into query.Query, per the Stage-0 spec. It mirrors internal/schema/gql.
//
// Stage 14 widens the constructor to accept functional options: today the
// only option is WithRegistry, which supplies the procedure signature
// registry consulted at CALL clauses. Callers that pass no options
// (cypher.New()) get the pre-Stage-14 behaviour on every query that does
// not contain CALL; a CALL against a registry-less parser raises
// ErrUnknownProcedure at the fail-site.
func New(opts ...Option) query.Parser {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return parser{cfg: c}
}

// Parse wires the ANTLR lexer, parser and a single syntax-error sink (mirroring
// gql), walks the tree to collect the read core, and lowers it into query.Query
// via build(). A syntax error, an unsupported construct, or an inconsistency
// (unbound variable / kind conflict) surfaces as a non-nil error with a zero
// Query. The executed query stays the original text (ADR 0005); this model is the
// type interface only.
func (p parser) Parse(r io.Reader) (query.Query, error) {
	lex := gen.NewCypherLexer(antlr.NewIoStream(r))
	ts := antlr.NewCommonTokenStream(lex, antlr.TokenDefaultChannel)
	cp := gen.NewCypherParser(ts)

	// The listener is the single error sink: it captures lexer/parser syntax
	// errors and the collection errors raised during the walk, all on l.err. walk
	// surfaces the first of them.
	l := newListener(ts, p.cfg.registry)
	lex.RemoveErrorListeners()
	lex.AddErrorListener(l)
	cp.RemoveErrorListeners()
	cp.AddErrorListener(l)

	tree := cp.OC_Cypher()
	if err := l.walk(tree); err != nil {
		return query.Query{}, err
	}

	return l.build()
}
