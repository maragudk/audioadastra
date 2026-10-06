package http

import (
	"context"
	"log/slog"

	. "maragu.dev/gomponents"

	"app/html"
	"app/model"
)

type handleResolver interface {
	ResolveHandle(ctx context.Context, did model.DID) (model.Handle, error)
}

// Profile page of the logged-in user, with the handle resolved and verified on each visit. A handle
// that fails to resolve is shown as "handle.invalid", as an unverified one is. The handler expects
// [GetUserFromContext] to return a user, so the route must be behind middleware that requires one.
func Profile(r *Router, log *slog.Logger, hr handleResolver) {
	r.Get("/profile", func(props html.PageProps) (Node, error) {
		user := GetUserFromContext(props.Ctx)

		handle, err := hr.ResolveHandle(props.Ctx, user.DID)
		if err != nil {
			log.WarnContext(props.Ctx, "Error resolving handle, rendering it as invalid", "error", err, "did", user.DID)
			handle = model.HandleInvalid
		}

		return html.ProfilePage(html.ProfilePageProps{
			PageProps: withTitle(props, "Profile"),
			Handle:    handle,
		}), nil
	})
}
