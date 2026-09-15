package atproto_test

import (
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"maragu.dev/is"

	"app/atproto"
)

func TestNew(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	is.NotError(t, err)

	t.Run("should build a localhost client for the real network without a key", func(t *testing.T) {
		c, err := atproto.New(atproto.NewOptions{BaseURL: "http://localhost:8080", Store: oauth.NewMemStore()})
		is.NotError(t, err)
		is.True(t, !c.Local)
		is.True(t, !c.OAuth.Config.IsConfidential())
		is.True(t, c.OAuth.Dir == c.Directory, "OAuth client app and directory differ")
	})

	t.Run("should refuse a public base URL without a key", func(t *testing.T) {
		_, err := atproto.New(atproto.NewOptions{BaseURL: "https://app.example.com", Store: oauth.NewMemStore()})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a missing store", func(t *testing.T) {
		_, err := atproto.New(atproto.NewOptions{BaseURL: "http://localhost:8080"})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should build a confidential client for a local network when a PLC URL is given", func(t *testing.T) {
		c, err := atproto.New(atproto.NewOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: key.Multibase(), KeyID: "k1", Store: oauth.NewMemStore(), PLCURL: "http://localhost:2582", LocalHandleSuffix: ".test"})
		is.NotError(t, err)
		is.True(t, c.Local)
		is.True(t, c.OAuth.Config.IsConfidential())
		is.True(t, c.OAuth.Client == c.OAuth.Resolver.Client, "OAuth and resolver clients differ")
		is.True(t, c.OAuth.Dir == c.Directory, "OAuth client app and directory differ")
	})

	t.Run("should refuse a CA file that does not exist", func(t *testing.T) {
		_, err := atproto.New(atproto.NewOptions{BaseURL: "http://localhost:8080", Store: oauth.NewMemStore(), PLCURL: "http://localhost:2582", CAFile: "nope.crt"})
		is.True(t, err != nil, "expected an error")
	})
}

func TestNewOAuthClientConfig(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	is.NotError(t, err)

	t.Run("should give a localhost client with a 127.0.0.1 callback for a localhost base URL", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "http://localhost:8080"})
		is.NotError(t, err)
		is.True(t, strings.HasPrefix(config.ClientID, "http://localhost?"), config.ClientID)
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", config.CallbackURL)
		is.True(t, !config.IsConfidential())
	})

	t.Run("should give a localhost client for a 127.0.0.1 base URL, ignoring any key", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "http://127.0.0.1:8080/", PrivateKeyMultibase: key.Multibase(), KeyID: "k1"})
		is.NotError(t, err)
		is.Equal(t, "http://127.0.0.1:8080/oauth/callback", config.CallbackURL)
		is.True(t, !config.IsConfidential())
	})

	t.Run("should give a confidential client for a public base URL with a key", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: key.Multibase(), KeyID: "k1"})
		is.NotError(t, err)
		is.Equal(t, "https://app.example.com/oauth/client-metadata.json", config.ClientID)
		is.Equal(t, "https://app.example.com/oauth/callback", config.CallbackURL)
		is.True(t, config.IsConfidential())
		is.Equal(t, "k1", *config.KeyID)
	})

	t.Run("should request the atproto scope, the profile collection and both blob types, all parseable", func(t *testing.T) {
		config, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "http://localhost:8080"})
		is.NotError(t, err)
		is.EqualSlice(t, []string{"atproto", "repo:com.audioadastra.actor.profile", "blob:audio/*", "blob:image/*"}, config.Scopes)

		// Every scope must parse as a permission, or a granted-versus-requested check built on parsed
		// permissions could pass vacuously.
		for _, scope := range config.Scopes[1:] {
			_, err := auth.ParsePermissionString(scope)
			is.NotError(t, err, scope)
		}
	})

	t.Run("should refuse a public base URL without a key", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com"})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a public base URL with a key but no key ID", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: key.Multibase()})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a key that is not a P-256 private key", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "https://app.example.com", PrivateKeyMultibase: "znope", KeyID: "k1"})
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should refuse a base URL without a host", func(t *testing.T) {
		_, err := atproto.NewOAuthClientConfig(atproto.NewOAuthClientConfigOptions{BaseURL: "nope"})
		is.True(t, err != nil, "expected an error")
	})
}
