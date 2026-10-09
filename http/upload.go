package http

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	gluehttp "maragu.dev/glue/http"
	. "maragu.dev/gomponents"

	"app/html"
	"app/model"
)

type trackUploader interface {
	GetBlobUploadLimit(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (int64, error)
	UploadTrack(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, upload model.TrackUpload) (model.Track, bool, error)
}

type noticePutter interface {
	Put(ctx context.Context, key string, val any)
}

// sessionNoticeKey is the cookie session key of a notice for the profile page, shown once.
const sessionNoticeKey = "profileNotice"

// UploadOptions for [Upload].
type UploadOptions struct {
	// Dir that uploads are written to on their way to the PDS. It must exist.
	Dir string
	// MaxSize of an uploaded file in bytes: the app's own limit, whatever the PDS's is.
	MaxSize int64
	// IdleTimeout is how long receiving an upload may go without progress before it fails, 30 seconds
	// when zero.
	IdleTimeout time.Duration
}

// errReceiveFailed when the upload's request body cannot be read to the end, or is not a form.
var errReceiveFailed = errors.New("receiving upload failed")

// Upload page and the upload of a track. The handlers expect [GetUserFromContext] to return a user, so
// the routes must be behind middleware that requires one.
//
// The upload is a multipart form, streamed to a file in the upload directory and deleted once the
// track is published or has failed. The request is refused early when its declared length is over the
// size limit, the smaller of the app's own and the PDS's, and cut off when its body goes over. A client
// still sending a body refused early may see the connection closed rather than the refusal, which is
// why browsers check the size before they send. While the body is read, the server's read deadline
// only applies to each read, so a large upload has no deadline as a whole but fails when it stalls.
// Once it is read, there is no read deadline until the response is written, since the server ends a
// request whose connection times out on reading, and publishing may take long; the write deadline is
// set again before the response is written too.
//
// A request that accepts JSON is answered with {"redirect": "/profile"} or {"error": "..."}; any other with a redirect to the profile page, or the upload page with the error
// and the title and description kept. What happened lands on the span in the context as
// upload.outcome, with upload.failed_step for where it failed.
func Upload(r *Router, svc trackUploader, sm noticePutter, opts UploadOptions) {
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = 30 * time.Second
	}

	r.Get("/upload", func(props html.PageProps) (Node, error) {
		user := GetUserFromContext(props.Ctx)
		limit := blobUploadLimit(props.Ctx, svc, user.DID, GetOAuthSessionIDFromContext(props.Ctx))

		return html.UploadPage(html.UploadPageProps{
			PageProps:       withTitle(props, "Upload a track"),
			BlobUploadLimit: limit,
			MaxSize:         maxUploadSize(opts.MaxSize, limit),
		}), nil
	})

	r.Post("/upload", func(props html.PageProps) (Node, error) {
		ctx := props.Ctx
		span := trace.SpanFromContext(ctx)
		user := GetUserFromContext(ctx)
		sessionID := GetOAuthSessionIDFromContext(ctx)

		rc := http.NewResponseController(props.W)

		limit := blobUploadLimit(ctx, svc, user.DID, sessionID)
		maxSize := maxUploadSize(opts.MaxSize, limit)
		span.SetAttributes(attribute.Int64("upload.max_size_bytes", maxSize))

		received, err := receiveUpload(props.W, props.R, rc, opts, maxSize, limit)
		defer func() {
			if received.path != "" {
				_ = os.Remove(received.path)
			}
		}()
		_ = rc.SetReadDeadline(time.Time{})
		if err != nil {
			span.SetAttributes(attribute.String("upload.failed_step", "receive"))
		}

		var track model.Track
		saved := false
		if err == nil {
			track, saved, err = svc.UploadTrack(ctx, user.DID, sessionID, model.TrackUpload{
				Title:       received.title,
				Description: received.description,
				AudioPath:   received.path,
			})
		}

		_ = rc.SetReadDeadline(time.Now().Add(opts.IdleTimeout))
		_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		wantsJSON := strings.Contains(props.R.Header.Get("Accept"), "application/json")

		if err != nil {
			if errors.Is(err, context.Canceled) {
				span.SetAttributes(attribute.String("upload.outcome", "client_gone"))
				return nil, err
			}
			failure := uploadFailureOf(err, maxSize, limit)
			span.SetAttributes(attribute.String("upload.outcome", failure.outcome))
			span.RecordError(err)
			if failure.outcome == "error" {
				span.SetStatus(codes.Error, err.Error())
			}

			if wantsJSON {
				writeJSONStatus(props.W, props.R, failure.code, map[string]string{"error": failure.message})
				return nil, nil
			}
			return html.UploadPage(html.UploadPageProps{
				PageProps:       withTitle(props, "Upload a track"),
				BlobUploadLimit: limit,
				MaxSize:         maxSize,
				Title:           received.title,
				Description:     received.description,
				Error:           failure.message,
			}), gluehttp.Error{Code: failure.code, Err: err}
		}

		notice := "“" + track.Title + "” is published."
		if saved {
			span.SetAttributes(attribute.String("upload.outcome", "published"))
		} else {
			span.SetAttributes(attribute.String("upload.outcome", "published_unsaved"))
			notice += " It can take a little while to show up here."
		}
		sm.Put(ctx, sessionNoticeKey, notice)

		if wantsJSON {
			writeJSONStatus(props.W, props.R, http.StatusOK, map[string]string{"redirect": "/profile"})
			return nil, nil
		}
		http.Redirect(props.W, props.R, "/profile", http.StatusSeeOther)
		return nil, nil
	})
}

