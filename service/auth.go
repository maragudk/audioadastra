package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"app/lexicons"
	"app/model"
)

// OAuthScopes every login requests, and which every session must have been granted in full. They are
// asked for up front so later features do not send the user back to consent.
//
// The repo scope names the profile collection explicitly: the permission syntax has no partial
// wildcard, so "repo:com.audioadastra.*" is not a valid scope.
var OAuthScopes = []string{"atproto", "repo:" + lexicons.ActorProfile, "blob:audio/*", "blob:image/*"}

// NewOAuthClientConfigOptions for [NewOAuthClientConfig].
type NewOAuthClientConfigOptions struct {
	// BaseURL of the app, which the client ID and callback URL are under.
	BaseURL string
	// PrivateKeyMultibase is the P-256 client assertion key in multibase encoding. Required unless
	// BaseURL is a localhost URL.
	PrivateKeyMultibase string
	// KeyID names the key in the published JWKS. Required with PrivateKeyMultibase.
	KeyID string
}

// NewOAuthClientConfig for the app at the given base URL.
//
// A base URL on localhost or 127.0.0.1 gives the localhost development client the OAuth spec allows,
// which needs no key and no published metadata; its callback is on 127.0.0.1, since loopback redirect
// URIs may not use the localhost name. Any other base URL gives a confidential client, and the key is
// required.
func NewOAuthClientConfig(opts NewOAuthClientConfigOptions) (oauth.ClientConfig, error) {
	base, err := url.Parse(strings.TrimSuffix(opts.BaseURL, "/"))
	if err != nil || base.Host == "" {
		return oauth.ClientConfig{}, fmt.Errorf("base URL %q is not a URL with a host", opts.BaseURL)
	}

	if base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" {
		callback := *base
		callback.Host = "127.0.0.1"
		if port := base.Port(); port != "" {
			callback.Host += ":" + port
		}
		callback.Path = strings.TrimSuffix(base.Path, "/") + "/oauth/callback"
		config := oauth.NewLocalhostConfig(callback.String(), OAuthScopes)
		config.UserAgent = "audioadastra"
		return config, nil
	}

	if opts.PrivateKeyMultibase == "" || opts.KeyID == "" {
		return oauth.ClientConfig{}, errors.New("an OAuth private key and key ID are required unless the base URL is on localhost")
	}
	key, err := atcrypto.ParsePrivateMultibase(opts.PrivateKeyMultibase)
	if err != nil {
		return oauth.ClientConfig{}, fmt.Errorf("parsing OAuth private key: %w", err)
	}

	config := oauth.NewPublicConfig(base.String()+"/oauth/client-metadata.json", base.String()+"/oauth/callback", OAuthScopes)
	config.UserAgent = "audioadastra"
	if err := config.SetClientSecret(key, opts.KeyID); err != nil {
		return oauth.ClientConfig{}, fmt.Errorf("setting OAuth client secret: %w", err)
	}
	return config, nil
}

// StartLogin wires [Fat.StartLogin] to the given OAuth client app, whose identity directory, resolver
// and store it uses.
func StartLogin(f *Fat, app *oauth.ClientApp) {
	if f.startLogin != nil {
		panic("service: StartLogin already wired")
	}
	if app == nil {
		panic("service: StartLogin needs an OAuth client app")
	}

	f.startLogin = func(ctx context.Context, identifier string) (LoginStart, error) {
		return startLogin(ctx, f, app, identifier)
	}
}

// LoginStart is a login flow that has been pushed to the auth server and awaits the user's consent.
type LoginStart struct {
	// RedirectURL the user must be sent to for consent.
	RedirectURL string
	// State identifying the flow. The auth server sends it back with the callback, and [Fat.FinishLogin]
	// must be given the same value, so a callback can only finish the flow that was started for it.
	State string
}

