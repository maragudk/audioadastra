package html_test

import (
	"strings"
	"testing"
	"time"

	"maragu.dev/is"

	"app/html"
	"app/model"
)

func TestProfilePage(t *testing.T) {
	t.Run("renders the handle, an upload link and a logout form", func(t *testing.T) {
		page := renderNode(t, html.ProfilePage(html.ProfilePageProps{Handle: "alice.test"}))

		is.True(t, strings.Contains(page, `@alice.test`), "no handle")
		is.True(t, strings.Contains(page, `<a href="/upload"`), "no upload link")
		is.True(t, strings.Contains(page, `<form action="/logout" method="post">`), "no logout form")
		is.True(t, strings.Contains(page, `>Log out</button>`), "no logout button")
	})

	t.Run("says there are no tracks yet", func(t *testing.T) {
		page := renderNode(t, html.ProfilePage(html.ProfilePageProps{Handle: "alice.test"}))

		is.True(t, strings.Contains(page, "No tracks yet."), "no empty state")
		is.True(t, !strings.Contains(page, `id="tracks"`), "track list rendered")
		is.True(t, !strings.Contains(page, `role="status"`), "notice rendered")
	})

	t.Run("lists the tracks in the order given, with their upload dates", func(t *testing.T) {
		page := renderNode(t, html.ProfilePage(html.ProfilePageProps{
			Handle: "alice.test",
			Tracks: []model.Track{
				{Title: "Newer <track>", Created: model.Time{T: time.Date(2026, 10, 9, 23, 30, 0, 0, time.UTC)}},
				{Title: "Older", Created: model.Time{T: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}},
			},
		}))

		is.True(t, !strings.Contains(page, "No tracks yet."), "empty state rendered")
		newer := strings.Index(page, "Newer &lt;track&gt;")
		older := strings.Index(page, "Older")
		is.True(t, newer > 0 && older > newer, "tracks not listed in order")
		is.True(t, strings.Contains(page, `<time datetime="2026-10-09" class="shrink-0 text-sm text-gray-600">9 October 2026</time>`), "no upload date")
	})

	t.Run("shows a notice", func(t *testing.T) {
		page := renderNode(t, html.ProfilePage(html.ProfilePageProps{Handle: "alice.test", Notice: "Your track is published."}))

		is.True(t, strings.Contains(page, `role="status"`), "no notice")
		is.True(t, strings.Contains(page, "Your track is published."), "no notice text")
	})
}
