package html

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"maragu.dev/glue/html"
	. "maragu.dev/gomponents"
	data "maragu.dev/gomponents-datastar"
	. "maragu.dev/gomponents/components"
	. "maragu.dev/gomponents/html"
)

var hashOnce sync.Once
var appCSSPath, appJSPath string
var datastarJSPath string

func Page(props PageProps, body ...Node) Node {
	hashOnce.Do(func() {
		appCSSPath = getHashedPath("public/styles/app.css")
		appJSPath = getHashedPath("public/scripts/app.js")

		datastarJSPath = getHashedPath("public/scripts/datastar.js")
	})

	title := "Audio Ad Astra"
	if props.Title != "" {
		title = props.Title + " " + title
	}

	return HTML5(HTML5Props{
		Title:       title,
		Description: props.Description,
		Language:    "en",
		Head: Group{
			Link(Rel("stylesheet"), Href(appCSSPath)),
			Script(Type("module"), Src(datastarJSPath), Defer()),
			Script(Src(appJSPath), Defer()),
			Script(Src("https://cdn.usefathom.com/script.js"), Data("site", "RDRSRWDR"), Defer()),
			html.FavIcons("Audio Ad Astra"),
		},
		HTMLAttrs: Group{Class("scheme-light dark:scheme-dark")},
		Body: Group{Class("bg-primary-600 text-gray-900 dark:text-white"),
			Div(Class("min-h-dvh flex flex-col justify-between"),
				header(props),
				Div(Class("grow bg-white dark:bg-gray-800 h-auto"),
					container(true,
						Group(body),
					),
				),
				Div(Class("bg-white dark:bg-gray-800"),
					footer(),
				),
			),
		},
	})
}

func header(_ PageProps) Node {
	return Div(
		container(false,
			Div(Class("flex items-center py-1"),
				A(Href("/"), Title("Front page"),
					Img(Src("/images/logo.png"), Alt("Audio Ad Astra"), Class("h-8 w-auto")),
				),
			),
		),
	)
}

func footer() Node {
	return Div(
		container(false,
			Div(Class("flex items-center justify-center space-x-4 sm:space-x-8 py-2 text-gray-500 dark:text-gray-400"),
				data.Init("console.log('Datastar loaded')"),
				A(Href("https://bsky.app/profile/audioadastra.com"), Title("Bluesky"), Rel("me"),
					Class("hover:text-gray-900 dark:hover:text-white"),
					Span(Class("sr-only"), Text("Bluesky")),
					blueskyIcon(),
				),
				A(Href("https://github.com/maragudk/audioadastra"), Title("GitHub"),
					Class("hover:text-gray-900 dark:hover:text-white"),
					Span(Class("sr-only"), Text("GitHub")),
					githubIcon(),
				),
			),
		),
	)
}

func blueskyIcon() Node {
	return SVG(
		Attr("viewBox", "0 0 600 530"),
		Attr("fill", "currentColor"),
		Class("h-6 w-6"),
		Aria("hidden", "true"),
		El("path", Attr("d", "m135.72 44.03c66.496 49.921 138.02 151.14 164.28 205.46 26.262-54.316 97.782-155.54 164.28-205.46 47.98-36.021 125.72-63.892 125.72 24.795 0 17.712-10.155 148.79-16.111 170.07-20.703 73.984-96.144 92.854-163.25 81.433 117.3 19.964 147.14 86.092 82.697 152.22-122.39 125.59-175.91-31.511-189.63-71.766-2.514-7.3797-3.6904-10.832-3.7077-7.8964-0.0174-2.9357-1.1937 0.51669-3.7077 7.8964-13.714 40.255-67.233 197.36-189.63 71.766-64.444-66.128-34.605-132.26 82.697-152.22-67.108 11.421-142.55-7.4491-163.25-81.433-5.9562-21.282-16.111-152.36-16.111-170.07 0-88.687 77.742-60.816 125.72-24.795z")),
	)
}

func githubIcon() Node {
	return SVG(
		Attr("viewBox", "0 0 24 24"),
		Attr("fill", "currentColor"),
		Class("h-6 w-6"),
		Aria("hidden", "true"),
		El("path",
			Attr("fill-rule", "evenodd"),
			Attr("clip-rule", "evenodd"),
			Attr("d", "M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0112 6.844c.85.004 1.705.115 2.504.337 1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"),
		),
	)
}

func container(padY bool, children ...Node) Node {
	return Div(
		Classes{
			"max-w-7xl mx-auto h-full": true,
			"px-4 sm:px-6 lg:px-8":     true,
			"py-4 md:py-8":             padY,
		},
		Group(children),
	)
}

func getHashedPath(path string) string {
	externalPath := strings.TrimPrefix(path, "public")
	ext := filepath.Ext(path)
	if ext == "" {
		panic("no extension found")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%v.x%v", strings.TrimSuffix(externalPath, ext), ext)
	}

	return fmt.Sprintf("%v.%x%v", strings.TrimSuffix(externalPath, ext), sha256.Sum256(data), ext)
}
