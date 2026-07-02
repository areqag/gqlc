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
	ctx := context.Background()
	logger.Init(slog.LevelDebug)

	start := time.Now()
	defer func() {
		finish := time.Now()
		slog.DebugContext(ctx, "execution complete", "duration_ms", finish.Sub(start).Milliseconds())
	}()

	f, err := os.Open("./test/data/schema/gql/valid/sample_schema.gql")
	if err != nil {
		slog.ErrorContext(ctx, "failed to open schema", "err", err)
		return
	}
	defer f.Close()

	p := schema_parser_gql.New()
	if _, err := p.Parse(f); err != nil {
		slog.ErrorContext(ctx, "failed to parse schema", "err", err)
	}
}
