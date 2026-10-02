package atproto_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

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

// bobDID is a second well-formed example DID, for accounts a test adds itself.
const bobDID = "did:plc:bobbobbobbobbobbobbobbob"

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
		is.Equal(t, model.DID(atprototest.AliceDID), flow.DID)
		is.Equal(t, model.Handle("alice.test"), flow.Handle)
		is.Equal(t, "pds.test", flow.PDSHost)
		is.Equal(t, "auth.test", flow.AuthServerHost)

		r, err := h.db.GetOAuthAuthRequest(t.Context(), flow.State)
		is.NotError(t, err)
		is.Equal(t, model.DID(atprototest.AliceDID), r.AccountDID)
		is.Equal(t, h.net.AuthServerURL, r.AuthServerURL.String())
		is.EqualSlice(t, h.client.RequestedScopes(), r.Scopes)

		for _, name := range []string{"identity.lookup", "oauth.discover_auth_server", "oauth.pushed_authorization_request"} {
			is.True(t, h.hasSpan(name), "no child span "+name)
		}
	})

	t.Run("should accept a DID", func(t *testing.T) {
		h := newHarness(t)

		flow, err := h.client.StartAuthFlow(t.Context(), atprototest.AliceDID)
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
			DID:      bobDID,
			Handle:   "bob.test",
			Services: map[string]identity.ServiceEndpoint{"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://nowhere.test"}},
		})

		flow, err := h.client.StartAuthFlow(t.Context(), "bob.test")
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.Equal(t, model.DID(bobDID), flow.DID)
		is.Equal(t, "nowhere.test", flow.PDSHost)
	})
}

func TestClient_ProcessCallback(t *testing.T) {
	t.Run("should exchange the code and persist the session with the granted scopes", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		sess, err := h.client.ProcessCallback(t.Context(), callbackOf(h.net.Authorize(t, flow.RedirectURL)), flow.State)
		is.NotError(t, err)
		is.Equal(t, model.DID(atprototest.AliceDID), sess.DID)
		is.Equal(t, model.OAuthSessionID(flow.State), sess.SessionID)
		is.Equal(t, h.net.PDSURL, sess.HostURL.String())
		is.Equal(t, h.net.AuthServerURL, sess.AuthServerURL.String())
		is.EqualSlice(t, h.client.RequestedScopes(), sess.Scopes)

		_, err = h.db.GetOAuthSession(t.Context(), atprototest.AliceDID, sess.SessionID)
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

		sess, err := h.client.ProcessCallback(t.Context(), callbackOf(h.net.Authorize(t, flow.RedirectURL)), flow.State)
		is.NotError(t, err)
		is.EqualSlice(t, []string{"atproto", "blob:audio/*"}, sess.Scopes)
	})

	t.Run("should refuse a denial, recording its code on the span, and spend the auth request", func(t *testing.T) {
		h := newHarness(t)
		h.net.Deny = true
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		ctx, span := otel.Tracer("test").Start(t.Context(), "request")
		_, err = h.client.ProcessCallback(ctx, callbackOf(h.net.Authorize(t, flow.RedirectURL)), flow.State)
		span.End()
		is.Error(t, model.ErrorLoginCancelled, err)
		is.True(t, strings.Contains(err.Error(), "the user said no ("+h.net.AuthServerURL+"/errors/access_denied)"), err.Error())
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.String("oauth.callback_error", "access_denied")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, h.count(t, "oauth_auth_requests"))
	})

	t.Run("should refuse a callback for a flow the user did not start", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		_, err = h.client.ProcessCallback(t.Context(), callbackOf(h.net.Authorize(t, flow.RedirectURL)), "another-flow")
		is.Error(t, model.ErrorLoginCancelled, err)
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback with an unknown state, and one without a state", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.ProcessCallback(t.Context(), model.OAuthCallback{State: "nope", Code: "c", Issuer: h.net.AuthServerURL}, "nope")
		is.Error(t, model.ErrorLoginCancelled, err)
		_, err = h.client.ProcessCallback(t.Context(), model.OAuthCallback{}, "")
		is.Error(t, model.ErrorLoginCancelled, err)
	})

	t.Run("should refuse a callback without a code, and one from another auth server", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)
		callback := callbackOf(h.net.Authorize(t, flow.RedirectURL))

		noCode := callback
		noCode.Code = ""
		_, err = h.client.ProcessCallback(t.Context(), noCode, flow.State)
		is.Error(t, model.ErrorLoginCancelled, err)

		otherIssuer := callback
		otherIssuer.Issuer = "https://evil.test"
		_, err = h.client.ProcessCallback(t.Context(), otherIssuer, flow.State)
		is.Error(t, model.ErrorLoginCancelled, err)
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback whose code the auth server rejects", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)
		callback := callbackOf(h.net.Authorize(t, flow.RedirectURL))
		callback.Code = "forged"

		_, err = h.client.ProcessCallback(t.Context(), callback, flow.State)
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, h.count(t, "oauth_auth_requests"))
	})
}

