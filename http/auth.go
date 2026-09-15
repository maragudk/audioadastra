package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atclient"
	gluehttp "maragu.dev/glue/http"

	"app/model"
)

const contextUserKey = gluehttp.ContextKey("user")
const contextOAuthSessionIDKey = gluehttp.ContextKey("oauthSessionID")

// SessionOAuthSessionIDKey is the cookie session key holding the ID of the OAuth session the login
// established, next to [gluehttp.SessionUserIDKey].
const SessionOAuthSessionIDKey = "oauthSessionID"

type userGetter interface {
	GetUser(ctx context.Context, id model.UserID) (model.User, error)
}

type sessionManager interface {
	Destroy(ctx context.Context) error
	GetString(ctx context.Context, key string) string
}

type pdsClientGetter interface {
	PDSClient(ctx context.Context, did model.DID, sessionID string) (*atclient.APIClient, error)
}

// AddUserToContext is [gluehttp.Middleware] to add an authenticated user and the ID of their OAuth
// session to the request context, if the user ID is available in the request context.
//
// A cookie session whose OAuth session no longer exists is destroyed and the request redirected to the
// login page, so a cookie cannot outlive the OAuth session it was issued for.
func AddUserToContext(log *slog.Logger, ug userGetter, sm sessionManager, pg pdsClientGetter) gluehttp.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			userID := gluehttp.GetUserIDFromContext(ctx)
			if userID == nil {
				next.ServeHTTP(w, r)
				return
			}

			user, err := ug.GetUser(ctx, *userID)
			if err != nil {
				log.ErrorContext(ctx, "Error getting user from context", "error", err)
				http.Error(w, "error getting user from context", http.StatusBadGateway)
				return
			}

			sessionID := sm.GetString(ctx, SessionOAuthSessionIDKey)
			if _, err := pg.PDSClient(ctx, user.DID, sessionID); err != nil {
				if !errors.Is(err, model.ErrorOAuthSessionNotFound) {
					log.ErrorContext(ctx, "Error resuming OAuth session", "error", err, "userID", user.ID)
					http.Error(w, "error resuming OAuth session", http.StatusInternalServerError)
					return
				}
				log.InfoContext(ctx, "Destroying session without an OAuth session", "userID", user.ID)
				if err := sm.Destroy(ctx); err != nil {
					log.ErrorContext(ctx, "Error destroying session", "error", err, "userID", user.ID)
					http.Error(w, "error destroying session", http.StatusInternalServerError)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx = context.WithValue(ctx, contextUserKey, &user)
			ctx = context.WithValue(ctx, contextOAuthSessionIDKey, sessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserFromContext, which is nil when the request is not authenticated.
func GetUserFromContext(ctx context.Context) *model.User {
	user, _ := ctx.Value(contextUserKey).(*model.User)
	return user
}

// GetOAuthSessionIDFromContext, which is empty when the request is not authenticated.
func GetOAuthSessionIDFromContext(ctx context.Context) string {
	sessionID, _ := ctx.Value(contextOAuthSessionIDKey).(string)
	return sessionID
}
