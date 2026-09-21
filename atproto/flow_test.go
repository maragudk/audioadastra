package atproto_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"maragu.dev/glue/oteltest"
	"maragu.dev/is"

	"app/atproto"
	"app/atprototest"
	"app/model"
	"app/sqlite"
	"app/sqlitetest"
)

func TestClient_StartAuthFlow(t *testing.T) {
	t.Run("should push the auth request and return the flow with what it learned about the account", func(t *testing.T) {
		h := newHarness(t)

		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		u, err := url.Parse(flow.RedirectURL)
		is.NotError(t, err)
		is.Equal(t, h.net.AuthServerURL+"/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
		is.Equal(t, h.client.ClientID(), u.Query().Get("client_id"))
		is.True(t, u.Query().Get("request_uri") != "")
		is.Equal(t, model.DID("did:plc:alice"), flow.DID)
		is.Equal(t, model.Handle("alice.test"), flow.Handle)
		is.Equal(t, "pds.test", flow.PDSHost)
		is.Equal(t, "auth.test", flow.AuthServerHost)

		r, err := h.db.GetOAuthAuthRequest(t.Context(), flow.State)
		is.NotError(t, err)
		is.Equal(t, model.DID("did:plc:alice"), r.AccountDID)
		is.Equal(t, h.net.AuthServerURL, r.AuthServerURL)
		is.EqualSlice(t, h.client.RequestedScopes(), r.Scopes)

		for _, name := range []string{"identity.lookup", "oauth.discover_auth_server", "oauth.pushed_authorization_request"} {
			is.True(t, h.hasSpan(name), "no child span "+name)
		}
	})

	t.Run("should accept a DID", func(t *testing.T) {
		h := newHarness(t)

		flow, err := h.client.StartAuthFlow(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(flow.RedirectURL, h.net.AuthServerURL+"/oauth/authorize?"))
	})

	t.Run("should refuse an identifier that is neither a handle nor a DID", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.StartAuthFlow(t.Context(), "not a handle")
		is.Error(t, model.ErrorIdentityUnresolved, err)
	})

	t.Run("should refuse a handle that does not resolve", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.StartAuthFlow(t.Context(), "nobody.test")
		is.Error(t, model.ErrorIdentityUnresolved, err)
	})

	t.Run("should refuse an account whose PDS does not serve auth server discovery, keeping the account", func(t *testing.T) {
		h := newHarness(t)
		h.net.Directory.Insert(identity.Identity{
			DID:      "did:plc:bob",
			Handle:   "bob.test",
			Services: map[string]identity.ServiceEndpoint{"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://nowhere.test"}},
		})

		flow, err := h.client.StartAuthFlow(t.Context(), "bob.test")
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.Equal(t, model.DID("did:plc:bob"), flow.DID)
		is.Equal(t, "nowhere.test", flow.PDSHost)
	})
}

