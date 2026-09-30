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
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
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
var scopes = []string{"atproto", "repo:" + model.CollectionActorProfile, "blob:audio/*", "blob:image/*"}

// Client for one network, built once by [New]. Safe for concurrent use.
type Client struct {
	app    *oauth.ClientApp
	dir    identity.Directory
	store  Store
	log    *slog.Logger
	tracer trace.Tracer
	local  bool
}

// NewOptions for [New].
type NewOptions struct {
	// BaseURL of the app, which the client ID and callback URL are under.
	BaseURL string
	// PrivateKeyMultibase is the P-256 client assertion key in multibase encoding. Required unless
	// BaseURL is a localhost URL.
	PrivateKeyMultibase string
	// KeyID names the key in the published JWKS. Required with PrivateKeyMultibase.
	KeyID string
	// Store for auth requests and sessions.
	Store Store
	// Log for what happens on the way, such as a revocation that failed. A nil Log discards.
	Log *slog.Logger

	// PLCURL of a local PLC directory. When set, the identity directory resolves did:plc through it and
	// every HTTP client goes without SSRF protection, since the local network is on loopback. When
	// empty, the real network is used with the protections on.
	PLCURL string
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

// New client for the real network by default, or for a local one when a PLC URL is given.
func New(opts NewOptions) (*Client, error) {
	if opts.Store == nil {
		return nil, errors.New("a store is required")
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}

	config, err := NewOAuthClientConfig(NewOAuthClientConfigOptions{
		BaseURL:             opts.BaseURL,
		PrivateKeyMultibase: opts.PrivateKeyMultibase,
		KeyID:               opts.KeyID,
	})
	if err != nil {
		return nil, fmt.Errorf("configuring OAuth client: %w", err)
	}

	c := &Client{
		app:    oauth.NewClientApp(&config, &clientAuthStore{store: opts.Store}),
		dir:    identity.DefaultDirectory(),
		store:  opts.Store,
		log:    opts.Log,
		tracer: otel.Tracer("app/atproto"),
	}

	if opts.PLCURL != "" {
		httpClient, err := NewLocalHTTPClient(opts.CAFile, opts.LocalHandleSuffix)
		if err != nil {
			return nil, err
		}
		base := &identity.BaseDirectory{
			PLCURL:     opts.PLCURL,
			HTTPClient: *httpClient,
			PLCClient:  httpClient,
			Resolver:   net.Resolver{},
			UserAgent:  "audioadastra",
		}
		c.dir = identity.NewCacheDirectory(base, 1000, time.Hour, time.Minute, time.Minute)
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

// CheckScopes that were granted against those every login requests, comparing parsed permissions
// rather than strings so an auth server that normalizes its scope strings still passes.
//
// The error is [model.ErrorScopeDenied] when any requested scope is missing.
func (c *Client) CheckScopes(granted []string) error {
	grantedPermissions, err := auth.ParseOAuthScope(strings.Join(granted, " "))
	if err != nil {
		return fmt.Errorf("%w: %w", model.ErrorScopeDenied, err)
	}
	have := map[string]bool{}
	for _, p := range grantedPermissions {
		have[p.ScopeString()] = true
	}

	for _, scope := range c.app.Config.Scopes {
		if scope == "atproto" {
			continue
		}
		required, err := auth.ParsePermissionString(scope)
		if err != nil {
			panic("atproto: requested OAuth scope " + scope + " does not parse: " + err.Error())
		}
		if !have[required.ScopeString()] {
			return fmt.Errorf("%w: %v not granted", model.ErrorScopeDenied, scope)
		}
	}
	return nil
}

// ClientMetadata document, for serving at the client ID as JSON. The base URL is taken without a
// trailing slash, as the client ID and callback URL are derived from it.
func (c *Client) ClientMetadata(baseURL string) any {
	baseURL = strings.TrimSuffix(baseURL, "/")

	meta := c.app.Config.ClientMetadata()
	meta.ClientName = new("Audio Ad Astra")
	meta.ClientURI = new(baseURL)
	if c.Confidential() {
		meta.JWKSURI = new(baseURL + "/oauth/jwks.json")
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
// account on a PDS, and [model.ErrorAuthServerUnavailable] when the auth server cannot be discovered
// or refuses the request.
func (c *Client) StartAuthFlow(ctx context.Context, identifier string) (model.AuthFlow, error) {
	atid, err := syntax.ParseAtIdentifier(strings.TrimSpace(identifier))
	if err != nil {
		return model.AuthFlow{}, fmt.Errorf("%w: parsing identifier: %w", model.ErrorIdentityUnresolved, err)
	}

	ident, err := c.lookupIdentity(ctx, atid)
	if err != nil {
		return model.AuthFlow{}, fmt.Errorf("%w: resolving %v: %w", model.ErrorIdentityUnresolved, atid, err)
	}
	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return model.AuthFlow{}, fmt.Errorf("%w: %v has no PDS", model.ErrorIdentityUnresolved, ident.DID)
	}
	flow := model.AuthFlow{
		DID:     model.DID(ident.DID),
		Handle:  model.Handle(ident.Handle),
		PDSHost: hostOf(pdsURL),
	}

	meta, err := c.discoverAuthServer(ctx, pdsURL)
	if err != nil {
		return flow, fmt.Errorf("%w: discovering auth server for %v: %w", model.ErrorAuthServerUnavailable, pdsURL, err)
	}
	flow.AuthServerHost = hostOf(meta.Issuer)

	info, err := c.pushAuthRequest(ctx, meta, atid.String())
	if err != nil {
		return flow, fmt.Errorf("%w: pushing auth request to %v: %w", model.ErrorAuthServerUnavailable, meta.Issuer, err)
	}
	info.AccountDID = &ident.DID

	if err := c.app.Store.SaveAuthRequestInfo(ctx, *info); err != nil {
		return flow, fmt.Errorf("saving auth request: %w", err)
	}

	params := url.Values{}
	params.Set("client_id", c.app.Config.ClientID)
	params.Set("request_uri", info.RequestURI)
	flow.RedirectURL = meta.AuthorizationEndpoint + "?" + params.Encode()
	flow.State = info.State
	return flow, nil
}

func (c *Client) lookupIdentity(ctx context.Context, atid syntax.AtIdentifier) (ident *identity.Identity, err error) {
	ctx, span := c.tracer.Start(ctx, "identity.lookup", trace.WithSpanKind(trace.SpanKindClient))
	defer func() { endSpan(span, err) }()

	return c.dir.Lookup(ctx, atid)
}

// discoverAuthServer for the PDS at the given URL: the protected resource document names the auth
// server, whose own metadata document has the endpoints.
func (c *Client) discoverAuthServer(ctx context.Context, pdsURL string) (meta *oauth.AuthServerMetadata, err error) {
	ctx, span := c.tracer.Start(ctx, "oauth.discover_auth_server", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(pdsURL))))
	defer func() { endSpan(span, err) }()

	authServerURL, err := c.app.Resolver.ResolveAuthServerURL(ctx, pdsURL)
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

// ProcessCallback with the query parameters the auth server sent back, for the flow with the given
// state, which must be the state of the flow the same user started. It exchanges the code for tokens
// and persists the session, which is returned with the scopes the auth server granted. The auth
// request is spent either way, since its code was single use.
//
// Errors are [model.ErrorLoginCancelled] when the callback is for another flow, carries no code, or
// comes from another auth server than the flow was started with, and [model.ErrorAuthServerUnavailable]
// when the token exchange fails. A denial's error code lands on the span in the context as
// oauth.callback_error.
func (c *Client) ProcessCallback(ctx context.Context, params url.Values, state string) (model.OAuthSession, error) {
	if state == "" || params.Get("state") != state {
		return model.OAuthSession{}, fmt.Errorf("%w: callback state is not the flow's", model.ErrorLoginCancelled)
	}
	info, err := c.app.Store.GetAuthRequestInfo(ctx, state)
	if err != nil {
		if errors.Is(err, model.ErrorOAuthAuthRequestNotFound) {
			return model.OAuthSession{}, fmt.Errorf("%w: %w", model.ErrorLoginCancelled, err)
		}
		return model.OAuthSession{}, fmt.Errorf("loading auth request: %w", err)
	}
	// An error response is left for the token exchange to classify; a success response must carry a
	// code from the auth server the flow was started with.
	if params.Get("error") == "" && (params.Get("code") == "" || params.Get("iss") != info.AuthServerURL) {
		return model.OAuthSession{}, fmt.Errorf("%w: callback has no code from %v", model.ErrorLoginCancelled, info.AuthServerURL)
	}

	data, err := c.exchangeToken(ctx, info, params)
	if err != nil {
		if deleteErr := c.store.DeleteOAuthAuthRequest(context.WithoutCancel(ctx), state); deleteErr != nil {
			c.log.ErrorContext(ctx, "Error deleting auth request after failed token exchange", "error", deleteErr, "state", state)
		}
		var callbackErr *oauth.AuthRequestCallbackError
		if errors.As(err, &callbackErr) {
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("oauth.callback_error", callbackErr.ErrorCode))
			return model.OAuthSession{}, fmt.Errorf("%w: %w", model.ErrorLoginCancelled, err)
		}
		return model.OAuthSession{}, fmt.Errorf("%w: %w", model.ErrorAuthServerUnavailable, err)
	}
	return toSession(*data), nil
}

func (c *Client) exchangeToken(ctx context.Context, info *oauth.AuthRequestData, params url.Values) (data *oauth.ClientSessionData, err error) {
	ctx, span := c.tracer.Start(ctx, "oauth.token_exchange", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(semconv.ServerAddress(hostOf(info.AuthServerURL))))
	defer func() { endSpan(span, err) }()

	return c.app.ProcessCallback(ctx, params)
}

// ResumeSession of the account on one device from the store.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
func (c *Client) ResumeSession(ctx context.Context, did model.DID, sessionID string) (Session, error) {
	sess, err := c.app.ResumeSession(ctx, syntax.DID(did), sessionID)
	if err != nil {
		return nil, fmt.Errorf("resuming OAuth session: %w", err)
	}
	return &session{client: c, sess: sess, api: sess.APIClient()}, nil
}

// DeleteSession of the account on one device from the store, without revoking its tokens. Deleting a
// session that does not exist is not an error.
func (c *Client) DeleteSession(ctx context.Context, did model.DID, sessionID string) error {
	if err := c.store.DeleteOAuthSession(ctx, did, sessionID); err != nil {
		return fmt.Errorf("deleting OAuth session: %w", err)
	}
	return nil
}

// Logout of the session: its tokens are revoked at the auth server, best effort, and the session is
// deleted from the store. Whether the revocation went through lands on the span in the context as
// oauth.revoked. The delete outlives a cancelled or expired context, so a revocation that uses up the
// deadline, or a context that is done before it starts, still ends with the session gone.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session, unless the context is
// already done: the session is then deleted unseen, and a missing one is not an error.
func (c *Client) Logout(ctx context.Context, did model.DID, sessionID string) error {
	revoked := false
	sess, err := c.ResumeSession(ctx, did, sessionID)
	switch {
	case errors.Is(err, model.ErrorOAuthSessionNotFound):
		return err
	case err != nil:
		c.log.WarnContext(ctx, "Error resuming OAuth session to revoke its tokens at logout", "error", err, "did", did)
	default:
		if err := sess.Revoke(ctx); err != nil {
			c.log.WarnContext(ctx, "Error revoking OAuth tokens at logout", "error", err, "did", did)
		} else {
			revoked = true
		}
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("oauth.revoked", revoked))

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

// NewLocalHTTPClient for a local network: trusting the extra root certificate in the CA file, if one
// is given, dialing hosts under the handle suffix and under .localhost on loopback whatever the
// system resolver says, and without SSRF protection.
func NewLocalHTTPClient(caFile, localHandleSuffix string) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("loading system certificate pool: %w", err)
	}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificates in CA file " + caFile)
		}
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if host, port, err := net.SplitHostPort(addr); err == nil && (strings.HasSuffix(host, ".localhost") || (localHandleSuffix != "" && strings.HasSuffix(host, localHandleSuffix))) {
					addr = net.JoinHostPort("127.0.0.1", port)
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}, nil
}

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

// NewOAuthClientConfig for the app at the given base URL, requesting the scopes every login needs.
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

	config := oauth.NewPublicConfig(base.String()+"/oauth/client-metadata.json", base.String()+"/oauth/callback", scopes)
	config.UserAgent = "audioadastra"
	if err := config.SetClientSecret(key, opts.KeyID); err != nil {
		return oauth.ClientConfig{}, fmt.Errorf("setting OAuth client secret: %w", err)
	}
	return config, nil
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
