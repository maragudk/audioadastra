package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

// serverDescriber describes the PDS that hosts an account.
type serverDescriber interface {
	DescribeServer(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (model.ServerDescription, error)
}

// GetBlobUploadLimit wires [Fat.GetBlobUploadLimit] to the given server describer.
func GetBlobUploadLimit(f *Fat, servers serverDescriber) {
	if f.getBlobUploadLimit != nil {
		panic("service: GetBlobUploadLimit already wired")
	}
	if servers == nil {
		panic("service: GetBlobUploadLimit needs a server describer")
	}

	f.getBlobUploadLimit = func(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (int64, error) {
		description, err := servers.DescribeServer(ctx, did, sessionID)
		if err != nil {
			return 0, err
		}
		return description.BlobUploadLimit, nil
	}
}

// GetBlobUploadLimit of the PDS that hosts the account, in bytes, as the account on the device with the
// given OAuth session; 0 when the PDS does not say.
//
// Errors are [model.ErrorOAuthSessionNotFound] and [model.ErrorPDSUnavailable], when the limit is not
// known.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) GetBlobUploadLimit(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (int64, error) {
	if f.getBlobUploadLimit == nil {
		panic("service: GetBlobUploadLimit not wired; call service.GetBlobUploadLimit or service.Setup")
	}

	return f.getBlobUploadLimit(ctx, did, sessionID)
}

// trackSaver is the store tracks are saved in.
type trackSaver interface {
	SaveTrack(ctx context.Context, t model.Track) (bool, error)
}

// blobRecordCreator uploads blobs to and creates records in an account's repository, as the account on
// the device with the given OAuth session.
type blobRecordCreator interface {
	UploadBlob(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, content io.ReaderAt, size int64, mimeType string) (model.Blob, error)
	CreateRecord(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, record map[string]any) (model.RecordRef, error)
}

// audioProber finds out whether a file is audio, and what kind.
type audioProber interface {
	Probe(ctx context.Context, path string) (model.AudioInfo, error)
}

// UploadTrack wires [Fat.UploadTrack] to the given store, PDS client, audio prober and record validator.
func UploadTrack(f *Fat, db trackSaver, pds blobRecordCreator, prober audioProber, validator recordValidator) {
	if f.uploadTrack != nil {
		panic("service: UploadTrack already wired")
	}
	if db == nil || pds == nil || prober == nil || validator == nil {
		panic("service: UploadTrack needs a store, a PDS client, an audio prober and a record validator")
	}

	f.uploadTrack = func(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, upload model.TrackUpload) (track model.Track, saved bool, err error) {
		span := trace.SpanFromContext(ctx)
		step := ""
		defer func() {
			if err != nil {
				span.SetAttributes(attribute.String("upload.failed_step", step))
			}
		}()
		span.SetAttributes(attribute.String("atproto.did", did.String()))

		step = "validate"
		upload, err = upload.Normalized()
		if err != nil {
			return model.Track{}, false, err
		}

		file, err := os.Open(upload.AudioPath)
		if err != nil {
			return model.Track{}, false, fmt.Errorf("opening upload: %w", err)
		}
		defer func() { _ = file.Close() }()
		stat, err := file.Stat()
		if err != nil {
			return model.Track{}, false, fmt.Errorf("reading upload size: %w", err)
		}
		span.SetAttributes(attribute.Int64("upload.size_bytes", stat.Size()))
		if stat.Size() == 0 {
			return model.Track{}, false, model.ErrorAudioMissing
		}

		step = "probe"
		start := time.Now()
		info, err := prober.Probe(ctx, upload.AudioPath)
		span.SetAttributes(attribute.Float64("upload.probe_duration_ms", float64(time.Since(start))/float64(time.Millisecond)))
		if err != nil {
			return model.Track{}, false, err
		}
		span.SetAttributes(
			attribute.String("audio.format", info.Format),
			attribute.String("audio.codec", info.Codec),
			attribute.Float64("audio.duration_s", info.Duration.Seconds()),
			attribute.Int("audio.channels", info.Channels),
			attribute.Int("audio.sample_rate_hz", info.SampleRate),
			attribute.String("upload.declared_mime_type", info.MIMEType),
		)

		step = "upload_blob"
		start = time.Now()
		blob, err := pds.UploadBlob(ctx, did, sessionID, file, stat.Size(), info.MIMEType)
		span.SetAttributes(attribute.Float64("upload.upload_blob_duration_ms", float64(time.Since(start))/float64(time.Millisecond)))
		if err != nil {
			return model.Track{}, false, err
		}
		span.SetAttributes(attribute.String("atproto.blob_cid", blob.CID.String()), attribute.String("atproto.blob_mime_type", blob.MIMEType))

		// The record carries the blob as the PDS describes it, whose MIME type may be the PDS's own reading
		// of the bytes. A blob left unreferenced after a failure below is the PDS's to discard.
		step = "create_record"
		created := model.Time{T: time.Now()}
		record := map[string]any{
			"$type":     model.CollectionTrack.String(),
			"audio":     map[string]any{"original": blob},
			"title":     upload.Title,
			"createdAt": created.String(),
		}
		if upload.Description != "" {
			record["description"] = upload.Description
		}
		if err := validator.ValidateRecord(record, model.CollectionTrack); err != nil {
			return model.Track{}, false, fmt.Errorf("validating track record: %w", err)
		}
		ref, err := pds.CreateRecord(ctx, did, sessionID, model.CollectionTrack, record)
		if err != nil {
			return model.Track{}, false, err
		}
		span.SetAttributes(attribute.String("atproto.uri", ref.URI.String()), attribute.String("atproto.cid", ref.CID.String()))

		track = model.Track{
			URI:         ref.URI,
			DID:         did,
			RecordKey:   ref.RecordKey,
			CID:         ref.CID,
			Title:       upload.Title,
			Description: upload.Description,
			Created:     created,
			Audio:       blob,
		}

		// The track is published from here on, whatever happens to the app's own copy, which can be made
		// again from the account's repository.
		if _, err := db.SaveTrack(context.WithoutCancel(ctx), track); err != nil {
			span.SetAttributes(attribute.String("upload.save_error", err.Error()))
			span.RecordError(fmt.Errorf("saving published track: %w", err))
			return track, false, nil
		}
		return track, true, nil
	}
}

// UploadTrack for the account, as the account on the device with the given OAuth session: the audio
// file must read as audio, is uploaded to the account's PDS as a blob declared with the MIME type of
// its format, and published there in a com.audioadastra.track record with the title and description,
// trimmed. The track is then saved in the app's own store, which it reports; a failure to save it is
// recorded on the span in the context and is not an error, since the track is published all the same.
// The audio file is the caller's to delete.
//
// The steps and what they found land on the span in the context: the file's size, the audio's format,
// codec, duration, channels and sample rate, the declared and stored MIME types, the record's URI, how
// long probing and uploading took, and upload.failed_step for the step that failed.
//
// Errors are [model.ErrorTrackTextInvalid], [model.ErrorTrackTitleMissing],
// [model.ErrorTrackTitleTooLong], [model.ErrorTrackDescriptionTooLong], [model.ErrorAudioMissing] for
// an empty file, [model.ErrorNotAudio], [model.ErrorOAuthSessionNotFound], [model.ErrorPDSAuthFailed],
// [model.ErrorScopeDenied] for a session not granted the track collection,
// the errors of uploading a blob ([model.ErrorBlobTooLarge], [model.ErrorBlobTypeRefused],
// [model.ErrorBlobRejected], [model.ErrorPDSUnavailable]) and [model.ErrorRecordWriteFailed].
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) UploadTrack(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, upload model.TrackUpload) (model.Track, bool, error) {
	if f.uploadTrack == nil {
		panic("service: UploadTrack not wired; call service.UploadTrack or service.Setup")
	}

	return f.uploadTrack(ctx, did, sessionID, upload)
}

// trackGetter is the store tracks are read from.
type trackGetter interface {
	GetTracksByDID(ctx context.Context, did model.DID) ([]model.Track, error)
}

// GetTracks wires [Fat.GetTracks] to the given store.
func GetTracks(f *Fat, db trackGetter) {
	if f.getTracks != nil {
		panic("service: GetTracks already wired")
	}
	if db == nil {
		panic("service: GetTracks needs a store")
	}

	f.getTracks = db.GetTracksByDID
}

// GetTracks by the account with the given DID, newest first.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) GetTracks(ctx context.Context, did model.DID) ([]model.Track, error) {
	if f.getTracks == nil {
		panic("service: GetTracks not wired; call service.GetTracks or service.Setup")
	}

	return f.getTracks(ctx, did)
}
