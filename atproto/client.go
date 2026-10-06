// Package atproto is how the app reaches the atmosphere: OAuth login against an account's auth server,
// calls to the account's PDS, and identity resolution, for either the real network or a local one.
// The underlying SDK stays inside this package; everything crosses its boundary in the app's own
// types and errors.
package atproto

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

// scopes every login requests, and which every session must have been granted in full. They are
// asked for up front so later features do not send the user back to consent.
//
// The repo scope names the profile collection explicitly: the permission syntax has no partial
// wildcard, so "repo:com.audioadastra.*" is not a valid scope.
var scopes = []string{"atproto", "repo:" + model.CollectionActorProfile.String(), "blob:audio/*", "blob:image/*"}

// Client for one network, built once by [NewClient]. Safe for concurrent use.
type Client struct {
	// baseURL of the app, without a trailing slash.
	baseURL string
	app     *oauth.ClientApp
	dir     identity.Directory
	store   store
	tracer  trace.Tracer
	local   bool
}

// NewClientOptions for [NewClient].
type NewClientOptions struct {
	// BaseURL of the app, which the client ID and callback URL are under. It must be absolute, with a
	// host; a trailing slash is ignored.
	BaseURL *url.URL
	// PrivateKeyMultibase is the P-256 client assertion key in multibase encoding. Required unless
	// BaseURL is a localhost URL.
	PrivateKeyMultibase string
	// KeyID names the key in the published JWKS. Required with PrivateKeyMultibase.
	KeyID string
	// Store for auth requests and sessions.
	Store store

	// PLCURL of a local PLC directory. When set, the identity directory resolves did:plc through it and
	// every HTTP client goes without SSRF protection, since the local network is on loopback. When nil,
	// the real network is used with the protections on.
	PLCURL *url.URL
	// CAFile with an extra PEM root certificate to trust, such as the one a local reverse proxy issues
	// its certificates from.
	CAFile string
	// LocalHandleSuffix, such as ".test", of the local network's handles. Hosts under it are dialed on
	// loopback, since nothing resolves them, so handle verification over https reaches the local PDS.
	// Hosts under .localhost are dialed on loopback in any case, as browsers do.
	LocalHandleSuffix string

	// Directory to resolve identities with instead of the network's, and HTTPClient to reach auth
	// servers and PDSes with instead of the network's, for tests against fakes.
	Directory  identity.Directory
	HTTPClient *http.Client
}

// NewClient for the real network by default, or for a local one when a PLC URL is given.
func NewClient(opts NewClientOptions) (*Client, error) {
	if opts.Store == nil {
		return nil, errors.New("a store is required")
	}

	config, err := NewOAuthClientConfig(NewOAuthClientConfigOptions{
		BaseURL:             opts.BaseURL,
		PrivateKeyMultibase: opts.PrivateKeyMultibase,
		KeyID:               opts.KeyID,
	})
	if err != nil {
		return nil, fmt.Errorf("configuring OAuth client: %w", err)
	}

	base, err := appBase(opts.BaseURL)
	if err != nil {
		return nil, err
	}

	c := &Client{
		baseURL: base.String(),
		app:     oauth.NewClientApp(&config, &clientAuthStore{store: opts.Store}),
		dir:     identity.DefaultDirectory(),
		store:   opts.Store,
		tracer:  otel.Tracer("app/atproto"),
	}

	if opts.PLCURL != nil {
		httpClient, err := NewLocalHTTPClient(NewLocalHTTPClientOptions{CAFile: opts.CAFile, LocalHandleSuffix: opts.LocalHandleSuffix})
		if err != nil {
			return nil, err
		}
		dir := &identity.BaseDirectory{
			PLCURL:     opts.PLCURL.String(),
			HTTPClient: *httpClient,
			PLCClient:  httpClient,
			Resolver:   net.Resolver{},
			UserAgent:  "audioadastra",
		}
		c.dir = identity.NewCacheDirectory(dir, 1000, time.Hour, time.Minute, time.Minute)
		c.local = true
		c.app.Client = httpClient
		c.app.Resolver.Client = httpClient
	}

	if opts.Directory != nil {
		c.dir = opts.Directory
	}
	if opts.HTTPClient != nil {
		c.app.Client = opts.HTTPClient
		c.app.Resolver.Client = opts.HTTPClient
	}
	c.app.Dir = c.dir
	return c, nil
}

