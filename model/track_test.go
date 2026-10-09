package model_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/model"
)

func TestTrackUpload_Normalized(t *testing.T) {
	t.Run("should trim the title and description", func(t *testing.T) {
		u, err := model.TrackUpload{Title: "  Sounds of Earth \n", Description: "\tWhale song.  ", AudioPath: "/tmp/a"}.Normalized()
		is.NotError(t, err)
		is.Equal(t, "Sounds of Earth", u.Title)
		is.Equal(t, "Whale song.", u.Description)
		is.Equal(t, "/tmp/a", u.AudioPath)
	})

	tests := []struct {
		name        string
		title       string
		description string
		err         error
	}{
		{name: "should refuse a missing title", title: "", err: model.ErrorTrackTitleMissing},
		{name: "should refuse a title that is not UTF-8", title: "Sounds of \xff", err: model.ErrorTrackTextInvalid},
		{name: "should refuse a description that is not UTF-8", title: "t", description: "\xffWhale song.", err: model.ErrorTrackTextInvalid},
		{name: "should refuse a title of only whitespace", title: " \n\t ", err: model.ErrorTrackTitleMissing},
		{name: "should accept a title of 300 graphemes", title: strings.Repeat("a", 300)},
		{name: "should refuse a title of 301 graphemes", title: strings.Repeat("a", 301), err: model.ErrorTrackTitleTooLong},
		// A family emoji is one grapheme of 25 bytes, so 121 of them are within the grapheme limit but over the byte limit.
		{name: "should refuse a title over 3000 bytes in fewer than 300 graphemes", title: strings.Repeat("👨‍👩‍👧‍👦", 121), err: model.ErrorTrackTitleTooLong},
		{name: "should accept an empty description", title: "t", description: ""},
		{name: "should accept a description of 5000 graphemes", title: "t", description: strings.Repeat("a", 5000)},
		{name: "should refuse a description of 5001 graphemes", title: "t", description: strings.Repeat("a", 5001), err: model.ErrorTrackDescriptionTooLong},
		{name: "should refuse a description over 20000 bytes in fewer than 5000 graphemes", title: "t", description: strings.Repeat("👨‍👩‍👧‍👦", 801), err: model.ErrorTrackDescriptionTooLong},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := model.TrackUpload{Title: test.title, Description: test.description}.Normalized()
			if test.err == nil {
				is.NotError(t, err)
				return
			}
			is.Error(t, test.err, err)
		})
	}
}

func TestBlob_MarshalJSON(t *testing.T) {
	t.Run("should marshal to the data model's blob form", func(t *testing.T) {
		b, err := json.Marshal(map[string]any{"original": model.Blob{CID: "bafkreia", MIMEType: "audio/flac", Size: 42}})
		is.NotError(t, err)
		is.Equal(t, `{"original":{"$type":"blob","ref":{"$link":"bafkreia"},"mimeType":"audio/flac","size":42}}`, string(b))
	})
}
