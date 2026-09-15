package service_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"maragu.dev/glue/oteltest"
	"maragu.dev/is"

	"app/atprototest"
	"app/lexicons"
	"app/model"
	"app/service"
	"app/servicetest"
	"app/sqlite"
	"app/sqlitetest"
)

func TestNewOAuthClientConfig(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	is.NotError(t, err)

	t.Run("should give a localhost client with a 127.0.0.1 callback for a localhost base URL", func(t *testing.T) {
		config, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "http://localhost:8080"})
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(config.ClientID, "http://localhost?"), config.ClientID)
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", config.CallbackURL)
		is.True(t, !config.IsConfidential())
		is.EqualSlice(t, service.OAuthScopes, config.Scopes)
	})

	t.Run("should give a localhost client for a 127.0.0.1 base URL, ignoring any key", func(t *testing.T) {
		config, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "http://127.0.0.1:8080/", PrivateKeyMultibase: key.Multibase(), KeyID: "k1"})
		is.NotError(t, err)
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", config.CallbackURL)
		is.True(t, !config.IsConfidential())
	})

	t.Run("should give a confidential client for a public base URL with a key", func(t *testing.T) {
		config, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: key.Multibase(), KeyID: "k1"})
		is.NotError(t, err)
		is.Equal(t, "https://app.example.com/oauth/client-metadata.json", config.ClientID)
		is.Equal(t, "https://app.example.com/oauth/callback", config.CallbackURL)
		is.True(t, config.IsConfidential())
		is.Equal(t, "k1", *config.KeyID)
	})

	t.Run("should refuse a public base URL without a key", func(t *testing.T) {
		_, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com"})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a public base URL with a key but no key ID", func(t *testing.T) {
		_, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: key.Multibase()})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a key that is not a P-256 private key", func(t *testing.T) {
		_, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: "znope", KeyID: "k1"})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a base URL without a host", func(t *testing.T) {
		_, err := service.NewOAuthClientConfig(service.NewOAuthClientConfigOptions{BaseURL: "nope"})
		is.True(t, err != nil, "expected an error")
	})
}

func TestOAuthScopes(t *testing.T) {
	t.Run("should all parse as permissions, so the scope check cannot pass vacuously", func(t *testing.T) {
		for _, scope := range service.OAuthScopes {
			if scope == "atproto" {
				continue
			}
			_, err := auth.ParsePermissionString(scope)
			is.NotError(t, err, scope)
		}
	})
}

func TestFat_StartLogin(t *testing.T) {
	t.Run("should redirect to the auth server for a handle and remember the auth request", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		start, err := h.fat.StartLogin(ctx, "alice.test")
		span.End()
		is.NotError(t, err)

		u, err := url.Parse(start.RedirectURL)
		is.NotError(t, err)
		is.Equal(t, h.net.AuthServerURL+"/oauth/authorize", u.Scheme+"://"+u.Host+u.Path)
		is.Equal(t, h.app.Config.ClientID, u.Query().Get("client_id"))
		is.True(t, u.Query().Get("request_uri") != "")

		info, err := h.db.GetAuthRequestInfo(t.Context(), start.State)
		is.NotError(t, err)
		is.Equal(t, "did:plc:alice", info.AccountDID.String())
		is.Equal(t, h.net.AuthServerURL, info.AuthServerURL)

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.did", "did:plc:alice")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.handle", "alice.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.pds_host", "pds.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.auth_server", "auth.test")))
		is.True(t, !oteltest.HasAttributeKey(attrs, "login.condition"))

		for _, name := range []string{"identity.lookup", "oauth.discover_auth_server", "oauth.pushed_authorization_request"} {
			is.True(t, h.hasSpan(name), "no child span "+name)
		}
	})

	t.Run("should accept a DID", func(t *testing.T) {
		h := newHarness(t)

		start, err := h.fat.StartLogin(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(start.RedirectURL, h.net.AuthServerURL+"/oauth/authorize?"))
	})

	t.Run("should refuse an identifier that is neither a handle nor a DID", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		_, err := h.fat.StartLogin(ctx, "not a handle")
		span.End()
		is.Error(t, model.ErrorIdentityUnresolved, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "identity_error")))
	})

	t.Run("should refuse a handle that does not resolve", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		_, err := h.fat.StartLogin(ctx, "nobody.test")
		span.End()
		is.Error(t, model.ErrorIdentityUnresolved, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "identity_error")))
	})

	t.Run("should refuse an account whose PDS does not serve auth server discovery", func(t *testing.T) {
		h := newHarness(t)
		h.net.Directory.Insert(identity.Identity{
			DID:      "did:plc:bob",
			Handle:   "bob.test",
			Services: map[string]identity.ServiceEndpoint{"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: "https://nowhere.test"}},
		})

		ctx, span := h.startSpan(t)
		_, err := h.fat.StartLogin(ctx, "bob.test")
		span.End()
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "auth_server_error")))
	})

	t.Run("should panic naming the wiring function when called unwired", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: StartLogin not wired; call service.StartLogin or service.Setup", fmt.Sprint(recover()))
		}()

		_, _ = servicetest.NewFat(t).StartLogin(t.Context(), "alice.test")
	})

	t.Run("should panic when wired without an OAuth client app", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: StartLogin needs an OAuth client app", fmt.Sprint(recover()))
		}()

		service.StartLogin(servicetest.NewFat(t), nil)
	})
}

