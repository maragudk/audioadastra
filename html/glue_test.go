package html_test

import (
	"strings"
	"testing"

	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
	"maragu.dev/is"

	"app/html"
)

func TestGluePage(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "turns the not-found page into a message on the sky with a way back", title: "Not found", want: "Nothing up here"},
		{name: "turns the error page into a message on the sky with a way back", title: "Something went wrong", want: "Try again in a moment"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var b strings.Builder
			is.NotError(t, html.GluePage(html.PageProps{Title: test.title}, H1(Text(test.title))).Render(&b))
			page := b.String()

			is.True(t, strings.Contains(page, test.want), "no explanation")
			is.True(t, strings.Contains(page, `>Back to the front page</a>`), "no way back")
		})
	}

	t.Run("renders any other page as a plain page", func(t *testing.T) {
		var b strings.Builder
		is.NotError(t, html.GluePage(html.PageProps{Title: "Lost in a black hole"}, H1(Text("Hi"))).Render(&b))

		is.True(t, !strings.Contains(b.String(), "Back to the front page"), "plain page has a way back")
	})
}
