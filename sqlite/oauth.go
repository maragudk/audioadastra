package sqlite

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
func (d *Database) GetOAuthAuthRequest(ctx context.Context, state model.OAuthState) (model.OAuthAuthRequest, error) {
	var row authRequestRow
	if err := d.H.Get(ctx, &row, `select * from oauth_auth_requests where state = ?`, state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.OAuthAuthRequest{}, model.ErrorOAuthAuthRequestNotFound
		}
		return model.OAuthAuthRequest{}, err
	}

	var urls urlParser
	r := model.OAuthAuthRequest{
		State:                        model.OAuthState(row.State),
		Created:                      row.Created,
		Updated:                      row.Updated,
		AuthServerURL:                urls.parse("auth_server_url", row.AuthServerURL),
		AuthServerTokenEndpoint:      urls.parse("auth_server_token_endpoint", row.AuthServerTokenEndpoint),
		AuthServerRevocationEndpoint: urls.parseOptional("auth_server_revocation_endpoint", row.AuthServerRevocationEndpoint),
		Scopes:                       splitScopes(row.Scopes),
		RequestURI:                   row.RequestURI,
		PKCEVerifier:                 row.PKCEVerifier,
		DPoPAuthServerNonce:          row.DPoPAuthServerNonce,
		DPoPPrivateKeyMultibase:      row.DPoPPrivateKeyMultibase,
	}
	if urls.err != nil {
		return model.OAuthAuthRequest{}, urls.err
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
		r.State, urlString(r.AuthServerURL), accountDID, joinScopes(r.Scopes), r.RequestURI,
		urlString(r.AuthServerTokenEndpoint), urlString(r.AuthServerRevocationEndpoint), r.PKCEVerifier,
		r.DPoPAuthServerNonce, r.DPoPPrivateKeyMultibase)
}

func (d *Database) DeleteOAuthAuthRequest(ctx context.Context, state model.OAuthState) error {
	return d.H.Exec(ctx, `delete from oauth_auth_requests where state = ?`, state)
}

// GetOAuthSession for the given DID and session ID, which is [model.ErrorOAuthSessionNotFound] when
// there is none.
func (d *Database) GetOAuthSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (model.OAuthSession, error) {
	var row sessionRow
	if err := d.H.Get(ctx, &row, `select * from oauth_sessions where did = ? and session_id = ?`, did, sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.OAuthSession{}, model.ErrorOAuthSessionNotFound
		}
		return model.OAuthSession{}, err
	}

	var urls urlParser
	s := model.OAuthSession{
		DID:                          model.DID(row.DID),
		SessionID:                    model.OAuthSessionID(row.SessionID),
		Created:                      row.Created,
		Updated:                      row.Updated,
		HostURL:                      urls.parse("host_url", row.HostURL),
		AuthServerURL:                urls.parse("auth_server_url", row.AuthServerURL),
		AuthServerTokenEndpoint:      urls.parse("auth_server_token_endpoint", row.AuthServerTokenEndpoint),
		AuthServerRevocationEndpoint: urls.parseOptional("auth_server_revocation_endpoint", row.AuthServerRevocationEndpoint),
		Scopes:                       splitScopes(row.Scopes),
		AccessToken:                  row.AccessToken,
		RefreshToken:                 row.RefreshToken,
		DPoPAuthServerNonce:          row.DPoPAuthServerNonce,
		DPoPHostNonce:                row.DPoPHostNonce,
		DPoPPrivateKeyMultibase:      row.DPoPPrivateKeyMultibase,
	}
	if urls.err != nil {
		return model.OAuthSession{}, urls.err
	}
	return s, nil
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
		s.DID, s.SessionID, urlString(s.HostURL), urlString(s.AuthServerURL), urlString(s.AuthServerTokenEndpoint),
		urlString(s.AuthServerRevocationEndpoint), joinScopes(s.Scopes), s.AccessToken, s.RefreshToken,
		s.DPoPAuthServerNonce, s.DPoPHostNonce, s.DPoPPrivateKeyMultibase)
}

func (d *Database) DeleteOAuthSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	return d.H.Exec(ctx, `delete from oauth_sessions where did = ? and session_id = ?`, did, sessionID)
}

func joinScopes(scopes []string) string {
	return strings.Join(scopes, " ")
}

func splitScopes(scopes string) []string {
	return strings.Fields(scopes)
}

// urlParser parses URLs from text columns, keeping the first error so a value can be converted in one expression and
// checked once.
type urlParser struct {
	err error
}

// parse the named URL, which must be absolute, with a scheme and a host.
func (p *urlParser) parse(column, value string) *url.URL {
	u, err := url.Parse(value)
	if err == nil && (u.Scheme == "" || u.Host == "") {
		err = fmt.Errorf("%q is not an absolute URL", value)
	}
	if err != nil {
		if p.err == nil {
			p.err = fmt.Errorf("%v: %w", column, err)
		}
		return nil
	}
	return u
}

// parseOptional URL, which is nil when empty.
func (p *urlParser) parseOptional(column, value string) *url.URL {
	if value == "" {
		return nil
	}
	return p.parse(column, value)
}

// urlString for a text column, which is empty for a nil URL.
func urlString(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}
