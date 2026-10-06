package http_test

import (
	nethttp "net/http"
	"net/url"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/atprototest"
	"app/model"
)

func TestProfile(t *testing.T) {
	t.Run("should send a logged-out user to the login page and back after login", func(t *testing.T) {
		s := newServer(t)

		res, body := s.get(t, "/profile")
		is.Equal(t, "/login", res.Request.URL.Path)
		is.Equal(t, "/profile", res.Request.URL.Query().Get("redirect"))
		is.True(t, strings.Contains(body, `name="redirect" value="/profile"`), "no redirect field")

		res, body = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}, "redirect": {"/profile"}})
		is.Equal(t, "/profile", res.Request.URL.Path)
		is.True(t, strings.Contains(body, `@alice.test`), "no handle")
	})

	t.Run("should redirect a logged-out user to the login page with the path to come back to", func(t *testing.T) {
		s := newServer(t)
		s.http.CheckRedirect = func(req *nethttp.Request, via []*nethttp.Request) error {
			return nethttp.ErrUseLastResponse
		}

		res, _ := s.get(t, "/profile")
		is.Equal(t, nethttp.StatusTemporaryRedirect, res.StatusCode)
		is.Equal(t, "/login?redirect=%2Fprofile", res.Header.Get("Location"))
	})

	t.Run("should show the handle, and log out from there", func(t *testing.T) {
		s := newServer(t)
		_, _ = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})

		res, body := s.get(t, "/profile")
		is.Equal(t, nethttp.StatusOK, res.StatusCode)
		is.True(t, strings.Contains(body, `@alice.test`), "no handle")
		is.True(t, strings.Contains(body, `action="/logout"`), "no logout form")

		res, _ = s.postForm(t, "/logout", nil)
		is.Equal(t, "/", res.Request.URL.Path)
		s.assertLoggedOut(t)
	})

	t.Run("should show handle.invalid when the handle does not verify", func(t *testing.T) {
		s := newServer(t)
		_, _ = s.postForm(t, "/login", url.Values{"handle": {"alice.test"}})
		s.net.AddAccount(atprototest.AliceDID, model.HandleInvalid)

		_, body := s.get(t, "/profile")
		is.True(t, strings.Contains(body, `@handle.invalid`), "no handle.invalid")
	})
}
