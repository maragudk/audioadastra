package lexicons_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/lex/lexlint"
	"maragu.dev/is"
)

func TestLexiconSchemas(t *testing.T) {
	for _, schema := range readSchemaFiles(t) {
		t.Run("should lint "+schema.file.ID+" without issues", func(t *testing.T) {
			is.NotError(t, schema.file.FinishParse())

			issues := lexlint.LintSchemaFile(&schema.file)

			// Decode again strictly, to catch top-level keys the schema language does not know about.
			dec := json.NewDecoder(bytes.NewReader(schema.raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(new(lexicon.SchemaFile)); err != nil {
				issues = append(issues, lexlint.LintIssue{
					LintLevel: "warn",
					LintName:  "unexpected-field",
					Message:   err.Error(),
				})
			}

			for _, issue := range issues {
				t.Errorf("[%s] %s: %s", issue.LintLevel, issue.LintName, issue.Message)
			}
		})
	}
}

func TestLexicons(t *testing.T) {
	cat := lexicon.NewBaseCatalog()
	for _, schema := range readSchemaFiles(t) {
		is.NotError(t, cat.AddSchemaFile(schema.file), schema.path)
	}

	tests := []struct {
		name string
		file string
		// err is a substring of the expected validation error, or empty if the fixture is valid.
		err string
	}{
		{
			name: "should accept a full actor profile",
			file: "com/audioadastra/actor/profile/full-valid.json",
		},
		{
			name: "should accept a minimal actor profile with only $type and createdAt",
			file: "com/audioadastra/actor/profile/minimal-valid.json",
		},
		{
			name: "should reject an actor profile missing createdAt",
			file: "com/audioadastra/actor/profile/missing-created-at-invalid.json",
			err:  "required field missing: createdAt",
		},
		{
			name: "should reject an actor profile with a displayName over 64 graphemes",
			file: "com/audioadastra/actor/profile/display-name-too-long-invalid.json",
			err:  "string length (graphemes) outside specified range",
		},
		{
			name: "should reject an actor profile with a description over 1000 graphemes",
			file: "com/audioadastra/actor/profile/description-too-long-invalid.json",
			err:  "string length (graphemes) outside specified range",
		},
		{
			name: "should reject an actor profile with a website that is not a URI",
			file: "com/audioadastra/actor/profile/bad-website-invalid.json",
			err:  "URI syntax",
		},
		{
			name: "should reject an actor profile with a createdAt that is not a datetime",
			file: "com/audioadastra/actor/profile/bad-created-at-invalid.json",
			err:  "Datetime syntax",
		},
		{
			name: "should reject an actor profile missing $type",
			file: "com/audioadastra/actor/profile/missing-type-invalid.json",
			err:  "missing $type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", test.file))
			is.NotError(t, err)

			data, err := atdata.UnmarshalJSON(raw)
			is.NotError(t, err)

			err = lexicon.ValidateRecord(cat, data, "com.audioadastra.actor.profile", 0)
			if test.err == "" {
				is.NotError(t, err)
				return
			}
			is.True(t, err != nil, "expected a validation error")
			is.True(t, strings.Contains(err.Error(), test.err), "unexpected validation error:", err)
		})
	}
}

type schemaFile struct {
	path string
	raw  []byte
	file lexicon.SchemaFile
}

// readSchemaFiles from every JSON file under the current directory, skipping testdata.
// The files are parsed but not finished; call [lexicon.SchemaFile.FinishParse] before linting,
// and let [lexicon.BaseCatalog.AddSchemaFile] do it itself when loading a catalog.
func readSchemaFiles(t *testing.T) []schemaFile {
	t.Helper()

	var schemas []schemaFile
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return fs.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var file lexicon.SchemaFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return fmt.Errorf("parsing %v: %w", path, err)
		}
		schemas = append(schemas, schemaFile{path: path, raw: raw, file: file})
		return nil
	})
	is.NotError(t, err)
	is.True(t, len(schemas) > 0, "expected at least one lexicon schema")
	return schemas
}
