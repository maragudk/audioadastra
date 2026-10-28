package html_test

import (
	"strings"
	"testing"

	. "maragu.dev/gomponents"
	"maragu.dev/is"

	"app/html"
)

func TestUploadPage(t *testing.T) {
	t.Run("renders a multipart form for the upload script, without an error", func(t *testing.T) {
		page := renderNode(t, html.UploadPage(html.UploadPageProps{MaxSize: 300 * 1024 * 1024}))

		is.True(t, strings.Contains(page, `<form id="upload" action="/upload" method="post" enctype="multipart/form-data"`), "no form")
		is.True(t, strings.Contains(page, `data-upload=""`), "not marked for the upload script")
		is.True(t, strings.Contains(page, `data-max-size="314572800"`), "no max size")
		is.True(t, strings.Contains(page, `data-too-big="This file is too big. Files can be up to 300 MB here."`), "no too-big message")
		is.True(t, strings.Contains(page, `name="audio" required`), "no required file field")
		is.True(t, strings.Contains(page, `name="title" required`), "no required title field")
		is.True(t, strings.Contains(page, `name="description"`), "no description field")
		is.True(t, strings.Contains(page, `<div id="upload-error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm/6 text-red-700" hidden`), "error alert not hidden")
		is.True(t, !strings.Contains(page, "Your server takes"), "limit shown when unknown")
	})

	t.Run("shows the PDS limit, and blocks files over it", func(t *testing.T) {
		page := renderNode(t, html.UploadPage(html.UploadPageProps{BlobUploadLimit: 100 * 1024 * 1024, MaxSize: 100 * 1024 * 1024}))

		is.True(t, strings.Contains(page, "Your server takes files up to 100 MB."), "no limit")
		is.True(t, strings.Contains(page, `data-max-size="104857600"`), "no max size")
		is.True(t, strings.Contains(page, `data-too-big="This file is too big. Your server takes files up to 100 MB."`), "no too-big message")
	})

	t.Run("shows the app's own limit when the PDS's is higher", func(t *testing.T) {
		page := renderNode(t, html.UploadPage(html.UploadPageProps{BlobUploadLimit: 1024 * 1024 * 1024, MaxSize: 300 * 1024 * 1024}))

		is.True(t, strings.Contains(page, "Files can be up to 300 MB here."), "no limit")
		is.True(t, !strings.Contains(page, "Your server takes"), "PDS limit shown")
		is.True(t, strings.Contains(page, `data-too-big="This file is too big. Files can be up to 300 MB here."`), "no too-big message")
	})

	t.Run("keeps the title and description, and shows the error", func(t *testing.T) {
		page := renderNode(t, html.UploadPage(html.UploadPageProps{MaxSize: 1024, Title: `Sounds of "Earth"`, Description: "Whale <song>.", Error: "This file isn't audio."}))

		is.True(t, strings.Contains(page, `value="Sounds of &#34;Earth&#34;"`), "title not kept")
		is.True(t, strings.Contains(page, `>Whale &lt;song&gt;.</textarea>`), "description not kept")
		is.True(t, strings.Contains(page, `role="alert" class="rounded-xl bg-red-50 p-4 text-sm/6 text-red-700" data-attr="{hidden: !$uploadError}"`), "error alert hidden")
		is.True(t, strings.Contains(page, `>This file isn&#39;t audio.</div>`), "no error")
	})
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 512, want: "512 bytes"},
		{bytes: 5 * 1024 * 1024, want: "5 MB"},
		{bytes: 1536 * 1024 * 1024, want: "1.5 GB"},
		{bytes: 300 * 1024 * 1024, want: "300 MB"},
		{bytes: 100_000_000, want: "95.4 MB"},
	}
	for _, test := range tests {
		t.Run("should format "+test.want, func(t *testing.T) {
			is.Equal(t, test.want, html.FormatSize(test.bytes))
		})
	}
}

func renderNode(t *testing.T, n Node) string {
	t.Helper()

	var b strings.Builder
	is.NotError(t, n.Render(&b))
	return b.String()
}
