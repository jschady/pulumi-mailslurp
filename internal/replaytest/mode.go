// Package replaytest records the HTTP calls the MailSlurp tests make, and replays them later, so
// a test run needs no account.
package replaytest

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// ModeVariable names the environment variable that picks the mode.
const ModeVariable = "MAILSLURP_TEST_MODE"

// Mode is one of the three ways a test run reaches the MailSlurp API.
type Mode string

const (
	// Replay reads a cassette and sends nothing. An unset variable means this mode.
	Replay Mode = "replay"

	// Record sends every call and writes a cassette. It needs an API key.
	Record Mode = "record"

	// Live sends every call and writes nothing. It needs an API key.
	Live Mode = "live"
)

// everyMode lists the three values in the order the refusal names them.
func everyMode() []Mode { return []Mode{Replay, Record, Live} }

// ReadMode answers the mode the environment asks for. An empty value means replay.
func ReadMode() (Mode, error) { return modeOf(os.Getenv(ModeVariable)) }

// ModeOf answers the mode, and fails the test when the variable names no mode.
func ModeOf(t *testing.T) Mode {
	t.Helper()
	mode, err := ReadMode()
	if err != nil {
		t.Fatal(err)
	}
	return mode
}

// unknownModeMessage names the variable, the value it holds, and the three values it takes.
const unknownModeMessage = "%s holds %q, which names no mode. Set it to %s."

func modeOf(value string) (Mode, error) {
	if value == "" {
		return Replay, nil
	}
	names := make([]string, 0, len(everyMode()))
	for _, mode := range everyMode() {
		if value == string(mode) {
			return mode, nil
		}
		names = append(names, string(mode))
	}
	//nolint:staticcheck // ST1005: user-facing diagnostics are full sentences by design.
	return "", fmt.Errorf(unknownModeMessage, ModeVariable, value, strings.Join(names, ", "))
}
