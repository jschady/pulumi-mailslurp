//go:build nodejs || all

package examples

import "testing"

// The base program converted to nodejs. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInNodeJs(t *testing.T) {
	opts := nodejsOptions(t).With(baseProgramOptions(t, "nodejs"))
	runProgram(t, opts)
}
