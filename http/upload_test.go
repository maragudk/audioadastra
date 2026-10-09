package http_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	nethttp "net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
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
			setup: func(s *server) { s.net.CreateRecordFails = true },
			form:  uploadForm{title: "Sounds of Earth", description: "Whale song.", file: audiotest.FLAC},
			code:  nethttp.StatusBadGateway, message: "Your server couldn&#39;t publish the track. Try again in a little while.", outcome: "record_write_failed", blobs: 1,
		},
		{
			name:  "should ask a user whose login predates the track scope to log in again",
			setup: func(s *server) { s.net.CreateRecordScopeMissing = true },
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

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
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

// assertNoUploads left in the upload directory.
func (s *server) assertNoUploads(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(s.uploads)
	is.NotError(t, err)
	is.Equal(t, 0, len(entries), "uploads left behind")
}
