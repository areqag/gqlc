package config

// CheckEntryCount hands the unexported §4 count check to the external test
// package. TestEntryCountInvariant has to reach it directly — §4 establishes
// that no document reaches it, so there is no input that exercises the message
// through Load — but reaching it from `package config` put the whole file, and
// the yaml.v3 import it needs to build a *yaml.Node, outside govulncheck's
// call graph (bd gqlc-m5rc).
//
// This file declares no imports, and that is the point rather than an
// accident: `vuln-root-residual` measures a package's blindness from the
// imports of its in-package test files, so a bridge that names no third-party
// package leaves internal/config scannable while still opening the internal.
var CheckEntryCount = checkEntryCount
