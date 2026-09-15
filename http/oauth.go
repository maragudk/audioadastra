package http

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

// OAuthMetadata documents: the client metadata the client ID points at, and the JWKS with the public
// half of the client assertion key. Auth servers fetch both, so they are public and unauthenticated.
// A localhost client needs neither, and its JWKS is an empty key set.
//
// The base URL is taken without a trailing slash, as the client ID and callback URL in the config are
// derived from it, so the URLs in the document agree with the one the document is served at.
func OAuthMetadata(r *Router, log *slog.Logger, config *oauth.ClientConfig, baseURL string) {
	baseURL = strings.TrimSuffix(baseURL, "/")

	r.Mux.Get("/oauth/client-metadata.json", func(w http.ResponseWriter, req *http.Request) {
		meta := config.ClientMetadata()
		meta.ClientName = new("Audio Ad Astra")
		meta.ClientURI = new(baseURL)
		if config.IsConfidential() {
			meta.JWKSURI = new(baseURL + "/oauth/jwks.json")
		}
		writeJSON(w, req, log, meta)
	})

	r.Mux.Get("/oauth/jwks.json", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, req, log, config.PublicJWKS())
	})
}

func writeJSON(w http.ResponseWriter, req *http.Request, log *slog.Logger, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, v); err != nil {
		log.ErrorContext(req.Context(), "Error writing JSON", "error", err)
	}
}