// Local reports whether the client is for a local network, without SSRF protection.
func (c *Client) Local() bool {
	return c.local
}

// Confidential reports whether the client authenticates with a key, rather than being a localhost
// development client.
func (c *Client) Confidential() bool {
	return c.app.Config.IsConfidential()
}

// ClientID of the OAuth client.
func (c *Client) ClientID() string {
	return c.app.Config.ClientID
}

// CallbackURL the auth server sends the user back to.
func (c *Client) CallbackURL() string {
	return c.app.Config.CallbackURL
}

// RequestedScopes every login asks for.
func (c *Client) RequestedScopes() []string {
	return c.app.Config.Scopes
}

// CheckScopes that were granted against those every login requests, comparing what the parsed
// permissions cover rather than their strings, so an auth server that grants the same access in
// another form, such as two blob types in one permission, still passes.
//
// The error is [model.ErrorScopeDenied] when any requested scope is not covered.
func (c *Client) CheckScopes(granted []string) error {
	grantedPermissions, err := auth.ParseOAuthScope(strings.Join(granted, " "))
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrorScopeDenied, err)
	}

	for _, scope := range c.app.Config.Scopes {
		if scope == "atproto" {
			continue
		}
		required, err := auth.ParsePermissionString(scope)
		if err != nil {
			panic("atproto: requested OAuth scope " + scope + " does not parse: " + err.Error())
		}
		if !covered(*required, grantedPermissions) {
			return fmt.Errorf("%w: %v not granted", model.ErrorScopeDenied, scope)
		}
	}
	return nil
}

// covered reports whether the granted permissions together cover the required one. Blob and repo
// permissions are compared by what they allow; any other kind must be granted exactly as required.
func covered(required auth.Permission, granted []auth.Permission) bool {
	switch required.Resource {
	case "blob":
		for _, accept := range required.Accept {
			if !slices.ContainsFunc(granted, func(g auth.Permission) bool {
				return g.Resource == "blob" && slices.ContainsFunc(g.Accept, func(a string) bool { return mimeCovers(a, accept) })
			}) {
				return false
			}
		}
		return true

	case "repo":
		for _, collection := range required.Collection {
			for _, action := range repoActions(required.Action) {
				if !slices.ContainsFunc(granted, func(g auth.Permission) bool {
					return g.Resource == "repo" &&
						(slices.Contains(g.Collection, collection) || slices.Contains(g.Collection, "*")) &&
						slices.Contains(repoActions(g.Action), action)
				}) {
					return false
				}
			}
		}
		return true

	default:
		return slices.ContainsFunc(granted, func(g auth.Permission) bool { return g.ScopeString() == required.ScopeString() })
	}
}

// mimeCovers reports whether a granted MIME pattern covers a requested one: */* covers everything,
// type/* covers that type's patterns and subtypes, and anything else covers only itself.
func mimeCovers(granted, requested string) bool {
	if granted == "*/*" {
		return true
	}
	if prefix, ok := strings.CutSuffix(granted, "*"); ok && strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(requested, prefix)
	}
	return granted == requested
}

// repoActions of a repo permission, where none listed means all of them.
func repoActions(actions []string) []string {
	if len(actions) == 0 {
		return []string{"create", "update", "delete"}
	}
	return actions
}

// ClientMetadata document, for serving at the client ID as JSON, with the app's base URL as the
// client URI.
func (c *Client) ClientMetadata() any {
	meta := c.app.Config.ClientMetadata()
	meta.ClientName = new("Audio Ad Astra")
	meta.ClientURI = new(c.baseURL)
	if c.Confidential() {
		meta.JWKSURI = new(c.baseURL + "/oauth/jwks.json")
	}
	return meta
}

// JWKS with the public half of the client's key, for serving as JSON. Empty for a localhost client.
func (c *Client) JWKS() any {
	return c.app.Config.PublicJWKS()
}

