package http_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	nethttp "net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"hegel.dev/go/hegel"
	"maragu.dev/is"

	"app/atprototest"
	"app/audiotest"
	"app/model"
)

func TestUpload(t *testing.T) {
	t.Run("should send a logged-out user to the login page", func(t *testing.T) {
		s := newServer(t)

		res, _ := s.get(t, "/upload")
		is.Equal(t, "/login", res.Request.URL.Path)
		is.Equal(t, "/upload", res.Request.URL.Query().Get("redirect"))
	})

	t.Run("should show the form with the PDS's limit", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		s.net.BlobUploadLimit = 512 * 1024

		res, body := s.get(t, "/upload")
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.True(t, strings.Contains(body, `enctype="multipart/form-data"`), "no form")
		is.True(t, strings.Contains(body, "Your server takes files up to 512 KB."), "no limit")
		is.True(t, strings.Contains(body, `data-max-size="524288"`), "no max size")
	})

	t.Run("should show the form without a limit when the PDS does not describe itself", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		s.net.DescribeServerFails = true

		res, body := s.get(t, "/upload")
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.True(t, !strings.Contains(body, "Your server takes"), "limit shown")
		is.True(t, strings.Contains(body, `data-max-size="1048576"`), "no max size")
		is.True(t, s.hasSpanAttributeKey("upload.pds_limit_error"), "not recorded on the span")
	})

	t.Run("should publish the track and list it at the top of the profile, with a notice shown once", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		_, _ = s.upload(t, false, uploadForm{title: "An older track", file: audiotest.FLAC})

		res, body := s.upload(t, false, uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.Equal(t, "/profile", res.Request.URL.Path)
		is.True(t, strings.Contains(body, "“Sounds of Earth” is published."), "no notice")
		is.True(t, strings.Index(body, ">Sounds of Earth<") < strings.Index(body, ">An older track<"), "not at the top")

		records := s.net.Records(atprototest.AliceDID, model.CollectionTrack)
		is.Equal(t, 2, len(records))
		s.assertNoUploads(t)
		is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", "published")), "no outcome on the span")
		is.True(t, s.hasSpanAttributeKey("upload.temp_write_duration_ms"), "no temp write duration on the span")

		_, body = s.get(t, "/profile")
		is.True(t, !strings.Contains(body, "is published."), "notice shown twice")
	})

	t.Run("should answer the upload script with where to go", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.upload(t, true, uploadForm{title: "Sounds of Earth", file: audiotest.FLAC, fieldsFirst: true})
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.Equal(t, "application/json", res.Header.Get("Content-Type"))
		is.Equal(t, `{"redirect":"/profile"}`, body)
		is.Equal(t, 1, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
		s.assertNoUploads(t)
	})

	t.Run("should tell the user the track is published when it could not be saved here", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		is.NotError(t, s.db.H.Exec(t.Context(), `create trigger refuse_tracks before insert on tracks begin select raise(abort, 'refused'); end`))

		res, body := s.upload(t, false, uploadForm{title: "Sounds of Earth", file: audiotest.FLAC})
		is.Equal(t, "/profile", res.Request.URL.Path)
		is.True(t, strings.Contains(body, "“Sounds of Earth” is published. It can take a little while to show up here."), "no notice")
		is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", "published_unsaved")), "no outcome on the span")
		s.assertNoUploads(t)
	})

	t.Run("should put a record key of its own in every upload page", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		_, first := s.get(t, "/upload")
		_, second := s.get(t, "/upload")
		key := formRecordKey(t, first)
		_, err := syntax.ParseTID(key)
		is.NotError(t, err)
		is.True(t, key != formRecordKey(t, second), "same key twice")
	})

	t.Run("should publish one track when the form is sent again after the PDS's answer was lost", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		s.net.PutRecordLosesAnswer = true
		form := uploadForm{title: "Sounds of Earth", file: audiotest.FLAC}

		res, body := s.upload(t, false, form)
		is.Equal(t, nethttp.StatusBadGateway, res.StatusCode)
		form.rkey = formRecordKey(t, body)

		s.net.PutRecordLosesAnswer = false
		res, body = s.upload(t, false, form)
		is.Equal(t, "/profile", res.Request.URL.Path)
		is.True(t, strings.Contains(body, "“Sounds of Earth” is published."), "no notice")
		is.Equal(t, 1, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
		is.Equal(t, 1, strings.Count(body, ">Sounds of Earth<"))
	})

	t.Run("should show the page again with a key to send it with after a refusal before the form was read", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.upload(t, false, uploadForm{title: "Sounds of Earth", file: make([]byte, 2*1024*1024)})
		is.Equal(t, nethttp.StatusRequestEntityTooLarge, res.StatusCode)
		_, err := syntax.ParseTID(formRecordKey(t, body))
		is.NotError(t, err)
	})

	t.Run("should refuse another track sent from a page one was published from, with a fresh page", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		_, page := s.get(t, "/upload")
		rkey := formRecordKey(t, page)
		_, _ = s.upload(t, false, uploadForm{title: "Sounds of Earth", file: audiotest.FLAC, rkey: rkey})

		res, body := s.upload(t, false, uploadForm{title: "Sounds of Mars", file: audiotest.FLAC, rkey: rkey})
		is.Equal(t, nethttp.StatusConflict, res.StatusCode)
		is.True(t, strings.Contains(body, "Another track was already published from this page. Reload the page and try again."), "no message")
		is.True(t, formRecordKey(t, body) != rkey, "same key again")
		is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", "record_conflict")), "no outcome on the span")
		records := s.net.Records(atprototest.AliceDID, model.CollectionTrack)
		is.Equal(t, 1, len(records))
		is.Equal(t, any("Sounds of Earth"), records[model.RecordKey(rkey)]["title"])
	})

	t.Run("should tell the user a PDS answered unexpectedly, and that the track is not published", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		s.net.PutRecordCID = "not a CID"

		res, body := s.upload(t, true, uploadForm{title: "Sounds of Earth", file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusBadGateway, res.StatusCode)
		is.Equal(t, `{"error":"Your server answered in a way we didn't expect, so the track wasn't published."}`, body)
		is.Equal(t, 0, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
	})

	tests := []struct {
		name    string
		setup   func(s *server)
		form    uploadForm
		code    int
		message string
		outcome string
		blobs   int
	}{
		{
			name: "should refuse a file that is not audio",
			form: uploadForm{title: "Sounds of Earth", description: "Whale song.", file: []byte("Not a song.\n")},
			code: nethttp.StatusBadRequest, message: "This file doesn&#39;t look like audio we can read. Please export it as WAV, FLAC or MP3.", outcome: "not_audio",
		},
		{
			name: "should refuse an upload without a title",
			form: uploadForm{description: "Whale song.", file: audiotest.FLAC},
			code: nethttp.StatusBadRequest, message: "Give your track a title.", outcome: "invalid",
		},
		{
			name: "should refuse an upload without a file",
			form: uploadForm{title: "Sounds of Earth", description: "Whale song.", noFile: true},
			code: nethttp.StatusBadRequest, message: "Choose an audio file to upload.", outcome: "invalid",
		},
		{
			name: "should refuse a file over the app's limit as it arrives",
			form: uploadForm{title: "Sounds of Earth", description: "Whale song.", file: make([]byte, 1024*1024+1), chunked: true},
			code: nethttp.StatusRequestEntityTooLarge, message: "This file is too big. Files can be up to 1 MB here.", outcome: "too_large",
		},
		{
			name:  "should refuse a file over the PDS's limit before sending it there",
			setup: func(s *server) { s.net.BlobUploadLimit = 10 * 1024 },
			form:  uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC},
			code:  nethttp.StatusRequestEntityTooLarge, message: "This file is too big. Your server takes files up to 10 KB.", outcome: "too_large_for_pds",
		},
		{
			name:  "should explain a PDS that does not take the file as audio",
			setup: func(s *server) { s.net.UploadBlobScopeMissing = true },
			form:  uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC},
			code:  nethttp.StatusBadRequest, message: "Your server doesn&#39;t recognise this file as audio. Please export it as WAV, FLAC or MP3.", outcome: "pds_refused_type", blobs: 1,
		},
		{
			name:  "should report a PDS that fails",
			setup: func(s *server) { s.net.UploadBlobFails = true },
			form:  uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC},
			code:  nethttp.StatusBadGateway, message: "We couldn&#39;t reach your server. Try again in a little while.", outcome: "pds_unavailable", blobs: 1,
		},
		{
			name:  "should report a record the PDS does not create, after the blob went up",
			setup: func(s *server) { s.net.PutRecordFails = true },
			form:  uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC},
			code:  nethttp.StatusBadGateway, message: "Your server couldn&#39;t publish the track. Try again in a little while.", outcome: "record_write_failed", blobs: 1,
		},
		{
			name: "should ask for a reload when the record key is not a TID",
			form: uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC, rkey: "self"},
			code: nethttp.StatusBadRequest, message: "This upload page has expired. Reload the page and try again.", outcome: "invalid_key",
		},
		{
			name: "should ask for a reload when the record key is from a page more than a day old",
			form: uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC, rkey: syntax.NewTIDFromTime(time.Now().Add(-25*time.Hour), 0).String()},
			code: nethttp.StatusBadRequest, message: "This upload page has expired. Reload the page and try again.", outcome: "invalid_key",
		},
		{
			name:  "should ask a user whose login predates the track scope to log in again",
			setup: func(s *server) { s.net.PutRecordScopeMissing = true },
			form:  uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC},
			code:  nethttp.StatusForbidden, message: "Your login doesn&#39;t allow publishing tracks yet. Log out, log in again to allow it, and try again.", outcome: "scope_denied", blobs: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := newServer(t)
			s.login(t)
			if test.setup != nil {
				test.setup(s)
			}

			res, body := s.upload(t, false, test.form)
			is.Equal(t, test.code, res.StatusCode)
			is.Equal(t, "/upload", res.Request.URL.Path)
			is.True(t, strings.Contains(body, test.message), "no message: "+test.message)
			if test.form.title != "" {
				is.True(t, strings.Contains(body, `value="`+test.form.title+`"`), "title not kept")
			}
			is.True(t, strings.Contains(body, ">Whale song.</textarea>"), "description not kept")
			is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", test.outcome)), "no outcome on the span")
			is.Equal(t, test.blobs, s.net.UploadBlobCalls())
			is.Equal(t, 0, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
			s.assertNoUploads(t)

			// The upload script gets the same message as JSON.
			test.form.chunked = false
			res, body = s.upload(t, true, test.form)
			is.Equal(t, test.code, res.StatusCode)
			var out struct {
				Error string `json:"error"`
			}
			is.NotError(t, json.Unmarshal([]byte(body), &out))
			is.Equal(t, strings.ReplaceAll(test.message, "&#39;", "'"), out.Error)
			s.assertNoUploads(t)
		})
	}

	t.Run("should publish when uploading and publishing take longer than the server's timeouts", func(t *testing.T) {
		s := newServer(t, func(c serverConfig) {
			c.http.ReadTimeout = 100 * time.Millisecond
			c.http.WriteTimeout = 100 * time.Millisecond
			c.upload.IdleTimeout = 100 * time.Millisecond
		})
		s.login(t)
		s.net.UploadBlobDelay = 300 * time.Millisecond

		res, body := s.upload(t, true, uploadForm{title: "Sounds of Earth", file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.Equal(t, `{"redirect":"/profile"}`, body)
		is.Equal(t, 1, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
	})

	t.Run("should refuse a title over the limit, however much of it was sent", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.upload(t, true, uploadForm{title: strings.Repeat("a", 4000), file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, "The title is too long. Keep it to 300 characters."), body)
		is.Equal(t, 0, s.net.UploadBlobCalls())
		s.assertNoUploads(t)
	})

	t.Run("should refuse a title over the byte limit that is short once trimmed, rather than publish a cut-off title", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.upload(t, true, uploadForm{title: strings.Repeat(" ", 2998) + "Hello", file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, "The title is too long. Keep it to 300 characters."), body)
		is.Equal(t, 0, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
	})

	t.Run("should refuse a description over the byte limit that is short once trimmed, rather than publish a cut-off description", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.upload(t, true, uploadForm{title: "Sounds of Earth", description: strings.Repeat("\n", 19998) + "Whale song.", file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, "The description is too long. Keep it to 5000 characters."), body)
		is.Equal(t, 0, len(s.net.Records(atprototest.AliceDID, model.CollectionTrack)))
	})

	t.Run("should publish a title and description just as sent, trimmed, or refuse them, but never cut them short", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		whitespace := hegel.Composite(func(tc hegel.TestCase) string {
			return strings.Repeat(hegel.Draw(tc, hegel.SampledFrom([]string{" ", "\n", "\t", "\u3000"})), hegel.Draw(tc, hegel.Integers(0, 7000)))
		})
		text := func(maxSize int) hegel.Generator[string] {
			return hegel.Composite(func(tc hegel.TestCase) string {
				return hegel.Draw(tc, whitespace) + hegel.Draw(tc, hegel.Text().MaxSize(maxSize)) + hegel.Draw(tc, whitespace)
			})
		}

		hegel.Test(t, func(ht *hegel.T) {
			title := hegel.Draw(ht, text(20))
			description := hegel.Draw(ht, text(20))
			before := len(s.net.Records(atprototest.AliceDID, model.CollectionTrack))

			res, _ := s.upload(t, true, uploadForm{title: title, description: description, file: audiotest.FLAC})
			records := s.net.Records(atprototest.AliceDID, model.CollectionTrack)
			if res.StatusCode != nethttp.StatusOK {
				is.Equal(ht, before, len(records))
				return
			}
			want, err := model.TrackUpload{Title: title, Description: description}.Normalized()
			is.NotError(ht, err)
			is.True(ht, len(title) <= model.TrackTitleMaxBytes && len(description) <= model.TrackDescriptionMaxBytes, "published fields over the limits as sent")
			is.Equal(ht, before+1, len(records))
			found := false
			for _, record := range records {
				if record["title"] == want.Title && (record["description"] == want.Description || (want.Description == "" && record["description"] == nil)) {
					found = true
				}
			}
			is.True(ht, found, "no record with the title and description as sent, trimmed")
		}, hegel.WithTestCases(50))
	})

	t.Run("should ask the user to log in again when the PDS refuses the session", func(t *testing.T) {
		s := newServer(t)
		s.login(t)
		s.net.ForgetTokens()

		res, body := s.upload(t, true, uploadForm{title: "Sounds of Earth", file: audiotest.FLAC})
		is.Equal(t, nethttp.StatusUnauthorized, res.StatusCode)
		is.True(t, strings.Contains(body, "log in again"), body)
		is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", "pds_auth_failed")), "no outcome on the span")
		s.assertNoUploads(t)
	})

	t.Run("should refuse a request that is not a multipart form", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.postForm(t, "/upload", url.Values{"title": {"Sounds of Earth"}})
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, "The upload was interrupted. Try again."), "no message")
		is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", "receive_failed")), "no outcome on the span")
	})

	t.Run("should give up on a client that stops sending, and delete what it sent", func(t *testing.T) {
		s := newServer(t, func(c serverConfig) { c.upload.IdleTimeout = 100 * time.Millisecond })
		s.login(t)

		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		part, err := w.CreateFormFile("audio", "track.flac")
		is.NotError(t, err)
		_, err = part.Write(audiotest.FLAC)
		is.NotError(t, err)
		// The form is never finished: the pipe stays open after the file's first bytes.
		pr, pw := io.Pipe()
		t.Cleanup(func() { _ = pw.Close() })
		go func() { _, _ = pw.Write(b.Bytes()) }()

		req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPost, "https://app.test/upload", pr)
		is.NotError(t, err)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req.Header.Set("Accept", "application/json")
		// The server takes a connection that stops sending for one that is gone.
		res, err := s.http.Do(req)
		is.NotError(t, err)
		_ = readBody(t, res)
		is.Equal(t, 499, res.StatusCode)
		is.True(t, s.hasSpanAttribute(attribute.String("upload.outcome", "client_gone")), "no outcome on the span")
		s.assertNoUploads(t)
	})

	t.Run("should refuse a request declared over the limit without reading it", func(t *testing.T) {
		s := newServer(t)
		s.login(t)

		res, body := s.upload(t, true, uploadForm{title: "Sounds of Earth", file: make([]byte, 2*1024*1024)})
		is.Equal(t, nethttp.StatusRequestEntityTooLarge, res.StatusCode)
		is.True(t, strings.Contains(body, "Files can be up to 1 MB here."), body)
		is.True(t, !s.hasSpanAttributeKey("upload.temp_write_duration_ms"), "read the file")
		s.assertNoUploads(t)
	})
}

