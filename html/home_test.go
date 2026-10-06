package html_test

import (
	"regexp"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/html"
)

func TestHomePage(t *testing.T) {
	var b strings.Builder
	is.NotError(t, html.HomePage(html.HomePageProps{}).Render(&b))
	page := b.String()

	t.Run("names the site in the heading for assistive technology, with the constellations hidden from it", func(t *testing.T) {
		h1 := regexp.MustCompile(`(?s)<h1[^>]*>.*?</h1>`).FindString(page)

		is.True(t, strings.Contains(h1, `<span class="sr-only">Audio Ad Astra</span>`), "no accessible name in the heading")
		svgs := regexp.MustCompile(`<svg[^>]*>`).FindAllString(h1, -1)
		is.Equal(t, 2, len(svgs), "want a wide and a narrow constellation")
		for _, svg := range svgs {
			is.True(t, strings.Contains(svg, `aria-hidden="true"`), "constellation is not hidden: "+svg)
		}
	})

	t.Run("leaves the header without a front page link, since the constellation is the name", func(t *testing.T) {
		is.True(t, !strings.Contains(page, `<a href="/"`), "front page links to itself")
	})

	t.Run("links to the Bluesky profile as the main action", func(t *testing.T) {
		is.True(t, regexp.MustCompile(`<a href="https://bsky.app/profile/audioadastra.com"[^>]*>.*?Follow on Bluesky</a>`).MatchString(page))
	})

	t.Run("hides the play button until the script shows it, so it never appears without sound", func(t *testing.T) {
		button := regexp.MustCompile(`<button[^>]*id="sky-play"[^>]*>`).FindString(page)

		is.True(t, button != "", "no play button")
		is.True(t, strings.Contains(button, `hidden`), "play button is not hidden: "+button)
		is.True(t, regexp.MustCompile(`<script src="/scripts/sky\.[^"]+\.js"`).MatchString(page), "no sky script")
	})

	t.Run("gives every constellation star a position for the score", func(t *testing.T) {
		stars := regexp.MustCompile(`<[^>]*class="sky-star[^"]*"[^>]*>`).FindAllString(page, -1)

		is.True(t, len(stars) > 0, "no stars")
		for _, star := range stars {
			is.True(t, strings.Contains(star, `data-x="`) && strings.Contains(star, `data-y="`), "star has no position: "+star)
		}
	})
}
