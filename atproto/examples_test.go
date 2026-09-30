package atproto_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"maragu.dev/is"
)

// TestExampleDIDs guards the example DIDs in code and fixtures: every one must be well-formed, which
// for the plc method is exactly 24 characters of base32 after the method.
func TestExampleDIDs(t *testing.T) {
	t.Run("should find only well-formed plc DIDs in Go files, SQL fixtures and JSON fixtures", func(t *testing.T) {
		// Assembled, so this file does not hold an occurrence of its own.
		prefix := "did:" + "plc:"
		occurrence := regexp.MustCompile(regexp.QuoteMeta(prefix) + `[A-Za-z0-9]*`)
		wellFormed := regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `[a-z2-7]{24}$`)

		var found int
		err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if path != ".." && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "tailwind-plus") || name == "data" || name == "node_modules") {
					return fs.SkipDir
				}
				return nil
			}
			if ext := filepath.Ext(name); ext != ".go" && ext != ".sql" && ext != ".json" {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, did := range occurrence.FindAllString(string(content), -1) {
				found++
				_, err := syntax.ParseDID(did)
				is.NotError(t, err, path+": "+did)
				is.True(t, wellFormed.MatchString(did), path+": "+did+" is not 24 characters of base32")
			}
			return nil
		})
		is.NotError(t, err)
		is.True(t, found > 0, "found no example DIDs at all")
	})
}
