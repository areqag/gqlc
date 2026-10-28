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
			// Both neo4j entries publish the same map, and the registry
			// merges two entries publishing one name under one value
			// without complaint — these two wire keys are one backend
			// package. The map holds the one refusal neo4j raises of its
			// own, ErrUnrepresentableOnDriverVersion, for a width the
			// backend carries on the other driver major (VECTOR, ADR 0051).
			Sentinels: neo4j.Sentinels(),
		},
		codegen.Entry{
			Key: string(config.DriverNeo4jGoV6),
			New: func(pkg string) codegen.Generator {
				return neo4j.New(neo4j.WithDriverVersion(neo4j.DriverV6), neo4j.WithPackageName(pkg))
			},
			Sentinels: neo4j.Sentinels(),
		},
		codegen.Entry{
			Key: string(config.DriverApacheAgePgxV5),
			New: func(pkg string) codegen.Generator {
				return age.New(age.WithPackageName(pkg))
			},
			Sentinels: age.Sentinels(),
		},
	)
}
