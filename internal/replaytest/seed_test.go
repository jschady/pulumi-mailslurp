package replaytest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The names of a replayed test must repeat the names of the recording, so the source must answer
// the same bytes for the same seed and the same draw.
func TestTheSameSeedDrawsTheSameBytes(t *testing.T) {
	first := NewSource([]byte("a seed of the tests"))
	second := NewSource([]byte("a seed of the tests"))

	require.Equal(t, first.Hex(4), second.Hex(4), "the first draw must repeat")
	require.Equal(t, first.Hex(8), second.Hex(8), "the second draw must repeat")
}

func TestEachDrawAnswersFreshBytes(t *testing.T) {
	source := NewSource([]byte("a seed of the tests"))

	require.NotEqual(t, source.Hex(16), source.Hex(16), "two draws must not collide")
}

func TestAnotherSeedDrawsOtherBytes(t *testing.T) {
	require.NotEqual(t,
		NewSource([]byte("one seed")).Hex(16),
		NewSource([]byte("another seed")).Hex(16))
}

func TestHexAnswersTwoCharactersForEachByte(t *testing.T) {
	require.Len(t, NewSource([]byte("one seed")).Hex(4), 8)
}

func TestRecordWritesTheSeedAndReplayReadsIt(t *testing.T) {
	dir := t.TempDir()

	recorded, err := sourceFor(Record, dir, "TestSeed")
	require.NoError(t, err)
	require.FileExists(t, SeedPath(dir, "TestSeed"))

	replayed, err := sourceFor(Replay, dir, "TestSeed")
	require.NoError(t, err)
	require.Equal(t, recorded.Hex(4), replayed.Hex(4), "replay must draw the recorded name")
}

func TestLiveDrawsASeedAndWritesNothing(t *testing.T) {
	dir := t.TempDir()

	source, err := sourceFor(Live, dir, "TestSeed")
	require.NoError(t, err)
	require.NotEmpty(t, source.Hex(4))
	require.NoFileExists(t, SeedPath(dir, "TestSeed"), "live must leave no seed behind")
}

func TestAMissingSeedNamesThePathAndTheRecordTargets(t *testing.T) {
	dir := t.TempDir()

	_, err := sourceFor(Replay, dir, "TestSeed")
	require.Error(t, err, "replay with no seed must fail")
	require.ErrorContains(t, err, SeedPath(dir, "TestSeed"), "the refusal must name the file")
	require.ErrorContains(t, err, "make record_examples")
	require.ErrorContains(t, err, "make record_integration")
}

func TestASeedThatIsNotHexadecimalFails(t *testing.T) {
	dir := t.TempDir()
	path := SeedPath(dir, "TestSeed")
	require.NoError(t, os.WriteFile(path, []byte("not a seed\n"), 0o600))

	_, err := sourceFor(Replay, dir, "TestSeed")
	require.Error(t, err, "a seed the run cannot read must fail")
	require.ErrorContains(t, err, path)
}

func TestTheSeedSitsNextToTheCassette(t *testing.T) {
	require.Equal(t, filepath.Join("testdata", "cassettes", "TestOne.seed"),
		SeedPath(filepath.Join("testdata", "cassettes"), "TestOne"))
}
