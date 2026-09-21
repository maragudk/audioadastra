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
		Div(Class("mx-auto max-w-sm space-y-6 text-center"),
			H1(Class("text-2xl/9 font-bold tracking-tight text-gray-900 dark:text-white"), Text("@"+props.Handle.String())),
			Form(Action("/logout"), Method("post"),
				Button(ID("logout"), Type("submit"),
					Class("rounded-md bg-white px-3 py-2 text-sm font-semibold text-gray-900 shadow-xs inset-ring inset-ring-gray-300 hover:bg-gray-50 dark:bg-white/10 dark:text-white dark:shadow-none dark:inset-ring-white/5 dark:hover:bg-white/20 cursor-pointer"),
					Text("Log out"),
				),
			),
		),
	)
}