// StartAuthFlow for the account with the given identifier, a handle or a DID: resolves the identity,
// discovers the account's auth server and pushes the authorization request, remembering it in the
// store.
//
// Errors are [model.ErrorIdentityUnresolved] when the identifier is not one or does not resolve to an
// account on a PDS, [model.ErrorIdentityUnavailable] when looking it up failed, and
// [model.ErrorAuthServerUnavailable] when the auth server cannot be discovered or refuses the request.
func (c *Client) StartAuthFlow(ctx context.Context, identifier string) (model.AuthFlow, error) {
	// The SDK's parse error repeats the input, which is kept out of the error, since the user may have
	// typed something private, such as an email address.
	atid, err := syntax.ParseAtIdentifier(strings.TrimSpace(identifier))
	if err != nil {
		return model.AuthFlow{}, fmt.Errorf("%w: identifier is neither a handle nor a DID", model.ErrorIdentityUnresolved)
	}
	flow := model.AuthFlow{Identifier: atid.String()}
	if did, err := atid.AsDID(); err == nil && did.Method() != "plc" && did.Method() != "web" {
		return flow, fmt.Errorf("%w: DID method %v is not supported", model.ErrorIdentityUnresolved, did.Method())
	}

	ident, err := c.lookupIdentity(ctx, atid)
	if err != nil {
		return flow, fmt.Errorf("%w: resolving %v: %w", identityLookupError(err), atid, err)
	}
	flow.DID = model.DID(ident.DID)
	flow.Handle = model.Handle(ident.Handle)
	pdsURL, err := url.Parse(ident.PDSEndpoint())
	if err != nil || pdsURL.Host == "" {
		return flow, fmt.Errorf("%w: %v has no PDS", model.ErrorIdentityUnresolved, ident.DID)
	}
	flow.PDSURL = pdsURL

	meta, err := c.discoverAuthServer(ctx, pdsURL)
	if err != nil {
		return flow, fmt.Errorf("%w: discovering auth server for %v: %w", model.ErrorAuthServerUnavailable, pdsURL, err)
	}
	authServerURL, err := url.Parse(meta.Issuer)
	if err != nil {
		return flow, fmt.Errorf("%w: parsing auth server issuer: %w", model.ErrorAuthServerUnavailable, err)
	}
	flow.AuthServerURL = authServerURL

	info, err := c.pushAuthRequest(ctx, meta, atid.String())
	if err != nil {
		return flow, fmt.Errorf("%w: pushing auth request to %v: %w", model.ErrorAuthServerUnavailable, meta.Issuer, err)
	}
	info.AccountDID = &ident.DID
	redirectURL, err := authorizationRedirectURL(meta.AuthorizationEndpoint, c.app.Config.ClientID, info.RequestURI)
	if err != nil {
		return flow, fmt.Errorf("%w: %w", model.ErrorAuthServerUnavailable, err)
	}

	if err := c.app.Store.SaveAuthRequestInfo(ctx, *info); err != nil {
		return flow, fmt.Errorf("saving auth request: %w", err)
	}

	flow.RedirectURL = redirectURL
	flow.State = model.OAuthState(info.State)
	return flow, nil
}

// authorizationRedirectURL to send the user to for consent: the authorization endpoint with the client
// ID and the pushed request's URI added to whatever query it already has.
func authorizationRedirectURL(endpoint, clientID, requestURI string) (*url.URL, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing authorization endpoint: %w", err)
	}
	query := u.Query()
	query.Set("client_id", clientID)
	query.Set("request_uri", requestURI)
	u.RawQuery = query.Encode()
	return u, nil
}

func (c *Client) lookupIdentity(ctx context.Context, atid syntax.AtIdentifier) (ident *identity.Identity, err error) {
	ctx, span := c.tracer.Start(ctx, "identity.lookup", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("atproto.identifier", atid.String())))
	defer func() { endSpan(span, err) }()

	return c.dir.Lookup(ctx, atid)
}

// identityLookupError classifies a failed identity lookup: [model.ErrorIdentityUnresolved] when the
// identifier is at fault, because nothing answers to it or it does not verify, and
// [model.ErrorIdentityUnavailable] for anything else, such as a resolver that failed or timed out.
func identityLookupError(err error) error {
	for _, identifierFault := range []error{
		identity.ErrHandleNotFound,
		identity.ErrDIDNotFound,
		identity.ErrInvalidHandle,
		identity.ErrHandleMismatch,
		identity.ErrHandleNotDeclared,
		identity.ErrHandleReservedTLD,
	} {
		if errors.Is(err, identifierFault) {
			return model.ErrorIdentityUnresolved
		}
	}
	return model.ErrorIdentityUnavailable
}

