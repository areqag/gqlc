// Package codegen holds the target-independent half of the generator:
// the [Generator] seam consumers accept, the batch [Input] / [File] wire
// types, the sentinel set, and the shared phases [Prepare] runs — schema
// admission, entity naming, batch admission, and per-query name
// derivation (ADR 0010). A backend sub-package supplies the Go-type
// table those phases read and owns the emission layer that turns a
// [Prepared] batch into files.
//
// Generate is pure: same Input in, byte-identical []File out (§2.3).
// All I/O is the caller's — no implementation touches disk.
package codegen

// Generator emits a generated package from a schema plus a batch of
// named queries. Consumers accept the interface so a target can be
// swapped without importing any one backend's construction options.
type Generator interface {
	Generate(in Input) ([]File, error)
}