// StartLogin for the account with the given identifier, a handle or a DID. It resolves the identity,
// discovers the account's auth server and pushes the authorization request.
//
// Errors are [model.ErrorIdentityUnresolved] when the identifier is not one or does not resolve to an
// account on a PDS, and [model.ErrorAuthServerUnavailable] when the auth server cannot be discovered
// or refuses the request.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) StartLogin(ctx context.Context, identifier string) (LoginStart, error) {
	if f.startLogin == nil {
		panic("service: StartLogin not wired; call service.StartLogin or service.Setup")
	}

	return f.startLogin(ctx, identifier)
}

func startLogin(ctx context.Context, f *Fat, app *oauth.ClientApp, identifier string) (start LoginStart, err error) {
	event := newLoginEvent(ctx)
	defer func() { event.finish(f.log, "Login start failed", err) }()

	atid, err := syntax.ParseAtIdentifier(strings.TrimSpace(identifier))
	if err != nil {
		return LoginStart{}, fmt.Errorf("%w: parsing identifier: %w", model.ErrorIdentityUnresolved, err)
	}
	if handle, err := atid.AsHandle(); err == nil {
		event.set(attribute.String("atproto.handle", handle.String()))
	}

	ident, err := lookupIdentity(ctx, f, app.Dir, atid)
	if err != nil {
		return LoginStart{}, fmt.Errorf("%w: resolving %v: %w", model.ErrorIdentityUnresolved, atid, err)
	}
	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return LoginStart{}, fmt.Errorf("%w: %v has no PDS", model.ErrorIdentityUnresolved, ident.DID)
	}
	event.set(attribute.String("atproto.did", ident.DID.String()), attribute.String("atproto.handle", ident.Handle.String()), attribute.String("atproto.pds_host", hostOf(pdsURL)))

	meta, err := discoverAuthServer(ctx, f, app.Resolver, pdsURL)
	if err != nil {
		return LoginStart{}, fmt.Errorf("%w: discovering auth server for %v: %w", model.ErrorAuthServerUnavailable, pdsURL, err)
	}
	event.set(attribute.String("oauth.auth_server", hostOf(meta.Issuer)))

	info, err := pushAuthRequest(ctx, f, app, meta, atid.String())
	if err != nil {
		return LoginStart{}, fmt.Errorf("%w: pushing auth request to %v: %w", model.ErrorAuthServerUnavailable, meta.Issuer, err)
	}
	info.AccountDID = &ident.DID

	if err := app.Store.SaveAuthRequestInfo(ctx, *info); err != nil {
		return LoginStart{}, fmt.Errorf("saving auth request: %w", err)
	}

	params := url.Values{}
	params.Set("client_id", app.Config.ClientID)
	params.Set("request_uri", info.RequestURI)
	return LoginStart{RedirectURL: meta.AuthorizationEndpoint + "?" + params.Encode(), State: info.State}, nil
}

func lookupIdentity(ctx context.Context, f *Fat, dir identity.Directory, atid syntax.AtIdentifier) (ident *identity.Identity, err error) {
	ctx, span := f.tracer.Start(ctx, "identity.lookup", trace.WithSpanKind(trace.SpanKindClient))
	defer func() { endSpan(span, err) }()

	return dir.Lookup(ctx, atid)
}

// discoverAuthServer for the PDS at the given URL: the protected resource document names the auth
// server, whose own metadata document has the endpoints.
func discoverAuthServer(ctx context.Context, f *Fat, resolver *oauth.Resolver, pdsURL string) (meta *oauth.AuthServerMetadata, err error) {
	ctx, span := f.tracer.Start(ctx, "oauth.discover_auth_server", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(pdsURL))))
	defer func() { endSpan(span, err) }()

	authServerURL, err := resolver.ResolveAuthServerURL(ctx, pdsURL)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.String("oauth.auth_server", hostOf(authServerURL)))

	return resolver.ResolveAuthServerMetadata(ctx, authServerURL)
}

func pushAuthRequest(ctx context.Context, f *Fat, app *oauth.ClientApp, meta *oauth.AuthServerMetadata, loginHint string) (info *oauth.AuthRequestData, err error) {
	ctx, span := f.tracer.Start(ctx, "oauth.pushed_authorization_request", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(meta.Issuer))))
	defer func() { endSpan(span, err) }()

	return app.SendAuthRequest(ctx, meta, app.Config.Scopes, loginHint)
}

