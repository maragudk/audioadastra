package atproto_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
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

func TestNewClient(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	is.NotError(t, err)

	t.Run("should build a localhost client for the real network without a key", func(t *testing.T) {
		c, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("http://localhost:8080"), Store: sqlitetest.NewDatabase(t)})
		is.NotError(t, err)
		is.True(t, !c.Local())
		is.True(t, !c.Confidential())
		is.True(t, strings.HasPrefix(c.ClientID(), "http://localhost?"), c.ClientID())
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", c.CallbackURL())
	})

	t.Run("should serve valid client metadata with the key in the JWKS", func(t *testing.T) {
		c, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("https://app.example.com"), PrivateKeyMultibase: key.Multibase(), KeyID: "k1", Store: sqlitetest.NewDatabase(t)})
		is.NotError(t, err)

		meta, ok := c.ClientMetadata().(oauth.ClientMetadata)
		is.True(t, ok, "not a client metadata document")
		is.NotError(t, meta.Validate(c.ClientID()))
		is.Equal(t, "https://app.example.com", *meta.ClientURI)
		is.Equal(t, "https://app.example.com/oauth/jwks.json", *meta.JWKSURI)

		jwks, ok := c.JWKS().(oauth.JWKS)
		is.True(t, ok, "not a JWKS")
		is.Equal(t, 1, len(jwks.Keys))
		is.Equal(t, "k1", *jwks.Keys[0].KeyID)
	})

	t.Run("should derive slash-free URLs from a base URL with a trailing slash", func(t *testing.T) {
		c, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("https://app.example.com/"), PrivateKeyMultibase: key.Multibase(), KeyID: "k1", Store: sqlitetest.NewDatabase(t)})
		is.NotError(t, err)
		is.Equal(t, "https://app.example.com/oauth/client-metadata.json", c.ClientID())
		is.Equal(t, "https://app.example.com/oauth/callback", c.CallbackURL())

		meta, ok := c.ClientMetadata().(oauth.ClientMetadata)
		is.True(t, ok, "not a client metadata document")
		is.Equal(t, "https://app.example.com", *meta.ClientURI)
		is.Equal(t, "https://app.example.com/oauth/jwks.json", *meta.JWKSURI)
		is.EqualSlice(t, []string{"https://app.example.com/oauth/callback"}, meta.RedirectURIs)
	})

	t.Run("should check granted scopes against the requested ones as permissions", func(t *testing.T) {
		c, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("http://localhost:8080"), Store: sqlitetest.NewDatabase(t)})
		is.NotError(t, err)

		is.NotError(t, c.CheckScopes(c.RequestedScopes()))
		is.NotError(t, c.CheckScopes([]string{"atproto", "repo?collection=com.audioadastra.actor.profile", "blob?accept=audio/*", "blob?accept=image/*"}))
		is.Error(t, model.ErrorScopeDenied, c.CheckScopes([]string{"atproto", "blob:audio/*"}))
		is.Error(t, model.ErrorScopeDenied, c.CheckScopes(c.RequestedScopes()[1:]))
	})

	t.Run("should compare scopes by what they cover", func(t *testing.T) {
		c, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("http://localhost:8080"), Store: sqlitetest.NewDatabase(t)})
		is.NotError(t, err)

		tests := []struct {
			name    string
			granted []string
			ok      bool
		}{
			{name: "both blob types in one permission", granted: []string{"atproto", "repo:com.audioadastra.actor.profile", "blob?accept=audio/*&accept=image/*"}, ok: true},
			{name: "the profile collection with all three actions listed", granted: []string{"atproto", "repo:com.audioadastra.actor.profile?action=create&action=update&action=delete", "blob:audio/*", "blob:image/*"}, ok: true},
			{name: "the actions spread over two permissions", granted: []string{"atproto", "repo:com.audioadastra.actor.profile?action=create", "repo:com.audioadastra.actor.profile?action=update&action=delete", "blob:audio/*", "blob:image/*"}, ok: true},
			{name: "every collection and every blob type", granted: []string{"atproto", "repo:*", "blob:*/*"}, ok: true},
			{name: "audio blobs only", granted: []string{"atproto", "repo:com.audioadastra.actor.profile", "blob:audio/*"}, ok: false},
			{name: "a single audio subtype for the audio pattern", granted: []string{"atproto", "repo:com.audioadastra.actor.profile", "blob:audio/mpeg", "blob:image/*"}, ok: false},
			{name: "the profile collection for creating only", granted: []string{"atproto", "repo:com.audioadastra.actor.profile?action=create", "blob:audio/*", "blob:image/*"}, ok: false},
			{name: "another collection", granted: []string{"atproto", "repo:com.example.other", "blob:audio/*", "blob:image/*"}, ok: false},
			{name: "everything but atproto", granted: []string{"repo:*", "blob:*/*"}, ok: false},
		}

		for _, test := range tests {
			err := c.CheckScopes(test.granted)
			if test.ok {
				is.NotError(t, err, test.name)
			} else {
				is.Error(t, model.ErrorScopeDenied, err, test.name)
			}
		}
	})

	t.Run("should refuse a public base URL without a key", func(t *testing.T) {
		_, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("https://app.example.com"), Store: sqlitetest.NewDatabase(t)})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a missing store", func(t *testing.T) {
		_, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("http://localhost:8080")})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should build a confidential client for a local network when a PLC URL is given", func(t *testing.T) {
		c, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("https://app.example.com"), PrivateKeyMultibase: key.Multibase(), KeyID: "k1", Store: sqlitetest.NewDatabase(t), PLCURL: mustParseURL("http://localhost:2582"), LocalHandleSuffix: ".test"})
		is.NotError(t, err)
		is.True(t, c.Local())
		is.True(t, c.Confidential())
		is.Equal(t, "https://app.example.com/oauth/client-metadata.json", c.ClientID())
		is.EqualSlice(t, []string{"atproto", "repo:com.audioadastra.actor.profile", "blob:audio/*", "blob:image/*"}, c.RequestedScopes())
	})

	t.Run("should refuse a CA file that does not exist", func(t *testing.T) {
		_, err := atproto.NewClient(atproto.NewClientOptions{BaseURL: mustParseURL("http://localhost:8080"), Store: sqlitetest.NewDatabase(t), PLCURL: mustParseURL("http://localhost:2582"), CAFile: "nope.crt"})
		is.True(t, err != nil, "expected an error")
	})
}

