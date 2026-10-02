package sqlite_test

import (
	"net/url"
	"reflect"
	"testing"

	"maragu.dev/is"

	"app/model"
	"app/sqlitetest"
)

// aliceDID is a well-formed example DID: 24 characters of base32 after the method.
const aliceDID = "did:plc:alicealicealicealicealic"

func TestDatabase_SaveOAuthAuthRequest(t *testing.T) {
	t.Run("should save an auth request and get it back by state", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		r := newAuthRequest("state1")
		is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), r))

		got, err := db.GetOAuthAuthRequest(t.Context(), "state1")
		is.NotError(t, err)
		is.True(t, !got.Created.T.IsZero())
		is.True(t, !got.Updated.T.IsZero())
		got.Created, got.Updated = model.Time{}, model.Time{}
		is.True(t, reflect.DeepEqual(r, got), "auth request differs: %+v", got)
	})

	t.Run("should save an auth request without an account DID", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		r := newAuthRequest("state1")
		r.AccountDID = ""
		is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), r))

		got, err := db.GetOAuthAuthRequest(t.Context(), "state1")
		is.NotError(t, err)
		is.Equal(t, model.DID(""), got.AccountDID)
	})

	t.Run("should give back the auth server URL byte for byte", func(t *testing.T) {
		for _, authServerURL := range []string{"https://host", "https://host/", "https://host:1234", "https://host/path", "http://127.0.0.1:2583"} {
			db := sqlitetest.NewDatabase(t)

			r := newAuthRequest("state1")
			r.AuthServerURL = mustParseURL(authServerURL)
			is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), r))

			got, err := db.GetOAuthAuthRequest(t.Context(), "state1")
			is.NotError(t, err)
			is.Equal(t, authServerURL, got.AuthServerURL.String())
		}
	})

	t.Run("should save an auth request without a revocation endpoint", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		r := newAuthRequest("state1")
		r.AuthServerRevocationEndpoint = nil
		is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), r))

		got, err := db.GetOAuthAuthRequest(t.Context(), "state1")
		is.NotError(t, err)
		is.Nil(t, got.AuthServerRevocationEndpoint)
	})

	t.Run("should error for a stored auth server URL that is not an absolute URL", func(t *testing.T) {
		for _, value := range []string{":not a url", "auth.test", ""} {
			db := sqlitetest.NewDatabase(t)

			is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), newAuthRequest("state1")))
			is.NotError(t, db.H.Exec(t.Context(), `update oauth_auth_requests set auth_server_url = ? where state = 'state1'`, value))

			_, err := db.GetOAuthAuthRequest(t.Context(), "state1")
			is.True(t, err != nil, "expected an error for "+value)
		}
	})

	t.Run("should error when saving the same state twice", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), newAuthRequest("state1")))
		err := db.SaveOAuthAuthRequest(t.Context(), newAuthRequest("state1"))
		is.True(t, err != nil, "expected an error")
	})
}

func TestDatabase_GetOAuthAuthRequest(t *testing.T) {
	t.Run("should return not found for an unknown state", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetOAuthAuthRequest(t.Context(), "nope")
		is.Error(t, model.ErrorOAuthAuthRequestNotFound, err)
	})
}

func TestDatabase_DeleteOAuthAuthRequest(t *testing.T) {
	t.Run("should delete an auth request so it cannot be gotten again", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveOAuthAuthRequest(t.Context(), newAuthRequest("state1")))
		is.NotError(t, db.DeleteOAuthAuthRequest(t.Context(), "state1"))

		_, err := db.GetOAuthAuthRequest(t.Context(), "state1")
		is.Error(t, model.ErrorOAuthAuthRequestNotFound, err)
	})

	t.Run("should not error deleting an unknown state", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.DeleteOAuthAuthRequest(t.Context(), "nope"))
	})
}

