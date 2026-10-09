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

	"app/lexicons"
	"app/model"
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
		nsid string
		file string
		// err is a substring of the expected validation error, or empty if the fixture is valid.
		err string
	}{
		{
			name: "should accept a full actor profile",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/full-valid.json",
		},
		{
			name: "should accept a minimal actor profile with only $type and createdAt",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/minimal-valid.json",
		},
		{
			name: "should reject an actor profile missing createdAt",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/missing-created-at-invalid.json",
			err:  "required field missing: createdAt",
		},
		{
			name: "should reject an actor profile with a displayName over 64 graphemes",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/display-name-too-long-invalid.json",
			err:  "string length (graphemes) outside specified range",
		},
		{
			name: "should reject an actor profile with a description over 1000 graphemes",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/description-too-long-invalid.json",
			err:  "string length (graphemes) outside specified range",
		},
		{
			name: "should reject an actor profile with a website that is not a URI",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/bad-website-invalid.json",
			err:  "URI syntax",
		},
		{
			name: "should reject an actor profile with a createdAt that is not a datetime",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/bad-created-at-invalid.json",
			err:  "Datetime syntax",
		},
		{
			name: "should reject an actor profile missing $type",
			nsid: "com.audioadastra.actor.profile",
			file: "com/audioadastra/actor/profile/missing-type-invalid.json",
			err:  "missing $type",
		},
		{
			name: "should accept a full track",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/full-valid.json",
		},
		{
			name: "should accept a minimal track with a FLAC original and no description",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/minimal-valid.json",
		},
		{
			name: "should reject a track missing audio",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/missing-audio-invalid.json",
			err:  "required field missing: audio",
		},
		{
			name: "should reject a track missing the original audio",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/missing-original-invalid.json",
			err:  "required field missing: original",
		},
		{
			name: "should accept a track whose original audio the PDS labelled with a non-audio mimetype",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/original-relabelled-by-pds-valid.json",
		},
		{
			name: "should reject a track missing title",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/missing-title-invalid.json",
			err:  "required field missing: title",
		},
		{
			name: "should reject a track with an empty title",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/empty-title-invalid.json",
			err:  "string length outside specified range: 0",
		},
		{
			name: "should reject a track with a title over 300 graphemes",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/title-too-long-invalid.json",
			err:  "string length (graphemes) outside specified range: 301",
		},
		{
			name: "should reject a track with a title over 3000 bytes",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/title-too-many-bytes-invalid.json",
			err:  "string length outside specified range: 3025",
		},
		{
			name: "should reject a track with a description over 5000 graphemes",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/description-too-long-invalid.json",
			err:  "string length (graphemes) outside specified range: 5001",
		},
		{
			name: "should reject a track missing createdAt",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/missing-created-at-invalid.json",
			err:  "required field missing: createdAt",
		},
		{
			name: "should reject a track with a createdAt that is not a datetime",
			nsid: "com.audioadastra.track",
			file: "com/audioadastra/track/bad-created-at-invalid.json",
			err:  "Datetime syntax",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", test.file))
			is.NotError(t, err)

			data, err := atdata.UnmarshalJSON(raw)
			is.NotError(t, err)

			err = lexicon.ValidateRecord(cat, data, test.nsid, 0)
			if test.err == "" {
				is.NotError(t, err)
				return
			}
			is.True(t, err != nil, "expected a validation error")
			is.True(t, strings.Contains(err.Error(), test.err), "unexpected validation error:", err)
		})
	}
}

func TestNewCatalog(t *testing.T) {
	t.Run("should load the actor profile schema", func(t *testing.T) {
		cat, err := lexicons.NewCatalog()
		is.NotError(t, err)

		is.NotError(t, cat.ValidateRecord(map[string]any{"$type": model.CollectionActorProfile.String(), "createdAt": "2026-09-21T00:00:00.000Z"}, model.CollectionActorProfile))
		err = cat.ValidateRecord(map[string]any{"$type": model.CollectionActorProfile.String()}, model.CollectionActorProfile)
		is.True(t, err != nil, "expected a validation error")
		is.True(t, strings.Contains(err.Error(), "required field missing: createdAt"), err.Error())
	})

	t.Run("should load the track schema", func(t *testing.T) {
		cat, err := lexicons.NewCatalog()
		is.NotError(t, err)

		raw, err := os.ReadFile("testdata/com/audioadastra/track/minimal-valid.json")
		is.NotError(t, err)
		record, err := atdata.UnmarshalJSON(raw)
		is.NotError(t, err)

		is.NotError(t, cat.ValidateRecord(record, "com.audioadastra.track"))
		delete(record, "audio")
		err = cat.ValidateRecord(record, "com.audioadastra.track")
		is.True(t, err != nil, "expected a validation error")
		is.True(t, strings.Contains(err.Error(), "required field missing: audio"), err.Error())
	})

	t.Run("should validate a track record with the app's own blob type as it goes on the wire", func(t *testing.T) {
		cat, err := lexicons.NewCatalog()
		is.NotError(t, err)

		record := map[string]any{
			"$type": model.CollectionTrack.String(),
			"audio": map[string]any{
				"original": model.Blob{CID: "bafkreichwqg55i6naccjmhlvfsbcsdy62isbqlz2xk7kali463p4u3ie4e", MIMEType: "audio/flac", Size: 4242},
			},
			"title":     "Sounds of Earth",
			"createdAt": "2026-10-09T12:00:00.000Z",
		}
		is.NotError(t, cat.ValidateRecord(record, model.CollectionTrack))

		record["audio"] = map[string]any{"original": model.Blob{CID: "not a CID", MIMEType: "audio/flac", Size: 4242}}
		is.True(t, cat.ValidateRecord(record, model.CollectionTrack) != nil, "expected an error for a blob with a bad CID")
	})
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
