//go:build dotnet || all

package examples

import "testing"

// The base program converted to dotnet. It attaches to the shared inbox, so it creates none.
func TestTheBaseProgramInDotNet(t *testing.T) {
	opts := dotnetOptions(t).With(baseProgramOptions(t, "dotnet"))
	runProgram(t, opts)
}
