package codegen

// version is the stamp embedded in every generated file's header line
// (§5.2 of docs/specs/codegen-stage-c6.md). Default "dev"; release
// binaries override at build time:
//
//	go build -ldflags "-X github.com/areqag/gqlc/internal/codegen.version=$(git describe --tags)"
//
// The value is a package-level variable so the double-run determinism
// test (C0 §2.3) holds across arbitrary invocations of the same binary:
// two invocations of the same binary always see the same string.
//
// var, not const — Go's -ldflags -X overrides string variables at link
// time; a const cannot be overridden. Lowering to var at C6 fixes a
// latent C0 release-recipe bug where the override silently no-op'd.
var version = "dev"
