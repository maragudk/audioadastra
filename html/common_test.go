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

	t.Run("loads the Datastar script as a module", func(t *testing.T) {
		tag := regexp.MustCompile(`<script[^>]*src="/scripts/datastar\.[^"]+\.js"[^>]*>`).FindString(render(t))

		is.True(t, tag != "", "no Datastar script tag")
		is.True(t, strings.Contains(tag, `type="module"`), "Datastar script tag is not a module: "+tag)
	})

	socialLinks := []struct {
		name string
		href string
		rel  string
	}{
		{name: "Bluesky", href: "https://bsky.app/profile/audioadastra.com", rel: "me"},
		{name: "GitHub", href: "https://github.com/maragudk/audioadastra"},
	}

	for _, test := range socialLinks {
		t.Run("renders a "+test.name+" link in the footer, named for assistive technology and with the icon hidden from it", func(t *testing.T) {
			link := regexp.MustCompile(`<a href="` + regexp.QuoteMeta(test.href) + `"[^>]*>.*?</a>`).FindString(render(t))

			is.True(t, link != "", "no "+test.name+" link")
			is.True(t, strings.Contains(link, `title="`+test.name+`"`), test.name+" link has no title: "+link)
			is.True(t, strings.Contains(link, `<span class="sr-only">`+test.name+`</span>`), test.name+" link has no accessible name: "+link)

			if test.rel != "" {
				is.True(t, strings.Contains(link, `rel="`+test.rel+`"`), test.name+" link is not rel="+test.rel+": "+link)
			}

			// The sr-only span is the link's only accessible name, so the icon must stay hidden from
			// assistive technology to avoid announcing the link twice.
			svg := regexp.MustCompile(`<svg[^>]*>`).FindString(link)
			is.True(t, svg != "", test.name+" link has no icon: "+link)
			is.True(t, strings.Contains(svg, `aria-hidden="true"`), test.name+" icon is not hidden: "+svg)
			is.True(t, strings.Contains(link, `<path `), test.name+" icon has no path: "+link)
		})
	}

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
