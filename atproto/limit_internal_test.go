package atproto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"maragu.dev/is"

	"app/sqlitetest"
)

func TestLimitBodies(t *testing.T) {
	serve := func(t *testing.T, body string) *httptest.Server {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(server.Close)
		return server
	}

	t.Run("should read a body up to the limit", func(t *testing.T) {
		server := serve(t, "0123456789")
		client := limitBodies(server.Client(), 10)

		res, err := client.Get(server.URL)
		is.NotError(t, err)
		body, err := io.ReadAll(res.Body)
		is.NotError(t, err)
		is.Equal(t, "0123456789", string(body))
		is.NotError(t, res.Body.Close())
	})

	t.Run("should fail reading a body over the limit, instead of ending it early", func(t *testing.T) {
		server := serve(t, "0123456789x")
		client := limitBodies(server.Client(), 10)

		res, err := client.Get(server.URL)
		is.NotError(t, err)
		body, err := io.ReadAll(res.Body)
		is.True(t, err != nil, "expected an error")
		is.Equal(t, "response body is over 10 bytes", err.Error())
		is.Equal(t, "0123456789", string(body))
		_ = res.Body.Close()
	})

	t.Run("should keep the real network's refusal to dial private addresses", func(t *testing.T) {
		server := serve(t, "private")
		c, err := NewClient(NewClientOptions{BaseURL: &url.URL{Scheme: "http", Host: "localhost:8080"}, Store: sqlitetest.NewDatabase(t)})
		is.NotError(t, err)

		for _, client := range []*http.Client{c.app.Client, c.app.Resolver.Client} {
			_, ok := client.Transport.(*bodyLimitTransport)
			is.True(t, ok, "bodies not limited")
			_, err := client.Get(server.URL)
			is.True(t, err != nil && strings.Contains(err.Error(), "not a public IP address"), "dialed a private address")
		}
	})
}