// discoverAuthServer for the PDS at the given URL: the protected resource document names the auth
// server, whose own metadata document has the endpoints.
func (c *Client) discoverAuthServer(ctx context.Context, pdsURL *url.URL) (meta *oauth.AuthServerMetadata, err error) {
	ctx, span := c.tracer.Start(ctx, "oauth.discover_auth_server", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(pdsURL.Host)))
	defer func() { endSpan(span, err) }()

	authServerURL, err := c.app.Resolver.ResolveAuthServerURL(ctx, pdsURL.String())
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.String("oauth.auth_server", hostOf(authServerURL)))

	return c.app.Resolver.ResolveAuthServerMetadata(ctx, authServerURL)
}

func (c *Client) pushAuthRequest(ctx context.Context, meta *oauth.AuthServerMetadata, loginHint string) (info *oauth.AuthRequestData, err error) {
	ctx, span := c.tracer.Start(ctx, "oauth.pushed_authorization_request", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(meta.Issuer))))
	defer func() { endSpan(span, err) }()

	return c.app.SendAuthRequest(ctx, meta, c.app.Config.Scopes, loginHint)
}

// ProcessCallback from the auth server, for the flow with the given state, which must be the state of
// the flow the same user started. It exchanges the code for tokens and persists the session, which is
// returned with the scopes the auth server granted. A denial or a token exchange spends the auth
// request: either way the auth server has concluded it.
//
// Errors are [model.ErrorLoginCancelled] when the callback is for another flow, is a denial, carries no
// code, or comes from another auth server than the flow was started with, and
// [model.ErrorAuthServerUnavailable] when the token exchange fails. Why a callback was refused lands on
// the span in the context as login.callback_reason, and a denial's error code and description as
// oauth.callback_error and oauth.callback_error_description.
func (c *Client) ProcessCallback(ctx context.Context, callback model.OAuthCallback, state model.OAuthState) (model.OAuthSession, error) {
	span := trace.SpanFromContext(ctx)
	refuse := func(reason string, err error) (model.OAuthSession, error) {
		span.SetAttributes(attribute.String("login.callback_reason", reason))
		return model.OAuthSession{}, fmt.Errorf("%w: %w", model.ErrorLoginCancelled, err)
	}

	if state == "" || callback.State != state {
		return refuse("state_mismatch", errors.New("callback state is not the flow's"))
	}
	info, err := c.app.Store.GetAuthRequestInfo(ctx, state.String())
	if err != nil {
		if errors.Is(err, model.ErrorOAuthAuthRequestNotFound) {
			return refuse("request_not_found", err)
		}
		return model.OAuthSession{}, fmt.Errorf("loading auth request: %w", err)
	}

	// A denial needs no exchange, but spends the auth request all the same.
	if callback.Error != "" {
		c.deleteAuthRequest(ctx, state)
		span.SetAttributes(attribute.String("oauth.callback_error", callback.Error))
		if callback.ErrorDescription != "" {
			span.SetAttributes(attribute.String("oauth.callback_error_description", callback.ErrorDescription))
		}
		return refuse("denied", fmt.Errorf("auth server denied the request with %v: %v", callback.Error, callback.ErrorDescription))
	}
	if callback.Code == "" {
		return refuse("no_code", fmt.Errorf("callback has no code from %v", info.AuthServerURL))
	}
	if callback.Issuer != info.AuthServerURL {
		return refuse("issuer_mismatch", fmt.Errorf("callback is from %v, not %v", callback.Issuer, info.AuthServerURL))
	}

	data, err := c.exchangeToken(ctx, info, callbackParams(callback))
	if err != nil {
		c.deleteAuthRequest(ctx, state)
		return model.OAuthSession{}, fmt.Errorf("%w: %w", model.ErrorAuthServerUnavailable, err)
	}
	sess, err := toSession(*data)
	if err != nil {
		return model.OAuthSession{}, fmt.Errorf("converting session: %w", err)
	}
	return sess, nil
}

