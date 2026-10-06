package http

import (
	"log/slog"

	"maragu.dev/glue/http"

	"app/model"
	"app/service"
)

func InjectHTTPRouter(log *slog.Logger, svc *service.Fat) func(*Router) {
	return func(r *Router) {
		r.Use(AddUserToContext(log, svc, r.SM, svc))

		OAuthMetadata(r, log, svc)

		r.Group(func(r *http.Router) {
			Home(r, log)
			Login(r, log, svc, r.SM)
			// The router already has a POST /logout from the server's own setup, registered before this
			// injector runs; registering the pattern again replaces that handler with this one, which
			// also ends the OAuth session.
			Logout(r, log, svc, r.SM)

			r.Group(func(r *http.Router) {
				r.Use(http.Authorize(log, svc, model.PermissionView))

				Profile(r, log, svc)
			})
		})
	}
}
