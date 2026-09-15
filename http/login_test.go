package http_test

import (
	"encoding/json"
	"io"
	"log/slog"
	nethttp "net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	gluehttp "maragu.dev/glue/http"
	"maragu.dev/is"

	"app/atprototest"
	"app/http"
	"app/lexicons"
	"app/service"
	"app/servicetest"
	"app/sqlite"
	"app/sqlitetest"
)

func TestLogin(t *testing.T) {
	t.Run("should render the login form", func(t *testing.T) {
		s := newServer(t)

		res, body := s.get(t, "/login")
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.True(t, strings.Contains(body, `name="handle"`), "no handle field")
		is.True(t, strings.Contains(body, `href="/login"`), "no login link in the nav")
	})

	t.Run("should keep a local redirect in the form and drop an external one", func(t *testing.T) {
		s := newServer(t)

		_, body := s.get(t, "/login?redirect=/somewhere")
		is.True(t, strings.Contains(body, `name="redirect" value="/somewhere"`), "no redirect field")

		_, body = s.get(t, "/login?redirect=https://evil.test/")
		is.True(t, !strings.Contains(body, `name="redirect"`), "external redirect kept")

		_, body = s.get(t, "/login?redirect=//evil.test/")
		is.True(t, !strings.Contains(body, `name="redirect"`), "protocol-relative redirect kept")
	})

	t.Run("should re-render the form with an error for a bad identifier", func(t *testing.T) {
		s := newServer(t)

		res, body := s.postForm(t, "/login", url.Values{"handle": {"nobody.test"}})
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, `role="alert"`), "no error")
		is.True(t, strings.Contains(body, `value="nobody.test"`), "handle not kept")
		is.Equal(t, 0, s.count(t, "oauth_auth_requests"))
	})

	t.Run("should redirect a valid identifier to the auth server", func(t *testing.T) {
		s := newServer(t)
		s.client.CheckRedirect = func(req *nethttp.Request, via []*nethttp.Request) error {
			return nethttp.ErrUseLastResponse
		}

		res, _ := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.Equal(t, nethttp.StatusSeeOther, res.StatusCode)
		is.True(t, strings.HasPrefix(res.Header.Get("Location"), s.net.AuthServerURL+"/oauth/authorize?"), res.Header.Get("Location"))
		is.Equal(t, 1, s.count(t, "oauth_auth_requests"))
	})

	t.Run("should log in on first login, creating the user and profile, and redirect a logged-in user away from the login page", func(t *testing.T) {
		s := newServer(t)

		res, body := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.Equal(t, "/", res.Request.URL.Path)
		is.True(t, strings.Contains(body, `@alice.test`), "no handle in the nav")
		is.True(t, strings.Contains(body, `action="/logout"`), "no logout form")

		is.Equal(t, 1, s.count(t, "users"))
		is.Equal(t, 1, s.count(t, "oauth_sessions"))
		is.Equal(t, 0, s.count(t, "oauth_auth_requests"))
		_, ok := s.net.GetRecord("did:plc:alice", lexicons.ActorProfile, "self")
		is.True(t, ok, "no profile record")

		res, _ = s.get(t, "/login")
		is.Equal(t, "/", res.Request.URL.Path)
	})

	t.Run("should send the user to the local redirect after login", func(t *testing.T) {
		s := newServer(t)

		res, _ := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}, "redirect": {"/somewhere"}})
		is.Equal(t, "/somewhere", res.Request.URL.Path)
	})

	t.Run("should keep two logins apart and log out only one", func(t *testing.T) {
		s := newServer(t)
		one, two := s.client, s.newClient(t)

		s.client = one
		_, body := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.True(t, strings.Contains(body, `@alice.test`))
		s.client = two
		_, body = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.True(t, strings.Contains(body, `@alice.test`))

		is.Equal(t, 1, s.count(t, "users"))
		is.Equal(t, 2, s.count(t, "oauth_sessions"))
		is.Equal(t, 1, s.net.PutRecordCalls())

		res, body := s.postForm(t, "/logout", nil)
		is.Equal(t, "/", res.Request.URL.Path)
		is.True(t, strings.Contains(body, `href="/login"`), "still logged in after logout")
		is.Equal(t, 1, s.count(t, "oauth_sessions"))
		is.Equal(t, 2, len(s.net.Revoked()))

		s.client = one
		_, body = s.get(t, "/")
		is.True(t, strings.Contains(body, `@alice.test`), "other session logged out too")
	})

	t.Run("should refuse and leave nothing behind when a scope is denied", func(t *testing.T) {
		s := newServer(t)
		s.net.GrantScopes = "atproto"

		res, body := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, `role="alert"`), "no error")
		s.assertLoggedOut(t)
	})

	t.Run("should refuse an inactive user", func(t *testing.T) {
		s := newServer(t)
		user, _, err := s.db.GetOrCreateUser(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.NotError(t, s.db.H.Exec(t.Context(), `update users set active = 0 where id = ?`, user.ID))

		res, body := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.Equal(t, nethttp.StatusForbidden, res.StatusCode)
		is.True(t, strings.Contains(body, `role="alert"`), "no error")
		s.assertLoggedOut(t)
	})

	t.Run("should refuse when the profile cannot be written", func(t *testing.T) {
		s := newServer(t)
		s.net.PutRecordFails = true

		res, body := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.Equal(t, nethttp.StatusBadGateway, res.StatusCode)
		is.True(t, strings.Contains(body, `role="alert"`), "no error")
		s.assertLoggedOut(t)
	})

	t.Run("should show a cancelled message for a callback with a bad state", func(t *testing.T) {
		s := newServer(t)

		res, body := s.get(t, "/oauth/callback?state=nope&code=c&iss="+url.QueryEscape(s.net.AuthServerURL))
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, "cancelled or failed"), "no cancelled message")
		s.assertLoggedOut(t)
	})

	t.Run("should refuse a callback in a browser that did not start the flow", func(t *testing.T) {
		s := newServer(t)
		s.client.CheckRedirect = func(req *nethttp.Request, via []*nethttp.Request) error {
			return nethttp.ErrUseLastResponse
		}
		res, _ := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		callback := s.net.Authorize(t, res.Header.Get("Location"))

		// Another browser, handed the callback URL by whoever started the flow.
		s.client = s.newClient(t)
		res, body := s.get(t, "/oauth/callback?"+callback.Encode())
		is.Equal(t, nethttp.StatusBadRequest, res.StatusCode)
		is.True(t, strings.Contains(body, "cancelled or failed"), "no cancelled message")
		s.assertLoggedOut(t)
	})

	t.Run("should change the session token on login, so a session fixated before login is not the logged-in one", func(t *testing.T) {
		s := newServer(t)
		s.client.CheckRedirect = func(req *nethttp.Request, via []*nethttp.Request) error {
			return nethttp.ErrUseLastResponse
		}
		// The session cookie is first set when the flow starts, since that is the first write to it.
		res, _ := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		before := s.sessionCookie(t)
		is.True(t, before != "", "no session cookie after starting the login")
		callback := s.net.Authorize(t, res.Header.Get("Location"))

		s.client.CheckRedirect = nil
		_, body := s.get(t, "/oauth/callback?"+callback.Encode())
		is.True(t, strings.Contains(body, `@alice.test`), "not logged in")
		is.True(t, s.sessionCookie(t) != before, "session token unchanged across login")
	})

	t.Run("should send a logged-in user away from the callback without touching their session", func(t *testing.T) {
		s := newServer(t)
		_, _ = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})

		res, body := s.get(t, "/oauth/callback?state=nope&code=c")
		is.Equal(t, "/", res.Request.URL.Path)
		is.True(t, strings.Contains(body, `@alice.test`), "logged out by the callback")
		is.Equal(t, 1, s.count(t, "oauth_sessions"))
	})

	t.Run("should forget the redirect of a failed login", func(t *testing.T) {
		s := newServer(t)
		_, _ = s.postForm(t, "/login", url.Values{"handle": {"nobody.test"}, "redirect": {"/somewhere"}})

		res, _ := s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.Equal(t, "/", res.Request.URL.Path)
	})

	t.Run("should log the user out here when the OAuth session is gone", func(t *testing.T) {
		s := newServer(t)
		_, _ = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		is.NotError(t, s.db.H.Exec(t.Context(), `delete from oauth_sessions`))

		res, _ := s.get(t, "/")
		is.Equal(t, "/login", res.Request.URL.Path)
		s.assertLoggedOut(t)
	})
}

