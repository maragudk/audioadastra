package atproto_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"maragu.dev/glue/oteltest"
	"maragu.dev/is"

	"app/atproto"
	"app/atprototest"
	"app/model"
)

func TestClient_DescribeServer(t *testing.T) {
	t.Run("should get the blob upload limit of the account's PDS, and answer again from the cache", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.BlobUploadLimit = 100 * 1024 * 1024

		ctx, span := otel.Tracer("test").Start(t.Context(), "first")
		description, err := h.client.DescribeServer(ctx, did, sessionID)
		span.End()
		is.NotError(t, err)
		is.Equal(t, int64(100*1024*1024), description.BlobUploadLimit)
		is.True(t, h.hasSpan("com.atproto.server.describeServer"), "no child span")
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "first"), attribute.String("atproto.describe_server_cache", "miss")), "no cache miss")
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "first"), attribute.String("atproto.pds_host", "pds.test")), "no PDS host")

		ctx, span = otel.Tracer("test").Start(t.Context(), "second")
		description, err = h.client.DescribeServer(ctx, did, sessionID)
		span.End()
		is.NotError(t, err)
		is.Equal(t, int64(100*1024*1024), description.BlobUploadLimit)
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "second"), attribute.String("atproto.describe_server_cache", "hit")), "no cache hit")
		is.Equal(t, 1, h.net.DescribeServerCalls())
	})

	t.Run("should give no limit when the PDS does not say", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		description, err := h.client.DescribeServer(t.Context(), did, sessionID)
		is.NotError(t, err)
		is.Equal(t, int64(0), description.BlobUploadLimit)
	})

	t.Run("should report a PDS that fails, and not ask it again right away", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.DescribeServerFails = true

		_, err := h.client.DescribeServer(t.Context(), did, sessionID)
		is.Error(t, model.ErrorPDSUnavailable, err)

		h.net.DescribeServerFails = false
		_, err = h.client.DescribeServer(t.Context(), did, sessionID)
		is.Error(t, model.ErrorPDSUnavailable, err)
		is.Equal(t, 1, h.net.DescribeServerCalls())
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.DescribeServer(t.Context(), atprototest.AliceDID, "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_UploadBlob(t *testing.T) {
	t.Run("should upload the content with the declared type, picking up a stale nonce before sending it", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		content := []byte("ID3 pretend this is an MP3")

		h.net.RotatePDSNonce()
		blob, err := h.client.UploadBlob(t.Context(), did, sessionID, bytes.NewReader(content), int64(len(content)), "audio/mpeg")
		is.NotError(t, err)
		is.Equal(t, "audio/mpeg", blob.MIMEType)
		is.Equal(t, int64(len(content)), blob.Size)
		is.True(t, strings.HasPrefix(blob.CID.String(), "bafkrei"), blob.CID.String())

		stored, ok := h.net.GetBlob(blob.CID)
		is.True(t, ok, "no blob on the PDS")
		is.Equal(t, string(content), string(stored.Content))
		is.Equal(t, "audio/mpeg", stored.DeclaredMIMEType)
		is.Equal(t, 1, h.net.UploadBlobCalls())
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "com.atproto.repo.uploadBlob"), attribute.String("atproto.declared_mime_type", "audio/mpeg")), "no declared type on the span")
	})

	t.Run("should send the whole content again when the PDS asks for a retry before reading it", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.UploadBlobRotatesNonce = true
		// Larger than the transport's buffers, so the first attempt is still being written when the PDS
		// answers it.
		content := make([]byte, 4*1024*1024)
		_, err := rand.Read(content)
		is.NotError(t, err)

		blob, err := h.client.UploadBlob(t.Context(), did, sessionID, bytes.NewReader(content), int64(len(content)), "audio/wav")
		is.NotError(t, err)
		is.Equal(t, int64(len(content)), blob.Size)

		stored, ok := h.net.GetBlob(blob.CID)
		is.True(t, ok, "no blob on the PDS")
		is.True(t, bytes.Equal(content, stored.Content), "content changed on the way")
		is.Equal(t, 2, h.net.UploadBlobCalls())
	})

	t.Run("should report a session the PDS refuses", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.ForgetTokens()

		_, err := h.client.UploadBlob(t.Context(), did, sessionID, strings.NewReader("four"), 4, "audio/mpeg")
		is.Error(t, model.ErrorPDSAuthFailed, err)
	})

	t.Run("should return the type the PDS labelled the blob with", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.DetectedMIMEType = "video/webm"

		blob, err := h.client.UploadBlob(t.Context(), did, sessionID, strings.NewReader("webm"), 4, "audio/webm")
		is.NotError(t, err)
		is.Equal(t, "video/webm", blob.MIMEType)
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "com.atproto.repo.uploadBlob"), attribute.String("atproto.blob_mime_type", "video/webm")), "no PDS type on the span")
	})

	tests := []struct {
		name  string
		setup func(n *atprototest.Network)
		err   error
	}{
		{name: "should report a blob over the PDS's limit as too large", setup: func(n *atprototest.Network) { n.BlobUploadLimit = 3 }, err: model.ErrorBlobTooLarge},
		{name: "should report a type outside the granted scopes as refused", setup: func(n *atprototest.Network) { n.UploadBlobScopeMissing = true }, err: model.ErrorBlobTypeRefused},
		{name: "should report a PDS that fails as unavailable", setup: func(n *atprototest.Network) { n.UploadBlobFails = true }, err: model.ErrorPDSUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			did, sessionID := h.login(t)
			test.setup(h.net)

			_, err := h.client.UploadBlob(t.Context(), did, sessionID, strings.NewReader("four"), 4, "audio/mpeg")
			is.Error(t, test.err, err)
		})
	}

	t.Run("should give up on a PDS that stops making progress, with no deadline on the whole upload", func(t *testing.T) {
		h := newHarness(t, func(opts *atproto.NewClientOptions) { opts.UploadIdleTimeout = 50 * time.Millisecond })
		did, sessionID := h.login(t)
		h.net.UploadBlobDelay = time.Minute

		_, err := h.client.UploadBlob(t.Context(), did, sessionID, strings.NewReader("four"), 4, "audio/mpeg")
		is.Error(t, model.ErrorPDSUnavailable, err)
		is.True(t, strings.Contains(err.Error(), "no progress"), err.Error())
	})

	t.Run("should keep the cancellation of a client that went away", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := h.client.UploadBlob(ctx, did, sessionID, strings.NewReader("four"), 4, "audio/mpeg")
		is.Error(t, context.Canceled, err)
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.UploadBlob(t.Context(), atprototest.AliceDID, "nope", strings.NewReader("four"), 4, "audio/mpeg")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_CreateRecord(t *testing.T) {
	t.Run("should create a record with a key the PDS picks, and return where it is", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		record := map[string]any{
			"$type":     model.CollectionTrack.String(),
			"audio":     map[string]any{"original": model.Blob{CID: "bafkreichwqg55i6naccjmhlvfsbcsdy62isbqlz2xk7kali463p4u3ie4e", MIMEType: "audio/flac", Size: 42}},
			"title":     "Sounds of Earth",
			"createdAt": "2026-10-09T12:00:00.000Z",
		}

		ref, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, record)
		is.NotError(t, err)
		is.True(t, ref.RecordKey != "", "no record key")
		is.Equal(t, model.ATURI("at://"+atprototest.AliceDID+"/com.audioadastra.track/"+ref.RecordKey.String()), ref.URI)
		is.True(t, strings.HasPrefix(ref.CID.String(), "bafyrei"), ref.CID.String())
		is.True(t, h.hasSpan("com.atproto.repo.createRecord"), "no child span")

		stored, ok := h.net.GetRecord(atprototest.AliceDID, model.CollectionTrack, ref.RecordKey)
		is.True(t, ok, "no record on the PDS")
		is.Equal(t, any("Sounds of Earth"), stored["title"])
		original := stored["audio"].(map[string]any)["original"].(map[string]any)
		is.Equal(t, any("blob"), original["$type"])
		is.Equal(t, any("audio/flac"), original["mimeType"])
	})

	t.Run("should report a failed write", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.CreateRecordFails = true

		_, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, map[string]any{"$type": model.CollectionTrack.String()})
		is.Error(t, model.ErrorRecordWriteFailed, err)
	})

	t.Run("should report a session the PDS refuses", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.ForgetTokens()

		_, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, map[string]any{"$type": model.CollectionTrack.String()})
		is.Error(t, model.ErrorPDSAuthFailed, err)
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.CreateRecord(t.Context(), atprototest.AliceDID, "nope", model.CollectionTrack, map[string]any{})
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}