func TestDatabase_SaveOAuthSession(t *testing.T) {
	t.Run("should save a session without a revocation endpoint", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		sess := newSession(aliceDID, "s1")
		sess.AuthServerRevocationEndpoint = nil
		is.NotError(t, db.SaveOAuthSession(t.Context(), sess))

		got, err := db.GetOAuthSession(t.Context(), aliceDID, "s1")
		is.NotError(t, err)
		is.Nil(t, got.AuthServerRevocationEndpoint)
	})

	t.Run("should error for a stored host URL that is not an absolute URL", func(t *testing.T) {
		for _, value := range []string{":not a url", "pds.test", ""} {
			db := sqlitetest.NewDatabase(t)

			is.NotError(t, db.SaveOAuthSession(t.Context(), newSession(aliceDID, "s1")))
			is.NotError(t, db.H.Exec(t.Context(), `update oauth_sessions set host_url = ? where session_id = 's1'`, value))

			_, err := db.GetOAuthSession(t.Context(), aliceDID, "s1")
			is.True(t, err != nil, "expected an error for "+value)
		}
	})

	t.Run("should save a session and get it back by DID and session ID", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		s := newSession(aliceDID, "sess1")
		is.NotError(t, db.SaveOAuthSession(t.Context(), s))

		got, err := db.GetOAuthSession(t.Context(), aliceDID, "sess1")
		is.NotError(t, err)
		is.True(t, !got.Created.T.IsZero())
		got.Created, got.Updated = model.Time{}, model.Time{}
		is.True(t, reflect.DeepEqual(s, got), "session differs: %+v", got)
	})

	t.Run("should upsert an existing session, replacing its tokens and nonces", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		s := newSession(aliceDID, "sess1")
		is.NotError(t, db.SaveOAuthSession(t.Context(), s))

		s.AccessToken = "access2"
		s.RefreshToken = "refresh2"
		s.DPoPAuthServerNonce = "asnonce2"
		s.DPoPHostNonce = "hostnonce2"
		is.NotError(t, db.SaveOAuthSession(t.Context(), s))

		got, err := db.GetOAuthSession(t.Context(), aliceDID, "sess1")
		is.NotError(t, err)
		got.Created, got.Updated = model.Time{}, model.Time{}
		is.True(t, reflect.DeepEqual(s, got), "session differs: %+v", got)

		var count int
		is.NotError(t, db.H.Get(t.Context(), &count, `select count(*) from oauth_sessions`))
		is.Equal(t, 1, count)
	})

	t.Run("should keep two sessions for the same DID apart", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveOAuthSession(t.Context(), newSession(aliceDID, "sess1")))
		two := newSession(aliceDID, "sess2")
		two.AccessToken = "access-two"
		is.NotError(t, db.SaveOAuthSession(t.Context(), two))

		got, err := db.GetOAuthSession(t.Context(), aliceDID, "sess2")
		is.NotError(t, err)
		is.Equal(t, "access-two", got.AccessToken)
	})
}

func TestDatabase_GetOAuthSession(t *testing.T) {
	t.Run("should return not found for an unknown session", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetOAuthSession(t.Context(), aliceDID, "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestDatabase_DeleteOAuthSession(t *testing.T) {
	t.Run("should delete only the given session", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveOAuthSession(t.Context(), newSession(aliceDID, "sess1")))
		is.NotError(t, db.SaveOAuthSession(t.Context(), newSession(aliceDID, "sess2")))

		is.NotError(t, db.DeleteOAuthSession(t.Context(), aliceDID, "sess1"))

		_, err := db.GetOAuthSession(t.Context(), aliceDID, "sess1")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		_, err = db.GetOAuthSession(t.Context(), aliceDID, "sess2")
		is.NotError(t, err)
	})

	t.Run("should not error deleting an unknown session", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.DeleteOAuthSession(t.Context(), aliceDID, "nope"))
	})
}

func newAuthRequest(state model.OAuthState) model.OAuthAuthRequest {
	return model.OAuthAuthRequest{
		State:                        state,
		AccountDID:                   aliceDID,
		AuthServerURL:                mustParseURL("https://auth.test"),
		AuthServerTokenEndpoint:      mustParseURL("https://auth.test/oauth/token"),
		AuthServerRevocationEndpoint: mustParseURL("https://auth.test/oauth/revoke"),
		Scopes:                       []string{"atproto", "blob:audio/*"},
		RequestURI:                   "urn:ietf:params:oauth:request_uri:" + state.String(),
		PKCEVerifier:                 "verifier",
		DPoPAuthServerNonce:          "nonce",
		DPoPPrivateKeyMultibase:      "zkey",
	}
}

func newSession(did model.DID, sessionID model.OAuthSessionID) model.OAuthSession {
	return model.OAuthSession{
		DID:                          did,
		SessionID:                    sessionID,
		HostURL:                      mustParseURL("https://pds.test"),
		AuthServerURL:                mustParseURL("https://auth.test"),
		AuthServerTokenEndpoint:      mustParseURL("https://auth.test/oauth/token"),
		AuthServerRevocationEndpoint: mustParseURL("https://auth.test/oauth/revoke"),
		Scopes:                       []string{"atproto", "blob:audio/*"},
		AccessToken:                  "access",
		RefreshToken:                 "refresh",
		DPoPAuthServerNonce:          "asnonce",
		DPoPHostNonce:                "hostnonce",
		DPoPPrivateKeyMultibase:      "zkey",
	}
}

// mustParseURL for fixtures that are known to parse.
func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
