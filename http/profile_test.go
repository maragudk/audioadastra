package http_test

import (
	nethttp "net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"maragu.dev/is"
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
		s.net.Directory.Insert(identity.Identity{DID: "did:plc:alice", Handle: syntax.HandleInvalid})

		_, body := s.get(t, "/profile")
		is.True(t, strings.Contains(body, `@handle.invalid`), "no handle.invalid")
	})
}
