package html

import (
	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type LoginPageProps struct {
	PageProps
	// Handle as the user typed it, to re-render after an error.
	Handle string
	// Redirect is the local path to send the user to after login, if any.
	Redirect string
	// Error to show above the form, if any.
	Error string
}

func LoginPage(props LoginPageProps) Node {
	return Page(props.PageProps,
		Div(Class("flex flex-col justify-center px-6 py-12 lg:px-8"),
			Div(Class("sm:mx-auto sm:w-full sm:max-w-sm"),
				H1(Class("text-center text-2xl/9 font-bold tracking-tight text-gray-900 dark:text-white"), Text("Log in")),
				P(Class("mt-2 text-center text-sm/6 text-gray-500 dark:text-gray-400"), Text("Use your atproto account, like the one you use for Bluesky.")),
			),

			Div(Class("mt-10 sm:mx-auto sm:w-full sm:max-w-sm"),
				If(props.Error != "",
					Div(Class("mb-6 rounded-md bg-red-50 p-4 text-sm/6 text-red-700 dark:bg-red-500/10 dark:text-red-400"), Role("alert"),
						Text(props.Error),
					),
				),

				Form(Action("/login"), Method("post"), Class("space-y-6"),
					If(props.Redirect != "", Input(Type("hidden"), Name("redirect"), Value(props.Redirect))),

					Div(
						Label(For("handle"), Class("block text-sm/6 font-medium text-gray-900 dark:text-gray-100"), Text("Handle")),
						Div(Class("mt-2"),
							Input(ID("handle"), Type("text"), Name("handle"), Required(), AutoComplete("username"), Placeholder("you.bsky.social"), Value(props.Handle),
								Class("block w-full rounded-md bg-white px-3 py-1.5 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-primary-600 sm:text-sm/6 dark:bg-white/5 dark:text-white dark:outline-white/10 dark:placeholder:text-gray-500 dark:focus:outline-primary-500"),
							),
						),
					),

					Div(
						Button(Type("submit"),
							Class("flex w-full justify-center rounded-md bg-primary-600 px-3 py-1.5 text-sm/6 font-semibold text-white shadow-xs hover:bg-primary-500 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-600 dark:bg-primary-500 dark:shadow-none dark:hover:bg-primary-400 dark:focus-visible:outline-primary-500 cursor-pointer"),
							Text("Log in"),
						),
					),
				),
			),
		),
	)
}
