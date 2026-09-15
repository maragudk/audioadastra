package html_test

import (
	"strings"
	"testing"

	"maragu.dev/is"

	"app/html"
)

func TestLoginPage(t *testing.T) {
	t.Run("renders a form posting the handle to /login", func(t *testing.T) {
		page := renderLogin(t, html.LoginPageProps{})

		is.True(t, strings.Contains(page, `<form action="/login" method="post"`), "no login form")
		is.True(t, strings.Contains(page, `name="handle"`), "no handle field")
		is.True(t, strings.Contains(page, `placeholder="you.bsky.social"`), "no placeholder")
		is.True(t, strings.Contains(page, `>Log in</button>`), "no submit button")
		is.True(t, !strings.Contains(page, `name="redirect"`), "redirect field rendered without a redirect")
		is.True(t, !strings.Contains(page, `role="alert"`), "error rendered without an error")
	})

	t.Run("renders the error, the typed handle and the redirect, escaped", func(t *testing.T) {
		page := renderLogin(t, html.LoginPageProps{Handle: `"><script>`, Redirect: "/somewhere", Error: "Nope <b>"})

		is.True(t, strings.Contains(page, `role="alert"`), "no error")
		is.True(t, strings.Contains(page, `Nope &lt;b&gt;`), "error not escaped")
		is.True(t, strings.Contains(page, `value="&#34;&gt;&lt;script&gt;"`), "handle not escaped")
		is.True(t, strings.Contains(page, `<input type="hidden" name="redirect" value="/somewhere">`), "no redirect field")
	})
}

func renderLogin(t *testing.T, props html.LoginPageProps) string {
	t.Helper()

	var b strings.Builder
	is.NotError(t, html.LoginPage(props).Render(&b))
	return b.String()
}
