package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"app/model"
)

// Store persists auth requests and sessions, in the app's own types.
type Store interface {
	GetOAuthAuthRequest(ctx context.Context, state string) (model.OAuthAuthRequest, error)
	SaveOAuthAuthRequest(ctx context.Context, r model.OAuthAuthRequest) error
	DeleteOAuthAuthRequest(ctx context.Context, state string) error
	GetOAuthSession(ctx context.Context, did model.DID, sessionID string) (model.OAuthSession, error)
	SaveOAuthSession(ctx context.Context, s model.OAuthSession) error
	DeleteOAuthSession(ctx context.Context, did model.DID, sessionID string) error
}

// clientAuthStore adapts a [Store] to the OAuth client's own store interface, converting between the
// app's types and the client's.
type clientAuthStore struct {
	store Store
}

var _ oauth.ClientAuthStore = (*clientAuthStore)(nil)

func (s *clientAuthStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	r, err := s.store.GetOAuthAuthRequest(ctx, state)
	if err != nil {
		return nil, err
	}

	info := oauth.AuthRequestData{
		State:                        r.State,
		AuthServerURL:                r.AuthServerURL,
		Scopes:                       r.Scopes,
		RequestURI:                   r.RequestURI,
		AuthServerTokenEndpoint:      r.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: r.AuthServerRevocationEndpoint,
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
	r := model.OAuthAuthRequest{
		State:                        info.State,
		AuthServerURL:                info.AuthServerURL,
		AuthServerTokenEndpoint:      info.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: info.AuthServerRevocationEndpoint,
		Scopes:                       info.Scopes,
		RequestURI:                   info.RequestURI,
		PKCEVerifier:                 info.PKCEVerifier,
		DPoPAuthServerNonce:          info.DPoPAuthServerNonce,
		DPoPPrivateKeyMultibase:      info.DPoPPrivateKeyMultibase,
	}
	if info.AccountDID != nil {
		r.AccountDID = model.DID(*info.AccountDID)
	}
	return s.store.SaveOAuthAuthRequest(ctx, r)
}

func (s *clientAuthStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	return s.store.DeleteOAuthAuthRequest(ctx, state)
}

func (s *clientAuthStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	sess, err := s.store.GetOAuthSession(ctx, model.DID(did), sessionID)
	if err != nil {
		return nil, err
	}
	data := toClientSessionData(sess)
	return &data, nil
}

func (s *clientAuthStore) SaveSession(ctx context.Context, data oauth.ClientSessionData) error {
	return s.store.SaveOAuthSession(ctx, toSession(data))
}

func (s *clientAuthStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	return s.store.DeleteOAuthSession(ctx, model.DID(did), sessionID)
}

func toSession(data oauth.ClientSessionData) model.OAuthSession {
	return model.OAuthSession{
		DID:                          model.DID(data.AccountDID),
		SessionID:                    data.SessionID,
		HostURL:                      data.HostURL,
		AuthServerURL:                data.AuthServerURL,
		AuthServerTokenEndpoint:      data.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: data.AuthServerRevocationEndpoint,
		Scopes:                       data.Scopes,
		AccessToken:                  data.AccessToken,
		RefreshToken:                 data.RefreshToken,
		DPoPAuthServerNonce:          data.DPoPAuthServerNonce,
		DPoPHostNonce:                data.DPoPHostNonce,
		DPoPPrivateKeyMultibase:      data.DPoPPrivateKeyMultibase,
	}
}

func toClientSessionData(sess model.OAuthSession) oauth.ClientSessionData {
	return oauth.ClientSessionData{
		AccountDID:                   syntax.DID(sess.DID),
		SessionID:                    sess.SessionID,
		HostURL:                      sess.HostURL,
		AuthServerURL:                sess.AuthServerURL,
		AuthServerTokenEndpoint:      sess.AuthServerTokenEndpoint,
		AuthServerRevocationEndpoint: sess.AuthServerRevocationEndpoint,
		Scopes:                       sess.Scopes,
		AccessToken:                  sess.AccessToken,
		RefreshToken:                 sess.RefreshToken,
		DPoPAuthServerNonce:          sess.DPoPAuthServerNonce,
		DPoPHostNonce:                sess.DPoPHostNonce,
		DPoPPrivateKeyMultibase:      sess.DPoPPrivateKeyMultibase,
	}
}
