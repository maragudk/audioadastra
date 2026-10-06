package http

import (
	"app/html"

	. "maragu.dev/gomponents"
)

func Home(r *Router) {
	r.Get("/", func(props html.PageProps) (Node, error) {
		return html.HomePage(html.HomePageProps{PageProps: props}), nil
	})
}
