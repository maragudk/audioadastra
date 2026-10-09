package service_test

import (
	"context"
	"net/url"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"maragu.dev/glue/oteltest"
	"maragu.dev/is"

	"app/atproto"
	"app/atprototest"
	"app/audiotest"
	"app/ffprobe"
	"app/lexicons"
	"app/model"
	"app/service"
	"app/sqlite"
	"app/sqlitetest"
)

func TestFat_UploadTrack(t *testing.T) {
	t.Run("should publish the audio and record on the PDS, save the track, and describe it all on the span", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)

		ctx, span := otel.Tracer("test").Start(t.Context(), "request")
		track, saved, err := h.fat.UploadTrack(ctx, did, sessionID, model.TrackUpload{
			Title:       " Sounds of Earth ",
			Description: "Whale song.",
			AudioPath:   audiotest.WriteFile(t, "tone.flac", audiotest.FLAC),
		})
		span.End()
		is.NotError(t, err)
		is.True(t, saved, "not saved")
		is.Equal(t, "Sounds of Earth", track.Title)
		is.Equal(t, model.Blob{CID: track.Audio.CID, MIMEType: "audio/flac", Size: int64(len(audiotest.FLAC))}, track.Audio)

		blob, ok := h.net.GetBlob(track.Audio.CID)
		is.True(t, ok, "no blob on the PDS")
		is.Equal(t, "audio/flac", blob.DeclaredMIMEType)

		record, ok := h.net.GetRecord(did, model.CollectionTrack, track.RecordKey)
		is.True(t, ok, "no record on the PDS")
		is.Equal(t, any(model.CollectionTrack.String()), record["$type"])
		is.Equal(t, any("Sounds of Earth"), record["title"])
		is.Equal(t, any("Whale song."), record["description"])
		is.Equal(t, any(track.Created.String()), record["createdAt"])
		original := record["audio"].(map[string]any)["original"].(map[string]any)
		is.Equal(t, any(track.Audio.CID.String()), original["ref"].(map[string]any)["$link"])

		tracks, err := h.db.GetTracksByDID(t.Context(), did)
		is.NotError(t, err)
		is.Equal(t, 1, len(tracks))
		is.Equal(t, track.URI, tracks[0].URI)
		is.Equal(t, track.CID, tracks[0].CID)

		attrs := h.spanAttributes(t, "request")
		for _, attr := range []attribute.KeyValue{
			attribute.String("atproto.did", did.String()),
			attribute.Int64("upload.size_bytes", int64(len(audiotest.FLAC))),
			attribute.String("audio.format", "flac"),
			attribute.String("audio.codec", "flac"),
			attribute.Float64("audio.duration_s", 2),
			attribute.Int("audio.channels", 1),
			attribute.Int("audio.sample_rate_hz", 8000),
			attribute.String("upload.declared_mime_type", "audio/flac"),
			attribute.String("atproto.blob_mime_type", "audio/flac"),
			attribute.String("atproto.uri", track.URI.String()),
		} {
			is.True(t, oteltest.HasAttribute(attrs, attr), string(attr.Key))
		}
		is.True(t, oteltest.HasAttributeKey(attrs, "upload.probe_duration_ms"), "no probe duration")
		is.True(t, !oteltest.HasAttributeKey(attrs, "upload.failed_step"), "failed step on success")
	})

	t.Run("should leave the description out of the record when there is none", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)

		track, _, err := h.fat.UploadTrack(t.Context(), did, sessionID, model.TrackUpload{Title: "t", Description: "  ", AudioPath: audiotest.WriteFile(t, "tone.flac", audiotest.FLAC)})
		is.NotError(t, err)

		record, _ := h.net.GetRecord(did, model.CollectionTrack, track.RecordKey)
		_, ok := record["description"]
		is.True(t, !ok, "description in the record")
	})

	t.Run("should publish the blob as the PDS labelled it", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)
		h.net.DetectedMIMEType = "audio/x-flac"

		track, _, err := h.fat.UploadTrack(t.Context(), did, sessionID, model.TrackUpload{Title: "t", AudioPath: audiotest.WriteFile(t, "tone.flac", audiotest.FLAC)})
		is.NotError(t, err)
		is.Equal(t, "audio/x-flac", track.Audio.MIMEType)

		record, _ := h.net.GetRecord(did, model.CollectionTrack, track.RecordKey)
		is.Equal(t, any("audio/x-flac"), record["audio"].(map[string]any)["original"].(map[string]any)["mimeType"])
	})

	tests := []struct {
		name    string
		upload  func(t *testing.T) model.TrackUpload
		setup   func(n *atprototest.Network)
		err     error
		step    string
		blobs   int
		records int
	}{
		{
			name: "should refuse a missing title before touching the PDS",
			upload: func(t *testing.T) model.TrackUpload {
				return model.TrackUpload{AudioPath: audiotest.WriteFile(t, "a.flac", audiotest.FLAC)}
			},
			err: model.ErrorTrackTitleMissing, step: "validate",
		},
		{
			name: "should refuse an empty file before touching the PDS",
			upload: func(t *testing.T) model.TrackUpload {
				return model.TrackUpload{Title: "t", AudioPath: audiotest.WriteFile(t, "a.flac", nil)}
			},
			err: model.ErrorAudioMissing, step: "validate",
		},
		{
			name: "should refuse a file that is not audio before touching the PDS",
			upload: func(t *testing.T) model.TrackUpload {
				return model.TrackUpload{Title: "t", AudioPath: audiotest.WriteFile(t, "a.txt", []byte("Not a song.\n"))}
			},
			err: model.ErrorNotAudio, step: "probe",
		},
		{
			name:   "should pass on a blob type the PDS refuses, and create no record",
			upload: flacUpload,
			setup:  func(n *atprototest.Network) { n.UploadBlobScopeMissing = true },
			err:    model.ErrorBlobTypeRefused, step: "upload_blob", blobs: 1,
		},
		{
			name:   "should pass on a blob over the PDS's limit, and create no record",
			upload: flacUpload,
			setup:  func(n *atprototest.Network) { n.BlobUploadLimit = 100 },
			err:    model.ErrorBlobTooLarge, step: "upload_blob", blobs: 1,
		},
		{
			name:   "should pass on a failed record write after the blob went up, doing nothing more",
			upload: flacUpload,
			setup:  func(n *atprototest.Network) { n.CreateRecordFails = true },
			err:    model.ErrorRecordWriteFailed, step: "create_record", blobs: 1, records: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newUploadHarness(t)
			did, sessionID := h.login(t)
			if test.setup != nil {
				test.setup(h.net)
			}

			ctx, span := otel.Tracer("test").Start(t.Context(), "request")
			_, saved, err := h.fat.UploadTrack(ctx, did, sessionID, test.upload(t))
			span.End()
			is.Error(t, test.err, err)
			is.True(t, !saved, "saved")
			is.Equal(t, test.blobs, h.net.UploadBlobCalls())
			is.Equal(t, test.records, h.net.CreateRecordCalls())
			is.Equal(t, 0, len(h.net.Records(did, model.CollectionTrack)))
			is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.String("upload.failed_step", test.step)), "no failed step")

			tracks, err := h.db.GetTracksByDID(t.Context(), did)
			is.NotError(t, err)
			is.Equal(t, 0, len(tracks))
		})
	}

	t.Run("should report the track published when saving it fails, recording why on the span", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)
		is.NotError(t, h.db.H.Exec(t.Context(), `create trigger refuse_tracks before insert on tracks begin select raise(abort, 'refused'); end`))

		ctx, span := otel.Tracer("test").Start(t.Context(), "request")
		track, saved, err := h.fat.UploadTrack(ctx, did, sessionID, flacUpload(t))
		span.End()
		is.NotError(t, err)
		is.True(t, !saved, "saved")
		is.True(t, track.URI != "", "no track")
		is.Equal(t, 1, len(h.net.Records(did, model.CollectionTrack)))
		is.True(t, oteltest.HasAttributeKey(h.spanAttributes(t, "request"), "upload.save_error"), "no save error on the span")
	})

	t.Run("should save the track when the client goes away after the record is created", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)
		ctx, cancel := context.WithCancel(t.Context())
		h.fat = service.NewFat()
		service.UploadTrack(h.fat, cancellingSaver{db: h.db, cancel: cancel}, h.client, h.prober, h.catalog)

		_, saved, err := h.fat.UploadTrack(ctx, did, sessionID, flacUpload(t))
		is.NotError(t, err)
		is.True(t, saved, "not saved")
	})
}