func TestFat_FinishLogin(t *testing.T) {
	t.Run("should create the user, write the profile and return the session on first login", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		user, sessionID, err := h.login(t, ctx, "alice.test")
		span.End()
		is.NotError(t, err)
		is.Equal(t, model.DID("did:plc:alice"), user.DID)
		is.True(t, user.Active)
		is.True(t, sessionID != "")

		record, ok := h.net.GetRecord("did:plc:alice", lexicons.ActorProfile, "self")
		is.True(t, ok, "no profile record")
		is.Equal(t, lexicons.ActorProfile, record["$type"])
		_, err = syntax.ParseDatetime(fmt.Sprint(record["createdAt"]))
		is.NotError(t, err)

		_, err = h.db.GetSession(t.Context(), "did:plc:alice", sessionID)
		is.NotError(t, err)

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("enduser.pseudo.id", string(user.ID))))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.did", "did:plc:alice")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.pds_host", "pds.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.auth_server", "auth.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.scopes_granted", strings.Join(service.OAuthScopes, " "))))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.first_login", true)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.profile_created", true)))
		is.True(t, !oteltest.HasAttributeKey(attrs, "login.condition"))

		for _, name := range []string{"oauth.token_exchange", "com.atproto.repo.getRecord", "com.atproto.repo.putRecord"} {
			is.True(t, h.hasSpan(name), "no child span "+name)
		}
	})

	t.Run("should keep the user and profile on second login and add a second session", func(t *testing.T) {
		h := newHarness(t)

		first, firstSession, err := h.login(t, t.Context(), "alice.test")
		is.NotError(t, err)

		ctx, span := h.startSpan(t)
		second, secondSession, err := h.login(t, ctx, "alice.test")
		span.End()
		is.NotError(t, err)
		is.Equal(t, first.ID, second.ID)
		is.True(t, firstSession != secondSession)
		is.Equal(t, 1, h.net.PutRecordCalls())

		var count int
		is.NotError(t, h.db.H.Get(t.Context(), &count, `select count(*) from oauth_sessions`))
		is.Equal(t, 2, count)

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.first_login", false)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.profile_created", false)))
	})

	t.Run("should not overwrite a profile written concurrently between the read and the write", func(t *testing.T) {
		h := newHarness(t)
		h.net.PutRecordRaces = true

		ctx, span := h.startSpan(t)
		_, sessionID, err := h.login(t, ctx, "alice.test")
		span.End()
		is.NotError(t, err)
		is.True(t, sessionID != "")
		is.Equal(t, 1, h.net.PutRecordCalls())

		record, ok := h.net.GetRecord("did:plc:alice", lexicons.ActorProfile, "self")
		is.True(t, ok, "no profile record")
		is.Equal(t, "2000-01-01T00:00:00.000Z", record["createdAt"])

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.first_login", true)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.profile_created", false)))
		is.True(t, !oteltest.HasAttributeKey(attrs, "login.condition"))
	})

	t.Run("should refuse when a required scope was not granted, leaving no session or user", func(t *testing.T) {
		h := newHarness(t)
		h.net.GrantScopes = "atproto blob:audio/*"

		ctx, span := h.startSpan(t)
		_, _, err := h.login(t, ctx, "alice.test")
		span.End()
		is.Error(t, model.ErrorScopeDenied, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "scope_denied")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, h.count(t, "users"))
	})

	t.Run("should refuse an inactive user, leaving no session", func(t *testing.T) {
		h := newHarness(t)
		user, _, err := h.db.GetOrCreateUser(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.NotError(t, h.db.H.Exec(t.Context(), `update users set active = 0 where id = ?`, user.ID))

		ctx, span := h.startSpan(t)
		_, _, err = h.login(t, ctx, "alice.test")
		span.End()
		is.Error(t, model.ErrorUserInactive, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "user_inactive")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
		is.Equal(t, 0, h.net.PutRecordCalls())
	})

	t.Run("should refuse when the profile cannot be written, leaving no session", func(t *testing.T) {
		h := newHarness(t)
		h.net.PutRecordFails = true

		ctx, span := h.startSpan(t)
		_, _, err := h.login(t, ctx, "alice.test")
		span.End()
		is.Error(t, model.ErrorProfileWriteFailed, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "profile_write_failed")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse when the user denied consent", func(t *testing.T) {
		h := newHarness(t)
		h.net.Deny = true

		ctx, span := h.startSpan(t)
		_, _, err := h.login(t, ctx, "alice.test")
		span.End()
		is.Error(t, model.ErrorLoginCancelled, err)
		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("login.condition", "callback_error")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.callback_error", "access_denied")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse when the granted scopes lack atproto", func(t *testing.T) {
		h := newHarness(t)
		h.net.GrantScopes = strings.Join(service.OAuthScopes[1:], " ")

		ctx, span := h.startSpan(t)
		_, _, err := h.login(t, ctx, "alice.test")
		span.End()
		is.Error(t, model.ErrorScopeDenied, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "scope_denied")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback for a flow the user did not start", func(t *testing.T) {
		h := newHarness(t)

		start, err := h.fat.StartLogin(t.Context(), "alice.test")
		is.NotError(t, err)
		params := h.net.Authorize(t, start.RedirectURL)

		ctx, span := h.startSpan(t)
		_, _, err = h.fat.FinishLogin(ctx, params, "another-flow")
		span.End()
		is.Error(t, model.ErrorLoginCancelled, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "callback_error")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback with an unknown state", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		_, _, err := h.fat.FinishLogin(ctx, url.Values{"state": {"nope"}, "code": {"c"}, "iss": {h.net.AuthServerURL}}, "nope")
		span.End()
		is.Error(t, model.ErrorLoginCancelled, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "callback_error")))
	})

	t.Run("should refuse a callback without a state", func(t *testing.T) {
		h := newHarness(t)

		_, _, err := h.fat.FinishLogin(t.Context(), url.Values{}, "")
		is.Error(t, model.ErrorLoginCancelled, err)
	})

	t.Run("should refuse a callback without a code, and one from another auth server", func(t *testing.T) {
		h := newHarness(t)

		start, err := h.fat.StartLogin(t.Context(), "alice.test")
		is.NotError(t, err)
		params := h.net.Authorize(t, start.RedirectURL)

		noCode := url.Values{"state": {params.Get("state")}, "iss": {params.Get("iss")}}
		_, _, err = h.fat.FinishLogin(t.Context(), noCode, start.State)
		is.Error(t, model.ErrorLoginCancelled, err)

		otherIssuer := url.Values{"state": {params.Get("state")}, "code": {params.Get("code")}, "iss": {"https://evil.test"}}
		_, _, err = h.fat.FinishLogin(t.Context(), otherIssuer, start.State)
		is.Error(t, model.ErrorLoginCancelled, err)

		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should refuse a callback whose code the auth server rejects", func(t *testing.T) {
		h := newHarness(t)

		start, err := h.fat.StartLogin(t.Context(), "alice.test")
		is.NotError(t, err)
		params := h.net.Authorize(t, start.RedirectURL)
		params.Set("code", "forged")

		ctx, span := h.startSpan(t)
		_, _, err = h.fat.FinishLogin(ctx, params, start.State)
		span.End()
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "auth_server_error")))
		is.Equal(t, 0, h.count(t, "oauth_sessions"))
	})

	t.Run("should panic naming the wiring function when called unwired", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: FinishLogin not wired; call service.FinishLogin or service.Setup", fmt.Sprint(recover()))
		}()

		_, _, _ = servicetest.NewFat(t).FinishLogin(t.Context(), url.Values{}, "")
	})
}

