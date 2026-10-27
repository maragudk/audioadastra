package bound_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"hegel.dev/go/hegel"
	"maragu.dev/is"

	"app/bound"
)

func TestText(t *testing.T) {
	t.Run("should keep text of up to 256 bytes, and cut longer text to a prefix of whole characters, marked", func(t *testing.T) {
		hegel.Test(t, func(ht *hegel.T) {
			s := hegel.Draw(ht, hegel.OneOf(
				hegel.Text().MaxSize(400),
				hegel.Map(hegel.Binary(0, 400), func(b []byte) string { return string(b) }),
				hegel.Map(hegel.Integers(0, 300), func(n int) string { return strings.Repeat("é", n) }),
				hegel.Map(hegel.Integers(0, 100), func(n int) string { return strings.Repeat("👨‍👩‍👧‍👦", n) }),
				// Continuation bytes, with no character boundary to cut at.
				hegel.Map(hegel.Integers(0, 300), func(n int) string { return "a" + strings.Repeat("\x80", n) }),
			))

			out := bound.Text(s)
			if len(s) <= 256 {
				is.Equal(ht, s, out)
				return
			}
			prefix, ok := strings.CutSuffix(out, "…[truncated]")
			is.True(ht, ok, "no marker on", out)
			is.True(ht, strings.HasPrefix(s, prefix), "not a prefix")
			is.True(ht, len(prefix) <= 256 && len(prefix) > 256-utf8.UTFMax, "prefix of", len(prefix), "bytes")
			if utf8.ValidString(s) {
				is.True(ht, utf8.ValidString(out), "cut a character")
			}
		}, hegel.WithTestCases(2000))
	})
}

func TestError(t *testing.T) {
	t.Run("should cut the error's text and still wrap it", func(t *testing.T) {
		err := bound.Error(fmt.Errorf("%v: %w", strings.Repeat("x", 1000), context.Canceled))

		is.Equal(t, strings.Repeat("x", 256)+"…[truncated]", err.Error())
		is.True(t, errors.Is(err, context.Canceled), "not wrapped")
	})
}
