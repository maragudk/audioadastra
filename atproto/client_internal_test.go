package atproto

import (
	"testing"

	"maragu.dev/is"
)

func TestAuthorizationRedirectURL(t *testing.T) {
	t.Run("should add the client ID and request URI to the authorization endpoint", func(t *testing.T) {
		u, err := authorizationRedirectURL("https://auth.test/oauth/authorize", "https://app.test/oauth/client-metadata.json", "urn:ietf:params:oauth:request_uri:abc")
		is.NotError(t, err)
		is.Equal(t, "https://auth.test/oauth/authorize?client_id=https%3A%2F%2Fapp.test%2Foauth%2Fclient-metadata.json&request_uri=urn%3Aietf%3Aparams%3Aoauth%3Arequest_uri%3Aabc", u.String())
	})

	t.Run("should keep a query the endpoint already has, replacing only the client's own parameters", func(t *testing.T) {
		u, err := authorizationRedirectURL("https://auth.test/oauth/authorize?prompt=login&client_id=wrong", "client", "request")
		is.NotError(t, err)
		query := u.Query()
		is.Equal(t, "login", query.Get("prompt"))
		is.EqualSlice(t, []string{"client"}, query["client_id"])
		is.EqualSlice(t, []string{"request"}, query["request_uri"])
		is.Equal(t, "/oauth/authorize", u.Path)
	})

	t.Run("should refuse an endpoint that does not parse", func(t *testing.T) {
		_, err := authorizationRedirectURL(":nope", "client", "request")
		is.True(t, err != nil, "expected an error")
	})
}