func TestFat_GetBlobUploadLimit(t *testing.T) {
	t.Run("should get the limit of the account's PDS", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)
		h.net.BlobUploadLimit = 1234

		limit, err := h.fat.GetBlobUploadLimit(t.Context(), did, sessionID)
		is.NotError(t, err)
		is.Equal(t, int64(1234), limit)
	})

	t.Run("should pass on a PDS that fails", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)
		h.net.DescribeServerFails = true

		_, err := h.fat.GetBlobUploadLimit(t.Context(), did, sessionID)
		is.Error(t, model.ErrorPDSUnavailable, err)
	})
}

func TestFat_GetTracks(t *testing.T) {
	t.Run("should get the account's tracks", func(t *testing.T) {
		h := newUploadHarness(t)
		did, sessionID := h.login(t)
		track, _, err := h.fat.UploadTrack(t.Context(), did, sessionID, flacUpload(t))
		is.NotError(t, err)

		tracks, err := h.fat.GetTracks(t.Context(), did)
		is.NotError(t, err)
		is.Equal(t, 1, len(tracks))
		is.Equal(t, track.URI, tracks[0].URI)
	})
}

// uploadHarness is the upload operations wired to the fake network, a real ffprobe, the lexicon catalog
// and a database, with a span recorder in place before the client is made.
type uploadHarness struct {
	sr      *tracetest.SpanRecorder
	net     *atprototest.Network
	db      *sqlite.Database
	client  *atproto.Client
	prober  *ffprobe.Prober
	catalog *lexicons.Catalog
	fat     *service.Fat
}

