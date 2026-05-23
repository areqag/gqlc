package main

import (
	"context"
	"log/slog"
	"os"

	schema_parser_gql "github.com/antranig-yeretzian/gqlc/internal/schema/gql"
)

func main() {
	ctx := context.Background()

	f, err := os.Open("./test/data/sample_schema.gql")
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
