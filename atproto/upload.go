package atproto

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

// DescribeServer of the PDS that hosts the account, the one its session on the device with the given
// ID talks to. Descriptions are cached per PDS host for a day, and failures for a minute, so a PDS that
// is down is not asked again on every call. The PDS host lands on the span in the context as
// atproto.pds_host, and whether the cache answered as atproto.describe_server_cache, hit or miss.
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
// not respond once it has it all. As with [Client.GetRecord], calls as the same session must not
// overlap.
//
// The blob returned is as the PDS describes it, whose MIME type may be the PDS's own reading of the
// content rather than the one declared.
//
// Errors are [model.ErrorOAuthSessionNotFound] when there is no such session, [model.ErrorBlobTooLarge]
// when the blob is over the PDS's limit, [model.ErrorBlobTypeRefused] when the PDS refuses the type
// under the granted scopes, [model.ErrorPDSAuthFailed] when it refuses the session,
// [model.ErrorBlobRejected] when it refuses the blob for another reason, and [model.ErrorPDSUnavailable]
// when it cannot be reached, fails, or stops making progress.
func (c *Client) UploadBlob(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, content io.ReaderAt, size int64, mimeType string) (model.Blob, error) {
	sess, err := c.resumeSession(ctx, did, sessionID)
	if err != nil {
		return model.Blob{}, err
	}
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
			return model.Blob{}, fmt.Errorf("%w: uploading blob: %w", model.ErrorPDSAuthFailed, err)
		}
		return model.Blob{}, fmt.Errorf("%w: uploading blob: %w", model.ErrorPDSUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	span.SetAttributes(semconv.HTTPResponseStatusCode(res.StatusCode))

	if res.StatusCode < 200 || res.StatusCode > 299 {
		var apiErr atclient.ErrorBody
		_ = json.UnmarshalRead(io.LimitReader(res.Body, 64*1024), &apiErr)
		if apiErr.Name != "" {
			span.SetAttributes(attribute.String("atproto.error", apiErr.Name))
		}
		return model.Blob{}, fmt.Errorf("%w: uploading blob: %v %v: %v", blobRefusal(res.StatusCode, apiErr.Name), res.StatusCode, apiErr.Name, apiErr.Message)
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
		return model.Blob{}, fmt.Errorf("%w: reading uploadBlob response: %w", model.ErrorPDSUnavailable, err)
	}
	if _, err := syntax.ParseCID(out.Blob.Ref.Link); err != nil {
		return model.Blob{}, fmt.Errorf("%w: uploadBlob response has no valid blob CID: %w", model.ErrorPDSUnavailable, err)
	}
	blob = model.Blob{CID: model.CID(out.Blob.Ref.Link), MIMEType: out.Blob.MIMEType, Size: out.Blob.Size}
	span.SetAttributes(attribute.String("atproto.blob_cid", blob.CID.String()), attribute.String("atproto.blob_mime_type", blob.MIMEType))
	return blob, nil
}

// refreshAuth of the session with a cheap authenticated call, which picks up the PDS's current DPoP
// nonce and refreshes an expired access token. It is best effort: a failure lands on the span as
// atproto.auth_refresh_error, and the call it goes before finds out for itself.
func (s *session) refreshAuth(ctx context.Context, span trace.Span) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.api.Get(ctx, "com.atproto.server.getSession", nil, nil); err != nil {
		span.SetAttributes(attribute.String("atproto.auth_refresh_error", err.Error()))
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

// CreateRecord in the account's repository with a record key the PDS picks, as the account on the device
// with the given session. As with [Client.GetRecord], calls as the same session must not overlap.
//
// Errors are [model.ErrorOAuthSessionNotFound] when there is no such session,
// [model.ErrorPDSAuthFailed] when the PDS refuses the session, and [model.ErrorRecordWriteFailed] when
// it does not create the record for another reason.
func (c *Client) CreateRecord(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, record map[string]any) (model.RecordRef, error) {
	sess, err := c.resumeSession(ctx, did, sessionID)
	if err != nil {
		return model.RecordRef{}, err
	}
	return sess.CreateRecord(ctx, collection, record)
}

// CreateRecord in the account's repository, as [Client.CreateRecord] describes.
func (s *session) CreateRecord(ctx context.Context, collection model.NSID, record map[string]any) (ref model.RecordRef, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ctx, span := s.client.tracer.Start(ctx, "com.atproto.repo.createRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.HostURL)), attribute.String("atproto.did", s.DID().String()), attribute.String("atproto.collection", collection.String())))
	defer func() { endSpan(span, err) }()

	body := map[string]any{"repo": s.DID().String(), "collection": collection.String(), "record": record}
	var out struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	if err := s.api.Post(ctx, "com.atproto.repo.createRecord", body, &out); err != nil {
		var apiErr *atclient.APIError
		if (errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized) || refreshRefused(err) {
			return model.RecordRef{}, fmt.Errorf("%w: creating record in %v: %w", model.ErrorPDSAuthFailed, collection, err)
		}
		return model.RecordRef{}, fmt.Errorf("%w: creating record in %v: %w", model.ErrorRecordWriteFailed, collection, err)
	}

	uri, err := syntax.ParseATURI(out.URI)
	if err != nil {
		return model.RecordRef{}, fmt.Errorf("%w: createRecord response has no valid URI: %w", model.ErrorRecordWriteFailed, err)
	}
	if _, err := syntax.ParseCID(out.CID); err != nil {
		return model.RecordRef{}, fmt.Errorf("%w: createRecord response has no valid CID: %w", model.ErrorRecordWriteFailed, err)
	}
	span.SetAttributes(attribute.String("atproto.uri", uri.String()))
	return model.RecordRef{URI: model.ATURI(uri.String()), RecordKey: model.RecordKey(uri.RecordKey()), CID: model.CID(out.CID)}, nil
}
