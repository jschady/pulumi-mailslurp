//go:build dotnet || all

package examples

import "testing"

// The base program converted to dotnet. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInDotNet(t *testing.T) {
	fixture := recorderFor(t)
	opts := dotnetOptions(t).With(baseProgramOptions(t, fixture, "dotnet"))
	runProgram(t, fixture, opts)
}