// userGetOrCreator is the store a user is looked up or created in by DID.
type userGetOrCreator interface {
	GetOrCreateUser(ctx context.Context, did model.DID) (model.User, bool, error)
}

// FinishLogin wires [Fat.FinishLogin] to the given store, OAuth client app and lexicon catalog.
func FinishLogin(f *Fat, db userGetOrCreator, app *oauth.ClientApp, catalog lexicon.Catalog) {
	if f.finishLogin != nil {
		panic("service: FinishLogin already wired")
	}
	if db == nil || app == nil || catalog == nil {
		panic("service: FinishLogin needs a store, an OAuth client app and a lexicon catalog")
	}

	f.finishLogin = func(ctx context.Context, params url.Values, state string) (model.User, string, error) {
		return finishLogin(ctx, f, db, app, catalog, params, state)
	}
}

// FinishLogin with the query parameters the auth server sent to the callback, for the flow with the
// given state, which is the [LoginStart.State] of the flow the same user started: a callback for any
// other flow is refused. It exchanges the code for tokens, checks that every scope in [OAuthScopes] was
// granted, gets or creates the user for the DID, refuses inactive users, and makes sure the account's
// profile record exists, writing an empty one on first login. The OAuth session ID returned is what
// [Fat.PDSClient] and [Fat.Logout] take.
//
// Errors are [model.ErrorLoginCancelled] when the callback is for another flow, carries no code, or
// comes from another auth server than the flow was started with, [model.ErrorAuthServerUnavailable]
// when the token exchange fails, [model.ErrorScopeDenied], [model.ErrorUserInactive] and
// [model.ErrorProfileWriteFailed]. No OAuth session is left behind on any error.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) FinishLogin(ctx context.Context, params url.Values, state string) (model.User, string, error) {
	if f.finishLogin == nil {
		panic("service: FinishLogin not wired; call service.FinishLogin or service.Setup")
	}

	return f.finishLogin(ctx, params, state)
}

func finishLogin(ctx context.Context, f *Fat, db userGetOrCreator, app *oauth.ClientApp, catalog lexicon.Catalog, params url.Values, state string) (user model.User, sessionID string, err error) {
	event := newLoginEvent(ctx)
	defer func() { event.finish(f.log, "Login failed", err) }()

	if state == "" || params.Get("state") != state {
		return model.User{}, "", fmt.Errorf("%w: callback state is not the flow's", model.ErrorLoginCancelled)
	}
	info, err := app.Store.GetAuthRequestInfo(ctx, state)
	if err != nil {
		if errors.Is(err, model.ErrorOAuthAuthRequestNotFound) {
			return model.User{}, "", fmt.Errorf("%w: %w", model.ErrorLoginCancelled, err)
		}
		return model.User{}, "", fmt.Errorf("loading auth request: %w", err)
	}
	event.set(attribute.String("oauth.auth_server", hostOf(info.AuthServerURL)))
	if info.AccountDID != nil {
		event.set(attribute.String("atproto.did", info.AccountDID.String()))
	}
	// An error response is left for the token exchange to classify; a success response must carry a
	// code from the auth server the flow was started with.
	if params.Get("error") == "" && (params.Get("code") == "" || params.Get("iss") != info.AuthServerURL) {
		return model.User{}, "", fmt.Errorf("%w: callback has no code from %v", model.ErrorLoginCancelled, info.AuthServerURL)
	}

	sess, err := exchangeToken(ctx, f, app, info, params)
	if err != nil {
		var callbackErr *oauth.AuthRequestCallbackError
		if errors.As(err, &callbackErr) {
			event.set(attribute.String("oauth.callback_error", callbackErr.ErrorCode))
			return model.User{}, "", fmt.Errorf("%w: %w", model.ErrorLoginCancelled, err)
		}
		return model.User{}, "", fmt.Errorf("%w: %w", model.ErrorAuthServerUnavailable, err)
	}

	// The OAuth session is persisted from here on, so a refusal below must take it with it: its ID is
	// never returned, so nothing else would ever delete it. The delete outlives a cancelled context for
	// the same reason.
	defer func() {
		if err == nil {
			return
		}
		if deleteErr := app.Store.DeleteSession(context.WithoutCancel(ctx), sess.AccountDID, sess.SessionID); deleteErr != nil {
			f.log.ErrorContext(ctx, "Error deleting OAuth session after failed login", "error", deleteErr, "did", sess.AccountDID, "sessionID", sess.SessionID)
		}
	}()

	event.set(
		attribute.String("atproto.did", sess.AccountDID.String()),
		attribute.String("atproto.pds_host", hostOf(sess.HostURL)),
		attribute.String("oauth.scopes_granted", strings.Join(sess.Scopes, " ")),
	)

	if err := checkScopes(sess.Scopes); err != nil {
		return model.User{}, "", err
	}

	user, created, err := db.GetOrCreateUser(ctx, model.DID(sess.AccountDID))
	if err != nil {
		return model.User{}, "", fmt.Errorf("getting or creating user for %v: %w", sess.AccountDID, err)
	}
	event.set(semconv.EnduserPseudoID(string(user.ID)), attribute.Bool("login.first_login", created))
	if !user.Active {
		return model.User{}, "", model.ErrorUserInactive
	}

	profileCreated, err := ensureProfile(ctx, f, app, catalog, sess)
	if err != nil {
		return model.User{}, "", fmt.Errorf("%w: %w", model.ErrorProfileWriteFailed, err)
	}
	event.set(attribute.Bool("login.profile_created", profileCreated))

	return user, sess.SessionID, nil
}

