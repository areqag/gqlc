// Package version holds the gqlc version string: the single
// -ldflags -X substitution target serving both the generated-file
// header (internal/codegen) and the `gqlc version` command.
package version

// Version is "dev" unless a release build overrides it:
//
//	go build -ldflags "-X github.com/areqag/gqlc/internal/version.Version=$(git describe --tags)"
//
// var, not const — -ldflags -X only overrides string variables
// (C6 §4.1). The codegen golden corpus pins the default: it must
// stay exactly "dev" or every golden header changes.
var Version = "dev"
