package lexicons_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"maragu.dev/is"
)

func TestLexicons(t *testing.T) {
	cat := lexicon.NewBaseCatalog()
	is.NotError(t, cat.LoadDirectory("com"))

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
