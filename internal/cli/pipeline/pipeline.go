// Package pipeline runs stages 1-8 of the CLI-1 generate pipeline —
// config load through codegen — and returns the file batch in memory.
// Deliberately subcommand-agnostic: names no sibling command (the
// "run gqlc init" hint on a missing config lives in the CLI, which
// owns UX copy). Callers own all filesystem writes under the ADR 0012
// tripwire; the pipeline never writes.
//
// Result caller invariant, non-negotiable:
//
//	Files is non-nil iff Diagnostics is empty AND err is nil.
//	Callers MUST NOT write Result.Files when len(Result.Diagnostics)
//	> 0; that state means "errors accumulated, batch discarded" and
//	Files is nil in that branch. Ignoring the invariant lets the ADR
//	0012 tripwire wipe a marked output directory to write zero files
//	— the exact footgun the split exists to prevent.
//
// The authoritative user-facing contracts live in docs/specs/cli-stage-1.md;
// this package's own spec is docs/specs/cli-generate-pipeline.md.
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

// Result is what a successful or diagnostic-accumulating pipeline run
// yields: the codegen batch, the resolved output directory the caller
// writes it to, and the ordered per-failure diagnostic lines from
// stage 7. Both slices preserve pipeline order — the caller writes
// Diagnostics to stderr in order and writes Files to disk in slice
// order.
//
// OutDir is the config's output path joined against
// filepath.Dir(cfgPath) — the spec §3.1 stage-2 resolution rule —
// carried out of the pipeline so the caller does not re-load the
// config to reach it. Populated whenever config load (stage 1)
// succeeded, i.e. in every non-ErrConfigMissing branch including
// stage-7 accumulation and every stage 3-8 singular failure. Empty
// only when Run returned ErrConfigMissing or any other stage-1
// failure (config never loaded).
//
// Field invariant (package doc, restated): Files is non-nil iff
// Diagnostics is empty and the corresponding Run call returned a nil
// error.
type Result struct {
	Files       []codegen.File
	Diagnostics []string
	OutDir      string
}

// Run executes stages 1-8 of the generate pipeline (CLI-1 spec §3.1)
// against the config file at cfgPath. It performs no filesystem
// writes — the caller writes Result.Files under the ADR 0012
// tripwire.
//
// Return contract, exhaustive:
//
//   - err != nil, ErrConfigMissing or other stage-1 failure → Result
//     is the zero value (Files nil, Diagnostics nil, OutDir empty).
//     Config never loaded, so OutDir cannot be computed.
//   - err != nil, any stage 3-8 failure → Result carries OutDir (the
//     stage-2 resolved path); Files and Diagnostics are nil.
//   - err == nil, len(Diagnostics) > 0  → stage-7 accumulation. Files
//     is nil; OutDir carries the resolved path. Caller prints each
//     diagnostic line and returns its own summary error (spec §2.3);
//     the tripwire does NOT run.
//   - err == nil, len(Diagnostics) == 0 → success. Files is non-nil,
//     sorted by Path (codegen.Generate's contract), OutDir carries
//     the resolved path, ready for the caller's tripwire-guarded
//     write.
//
// No other combinations exist; the caller may rely on this.
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

	// Stage 2 — resolve paths against the config file's directory.
	// No existence checks here; each consuming stage owns its own
	// open failure. outDir is carried on every Result returned after
	// this point so the caller does not re-load the config to reach
	// it (see Result doc).
	baseDir := filepath.Dir(cfgPath)
	schemaPath := resolvePath(baseDir, cfg.SchemaPath)
	queryDir := resolvePath(baseDir, cfg.QueryDir)
	outDir := resolvePath(baseDir, cfg.OutputDir)

	// Stage 3 — parse schema per the SchemaLang axis (spec §3.2).
	var schemaParser schema.Parser
	switch cfg.SchemaLang {
	case config.SchemaLangGQL:
		schemaParser = gql.New()
	default:
		return Result{OutDir: outDir}, fmt.Errorf("internal: no pipeline mapping for schema_language %q", string(cfg.SchemaLang))
	}
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return Result{OutDir: outDir}, fmt.Errorf("schema: %w", err)
	}
	sch, err := schemaParser.Parse(bytes.NewReader(schemaBytes))
	if err != nil {
		return Result{OutDir: outDir}, fmt.Errorf("schema %s: %w", schemaPath, err)
	}

	// Stage 4 — load procsig. When the key is absent the zero
	// Registry misses on every Lookup, so a CALL in a registry-less
	// project fails at cypher parse with ErrUnknownProcedure —
	// the correct diagnosis (spec §3.1).
	var reg procsig.Registry
	if cfg.ProcsigPath != "" {
		reg, err = procsig.Load(resolvePath(baseDir, cfg.ProcsigPath))
		if err != nil {
			return Result{OutDir: outDir}, err
		}
	}

	// Stage 5 — construct the front end once, outside the query loop.
	// The same registry feeds both the parser and the resolver.
	var queryParser query.Parser
	switch cfg.QueryLang {
	case config.QueryLangOpenCypher:
		queryParser = cypher.New(cypher.WithRegistry(reg))
	default:
		return Result{OutDir: outDir}, fmt.Errorf("internal: no pipeline mapping for query_language %q", string(cfg.QueryLang))
	}
	res := resolver.New(sch, resolver.WithRegistry(reg))

	// Stage 6 — discover query files (spec §4).
	names, err := discoverQueryFiles(queryDir)
	if err != nil {
		return Result{OutDir: outDir}, err
	}

	// Stage 7 — front-end walk with error accumulation (spec §3.3).
	// The caller (CLI) prints diagnostics and forms the summary error;
	// Run returns nil error + populated Diagnostics in this branch.
	batch, diags := frontEndWalk(queryParser, res, queryDir, names)
	if len(diags) > 0 {
		return Result{Diagnostics: diags, OutDir: outDir}, nil
	}

	// Stage 8 — generate, with the Driver axis mapping (spec §3.2) and
	// the configured package name (spec §3.4; the loader rejects an
	// empty one).
	var driverOpt codegen.Option
	switch cfg.Driver {
	case config.DriverNeo4jGoV5:
		driverOpt = codegen.WithDriverVersion(codegen.DriverV5)
	case config.DriverNeo4jGoV6:
		driverOpt = codegen.WithDriverVersion(codegen.DriverV6)
	default:
		return Result{OutDir: outDir}, fmt.Errorf("internal: no pipeline mapping for driver %q", string(cfg.Driver))
	}
	files, err := codegen.New(driverOpt, codegen.WithPackageName(cfg.OutputPackage)).
		Generate(codegen.Input{Schema: sch, Queries: batch})
	if err != nil {
		return Result{OutDir: outDir}, err
	}

	return Result{Files: files, OutDir: outDir}, nil
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