// blobUploadLimit of the user's PDS, or 0 when it is not known, in which case why lands on the span in
// the context as upload.pds_limit_error.
func blobUploadLimit(ctx context.Context, svc trackUploader, did model.DID, sessionID model.OAuthSessionID) int64 {
	span := trace.SpanFromContext(ctx)
	limit, err := svc.GetBlobUploadLimit(ctx, did, sessionID)
	if err != nil {
		span.SetAttributes(attribute.String("upload.pds_limit_error", err.Error()))
		return 0
	}
	if limit > 0 {
		span.SetAttributes(attribute.Int64("upload.pds_limit_bytes", limit))
	}
	return limit
}

// maxUploadSize is the smaller of the app's own limit and the PDS's, when that is known.
func maxUploadSize(appMax, pdsLimit int64) int64 {
	if pdsLimit > 0 && pdsLimit < appMax {
		return pdsLimit
	}
	return appMax
}

// receivedUpload is the form of an upload, with the file on disk at path, if it got that far.
type receivedUpload struct {
	title       string
	description string
	path        string
}

// receiveUpload of the multipart form in the request: the title and description fields, read up to
// one byte over their limits so that going over is caught, and the first file in the audio field,
// written to a new file in the directory. The file is returned whenever it was created, even with an
// error, so the caller can delete it. How long writing it took and its size land on the span in the
// request's context.
//
// Errors are [model.ErrorUploadTooLarge] or [model.ErrorBlobTooLarge], depending on whose limit the
// size is over, [model.ErrorAudioMissing] when there is no file or an empty one, and
// [errReceiveFailed] when the body is not a form or cannot be read.
func receiveUpload(w http.ResponseWriter, r *http.Request, rc *http.ResponseController, opts UploadOptions, maxSize, pdsLimit int64) (receivedUpload, error) {
	var u receivedUpload
	tooLarge := model.ErrorUploadTooLarge
	if maxSize == pdsLimit {
		tooLarge = model.ErrorBlobTooLarge
	}
	// The title and description at their longest, and the multipart framing, come on top of the file.
	const formOverhead = 64 * 1024
	if r.ContentLength > maxSize+formOverhead {
		return u, fmt.Errorf("%w: request body is %v bytes", tooLarge, r.ContentLength)
	}

	body := http.MaxBytesReader(w, r.Body, maxSize+formOverhead)
	r.Body = readCloser{Reader: &deadlineReader{r: body, rc: rc, idle: opts.IdleTimeout}, Closer: body}
	mr, err := r.MultipartReader()
	if err != nil {
		return u, fmt.Errorf("%w: %w", errReceiveFailed, err)
	}

	// A file over the limit is not kept, but the rest of the form is read, within the body's limit, so
	// that the title and description after it can be shown again.
	var fileErr error
	span := trace.SpanFromContext(r.Context())
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return u, receiveError(r.Context(), tooLarge, err)
		}

		switch part.FormName() {
		case "title":
			u.title, err = readField(part, model.TrackTitleMaxBytes)
		case "description":
			u.description, err = readField(part, model.TrackDescriptionMaxBytes)
		case "audio":
			if u.path != "" {
				break
			}
			var size int64
			start := time.Now()
			u.path, size, err = writeTempFile(opts.Dir, part, maxSize)
			span.SetAttributes(
				attribute.Float64("upload.temp_write_duration_ms", float64(time.Since(start))/float64(time.Millisecond)),
				attribute.Int64("upload.received_bytes", size),
			)
			if err == nil && size > maxSize {
				fileErr = fmt.Errorf("%w: file is over %v bytes", tooLarge, maxSize)
			}
		}
		_ = part.Close()
		if err != nil {
			return u, receiveError(r.Context(), tooLarge, err)
		}
	}

	if fileErr != nil {
		return u, fileErr
	}
	if u.path == "" {
		return u, model.ErrorAudioMissing
	}
	return u, nil
}

// receiveError classifies an error from reading the request body: a body over the limit is too large,
// a client that went away is a cancellation, and anything else is [errReceiveFailed].
func receiveError(ctx context.Context, tooLarge error, err error) error {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesErr):
		return fmt.Errorf("%w: request body is over %v bytes", tooLarge, maxBytesErr.Limit)
	case ctx.Err() != nil:
		return fmt.Errorf("receiving upload: %w", context.Cause(ctx))
	default:
		return fmt.Errorf("%w: %w", errReceiveFailed, err)
	}
}

