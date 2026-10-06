package html

import (
	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type LoginPageProps struct {
	PageProps
	// Identifier, a handle or a DID, as the user typed it, to re-render after an error.
	Identifier string
	// Redirect is the local path to send the user to after login, if any.
	Redirect string
	// Error to show above the form, if any.
	Error string
}

func LoginPage(props LoginPageProps) Node {
	return Page(props.PageProps,
		Div(Class("flex grow flex-col justify-center py-6 sm:py-12"),
			sheet(
				H1(Class("text-center font-display text-4xl/tight font-bold text-gray-950"), Text("Log in")),
				P(Class("mt-2 text-center text-sm/6 text-balance text-gray-600"), Text("Use your atproto account, like the one you use for Bluesky.")),

				If(props.Error != "",
					Div(Class("mt-8 rounded-xl bg-red-50 p-4 text-sm/6 text-red-700"), Role("alert"),
						Text(props.Error),
					),
				),

				Form(Action("/login"), Method("post"), Class("mt-8 space-y-6"),
					If(props.Redirect != "", Input(Type("hidden"), Name("redirect"), Value(props.Redirect))),

					Div(Class("relative"),
						Label(For("handle"), Class("absolute -top-2 left-4 inline-block rounded-lg bg-white px-1 text-xs font-medium text-gray-900"), Text("Handle")),
						Div(Class("flex items-center rounded-full bg-white pl-4 outline-1 -outline-offset-1 outline-gray-300 focus-within:outline-2 focus-within:-outline-offset-2 focus-within:outline-primary-600"),
							Div(Class("shrink-0 text-base text-gray-500 select-none"), Aria("hidden", "true"), Text("@")),
							Input(ID("handle"), Type("text"), Name("handle"), Required(), AutoComplete("username"), Placeholder("you.bsky.social"), Value(props.Identifier),
								Class("block min-w-0 grow bg-transparent py-3 pr-4 pl-1 text-base text-gray-900 placeholder:text-gray-400 focus:outline-none"),
							),
						),
					),

					Button(Type("submit"),
						Class("group flex w-full cursor-pointer items-center justify-center gap-2.5 rounded-full bg-primary-600 px-6 py-3 text-lg font-bold text-white transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-primary-700 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary-600 motion-reduce:hover:translate-y-0"),
						sparkleIcon(),
						Text("Log in"),
					),
				),
			),
		),
	)
}