func TestLogout(t *testing.T) {
	t.Run("should redirect to the front page when not logged in", func(t *testing.T) {
		s := newServer(t)

		res, _ := s.postForm(t, "/logout", nil)
		is.Equal(t, "/", res.Request.URL.Path)
	})

	t.Run("should end the OAuth session and the cookie session", func(t *testing.T) {
		s := newServer(t)
		_, _ = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})

		res, _ := s.postForm(t, "/logout", nil)
		is.Equal(t, "/", res.Request.URL.Path)
		is.Equal(t, 2, len(s.net.Revoked()))
		s.assertLoggedOut(t)
	})
}

func TestOAuthMetadata(t *testing.T) {
	t.Run("should serve client metadata for the confidential client", func(t *testing.T) {
		s := newServer(t)

		res, body := s.get(t, "/oauth/client-metadata.json")
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.Equal(t, "application/json", res.Header.Get("Content-Type"))

		var meta oauth.ClientMetadata
		is.NotError(t, json.Unmarshal([]byte(body), &meta))
		is.Equal(t, "https://app.test/oauth/client-metadata.json", meta.ClientID)
		is.Equal(t, "Audio Ad Astra", *meta.ClientName)
		is.Equal(t, "https://app.test", *meta.ClientURI)
		is.Equal(t, "https://app.test/oauth/jwks.json", *meta.JWKSURI)
		is.Equal(t, "private_key_jwt", meta.TokenEndpointAuthMethod)
		is.EqualSlice(t, []string{"https://app.test/oauth/callback"}, meta.RedirectURIs)
		is.Equal(t, strings.Join(service.OAuthScopes, " "), meta.Scope)
		is.NotError(t, meta.Validate(meta.ClientID))
	})

	t.Run("should serve the JWKS with the public key", func(t *testing.T) {
		s := newServer(t)

		res, body := s.get(t, "/oauth/jwks.json")
		is.Equal(t, nethttp.StatusOK, res.StatusCode)

		var jwks oauth.JWKS
		is.NotError(t, json.Unmarshal([]byte(body), &jwks))
		is.Equal(t, 1, len(jwks.Keys))
		is.Equal(t, "test", *jwks.Keys[0].KeyID)
	})
}

