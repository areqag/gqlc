// Package pipeline runs stages 1-8 of the CLI-1 generate pipeline —
// config load through codegen — for every generation target the config
// file declares, and returns the file batches in memory. Deliberately
// subcommand-agnostic: names no sibling command (the "run gqlc init"
// hint on a missing config lives in the CLI, which owns UX copy).
// Callers own all filesystem writes under the ADR 0012 tripwire; the
// pipeline never writes.
//
// Result caller invariant, non-negotiable:
//
//	Targets is non-nil iff Diagnostics is empty AND err is nil.
//	Callers MUST NOT write any TargetResult when
//	len(Result.Diagnostics) > 0; that state means "errors accumulated,
//	every batch discarded" and Targets is nil in that branch. Ignoring
//	the invariant lets the ADR 0012 tripwire wipe marked output
//	directories to write zero files — the exact footgun the split
//	exists to prevent.
//
// The authoritative user-facing contracts live in docs/specs/cli-stage-1.md;
// this package's own spec is docs/specs/cli-generate-pipeline.md, and the
// multi-target contract is docs/specs/config-multi-target.md §6.
package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/config"
	"github.com/areqag/gqlc/internal/procsig"
	"github.com/areqag/gqlc/internal/query"
	"github.com/areqag/gqlc/internal/query/cypher"
	"github.com/areqag/gqlc/internal/queryfile"
	"github.com/areqag/gqlc/internal/resolver"
	"github.com/areqag/gqlc/internal/schema"
	"github.com/areqag/gqlc/internal/schema/gql"
)

// ErrConfigMissing is the sentinel Run wraps when the config file at
// cfgPath does not exist (fs.ErrNotExist from config.Load, spec §2.3).
// The wrap chain preserves fs.ErrNotExist, so both errors.Is targets
// match — but the CLI keys on this sentinel, not fs.ErrNotExist, to
// avoid over-triggering on schema/queries ErrNotExist which the spec
// wraps differently. The CLI maps this sentinel to the user-facing
// "run gqlc init" hint.
var ErrConfigMissing = errors.New("config file not found")

// Result is what a clean or diagnostic-accumulating run yields: one
// TargetResult per generation target the config declares, in document
// order, and the ordered diagnostic lines from every target's
// front-end walk. Both slices preserve pipeline order — the caller
// writes Diagnostics to stderr in order and each target's files to
// disk in slice order.
//
// Field invariant (package doc, restated): Targets is non-nil iff
// Diagnostics is empty and the corresponding Run call returned a nil
// error.
type Result struct {
	Targets     []TargetResult
	Diagnostics []string
}

// TargetResult is one target's generated batch and the resolved
// directory the caller writes it to.
//
// OutDir is the target's gen.go.out joined against
// filepath.Dir(cfgPath) — the CLI-1 §3.1 stage-2 resolution rule —
// carried out of the pipeline so the caller does not re-load the
// config to reach it.
type TargetResult struct {
	Files  []codegen.File
	OutDir string
}

