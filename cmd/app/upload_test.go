package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"maragu.dev/is"

	"app/atprototest"
	"app/audiotest"
	"app/model"
)

// TestUpload in a real Chrome against the local atproto network from the repository's docker compose
// file, with the app started in-process. It is skipped in short mode.
func TestUpload(t *testing.T) {
	t.Run("should publish a track to the PDS and list it at the top of the profile", func(t *testing.T) {
		network := atprototest.LocalNetwork(t)
		account := network.CreateAccount(t)
		app := startApp(t, network)
		b := newBrowser(t)
		b.logIn(t, app, account)

		var first, notice string
		b.run(t, "upload",
			chromedp.Navigate(app.baseURL+"/profile"),
			chromedp.WaitVisible(`a[href="/upload"]`),
			chromedp.Click(`a[href="/upload"]`),
			waitLoaded(`#audio`),
			chromedp.SetUploadFiles(`#audio`, []string{audiotest.WriteFile(t, "tone.flac", audiotest.FLAC)}),
			chromedp.SendKeys(`#title`, "Sounds of Earth"),
			chromedp.SendKeys(`#description`, "Whale song."),
			chromedp.Click(`form#upload button[type="submit"]`),
			// The upload script sends the user on to the profile once the track is published.
			waitLoaded(`#tracks li`),
			chromedp.Text(`#tracks li:first-child p`, &first),
			chromedp.Text(`#notice`, &notice),
		)
		is.Equal(t, "Sounds of Earth", first)
		is.True(t, strings.Contains(notice, "is published"), notice)

		tracks, err := app.db.GetTracksByDID(t.Context(), account.DID)
		is.NotError(t, err)
		is.Equal(t, 1, len(tracks))
		track := tracks[0]

		record, ok := network.GetRecord(t, account.DID, model.CollectionTrack, track.RecordKey)
		is.True(t, ok, "no track record on the PDS")
		is.Equal(t, any(model.CollectionTrack.String()), record["$type"])
		is.Equal(t, any("Sounds of Earth"), record["title"])
		is.Equal(t, any("Whale song."), record["description"])
		original := record["audio"].(map[string]any)["original"].(map[string]any)
		is.Equal(t, any(track.Audio.CID.String()), original["ref"].(map[string]any)["$link"])
		t.Logf("the PDS labelled the FLAC as %v", original["mimeType"])
		is.True(t, strings.HasPrefix(track.Audio.MIMEType, "audio/"), track.Audio.MIMEType)

		content, _ := network.GetBlob(t, account.DID, track.Audio.CID)
		is.Equal(t, len(audiotest.FLAC), len(content))
		assertNoUploads(t, app)
	})

	t.Run("should refuse a file that is not audio, keeping the title", func(t *testing.T) {
		network := atprototest.LocalNetwork(t)
		account := network.CreateAccount(t)
		app := startApp(t, network)
		b := newBrowser(t)
		b.logIn(t, app, account)

		var message, title string
		b.run(t, "upload a text file",
			chromedp.Navigate(app.baseURL+"/upload"),
			chromedp.WaitVisible(`#audio`),
			chromedp.SetUploadFiles(`#audio`, []string{audiotest.WriteFile(t, "notes.mp3", []byte("Not a song, just some notes.\n"))}),
			chromedp.SendKeys(`#title`, "Sounds of Earth"),
			chromedp.Click(`form#upload button[type="submit"]`),
			chromedp.WaitVisible(`#upload-error`),
			chromedp.Text(`#upload-error`, &message),
			chromedp.Value(`#title`, &title),
		)
		is.True(t, strings.Contains(message, "doesn't look like audio"), message)
		is.Equal(t, "Sounds of Earth", title)

		tracks, err := app.db.GetTracksByDID(t.Context(), account.DID)
		is.NotError(t, err)
		is.Equal(t, 0, len(tracks))
		assertNoUploads(t, app)
	})
}

// assertNoUploads left in the app's upload directory.
func assertNoUploads(t *testing.T, app *testApp) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(app.tempDataDir, "uploads"))
	is.NotError(t, err)
	is.Equal(t, 0, len(entries), "uploads left behind")
}
