package html

import (
	"maragu.dev/glue/html"

	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type PageProps = html.PageProps

type PageFunc = html.PageFunc

func ErrorPage(props PageProps) Node {
	return html.ErrorPage(errorPage, props)
}

func NotFoundPage(props PageProps) Node {
	return html.NotFoundPage(notFoundPage, props)
}

var (
	errorPage    = skyMessage("Try again in a moment. If it keeps happening, tell us on Bluesky.")
	notFoundPage = skyMessage("Nothing up here. Maybe the link has drifted.")
)

// GluePage is the [PageFunc] for pages the glue server renders itself. Its error and not-found pages
// arrive as a bare heading with the title glue gives them; those become messages on the sky with a
// way back, and everything else is a [Page].
func GluePage(props PageProps, body ...Node) Node {
	switch props.Title {
	case "Something went wrong":
		return errorPage(props, body...)
	case "Not found":
		return notFoundPage(props, body...)
	default:
		return Page(props, body...)
	}
}

// skyMessage is a [PageFunc] for short messages on the sky: the heading the caller gives, a line of
// explanation, and a way back to the front page.
func skyMessage(explanation string) PageFunc {
	return func(props PageProps, body ...Node) Node {
		return Page(props,
			Div(Class("flex grow flex-col justify-center gap-6 py-12 [&_h1]:font-display [&_h1]:text-5xl/tight [&_h1]:font-bold [&_h1]:text-balance sm:[&_h1]:text-6xl/tight"),
				Div(Data("sky-avoid", ""), Class("w-fit"), Group(body)),
				P(Data("sky-avoid", ""), Class("w-fit max-w-2xl text-[1.1875rem]/7 font-bold text-pretty sm:text-2xl/10 sm:font-normal sm:text-pink-50"), Text(explanation)),
				Div(Data("sky-avoid", ""), Class("w-fit"),
					A(Href("/"), Class("inline-flex items-center gap-3 rounded-full bg-white px-7 py-3.5 text-xl font-bold text-pink-700 transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-pink-700 hover:text-white hover:ring-2 hover:ring-white hover:ring-inset motion-reduce:hover:translate-y-0 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"),
						Text("Back to the front page"),
					),
				),
			),
		)
	}
}