func exchangeToken(ctx context.Context, f *Fat, app *oauth.ClientApp, info *oauth.AuthRequestData, params url.Values) (sess *oauth.ClientSessionData, err error) {
	ctx, span := f.tracer.Start(ctx, "oauth.token_exchange", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(info.AuthServerURL))))
	defer func() { endSpan(span, err) }()

	return app.ProcessCallback(ctx, params)
}

// checkScopes that were granted against [OAuthScopes], comparing parsed permissions rather than
// strings so an auth server that normalizes its scope strings still passes.
func checkScopes(granted []string) error {
	grantedPermissions, err := auth.ParseOAuthScope(strings.Join(granted, " "))
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrorScopeDenied, err)
	}
	have := map[string]bool{}
	for _, p := range grantedPermissions {
		have[p.ScopeString()] = true
	}

	for _, scope := range OAuthScopes {
		if scope == "atproto" {
			continue
		}
		required, err := auth.ParsePermissionString(scope)
		if err != nil {
			panic("service: required OAuth scope " + scope + " does not parse: " + err.Error())
		}
		if !have[required.ScopeString()] {
			return fmt.Errorf("%w: %v not granted", model.ErrorScopeDenied, scope)
		}
	}
	return nil
}

// ensureProfile exists in the account's repository, writing an empty one if not, and reports whether
// it wrote one.
func ensureProfile(ctx context.Context, f *Fat, app *oauth.ClientApp, catalog lexicon.Catalog, sess *oauth.ClientSessionData) (created bool, err error) {
	oauthSess, err := app.ResumeSession(ctx, sess.AccountDID, sess.SessionID)
	if err != nil {
		return false, fmt.Errorf("resuming OAuth session: %w", err)
	}
	client := oauthSess.APIClient()

	exists, err := getProfile(ctx, f, client, sess)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	record := map[string]any{
		"$type":     lexicons.ActorProfile,
		"createdAt": syntax.DatetimeNow().String(),
	}
	if err := lexicon.ValidateRecord(catalog, record, lexicons.ActorProfile, 0); err != nil {
		return false, fmt.Errorf("validating profile record: %w", err)
	}
	return putProfile(ctx, f, client, sess, record)
}

