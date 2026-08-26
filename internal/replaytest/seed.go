package replaytest

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// seedSize is the length of a seed in bytes.
const seedSize = 32

// recordTargets names the make targets that write a cassette and a seed.
const recordTargets = "`make record_examples` or `make record_integration`"

// Source draws bytes from one seed. The Nth draw of a test reads the same bytes on every run, so
// a replayed name matches the name of the recording.
type Source struct {
	mu      sync.Mutex
	seed    []byte
	counter uint64
}

// NewSource builds a source from one seed.
func NewSource(seed []byte) *Source {
	return &Source{seed: append([]byte(nil), seed...)}
}

// Read fills p with the next bytes of the stream, and reports no error.
func (s *Source) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for filled := 0; filled < len(p); {
		var counter [8]byte
		binary.BigEndian.PutUint64(counter[:], s.counter)
		s.counter++
		block := sha256.Sum256(append(append([]byte(nil), s.seed...), counter[:]...))
		filled += copy(p[filled:], block[:])
	}
	return len(p), nil
}

// Hex answers the next size bytes of the stream as a hexadecimal string.
func (s *Source) Hex(size int) string {
	raw := make([]byte, size)
	_, _ = s.Read(raw)
	return hex.EncodeToString(raw)
}

// SeedPath answers the file that holds the seed of one cassette.
func SeedPath(cassetteDir, name string) string {
	return filepath.Join(cassetteDir, name+".seed")
}

// missingSeedMessage names the file and the targets that write it.
const missingSeedMessage = "The seed file %s is missing. Run %s to write it."

// unreadableSeedMessage names the file a run cannot read.
const unreadableSeedMessage = "The seed file %s holds no hexadecimal seed. Run %s to write it again."

// sourceFor answers the seed source of one test. Record draws a fresh seed and writes it, replay
// reads the seed of the recording, and live draws a fresh seed and writes nothing.
func sourceFor(mode Mode, cassetteDir, name string) (*Source, error) {
	path := SeedPath(cassetteDir, name)

	if mode == Replay {
		raw, err := os.ReadFile(path) //nolint:gosec // G304: the caller names the cassette directory.
		if err != nil {
			//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
			return nil, fmt.Errorf(missingSeedMessage, path, recordTargets)
		}
		seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(seed) == 0 {
			//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
			return nil, fmt.Errorf(unreadableSeedMessage, path, recordTargets)
		}
		return NewSource(seed), nil
	}

	seed := make([]byte, seedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	if mode == Record {
		if err := os.MkdirAll(cassetteDir, 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(seed)+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	return NewSource(seed), nil
}