// deleteAuthRequest for the given state, outliving a cancelled or expired context, since a spent auth
// request left behind would never be deleted. A failure lands on the span in the context as
// oauth.cleanup_error, leaving the callback's outcome as it is.
func (c *Client) deleteAuthRequest(ctx context.Context, state model.OAuthState) {
	if err := c.store.DeleteOAuthAuthRequest(context.WithoutCancel(ctx), state); err != nil {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("oauth.cleanup_error", "deleting spent auth request: "+err.Error()))
	}
}

// callbackParams of the callback, which is the form [oauth.ClientApp.ProcessCallback] takes.
func callbackParams(callback model.OAuthCallback) url.Values {
	return url.Values{
		"state":             {callback.State.String()},
		"code":              {callback.Code},
		"iss":               {callback.Issuer},
		"error":             {callback.Error},
		"error_description": {callback.ErrorDescription},
		"error_uri":         {callback.ErrorURI},
	}
}

func (c *Client) exchangeToken(ctx context.Context, info *oauth.AuthRequestData, params url.Values) (data *oauth.ClientSessionData, err error) {
	ctx, span := c.tracer.Start(ctx, "oauth.token_exchange", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(info.AuthServerURL))))
	defer func() { endSpan(span, err) }()

	return c.app.ProcessCallback(ctx, params)
}

// resumeSession of the account on one device from the store. It reads the store and parses the
// session's key, and calls no server.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
func (c *Client) resumeSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (*session, error) {
	sess, err := c.app.ResumeSession(ctx, syntax.DID(did), sessionID.String())
	if err != nil {
		return nil, fmt.Errorf("resuming OAuth session: %w", err)
	}
	return &session{client: c, sess: sess, api: sess.APIClient()}, nil
}

// CheckSession of the account on one device: that it is in the store and can be resumed. No server is
// called, so a session the auth server has since revoked still passes.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
func (c *Client) CheckSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	_, err := c.resumeSession(ctx, did, sessionID)
	return err
}

// GetRecord from the account's repository, as the account on the device with the given session, and
// whether it exists. Each call resumes the session afresh and persists any refreshed tokens and nonces
// for the next, so calls as the same session must not overlap.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
func (c *Client) GetRecord(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, rkey model.RecordKey) (map[string]any, bool, error) {
	sess, err := c.resumeSession(ctx, did, sessionID)
	if err != nil {
		return nil, false, err
	}
	return sess.GetRecord(ctx, collection, rkey)
}

// PutRecordIfMissing in the account's repository, as the account on the device with the given
// session, reporting whether this call created it. The write is conditional on the record's absence:
// a record that appeared in the meantime is left alone, which is not an error. As with
// [Client.GetRecord], calls as the same session must not overlap.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
func (c *Client) PutRecordIfMissing(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, rkey model.RecordKey, record map[string]any) (bool, error) {
	sess, err := c.resumeSession(ctx, did, sessionID)
	if err != nil {
		return false, err
	}
	return sess.PutRecordIfMissing(ctx, collection, rkey, record)
}

// DeleteSession of the account on one device from the store, without revoking its tokens. Deleting a
// session that does not exist is not an error.
func (c *Client) DeleteSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	if err := c.store.DeleteOAuthSession(ctx, did, sessionID); err != nil {
		return fmt.Errorf("deleting OAuth session: %w", err)
	}
	return nil
}

// Logout of the session: its tokens are revoked at the auth server, best effort, and the session is
// deleted from the store. Whether the revocation went through lands on the span in the context as
// oauth.revoked, and why it did not as oauth.revoke_error. The delete outlives a cancelled or expired
// context, so a revocation that uses up the deadline, or a context that is done before it starts,
// still ends with the session gone.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session, unless the context is
// already done: the session is then deleted unseen, and a missing one is not an error.
func (c *Client) Logout(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	span := trace.SpanFromContext(ctx)
	revoked := false
	sess, err := c.resumeSession(ctx, did, sessionID)
	switch {
	case errors.Is(err, model.ErrorOAuthSessionNotFound):
		return err
	case err != nil:
		span.SetAttributes(attribute.String("oauth.revoke_error", err.Error()))
	default:
		if err := sess.Revoke(ctx); err != nil {
			span.SetAttributes(attribute.String("oauth.revoke_error", err.Error()))
		} else {
			revoked = true
		}
	}
	span.SetAttributes(attribute.Bool("oauth.revoked", revoked))

	return c.DeleteSession(context.WithoutCancel(ctx), did, sessionID)
}

