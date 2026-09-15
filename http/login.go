package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	gluehttp "maragu.dev/glue/http"
	. "maragu.dev/gomponents"

	"app/html"
	"app/model"
	"app/service"
)

type loginStarter interface {
	StartLogin(ctx context.Context, identifier string) (service.LoginStart, error)
}

type loginFinisher interface {
	FinishLogin(ctx context.Context, params url.Values, state string) (model.User, string, error)
}

type loginStarterFinisher interface {
	loginStarter
	loginFinisher
}

type loginSessionManager interface {
	Put(ctx context.Context, key string, val any)
	PopString(ctx context.Context, key string) string
	Remove(ctx context.Context, key string)
	RenewToken(ctx context.Context) error
}

// Login pages and the OAuth callback. Logged-in users are sent to the front page instead, whichever
// of them they hit.
//
// The cookie session carries the flow between the form post and the callback: the state of the flow,
// so only the browser that started a flow can finish it, and where to send the user afterwards.
func Login(r *Router, log *slog.Logger, svc loginStarterFinisher, sm loginSessionManager) {
	r.Group(func(r *Router) {
		r.Use(gluehttp.RedirectIfAuthenticated("/"))

		r.Get("/login", func(props html.PageProps) (Node, error) {
			return html.LoginPage(html.LoginPageProps{
				PageProps: withTitle(props, "Log in"),
				Redirect:  localPath(props.R.URL.Query().Get("redirect")),
			}), nil
		})
	})

	r.Post("/login", func(props html.PageProps) (Node, error) {
		if props.UserID != nil {
			http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
			return nil, nil
		}

		handle := props.R.FormValue("handle")
		redirect := localPath(props.R.FormValue("redirect"))

		start, err := svc.StartLogin(props.Ctx, handle)
		if err != nil {
			return loginErrorPage(props, handle, redirect, err)
		}

		sm.Put(props.Ctx, "loginState", start.State)
		if redirect != "" {
			sm.Put(props.Ctx, "loginRedirect", redirect)
		} else {
			sm.Remove(props.Ctx, "loginRedirect")
		}
		http.Redirect(props.W, props.R, start.RedirectURL, http.StatusSeeOther)
		return nil, nil
	})

	r.Get("/oauth/callback", func(props html.PageProps) (Node, error) {
		if props.UserID != nil {
			http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
			return nil, nil
		}

		// A new token before the session is elevated, so a session fixated before login is worthless
		// after, and before the login is finished, so a failure here leaves no OAuth session behind.
		if err := sm.RenewToken(props.Ctx); err != nil {
			log.ErrorContext(props.Ctx, "Error renewing session token before login", "error", err)
			return html.ErrorPage(), err
		}
		state := sm.PopString(props.Ctx, "loginState")
		redirect := localPath(sm.PopString(props.Ctx, "loginRedirect"))

		user, sessionID, err := svc.FinishLogin(props.Ctx, props.R.URL.Query(), state)
		if err != nil {
			return loginErrorPage(props, "", "", err)
		}

		sm.Put(props.Ctx, gluehttp.SessionUserIDKey, string(user.ID))
		sm.Put(props.Ctx, SessionOAuthSessionIDKey, sessionID)

		if redirect == "" {
			redirect = "/"
		}
		http.Redirect(props.W, props.R, redirect, http.StatusSeeOther)
		return nil, nil
	})
}

// loginErrorPage for a failed login: a generic message per known refusal, never the auth server's own
// words, and the error page with a 500 for anything unknown.
func loginErrorPage(props html.PageProps, handle, redirect string, err error) (Node, error) {
	var message string
	var code int
	switch {
	case errors.Is(err, model.ErrorIdentityUnresolved):
		message, code = "That does not look like an account we can find. Check the handle and try again.", http.StatusBadRequest
	case errors.Is(err, model.ErrorAuthServerUnavailable):
		message, code = "Your account's server could not complete the login. Try again in a little while.", http.StatusBadGateway
	case errors.Is(err, model.ErrorLoginCancelled):
		message, code = "The login was cancelled or failed. Try again.", http.StatusBadRequest
	case errors.Is(err, model.ErrorScopeDenied):
		message, code = "The login did not grant everything the app needs. Try again and approve all permissions.", http.StatusBadRequest
	case errors.Is(err, model.ErrorUserInactive):
		message, code = "This account is not active here.", http.StatusForbidden
	case errors.Is(err, model.ErrorProfileWriteFailed):
		message, code = "Your profile could not be set up on your account's server. Try again in a little while.", http.StatusBadGateway
	default:
		return html.ErrorPage(), err
	}

	return html.LoginPage(html.LoginPageProps{
		PageProps: withTitle(props, "Log in"),
		Handle:    handle,
		Redirect:  redirect,
		Error:     message,
	}), gluehttp.Error{Code: code, Err: err}
}

type logouter interface {
	Logout(ctx context.Context, did model.DID, sessionID string) error
}

type sessionDestroyer interface {
	Destroy(ctx context.Context) error
}

// Logout the current user: the OAuth session is revoked and deleted, best effort, and the cookie
// session is destroyed either way.
func Logout(r *Router, log *slog.Logger, svc logouter, sm sessionDestroyer) {
	r.Post("/logout", func(props html.PageProps) (Node, error) {
		user := GetUserFromContext(props.Ctx)
		if user == nil {
			http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
			return nil, nil
		}

		if err := svc.Logout(props.Ctx, user.DID, GetOAuthSessionIDFromContext(props.Ctx)); err != nil && !errors.Is(err, model.ErrorOAuthSessionNotFound) {
			log.ErrorContext(props.Ctx, "Error logging out of OAuth session, destroying cookie session anyway", "error", err, "userID", user.ID)
		}

		if err := sm.Destroy(props.Ctx); err != nil {
			log.ErrorContext(props.Ctx, "Error destroying session", "error", err, "userID", user.ID)
			return html.ErrorPage(), err
		}

		http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
		return nil, nil
	})
}

// localPath from a redirect parameter: a path on this site, or empty. Anything with a scheme or host,
// including protocol-relative "//host", is dropped so a login link cannot send users elsewhere.
func localPath(redirect string) string {
	if !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") || strings.HasPrefix(redirect, "/\\") {
		return ""
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	return redirect
}

func withTitle(props html.PageProps, title string) html.PageProps {
	props.Title = title
	return props
}
