package model

import (
	"fmt"
	"net/url"

	"maragu.dev/glue/model"
)

type UserID = model.UserID

type User struct {
	ID      UserID
	Created Time
	Updated Time
	DID     DID
	Active  bool
}

// OAuthState is the random value identifying one OAuth flow, from the pushed authorization request
// until the auth server sends it back with the callback.
type OAuthState string

// String satisfies [fmt.Stringer].
func (s OAuthState) String() string {
	return string(s)
}

var _ fmt.Stringer = OAuthState("")

// OAuthSessionID identifies one OAuth session of an account, which is one device's login; together with
// the account's DID it names the session.
type OAuthSessionID string

// String satisfies [fmt.Stringer].
func (s OAuthSessionID) String() string {
	return string(s)
}

var _ fmt.Stringer = OAuthSessionID("")

// OAuthCallback is what the auth server sends to the callback URL: a state with either a code from the
// issuer, or an error.
type OAuthCallback struct {
	// State of the flow the callback is for.
	State OAuthState
	// Code to exchange for tokens, on success.
	Code string
	// Issuer is the auth server the callback comes from, on success. It is a string rather than a URL,
	// since it is compared byte for byte with the auth server URL the flow was started with.
	Issuer string
	// Error code, such as access_denied, when the flow failed.
	Error string
	// ErrorDescription for humans, optional with an error.
	ErrorDescription string
	// ErrorURI of a page about the error, optional with an error.
	ErrorURI string
}

// LoginStart is what a started login needs next: where to send the user for consent, and the state
// that identifies the flow when the auth server calls back.
type LoginStart struct {
	// RedirectURL the user must be sent to for consent.
	RedirectURL string
	// State identifying the flow, which the auth server sends back with the callback.
	State OAuthState
}

// AuthFlow is a login that has been pushed to the account's auth server and awaits the user's consent,
// with what was learned about the account on the way.
type AuthFlow struct {
	// RedirectURL the user must be sent to for consent.
	RedirectURL string
	// State identifying the flow, which the auth server sends back with the callback.
	State OAuthState

	DID            DID
	Handle         Handle
	PDSHost        string
	AuthServerHost string
}

// OAuthAuthRequest is a pending OAuth authorization request, from the pushed authorization request
// until the callback for its state arrives.
type OAuthAuthRequest struct {
	State   OAuthState
	Created Time
	Updated Time
	// AccountDID the flow was started for, or empty when it was started from an auth server URL.
	AccountDID DID
	// AuthServerURL and AuthServerTokenEndpoint are never nil.
	AuthServerURL           *url.URL
	AuthServerTokenEndpoint *url.URL
	// AuthServerRevocationEndpoint, or nil when the auth server has none.
	AuthServerRevocationEndpoint *url.URL
	Scopes                       []string
	// RequestURI from the pushed authorization request, an opaque identifier rather than a web URL.
	RequestURI              string
	PKCEVerifier            string
	DPoPAuthServerNonce     string
	DPoPPrivateKeyMultibase string
}

// OAuthSession is an established OAuth session of one account on one device, with the tokens and
// keys to call the account's PDS.
type OAuthSession struct {
	DID       DID
	SessionID OAuthSessionID
	Created   Time
	Updated   Time
	// HostURL of the PDS. It, AuthServerURL and AuthServerTokenEndpoint are never nil.
	HostURL                 *url.URL
	AuthServerURL           *url.URL
	AuthServerTokenEndpoint *url.URL
	// AuthServerRevocationEndpoint, or nil when the auth server has none.
	AuthServerRevocationEndpoint *url.URL
	// Scopes the auth server granted.
	Scopes                  []string
	AccessToken             string
	RefreshToken            string
	DPoPAuthServerNonce     string
	DPoPHostNonce           string
	DPoPPrivateKeyMultibase string
}
