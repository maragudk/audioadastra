package model

import "fmt"

// CollectionActorProfile is the NSID of the account profile record collection, whose one record has
// the fixed record key "self".
const CollectionActorProfile = "com.audioadastra.actor.profile"

// Handle of an atproto account, such as alice.bsky.social. Handles are mutable and verified against
// the DID they claim; one that does not verify is [HandleInvalid].
type Handle string

// HandleInvalid is the handle of an account whose declared handle does not point back at its DID.
const HandleInvalid Handle = "handle.invalid"

// String satisfies [fmt.Stringer].
func (h Handle) String() string {
	return string(h)
}

var _ fmt.Stringer = Handle("")

// AuthFlow is a login that has been pushed to the account's auth server and awaits the user's consent,
// with what was learned about the account on the way.
type AuthFlow struct {
	// RedirectURL the user must be sent to for consent.
	RedirectURL string
	// State identifying the flow, which the auth server sends back with the callback.
	State          string
	DID            DID
	Handle         Handle
	PDSHost        string
	AuthServerHost string
}

// OAuthAuthRequest is a pending OAuth authorization request, from the pushed authorization request
// until the callback for its state arrives.
type OAuthAuthRequest struct {
	State   string
	Created Time
	Updated Time
	// AccountDID the flow was started for, or empty when it was started from an auth server URL.
	AccountDID                   DID
	AuthServerURL                string
	AuthServerTokenEndpoint      string
	AuthServerRevocationEndpoint string
	Scopes                       []string
	RequestURI                   string
	PKCEVerifier                 string
	DPoPAuthServerNonce          string
	DPoPPrivateKeyMultibase      string
}

// OAuthSession is an established OAuth session of one account on one device, with the tokens and
// keys to call the account's PDS.
type OAuthSession struct {
	DID       DID
	SessionID string
	Created   Time
	Updated   Time
	// HostURL of the PDS.
	HostURL                      string
	AuthServerURL                string
	AuthServerTokenEndpoint      string
	AuthServerRevocationEndpoint string
	// Scopes the auth server granted.
	Scopes                  []string
	AccessToken             string
	RefreshToken            string
	DPoPAuthServerNonce     string
	DPoPHostNonce           string
	DPoPPrivateKeyMultibase string
}
