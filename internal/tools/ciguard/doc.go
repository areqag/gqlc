// Package ciguard holds assertions about this repository's own CI wiring.
//
// It has no runtime code. The workflows are the only artefacts in the tree
// that gate every other change and that nothing else compiles, parses or
// tests — actionlint checks that ci.yml is well-formed, not that a gate in it
// is still connected to anything. What lives here is the second half:
// properties whose loss would leave a green pipeline that has stopped
// checking. They are pinned against the parsed YAML rather than the file's
// bytes, so a rule restated in a comment cannot stand in for the rule.
package ciguard