func TestFat_Logout(t *testing.T) {
	t.Run("should revoke and delete one session and leave the other", func(t *testing.T) {
		h := newHarness(t)
		_, first, err := h.login(t, t.Context(), "alice.test")
		is.NotError(t, err)
		_, second, err := h.login(t, t.Context(), "alice.test")
		is.NotError(t, err)
		firstData, err := h.db.GetSession(t.Context(), "did:plc:alice", first)
		is.NotError(t, err)

		is.NotError(t, h.fat.Logout(t.Context(), "did:plc:alice", first))

		revoked := h.net.Revoked()
		is.EqualSlice(t, []string{firstData.AccessToken, firstData.RefreshToken}, revoked)
		is.True(t, h.hasSpan("oauth.revoke"))

		_, err = h.db.GetSession(t.Context(), "did:plc:alice", first)
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		_, err = h.db.GetSession(t.Context(), "did:plc:alice", second)
		is.NotError(t, err)
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		err := h.fat.Logout(t.Context(), "did:plc:alice", "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestFat_PDSClient(t *testing.T) {
	t.Run("should return a client that reads from the account's PDS as the account", func(t *testing.T) {
		h := newHarness(t)
		_, sessionID, err := h.login(t, t.Context(), "alice.test")
		is.NotError(t, err)

		client, err := h.fat.PDSClient(t.Context(), "did:plc:alice", sessionID)
		is.NotError(t, err)
		is.Equal(t, h.net.PDSURL, client.Host)

		var out struct {
			Value map[string]any `json:"value"`
		}
		params := map[string]any{"repo": "did:plc:alice", "collection": lexicons.ActorProfile, "rkey": "self"}
		is.NotError(t, client.Get(t.Context(), "com.atproto.repo.getRecord", params, &out))
		is.Equal(t, lexicons.ActorProfile, out.Value["$type"])
	})

	t.Run("should return not found for an unknown session", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.fat.PDSClient(t.Context(), "did:plc:alice", "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestFat_ResolveHandle(t *testing.T) {
	t.Run("should resolve the handle of a known DID", func(t *testing.T) {
		h := newHarness(t)

		handle, err := h.fat.ResolveHandle(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.Equal(t, "alice.test", handle)
	})

	t.Run("should resolve handle.invalid for an account whose handle does not verify", func(t *testing.T) {
		h := newHarness(t)
		h.net.Directory.Insert(identity.Identity{DID: "did:plc:bob", Handle: syntax.HandleInvalid})

		handle, err := h.fat.ResolveHandle(t.Context(), "did:plc:bob")
		is.NotError(t, err)
		is.Equal(t, "handle.invalid", handle)
	})

	t.Run("should error for an unknown DID", func(t *testing.T) {
		h := newHarness(t)

		_, err := h.fat.ResolveHandle(t.Context(), "did:plc:nobody")
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should panic when wired without a directory, rather than reach for the real network", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: ResolveHandle needs an identity directory", fmt.Sprint(recover()))
		}()

		service.ResolveHandle(servicetest.NewFat(t), nil)
	})
}

// harness wires a Fat to a database and the fake network, with a span recorder in place before the Fat
// is made so its tracer records into it.
type harness struct {
	net *atprototest.Network
	db  *sqlite.Database
	app *oauth.ClientApp
	fat *service.Fat
	sr  *tracetest.SpanRecorder
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		sr:  oteltest.NewSpanRecorder(t),
		net: atprototest.NewNetwork(t),
		db:  sqlitetest.NewDatabase(t),
	}
	h.net.AddAccount("did:plc:alice", "alice.test")
	h.app = h.net.NewClientApp(t, h.db, service.OAuthScopes)

	catalog, err := lexicons.NewCatalog()
	is.NotError(t, err)

	h.fat = servicetest.NewFat(t)
	service.StartLogin(h.fat, h.app)
	service.FinishLogin(h.fat, h.db, h.app, catalog)
	service.Logout(h.fat, h.app)
	service.PDSClient(h.fat, h.app)
	service.ResolveHandle(h.fat, h.net.Directory)
	return h
}

// login all the way: start, authorize at the fake auth server as the user would, finish.
func (h *harness) login(t *testing.T, ctx context.Context, identifier string) (model.User, string, error) {
	t.Helper()

	start, err := h.fat.StartLogin(ctx, identifier)
	if err != nil {
		return model.User{}, "", err
	}
	return h.fat.FinishLogin(ctx, h.net.Authorize(t, start.RedirectURL), start.State)
}

// startSpan standing in for the request span the operations write their attributes to.
func (h *harness) startSpan(t *testing.T) (context.Context, trace.Span) {
	t.Helper()

	return otel.Tracer("test").Start(t.Context(), "request")
}

func (h *harness) requestSpanAttributes(t *testing.T) []attribute.KeyValue {
	t.Helper()

	for _, span := range h.sr.Ended() {
		if span.Name() == "request" {
			return span.Attributes()
		}
	}
	t.Fatal("no request span ended")
	return nil
}

func (h *harness) hasSpan(name string) bool {
	for _, span := range h.sr.Ended() {
		if span.Name() == name {
			return true
		}
	}
	return false
}

func (h *harness) count(t *testing.T, table string) int {
	t.Helper()

	var count int
	is.NotError(t, h.db.H.Get(t.Context(), &count, `select count(*) from `+table))
	return count
}
