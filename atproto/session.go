package atproto

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

// session of one account on one device, resumed from the store, for calling the account's PDS as the
// account. Token refreshes and DPoP nonce rotations happen behind the calls and are persisted.
type session struct {
	client *Client
	sess   *oauth.ClientSession
	api    *atclient.APIClient
}

func (s *session) DID() model.DID {
	return model.DID(s.sess.Data.AccountDID)
}

// GetRecord from the account's repository, and whether it exists.
func (s *session) GetRecord(ctx context.Context, collection, rkey string) (record map[string]any, exists bool, err error) {
	ctx, span := s.client.tracer.Start(ctx, "com.atproto.repo.getRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.HostURL)), attribute.String("atproto.did", s.DID().String()), attribute.String("atproto.collection", collection)))
	defer func() { endSpan(span, err) }()

	var out struct {
		Value map[string]any `json:"value"`
	}
	params := map[string]any{"repo": s.DID().String(), "collection": collection, "rkey": rkey}
	err = s.api.Get(ctx, "com.atproto.repo.getRecord", params, &out)
	if err == nil {
		span.SetAttributes(attribute.Bool("atproto.record_found", true))
		return out.Value, true, nil
	}
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) && apiErr.Name == "RecordNotFound" {
		span.SetAttributes(attribute.Bool("atproto.record_found", false))
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("getting record %v/%v: %w", collection, rkey, err)
}

// PutRecordIfMissing in the account's repository, reporting whether this call created it: a record
// that appeared in the meantime is left alone, which is not an error. The write has a null swapRecord,
// which makes it conditional on the record's absence, so two writers racing past a read cannot
// overwrite each other's record.
func (s *session) PutRecordIfMissing(ctx context.Context, collection, rkey string, record map[string]any) (created bool, err error) {
	ctx, span := s.client.tracer.Start(ctx, "com.atproto.repo.putRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.HostURL)), attribute.String("atproto.did", s.DID().String()), attribute.String("atproto.collection", collection)))
	defer func() { endSpan(span, err) }()

	body := map[string]any{"repo": s.DID().String(), "collection": collection, "rkey": rkey, "record": record, "swapRecord": nil}
	err = s.api.Post(ctx, "com.atproto.repo.putRecord", body, nil)
	if err == nil {
		return true, nil
	}
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) && apiErr.Name == "InvalidSwap" {
		span.SetAttributes(attribute.Bool("atproto.record_found", true))
		return false, nil
	}
	return false, fmt.Errorf("putting record %v/%v: %w", collection, rkey, err)
}

// Revoke the session's tokens at the auth server. An auth server without revocation is not an error.
func (s *session) Revoke(ctx context.Context) (err error) {
	if s.sess.Data.AuthServerRevocationEndpoint == "" {
		return nil
	}

	ctx, span := s.client.tracer.Start(ctx, "oauth.revoke", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(s.sess.Data.AuthServerURL))))
	defer func() { endSpan(span, err) }()

	return s.sess.RevokeSession(ctx)
}
