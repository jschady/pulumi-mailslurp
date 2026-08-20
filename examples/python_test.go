//go:build python || all

package examples

import "testing"

// The base program converted to python. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInPython(t *testing.T) {
	opts := pythonOptions(t).With(baseProgramOptions(t, "python"))
	runProgram(t, opts)
}