func TestClient_GetRecord(t *testing.T) {
	t.Run("should read a record as the account, and report whether it exists", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		_, exists, err := h.client.GetRecord(t.Context(), did, sessionID, model.CollectionActorProfile, "self")
		is.NotError(t, err)
		is.True(t, !exists, "record exists before it was written")
		is.True(t, h.hasSpan("com.atproto.repo.getRecord"), "no child span")

		_, err = h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile, "createdAt": "2026-09-21T00:00:00.000Z"})
		is.NotError(t, err)

		record, exists, err := h.client.GetRecord(t.Context(), did, sessionID, model.CollectionActorProfile, "self")
		is.NotError(t, err)
		is.True(t, exists)
		is.Equal(t, model.CollectionActorProfile, record["$type"])
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, _, err := h.client.GetRecord(t.Context(), atprototest.AliceDID, "nope", model.CollectionActorProfile, "self")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_PutRecordIfMissing(t *testing.T) {
	t.Run("should write a record as the account only when it is missing", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		created, err := h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile, "createdAt": "2026-09-21T00:00:00.000Z"})
		is.NotError(t, err)
		is.True(t, created)
		is.True(t, h.hasSpan("com.atproto.repo.putRecord"), "no child span")

		created, err = h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile, "createdAt": "2026-09-22T00:00:00.000Z"})
		is.NotError(t, err)
		is.True(t, !created)
		record, _ := h.net.GetRecord(atprototest.AliceDID, model.CollectionActorProfile, "self")
		is.Equal(t, "2026-09-21T00:00:00.000Z", record["createdAt"])
	})

	t.Run("should report a failed write", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.PutRecordFails = true

		_, err := h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.PutRecordIfMissing(t.Context(), atprototest.AliceDID, "nope", model.CollectionActorProfile, "self", map[string]any{"$type": model.CollectionActorProfile})
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_CheckSession(t *testing.T) {
	t.Run("should pass for a session in the store", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		is.NotError(t, h.client.CheckSession(t.Context(), did, sessionID))
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		err := h.client.CheckSession(t.Context(), atprototest.AliceDID, "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_Logout(t *testing.T) {
	t.Run("should revoke and delete one session and leave the other", func(t *testing.T) {
		h := newHarness(t)
		h.login(t)
		h.login(t)
		firstData, err := h.db.GetOAuthSession(t.Context(), atprototest.AliceDID, h.sessionIDs[0])
		is.NotError(t, err)

		ctx, span := otel.Tracer("test").Start(t.Context(), "request")
		is.NotError(t, h.client.Logout(ctx, atprototest.AliceDID, h.sessionIDs[0]))
		span.End()

		is.EqualSlice(t, []string{firstData.AccessToken, firstData.RefreshToken}, h.net.Revoked())
		is.True(t, h.hasSpan("oauth.revoke"), "no child span")
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.Bool("oauth.revoked", true)))

		_, err = h.db.GetOAuthSession(t.Context(), atprototest.AliceDID, h.sessionIDs[0])
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		_, err = h.db.GetOAuthSession(t.Context(), atprototest.AliceDID, h.sessionIDs[1])
		is.NotError(t, err)
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		err := h.client.Logout(t.Context(), atprototest.AliceDID, "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})

	t.Run("should delete the session when the revocation uses up the deadline", func(t *testing.T) {
		h := newHarness(t)
		h.login(t)
		h.net.Stall = true

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		ctx, span := otel.Tracer("test").Start(ctx, "request")
		is.NotError(t, h.client.Logout(ctx, atprototest.AliceDID, h.sessionIDs[0]))
		span.End()

		is.True(t, ctx.Err() != nil, "the deadline did not pass")
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.Bool("oauth.revoked", false)))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should delete the session when the context is already cancelled", func(t *testing.T) {
		h := newHarness(t)
		h.login(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		is.NotError(t, h.client.Logout(ctx, atprototest.AliceDID, h.sessionIDs[0]))

		is.Equal(t, 0, len(h.net.Revoked()))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})
}

func TestClient_DeleteSession(t *testing.T) {
	t.Run("should delete one session and leave the other, without revoking", func(t *testing.T) {
		h := newHarness(t)
		h.login(t)
		h.login(t)

		is.NotError(t, h.client.DeleteSession(t.Context(), atprototest.AliceDID, h.sessionIDs[0]))

		_, err := h.db.GetOAuthSession(t.Context(), atprototest.AliceDID, h.sessionIDs[0])
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		is.Equal(t, 1, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, len(h.net.Revoked()))
	})

	t.Run("should not error for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		is.NotError(t, h.client.DeleteSession(t.Context(), atprototest.AliceDID, "nope"))
	})
}

func TestClient_ResolveHandle(t *testing.T) {
	t.Run("should resolve the handle of a known DID", func(t *testing.T) {
		h := newHarness(t)

		handle, err := h.client.ResolveHandle(t.Context(), atprototest.AliceDID)
		is.NotError(t, err)
		is.Equal(t, model.Handle("alice.test"), handle)
	})

	t.Run("should resolve handle.invalid for an account whose handle does not verify", func(t *testing.T) {
		h := newHarness(t)
		h.net.Directory.Insert(identity.Identity{DID: bobDID, Handle: syntax.HandleInvalid})

		handle, err := h.client.ResolveHandle(t.Context(), bobDID)
		is.NotError(t, err)
		is.Equal(t, model.HandleInvalid, handle)
	})

	t.Run("should error for an unknown DID", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.ResolveHandle(t.Context(), "did:plc:nobodynobodynobodynobody")
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
	sessionIDs []model.OAuthSessionID
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		sr:  oteltest.NewSpanRecorder(t),
		net: atprototest.NewNetwork(t),
		db:  sqlitetest.NewDatabase(t),
	}
	h.net.AddAccount(atprototest.AliceDID, "alice.test")
	h.client = h.net.NewClient(t, h.db)
	return h
}

// login all the way as alice, remembering the session's ID, and return the DID and session ID.
func (h *harness) login(t *testing.T) (model.DID, model.OAuthSessionID) {
	t.Helper()

	flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
	is.NotError(t, err)
	oauthSession, err := h.client.ProcessCallback(t.Context(), callbackOf(h.net.Authorize(t, flow.RedirectURL)), flow.State)
	is.NotError(t, err)
	h.sessionIDs = append(h.sessionIDs, oauthSession.SessionID)
	return oauthSession.DID, oauthSession.SessionID
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

// callbackOf the query the auth server sends to the callback URL, read as the callback handler reads it.
func callbackOf(query url.Values) model.OAuthCallback {
	return model.OAuthCallback{
		State:            model.OAuthState(query.Get("state")),
		Code:             query.Get("code"),
		Issuer:           query.Get("iss"),
		Error:            query.Get("error"),
		ErrorDescription: query.Get("error_description"),
		ErrorURI:         query.Get("error_uri"),
	}
}