// Run executes stages 1-8 of the generate pipeline (CLI-1 spec §3.1)
// against every generation target the config file at cfgPath declares,
// in document order. It performs no filesystem writes — the caller
// writes each TargetResult's files under the ADR 0012 tripwire.
//
// Return contract, exhaustive:
//
//   - err != nil → Result is the zero value. This covers the stage-1
//     config failures (ErrConfigMissing and every other config.Load
//     error) and every per-target setup failure, which is wrapped
//     "graph[<i>]: " (config-multi-target §6.1).
//   - err == nil, len(Diagnostics) > 0 → front-end accumulation.
//     Targets is nil. The caller prints each line and returns its own
//     summary error (spec §2.3); nothing is written.
//   - err == nil, len(Diagnostics) == 0 → success. Targets has exactly
//     one entry per config target, in document order; each Files is
//     non-nil and sorted by Path (codegen.Generate's contract), each
//     OutDir is resolved against filepath.Dir(cfgPath).
//
// No other combinations exist; the caller may rely on this. In
// particular a failed run never returns a partially populated Targets:
// a wipe-and-write driven from one would half-generate the project.
func Run(cfgPath string) (Result, error) {
	// Stage 1 — load config. The fs.ErrNotExist branch is the exact
	// seam config.Load documents for a missing file (spec §2.3). We
	// wrap into ErrConfigMissing so the CLI's UX copy (the "run gqlc
	// init" hint) stays in the CLI; the wrap preserves fs.ErrNotExist
	// in the chain.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Result{}, fmt.Errorf("%w: %s: %w", ErrConfigMissing, cfgPath, err)
		}
		return Result{}, err
	}

	baseDir := filepath.Dir(cfgPath)
	targets := make([]TargetResult, 0, len(cfg.Targets))
	var diags []string
	for i, tgt := range cfg.Targets {
		// A setup failure aborts the whole run at this entry and
		// discards whatever earlier targets accumulated (§6.1).
		tr, tdiags, err := runTarget(baseDir, tgt, len(diags) == 0)
		if err != nil {
			return Result{}, fmt.Errorf("graph[%d]: %w", i, err)
		}
		for _, d := range tdiags {
			diags = append(diags, fmt.Sprintf("graph[%d]: %s", i, d))
		}
		targets = append(targets, tr)
	}

	// All-or-nothing (§6.2): one diagnostic anywhere discards every
	// target's batch, so the caller cannot write a subset.
	if len(diags) > 0 {
		return Result{Diagnostics: diags}, nil
	}
	return Result{Targets: targets}, nil
}

// runTarget runs CLI-1 §3.1 stages 2-8 against one generation target.
// Errors come back unprefixed; Run adds the entry prefix
// (config-multi-target §6.1). Nothing here is shared with the previous
// target: each target parses its own schema, loads its own registry
// and builds its own front end (§6.3 — no cache).
//
// genBatch false skips stage 8. An earlier target already accumulated
// a diagnostic, so every batch is discarded (§6.2) and generating this
// one could only add a codegen error to a run that is already failing.
func runTarget(baseDir string, tgt config.Target, genBatch bool) (TargetResult, []string, error) {
	// Stage 2 — resolve paths against the config file's directory.
	// No existence checks here; each consuming stage owns its own
	// open failure.
	schemaPath := resolvePath(baseDir, tgt.SchemaPath)
	queryDir := resolvePath(baseDir, tgt.QueryDir)
	outDir := resolvePath(baseDir, tgt.Go.Out)

	// Stage 3 — parse schema per the SchemaLang axis (spec §3.2).
	var schemaParser schema.Parser
	switch tgt.SchemaLang {
	case config.SchemaLangGQL:
		schemaParser = gql.New()
	default:
		return TargetResult{}, nil, fmt.Errorf("internal: no pipeline mapping for schema_language %q", string(tgt.SchemaLang))
	}
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return TargetResult{}, nil, fmt.Errorf("schema: %w", err)
	}
	sch, err := schemaParser.Parse(bytes.NewReader(schemaBytes))
	if err != nil {
		return TargetResult{}, nil, fmt.Errorf("schema %s: %w", schemaPath, err)
	}

	// Stage 4 — load procsig. When the key is absent the zero
	// Registry misses on every Lookup, so a CALL in a registry-less
	// project fails at cypher parse with ErrUnknownProcedure —
	// the correct diagnosis (spec §3.1).
	var reg procsig.Registry
	if tgt.ProcsigPath != "" {
		reg, err = procsig.Load(resolvePath(baseDir, tgt.ProcsigPath))
		if err != nil {
			return TargetResult{}, nil, err
		}
	}

	// Stage 5 — construct the front end once, outside the query loop.
	// The same registry feeds both the parser and the resolver.
	var queryParser query.Parser
	switch tgt.QueryLang {
	case config.QueryLangOpenCypher:
		queryParser = cypher.New(cypher.WithRegistry(reg))
	default:
		return TargetResult{}, nil, fmt.Errorf("internal: no pipeline mapping for query_language %q", string(tgt.QueryLang))
	}
	res := resolver.New(sch, resolver.WithRegistry(reg))

	// Stage 6 — discover query files (spec §4).
	names, err := discoverQueryFiles(queryDir)
	if err != nil {
		return TargetResult{}, nil, err
	}

	// Stage 7 — front-end walk with error accumulation (spec §3.3).
	// The caller (CLI) prints diagnostics and forms the summary error;
	// Run returns nil error + populated Diagnostics in this branch.
	batch, diags := frontEndWalk(queryParser, res, queryDir, names)
	if len(diags) > 0 || !genBatch {
		// The zero TargetResult, not one carrying outDir: this branch is
		// reached only when the run already has a diagnostic, and Run
		// discards every target in that case, so a populated OutDir here
		// is a value no caller can read.
		return TargetResult{}, diags, nil
	}

	// Stage 8 — generate, with the Driver axis mapping (spec §3.2) and
	// the configured package name (spec §3.4; the loader rejects an
	// empty one).
	var driverOpt neo4j.Option
	switch tgt.Go.Driver {
	case config.DriverNeo4jGoV5:
		driverOpt = neo4j.WithDriverVersion(neo4j.DriverV5)
	case config.DriverNeo4jGoV6:
		driverOpt = neo4j.WithDriverVersion(neo4j.DriverV6)
	default:
		return TargetResult{}, nil, fmt.Errorf("internal: no pipeline mapping for driver %q", string(tgt.Go.Driver))
	}
	files, err := neo4j.New(driverOpt, neo4j.WithPackageName(tgt.Go.Package)).
		Generate(codegen.Input{Schema: sch, Queries: batch})
	if err != nil {
		return TargetResult{}, nil, err
	}

	return TargetResult{Files: files, OutDir: outDir}, nil, nil
}

