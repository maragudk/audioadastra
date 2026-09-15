package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	gluehttp "maragu.dev/glue/http"
	. "maragu.dev/gomponents"

	"app/html"
	"app/model"
)

type handleResolver interface {
	ResolveHandle(ctx context.Context, did model.DID) (string, error)
}

// Profile page of the logged-in user, with the handle resolved and verified on each visit. A handle
// that fails to resolve is shown as "handle.invalid", as an unverified one is.
func Profile(r *Router, log *slog.Logger, hr handleResolver) {
	r.Group(func(r *Router) {
		r.Use(requireUser)

		r.Get("/profile", func(props html.PageProps) (Node, error) {
			user := GetUserFromContext(props.Ctx)

			handle, err := hr.ResolveHandle(props.Ctx, user.DID)
			if err != nil {
				log.WarnContext(props.Ctx, "Error resolving handle, rendering it as invalid", "error", err, "did", user.DID)
				handle = syntax.HandleInvalid.String()
			}

			return html.ProfilePage(html.ProfilePageProps{
				PageProps: withTitle(props, "Profile"),
				Handle:    handle,
			}), nil
		})
	})
}

// requireUser is [gluehttp.Middleware] sending a request without a logged-in user to the login page,
// which brings the user back to the requested path afterwards.
func requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gluehttp.GetUserIDFromContext(r.Context()) == nil {
			http.Redirect(w, r, "/login?redirect="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
