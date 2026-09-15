package sqlite_test

import (
	"reflect"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"maragu.dev/is"

	"app/model"
	"app/sqlite"
	"app/sqlitetest"
)

var _ oauth.ClientAuthStore = (*sqlite.Database)(nil)

func TestDatabase_SaveAuthRequestInfo(t *testing.T) {
	t.Run("should save an auth request and get it back by state", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		info := newAuthRequestData("state1")
		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), info))

		got, err := db.GetAuthRequestInfo(t.Context(), "state1")
		is.NotError(t, err)
		is.True(t, reflect.DeepEqual(info, *got), "auth request differs: %+v", *got)
	})

	t.Run("should save an auth request without an account DID", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		info := newAuthRequestData("state1")
		info.AccountDID = nil
		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), info))

		got, err := db.GetAuthRequestInfo(t.Context(), "state1")
		is.NotError(t, err)
		is.True(t, got.AccountDID == nil)
		is.True(t, reflect.DeepEqual(info, *got), "auth request differs: %+v", *got)
	})

	t.Run("should error when saving the same state twice", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), newAuthRequestData("state1")))
		err := db.SaveAuthRequestInfo(t.Context(), newAuthRequestData("state1"))
		is.True(t, err != nil, "expected an error")
	})

	t.Run("should sweep auth requests older than ten minutes but keep younger ones", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), newAuthRequestData("old")))
		is.NotError(t, db.H.Exec(t.Context(), `update oauth_auth_requests set created = strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-11 minutes') where state = 'old'`))
		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), newAuthRequestData("young")))
		is.NotError(t, db.H.Exec(t.Context(), `update oauth_auth_requests set created = strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-9 minutes') where state = 'young'`))

		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), newAuthRequestData("new")))

		_, err := db.GetAuthRequestInfo(t.Context(), "old")
		is.Error(t, model.ErrorOAuthAuthRequestNotFound, err)
		_, err = db.GetAuthRequestInfo(t.Context(), "young")
		is.NotError(t, err)
		_, err = db.GetAuthRequestInfo(t.Context(), "new")
		is.NotError(t, err)
	})
}

func TestDatabase_GetAuthRequestInfo(t *testing.T) {
	t.Run("should return not found for an unknown state", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetAuthRequestInfo(t.Context(), "nope")
		is.Error(t, model.ErrorOAuthAuthRequestNotFound, err)
	})
}

func TestDatabase_DeleteAuthRequestInfo(t *testing.T) {
	t.Run("should delete an auth request so it cannot be gotten again", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveAuthRequestInfo(t.Context(), newAuthRequestData("state1")))
		is.NotError(t, db.DeleteAuthRequestInfo(t.Context(), "state1"))

		_, err := db.GetAuthRequestInfo(t.Context(), "state1")
		is.Error(t, model.ErrorOAuthAuthRequestNotFound, err)
	})

	t.Run("should not error deleting an unknown state", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.DeleteAuthRequestInfo(t.Context(), "nope"))
	})
}

