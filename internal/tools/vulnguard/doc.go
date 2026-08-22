// Package vulnguard holds the regression for the `vuln` recipe's own
// assertions.
//
// It has no runtime code. The recipe grades what govulncheck reported about
// the standard library, and the clauses doing that grading are exercised on
// every run — but only in the direction a good tree takes, where each one
// passes in silence. A clause whose passing case is silence is one nothing
// distinguishes from a deleted clause, which is what bd gqlc-agt0 measured:
// ten of them could be neutered with `just vuln` still green.
//
// What lives here is the other direction. The recipe is run over a throwaway
// tree with a stubbed toolchain, once per deliberate defect, and each run has
// to refuse and to name the defect it found. Neutering one of the ten then
// leaves a run that should have refused exiting 0, and this package reddens.
package vulnguard
