package atproto

import (
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

// DescribeServer of the PDS that hosts the account, the one its session on the device with the given
// ID talks to. Descriptions are cached per PDS host for a day, and failures for a minute, so a PDS that
// is down is not asked again on every call. The PDS host lands on the span in the context as
// atproto.pds_host, and whether the cache answered as atproto.describe_server_cache, hit or miss. It
// only reads the session's host, so it does not wait for other calls as the session.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session, and
// [model.ErrorPDSUnavailable] when the PDS does not describe itself.
func (c *Client) DescribeServer(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (model.ServerDescription, error) {
	sess, err := c.resumeSession(ctx, did, sessionID)
	if err != nil {
		return model.ServerDescription{}, err
	}
	host := sess.sess.Data.HostURL
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("atproto.pds_host", hostOf(host)))

	description, hit, err := c.servers.get(ctx, host, c.describeServer)
	if hit {
		span.SetAttributes(attribute.String("atproto.describe_server_cache", "hit"))
	} else {
		span.SetAttributes(attribute.String("atproto.describe_server_cache", "miss"))
	}
	return description, err
}

// describeServer at the PDS host, which is an unauthenticated query.
func (c *Client) describeServer(ctx context.Context, host string) (description model.ServerDescription, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ctx, span := c.tracer.Start(ctx, "com.atproto.server.describeServer", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(host))))
	defer func() { endSpan(span, err) }()

	api := atclient.NewAPIClient(host)
	api.Client = c.app.Client
	var out struct {
		BlobUploadLimit int64 `json:"blobUploadLimit"`
	}
	if err := api.Get(ctx, "com.atproto.server.describeServer", nil, &out); err != nil {
		return model.ServerDescription{}, fmt.Errorf("%w: describing server: %w", model.ErrorPDSUnavailable, err)
	}
	if out.BlobUploadLimit > 0 {
		span.SetAttributes(attribute.Int64("atproto.blob_upload_limit", out.BlobUploadLimit))
	}
	return model.ServerDescription{BlobUploadLimit: max(out.BlobUploadLimit, 0)}, nil
}

// serverCache of PDS descriptions by host. Safe for concurrent use.
type serverCache struct {
	mu      sync.Mutex
	entries map[string]serverCacheEntry
}

type serverCacheEntry struct {
	description model.ServerDescription
	err         error
	expires     time.Time
}

// get the description of the PDS at the host from the cache, or fetch and cache it, reporting whether
// the cache answered. A fetch cut short by the caller's own context is not cached, since it says nothing
// about the PDS. Concurrent misses for one host each fetch.
func (c *serverCache) get(ctx context.Context, host string, fetch func(context.Context, string) (model.ServerDescription, error)) (model.ServerDescription, bool, error) {
	c.mu.Lock()
	entry, ok := c.entries[host]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.description, true, entry.err
	}

	description, err := fetch(ctx, host)
	if ctx.Err() != nil {
		return description, false, err
	}

	ttl := 24 * time.Hour
	if err != nil {
		ttl = time.Minute
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]serverCacheEntry{}
	}
	c.entries[host] = serverCacheEntry{description: description, err: err, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
	return description, false, err
}

// UploadBlob to the account's repository, as the account on the device with the given session,
// declaring the given MIME type. The content of the given size is read from its start, and read again
// from its start, independently, if the request has to be retried. A cheap authenticated call goes
// first, so that a stale DPoP nonce or an expired access token costs a retry of that call rather than
// a second upload of the content. There is no deadline on the whole upload, which may be large, but it
// fails when no progress has been made for a while: when the PDS takes no more of the content, or does
// not respond once it has it all. As with [Client.GetRecord], calls as the same session take turns,
// so another call as the session waits for an upload to finish.
//
// The PDS's description of the blob is checked against the content: the size must be the one sent,
// and the CID the content's own in the form blobs have, a CIDv1 with the raw codec and a SHA-256
// multihash, in any multibase. Which check failed lands on the span as atproto.blob_check_failed, with
// the CID the PDS answered with as atproto.returned_cid. The blob returned has the content's CID, in
// base32, and the MIME type the PDS describes it with, which may be the PDS's own reading of the
// content rather than the one declared.
//
// Errors are the context's when it is done while waiting for another call as the session,
// [model.ErrorOAuthSessionNotFound] when there is no such session, [model.ErrorBlobTooLarge] when the
// blob is over the PDS's limit, [model.ErrorBlobTypeRefused] when the PDS refuses the type
// under the granted scopes, [model.ErrorPDSAuthFailed] when it refuses the session,
// [model.ErrorBlobRejected] when it refuses the blob for another reason or describes it as other than
// what was sent, and [model.ErrorPDSUnavailable] when it cannot be reached, fails, or stops making
// progress.
func (c *Client) UploadBlob(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, content io.ReaderAt, size int64, mimeType string) (model.Blob, error) {
	sess, unlock, err := c.lockSession(ctx, did, sessionID)
	if err != nil {
		return model.Blob{}, err
	}
	defer unlock()
	return sess.UploadBlob(ctx, content, size, mimeType)
}