// ResolveHandle of the DID, bidirectionally verified: [model.HandleInvalid] when the account's
// declared handle does not point back at it.
func (c *Client) ResolveHandle(ctx context.Context, did model.DID) (model.Handle, error) {
	ident, err := c.lookupIdentity(ctx, syntax.DID(did).AtIdentifier())
	if err != nil {
		return "", fmt.Errorf("resolving %v: %w", did, err)
	}
	return model.Handle(ident.Handle), nil
}

// NewLocalHTTPClientOptions for [NewLocalHTTPClient].
type NewLocalHTTPClientOptions struct {
	// CAFile with an extra PEM root certificate to trust, if any.
	CAFile string
	// LocalHandleSuffix, such as ".test", whose hosts are dialed on loopback, if any.
	LocalHandleSuffix string
}

// NewLocalHTTPClient for a local network: trusting the extra root certificate in the CA file, if one
// is given, dialing hosts under the handle suffix and under .localhost on loopback whatever the
// system resolver says, and without SSRF protection.
func NewLocalHTTPClient(opts NewLocalHTTPClientOptions) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("loading system certificate pool: %w", err)
	}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificates in CA file " + opts.CAFile)
		}
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if host, port, err := net.SplitHostPort(addr); err == nil && (strings.HasSuffix(host, ".localhost") || (opts.LocalHandleSuffix != "" && strings.HasSuffix(host, opts.LocalHandleSuffix))) {
					addr = net.JoinHostPort("127.0.0.1", port)
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}, nil
}

// NewOAuthClientConfigOptions for [NewOAuthClientConfig].
type NewOAuthClientConfigOptions struct {
	// BaseURL of the app, which the client ID and callback URL are under. It must be absolute, with a
	// host; a trailing slash is ignored.
	BaseURL *url.URL
	// PrivateKeyMultibase is the P-256 client assertion key in multibase encoding. Required unless
	// BaseURL is a localhost URL.
	PrivateKeyMultibase string
	// KeyID names the key in the published JWKS. Required with PrivateKeyMultibase.
	KeyID string
}

// NewOAuthClientConfig for the app at the given base URL, requesting the scopes every login needs.
//
// A base URL on localhost or 127.0.0.1 gives the localhost development client the OAuth spec allows,
// which needs no key and no published metadata; its callback is on 127.0.0.1, since loopback redirect
// URIs may not use the localhost name. Any other base URL gives a confidential client, and the key is
// required.
func NewOAuthClientConfig(opts NewOAuthClientConfigOptions) (oauth.ClientConfig, error) {
	base, err := appBase(opts.BaseURL)
	if err != nil {
		return oauth.ClientConfig{}, err
	}

	if base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1" {
		callback := *base
		callback.Host = "127.0.0.1"
		if port := base.Port(); port != "" {
			callback.Host += ":" + port
		}
		callback.Path = base.Path + "/oauth/callback"
		callback.RawPath = ""
		config := oauth.NewLocalhostConfig(callback.String(), scopes)
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

	// The client ID path is the conventional one, so that PDS consent screens name the app by its host
	// instead of the full client ID URL. That only holds when the base URL has no port, no query and no path.
	config := oauth.NewPublicConfig(base.String()+"/oauth-client-metadata.json", base.String()+"/oauth/callback", scopes)
	config.UserAgent = "audioadastra"
	if err := config.SetClientSecret(key, opts.KeyID); err != nil {
		return oauth.ClientConfig{}, fmt.Errorf("setting OAuth client secret: %w", err)
	}
	return config, nil
}

// appBase is the app's base URL without a trailing slash, which the client ID, callback URL and JWKS
// URI are built on.
func appBase(baseURL *url.URL) (*url.URL, error) {
	if baseURL == nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("base URL %v is not an absolute URL with a host", baseURL)
	}
	base := *baseURL
	base.Path = strings.TrimSuffix(base.Path, "/")
	base.RawPath = strings.TrimSuffix(base.RawPath, "/")
	return &base, nil
}

// endSpan with the error recorded and the status set, when there is one.
func endSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// hostOf a URL string from the SDK, for attributes; the URL itself when it does not parse.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