func TestNewLocalHTTPClient(t *testing.T) {
	t.Run("should dial hosts under .localhost and the handle suffix on loopback", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(r.Host))
		}))
		t.Cleanup(server.Close)
		_, port, err := net.SplitHostPort(server.Listener.Addr().String())
		is.NotError(t, err)

		client, err := atproto.NewLocalHTTPClient(atproto.NewLocalHTTPClientOptions{LocalHandleSuffix: ".test"})
		is.NotError(t, err)

		// No resolver answers for the .test name, and only some do for the .localhost one, so a response
		// means the client did not ask. The request keeps the name it was made for.
		for _, host := range []string{"no-such-name.localhost", "no-such-name.test"} {
			res, err := client.Get("http://" + net.JoinHostPort(host, port))
			is.NotError(t, err, host)
			body, err := io.ReadAll(res.Body)
			is.NotError(t, err, host)
			_ = res.Body.Close()
			is.Equal(t, net.JoinHostPort(host, port), string(body))
		}
	})

	t.Run("should error when the CA file holds no certificate", func(t *testing.T) {
		_, err := atproto.NewLocalHTTPClient(atproto.NewLocalHTTPClientOptions{CAFile: "client_test.go"})
		is.True(t, err != nil, "expected an error")
	})
}

func TestNewOAuthClientConfig(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	is.NotError(t, err)

	t.Run("should give a localhost client with a 127.0.0.1 callback for a localhost base URL", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("http://localhost:8080")})
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(config.ClientID, "http://localhost?"), config.ClientID)
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", config.CallbackURL)
		is.True(t, !config.IsConfidential())
	})

	t.Run("should give a localhost client for a 127.0.0.1 base URL, ignoring any key", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("http://127.0.0.1:8080/"), PrivateKeyMultibase: key.Multibase(), KeyID: "k1"})
		is.NotError(t, err)
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", config.CallbackURL)
		is.True(t, !config.IsConfidential())
	})

	t.Run("should give a confidential client for a public base URL with a key", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("https://app.example.com"), PrivateKeyMultibase: key.Multibase(), KeyID: "k1"})
		is.NotError(t, err)
		is.Equal(t, "https://app.example.com/oauth/client-metadata.json", config.ClientID)
		is.Equal(t, "https://app.example.com/oauth/callback", config.CallbackURL)
		is.True(t, config.IsConfidential())
		is.Equal(t, "k1", *config.KeyID)
	})

	t.Run("should request the atproto scope, the profile collection and both blob types, all parseable", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("http://localhost:8080")})
		is.NotError(t, err)
		is.EqualSlice(t, []string{"atproto", "repo:com.audioadastra.actor.profile", "blob:audio/*", "blob:image/*"}, config.Scopes)

		// Every scope must parse as a permission, or a granted-versus-requested check built on parsed
		// permissions could pass vacuously.
		for _, scope := range config.Scopes[1:] {
			_, err := auth.ParsePermissionString(scope)
			is.NotError(t, err, scope)
		}
	})

	t.Run("should refuse a public base URL without a key", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("https://app.example.com")})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a public base URL with a key but no key ID", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("https://app.example.com"), PrivateKeyMultibase: key.Multibase()})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a key that is not a P-256 private key", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("https://app.example.com"), PrivateKeyMultibase: "znope", KeyID: "k1"})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a base URL without a host, and a missing one", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: mustParseURL("nope")})
		is.True(t, err != nil, "expected an error")
		_, err = atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{})
		is.True(t, err != nil, "expected an error")
	})
}