// uploadForm to post to the upload page.
type uploadForm struct {
	title       string
	description string
	file        []byte
	noFile      bool
	// fieldsFirst puts the title and description before the file, which otherwise comes first, as in
	// the page.
	fieldsFirst bool
	// chunked sends the body without a declared length.
	chunked bool
	// rkey to send, or the one in a freshly rendered upload page when empty.
	rkey string
}

// login as alice through the fake network.
func (s *server) login(t *testing.T) {
	t.Helper()

	_, body := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
	is.True(t, strings.Contains(body, `href="/profile"`), "not logged in")
}

// upload the form, as the upload script does when json is true, and as a plain form post otherwise.
func (s *server) upload(t *testing.T, json bool, form uploadForm) (*nethttp.Response, string) {
	t.Helper()

	if form.rkey == "" {
		_, page := s.get(t, "/upload")
		form.rkey = formRecordKey(t, page)
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	// The key comes first, as in the page.
	is.NotError(t, w.WriteField("rkey", form.rkey))
	fields := func() {
		is.NotError(t, w.WriteField("title", form.title))
		is.NotError(t, w.WriteField("description", form.description))
	}
	if form.fieldsFirst {
		fields()
	}
	if !form.noFile {
		part, err := w.CreateFormFile("audio", "track.flac")
		is.NotError(t, err)
		_, err = part.Write(form.file)
		is.NotError(t, err)
	}
	if !form.fieldsFirst {
		fields()
	}
	is.NotError(t, w.Close())

	var body io.Reader = &b
	if form.chunked {
		body = io.MultiReader(&b)
	}
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPost, "https://app.test/upload", body)
	is.NotError(t, err)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if json {
		req.Header.Set("Accept", "application/json")
	}
	res, err := s.http.Do(req)
	is.NotError(t, err)
	return res, readBody(t, res)
}

// formRecordKey in the upload page.
func formRecordKey(t *testing.T, page string) string {
	t.Helper()

	match := regexp.MustCompile(`name="rkey" value="([^"]*)"`).FindStringSubmatch(page)
	is.True(t, match != nil, "no record key in the page")
	return match[1]
}

// assertNoUploads left in the upload directory.
func (s *server) assertNoUploads(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(s.uploads)
	is.NotError(t, err)
	is.Equal(t, 0, len(entries), "uploads left behind")
}
