// Package backends is the command layer's composition root for code
// generation: the one place a driver wire key from the config
// vocabulary is bound to the backend that emits for it. Both lists are
// closed and must agree member for member; a parity test holds them
// together.
package backends

import (
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/codegen/age"
	"github.com/areqag/gqlc/internal/codegen/neo4j"
	"github.com/areqag/gqlc/internal/config"
)

// Registry returns the generator registry the generate pipeline
// resolves a target's driver through. The error reports a malformed
// entry list — a programming error in the entries below, not anything a
// config file can provoke.
func Registry() (codegen.Registry, error) {
	return codegen.NewRegistry(
		codegen.Entry{
			Key: string(config.DriverNeo4jGoV5),
			New: func(pkg string) codegen.Generator {
				return neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV5), neo4j.WithPackageName(pkg))
			},
		},
		codegen.Entry{
			Key: string(config.DriverNeo4jGoV6),
			New: func(pkg string) codegen.Generator {
				return neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV6), neo4j.WithPackageName(pkg))
			},
		},
		codegen.Entry{
			Key: string(config.DriverApacheAgePgxV5),
			New: func(pkg string) codegen.Generator {
				return age.New(age.WithPackageName(pkg))
			},
			// The two neo4j entries above publish nothing on purpose:
			// their exported Err* vars are template text emitted INTO
			// generated code, never refusals their generator returns.
			Sentinels: age.Sentinels(),
		},
	)
}
