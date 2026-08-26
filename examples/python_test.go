//go:build python || all

package examples

import "testing"

// The base program converted to python. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInPython(t *testing.T) {
	fixture := recorderFor(t)
	opts := pythonOptions(t).With(baseProgramOptions(t, fixture, "python"))
	runProgram(t, fixture, opts)
}