func getProfile(ctx context.Context, f *Fat, client *atclient.APIClient, sess *oauth.ClientSessionData) (exists bool, err error) {
	ctx, span := f.tracer.Start(ctx, "com.atproto.repo.getRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(sess.HostURL)), attribute.String("atproto.did", sess.AccountDID.String()), attribute.String("atproto.collection", lexicons.ActorProfile)))
	defer func() { endSpan(span, err) }()

	params := map[string]any{"repo": sess.AccountDID.String(), "collection": lexicons.ActorProfile, "rkey": "self"}
	err = client.Get(ctx, "com.atproto.repo.getRecord", params, nil)
	if err == nil {
		return true, nil
	}
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) && apiErr.Name == "RecordNotFound" {
		span.SetAttributes(attribute.Bool("atproto.record_found", false))
		return false, nil
	}
	return false, fmt.Errorf("getting profile record: %w", err)
}

// putProfile only if none exists: a null swapRecord makes the write conditional on the record's
// absence, so two logins racing past the read cannot overwrite each other's record. Losing that race
// is not an error, and reports the record as not created by this call.
func putProfile(ctx context.Context, f *Fat, client *atclient.APIClient, sess *oauth.ClientSessionData, record map[string]any) (created bool, err error) {
	ctx, span := f.tracer.Start(ctx, "com.atproto.repo.putRecord", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(sess.HostURL)), attribute.String("atproto.did", sess.AccountDID.String()), attribute.String("atproto.collection", lexicons.ActorProfile)))
	defer func() { endSpan(span, err) }()

	body := map[string]any{"repo": sess.AccountDID.String(), "collection": lexicons.ActorProfile, "rkey": "self", "record": record, "swapRecord": nil}
	err = client.Post(ctx, "com.atproto.repo.putRecord", body, nil)
	if err == nil {
		return true, nil
	}
	var apiErr *atclient.APIError
	if errors.As(err, &apiErr) && apiErr.Name == "InvalidSwap" {
		span.SetAttributes(attribute.Bool("atproto.record_found", true))
		return false, nil
	}
	return false, fmt.Errorf("putting profile record: %w", err)
}

// Logout wires [Fat.Logout] to the given OAuth client app.
func Logout(f *Fat, app *oauth.ClientApp) {
	if f.logout != nil {
		panic("service: Logout already wired")
	}
	if app == nil {
		panic("service: Logout needs an OAuth client app")
	}

	f.logout = func(ctx context.Context, did model.DID, sessionID string) error {
		return logout(ctx, f, app, did, sessionID)
	}
}

// Logout of the given OAuth session: its tokens are revoked at the auth server when it supports that,
// which is best effort, and the session is deleted. Other sessions of the same account are untouched.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) Logout(ctx context.Context, did model.DID, sessionID string) error {
	if f.logout == nil {
		panic("service: Logout not wired; call service.Logout or service.Setup")
	}

	return f.logout(ctx, did, sessionID)
}

func logout(ctx context.Context, f *Fat, app *oauth.ClientApp, did model.DID, sessionID string) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("atproto.did", did.String()))

	sess, err := app.ResumeSession(ctx, syntax.DID(did), sessionID)
	if err != nil {
		return fmt.Errorf("resuming OAuth session: %w", err)
	}

	if sess.Data.AuthServerRevocationEndpoint != "" {
		if err := revoke(ctx, f, sess); err != nil {
			f.log.WarnContext(ctx, "Error revoking OAuth tokens at logout", "error", err, "did", did, "authServer", hostOf(sess.Data.AuthServerURL))
			span.SetAttributes(attribute.Bool("oauth.revoked", false))
		} else {
			span.SetAttributes(attribute.Bool("oauth.revoked", true))
		}
	}

	if err := app.Store.DeleteSession(ctx, syntax.DID(did), sessionID); err != nil {
		return fmt.Errorf("deleting OAuth session: %w", err)
	}
	return nil
}

func revoke(ctx context.Context, f *Fat, sess *oauth.ClientSession) (err error) {
	ctx, span := f.tracer.Start(ctx, "oauth.revoke", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(sess.Data.AuthServerURL))))
	defer func() { endSpan(span, err) }()

	return sess.RevokeSession(ctx)
}

