package html_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/html"
)

func TestPage(t *testing.T) {
	t.Run("renders the logo in the header, linking to the front page", func(t *testing.T) {
		link := regexp.MustCompile(`<a href="/"[^>]*>\s*<img[^>]*>\s*</a>`).FindString(render(t))

		is.True(t, link != "", "no logo link to the front page")
		is.True(t, strings.Contains(link, `src="/images/logo.png"`), "logo link has no logo image: "+link)
	})

	t.Run("renders a login link in the header when there is no viewer", func(t *testing.T) {
		page := render(t)

		is.True(t, strings.Contains(page, `<a href="/login"`), "no login link")
		is.True(t, !strings.Contains(page, `action="/logout"`), "logout form rendered for a logged-out page")
	})

	t.Run("renders the handle and a logout form in the header for a viewer", func(t *testing.T) {
		ctx := html.ContextWithViewer(t.Context(), html.Viewer{Handle: "alice.test"})

		var b strings.Builder
		is.NotError(t, html.Page(html.PageProps{Title: "Test", Ctx: ctx}).Render(&b))
		page := b.String()

		is.True(t, strings.Contains(page, `@alice.test`), "no handle")
		is.True(t, regexp.MustCompile(`<form action="/logout" method="post">\s*<button type="submit"[^>]*>Log out</button>`).MatchString(page), "no logout form")
		is.True(t, !strings.Contains(page, `href="/login"`), "login link rendered for a logged-in page")
	})

	t.Run("loads the Datastar script as a module", func(t *testing.T) {
		tag := regexp.MustCompile(`<script[^>]*src="/scripts/datastar\.[^"]+\.js"[^>]*>`).FindString(render(t))

		is.True(t, tag != "", "no Datastar script tag")
		is.True(t, strings.Contains(tag, `type="module"`), "Datastar script tag is not a module: "+tag)
	})

	t.Run("renders the Datastar smoke test attribute, with the expression HTML-escaped", func(t *testing.T) {
		want := `data-init="console.log(&#39;Datastar loaded&#39;)"`

		is.True(t, strings.Contains(render(t), want), "no "+want)
	})
}

// TestDatastarVersion guards the two independent pins of the Datastar version against each other:
// the Go helpers emit attributes that only the matching client runtime understands.
func TestDatastarVersion(t *testing.T) {
	t.Run("the vendored bundle is the version the Makefile pins", func(t *testing.T) {
		makefile, err := os.ReadFile("../Makefile")
		is.NotError(t, err)

		match := regexp.MustCompile(`datastar@(\S+)/bundles/datastar\.js`).FindSubmatch(makefile)
		is.True(t, match != nil, "no Datastar bundle URL in the Makefile")

		bundle, err := os.ReadFile("../public/scripts/datastar.js")
		is.NotError(t, err)

		banner, _, _ := strings.Cut(string(bundle), "\n")
		is.Equal(t, "// Datastar v"+string(match[1]), banner)
	})
}

func render(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	err := html.Page(html.PageProps{Title: "Test"}).Render(&b)
	is.NotError(t, err)
	return b.String()
}
