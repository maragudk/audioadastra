package sqlite

import (
	"context"
	"errors"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"maragu.dev/glue/sql"

	"app/model"
)

// authRequestRow is the oauth_auth_requests table shape. Scopes are stored as one space-separated
// string, the same form they take on the wire, and the account DID is nullable, as it is optional in
// [oauth.AuthRequestData].
type authRequestRow struct {
	State                        string
	Created                      model.Time
	AuthServerURL                string  `db:"auth_server_url"`
	AccountDID                   *string `db:"account_did"`
	Scopes                       string
	RequestURI                   string `db:"request_uri"`
	AuthServerTokenEndpoint      string `db:"auth_server_token_endpoint"`
	AuthServerRevocationEndpoint string `db:"auth_server_revocation_endpoint"`
	PKCEVerifier                 string `db:"pkce_verifier"`
	DPoPAuthServerNonce          string `db:"dpop_auth_server_nonce"`
	DPoPPrivateKeyMultibase      string `db:"dpop_private_key_multibase"`
}

// sessionRow is the oauth_sessions table shape.
type sessionRow struct {
	DID                          string
	SessionID                    string `db:"session_id"`
	Created                      model.Time
	Updated                      model.Time
	HostURL                      string `db:"host_url"`
	AuthServerURL                string `db:"auth_server_url"`
	AuthServerTokenEndpoint      string `db:"auth_server_token_endpoint"`
	AuthServerRevocationEndpoint string `db:"auth_server_revocation_endpoint"`
	Scopes                       string
	AccessToken                  string `db:"access_token"`
	RefreshToken                 string `db:"refresh_token"`
	DPoPAuthServerNonce          string `db:"dpop_auth_server_nonce"`
	DPoPHostNonce                string `db:"dpop_host_nonce"`
	DPoPPrivateKeyMultibase      string `db:"dpop_private_key_multibase"`
}

// GetAuthRequestInfo for the given state, which is [model.ErrorOAuthAuthRequestNotFound] when there is
// no pending auth request for it.
func (d *Database) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	var row authRequestRow
	if err := d.H.Get(ctx, &row, `select * from oauth_auth_requests where state = ?`, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrorOAuthAuthRequestNotFound
		}
		return nil, err
	}

	info := oauth.AuthRequestData{
		State:                        row.State,
		AuthServerURL:                row.AuthServerURL,
		Scopes:                       splitScopes(row.Scopes),
		RequestURI:                   row.RequestURI,
		AuthServerTokenEndpoint:      row.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: row.AuthServerRevocationEndpoint,
		PKCEVerifier:                 row.PKCEVerifier,
		DPoPAuthServerNonce:          row.DPoPAuthServerNonce,
		DPoPPrivateKeyMultibase:      row.DPoPPrivateKeyMultibase,
	}
	if row.AccountDID != nil {
		did := syntax.DID(*row.AccountDID)
		info.AccountDID = &did
	}
	return &info, nil
}

// SaveAuthRequestInfo for a new auth flow. This is create-only: saving the same state twice is an
// error. It also sweeps auth requests older than ten minutes, which is longer than any auth server
// keeps a pushed authorization request alive, so nothing waits on a job to garbage-collect them.
func (d *Database) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	var accountDID *string
	if info.AccountDID != nil {
		accountDID = new(info.AccountDID.String())
	}

	return d.H.InTx(ctx, func(ctx context.Context, tx *Tx) error {
		query := `delete from oauth_auth_requests where created < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-10 minutes')`
		if err := tx.Exec(ctx, query); err != nil {
			return err
		}

		query = `
			insert into oauth_auth_requests (
				state, auth_server_url, account_did, scopes, request_uri, auth_server_token_endpoint,
				auth_server_revocation_endpoint, pkce_verifier, dpop_auth_server_nonce, dpop_private_key_multibase
			) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		return tx.Exec(ctx, query,
			info.State, info.AuthServerURL, accountDID, joinScopes(info.Scopes), info.RequestURI,
			info.AuthServerTokenEndpoint, info.AuthServerRevocationEndpoint, info.PKCEVerifier,
			info.DPoPAuthServerNonce, info.DPoPPrivateKeyMultibase)
	})
}

func (d *Database) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return d.H.Exec(ctx, `delete from oauth_auth_requests where state = ?`, state)
}

// GetSession for the given DID and session ID, which is [model.ErrorOAuthSessionNotFound] when there
// is none.
func (d *Database) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	var row sessionRow
	if err := d.H.Get(ctx, &row, `select * from oauth_sessions where did = ? and session_id = ?`, did.String(), sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrorOAuthSessionNotFound
		}
		return nil, err
	}

	return &oauth.ClientSessionData{
		AccountDID:                   syntax.DID(row.DID),
		SessionID:                    row.SessionID,
		HostURL:                      row.HostURL,
		AuthServerURL:                row.AuthServerURL,
		AuthServerTokenEndpoint:      row.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: row.AuthServerRevocationEndpoint,
		Scopes:                       splitScopes(row.Scopes),
		AccessToken:                  row.AccessToken,
		RefreshToken:                 row.RefreshToken,
		DPoPAuthServerNonce:          row.DPoPAuthServerNonce,
		DPoPHostNonce:                row.DPoPHostNonce,
		DPoPPrivateKeyMultibase:      row.DPoPPrivateKeyMultibase,
	}, nil
}

// SaveSession as an upsert: a session with the same DID and session ID has every field replaced.
//
// It also sweeps sessions untouched for a year. A session is written whenever it is used, so one that
// old has no user coming back for it, and its tokens would otherwise stay on disk forever.
func (d *Database) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	return d.H.InTx(ctx, func(ctx context.Context, tx *Tx) error {
		query := `delete from oauth_sessions where updated < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 year')`
		if err := tx.Exec(ctx, query); err != nil {
			return err
		}

		return saveSession(ctx, tx, sess)
	})
}

func saveSession(ctx context.Context, tx *Tx, sess oauth.ClientSessionData) error {
	query := `
		insert into oauth_sessions (
			did, session_id, host_url, auth_server_url, auth_server_token_endpoint,
			auth_server_revocation_endpoint, scopes, access_token, refresh_token,
			dpop_auth_server_nonce, dpop_host_nonce, dpop_private_key_multibase
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict (did, session_id) do update set
			host_url = excluded.host_url,
			auth_server_url = excluded.auth_server_url,
			auth_server_token_endpoint = excluded.auth_server_token_endpoint,
			auth_server_revocation_endpoint = excluded.auth_server_revocation_endpoint,
			scopes = excluded.scopes,
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			dpop_auth_server_nonce = excluded.dpop_auth_server_nonce,
			dpop_host_nonce = excluded.dpop_host_nonce,
			dpop_private_key_multibase = excluded.dpop_private_key_multibase`
	return tx.Exec(ctx, query,
		sess.AccountDID.String(), sess.SessionID, sess.HostURL, sess.AuthServerURL, sess.AuthServerTokenEndpoint,
		sess.AuthServerRevocationEndpoint, joinScopes(sess.Scopes), sess.AccessToken, sess.RefreshToken,
		sess.DPoPAuthServerNonce, sess.DPoPHostNonce, sess.DPoPPrivateKeyMultibase)
}

func (d *Database) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return d.H.Exec(ctx, `delete from oauth_sessions where did = ? and session_id = ?`, did.String(), sessionID)
}

func joinScopes(scopes []string) string {
	return strings.Join(scopes, " ")
}

func splitScopes(scopes string) []string {
	return strings.Fields(scopes)
}