// PDSClient wires [Fat.PDSClient] to the given OAuth client app.
func PDSClient(f *Fat, app *oauth.ClientApp) {
	if f.pdsClient != nil {
		panic("service: PDSClient already wired")
	}
	if app == nil {
		panic("service: PDSClient needs an OAuth client app")
	}

	f.pdsClient = func(ctx context.Context, did model.DID, sessionID string) (*atclient.APIClient, error) {
		sess, err := app.ResumeSession(ctx, syntax.DID(did), sessionID)
		if err != nil {
			return nil, fmt.Errorf("resuming OAuth session: %w", err)
		}
		return sess.APIClient(), nil
	}
}

// PDSClient for the account's PDS, authenticated with the given OAuth session. Token refreshes and
// DPoP nonce rotations happen behind it and are persisted.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) PDSClient(ctx context.Context, did model.DID, sessionID string) (*atclient.APIClient, error) {
	if f.pdsClient == nil {
		panic("service: PDSClient not wired; call service.PDSClient or service.Setup")
	}

	return f.pdsClient(ctx, did, sessionID)
}

// ResolveHandle wires [Fat.ResolveHandle] to the given identity directory.
func ResolveHandle(f *Fat, dir identity.Directory) {
	if f.resolveHandle != nil {
		panic("service: ResolveHandle already wired")
	}
	if dir == nil {
		panic("service: ResolveHandle needs an identity directory")
	}

	f.resolveHandle = func(ctx context.Context, did model.DID) (string, error) {
		ident, err := lookupIdentity(ctx, f, dir, syntax.DID(did).AtIdentifier())
		if err != nil {
			return "", fmt.Errorf("resolving %v: %w", did, err)
		}
		return ident.Handle.String(), nil
	}
}

// ResolveHandle of the given DID, bidirectionally verified, which is "handle.invalid" when the
// account's declared handle does not point back at it.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) ResolveHandle(ctx context.Context, did model.DID) (string, error) {
	if f.resolveHandle == nil {
		panic("service: ResolveHandle not wired; call service.ResolveHandle or service.Setup")
	}

	return f.resolveHandle(ctx, did)
}

// loginEvent gathers the attributes of one login attempt on the span in the context as they become
// known, so the same set lands on the span as a wide event and in the warning logged when the attempt
// fails. A key set again replaces its earlier value, as it does on the span.
type loginEvent struct {
	ctx   context.Context
	span  trace.Span
	attrs []attribute.KeyValue
}

func newLoginEvent(ctx context.Context) *loginEvent {
	return &loginEvent{ctx: ctx, span: trace.SpanFromContext(ctx)}
}

func (e *loginEvent) set(attrs ...attribute.KeyValue) {
	for _, attr := range attrs {
		e.attrs = slices.DeleteFunc(e.attrs, func(existing attribute.KeyValue) bool { return existing.Key == attr.Key })
		e.attrs = append(e.attrs, attr)
	}
	e.span.SetAttributes(attrs...)
}

// finish the attempt: a known refusal is recorded as login.condition and every failure is logged with
// the gathered attributes. Nothing is recorded on success.
func (e *loginEvent) finish(log *slog.Logger, msg string, err error) {
	if err == nil {
		return
	}

	if condition := loginCondition(err); condition != "" {
		e.set(attribute.String("login.condition", condition))
	}

	args := []any{"error", err}
	for _, attr := range e.attrs {
		args = append(args, string(attr.Key), attr.Value.AsInterface())
	}
	log.WarnContext(e.ctx, msg, args...)
}

// loginCondition for the error, as the login.condition attribute value, or the empty string if it is
// not a known refusal.
func loginCondition(err error) string {
	conditions := []struct {
		err       error
		condition string
	}{
		{model.ErrorIdentityUnresolved, "identity_error"},
		{model.ErrorAuthServerUnavailable, "auth_server_error"},
		{model.ErrorLoginCancelled, "callback_error"},
		{model.ErrorScopeDenied, "scope_denied"},
		{model.ErrorUserInactive, "user_inactive"},
		{model.ErrorProfileWriteFailed, "profile_write_failed"},
	}
	for _, c := range conditions {
		if errors.Is(err, c.err) {
			return c.condition
		}
	}
	return ""
}

// endSpan with the error recorded and the status set, when there is one.
func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// hostOf a URL, for attributes; the URL itself when it does not parse.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
