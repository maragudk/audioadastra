package atproto

import (
	"context"
	"fmt"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"app/model"
)

// Store persists auth requests and sessions, in the app's own types.
type Store interface {
	GetOAuthAuthRequest(ctx context.Context, state model.OAuthState) (model.OAuthAuthRequest, error)
	SaveOAuthAuthRequest(ctx context.Context, r model.OAuthAuthRequest) error
	DeleteOAuthAuthRequest(ctx context.Context, state model.OAuthState) error
	GetOAuthSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (model.OAuthSession, error)
	SaveOAuthSession(ctx context.Context, s model.OAuthSession) error
	// DeleteOAuthSession, which is not an error when there is no such session.
	DeleteOAuthSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error
}

// clientAuthStore adapts a [Store] to the OAuth client's own store interface, converting between the
// app's types and the client's.
type clientAuthStore struct {
	store Store
}

var _ oauth.ClientAuthStore = (*clientAuthStore)(nil)

func (s *clientAuthStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	r, err := s.store.GetOAuthAuthRequest(ctx, model.OAuthState(state))
	if err != nil {
		return nil, err
	}

	info := oauth.AuthRequestData{
		State:                        r.State.String(),
		AuthServerURL:                urlString(r.AuthServerURL),
		Scopes:                       r.Scopes,
		RequestURI:                   r.RequestURI,
		AuthServerTokenEndpoint:      urlString(r.AuthServerTokenEndpoint),
		AuthServerRevocationEndpoint: urlString(r.AuthServerRevocationEndpoint),
		PKCEVerifier:                 r.PKCEVerifier,
		DPoPAuthServerNonce:          r.DPoPAuthServerNonce,
		DPoPPrivateKeyMultibase:      r.DPoPPrivateKeyMultibase,
	}
	if r.AccountDID != "" {
		info.AccountDID = new(syntax.DID(r.AccountDID))
	}
	return &info, nil
}

func (s *clientAuthStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	var urls urlParser
	r := model.OAuthAuthRequest{
		State:                        model.OAuthState(info.State),
		AuthServerURL:                urls.parse("auth server URL", info.AuthServerURL),
		AuthServerTokenEndpoint:      urls.parse("token endpoint", info.AuthServerTokenEndpoint),
		AuthServerRevocationEndpoint: urls.parseOptional("revocation endpoint", info.AuthServerRevocationEndpoint),
		Scopes:                       info.Scopes,
		RequestURI:                   info.RequestURI,
		PKCEVerifier:                 info.PKCEVerifier,
		DPoPAuthServerNonce:          info.DPoPAuthServerNonce,
		DPoPPrivateKeyMultibase:      info.DPoPPrivateKeyMultibase,
	}
	if urls.err != nil {
		return urls.err
	}
	if info.AccountDID != nil {
		r.AccountDID = model.DID(*info.AccountDID)
	}
	return s.store.SaveOAuthAuthRequest(ctx, r)
}

func (s *clientAuthStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return s.store.DeleteOAuthAuthRequest(ctx, model.OAuthState(state))
}

func (s *clientAuthStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	sess, err := s.store.GetOAuthSession(ctx, model.DID(did), model.OAuthSessionID(sessionID))
	if err != nil {
		return nil, err
	}
	data := toClientSessionData(sess)
	return &data, nil
}

func (s *clientAuthStore) SaveSession(ctx context.Context, data oauth.ClientSessionData) error {
	sess, err := toSession(data)
	if err != nil {
		return err
	}
	return s.store.SaveOAuthSession(ctx, sess)
}

func (s *clientAuthStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.store.DeleteOAuthSession(ctx, model.DID(did), model.OAuthSessionID(sessionID))
}

func toSession(data oauth.ClientSessionData) (model.OAuthSession, error) {
	var urls urlParser
	sess := model.OAuthSession{
		DID:                          model.DID(data.AccountDID),
		SessionID:                    model.OAuthSessionID(data.SessionID),
		HostURL:                      urls.parse("host URL", data.HostURL),
		AuthServerURL:                urls.parse("auth server URL", data.AuthServerURL),
		AuthServerTokenEndpoint:      urls.parse("token endpoint", data.AuthServerTokenEndpoint),
		AuthServerRevocationEndpoint: urls.parseOptional("revocation endpoint", data.AuthServerRevocationEndpoint),
		Scopes:                       data.Scopes,
		AccessToken:                  data.AccessToken,
		RefreshToken:                 data.RefreshToken,
		DPoPAuthServerNonce:          data.DPoPAuthServerNonce,
		DPoPHostNonce:                data.DPoPHostNonce,
		DPoPPrivateKeyMultibase:      data.DPoPPrivateKeyMultibase,
	}
	if urls.err != nil {
		return model.OAuthSession{}, urls.err
	}
	return sess, nil
}

func toClientSessionData(sess model.OAuthSession) oauth.ClientSessionData {
	return oauth.ClientSessionData{
		AccountDID:                   syntax.DID(sess.DID),
		SessionID:                    sess.SessionID.String(),
		HostURL:                      urlString(sess.HostURL),
		AuthServerURL:                urlString(sess.AuthServerURL),
		AuthServerTokenEndpoint:      urlString(sess.AuthServerTokenEndpoint),
		AuthServerRevocationEndpoint: urlString(sess.AuthServerRevocationEndpoint),
		Scopes:                       sess.Scopes,
		AccessToken:                  sess.AccessToken,
		RefreshToken:                 sess.RefreshToken,
		DPoPAuthServerNonce:          sess.DPoPAuthServerNonce,
		DPoPHostNonce:                sess.DPoPHostNonce,
		DPoPPrivateKeyMultibase:      sess.DPoPPrivateKeyMultibase,
	}
}

// urlParser parses the SDK's URL strings, keeping the first error so a value can be converted in one expression and
// checked once.
type urlParser struct {
	err error
}

// parse the named URL, which must be absolute, with a scheme and a host.
func (p *urlParser) parse(name, value string) *url.URL {
	u, err := url.Parse(value)
	if err == nil && (u.Scheme == "" || u.Host == "") {
		err = fmt.Errorf("%q is not an absolute URL", value)
	}
	if err != nil {
		if p.err == nil {
			p.err = fmt.Errorf("%v: %w", name, err)
		}
		return nil
	}
	return u
}

// parseOptional URL, which is nil when empty.
func (p *urlParser) parseOptional(name, value string) *url.URL {
	if value == "" {
		return nil
	}
	return p.parse(name, value)
}

// urlString for the SDK, which is empty for a nil URL.
func urlString(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}
