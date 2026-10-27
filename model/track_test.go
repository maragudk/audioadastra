package model_test

import (
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"hegel.dev/go/hegel"
	"maragu.dev/is"

	"app/lexicons"
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

	t.Run("should trim whole graphemes of whitespace only, keeping a mark that combines with a space", func(t *testing.T) {
		u, err := model.TrackUpload{Title: " \u0301Sounds of Earth", Description: "Whale song\u0600 \n"}.Normalized()
		is.NotError(t, err)
		is.Equal(t, " \u0301Sounds of Earth", u.Title)
		is.Equal(t, "Whale song\u0600 ", u.Description)
	})

	t.Run("should trim every control before a zero-width joiner, as it does not join to a control", func(t *testing.T) {
		u, err := model.TrackUpload{Title: "t", Description: "\t\t\u200dWhale song"}.Normalized()
		is.NotError(t, err)
		is.Equal(t, "\u200dWhale song", u.Description)
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

	t.Run("should either refuse an upload or give one within the lexicon's limits, as valid UTF-8", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			in := drawUpload(ht)

			out, err := in.Normalized()
			if err != nil {
				return
			}
			for name, text := range map[string]string{"title": out.Title, "description": out.Description} {
				is.True(ht, utf8.ValidString(text), name, "is not valid UTF-8")
			}
			is.True(ht, out.Title != "", "empty title")
			is.True(ht, len(out.Title) <= model.TrackTitleMaxBytes, "title over the byte limit")
			is.True(ht, uniseg.GraphemeClusterCount(out.Title) <= model.TrackTitleMaxGraphemes, "title over the grapheme limit")
			is.True(ht, len(out.Description) <= model.TrackDescriptionMaxBytes, "description over the byte limit")
			is.True(ht, uniseg.GraphemeClusterCount(out.Description) <= model.TrackDescriptionMaxGraphemes, "description over the grapheme limit")
		}, hegel.WithTestCases(1000))
	})

	t.Run("should refuse an upload exactly when its trimmed text breaks a rule, with the first rule broken", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			in := drawUpload(ht)

			out, err := in.Normalized()
			is.Equal(ht, in.AudioPath, out.AudioPath)
			var want error
			switch {
			case !utf8.ValidString(out.Title) || !utf8.ValidString(out.Description):
				want = model.ErrorTrackTextInvalid
			case out.Title == "":
				want = model.ErrorTrackTitleMissing
			case len(out.Title) > model.TrackTitleMaxBytes || uniseg.GraphemeClusterCount(out.Title) > model.TrackTitleMaxGraphemes:
				want = model.ErrorTrackTitleTooLong
			case len(out.Description) > model.TrackDescriptionMaxBytes || uniseg.GraphemeClusterCount(out.Description) > model.TrackDescriptionMaxGraphemes:
				want = model.ErrorTrackDescriptionTooLong
			}
			if want == nil {
				is.NotError(ht, err)
				return
			}
			is.True(ht, errors.Is(err, want), "got", err, "want", want)
		}, hegel.WithTestCases(1000))
	})

	t.Run("should only take whole graphemes of whitespace off either end of the text", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			in := drawUpload(ht)

			out, _ := in.Normalized()
			for _, pair := range [][2]string{{in.Title, out.Title}, {in.Description, out.Description}} {
				before, after := pair[0], pair[1]
				start := strings.Index(before, after)
				is.True(ht, start >= 0, "trimmed text is not part of the text")
				// The trimmed text sits where only whitespace is around it, which is the first place it is found,
				// unless it is empty.
				if after == "" {
					is.True(ht, onlyWhitespace(before), "emptied text that is not all whitespace:", before)
					continue
				}
				end := start + len(after)
				is.True(ht, onlyWhitespace(before[:start]), "took off more than whitespace at the start")
				is.True(ht, onlyWhitespace(before[end:]), "took off more than whitespace at the end")
				first, last := firstAndLastGraphemes(after)
				is.True(ht, !onlyWhitespace(first), "left a grapheme of whitespace at the start:", after)
				is.True(ht, !onlyWhitespace(last), "left a grapheme of whitespace at the end:", after)
				if utf8.ValidString(before) {
					is.True(ht, graphemeBoundary(before, start), "split a grapheme at the start:", before)
					is.True(ht, graphemeBoundary(before, end), "split a grapheme at the end:", before)
				}
			}
		}, hegel.WithTestCases(1000))
	})

	t.Run("should give the same upload when normalized again", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			in := drawUpload(ht)

			once, err := in.Normalized()
			twice, errAgain := once.Normalized()
			is.Equal(ht, once, twice)
			is.Equal(ht, err, errAgain)
		}, hegel.WithTestCases(1000))
	})

	catalog, err := lexicons.NewCatalog()
	is.NotError(t, err)

	t.Run("should only accept text that the track lexicon accepts too", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			in := drawUpload(ht)

			out, err := in.Normalized()
			ht.Assume(err == nil)
			record := map[string]any{
				"$type":     model.CollectionTrack.String(),
				"audio":     map[string]any{"original": model.Blob{CID: "bafkreichwqg55i6naccjmhlvfsbcsdy62isbqlz2xk7kali463p4u3ie4e", MIMEType: "audio/flac", Size: 42}},
				"title":     out.Title,
				"createdAt": model.Time{T: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}.String(),
			}
			if out.Description != "" {
				record["description"] = out.Description
			}
			is.NotError(ht, catalog.ValidateRecord(record, model.CollectionTrack))
		}, hegel.WithTestCases(1000))
	})

	t.Run("should only refuse text as too long that the track lexicon refuses too", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			// Text with a run long enough to break a limit in it, as random text rarely is.
			long := hegel.Composite(func(tc hegel.TestCase) string {
				run := strings.Repeat(hegel.Draw(tc, validPieces()), hegel.Draw(tc, hegel.Integers(1, 2*model.TrackDescriptionMaxGraphemes)))
				if cut := model.TrackDescriptionMaxBytes + 100; len(run) > cut {
					for !utf8.RuneStart(run[cut]) {
						cut--
					}
					run = run[:cut]
				}
				return hegel.Draw(tc, trackText(100)) + run + hegel.Draw(tc, trackText(100))
			})
			in := model.TrackUpload{Title: hegel.Draw(ht, long), Description: hegel.Draw(ht, long)}

			out, err := in.Normalized()
			ht.Assume(errors.Is(err, model.ErrorTrackTitleTooLong) || errors.Is(err, model.ErrorTrackDescriptionTooLong))
			title, description := out.Title, "d"
			if errors.Is(err, model.ErrorTrackDescriptionTooLong) {
				title, description = "t", out.Description
			}
			record := map[string]any{
				"$type":       model.CollectionTrack.String(),
				"audio":       map[string]any{"original": model.Blob{CID: "bafkreichwqg55i6naccjmhlvfsbcsdy62isbqlz2xk7kali463p4u3ie4e", MIMEType: "audio/flac", Size: 42}},
				"title":       title,
				"description": description,
				"createdAt":   model.Time{T: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}.String(),
			}
			is.True(ht, catalog.ValidateRecord(record, model.CollectionTrack) != nil, "lexicon accepts what is too long")
		}, hegel.WithTestCases(300))
	})

	t.Run("should trim just as the standard library does for text where no grapheme mixes whitespace with anything else", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			title := hegel.Draw(ht, hegel.Map(hegel.Lists(hegel.OneOf(hegel.Text(), hegel.SampledFrom([]string{" ", "\t", "\r\n", "\u0085", "\u00a0", "\u3000"}))).MaxSize(10),
				func(parts []string) string { return strings.Join(parts, "") }))
			// Text where no grapheme mixes whitespace with anything else, such as a mark on a space.
			g := uniseg.NewGraphemes(title)
			for g.Next() {
				ht.Assume(onlyWhitespace(g.Str()) || strings.IndexFunc(g.Str(), unicode.IsSpace) < 0)
			}

			out, _ := model.TrackUpload{Title: title}.Normalized()
			is.Equal(ht, strings.TrimSpace(title), out.Title)
		}, hegel.WithTestCases(1000))
	})
}

