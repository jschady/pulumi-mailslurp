//go:build go || all

package examples

import "testing"

// The base program converted to go. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInGo(t *testing.T) {
	fixture := recorderFor(t)
	opts := goOptions(t).With(baseProgramOptions(t, fixture, "go"))
	runProgram(t, fixture, opts)
}
