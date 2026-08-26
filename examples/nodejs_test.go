//go:build nodejs || all

package examples

import "testing"

// The base program converted to nodejs. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInNodeJs(t *testing.T) {
	fixture := recorderFor(t)
	opts := nodejsOptions(t).With(baseProgramOptions(t, fixture, "nodejs"))
	runProgram(t, fixture, opts)
}
