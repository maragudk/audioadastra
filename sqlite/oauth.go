package sqlite

import (
	"context"
	"errors"
	"strings"

	"maragu.dev/glue/sql"

	"app/model"
)

// authRequestRow is the oauth_auth_requests table shape. Scopes are stored as one space-separated
// string, the same form they take on the wire, and the account DID is nullable.
type authRequestRow struct {
	State                        string
	Created                      model.Time
	Updated                      model.Time
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

// GetOAuthAuthRequest for the given state, which is [model.ErrorOAuthAuthRequestNotFound] when there
// is no pending auth request for it.
func (d *Database) GetOAuthAuthRequest(ctx context.Context, state string) (model.OAuthAuthRequest, error) {
	var row authRequestRow
	if err := d.H.Get(ctx, &row, `select * from oauth_auth_requests where state = ?`, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.OAuthAuthRequest{}, model.ErrorOAuthAuthRequestNotFound
		}
		return model.OAuthAuthRequest{}, err
	}

	r := model.OAuthAuthRequest{
		State:                        row.State,
		Created:                      row.Created,
		Updated:                      row.Updated,
		AuthServerURL:                row.AuthServerURL,
		AuthServerTokenEndpoint:      row.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: row.AuthServerRevocationEndpoint,
		Scopes:                       splitScopes(row.Scopes),
		RequestURI:                   row.RequestURI,
		PKCEVerifier:                 row.PKCEVerifier,
		DPoPAuthServerNonce:          row.DPoPAuthServerNonce,
		DPoPPrivateKeyMultibase:      row.DPoPPrivateKeyMultibase,
	}
	if row.AccountDID != nil {
		r.AccountDID = model.DID(*row.AccountDID)
	}
	return r, nil
}

// SaveOAuthAuthRequest for a new auth flow. This is create-only: saving the same state twice is an
// error.
func (d *Database) SaveOAuthAuthRequest(ctx context.Context, r model.OAuthAuthRequest) error {
	var accountDID *string
	if r.AccountDID != "" {
		accountDID = new(r.AccountDID.String())
	}

	query := `
		insert into oauth_auth_requests (
			state, auth_server_url, account_did, scopes, request_uri, auth_server_token_endpoint,
			auth_server_revocation_endpoint, pkce_verifier, dpop_auth_server_nonce, dpop_private_key_multibase
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	return d.H.Exec(ctx, query,
		r.State, r.AuthServerURL, accountDID, joinScopes(r.Scopes), r.RequestURI,
		r.AuthServerTokenEndpoint, r.AuthServerRevocationEndpoint, r.PKCEVerifier,
		r.DPoPAuthServerNonce, r.DPoPPrivateKeyMultibase)
}

func (d *Database) DeleteOAuthAuthRequest(ctx context.Context, state string) error {
	return d.H.Exec(ctx, `delete from oauth_auth_requests where state = ?`, state)
}

// GetOAuthSession for the given DID and session ID, which is [model.ErrorOAuthSessionNotFound] when
// there is none.
func (d *Database) GetOAuthSession(ctx context.Context, did model.DID, sessionID string) (model.OAuthSession, error) {
	var row sessionRow
	if err := d.H.Get(ctx, &row, `select * from oauth_sessions where did = ? and session_id = ?`, did, sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.OAuthSession{}, model.ErrorOAuthSessionNotFound
		}
		return model.OAuthSession{}, err
	}

	return model.OAuthSession{
		DID:                          model.DID(row.DID),
		SessionID:                    row.SessionID,
		Created:                      row.Created,
		Updated:                      row.Updated,
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

// SaveOAuthSession as an upsert: a session with the same DID and session ID has every field replaced.
func (d *Database) SaveOAuthSession(ctx context.Context, s model.OAuthSession) error {
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
	return d.H.Exec(ctx, query,
		s.DID, s.SessionID, s.HostURL, s.AuthServerURL, s.AuthServerTokenEndpoint,
		s.AuthServerRevocationEndpoint, joinScopes(s.Scopes), s.AccessToken, s.RefreshToken,
		s.DPoPAuthServerNonce, s.DPoPHostNonce, s.DPoPPrivateKeyMultibase)
}

func (d *Database) DeleteOAuthSession(ctx context.Context, did model.DID, sessionID string) error {
	return d.H.Exec(ctx, `delete from oauth_sessions where did = ? and session_id = ?`, did, sessionID)
}

func joinScopes(scopes []string) string {
	return strings.Join(scopes, " ")
}

func splitScopes(scopes string) []string {
	return strings.Fields(scopes)
}
