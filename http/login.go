package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	gluehttp "maragu.dev/glue/http"
	. "maragu.dev/gomponents"

	"app/html"
	"app/model"
)

type loginStarter interface {
	StartLogin(ctx context.Context, identifier string) (model.LoginStart, error)
}

type loginFinisher interface {
	FinishLogin(ctx context.Context, callback model.OAuthCallback, state model.OAuthState) (model.User, model.OAuthSessionID, error)
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
func Login(r *Router, svc loginStarterFinisher, sm loginSessionManager) {
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
			redirectLoggedIn(props)
			return nil, nil
		}

		identifier := props.R.FormValue("handle")
		redirect := localPath(props.R.FormValue("redirect"))

		start, err := svc.StartLogin(props.Ctx, identifier)
		if err != nil {
			return loginErrorPage(props, identifier, redirect, err)
		}

		sm.Put(props.Ctx, "loginState", start.State.String())
		if redirect != "" {
			sm.Put(props.Ctx, "loginRedirect", redirect)
		} else {
			sm.Remove(props.Ctx, "loginRedirect")
		}
		http.Redirect(props.W, props.R, start.RedirectURL.String(), http.StatusSeeOther)
		return nil, nil
	})

	r.Get("/oauth/callback", func(props html.PageProps) (Node, error) {
		if props.UserID != nil {
			redirectLoggedIn(props)
			return nil, nil
		}

		// A new token before the session is elevated, so a session fixated before login is worthless
		// after, and before the login is finished, so a failure here leaves no OAuth session behind.
		if err := sm.RenewToken(props.Ctx); err != nil {
			trace.SpanFromContext(props.Ctx).RecordError(err)
			return html.ErrorPage(props), err
		}
		state := model.OAuthState(sm.PopString(props.Ctx, "loginState"))
		redirect := localPath(sm.PopString(props.Ctx, "loginRedirect"))

		query := props.R.URL.Query()
		callback := model.OAuthCallback{
			State:            model.OAuthState(query.Get("state")),
			Code:             query.Get("code"),
			Issuer:           query.Get("iss"),
			Error:            query.Get("error"),
			ErrorDescription: query.Get("error_description"),
			ErrorURI:         query.Get("error_uri"),
		}
		user, sessionID, err := svc.FinishLogin(props.Ctx, callback, state)
		if err != nil {
			return loginErrorPage(props, "", "", err)
		}

		sm.Put(props.Ctx, gluehttp.SessionUserIDKey, string(user.ID))
		sm.Put(props.Ctx, SessionOAuthSessionIDKey, sessionID.String())

		if redirect == "" {
			redirect = "/"
		}
		http.Redirect(props.W, props.R, redirect, http.StatusSeeOther)
		return nil, nil
	})
}

// redirectLoggedIn to the front page, a user who is already logged in, marking the span in the context
// with login.condition already_logged_in so the redirect is not taken for a completed login.
func redirectLoggedIn(props html.PageProps) {
	trace.SpanFromContext(props.Ctx).SetAttributes(attribute.String("login.condition", "already_logged_in"))
	http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
}

// loginErrorPage for a failed login: a generic message per known refusal, never the auth server's own
// words, and the error page with a 500 for anything unknown.
func loginErrorPage(props html.PageProps, identifier, redirect string, err error) (Node, error) {
	var message string
	var code int
	switch {
	case errors.Is(err, model.ErrorIdentityUnresolved):
		message, code = "That does not look like an account we can find. Check the handle and try again.", http.StatusBadRequest
	case errors.Is(err, model.ErrorIdentityUnavailable):
		message, code = "We couldn't look up your account right now. Try again in a little while.", http.StatusBadGateway
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
		return html.ErrorPage(props), err
	}

	return html.LoginPage(html.LoginPageProps{
		PageProps:  withTitle(props, "Log in"),
		Identifier: identifier,
		Redirect:   redirect,
		Error:      message,
	}), gluehttp.Error{Code: code, Err: err}
}

type logouter interface {
	Logout(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error
}

type sessionDestroyer interface {
	Destroy(ctx context.Context) error
}

// Logout the current user: the OAuth session is revoked and deleted, best effort, and the cookie
// session is destroyed either way. A failure to end the OAuth session lands on the span in the context
// as oauth.cleanup_error.
func Logout(r *Router, svc logouter, sm sessionDestroyer) {
	r.Post("/logout", func(props html.PageProps) (Node, error) {
		user := GetUserFromContext(props.Ctx)
		if user == nil {
			http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
			return nil, nil
		}

		if err := svc.Logout(props.Ctx, user.DID, GetOAuthSessionIDFromContext(props.Ctx)); err != nil && !errors.Is(err, model.ErrorOAuthSessionNotFound) {
			trace.SpanFromContext(props.Ctx).SetAttributes(attribute.String("oauth.cleanup_error", "logging out of OAuth session: "+err.Error()))
		}

		if err := sm.Destroy(props.Ctx); err != nil {
			trace.SpanFromContext(props.Ctx).RecordError(err)
			return html.ErrorPage(props), err
		}

		http.Redirect(props.W, props.R, "/", http.StatusSeeOther)
		return nil, nil
	})
}

// localPath from a redirect parameter: a cleaned path on this site, with its query, or empty. A login
// link must not send users elsewhere, so anything with a scheme or a host is dropped, and so is
// anything a browser could read as one once the path is cleaned: a backslash, which browsers treat as
// a slash, and control characters, which they strip. The path is cleaned here, as a redirect would
// clean it, so what is checked is what is redirected to.
func localPath(redirect string) string {
	if !strings.HasPrefix(redirect, "/") || strings.ContainsFunc(redirect, unsafeInPath) {
		return ""
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || strings.ContainsFunc(u.Path, unsafeInPath) {
		return ""
	}

	cleaned := path.Clean(u.Path)
	if strings.HasSuffix(u.Path, "/") && cleaned != "/" {
		cleaned += "/"
	}
	if !strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "//") {
		return ""
	}
	u.Path = cleaned
	u.RawPath = ""
	return u.String()
}

// unsafeInPath reports whether the rune is a backslash or a control character, which browsers turn
// into something else in a URL.
func unsafeInPath(r rune) bool {
	return r == '\\' || r < 0x20 || r == 0x7f
}

func withTitle(props html.PageProps, title string) html.PageProps {
	props.Title = title
	return props
}