func newUploadHarness(t *testing.T) *uploadHarness {
	t.Helper()

	h := &uploadHarness{
		sr:  oteltest.NewSpanRecorder(t),
		net: atprototest.NewNetwork(t),
		db:  sqlitetest.NewDatabase(t),
	}
	h.net.AddAccount(atprototest.AliceDID, "alice.test")
	h.client = h.net.NewClient(t, h.db)

	var err error
	h.prober, err = ffprobe.NewProber()
	if err != nil {
		t.Fatalf("ffprobe is required, from ffmpeg: %v", err)
	}
	h.catalog, err = lexicons.NewCatalog()
	is.NotError(t, err)

	h.fat = service.NewFat()
	service.UploadTrack(h.fat, h.db, h.client, h.prober, h.catalog)
	service.GetBlobUploadLimit(h.fat, h.client)
	service.GetTracks(h.fat, h.db)
	return h
}

// login all the way as alice through the fake network, and return the DID and session ID.
func (h *uploadHarness) login(t *testing.T) (model.DID, model.OAuthSessionID) {
	t.Helper()

	flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
	is.NotError(t, err)
	query := h.net.Authorize(t, flow.RedirectURL)
	session, err := h.client.ProcessCallback(t.Context(), callbackOf(query), flow.State)
	is.NotError(t, err)
	return session.DID, session.SessionID
}

func (h *uploadHarness) spanAttributes(t *testing.T, name string) []attribute.KeyValue {
	t.Helper()

	for _, span := range h.sr.Ended() {
		if span.Name() == name {
			return span.Attributes()
		}
	}
	t.Fatal("no span " + name)
	return nil
}

func flacUpload(t *testing.T) model.TrackUpload {
	t.Helper()

	return model.TrackUpload{Title: "Sounds of Earth", AudioPath: audiotest.WriteFile(t, "tone.flac", audiotest.FLAC)}
}

func callbackOf(query url.Values) model.OAuthCallback {
	return model.OAuthCallback{State: model.OAuthState(query.Get("state")), Code: query.Get("code"), Issuer: query.Get("iss")}
}

// cancellingSaver cancels the request's context as the track is saved, as a client going away right
// after the record was created would.
type cancellingSaver struct {
	db     *sqlite.Database
	cancel context.CancelFunc
}

func (s cancellingSaver) SaveTrack(ctx context.Context, t model.Track) (bool, error) {
	s.cancel()
	return s.db.SaveTrack(ctx, t)
}
