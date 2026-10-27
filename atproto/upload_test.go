package atproto_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"hegel.dev/go/hegel"
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

	t.Run("should refuse an answer over 1 MiB, quickly", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.ResponsePadding = 2 * 1024 * 1024

		start := time.Now()
		_, err := h.client.DescribeServer(t.Context(), did, sessionID)
		is.Error(t, model.ErrorPDSUnavailable, err)
		is.True(t, strings.Contains(err.Error(), "response body is over 1048576 bytes"), err.Error())
		is.True(t, time.Since(start) < time.Second, "slow to refuse")
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

	content := []byte("ID3 pretend this is an MP3")
	otherContent := []byte("ID3 another MP3")
	blobChecks := []struct {
		name   string
		answer func(blob map[string]any)
		failed string
	}{
		{name: "another size", answer: func(blob map[string]any) { blob["size"] = -1 }, failed: "size"},
		{name: "another blob's CID", answer: func(blob map[string]any) {
			blob["ref"] = map[string]any{"$link": cidOf(t, cid.Raw, multihash.SHA2_256, otherContent)}
		}, failed: "cid"},
		{name: "a CID with another codec", answer: func(blob map[string]any) {
			blob["ref"] = map[string]any{"$link": cidOf(t, cid.DagCBOR, multihash.SHA2_256, content)}
		}, failed: "cid_codec"},
		{name: "a CID with another hash", answer: func(blob map[string]any) {
			blob["ref"] = map[string]any{"$link": cidOf(t, cid.Raw, multihash.SHA2_512, content)}
		}, failed: "cid_hash"},
		{name: "a CID that does not parse", answer: func(blob map[string]any) { blob["ref"] = map[string]any{"$link": "not a CID"} }, failed: "cid_syntax"},
	}
	for _, test := range blobChecks {
		t.Run("should refuse a blob described with "+test.name, func(t *testing.T) {
			h := newHarness(t)
			did, sessionID := h.login(t)
			h.net.UploadBlobAnswer = test.answer

			_, err := h.client.UploadBlob(t.Context(), did, sessionID, bytes.NewReader(content), int64(len(content)), "audio/mpeg")
			is.Error(t, model.ErrorBlobRejected, err)
			is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "com.atproto.repo.uploadBlob"), attribute.String("atproto.blob_check_failed", test.failed)), "no failed check on the span")
		})
	}

	t.Run("should return a blob the PDS describes as the content sent, and refuse any other with the failed check on the span", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		hegel.Test(t, func(ht *hegel.T) {
			content := hegel.Draw(ht, hegel.Binary(0, 256))
			mimeType := hegel.Draw(ht, hegel.Text().MaxSize(40))
			size := int64(len(content))
			answeredSize := size
			var failed []string
			if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
				answeredSize += hegel.Draw(ht, hegel.Filter(hegel.Integers[int64](math.MinInt64/2, math.MaxInt64/2), func(n int64) bool { return n != 0 }))
				failed = append(failed, "size")
			}
			link, cidFailed := drawAnsweredCID(ht, cid.Raw, content)
			if cidFailed != "" {
				failed = append(failed, cidFailed)
			}
			h.net.UploadBlobAnswer = func(blob map[string]any) {
				blob["ref"] = map[string]any{"$link": link}
				blob["size"] = answeredSize
				blob["mimeType"] = mimeType
			}

			blob, err := h.client.UploadBlob(t.Context(), did, sessionID, bytes.NewReader(content), size, "audio/mpeg")
			attrs := h.lastSpanAttributes("com.atproto.repo.uploadBlob")
			if len(failed) == 0 {
				is.NotError(ht, err)
				is.Equal(ht, model.Blob{CID: model.CID(cidOfContent(cid.Raw, content).String()), MIMEType: mimeType, Size: size}, blob)
				is.True(ht, !oteltest.HasAttributeKey(attrs, "atproto.blob_check_failed"), "failed check on the span")
				return
			}
			is.Error(ht, model.ErrorBlobRejected, err)
			is.True(ht, oteltest.HasAttribute(attrs, attribute.String("atproto.blob_check_failed", failed[0])), "want", failed[0], "on the span")
		}, hegel.WithTestCases(300))
	})

	t.Run("should return the content's CID in base32, however the PDS encodes it", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		contentCID := cidOf(t, cid.Raw, multihash.SHA2_256, content)
		c, err := cid.Decode(contentCID)
		is.NotError(t, err)
		base58, err := c.StringOfBase(multibase.Base58BTC)
		is.NotError(t, err)
		h.net.UploadBlobAnswer = func(blob map[string]any) { blob["ref"] = map[string]any{"$link": base58} }

		blob, err := h.client.UploadBlob(t.Context(), did, sessionID, bytes.NewReader(content), int64(len(content)), "audio/mpeg")
		is.NotError(t, err)
		is.Equal(t, model.CID(contentCID), blob.CID)
		_, ok := h.net.GetBlob(blob.CID)
		is.True(t, ok, "no blob on the PDS by the CID returned")
	})

	t.Run("should cut what the PDS describes the blob with short on the span and in the error", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		long := strings.Repeat("é", 10000)
		h.net.UploadBlobAnswer = func(blob map[string]any) {
			blob["ref"] = map[string]any{"$link": long}
			blob["mimeType"] = long
		}

		_, err := h.client.UploadBlob(t.Context(), did, sessionID, bytes.NewReader(content), int64(len(content)), "audio/mpeg")
		is.Error(t, model.ErrorBlobRejected, err)
		is.True(t, len(err.Error()) < 1024, "error is", len(err.Error()), "bytes")
		attrs := h.spanAttributes(t, "com.atproto.repo.uploadBlob")
		for _, key := range []attribute.Key{"atproto.returned_cid", "atproto.blob_mime_type"} {
			is.True(t, oteltest.HasAttribute(attrs, attribute.String(string(key), strings.Repeat("é", 128)+"…[truncated]")), string(key), "not cut short")
		}
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

	t.Run("should make uploads as the same session take turns, and let other sessions' run alongside", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		_, otherSessionID := h.login(t)
		h.net.UploadBlobDelay = 100 * time.Millisecond

		upload := func(sessionIDs ...model.OAuthSessionID) {
			var wg sync.WaitGroup
			for _, id := range sessionIDs {
				wg.Go(func() {
					_, err := h.client.UploadBlob(t.Context(), did, id, strings.NewReader("four"), 4, "audio/mpeg")
					is.NotError(t, err)
				})
			}
			wg.Wait()
		}

		upload(sessionID, sessionID)
		is.Equal(t, 1, h.net.MaxUploadBlobsInFlight())

		upload(sessionID, otherSessionID)
		is.Equal(t, 2, h.net.MaxUploadBlobsInFlight())
	})

	t.Run("should give up waiting for another upload as the session when the context is done", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.UploadBlobDelay = time.Minute

		first, cancelFirst := context.WithCancel(t.Context())
		defer cancelFirst()
		go func() { _, _ = h.client.UploadBlob(first, did, sessionID, strings.NewReader("four"), 4, "audio/mpeg") }()
		for h.net.UploadBlobCalls() == 0 {
			time.Sleep(time.Millisecond)
		}

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err := h.client.UploadBlob(ctx, did, sessionID, strings.NewReader("four"), 4, "audio/mpeg")
		is.Error(t, context.DeadlineExceeded, err)
		is.Equal(t, 1, h.net.UploadBlobCalls())
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

// cidOf the content, with the given codec and multihash.
func cidOf(t *testing.T, codec, hash uint64, content []byte) string {
	t.Helper()

	c, err := cid.NewPrefixV1(codec, hash).Sum(content)
	is.NotError(t, err)
	return c.String()
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

	refusals := []struct {
		name     string
		uri      string
		mismatch string
	}{
		{name: "another account's record", uri: "at://" + bobDID + "/com.audioadastra.track/3l2b5x3qe4c2a", mismatch: "authority is another account's DID"},
		{name: "a record in another collection", uri: "at://" + atprototest.AliceDID + "/com.audioadastra.actor.profile/3l2b5x3qe4c2a", mismatch: "collection is not the one asked for"},
		{name: "a record key that is not a TID", uri: "at://" + atprototest.AliceDID + "/com.audioadastra.track/self", mismatch: "record key is not a TID"},
		{name: "a handle for the authority", uri: "at://alice.test/com.audioadastra.track/3l2b5x3qe4c2a", mismatch: "authority is not a DID"},
	}
	for _, test := range refusals {
		t.Run("should refuse a response naming "+test.name, func(t *testing.T) {
			h := newHarness(t)
			did, sessionID := h.login(t)
			h.net.CreateRecordURI = test.uri

			ctx, span := otel.Tracer("test").Start(t.Context(), "request")
			_, err := h.client.CreateRecord(ctx, did, sessionID, model.CollectionTrack, map[string]any{"$type": model.CollectionTrack.String()})
			span.End()
			is.Error(t, model.ErrorRecordWriteFailed, err)
			is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "com.atproto.repo.createRecord"), attribute.String("atproto.uri_mismatch", test.mismatch)), "no mismatch on the span")
		})
	}

	// The CID of soundsOfEarth's canonical DAG-CBOR encoding, with the blob as a blob and its ref a link.
	soundsOfEarth := map[string]any{
		"$type":     model.CollectionTrack.String(),
		"audio":     map[string]any{"original": model.Blob{CID: "bafkreichwqg55i6naccjmhlvfsbcsdy62isbqlz2xk7kali463p4u3ie4e", MIMEType: "audio/flac", Size: 42}},
		"title":     "Sounds of Earth",
		"createdAt": "2026-10-09T12:00:00.000Z",
	}
	soundsOfEarthCID := "bafyreig777ldy2tccxvhu3psa2eatiablup6vq4qt2jrpx6mciayhsrzdm"

	t.Run("should answer with the CID of the record as a repository encodes it", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		ref, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, soundsOfEarth)
		is.NotError(t, err)
		is.Equal(t, model.CID(soundsOfEarthCID), ref.CID)
	})

	t.Run("should answer with the record's CID in base32, however the PDS encodes it", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		c, err := cid.Decode(soundsOfEarthCID)
		is.NotError(t, err)
		base58, err := c.StringOfBase(multibase.Base58BTC)
		is.NotError(t, err)
		h.net.CreateRecordCID = base58

		ref, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, soundsOfEarth)
		is.NotError(t, err)
		is.Equal(t, model.CID(soundsOfEarthCID), ref.CID)
	})

	sameHashRaw, err := cid.Decode(soundsOfEarthCID)
	is.NotError(t, err)
	for name, recordCID := range map[string]string{
		"another record's CID":                 cidOf(t, cid.DagCBOR, multihash.SHA2_256, []byte("another record")),
		"the record's hash with the raw codec": cid.NewCidV1(cid.Raw, sameHashRaw.Hash()).String(),
		"a CID that is not a CID":              "not a CID",
	} {
		t.Run("should refuse a response with "+name, func(t *testing.T) {
			h := newHarness(t)
			did, sessionID := h.login(t)
			h.net.CreateRecordCID = recordCID

			_, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, soundsOfEarth)
			is.Error(t, model.ErrorRecordWriteFailed, err)
			is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "com.atproto.repo.createRecord"), attribute.String("atproto.record_check_failed", "cid")), "no failed check on the span")
		})
	}

	t.Run("should return a record the PDS answers for as the one sent, and refuse any other with the failed check on the span", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		hegel.Test(t, func(ht *hegel.T) {
			record := hegel.Draw(ht, anyTrackRecords())
			rkey := syntax.NewTID(hegel.Draw(ht, hegel.Integers[int64](0, 1<<53-1)), hegel.Draw(ht, hegel.Integers[uint](0, 1023))).String()
			authority, collection := did.String(), model.CollectionTrack.String()
			var mismatches []string
			if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
				authority = hegel.Draw(ht, hegel.SampledFrom([]string{"alice.test", bobDID, "did:plc:" + strings.ToUpper(strings.TrimPrefix(did.String(), "did:plc:"))}))
				if strings.HasPrefix(authority, "did:") {
					mismatches = append(mismatches, "authority is another account's DID")
				} else {
					mismatches = append(mismatches, "authority is not a DID")
				}
			}
			if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
				collection = hegel.Draw(ht, hegel.SampledFrom([]string{"com.audioadastra.actor.profile", "com.audioadastra.Track", "app.bsky.feed.post"}))
				mismatches = append(mismatches, "collection is not the one asked for")
			}
			if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
				tid := rkey
				rkey = hegel.Draw(ht, hegel.Filter(hegel.SampledFrom([]string{"self", strings.ToUpper(tid), tid + "a"}), func(k string) bool { return k != tid }))
				mismatches = append(mismatches, "record key is not a TID")
			}
			raw, err := json.Marshal(record)
			is.NotError(ht, err)
			encoded, err := atdata.MarshalCBOR(must(atdata.UnmarshalJSON(raw)))
			is.NotError(ht, err)
			answeredCID, cidFailed := drawAnsweredCID(ht, cid.DagCBOR, encoded)
			uri := "at://" + authority + "/" + collection + "/" + rkey
			h.net.CreateRecordURI = uri
			h.net.CreateRecordCID = answeredCID

			ref, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, record)
			attrs := h.lastSpanAttributes("com.atproto.repo.createRecord")
			switch {
			case len(mismatches) > 0:
				is.Error(ht, model.ErrorRecordWriteFailed, err)
				is.True(ht, oteltest.HasAttribute(attrs, attribute.String("atproto.uri_mismatch", mismatches[0])), "want", mismatches[0], "on the span")
			case cidFailed != "":
				is.Error(ht, model.ErrorRecordWriteFailed, err)
				is.True(ht, oteltest.HasAttribute(attrs, attribute.String("atproto.record_check_failed", "cid")), "no failed check on the span")
			default:
				is.NotError(ht, err)
				is.Equal(ht, model.RecordRef{URI: model.ATURI(uri), RecordKey: model.RecordKey(rkey), CID: model.CID(cidOfContent(cid.DagCBOR, encoded).String())}, ref)
			}
		}, hegel.WithTestCases(300))
	})

	t.Run("should cut what the PDS answered with short on the span and in the error", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		long := strings.Repeat("b", 10000)
		h.net.CreateRecordCID = long

		_, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, soundsOfEarth)
		is.Error(t, model.ErrorRecordWriteFailed, err)
		is.True(t, len(err.Error()) < 1024, "error is", len(err.Error()), "bytes")
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "com.atproto.repo.createRecord"), attribute.String("atproto.returned_cid", strings.Repeat("b", 256)+"…[truncated]")), "CID not cut short")

		h.net.CreateRecordCID = ""
		h.net.CreateRecordURI = "at://did:example:" + strings.Repeat("a", 2000) + "/com.audioadastra.track/3l2b5x3qe4c2a"
		_, err = h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, soundsOfEarth)
		is.Error(t, model.ErrorRecordWriteFailed, err)
		is.True(t, len(err.Error()) < 1024, "error is", len(err.Error()), "bytes")
		is.True(t, oteltest.HasAttribute(h.lastSpanAttributes("com.atproto.repo.createRecord"), attribute.String("atproto.returned_uri", "at://did:example:"+strings.Repeat("a", 239)+"…[truncated]")), "URI not cut short")

		h.net.CreateRecordURI = "at://" + strings.Repeat("a", 8000) + "/com.audioadastra.track/3l2b5x3qe4c2a"
		_, err = h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, soundsOfEarth)
		is.Error(t, model.ErrorRecordWriteFailed, err)
		is.True(t, len(err.Error()) < 1024, "error is", len(err.Error()), "bytes")
	})

	t.Run("should report a session not granted the collection", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.CreateRecordScopeMissing = true

		_, err := h.client.CreateRecord(t.Context(), did, sessionID, model.CollectionTrack, map[string]any{"$type": model.CollectionTrack.String()})
		is.Error(t, model.ErrorScopeDenied, err)
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

// cidOfContent of the given codec, with a SHA-256 multihash.
func cidOfContent(codec uint64, content []byte) cid.Cid {
	return must(cid.NewPrefixV1(codec, multihash.SHA2_256).Sum(content))
}

// drawAnsweredCID for content of the given codec: its CID in any multibase, or a CID with another codec,
// hash or digest, in any combination, or no CID at all, with the check that fails first for it, or empty
// when none does.
func drawAnsweredCID(ht *hegel.T, codec uint64, content []byte) (string, string) {
	if hegel.Draw(ht, hegel.WeightedBooleans(0.1)) {
		// Not empty, which the fake PDS takes as no answer of the test's own.
		return hegel.Draw(ht, hegel.Filter(hegel.Text().MinSize(1), func(s string) bool { _, err := cid.Decode(s); return err != nil })), "cid_syntax"
	}
	hash := uint64(multihash.SHA2_256)
	var failed []string
	if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
		codec = hegel.Draw(ht, hegel.Filter(hegel.SampledFrom([]uint64{cid.Raw, cid.DagCBOR, cid.DagProtobuf, cid.DagJSON, 0x1234}), func(c uint64) bool { return c != codec }))
		failed = append(failed, "cid_codec")
	}
	if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
		hash = hegel.Draw(ht, hegel.SampledFrom([]uint64{multihash.SHA2_512, multihash.SHA3_256, multihash.SHA1, multihash.IDENTITY}))
		failed = append(failed, "cid_hash")
	}
	if hegel.Draw(ht, hegel.WeightedBooleans(0.2)) {
		content = append(slices.Clone(content), hegel.Draw(ht, hegel.Binary(1, 8))...)
		failed = append(failed, "cid")
	}
	c := must(cid.NewPrefixV1(codec, hash).Sum(content))
	encoded, err := c.StringOfBase(hegel.Draw(ht, hegel.SampledFrom([]multibase.Encoding{multibase.Base32, multibase.Base32Upper, multibase.Base58BTC, multibase.Base64, multibase.Base16, multibase.Base36})))
	is.NotError(ht, err)
	if len(failed) == 0 {
		return encoded, ""
	}
	return encoded, failed[0]
}

// anyTrackRecords with any title and description.
func anyTrackRecords() hegel.Generator[map[string]any] {
	return hegel.Composite(func(tc hegel.TestCase) map[string]any {
		record := map[string]any{
			"$type":     model.CollectionTrack.String(),
			"audio":     map[string]any{"original": model.Blob{CID: "bafkreichwqg55i6naccjmhlvfsbcsdy62isbqlz2xk7kali463p4u3ie4e", MIMEType: "audio/flac", Size: 42}},
			"title":     hegel.Draw(tc, hegel.Text().MinSize(1)),
			"createdAt": model.Time{T: hegel.Draw(tc, hegel.Datetimes())}.String(),
		}
		if hegel.Draw(tc, hegel.Booleans()) {
			record["description"] = hegel.Draw(tc, hegel.Text())
		}
		return record
	})
}

// must have the value, or panic with the error.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
