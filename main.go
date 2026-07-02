// gqlc is an sqlc analogue for graph query languages: it parses GQL graph
// schemas and openCypher queries and will generate type-safe code from them.
// The current entrypoint is a development driver that parses the sample schema.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/areqag/gqlc/internal/logger"
	schema_parser_gql "github.com/areqag/gqlc/internal/schema/gql"
)

func main() {
	logger.Init(slog.LevelDebug)
	if err := run(context.Background()); err != nil {
		os.Exit(1)
	}
}

// run owns all the defers; main holds the only os.Exit, so they always fire.
// Failures are logged here at the site that has the context, and returned
// only to set the exit code.
func run(ctx context.Context) error {
	start := time.Now()
	defer func() {
		slog.DebugContext(ctx, "execution complete", "duration_ms", time.Since(start).Milliseconds())
	}()

	f, err := os.Open("./test/data/schema/gql/valid/sample_schema.gql")
	if err != nil {
		slog.ErrorContext(ctx, "failed to open schema", "err", err)
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close schema file", "err", err)
		}
	}()

	p := schema_parser_gql.New()
	if _, err := p.Parse(f); err != nil {
		slog.ErrorContext(ctx, "failed to parse schema", "err", err)
		return err
	}
	return nil
}
