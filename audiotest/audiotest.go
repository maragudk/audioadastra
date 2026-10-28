// Package audiotest has audio files for tests.
package audiotest

import (
	_ "embed"
	"os"
	"path/filepath"
	"testing"
)

// FLAC is two seconds of a 440 Hz sine tone, mono at 8 kHz, in FLAC.
//
//go:embed testdata/tone.flac
var FLAC []byte

// WriteFile with the given name and content to a temporary directory of the test, and return its path.
func WriteFile(t *testing.T, name string, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
