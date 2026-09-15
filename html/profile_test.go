package html_test

import (
	"strings"
	"testing"

	"maragu.dev/is"

	"app/html"
)

func TestProfilePage(t *testing.T) {
	t.Run("renders the handle and a logout form", func(t *testing.T) {
		var b strings.Builder
		is.NotError(t, html.ProfilePage(html.ProfilePageProps{Handle: "alice.test"}).Render(&b))
		page := b.String()

		is.True(t, strings.Contains(page, `@alice.test`), "no handle")
		is.True(t, strings.Contains(page, `<form action="/logout" method="post">`), "no logout form")
		is.True(t, strings.Contains(page, `>Log out</button>`), "no logout button")
	})
}
