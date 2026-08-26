package replaytest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnUnsetVariableMeansReplay(t *testing.T) {
	t.Setenv(ModeVariable, "")

	mode, err := ReadMode()
	require.NoError(t, err)
	require.Equal(t, Replay, mode, "a run with no variable must read a cassette")
}

func TestEachOfTheThreeValuesNamesItsMode(t *testing.T) {
	for _, want := range everyMode() {
		t.Run(string(want), func(t *testing.T) {
			t.Setenv(ModeVariable, string(want))

			mode, err := ReadMode()
			require.NoError(t, err)
			require.Equal(t, want, mode)
		})
	}
}

// A value nobody meant must stop the run. A silent fall back to replay would hide a typo in a
// record command behind a wall of missing cassettes.
func TestAnUnknownModeNamesTheVariableAndTheThreeValues(t *testing.T) {
	t.Setenv(ModeVariable, "reply")

	_, err := ReadMode()
	require.Error(t, err, "an unknown mode must fail the run")
	require.ErrorContains(t, err, ModeVariable, "the refusal must name the variable")
	require.ErrorContains(t, err, "reply", "the refusal must quote the value it read")
	for _, mode := range everyMode() {
		require.ErrorContains(t, err, string(mode), "the refusal must name every mode")
	}
}

func TestCassetteNameTurnsASubtestNameIntoAFileName(t *testing.T) {
	require.Equal(t, "TestOne", CassetteName("TestOne"))
	require.Equal(t, "TestOne_a_b", CassetteName("TestOne/a/b"))
}
