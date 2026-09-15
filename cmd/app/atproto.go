package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
)

// atprotoClientsOptions for [newAtprotoClients].
type atprotoClientsOptions struct {
	// PLCURL of a local PLC directory. When set, the identity directory resolves did:plc through it and
	// every atproto HTTP client goes without SSRF protection, since the local network is on loopback.
	// When empty, the real network is used with the protections on.
	PLCURL string
	// CAFile with an extra PEM root certificate to trust, such as the one a local reverse proxy issues
	// its certificates from.
	CAFile string
	// LocalHandleSuffix, such as ".test", of the local network's handles. Hosts under it are dialed on
	// loopback, since nothing resolves them, so handle verification over https reaches the local PDS.
	LocalHandleSuffix string
}

// atprotoClients the app talks to the atmosphere with.
type atprotoClients struct {
	directory identity.Directory
	// client for OAuth and XRPC requests when local, nil otherwise, which leaves the defaults in place.
	client *http.Client
	local  bool
}

// newAtprotoClients for the real network by default, or for a local one when a PLC URL is given.
func newAtprotoClients(opts atprotoClientsOptions) (atprotoClients, error) {
	if opts.PLCURL == "" {
		return atprotoClients{directory: identity.DefaultDirectory()}, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		return atprotoClients{}, fmt.Errorf("loading system certificate pool: %w", err)
	}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return atprotoClients{}, fmt.Errorf("reading CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return atprotoClients{}, errors.New("no certificates in CA file " + opts.CAFile)
		}
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if host, port, err := net.SplitHostPort(addr); err == nil && opts.LocalHandleSuffix != "" && strings.HasSuffix(host, opts.LocalHandleSuffix) {
					addr = net.JoinHostPort("127.0.0.1", port)
				}
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}

	base := &identity.BaseDirectory{
		PLCURL:     opts.PLCURL,
		HTTPClient: *client,
		PLCClient:  client,
		Resolver:   net.Resolver{},
		UserAgent:  "audioadastra",
	}
	return atprotoClients{
		directory: identity.NewCacheDirectory(base, 1000, time.Hour, time.Minute, time.Minute),
		client:    client,
		local:     true,
	}, nil
}
