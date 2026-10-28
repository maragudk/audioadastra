package http

import (
	"context"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gluehttp "maragu.dev/glue/http"
	"maragu.dev/is"

	"app/model"
)

func TestLogout(t *testing.T) {
	t.Run("should keep the user logged in, and say why, when the logout gave up waiting for an upload", func(t *testing.T) {
		sm := &recordingSessions{}
		router := gluehttp.NewRouter(gluehttp.NewRouterOpts{})
		router.Use(func(next nethttp.Handler) nethttp.Handler {
			return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
				ctx := context.WithValue(r.Context(), contextUserKey, &model.User{DID: "did:plc:alicealicealicealicealic"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		})
		Logout(router, busyLogouts{}, sm)

		rec := httptest.NewRecorder()
		router.Mux.ServeHTTP(rec, httptest.NewRequest(nethttp.MethodPost, "/logout", nil))
		is.Equal(t, nethttp.StatusSeeOther, rec.Code)
		is.Equal(t, "/profile", rec.Header().Get("Location"))
		is.True(t, !sm.destroyed, "cookie session destroyed")
		is.True(t, strings.Contains(fmt.Sprint(sm.put[sessionNoticeKey]), "wait for your upload"), "no notice")
	})
}

// busyLogouts give up every logout waiting for another call as the session.
type busyLogouts struct{}

func (busyLogouts) Logout(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	return fmt.Errorf("%w: waiting for another call as the OAuth session: %w", model.ErrorOAuthSessionBusy, context.DeadlineExceeded)
}

// recordingSessions record what is done to the cookie session.
type recordingSessions struct {
	destroyed bool
	put       map[string]any
}

func (s *recordingSessions) Destroy(ctx context.Context) error {
	s.destroyed = true
	return nil
}

func (s *recordingSessions) Put(ctx context.Context, key string, val any) {
	if s.put == nil {
		s.put = map[string]any{}
	}
	s.put[key] = val
}

func TestLocalPath(t *testing.T) {
	tests := []struct {
		redirect string
		expected string
	}{
		{redirect: "/", expected: "/"},
		{redirect: "/profile", expected: "/profile"},
		{redirect: "/profile/", expected: "/profile/"},
		{redirect: "/profile?x=1", expected: "/profile?x=1"},
		{redirect: "/a/../profile", expected: "/profile"},
		{redirect: "/a/..//evil.test", expected: "/evil.test"},
		{redirect: "/foo/../\\evil.test", expected: ""},
		{redirect: "/./\\evil.test", expected: ""},
		{redirect: "/\\evil.test", expected: ""},
		{redirect: "/%5Cevil.test", expected: ""},
		{redirect: "/%5cevil.test", expected: ""},
		{redirect: "//evil.test", expected: ""},
		{redirect: "/\t/evil.test", expected: ""},
		{redirect: "/%09/evil.test", expected: ""},
		{redirect: "/\n/evil.test", expected: ""},
		{redirect: "https://evil.test", expected: ""},
		{redirect: "evil.test", expected: ""},
		{redirect: "", expected: ""},
	}

	for _, test := range tests {
		t.Run(test.redirect, func(t *testing.T) {
			is.Equal(t, test.expected, localPath(test.redirect))
		})
	}
}
