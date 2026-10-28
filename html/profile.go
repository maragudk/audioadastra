package html

import (
	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"

	"app/model"
)

type ProfilePageProps struct {
	PageProps
	// Handle of the logged-in user, or [model.HandleInvalid] when it could not be verified.
	Handle model.Handle
	// Tracks of the logged-in user, newest first.
	Tracks []model.Track
	// Notice to show above the tracks, if any, such as that a track was just published.
	Notice string
}

// ProfilePage of the logged-in user: the handle, the way to upload a track, the user's tracks, and the
// way out.
func ProfilePage(props ProfilePageProps) Node {
	return Page(props.PageProps,
		Div(Class("flex grow flex-col justify-center py-6 sm:py-12"),
			sheet(
				H1(Class("text-center text-3xl/tight font-bold tracking-tight break-words text-gray-950"), Text("@"+props.Handle.String())),

				If(props.Notice != "",
					Div(ID("notice"), Role("status"), Class("mt-8 rounded-xl bg-pink-50 p-4 text-sm/6 text-pink-700"), Text(props.Notice)),
				),

				A(Href("/upload"),
					Class("group mt-8 flex w-full cursor-pointer items-center justify-center gap-2.5 rounded-full bg-primary-600 px-6 py-3 text-lg font-bold text-white transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-primary-700 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary-600 motion-reduce:hover:translate-y-0"),
					sparkleIcon(),
					Text("Upload a track"),
				),

				H2(Class("mt-8 text-sm/6 font-semibold text-gray-900"), Text("Tracks")),
				If(len(props.Tracks) == 0,
					P(Class("mt-2 text-sm/6 text-gray-600"), Text("No tracks yet.")),
				),
				If(len(props.Tracks) > 0,
					Ul(ID("tracks"), Class("mt-2 divide-y divide-gray-100"),
						Map(props.Tracks, func(t model.Track) Node {
							return Li(Class("flex items-baseline justify-between gap-x-4 py-3"),
								P(Class("min-w-0 text-base font-medium break-words text-gray-900"), Text(t.Title)),
								Time(DateTime(t.Created.T.UTC().Format("2006-01-02")), Class("shrink-0 text-sm text-gray-600"),
									Text(t.Created.T.UTC().Format("2 January 2006")),
								),
							)
						}),
					),
				),

				Div(Class("text-center"), Form(Action("/logout"), Method("post"),
					Button(ID("logout"), Type("submit"),
						Class("mt-8 cursor-pointer rounded-full border-2 border-primary-600 px-6 py-2.5 text-base font-semibold text-primary-700 transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-primary-600 hover:text-white focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary-600 motion-reduce:hover:translate-y-0"),
						Text("Log out"),
					),
				)),
			),
		),
	)
}
