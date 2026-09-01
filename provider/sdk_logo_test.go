package provider

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

// svgFills answers every colour the vector fills a shape with, lower-cased and deduplicated. The
// vector is the drawing this repository authored, so it is the oracle for the raster.
func svgFills(t *testing.T, svg []byte) []string {
	t.Helper()
	matches := regexp.MustCompile(`fill="(#[0-9a-fA-F]{6})"`).FindAllSubmatch(svg, -1)
	require.NotEmpty(t, matches, "%s fills no shape with a colour", logoSourcePath)

	seen := map[string]bool{}
	fills := make([]string, 0, len(matches))
	for _, match := range matches {
		colour := strings.ToLower(string(match[1]))
		if seen[colour] {
			continue
		}
		seen[colour] = true
		fills = append(fills, colour)
	}
	return fills
}

// svgViewBox answers the width and the height of the coordinate space the vector draws in. The
// first two numbers of the attribute are the origin, and the drawing does not depend on them.
func svgViewBox(t *testing.T, svg []byte) (int, int) {
	t.Helper()
	pattern := regexp.MustCompile(`viewBox="\s*[\d.-]+\s+[\d.-]+\s+([1-9][0-9]*)\s+([1-9][0-9]*)\s*"`)
	match := pattern.FindSubmatch(svg)
	require.NotNil(t, match, "%s declares no view box with a width and a height", logoSourcePath)

	width, err := strconv.Atoi(string(match[1]))
	require.NoError(t, err)
	height, err := strconv.Atoi(string(match[2]))
	require.NoError(t, err)
	return width, height
}

// pngHasColour answers whether the raster draws one fully opaque pixel of the colour, written as
// `#rrggbb`. The comparison is exact. A measurement of the two files shows that every fill of
// docs/logo.svg covers a flat area of docs/logo.png, so the blended edges the rasteriser draws
// around that area need no tolerance.
func pngHasColour(img image.Image, rgb string) bool {
	value, err := strconv.ParseUint(strings.TrimPrefix(rgb, "#"), 16, 32)
	if err != nil {
		return false
	}
	wantR, wantG, wantB := uint32(value>>16)&0xff, uint32(value>>8)&0xff, uint32(value)&0xff

	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a>>8 == 0xff && r>>8 == wantR && g>>8 == wantG && b>>8 == wantB {
				return true
			}
		}
	}
	return false
}

// The raster and the vector are one mark drawn twice. A raster of somebody else's artwork would
// ship on NuGet while the registry page showed this one.
func TestTheRasterAndTheVectorDrawTheSameMark(t *testing.T) {
	t.Parallel()
	vector := readRepoBytes(t, logoSourcePath)
	boxWidth, boxHeight := svgViewBox(t, vector)
	assert.Equal(t, boxWidth, boxHeight, "%s draws in a view box that is not square", logoSourcePath)

	raster, err := png.Decode(bytes.NewReader(readRepoBytes(t, logoRasterPath)))
	require.NoError(t, err, "decode %s", logoRasterPath)
	assert.Zero(t, raster.Bounds().Dx()%boxWidth,
		"%s is not a whole multiple of the %s view box", logoRasterPath, logoSourcePath)

	for _, colour := range svgFills(t, vector) {
		assert.True(t, pngHasColour(raster, colour),
			"%s draws no pixel of %s, which %s fills a shape with", logoRasterPath, colour, logoSourcePath)
	}
}