func TestClient_ProcessCallback(t *testing.T) {
	t.Run("should exchange the code and persist the session with the granted scopes", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		sess, err := h.client.ProcessCallback(t.Context(), h.net.Authorize(t, flow.RedirectURL), flow.State)
		is.NotError(t, err)
		is.Equal(t, model.DID("did:plc:alice"), sess.DID)
		is.Equal(t, flow.State, sess.SessionID)
		is.Equal(t, h.net.PDSURL, sess.HostURL)
		is.Equal(t, h.net.AuthServerURL, sess.AuthServerURL)
		is.EqualSlice(t, h.client.RequestedScopes(), sess.Scopes)

		_, err = h.db.GetOAuthSession(t.Context(), "did:plc:alice", sess.SessionID)
		is.NotError(t, err)
		_, err = h.db.GetOAuthAuthRequest(t.Context(), flow.State)
		is.Error(t, model.ErrorOAuthAuthRequestNotFound, err)
		is.True(t, h.hasSpan("oauth.token_exchange"), "no child span")
	})

	t.Run("should return what the auth server granted", func(t *testing.T) {
		h := newHarness(t)
		h.net.GrantScopes = "atproto blob:audio/*"
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		sess, err := h.client.ProcessCallback(t.Context(), h.net.Authorize(t, flow.RedirectURL), flow.State)
		is.NotError(t, err)
		is.EqualSlice(t, []string{"atproto", "blob:audio/*"}, sess.Scopes)
	})

	t.Run("should refuse a denial, recording its code on the span, and spend the auth request", func(t *testing.T) {
		h := newHarness(t)
		h.net.Deny = true
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		ctx, span := otel.Tracer("test").Start(t.Context(), "request")
		_, err = h.client.ProcessCallback(ctx, h.net.Authorize(t, flow.RedirectURL), flow.State)
		span.End()
		is.Error(t, model.ErrorLoginCancelled, err)
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.String("oauth.callback_error", "access_denied")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, h.count(t, "oauth_auth_requests"))
	})

	t.Run("should refuse a callback for a flow the user did not start", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		_, err = h.client.ProcessCallback(t.Context(), h.net.Authorize(t, flow.RedirectURL), "another-flow")
		is.Error(t, model.ErrorLoginCancelled, err)
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback with an unknown state, and one without a state", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.ProcessCallback(t.Context(), url.Values{"state": {"nope"}, "code": {"c"}, "iss": {h.net.AuthServerURL}}, "nope")
		is.Error(t, model.ErrorLoginCancelled, err)
		_, err = h.client.ProcessCallback(t.Context(), url.Values{}, "")
		is.Error(t, model.ErrorLoginCancelled, err)
	})

	t.Run("should refuse a callback without a code, and one from another auth server", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)
		params := h.net.Authorize(t, flow.RedirectURL)

		noCode := url.Values{"state": {params.Get("state")}, "iss": {params.Get("iss")}}
		_, err = h.client.ProcessCallback(t.Context(), noCode, flow.State)
		is.Error(t, model.ErrorLoginCancelled, err)

		otherIssuer := url.Values{"state": {params.Get("state")}, "code": {params.Get("code")}, "iss": {"https://evil.test"}}
		_, err = h.client.ProcessCallback(t.Context(), otherIssuer, flow.State)
		is.Error(t, model.ErrorLoginCancelled, err)
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback whose code the auth server rejects", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)
		params := h.net.Authorize(t, flow.RedirectURL)
		params.Set("code", "forged")

		_, err = h.client.ProcessCallback(t.Context(), params, flow.State)
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, h.count(t, "oauth_auth_requests"))
	})
}

