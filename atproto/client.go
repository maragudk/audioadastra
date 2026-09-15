// Package atproto composes the clients the app talks to the atmosphere with: the OAuth client app,
// its configuration, and the identity directory, for either the real network or a local one.
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
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"

	"app/model"
)

// scopes every login requests, and which every session must have been granted in full. They are
// asked for up front so later features do not send the user back to consent.
//
// The repo scope names the profile collection explicitly: the permission syntax has no partial
// wildcard, so "repo:com.audioadastra.*" is not a valid scope.
var scopes = []string{"atproto", "repo:" + model.CollectionActorProfile, "blob:audio/*", "blob:image/*"}

// Client is the set of clients for one network, built once by [New].
type Client struct {
	// OAuth client app, whose Config is the client's OAuth configuration.
	OAuth *oauth.ClientApp
	// Directory to resolve identities with, which the OAuth client app also uses.
	Directory identity.Directory
	// Local reports whether a local network is in use, without SSRF protection.
	Local bool
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
	Store oauth.ClientAuthStore

	// PLCURL of a local PLC directory. When set, the identity directory resolves did:plc through it and
	// every HTTP client goes without SSRF protection, since the local network is on loopback. When
	// empty, the real network is used with the protections on.
	PLCURL string
	// CAFile with an extra PEM root certificate to trust, such as the one a local reverse proxy issues
	// its certificates from.
	CAFile string
	// LocalHandleSuffix, such as ".test", of the local network's handles. Hosts under it are dialed on
	// loopback, since nothing resolves them, so handle verification over https reaches the local PDS.
	LocalHandleSuffix string
}

// New client for the real network by default, or for a local one when a PLC URL is given.
func New(opts NewOptions) (*Client, error) {
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

	app := oauth.NewClientApp(&config, opts.Store)
	c := &Client{OAuth: app, Directory: identity.DefaultDirectory()}
	if opts.PLCURL == "" {
		app.Dir = c.Directory
		return c, nil
	}

	httpClient, err := newLocalHTTPClient(opts.CAFile, opts.LocalHandleSuffix)
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
	c.Directory = identity.NewCacheDirectory(base, 1000, time.Hour, time.Minute, time.Minute)
	c.Local = true
	app.Dir = c.Directory
	app.Client = httpClient
	app.Resolver.Client = httpClient
	return c, nil
}

// newLocalHTTPClient trusting an extra CA, dialing hosts under the handle suffix on loopback, and
// without SSRF protection.
func newLocalHTTPClient(caFile, localHandleSuffix string) (*http.Client, error) {
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
				if host, port, err := net.SplitHostPort(addr); err == nil && localHandleSuffix != "" && strings.HasSuffix(host, localHandleSuffix) {
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
