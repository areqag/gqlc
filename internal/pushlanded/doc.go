// Package pushlanded holds the behavioural tests for the justfile's
// `push-landed` recipe, which answers whether a push that reported failure
// actually landed.
//
// The recipe drives no Go tool, so there is no package it belongs beside;
// this one exists to give its tests a home and carries no code of its own.
// The tests run the shipped recipe over a throwaway repository rather than
// reading its text, which is the same instrument
// internal/tools/vulnguard/harness_test.go uses on `vuln`.
package pushlanded