// server under test: the real router with the session middleware, wired to a database and the fake
// network, served over TLS at https://app.test so the OAuth callback comes back to it.
type server struct {
	net    *atprototest.Network
	db     *sqlite.Database
	client *nethttp.Client
}

func newServer(t *testing.T) *server {
	t.Helper()

	s := &server{
		net: atprototest.NewNetwork(t),
		db:  sqlitetest.NewDatabase(t),
	}
	s.net.AddAccount("did:plc:alice", "alice.test")
	app := s.net.NewClientApp(t, s.db, service.OAuthScopes)

	catalog, err := lexicons.NewCatalog()
	is.NotError(t, err)

	fat := servicetest.NewFat(t)
	service.Setup(fat, s.db, nil, app, s.net.Directory, catalog)

	log := slog.New(slog.DiscardHandler)
	sm := scs.New()
	router := gluehttp.NewRouter(gluehttp.NewRouterOpts{SM: sm})
	router.Use(sm.LoadAndSave, gluehttp.Authenticate(log, sm, s.db))
	http.InjectHTTPRouter(log, fat, app.Config, "https://app.test")(router)

	ts := httptest.NewUnstartedServer(router.Mux)
	ts.StartTLS()
	t.Cleanup(ts.Close)
	s.net.Route("app.test", ts)

	s.client = s.newClient(t)
	return s
}

// newClient with a cookie jar of its own, which is a browser of its own.
func (s *server) newClient(t *testing.T) *nethttp.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	is.NotError(t, err)
	return &nethttp.Client{Transport: s.net.Transport, Jar: jar}
}

func (s *server) get(t *testing.T, path string) (*nethttp.Response, string) {
	t.Helper()

	res, err := s.client.Get("https://app.test" + path)
	is.NotError(t, err)
	return res, readBody(t, res)
}

func (s *server) postForm(t *testing.T, path string, form url.Values) (*nethttp.Response, string) {
	t.Helper()

	res, err := s.client.PostForm("https://app.test"+path, form)
	is.NotError(t, err)
	return res, readBody(t, res)
}

func (s *server) count(t *testing.T, table string) int {
	t.Helper()

	var count int
	is.NotError(t, s.db.H.Get(t.Context(), &count, `select count(*) from `+table))
	return count
}

// sessionCookie value the browser holds for the app, or empty.
func (s *server) sessionCookie(t *testing.T) string {
	t.Helper()

	u, err := url.Parse("https://app.test/")
	is.NotError(t, err)
	for _, c := range s.client.Jar.Cookies(u) {
		if strings.HasPrefix(c.Name, "session") {
			return c.Value
		}
	}
	return ""
}

// assertLoggedOut: no cookie session in effect, and no OAuth session left behind.
func (s *server) assertLoggedOut(t *testing.T) {
	t.Helper()

	_, body := s.get(t, "/")
	is.True(t, strings.Contains(body, `href="/login"`), "logged in")
	is.Equal(t, 0, s.count(t, "oauth_sessions"))
}

func readBody(t *testing.T, res *nethttp.Response) string {
	t.Helper()

	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	is.NotError(t, err)
	return string(body)
}