func TestClient_ResumeSession(t *testing.T) {
	t.Run("should read and write records as the account, writing only when missing", func(t *testing.T) {
		h := newHarness(t)
		sess := h.login(t)

		_, exists, err := sess.GetRecord(t.Context(), model.CollectionActorProfile, "self")
		is.NotError(t, err)
		is.True(t, !exists, "record exists before it was written")
		is.True(t, h.hasSpan("com.atproto.repo.getRecord"), "no child span")

		created, err := sess.PutRecordIfMissing(t.Context(), model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile, "createdAt": "2026-09-21T00:00:00.000Z"})
		is.NotError(t, err)
		is.True(t, created)
		is.True(t, h.hasSpan("com.atproto.repo.putRecord"), "no child span")

		record, exists, err := sess.GetRecord(t.Context(), model.CollectionActorProfile, "self")
		is.NotError(t, err)
		is.True(t, exists)
		is.Equal(t, model.CollectionActorProfile, record["$type"])

		created, err = sess.PutRecordIfMissing(t.Context(), model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile, "createdAt": "2026-09-22T00:00:00.000Z"})
		is.NotError(t, err)
		is.True(t, !created)
		record, _ = h.net.GetRecord("did:plc:alice", model.CollectionActorProfile, "self")
		is.Equal(t, "2026-09-21T00:00:00.000Z", record["createdAt"])
	})

	t.Run("should report a failed write", func(t *testing.T) {
		h := newHarness(t)
		sess := h.login(t)
		h.net.PutRecordFails = true

		_, err := sess.PutRecordIfMissing(t.Context(), model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.ResumeSession(t.Context(), "did:plc:alice", "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_Logout(t *testing.T) {
	t.Run("should revoke and delete one session and leave the other", func(t *testing.T) {
		h := newHarness(t)
		first := h.login(t)
		second := h.login(t)
		firstData, err := h.db.GetOAuthSession(t.Context(), "did:plc:alice", h.sessionIDs[0])
		is.NotError(t, err)

		ctx, span := otel.Tracer("test").Start(t.Context(), "request")
		is.NotError(t, h.client.Logout(ctx, first.DID(), h.sessionIDs[0]))
		span.End()

		is.EqualSlice(t, []string{firstData.AccessToken, firstData.RefreshToken}, h.net.Revoked())
		is.True(t, h.hasSpan("oauth.revoke"), "no child span")
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.Bool("oauth.revoked", true)))

		_, err = h.db.GetOAuthSession(t.Context(), "did:plc:alice", h.sessionIDs[0])
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		_, err = h.db.GetOAuthSession(t.Context(), second.DID(), h.sessionIDs[1])
		is.NotError(t, err)
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		err := h.client.Logout(t.Context(), "did:plc:alice", "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_ResolveHandle(t *testing.T) {
	t.Run("should resolve the handle of a known DID", func(t *testing.T) {
		h := newHarness(t)

		handle, err := h.client.ResolveHandle(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.Equal(t, model.Handle("alice.test"), handle)
	})

	t.Run("should resolve handle.invalid for an account whose handle does not verify", func(t *testing.T) {
		h := newHarness(t)
		h.net.Directory.Insert(identity.Identity{DID: "did:plc:bob", Handle: syntax.HandleInvalid})

		handle, err := h.client.ResolveHandle(t.Context(), "did:plc:bob")
		is.NotError(t, err)
		is.Equal(t, model.HandleInvalid, handle)
	})

	t.Run("should error for an unknown DID", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.ResolveHandle(t.Context(), "did:plc:nobody")
		is.True(t, err != nil, "expected an error")
	})
}

// harness is a client against the fake network with a store of its own, and a span recorder in place
// before the client is made so its tracer records into it.
type harness struct {
	sr         *tracetest.SpanRecorder
	net        *atprototest.Network
	db         *sqlite.Database
	client     *atproto.Client
	sessionIDs []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		sr:  oteltest.NewSpanRecorder(t),
		net: atprototest.NewNetwork(t),
		db:  sqlitetest.NewDatabase(t),
	}
	h.net.AddAccount("did:plc:alice", "alice.test")
	h.client = h.net.NewClient(t, h.db)
	return h
}

// login all the way as alice and resume the session, remembering its ID.
func (h *harness) login(t *testing.T) atproto.Session {
	t.Helper()

	flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
	is.NotError(t, err)
	oauthSession, err := h.client.ProcessCallback(t.Context(), h.net.Authorize(t, flow.RedirectURL), flow.State)
	is.NotError(t, err)
	h.sessionIDs = append(h.sessionIDs, oauthSession.SessionID)

	sess, err := h.client.ResumeSession(t.Context(), oauthSession.DID, oauthSession.SessionID)
	is.NotError(t, err)
	return sess
}

func (h *harness) hasSpan(name string) bool {
	for _, span := range h.sr.Ended() {
		if span.Name() == name {
			return true
		}
	}
	return false
}

func (h *harness) spanAttributes(t *testing.T, name string) []attribute.KeyValue {
	t.Helper()

	for _, span := range h.sr.Ended() {
		if span.Name() == name {
			return span.Attributes()
		}
	}
	t.Fatal("no span " + name)
	return nil
}

func (h *harness) count(t *testing.T, table string) int {
	t.Helper()

	var count int
	is.NotError(t, h.db.H.Get(t.Context(), &count, `select count(*) from `+table))
	return count
}