func TestDatabase_SaveSession(t *testing.T) {
	t.Run("should save a session and get it back by DID and session ID", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		sess := newClientSessionData("did:plc:alice", "sess1")
		is.NotError(t, db.SaveSession(t.Context(), sess))

		got, err := db.GetSession(t.Context(), "did:plc:alice", "sess1")
		is.NotError(t, err)
		is.True(t, reflect.DeepEqual(sess, *got), "session differs: %+v", *got)
	})

	t.Run("should upsert an existing session, replacing its tokens and nonces", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		sess := newClientSessionData("did:plc:alice", "sess1")
		is.NotError(t, db.SaveSession(t.Context(), sess))

		sess.AccessToken = "access2"
		sess.RefreshToken = "refresh2"
		sess.DPoPAuthServerNonce = "asnonce2"
		sess.DPoPHostNonce = "hostnonce2"
		is.NotError(t, db.SaveSession(t.Context(), sess))

		got, err := db.GetSession(t.Context(), "did:plc:alice", "sess1")
		is.NotError(t, err)
		is.True(t, reflect.DeepEqual(sess, *got), "session differs: %+v", *got)

		var count int
		is.NotError(t, db.H.Get(t.Context(), &count, `select count(*) from oauth_sessions`))
		is.Equal(t, 1, count)
	})

	t.Run("should sweep sessions untouched for a year but keep younger ones", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		// Inserted directly with their age, since the updated timestamp trigger would reset an update.
		insert := `
			insert into oauth_sessions (did, session_id, updated, host_url, auth_server_url, auth_server_token_endpoint, scopes, access_token, refresh_token, dpop_auth_server_nonce, dpop_host_nonce, dpop_private_key_multibase)
			values ('did:plc:alice', ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?), 'https://pds.test', 'https://auth.test', 'https://auth.test/oauth/token', 'atproto', 'access', 'refresh', '', '', 'zkey')`
		is.NotError(t, db.H.Exec(t.Context(), insert, "old", "-13 months"))
		is.NotError(t, db.H.Exec(t.Context(), insert, "young", "-11 months"))

		is.NotError(t, db.SaveSession(t.Context(), newClientSessionData("did:plc:alice", "new")))

		_, err := db.GetSession(t.Context(), "did:plc:alice", "old")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		_, err = db.GetSession(t.Context(), "did:plc:alice", "young")
		is.NotError(t, err)
		_, err = db.GetSession(t.Context(), "did:plc:alice", "new")
		is.NotError(t, err)
	})

	t.Run("should keep two sessions for the same DID apart", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveSession(t.Context(), newClientSessionData("did:plc:alice", "sess1")))
		two := newClientSessionData("did:plc:alice", "sess2")
		two.AccessToken = "access-two"
		is.NotError(t, db.SaveSession(t.Context(), two))

		got, err := db.GetSession(t.Context(), "did:plc:alice", "sess2")
		is.NotError(t, err)
		is.Equal(t, "access-two", got.AccessToken)
	})
}

func TestDatabase_GetSession(t *testing.T) {
	t.Run("should return not found for an unknown session", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.GetSession(t.Context(), "did:plc:alice", "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestDatabase_DeleteSession(t *testing.T) {
	t.Run("should delete only the given session", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.SaveSession(t.Context(), newClientSessionData("did:plc:alice", "sess1")))
		is.NotError(t, db.SaveSession(t.Context(), newClientSessionData("did:plc:alice", "sess2")))

		is.NotError(t, db.DeleteSession(t.Context(), "did:plc:alice", "sess1"))

		_, err := db.GetSession(t.Context(), "did:plc:alice", "sess1")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
		_, err = db.GetSession(t.Context(), "did:plc:alice", "sess2")
		is.NotError(t, err)
	})

	t.Run("should not error deleting an unknown session", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		is.NotError(t, db.DeleteSession(t.Context(), "did:plc:alice", "nope"))
	})
}

func newAuthRequestData(state string) oauth.AuthRequestData {
	did := syntax.DID("did:plc:alice")
	return oauth.AuthRequestData{
		State:                        state,
		AuthServerURL:                "https://auth.test",
		AccountDID:                   &did,
		Scopes:                       []string{"atproto", "blob:audio/*"},
		RequestURI:                   "urn:ietf:params:oauth:request_uri:" + state,
		AuthServerTokenEndpoint:      "https://auth.test/oauth/token",
		AuthServerRevocationEndpoint: "https://auth.test/oauth/revoke",
		PKCEVerifier:                 "verifier",
		DPoPAuthServerNonce:          "nonce",
		DPoPPrivateKeyMultibase:      "zkey",
	}
}

func newClientSessionData(did syntax.DID, sessionID string) oauth.ClientSessionData {
	return oauth.ClientSessionData{
		AccountDID:                   did,
		SessionID:                    sessionID,
		HostURL:                      "https://pds.test",
		AuthServerURL:                "https://auth.test",
		AuthServerTokenEndpoint:      "https://auth.test/oauth/token",
		AuthServerRevocationEndpoint: "https://auth.test/oauth/revoke",
		Scopes:                       []string{"atproto", "blob:audio/*"},
		AccessToken:                  "access",
		RefreshToken:                 "refresh",
		DPoPAuthServerNonce:          "asnonce",
		DPoPHostNonce:                "hostnonce",
		DPoPPrivateKeyMultibase:      "zkey",
	}
}
