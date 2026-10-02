package http

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
)

type oauthDocumenter interface {
	OAuthClientMetadata() any
	OAuthJWKS() any
}

// OAuthMetadata documents: the client metadata the client ID points at, and the JWKS with the public
// half of the client assertion key. Auth servers fetch both, so they are public and unauthenticated.
// A localhost client needs neither, and its JWKS is an empty key set.
func OAuthMetadata(r *Router, log *slog.Logger, docs oauthDocumenter) {
	r.Mux.Get("/oauth/client-metadata.json", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, req, log, docs.OAuthClientMetadata())
	})

	r.Mux.Get("/oauth/jwks.json", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, req, log, docs.OAuthJWKS())
	})
}

func writeJSON(w http.ResponseWriter, req *http.Request, log *slog.Logger, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, v); err != nil {
		log.ErrorContext(req.Context(), "Error writing JSON", "error", err)
	}
}