// drawUpload with a title and description each made of runs of text that is hard on normalization:
// whitespace of every kind, emoji and their ZWJ sequences, combining marks, prepended marks, regional
// indicators, invalid UTF-8, and random text, repeated often enough to reach the limits.
func drawUpload(ht *hegel.T) model.TrackUpload {
	return model.TrackUpload{
		Title:       hegel.Draw(ht, trackText(model.TrackTitleMaxBytes)),
		Description: hegel.Draw(ht, trackText(model.TrackDescriptionMaxBytes)),
		AudioPath:   hegel.Draw(ht, hegel.Text().MaxSize(20)),
	}
}

// trackText of runs of hard pieces, up to a little over the given number of bytes.
func trackText(maxBytes int) hegel.Generator[string] {
	valid := validPieces()
	invalid := hegel.OneOf(
		hegel.SampledFrom([]string{"\xff", "\xe2\x80", "\xc0\xaf", "\xed\xa0\x80"}),
		hegel.Map(hegel.Binary(1, 4), func(b []byte) string { return string(b) }),
	)
	run := func(piece hegel.Generator[string]) hegel.Generator[string] {
		return hegel.Composite(func(tc hegel.TestCase) string {
			p := hegel.Draw(tc, piece)
			// Mostly single pieces, sometimes runs long enough to reach the limits.
			n := 1
			if hegel.Draw(tc, hegel.WeightedBooleans(0.05)) {
				n = hegel.Draw(tc, hegel.Integers(1, maxBytes+10))
			}
			return strings.Repeat(p, n)
		})
	}
	validRuns := run(valid)
	anyRuns := run(hegel.OneOf(valid, invalid))
	return hegel.Composite(func(tc hegel.TestCase) string {
		// Mostly valid UTF-8, so that most inputs get past the first check.
		runs := validRuns
		if hegel.Draw(tc, hegel.WeightedBooleans(0.1)) {
			runs = anyRuns
		}
		s := strings.Join(hegel.Draw(tc, hegel.Lists(runs).MaxSize(12)), "")
		if cut := maxBytes * 5 / 4; len(s) > cut {
			for cut > 0 && !utf8.RuneStart(s[cut]) {
				cut--
			}
			s = s[:cut]
		}
		return s
	})
}