// readField of a form, up to one byte over the given number of bytes.
func readField(r io.Reader, maxBytes int) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(maxBytes)+1))
	return string(b), err
}

// writeTempFile in the directory with the content, up to one byte over the given size, and return its
// path and how much was written. The path is returned whenever the file was created.
func writeTempFile(dir string, content io.Reader, maxSize int64) (string, int64, error) {
	f, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("creating upload file: %w", err)
	}
	n, err := io.Copy(f, io.LimitReader(content, maxSize+1))
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("writing upload file: %w", closeErr)
	}
	return f.Name(), n, err
}

// deadlineReader moves the connection's read deadline forward before every read, so a body that keeps
// arriving is never cut off, and one that stops is after the idle time.
type deadlineReader struct {
	r    io.Reader
	rc   *http.ResponseController
	idle time.Duration
}

func (d *deadlineReader) Read(p []byte) (int, error) {
	_ = d.rc.SetReadDeadline(time.Now().Add(d.idle))
	return d.r.Read(p)
}

type readCloser struct {
	io.Reader
	io.Closer
}

// uploadFailure is how a failed upload is reported: the message for the user, the status code, and the
// upload.outcome attribute value.
type uploadFailure struct {
	message string
	code    int
	outcome string
}

// uploadFailureOf the error, with a generic message for anything unknown.
func uploadFailureOf(err error, maxSize, pdsLimit int64) uploadFailure {
	exportHint := " Please export it as WAV, FLAC or MP3."
	failures := []struct {
		err error
		uploadFailure
	}{
		{model.ErrorTrackTitleMissing, uploadFailure{"Give your track a title.", http.StatusBadRequest, "invalid"}},
		{model.ErrorTrackTitleTooLong, uploadFailure{fmt.Sprintf("The title is too long. Keep it to %v characters.", model.TrackTitleMaxGraphemes), http.StatusBadRequest, "invalid"}},
		{model.ErrorTrackDescriptionTooLong, uploadFailure{fmt.Sprintf("The description is too long. Keep it to %v characters.", model.TrackDescriptionMaxGraphemes), http.StatusBadRequest, "invalid"}},
		{model.ErrorTrackTextInvalid, uploadFailure{"The title or description has characters we can't read.", http.StatusBadRequest, "invalid"}},
		{model.ErrorAudioMissing, uploadFailure{"Choose an audio file to upload.", http.StatusBadRequest, "invalid"}},
		{model.ErrorNotAudio, uploadFailure{"This file doesn't look like audio we can read." + exportHint, http.StatusBadRequest, "not_audio"}},
		{model.ErrorUploadTooLarge, uploadFailure{"This file is too big. Files can be up to " + html.FormatSize(maxSize) + " here.", http.StatusRequestEntityTooLarge, "too_large"}},
		{model.ErrorBlobTooLarge, uploadFailure{"This file is too big for your server.", http.StatusRequestEntityTooLarge, "too_large_for_pds"}},
		{model.ErrorBlobTypeRefused, uploadFailure{"Your server doesn't recognise this file as audio." + exportHint, http.StatusBadRequest, "pds_refused_type"}},
		{model.ErrorBlobRejected, uploadFailure{"Your server didn't accept this file. Try again, or export it as WAV, FLAC or MP3.", http.StatusBadGateway, "pds_rejected"}},
		{model.ErrorScopeDenied, uploadFailure{"Your login doesn't allow publishing tracks yet. Log out, log in again to allow it, and try again.", http.StatusForbidden, "scope_denied"}},
		{model.ErrorPDSAuthFailed, uploadFailure{"Your server didn't accept your login. Log out, log in again and try again.", http.StatusUnauthorized, "pds_auth_failed"}},
		{model.ErrorPDSUnavailable, uploadFailure{"We couldn't reach your server. Try again in a little while.", http.StatusBadGateway, "pds_unavailable"}},
		{model.ErrorRecordWriteFailed, uploadFailure{"Your server couldn't publish the track. Try again in a little while.", http.StatusBadGateway, "record_write_failed"}},
		{errReceiveFailed, uploadFailure{"The upload was interrupted. Try again.", http.StatusBadRequest, "receive_failed"}},
	}
	for _, f := range failures {
		if errors.Is(err, f.err) {
			if f.err == model.ErrorBlobTooLarge && pdsLimit > 0 {
				f.message = "This file is too big. Your server takes files up to " + html.FormatSize(pdsLimit) + "."
			}
			return f.uploadFailure
		}
	}
	return uploadFailure{"Something went wrong. Try again in a moment.", http.StatusInternalServerError, "error"}
}

// writeJSONStatus of the value with the status code, recording a failure to write it on the span in
// the request's context.
func writeJSONStatus(w http.ResponseWriter, r *http.Request, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.MarshalWrite(w, v); err != nil {
		trace.SpanFromContext(r.Context()).RecordError(err)
	}
}