// bobDID is a second well-formed example DID, for accounts a test adds itself.
const bobDID = "did:plc:bobbobbobbobbobbobbobbob"

func TestClient_StartAuthFlow(t *testing.T) {
	t.Run("should push the auth request and return the flow with what it learned about the account", func(t *testing.T) {
		h := newHarness(t)

		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		u := flow.RedirectURL
		is.Equal(t, h.net.AuthServerURL+"/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
		is.Equal(t, h.client.ClientID(), u.Query().Get("client_id"))
		is.True(t, u.Query().Get("request_uri") != "")
		is.Equal(t, model.DID(atprototest.AliceDID), flow.DID)
		is.Equal(t, model.Handle("alice.test"), flow.Handle)
		is.Equal(t, h.net.PDSURL, flow.PDSURL.String())
		is.Equal(t, h.net.AuthServerURL, flow.AuthServerURL.String())

		r, err := h.db.GetOAuthAuthRequest(t.Context(), flow.State)
		is.NotError(t, err)
		is.Equal(t, model.DID(atprototest.AliceDID), r.AccountDID)
		is.Equal(t, h.net.AuthServerURL, r.AuthServerURL.String())
		is.EqualSlice(t, h.client.RequestedScopes(), r.Scopes)

		for _, name := range []string{"identity.lookup", "oauth.discover_auth_server", "oauth.pushed_authorization_request"} {
			is.True(t, h.hasSpan(name), "no child span "+name)
		}
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "identity.lookup"), attribute.String("atproto.identifier", "alice.test")))
	})

	t.Run("should accept a DID", func(t *testing.T) {
		h := newHarness(t)

		flow, err := h.client.StartAuthFlow(t.Context(), atprototest.AliceDID)
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(flow.RedirectURL.String(), h.net.AuthServerURL+"/oauth/authorize?"))
	})

	t.Run("should tell an identifier that is at fault from a lookup that failed", func(t *testing.T) {
		tests := []struct {
			err      error
			expected error
		}{
			{err: identity.ErrHandleNotFound, expected: model.ErrorIdentityUnresolved},
			{err: identity.ErrDIDNotFound, expected: model.ErrorIdentityUnresolved},
			{err: identity.ErrInvalidHandle, expected: model.ErrorIdentityUnresolved},
			{err: identity.ErrHandleMismatch, expected: model.ErrorIdentityUnresolved},
			{err: identity.ErrHandleNotDeclared, expected: model.ErrorIdentityUnresolved},
			{err: identity.ErrHandleReservedTLD, expected: model.ErrorIdentityUnresolved},
			{err: identity.ErrHandleResolutionFailed, expected: model.ErrorIdentityUnavailable},
			{err: identity.ErrDIDResolutionFailed, expected: model.ErrorIdentityUnavailable},
			{err: context.DeadlineExceeded, expected: model.ErrorIdentityUnavailable},
			{err: context.Canceled, expected: model.ErrorIdentityUnavailable},
			{err: errors.New("something unexpected"), expected: model.ErrorIdentityUnavailable},
		}

		for _, test := range tests {
			c, err := atproto.NewClient(atproto.NewClientOptions{
				BaseURL:   mustParseURL("http://localhost:8080"),
				Store:     sqlitetest.NewDatabase(t),
				Directory: failingDirectory{err: fmt.Errorf("looking up: %w", test.err)},
			})
			is.NotError(t, err)

			_, err = c.StartAuthFlow(t.Context(), "alice.test")
			is.Error(t, test.expected, err, test.err.Error())
			is.Error(t, test.err, err, test.err.Error())
		}
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
		is.Equal(t, "https://nowhere.test", flow.PDSURL.String())
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
		is.True(t, oteltest.HasAttribute(h.spanAttributes(t, "request"), attribute.String("login.callback_reason", "denied")))
		is.True(t, !h.hasSpan("oauth.token_exchange"), "a denial opened a token exchange span")
	})

	t.Run("should refuse a callback for a flow the user did not start", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)

		is.Equal(t, "state_mismatch", h.refusalReason(t, "request", callbackOf(h.net.Authorize(t, flow.RedirectURL)), "another-flow"))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback with an unknown state, and one without a state", func(t *testing.T) {
		h := newHarness(t)

		is.Equal(t, "request_not_found", h.refusalReason(t, "unknown", model.OAuthCallback{State: "nope", Code: "c", Issuer: h.net.AuthServerURL}, "nope"))
		is.Equal(t, "state_mismatch", h.refusalReason(t, "missing", model.OAuthCallback{}, ""))
	})

	t.Run("should refuse a callback without a code, and one from another auth server", func(t *testing.T) {
		h := newHarness(t)
		flow, err := h.client.StartAuthFlow(t.Context(), "alice.test")
		is.NotError(t, err)
		callback := callbackOf(h.net.Authorize(t, flow.RedirectURL))

		noCode := callback
		noCode.Code = ""
		is.Equal(t, "no_code", h.refusalReason(t, "no code", noCode, flow.State))

		otherIssuer := callback
		otherIssuer.Issuer = "https://evil.test"
		is.Equal(t, "issuer_mismatch", h.refusalReason(t, "other issuer", otherIssuer, flow.State))
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

		_, exists, err := h.client.GetRecord(t.Context(), did, sessionID, model.CollectionActorProfile, model.RecordKeySelf)
		is.NotError(t, err)
		is.True(t, !exists, "record exists before it was written")
		is.True(t, h.hasSpan("com.atproto.repo.getRecord"), "no child span")

		_, err = h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, model.RecordKeySelf, map[string]any{"$type": model.CollectionActorProfile.String(), "createdAt": "2026-09-21T00:00:00.000Z"})
		is.NotError(t, err)

		record, exists, err := h.client.GetRecord(t.Context(), did, sessionID, model.CollectionActorProfile, model.RecordKeySelf)
		is.NotError(t, err)
		is.True(t, exists)
		is.Equal(t, any(model.CollectionActorProfile.String()), record["$type"])
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, _, err := h.client.GetRecord(t.Context(), atprototest.AliceDID, "nope", model.CollectionActorProfile, model.RecordKeySelf)
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestClient_PutRecordIfMissing(t *testing.T) {
	t.Run("should write a record as the account only when it is missing", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)

		created, err := h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, model.RecordKeySelf, map[string]any{"$type": model.CollectionActorProfile.String(), "createdAt": "2026-09-21T00:00:00.000Z"})
		is.NotError(t, err)
		is.True(t, created)
		is.True(t, h.hasSpan("com.atproto.repo.putRecord"), "no child span")

		created, err = h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, model.RecordKeySelf, map[string]any{"$type": model.CollectionActorProfile.String(), "createdAt": "2026-09-22T00:00:00.000Z"})
		is.NotError(t, err)
		is.True(t, !created)
		record, _ := h.net.GetRecord(atprototest.AliceDID, model.CollectionActorProfile, model.RecordKeySelf)
		is.Equal(t, "2026-09-21T00:00:00.000Z", record["createdAt"])
	})

	t.Run("should report a failed write", func(t *testing.T) {
		h := newHarness(t)
		did, sessionID := h.login(t)
		h.net.PutRecordFails = true

		_, err := h.client.PutRecordIfMissing(t.Context(), did, sessionID, model.CollectionActorProfile, model.RecordKeySelf, map[string]any{"$type": model.CollectionActorProfile.String()})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.client.PutRecordIfMissing(t.Context(), atprototest.AliceDID, "nope", model.CollectionActorProfile, model.RecordKeySelf, map[string]any{"$type": model.CollectionActorProfile.String()})
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

// mustParseURL for fixtures that are known to parse.
func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

// refusalReason of a callback the client refuses, processed inside a span of the given name.
func (h *harness) refusalReason(t *testing.T, name string, callback model.OAuthCallback, state model.OAuthState) string {
	t.Helper()

	ctx, span := otel.Tracer("test").Start(t.Context(), name)
	_, err := h.client.ProcessCallback(ctx, callback, state)
	span.End()
	is.Error(t, model.ErrorLoginCancelled, err)

	for _, attr := range h.spanAttributes(t, name) {
		if attr.Key == "login.callback_reason" {
			return attr.Value.AsString()
		}
	}
	return ""
}

// failingDirectory fails every lookup with the same error.
type failingDirectory struct {
	err error
}

func (d failingDirectory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	return nil, d.err
}

func (d failingDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	return nil, d.err
}

func (d failingDirectory) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	return nil, d.err
}

func (d failingDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return nil
}
