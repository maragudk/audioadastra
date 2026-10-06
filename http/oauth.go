package http

import (
	"encoding/json/v2"
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

type oauthDocumenter interface {
	OAuthClientMetadata() any
	OAuthJWKS() any
}

// OAuthMetadata documents: the client metadata the client ID points at, and the JWKS with the public
// half of the client assertion key. Auth servers fetch both, so they are public and unauthenticated.
// A localhost client needs neither, and its JWKS is an empty key set.
func OAuthMetadata(r *Router, docs oauthDocumenter) {
	r.Mux.Get("/oauth-client-metadata.json", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, req, docs.OAuthClientMetadata())
	})

	r.Mux.Get("/oauth/jwks.json", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, req, docs.OAuthJWKS())
	})
}

// writeJSON of the value, recording a failure to write it on the span in the request's context.
func writeJSON(w http.ResponseWriter, req *http.Request, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.MarshalWrite(w, v); err != nil {
		trace.SpanFromContext(req.Context()).RecordError(err)
	}
}
