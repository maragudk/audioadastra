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
}

// ProfilePage of the logged-in user: the handle, and the way out.
func ProfilePage(props ProfilePageProps) Node {
	return Page(props.PageProps,
		Div(Class("flex grow flex-col justify-center py-6 sm:py-12"),
			sheet(Class("text-center"),
				H1(Class("text-3xl/tight font-bold tracking-tight break-words text-gray-950"), Text("@"+props.Handle.String())),
				Form(Action("/logout"), Method("post"),
					Button(ID("logout"), Type("submit"),
						Class("mt-8 cursor-pointer rounded-full border-2 border-primary-600 px-6 py-2.5 text-base font-semibold text-primary-700 transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-primary-600 hover:text-white focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary-600 motion-reduce:hover:translate-y-0"),
						Text("Log out"),
					),
				),
			),
		),
	)
}