// validPieces of text that are hard on normalization: whitespace of every kind, emoji and their ZWJ
// sequences, combining and prepended marks, regional indicators, and random text.
func validPieces() hegel.Generator[string] {
	return hegel.OneOf(
		hegel.SampledFrom([]string{
			" ", "\t", "\n", "\r\n", "\v", "\f", "\u0085", "\u00a0", "\u1680", "\u2000", "\u200a", "\u2028", "\u2029", "\u202f", "\u205f", "\u3000",
			"\u200b", "\ufeff", "\u180e", "\u200d", "\x00",
			"a", "Z", ".", "é", "e\u0301", "\u0301", "\u0301\u0302", "\u0903", "\u0600", "\u0d4e", "\u1100\u1161\u11a8",
			"😀", "👍🏽", "👨‍👩‍👧‍👦", "🏳️‍🌈", "🇩🇰", "🇩", "\u2764\ufe0f", "\U0001f9d1\u200d\U0001f3a4",
		}),
		hegel.Text().MaxSize(8),
	)
}

// onlyWhitespace reports whether the text is nothing but whitespace, or empty.
func onlyWhitespace(s string) bool {
	return strings.TrimFunc(s, unicode.IsSpace) == ""
}

// firstAndLastGraphemes of the text, both empty when it is.
func firstAndLastGraphemes(s string) (first, last string) {
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		if first == "" {
			first = g.Str()
		}
		last = g.Str()
	}
	return first, last
}

// graphemeBoundary reports whether the byte offset in the text is between two grapheme clusters, or at
// either end.
func graphemeBoundary(s string, offset int) bool {
	if offset == 0 || offset == len(s) {
		return true
	}
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		start, end := g.Positions()
		if start == offset || end == offset {
			return true
		}
		if start > offset {
			return false
		}
	}
	return false
}

func TestBlob_MarshalJSON(t *testing.T) {
	t.Run("should marshal to the data model's blob form", func(t *testing.T) {
		b, err := json.Marshal(map[string]any{"original": model.Blob{CID: "bafkreia", MIMEType: "audio/flac", Size: 42}})
		is.NotError(t, err)
		is.Equal(t, `{"original":{"$type":"blob","ref":{"$link":"bafkreia"},"mimeType":"audio/flac","size":42}}`, string(b))
	})
}
