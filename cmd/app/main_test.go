package main

import (
	"os"
	"path/filepath"
	"testing"

	"maragu.dev/is"
)

func TestPrepareUploadsDir(t *testing.T) {
	t.Run("should create the uploads directory in the temporary data directory", func(t *testing.T) {
		root := t.TempDir()

		dir, removed, err := prepareUploadsDir(root)
		is.NotError(t, err)
		is.Equal(t, filepath.Join(root, "uploads"), dir)
		is.Equal(t, 0, removed)
		info, err := os.Stat(dir)
		is.NotError(t, err)
		is.True(t, info.IsDir(), "not a directory")
	})

	t.Run("should empty the uploads directory of what an earlier run left, and nothing else", func(t *testing.T) {
		root := t.TempDir()
		is.NotError(t, os.MkdirAll(filepath.Join(root, "uploads", "nested"), 0o700))
		is.NotError(t, os.WriteFile(filepath.Join(root, "uploads", "upload-1"), []byte("left over"), 0o600))
		is.NotError(t, os.WriteFile(filepath.Join(root, "uploads", "nested", "upload-2"), []byte("left over"), 0o600))
		is.NotError(t, os.WriteFile(filepath.Join(root, "other"), []byte("not an upload"), 0o600))

		dir, removed, err := prepareUploadsDir(root)
		is.NotError(t, err)
		is.Equal(t, 2, removed)
		entries, err := os.ReadDir(dir)
		is.NotError(t, err)
		is.Equal(t, 0, len(entries))
		_, err = os.Stat(filepath.Join(root, "other"))
		is.NotError(t, err)
	})

	t.Run("should refuse an uploads directory that is a link to another directory, emptying nothing", func(t *testing.T) {
		root, elsewhere := t.TempDir(), t.TempDir()
		is.NotError(t, os.WriteFile(filepath.Join(elsewhere, "keep"), []byte("not an upload"), 0o600))
		is.NotError(t, os.Symlink(elsewhere, filepath.Join(root, "uploads")))

		_, _, err := prepareUploadsDir(root)
		is.True(t, err != nil, "expected an error")
		_, err = os.Stat(filepath.Join(elsewhere, "keep"))
		is.NotError(t, err)
	})
}

func TestParseAbsoluteURL(t *testing.T) {
	t.Run("should parse an absolute URL with a host", func(t *testing.T) {
		u, err := parseAbsoluteURL("BASE_URL", "https://app.example.com/")
		is.NotError(t, err)
		is.Equal(t, "https://app.example.com/", u.String())
	})

	tests := []struct {
		name  string
		value string
	}{
		{name: "should refuse a URL without a scheme", value: "app.example.com"},
		{name: "should refuse a URL without a host", value: "https:///path"},
		{name: "should refuse an empty value", value: ""},
		{name: "should refuse a value that does not parse", value: ":nope"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAbsoluteURL("BASE_URL", test.value)
			is.True(t, err != nil, "expected an error")
		})
	}
}