// UploadBlob to the account's repository, as [Client.UploadBlob] describes.
func (s *session) UploadBlob(ctx context.Context, content io.ReaderAt, size int64, mimeType string) (blob model.Blob, err error) {
	ctx, span := s.client.tracer.Start(ctx, "com.atproto.repo.uploadBlob", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.ServerAddress(hostOf(s.sess.Data.HostURL)),
			attribute.String("atproto.did", s.DID().String()),
			attribute.String("atproto.declared_mime_type", mimeType),
			semconv.HTTPRequestBodySize(int(size)),
		))
	defer func() { endSpan(span, err) }()

	// The content's CID, to hold the PDS's description of the blob to. It is computed before sending,
	// from the start, so that it is the same whichever attempt the PDS ends up keeping.
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(content, 0, size)); err != nil {
		return model.Blob{}, fmt.Errorf("reading blob content: %w", err)
	}
	contentCID := cid.NewCidV1(cid.Raw, must(multihash.Encode(hash.Sum(nil), multihash.SHA2_256)))

	s.refreshAuth(ctx, span)

	errUploadStalled := errors.New("upload stalled")
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idleTimeout := s.client.uploadIdleTimeout
	idle := time.AfterFunc(idleTimeout, func() { cancel(errUploadStalled) })
	defer idle.Stop()

	// Every attempt reads the content through a reader of its own, since the transport may still be
	// reading an abandoned attempt's body when a retry starts.
	body := func() (io.ReadCloser, error) {
		return io.NopCloser(&progressReader{r: io.NewSectionReader(content, 0, size), progress: func() { idle.Reset(idleTimeout) }}), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sess.Data.HostURL+"/xrpc/com.atproto.repo.uploadBlob", http.NoBody)
	if err != nil {
		return model.Blob{}, fmt.Errorf("building upload request: %w", err)
	}
	req.Body, _ = body()
	req.GetBody = body
	req.ContentLength = size
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("Accept", "application/json")
	if s.sess.Config.UserAgent != "" {
		req.Header.Set("User-Agent", s.sess.Config.UserAgent)
	}

	// The session's HTTP client has a deadline on whole requests, which an upload must not have; the
	// stall timer above stands in for it. A redirect would send the content and the DPoP proof on to
	// another URL, so it is not followed.
	httpClient := &http.Client{
		Transport: s.client.app.Client.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := s.sess.DoWithAuth(httpClient, req, "com.atproto.repo.uploadBlob")
	if err != nil {
		if errors.Is(context.Cause(ctx), errUploadStalled) {
			return model.Blob{}, fmt.Errorf("%w: uploading blob: no progress for %v: %w", model.ErrorPDSUnavailable, idleTimeout, err)
		}
		if ctx.Err() != nil {
			return model.Blob{}, fmt.Errorf("uploading blob: %w", context.Cause(ctx))
		}
		if refreshRefused(err) {
			return model.Blob{}, fmt.Errorf("%w: uploading blob: %w", model.ErrorPDSAuthFailed, boundedError{err})
		}
		return model.Blob{}, fmt.Errorf("%w: uploading blob: %w", model.ErrorPDSUnavailable, boundedError{err})
	}
	defer func() { _ = res.Body.Close() }()
	span.SetAttributes(semconv.HTTPResponseStatusCode(res.StatusCode))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		var apiErr atclient.ErrorBody
		_ = json.UnmarshalRead(io.LimitReader(res.Body, 64*1024), &apiErr)
		if apiErr.Name != "" {
			span.SetAttributes(attribute.String("atproto.error", bounded(apiErr.Name)))
		}
		return model.Blob{}, fmt.Errorf("%w: uploading blob: %v %v: %v", blobRefusal(res.StatusCode, apiErr.Name), res.StatusCode, bounded(apiErr.Name), bounded(apiErr.Message))
	}

	var out struct {
		Blob struct {
			Ref struct {
				Link string `json:"$link"`
			} `json:"ref"`
			MIMEType string `json:"mimeType"`
			Size     int64  `json:"size"`
		} `json:"blob"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, 64*1024), &out); err != nil {
		return model.Blob{}, fmt.Errorf("%w: reading uploadBlob response: %w", model.ErrorPDSUnavailable, boundedError{err})
	}
	blob = model.Blob{CID: model.CID(out.Blob.Ref.Link), MIMEType: out.Blob.MIMEType, Size: out.Blob.Size}
	span.SetAttributes(attribute.String("atproto.blob_mime_type", bounded(blob.MIMEType)))
	if failed := blobCheck(blob, contentCID, size); failed != "" {
		span.SetAttributes(attribute.String("atproto.blob_check_failed", failed), attribute.String("atproto.returned_cid", bounded(blob.CID.String())))
		return model.Blob{}, fmt.Errorf("%w: uploadBlob response describes another blob than was sent: %v: CID %v, size %v, MIME type %v",
			model.ErrorBlobRejected, failed, bounded(blob.CID.String()), blob.Size, bounded(blob.MIMEType))
	}
	// The CID is the content's own, in base32, whichever multibase the PDS answered in.
	blob.CID = model.CID(contentCID.String())
	span.SetAttributes(attribute.String("atproto.blob_cid", blob.CID.String()))
	return blob, nil
}

// blobCheck of the blob the PDS describes against the content sent: which check failed, or empty when
// none did.
func blobCheck(blob model.Blob, contentCID cid.Cid, size int64) string {
	if blob.Size != size {
		return "size"
	}
	got, err := cid.Decode(blob.CID.String())
	switch {
	case err != nil:
		return "cid_syntax"
	case got.Version() != 1 || got.Prefix().Codec != cid.Raw:
		return "cid_codec"
	case got.Prefix().MhType != multihash.SHA2_256:
		return "cid_hash"
	case !got.Equals(contentCID):
		return "cid"
	}
	return ""
}

// must have the value, or panic with the error, for calls that cannot fail with the arguments given.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// refreshAuth of the session with a cheap authenticated call, which picks up the PDS's current DPoP
// nonce and refreshes an expired access token. It is best effort: a failure lands on the span as
// atproto.auth_refresh_error, and the call it goes before finds out for itself.
func (s *session) refreshAuth(ctx context.Context, span trace.Span) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.api.Get(ctx, "com.atproto.server.getSession", nil, nil); err != nil {
		span.SetAttributes(attribute.String("atproto.auth_refresh_error", bounded(err.Error())))
	}
}

// blobRefusal classifies an uploadBlob response that is not a success, by its status and the XRPC error
// name. A PDS refuses a blob type outside the granted scopes with 403 ScopeMissingError, and a 401 is
// what is left after the session's own retries for a stale nonce or an expired token.
func blobRefusal(status int, name string) error {
	switch {
	case status == http.StatusUnauthorized:
		return model.ErrorPDSAuthFailed
	case status == http.StatusRequestEntityTooLarge:
		return model.ErrorBlobTooLarge
	case status == http.StatusForbidden && name == "ScopeMissingError":
		return model.ErrorBlobTypeRefused
	case status == http.StatusTooManyRequests || status >= 500:
		return model.ErrorPDSUnavailable
	default:
		return model.ErrorBlobRejected
	}
}

// recordCID of a record in its JSON form, as a repository stores it: its canonical DAG-CBOR encoding,
// read from JSON so that blobs and links are the data model's types, hashed with SHA-256 into a CIDv1
// with the dag-cbor codec.
func recordCID(record jsontext.Value) (cid.Cid, error) {
	data, err := atdata.UnmarshalJSON(record)
	if err != nil {
		return cid.Cid{}, fmt.Errorf("reading record as atproto data: %w", err)
	}
	encoded, err := atdata.MarshalCBOR(data)
	if err != nil {
		return cid.Cid{}, fmt.Errorf("marshaling record to CBOR: %w", err)
	}
	return cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(encoded)
}

// sameCID reports whether the CID a PDS answered with, in any multibase, is the given one.
func sameCID(answered string, want cid.Cid) bool {
	got, err := cid.Decode(answered)
	return err == nil && got.Equals(want)
}

// bounded text for span attributes and errors, where it may carry what a PDS answered, which can be
// anything: text over 256 bytes is cut short, at a character boundary when it is UTF-8, and marked as
// cut.
func bounded(s string) string {
	const limit = 256
	if len(s) <= limit {
		return s
	}
	end := limit
	for end > limit-utf8.UTFMax && !utf8.RuneStart(s[end]) {
		end--
	}
	if !utf8.RuneStart(s[end]) {
		end = limit
	}
	return s[:end] + "…[truncated]"
}

// boundedError wraps an error whose text may carry what a PDS answered, with the text [bounded].
type boundedError struct {
	err error
}

func (e boundedError) Error() string {
	return bounded(e.err.Error())
}

func (e boundedError) Unwrap() error {
	return e.err
}

// refreshRefused reports whether the error is from the auth server refusing to refresh the session's
// tokens, as for a session revoked elsewhere. The SDK says so only in its message, with the token
// endpoint's 4xx status, which tells a refusal apart from an auth server that could not be reached.
func refreshRefused(err error) bool {
	msg := err.Error()
	refused, _ := regexp.MatchString(`(token refresh|auth server request) failed \(HTTP 4\d\d\)`, msg)
	return refused
}

// progressReader calls progress after every read.
type progressReader struct {
	r        io.Reader
	progress func()
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.progress()
	return n, err
}

// CreateRecord in the account's repository at the given key, as the account on the device with the
// given session, unless it is there already. As with [Client.GetRecord], calls as the same session take
// turns.
//
// The write is conditional on the key being free, so calling again with the same record, as after an
// answer that was lost, does not publish it twice. A PDS answers the write of the very record that is
// there with a success and no commit, or says the key is taken: a record there with the CID of the one
// given is then taken as written, and one with another CID is left alone, and is an error. Either way,
// that a record was there lands on the span as atproto.record_existed.
//
// The URI and CID the PDS answers with are checked: the URI must be the record's, of the account, the
// collection and the key, and the CID the record's own, of its canonical DAG-CBOR encoding, in any
// multibase. Which check failed lands on the span as atproto.record_check_failed, uri or cid. As the
// PDS may have written the record all the same, the record at the key is then deleted, best effort, on
// condition that it has the record's own CID, whatever CID the PDS answered with, so that no other
// record is deleted. How that went lands on the span as atproto.cleanup: deleted, kept when the record
// there has another CID, or failed, with atproto.cleanup_error. The CID returned is the record's own,
// in base32.
//
// Errors are the context's when it is done while waiting for another call as the session,
// [model.ErrorOAuthSessionNotFound] when there is no such session,
// [model.ErrorPDSAuthFailed] when the PDS refuses the session, [model.ErrorScopeDenied] when the session
// was not granted the collection, [model.ErrorRecordConflict] when another record is at the key,
// [model.ErrorRecordAnswerUnexpected] when the PDS answers with another URI or CID, and
// [model.ErrorRecordWriteFailed] when the PDS does not write the record for another reason.
func (c *Client) CreateRecord(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, rkey model.RecordKey, record map[string]any) (model.RecordRef, error) {
	sess, unlock, err := c.lockSession(ctx, did, sessionID)
	if err != nil {
		return model.RecordRef{}, err
	}
	defer unlock()
	return sess.CreateRecord(ctx, collection, rkey, record)
}

// CreateRecord in the account's repository, as [Client.CreateRecord] describes.
func (s *session) CreateRecord(ctx context.Context, collection model.NSID, rkey model.RecordKey, record map[string]any) (ref model.RecordRef, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ctx, span := s.client.tracer.Start(ctx, "com.atproto.repo.putRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.HostURL)), attribute.String("atproto.did", s.DID().String()),
			attribute.String("atproto.collection", collection.String()), attribute.String("atproto.rkey", rkey.String())))
	defer func() { endSpan(span, err) }()

	// The record is encoded once, and the CID it should have computed from the very JSON sent, so the
	// CID the PDS answers with can be held to it.
	raw, err := json.Marshal(record)
	if err != nil {
		return model.RecordRef{}, fmt.Errorf("marshaling record: %w", err)
	}
	wantCID, err := recordCID(raw)
	if err != nil {
		return model.RecordRef{}, fmt.Errorf("computing record CID: %w", err)
	}
	wantURI := "at://" + s.DID().String() + "/" + collection.String() + "/" + rkey.String()
	want := model.RecordRef{URI: model.ATURI(wantURI), RecordKey: rkey, CID: model.CID(wantCID.String())}

	// A null swapRecord makes the write conditional on there being no record at the key. A PDS answers a
	// write of the very record that is there with success, and of another one with InvalidSwap.
	body := map[string]any{"repo": s.DID().String(), "collection": collection.String(), "rkey": rkey.String(), "record": jsontext.Value(raw), "swapRecord": nil}
	var out struct {
		URI    string `json:"uri"`
		CID    string `json:"cid"`
		Commit *struct {
			CID string `json:"cid"`
		} `json:"commit"`
	}
	if err := s.api.Post(ctx, "com.atproto.repo.putRecord", body, &out); err != nil {
		var apiErr *atclient.APIError
		switch {
		case (errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized) || refreshRefused(err):
			return model.RecordRef{}, fmt.Errorf("%w: creating record %v: %w", model.ErrorPDSAuthFailed, wantURI, boundedError{err})
		// A session granted before the app asked for the collection's scope is refused with this.
		case apiErr != nil && apiErr.StatusCode == http.StatusForbidden && apiErr.Name == "ScopeMissingError":
			return model.RecordRef{}, fmt.Errorf("%w: creating record %v: %w", model.ErrorScopeDenied, wantURI, boundedError{err})
		case apiErr != nil && apiErr.Name == "InvalidSwap":
			return s.existingRecord(ctx, span, collection, want, wantCID)
		}
		return model.RecordRef{}, fmt.Errorf("%w: creating record %v: %w", model.ErrorRecordWriteFailed, wantURI, boundedError{err})
	}

	// The PDS is the account's, not the app's, so what it answers with is only trusted for the record
	// written: its URI, and the CID of the version kept.
	failed := ""
	switch {
	case out.URI != wantURI:
		failed = "uri"
		span.SetAttributes(attribute.String("atproto.returned_uri", bounded(out.URI)))
	case !sameCID(out.CID, wantCID):
		failed = "cid"
		span.SetAttributes(attribute.String("atproto.returned_cid", bounded(out.CID)))
	}
	if failed != "" {
		span.SetAttributes(attribute.String("atproto.record_check_failed", failed))
		s.deleteRecord(ctx, span, collection, rkey, wantCID)
		return model.RecordRef{}, fmt.Errorf("%w: putRecord response for %v is URI %v, CID %v, not CID %v",
			model.ErrorRecordAnswerUnexpected, wantURI, bounded(out.URI), bounded(out.CID), wantCID)
	}
	span.SetAttributes(attribute.String("atproto.uri", wantURI), attribute.Bool("atproto.record_existed", out.Commit == nil))
	return want, nil
}

// existingRecord in the collection at the key of the wanted one, which a write found taken: the wanted
// record when the one there has its CID, and [model.ErrorRecordConflict] when it has another.
func (s *session) existingRecord(ctx context.Context, span trace.Span, collection model.NSID, want model.RecordRef, wantCID cid.Cid) (model.RecordRef, error) {
	span.SetAttributes(attribute.Bool("atproto.record_existed", true))
	var out struct {
		CID string `json:"cid"`
	}
	params := map[string]any{"repo": s.DID().String(), "collection": collection.String(), "rkey": want.RecordKey.String()}
	getCtx, getSpan := s.client.tracer.Start(ctx, "com.atproto.repo.getRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.HostURL))))
	err := s.api.Get(getCtx, "com.atproto.repo.getRecord", params, &out)
	endSpan(getSpan, err)
	if err != nil {
		return model.RecordRef{}, fmt.Errorf("%w: getting record %v that is in the way: %w", model.ErrorRecordWriteFailed, want.URI, boundedError{err})
	}
	if !sameCID(out.CID, wantCID) {
		span.SetAttributes(attribute.String("atproto.existing_cid", bounded(out.CID)))
		return model.RecordRef{}, fmt.Errorf("%w: %v is another record, with CID %v, not %v", model.ErrorRecordConflict, want.URI, bounded(out.CID), wantCID)
	}
	span.SetAttributes(attribute.String("atproto.uri", want.URI.String()))
	return want, nil
}

// deleteRecord at the key, on condition that it has the given CID, best effort: how it went lands on the
// span as atproto.cleanup. It outlives the caller's context, so the record is cleaned up after a caller
// that went away too.
func (s *session) deleteRecord(ctx context.Context, span trace.Span, collection model.NSID, rkey model.RecordKey, swap cid.Cid) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	ctx, deleteSpan := s.client.tracer.Start(ctx, "com.atproto.repo.deleteRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.HostURL))))
	body := map[string]any{"repo": s.DID().String(), "collection": collection.String(), "rkey": rkey.String(), "swapRecord": swap.String()}
	err := s.api.Post(ctx, "com.atproto.repo.deleteRecord", body, nil)
	endSpan(deleteSpan, err)
	var apiErr *atclient.APIError
	switch {
	case err == nil:
		span.SetAttributes(attribute.String("atproto.cleanup", "deleted"))
	case errors.As(err, &apiErr) && apiErr.Name == "InvalidSwap":
		span.SetAttributes(attribute.String("atproto.cleanup", "kept"))
	default:
		span.SetAttributes(attribute.String("atproto.cleanup", "failed"), attribute.String("atproto.cleanup_error", bounded(err.Error())))
	}
}