// resolvePath joins a config-file-relative path against the config
// file's directory (spec §3.1 stage 2); absolute paths pass through
// unchanged.
func resolvePath(baseDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

// discoverQueryFiles applies the spec §4 discovery rule: a query file
// is a non-directory entry of queryDir whose name ends in ".cypher"
// and does not begin with "."; no recursion. os.ReadDir order
// (lexical by filename) is the diagnostic order and the codegen batch
// order.
func discoverQueryFiles(queryDir string) ([]string, error) {
	entries, err := os.ReadDir(queryDir)
	if err != nil {
		return nil, fmt.Errorf("queries: %w", err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".cypher") {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no query files (*.cypher) in %s", queryDir)
	}
	return names, nil
}

// frontEndWalk runs stage 7 (spec §3.1): queryfile parse → cypher
// parse → resolve for every discovered file, accumulating one
// diagnostic per failure (spec §3.3) — one broken query never hides
// another. Returns the codegen batch (fully-successful queries only,
// discovery order × annotation order) and the diagnostics in pipeline
// order, shaped per spec §2.3: "<path>: <message>" for a file
// failure, "<path>: query <Name>: <message>" for a query failure.
func frontEndWalk(queryParser query.Parser, res *resolver.Resolver, queryDir string, names []string) ([]codegen.NamedQuery, []string) {
	fileParser := queryfile.New()
	var batch []codegen.NamedQuery
	var diags []string
	for _, name := range names {
		path := filepath.Join(queryDir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			diags = append(diags, fmt.Sprintf("%s: %s", path, err))
			continue
		}
		annotated, err := fileParser.Parse(bytes.NewReader(src))
		if err != nil {
			diags = append(diags, fmt.Sprintf("%s: %s", path, err))
			continue
		}
		for _, aq := range annotated {
			parsed, err := queryParser.Parse(strings.NewReader(aq.Text))
			if err != nil {
				diags = append(diags, fmt.Sprintf("%s: query %s: %s", path, aq.Name, err))
				continue
			}
			vq, err := res.Resolve(parsed)
			if err != nil {
				diags = append(diags, fmt.Sprintf("%s: query %s: %s", path, aq.Name, err))
				continue
			}
			batch = append(batch, codegen.NamedQuery{
				Name:        aq.Name,
				Cardinality: aq.Cardinality,
				SourceFile:  name,
				SourceText:  aq.Text,
				Validated:   vq,
			})
		}
	}
	return batch, diags
}
