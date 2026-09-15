package http

import (
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"maragu.dev/glue/http"

	"app/service"
)

func InjectHTTPRouter(log *slog.Logger, svc *service.Fat, oauthConfig *oauth.ClientConfig, baseURL string) func(*Router) {
	return func(r *Router) {
		r.Use(AddUserToContext(log, svc, r.SM, svc, svc))

		OAuthMetadata(r, log, oauthConfig, baseURL)

		r.Group(func(r *http.Router) {
			Home(r, log)
			Login(r, log, svc, r.SM)
			// The router already has a POST /logout from the server's own setup, registered before this
			// injector runs; registering the pattern again replaces that handler with this one, which
			// also ends the OAuth session.
			Logout(r, log, svc, r.SM)
		})
	}
}
