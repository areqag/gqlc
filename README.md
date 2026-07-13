# GQLC

gqlc is an analogue of the well-known sqlc library but for
graph query languages such as Cypher and GQL. It intends to
support gql schema files and queries in both gql and cypher.

## Installation

```sh
go install github.com/areqag/gqlc/cmd/gqlc@latest
```

## Getting started

Run `gqlc init` in your project root and answer the prompts. It shows
the exact config file it will write and writes it — and nothing
else — only after you confirm. Set `ACCESSIBLE=1` for a
screen-reader-friendly numbered-prompt mode. With the defaults
accepted, `gqlc.yaml` reads:

```yaml
version: 1
schema: schema.gql
queries: queries
output: internal/db
package: db
schema_language: gqlc
query_language: opencypher
driver: neo4j-go-v5
```

Put your schema at `schema.gql`:

```
CREATE PROPERTY GRAPH TYPE People AS {
    (:Person {
        id   :: INT64 NOT NULL,
        name :: STRING NOT NULL
    })
}
```

Add annotated queries under `queries/`, for example `people.cypher`:

```cypher
// name: AllPersons :many
MATCH (p:Person) RETURN p
```

Then run:

```sh
gqlc generate
```

This writes a typed Go package to `internal/db`: `db.go`, `models.go`,
`querier.go`, and one `<name>.cypher.go` per query file (here
`people.cypher.go`).

The output directory is owned exclusively by gqlc: every run replaces
its contents, and a run aborts — deleting nothing — if the directory
holds anything gqlc cannot prove it generated. Keep your own code out
of it (see
[ADR 0012](docs/adr/0012-output-directory-exclusively-owned.md)).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for dev environment setup and the
`just` recipes used for linting, testing, and formatting.
