package html

import (
	. "maragu.dev/gomponents"
	. "maragu.dev/gomponents/html"
)

type HomePageProps struct {
	PageProps
}

// HomePage is the night sky: the name as a constellation that plays as music, a few words about
// what Audio Ad Astra is, and an invitation to follow along on Bluesky.
func HomePage(props HomePageProps) Node {
	hashPaths()

	return frontPage(props.PageProps,
		container(false,
			Div(Class("flex flex-col gap-8 pt-8 pb-16 sm:gap-10 md:pt-10 md:pb-20"),
				H1(Class("w-full"),
					Span(Class("sr-only"), Text("Audio Ad Astra")),
					skyConstellation("wide", []string{"AUDIO", "AD ASTRA"}, "hidden sm:block w-full max-h-[48dvh]"),
					skyConstellation("narrow", []string{"AUDIO", "AD", "ASTRA"}, "sm:hidden w-full"),
				),

				Div(Class("flex max-w-4xl flex-col gap-6"),
					P(Data("sky-avoid", ""), Class("font-display text-4xl/tight font-bold text-white text-balance sm:text-5xl/tight"),
						Text("Listen to the stars of the open sky."),
					),
					P(Data("sky-avoid", ""), Class("max-w-3xl text-[1.1875rem]/7 font-bold text-white text-pretty sm:text-2xl/10 sm:font-normal sm:text-pink-50"),
						Text("Share your music on the open social web. Your tracks stay in your own account, any app can play them, and no algorithm picks who hears them. It's not ready yet, so follow along while it gets built."),
					),
					Div(Data("sky-avoid", ""), Class("flex flex-wrap items-center gap-4"),
						A(Href("https://bsky.app/profile/audioadastra.com"),
							Class("inline-flex items-center gap-3 rounded-full bg-white px-7 py-3.5 text-xl font-bold text-pink-700 transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-pink-700 hover:text-white hover:ring-2 hover:ring-white hover:ring-inset motion-reduce:hover:translate-y-0 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"),
							blueskyIcon(),
							Text("Follow on Bluesky"),
						),
						Button(Type("button"), ID("sky-play"), Hidden("hidden"),
							Class("group inline-flex cursor-pointer items-center gap-3 rounded-full border-2 border-white/70 px-6 py-3 text-xl font-semibold text-white transition duration-200 ease-out hover:-translate-y-0.5 hover:border-white hover:bg-white hover:text-pink-700 motion-reduce:hover:translate-y-0 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white data-playing:border-violet-950 data-playing:bg-violet-950 data-playing:text-white data-playing:hover:border-violet-900 data-playing:hover:bg-violet-900 data-playing:hover:text-white"),
							playIcon(),
							Span(Data("label", ""), Text("Play the sky")),
						),
					),
				),
			),
		),
	)
}

// sparklePath is the four-point sparkle of the sky, as a 24 by 24 icon.
const sparklePath = "M12 1.5 Q13.6 10.4 22.5 12 Q13.6 13.6 12 22.5 Q10.4 13.6 1.5 12 Q10.4 10.4 12 1.5Z"

// sparkleIcon turns a quarter when its button is hovered.
func sparkleIcon() Node {
	return SVG(Attr("viewBox", "0 0 24 24"), Class("h-5 w-5 fill-current transition-transform duration-300 ease-out group-hover:rotate-90 motion-reduce:transition-none"), Aria("hidden", "true"),
		El("path", Attr("d", sparklePath)),
	)
}

// playIcon is a sparkle that turns into a stop square while the sky plays.
func playIcon() Node {
	return SVG(Attr("viewBox", "0 0 24 24"), Class("h-6 w-6 fill-current transition-transform duration-300 ease-out group-hover:rotate-90 motion-reduce:transition-none"), Aria("hidden", "true"),
		El("path", Data("icon", "play"), Attr("d", sparklePath)),
		El("rect", Data("icon", "stop"), Class("hidden"), Attr("x", "6"), Attr("y", "6"), Attr("width", "12"), Attr("height", "12"), Attr("rx", "2")),
	)
}
