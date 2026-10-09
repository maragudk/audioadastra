package html

import (
	"fmt"
	"strings"

	. "maragu.dev/gomponents"
	data "maragu.dev/gomponents-datastar"
	. "maragu.dev/gomponents/html"
)

type UploadPageProps struct {
	PageProps
	// BlobUploadLimit of the user's PDS in bytes, or 0 when it is not known.
	BlobUploadLimit int64
	// MaxSize of a file the browser lets the user send, in bytes: the smaller of the app's own limit and
	// the PDS's.
	MaxSize int64
	// Title and Description as the user typed them, to re-render after an error.
	Title       string
	Description string
	// Error to show above the form, if any.
	Error string
}

// UploadPage with the form to publish a track. The form posts as multipart form data, and works without
// scripts. With them, app.js sends it with upload progress, and reports progress and outcome as events
// on the form, which Datastar renders: a progress bar, then "Publishing…", or an error in the alert.
func UploadPage(props UploadPageProps) Node {
	// The limit that binds: the user's server's, or the app's own when that is lower.
	limit := "Files can be up to " + FormatSize(props.MaxSize) + " here."
	if props.BlobUploadLimit > 0 && props.BlobUploadLimit <= props.MaxSize {
		limit = "Your server takes files up to " + FormatSize(props.BlobUploadLimit) + "."
	}

	return Page(props.PageProps,
		Div(Class("flex grow flex-col justify-center py-6 sm:py-12"),
			sheet(
				H1(Class("text-center font-display text-4xl/tight font-bold text-gray-950"), Text("Upload a track")),
				P(Class("mt-2 text-center text-sm/6 text-balance text-gray-600"),
					Text("An audio file, such as WAV, FLAC or MP3."),
					If(props.BlobUploadLimit > 0, Text(" "+limit)),
				),

				Form(ID("upload"), Action("/upload"), Method("post"), EncType("multipart/form-data"), Class("mt-8 space-y-6"),
					Data("upload", ""),
					Data("max-size", fmt.Sprint(props.MaxSize)),
					Data("too-big", "This file is too big. "+limit),
					data.Signals(map[string]any{"uploading": false, "publishing": false, "percent": 0, "uploadError": props.Error}),
					data.On("upload-progress", "$uploading = true; $percent = evt.detail.percent; $uploadError = ''"),
					data.On("upload-publishing", "$publishing = true; $percent = 100"),
					data.On("upload-failed", "$uploading = false; $publishing = false; $percent = 0; $uploadError = evt.detail.error"),

					// Rendered from the server's error without scripts, and kept in sync with the signal with them.
					Div(ID("upload-error"), Role("alert"), Class("rounded-xl bg-red-50 p-4 text-sm/6 text-red-700"),
						If(props.Error == "", Attr("hidden")),
						data.Attr("hidden", "!$uploadError"),
						data.Text("$uploadError"),
						Text(props.Error),
					),

					// Any audio type, and the extensions of the formats the app reads, for systems that do not
					// type every one of them as audio.
					field("audio", "Audio file",
						Input(ID("audio"), Type("file"), Name("audio"), Required(), Accept("audio/*,.aac,.aif,.aiff,.caf,.flac,.m4a,.mka,.mp3,.oga,.ogg,.opus,.wav,.webm,.wv"),
							Class("block w-full cursor-pointer py-2 pr-4 pl-2 text-sm text-gray-600 file:mr-4 file:cursor-pointer file:rounded-full file:border-0 file:bg-primary-50 file:px-4 file:py-2 file:text-sm file:font-semibold file:text-primary-700 hover:file:bg-primary-100 focus:outline-none"),
						),
					),

					field("title", "Title",
						Input(ID("title"), Type("text"), Name("title"), Required(), AutoComplete("off"), Value(props.Title),
							Class("block w-full bg-transparent px-4 py-3 text-base text-gray-900 placeholder:text-gray-400 focus:outline-none"),
						),
					),

					Div(Class("relative"),
						fieldLabel("description", "Description (optional)"),
						Textarea(ID("description"), Name("description"), Rows("4"),
							Class("block w-full rounded-3xl bg-white px-4 py-3 text-base text-gray-900 outline-1 -outline-offset-1 outline-gray-300 placeholder:text-gray-400 focus:outline-2 focus:-outline-offset-2 focus:outline-primary-600"),
							Text(props.Description),
						),
					),

					// The single-attribute forms (data-attr:name, data-class:name) are written out where the name
					// has a hyphen, since the helpers render an object whose keys would need quoting.
					Div(ID("upload-progress"), Class("space-y-2"), Attr("hidden"), data.Attr("hidden", "!$uploading"),
						Div(Class("flex justify-between text-sm/6 font-medium text-gray-900"),
							Span(data.Text("$publishing ? 'Publishing…' : 'Uploading…'"), Text("Uploading…")),
							Span(data.Attr("hidden", "$publishing"), data.Text("$percent + '%'"), Text("0%")),
						),
						Div(Class("h-2 overflow-hidden rounded-full bg-pink-100"), Role("progressbar"), Aria("label", "Upload progress"),
							Aria("valuemin", "0"), Aria("valuemax", "100"), Aria("valuenow", "0"), Attr("data-attr:aria-valuenow", "$percent"),
							Div(Class("h-full w-0 rounded-full bg-primary-600 transition-[width] duration-200 ease-out motion-reduce:animate-none motion-reduce:transition-none"),
								data.Style("width", "$percent + '%'"),
								Attr("data-class:animate-pulse", "$publishing"),
							),
						),
					),

					Button(Type("submit"), data.Attr("disabled", "$uploading"),
						Class("group flex w-full cursor-pointer items-center justify-center gap-2.5 rounded-full bg-primary-600 px-6 py-3 text-lg font-bold text-white transition duration-200 ease-out hover:-translate-y-0.5 hover:bg-primary-700 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-primary-600 disabled:cursor-wait disabled:opacity-60 disabled:hover:translate-y-0 motion-reduce:hover:translate-y-0"),
						sparkleIcon(),
						Text("Publish"),
					),
				),
			),
		),
	)
}

// field is a text field with a label overlapping its top border, as on the login page.
func field(id, label string, input Node) Node {
	return Div(Class("relative"),
		fieldLabel(id, label),
		Div(Class("flex items-center rounded-full bg-white outline-1 -outline-offset-1 outline-gray-300 focus-within:outline-2 focus-within:-outline-offset-2 focus-within:outline-primary-600"),
			input,
		),
	)
}

func fieldLabel(id, label string) Node {
	return Label(For(id), Class("absolute -top-2 left-4 inline-block rounded-lg bg-white px-1 text-xs font-medium text-gray-900"), Text(label))
}

// FormatSize in bytes for people, in binary units as size limits are usually set, with at most one
// decimal: "5 MB", "1.5 GB".
func FormatSize(bytes int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	size := float64(bytes)
	unit := 0
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprint(bytes, " bytes")
	}
	s := strings.TrimSuffix(fmt.Sprintf("%.1f", size), ".0")
	return s + " " + units[unit]
}
