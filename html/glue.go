package html

import (
	"maragu.dev/glue/html"

	. "maragu.dev/gomponents"
)

type PageProps = html.PageProps

type PageFunc = html.PageFunc

func ErrorPage(props PageProps) Node {
	return html.ErrorPage(Page, props)
}

func NotFoundPage(props PageProps) Node {
	return html.NotFoundPage(Page, props)
}
