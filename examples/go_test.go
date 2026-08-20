//go:build go || all

package examples

import "testing"

// The base program converted to go. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInGo(t *testing.T) {
	opts := goOptions(t).With(baseProgramOptions(t, "go"))
	runProgram(t, opts)
}
