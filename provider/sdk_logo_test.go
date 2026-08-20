package provider

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mark this repository authored, and the raster the .NET package carries: the generator writes
// whatever schema.logoUrl answers into logo.png. https://github.com/pulumi/pulumi/issues/13589
const (
	logoSourcePath = "docs/logo.svg"
	logoRasterPath = "docs/logo.png"
)

// NuGet reads the leading bytes of the icon rather than its extension, so an SVG or an error
// page named logo.png is rejected at push.
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func readRepoBytes(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	require.NoError(t, err, "read %s", rel)
	return raw
}

// pngHeader answers the width, height and colour type of a PNG, which live in the IHDR chunk
// that every PNG opens with.
func pngHeader(t *testing.T, raw []byte) (uint32, uint32, byte) {
	t.Helper()
	require.Greater(t, len(raw), 26, "the file is too short to be a PNG")
	require.Equal(t, []byte("IHDR"), raw[12:16], "the file does not open with an IHDR chunk")
	return binary.BigEndian.Uint32(raw[16:20]), binary.BigEndian.Uint32(raw[20:24]), raw[25]
}

// TestTheDotnetIconSourceIsARealRaster reads the file the .NET generation writes over the body
// it downloaded. The error page it replaces was 14 bytes of text named logo.png.
func TestTheDotnetIconSourceIsARealRaster(t *testing.T) {
	t.Parallel()
	raster := readRepoBytes(t, logoRasterPath)
	require.True(t, bytes.HasPrefix(raster, pngMagic),
		"%s is not a PNG: it starts %q", logoRasterPath, raster[:min(len(raster), 16)])

	width, height, colour := pngHeader(t, raster)
	assert.Equal(t, width, height, "%s is not square, so a registry crops it", logoRasterPath)
	assert.GreaterOrEqual(t, width, uint32(128),
		"%s is smaller than the icon a package registry renders", logoRasterPath)
	assert.EqualValues(t, 6, colour,
		"%s carries no alpha channel, so the rounded corners come out opaque", logoRasterPath)
}

// The raster and the vector are one mark drawn twice. A raster of somebody else's artwork would
// ship on NuGet while the registry page showed this one.
func TestTheRasterAndTheVectorDrawTheSameMark(t *testing.T) {
	t.Parallel()
	vector := string(readRepoBytes(t, logoSourcePath))
	for _, colour := range []string{"#232a3d", "#ffffff", "#f2994a"} {
		require.Contains(t, vector, colour, "%s no longer uses %s", logoSourcePath, colour)
	}

	raster := readRepoBytes(t, logoRasterPath)
	width, _, _ := pngHeader(t, raster)
	require.Contains(t, vector, `viewBox="0 0 64 64"`, "%s changed its coordinate space", logoSourcePath)
	assert.Zero(t, width%64, "%s is not a whole multiple of the %s view box", logoRasterPath, logoSourcePath)
}
