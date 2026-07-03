// Package procsig carries the procedure signature registry — the second
// compile-time input the Cypher parser consults for CALL clauses (Stage 14,
// ADR 0007). A signature is (name, params, results) with typed columns; the
// registry is an immutable name-keyed lookup a parser instance holds for the
// duration of a Parse call. An unknown procedure at generation time is an
// error by design, symmetric to a MATCH against an unknown label.
//
// The package is dependency-independent of both internal/query and
// internal/schema so its TypeToken sum can grow (or, for NUMBER, stay a
// signature-only marker) without touching the query.Type freeze surface.
// The bridge from TypeToken to query.Type lives in the cypher package (Stage
// 14 spec §3.2).
package procsig

import (
	"errors"
	"fmt"
	"strings"
)

// TypeToken is the signature-time type vocabulary. The sum is closed and
// intentionally decoupled from query.Type: NUMBER is a signature-only marker
// (assignable-from INTEGER-or-FLOAT at the argument-typing site — see Call3
// scenarios), which query.Type deliberately does not carry (Stage 14 §1.1).
// The four members are exactly what the TCK corpus attests
// (INTEGER?×47, STRING?×30, NUMBER?×4, FLOAT?×2, zero BOOLEAN) — sized to
// the corpus, no gold-plating. NewRegistry rejects a Signature declaring
// any token outside this sum.
type TypeToken int

const (
	// TokenInteger is a signature INTEGER param or result type.
	TokenInteger TypeToken = iota
	// TokenFloat is a signature FLOAT param or result type.
	TokenFloat
	// TokenString is a signature STRING param or result type.
	TokenString
	// TokenNumber is a signature NUMBER param type — an assignable-from
	// marker that accepts either INTEGER or FLOAT at the argument-typing
	// site (Call3 scenarios). It never appears as a runtime column type
	// on the query wire; the cypher-package bridge maps it to
	// query.TypeUnknown so no NUMBER identity leaks into the freeze
	// surface (Stage 14 §3.2).
	TokenNumber
)

// String returns the canonical uppercase name of the token, matching the
// declaration syntax in the TCK background steps
// ("test.my.proc(in :: INTEGER?) :: (out :: STRING?)"). It is the single
// source used in error messages so registry diagnostics do not drift from
// the input surface.
func (t TypeToken) String() string {
	switch t {
	case TokenFloat:
		return "FLOAT"
	case TokenString:
		return "STRING"
	case TokenNumber:
		return "NUMBER"
	default:
		return "INTEGER"
	}
}

// Param is one signature input in declaration order. Name is the parameter
// identifier (used to bind implicit-invocation arguments from query
// parameters at runtime); Token is the declared type; Nullable is the
// signature's trailing `?` (a static parser-time fact — every corpus
// signature marks every param nullable).
type Param struct {
	Name     string
	Token    TypeToken
	Nullable bool
}

// Result is one signature output column in declaration order. Name is the
// column identifier YIELD items reference by name (Call5[3] outline
// exercises both `YIELD a, b` and `YIELD b, a` against the same `(a, b)`
// signature — the ordering is by name, not position); Token is the
// declared type; Nullable is the trailing `?`.
type Result struct {
	Name     string
	Token    TypeToken
	Nullable bool
}

// Signature is one fully-qualified procedure signature. Name is the dotted
// form ("test.my.proc") — NewRegistry rejects any Signature whose Name is
// empty or unqualified (no dot). Params and Results carry declaration
// order; the same order drives the CALL YIELD * expansion and the
// standalone-CALL Returns expansion (spec §4.3).
type Signature struct {
	Name    string
	Params  []Param
	Results []Result
}

// Registry is an immutable, name-keyed lookup over a set of signatures.
// The zero value is a valid empty registry — every Lookup misses, so a
// CALL against an empty registry raises ErrUnknownProcedure at the
// parser's fail-site (spec §1.6). Registry values are pass-by-value and
// safe for concurrent reads; the underlying map is not exposed.
type Registry struct {
	byName map[string]Signature
}

// NewRegistry validates the given signatures and returns an immutable
// Registry. It rejects (in order): an empty Name; a Name without at least
// one dot (unqualified); a duplicate Name across the slice; an empty
// Param or Result Name; a duplicate Param Name within a single signature;
// a duplicate Result Name within a single signature; a Param or Result
// declaring a Token outside the TypeToken sum. On any violation it
// returns the zero Registry and a wrapping error naming the offending
// signature. An empty input slice yields the zero Registry (all lookups
// miss) without error.
func NewRegistry(sigs []Signature) (Registry, error) {
	if len(sigs) == 0 {
		return Registry{}, nil
	}
	byName := make(map[string]Signature, len(sigs))
	for _, sig := range sigs {
		if sig.Name == "" {
			return Registry{}, errors.New("procsig: signature name must not be empty")
		}
		if !strings.Contains(sig.Name, ".") {
			return Registry{}, fmt.Errorf("procsig: signature %q must be fully-qualified (contain at least one dot)", sig.Name)
		}
		if _, dup := byName[sig.Name]; dup {
			return Registry{}, fmt.Errorf("procsig: duplicate signature name %q", sig.Name)
		}
		if err := validateColumns(sig.Name, "param", paramNames(sig.Params), paramTokens(sig.Params)); err != nil {
			return Registry{}, err
		}
		if err := validateColumns(sig.Name, "result", resultNames(sig.Results), resultTokens(sig.Results)); err != nil {
			return Registry{}, err
		}
		byName[sig.Name] = sig
	}
	return Registry{byName: byName}, nil
}

// Lookup returns the Signature registered under name and true if present,
// or the zero Signature and false otherwise. The zero Registry (an
// uninitialised value or one constructed from an empty slice) always
// misses.
func (r Registry) Lookup(name string) (Signature, bool) {
	sig, ok := r.byName[name]
	return sig, ok
}

// validateColumns is the shared param/result column check: non-empty
// names, uniqueness inside the column list, and every token a member
// of TypeToken.
func validateColumns(sigName, kind string, names []string, tokens []TypeToken) error {
	seen := make(map[string]bool, len(names))
	for i, name := range names {
		if name == "" {
			return fmt.Errorf("procsig: signature %q has an empty %s name at position %d", sigName, kind, i)
		}
		if seen[name] {
			return fmt.Errorf("procsig: signature %q has duplicate %s name %q", sigName, kind, name)
		}
		seen[name] = true
		if !validToken(tokens[i]) {
			return fmt.Errorf("procsig: signature %q %s %q declares unknown TypeToken %d", sigName, kind, name, tokens[i])
		}
	}
	return nil
}

func validToken(t TypeToken) bool {
	switch t {
	case TokenInteger, TokenFloat, TokenString, TokenNumber:
		return true
	}
	return false
}

func paramNames(ps []Param) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func paramTokens(ps []Param) []TypeToken {
	out := make([]TypeToken, len(ps))
	for i, p := range ps {
		out[i] = p.Token
	}
	return out
}

func resultNames(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func resultTokens(rs []Result) []TypeToken {
	out := make([]TypeToken, len(rs))
	for i, r := range rs {
		out[i] = r.Token
	}
	return out
}
