package main

import (
	"context"
	"log/slog"

	"github.com/antranig-yeretzian/gqlc/internal/grammar/gql"
)

func main() {
	ctx := context.Background()

	v := gql.NewSchemaVisitor("./test/data/sample_schema.gql")
	if err := v.Parse(); err != nil {
		slog.ErrorContext(ctx, "failed to parse schema", "err", err)
	}
}
