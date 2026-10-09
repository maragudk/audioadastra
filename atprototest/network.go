// Package atprototest provides an in-process fake of the parts of the atmosphere the app touches: an
// OAuth authorization server, a PDS, and an identity directory that points at them. Nothing in here
// reaches the network, and the fakes speak enough of the protocol (PAR, PKCE, DPoP nonces, token
// exchange, revocation, record reads and writes, blob uploads, server description) for the real OAuth
// client to run against them.
package atprototest

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"app/atproto"
	"app/model"
	"app/sqlite"
)

// AliceDID is the example account for tests against the fakes. A plc DID is exactly 24 characters of
// base32, so it is well-formed although nothing ever registered it.
const AliceDID = "did:plc:alicealicealicealicealic"

// Network of fakes. The auth server lives at [Network.AuthServerURL] and the PDS at [Network.PDSURL];
// both are served by one TLS test server that [Network.Transport] routes every https host to, so the
// fakes can use real-looking hostnames. Register extra hosts with [Network.Route].
//
// Behaviour knobs are plain fields, set before the step they affect, and hosts are routed before any
// request is made. Everything else is safe for concurrent use.
type Network struct {
	AuthServerURL string
	PDSURL        string
	Directory     *identity.MockDirectory
	Transport     *http.Transport
	Client        *http.Client

	// Deny makes the auth server send the user back with an access_denied error instead of a code.
	Deny bool
	// GrantScopes, when set, is the scope string the auth server grants instead of what was requested.
	GrantScopes string
	// PutRecordFails makes the PDS respond with a server error to every putRecord.
	PutRecordFails bool
	// PutRecordRaces makes a record appear at the key just before every putRecord is applied, as a
	// concurrent writer would have put it, so a write conditional on absence loses.
	PutRecordRaces bool
	// Stall makes the auth server and the PDS hold every request until the client gives up on it.
	Stall bool
	// LookupFails makes every identity lookup fail, as an unreachable directory would.
	LookupFails bool
	// BlobUploadLimit, when set, is the blob size limit in bytes the PDS reports from describeServer and
	// enforces on uploadBlob, refusing a larger blob with 413 PayloadTooLarge.
	BlobUploadLimit int64
	// DescribeServerFails makes the PDS respond with a server error to every describeServer.
	DescribeServerFails bool
	// DetectedMIMEType, when set, is the MIME type the PDS labels every blob with instead of the one the
	// client declared, as a PDS that reads the type from the bytes does.
	DetectedMIMEType string
	// UploadBlobFails makes the PDS respond with a server error to every uploadBlob.
	UploadBlobFails bool
	// UploadBlobScopeMissing makes the PDS refuse every uploadBlob with 403 ScopeMissingError, as a PDS
	// does when the blob's type is outside the blob scopes the session was granted.
	UploadBlobScopeMissing bool
	// UploadBlobDelay, when set, is how long the PDS takes over every uploadBlob once it has the blob,
	// as a PDS storing a large one does.
	UploadBlobDelay time.Duration
	// UploadBlobRotatesNonce makes the PDS rotate its DPoP nonce as the first uploadBlob arrives, so it is
	// refused with the challenge to retry before the PDS reads any of the blob.
	UploadBlobRotatesNonce bool
	// CreateRecordFails makes the PDS respond with a server error to every createRecord.
	CreateRecordFails bool
	// CreateRecordScopeMissing makes the PDS refuse every createRecord with 403 ScopeMissingError, as a
	// PDS does for a session that was not granted the collection.
	CreateRecordScopeMissing bool

	server *httptest.Server
	hosts  map[string]string
	nonce  string

	mu             sync.Mutex
	parRequests    map[string]parRequest
	codes          map[string]parRequest
	accessTokens   map[string]syntax.DID
	refreshTokens  map[string]syntax.DID
	revoked        []string
	records        map[string]map[string]any
	blobs          map[string]Blob
	pdsNonceValue  string
	putRecordCalls int

	uploadBlobRotated bool
	tids              *syntax.TIDClock

	describeServerCalls int
	uploadBlobCalls     int
	createRecordCalls   int
}

// Blob stored on the fake PDS.
type Blob struct {
	// MIMEType the PDS labelled the blob with, and DeclaredMIMEType the client declared.
	MIMEType         string
	DeclaredMIMEType string
	Content          []byte
}

type parRequest struct {
	state         string
	redirectURI   string
	scope         string
	codeChallenge string
	clientID      string
	loginHint     string
}

// NewNetwork with no accounts. It is torn down with the test.
func NewNetwork(t *testing.T) *Network {
	t.Helper()

	n := &Network{
		AuthServerURL: "https://auth.test",
		PDSURL:        "https://pds.test",
		Directory:     identity.NewMockDirectory(),
		hosts:         map[string]string{},
		nonce:         randomToken(),
		parRequests:   map[string]parRequest{},
		codes:         map[string]parRequest{},
		accessTokens:  map[string]syntax.DID{},
		refreshTokens: map[string]syntax.DID{},
		records:       map[string]map[string]any{},
		blobs:         map[string]Blob{},
		tids:          syntax.NewTIDClock(0),
	}

	n.server = httptest.NewTLSServer(http.HandlerFunc(n.serve))
	t.Cleanup(n.server.Close)

	pool := x509.NewCertPool()
	pool.AddCert(n.server.Certificate())
	serverAddr := n.server.Listener.Addr().String()
	dialer := &net.Dialer{}
	n.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if port == "443" {
				if routed, ok := n.hosts[host]; ok {
					addr = routed
				} else {
					addr = serverAddr
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	n.Client = &http.Client{Transport: n.Transport}

	return n
}

// Route the given https host to a test server of its own, such as the app under test, instead of the
// fakes. The server must present the same test certificate, which every [httptest.Server] does.
func (n *Network) Route(host string, server *httptest.Server) {
	n.hosts[host] = server.Listener.Addr().String()
}

// AddAccount with the given DID and handle, hosted on the fake PDS. A handle of [model.HandleInvalid]
// gives an account whose handle does not verify.
func (n *Network) AddAccount(did model.DID, handle model.Handle) {
	n.Directory.Insert(identity.Identity{
		DID:    syntax.DID(did),
		Handle: syntax.Handle(handle),
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: n.PDSURL},
		},
	})
}

// NewClient for the fake network: the app's own confidential client for https://app.test with a
// fresh P-256 key, whose HTTP client and identity directory are the fakes'. The options, if any, change
// the client's options before it is made.
func (n *Network) NewClient(t *testing.T, db *sqlite.Database, options ...func(*atproto.NewClientOptions)) *atproto.Client {
	t.Helper()

	key, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	opts := atproto.NewClientOptions{
		BaseURL:             &url.URL{Scheme: "https", Host: "app.test"},
		PrivateKeyMultibase: key.Multibase(),
		KeyID:               "test",
		Store:               db,
		Directory:           &directory{n: n},
		HTTPClient:          n.Client,
	}
	for _, option := range options {
		option(&opts)
	}
	client, err := atproto.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// Authorize as the user would in a browser: visit the redirect URL returned from starting a login, and
// return the query the auth server sends back to the callback.
func (n *Network) Authorize(t *testing.T, redirectURL *url.URL) url.Values {
	t.Helper()

	client := &http.Client{
		Transport: n.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Get(redirectURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("authorize responded %v", res.Status)
	}
	location, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return location.Query()
}

// GetRecord from the fake PDS, and whether it exists.
func (n *Network) GetRecord(did model.DID, collection model.NSID, rkey model.RecordKey) (map[string]any, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	record, ok := n.records[recordKey(did.String(), collection.String(), rkey.String())]
	return record, ok
}

// PutRecordCalls made to the fake PDS so far, failed ones included.
func (n *Network) PutRecordCalls() int {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.putRecordCalls
}

// GetBlob from the fake PDS by its CID, and whether it exists.
func (n *Network) GetBlob(cid model.CID) (Blob, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	blob, ok := n.blobs[cid.String()]
	return blob, ok
}

// Records in the collection of the account on the fake PDS, by record key.
func (n *Network) Records(did model.DID, collection model.NSID) map[model.RecordKey]map[string]any {
	n.mu.Lock()
	defer n.mu.Unlock()

	prefix := recordKey(did.String(), collection.String(), "")
	records := map[model.RecordKey]map[string]any{}
	for key, record := range n.records {
		if rkey, ok := strings.CutPrefix(key, prefix); ok {
			records[model.RecordKey(rkey)] = record
		}
	}
	return records
}

// RotatePDSNonce to a new DPoP nonce, so the next request to the fake PDS with the old one is refused
// with a challenge to retry with the new one.
func (n *Network) RotatePDSNonce() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.pdsNonceValue = randomToken()
}

// ForgetTokens the auth server issued, as when they are revoked from elsewhere, so the PDS refuses them
// and the auth server will not refresh them.
func (n *Network) ForgetTokens() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.accessTokens = map[string]syntax.DID{}
	n.refreshTokens = map[string]syntax.DID{}
}

// pdsNonce the fake PDS currently requires: the auth server's, until it is rotated.
func (n *Network) pdsNonce() string {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.pdsNonceValue == "" {
		return n.nonce
	}
	return n.pdsNonceValue
}

// DescribeServerCalls made to the fake PDS so far, failed ones included.
func (n *Network) DescribeServerCalls() int {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.describeServerCalls
}

// UploadBlobCalls made to the fake PDS so far, failed ones included.
func (n *Network) UploadBlobCalls() int {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.uploadBlobCalls
}

// CreateRecordCalls made to the fake PDS so far, failed ones included.
func (n *Network) CreateRecordCalls() int {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.createRecordCalls
}

// Revoked tokens, in the order the auth server received them.
func (n *Network) Revoked() []string {
	n.mu.Lock()
	defer n.mu.Unlock()

	return append([]string(nil), n.revoked...)
}

func (n *Network) serve(w http.ResponseWriter, r *http.Request) {
	if n.Stall {
		// The server only notices the client going away once the request body has been read.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
		return
	}

	switch r.Host + r.URL.Path {
	case "auth.test/.well-known/oauth-authorization-server":
		n.serveAuthServerMetadata(w, r)
	case "auth.test/oauth/par":
		n.servePAR(w, r)
	case "auth.test/oauth/authorize":
		n.serveAuthorize(w, r)
	case "auth.test/oauth/token":
		n.serveToken(w, r)
	case "auth.test/oauth/revoke":
		n.serveRevoke(w, r)
	case "pds.test/.well-known/oauth-protected-resource":
		writeJSON(w, http.StatusOK, map[string]any{"resource": n.PDSURL, "authorization_servers": []string{n.AuthServerURL}})
	case "pds.test/xrpc/com.atproto.repo.getRecord":
		n.serveGetRecord(w, r)
	case "pds.test/xrpc/com.atproto.repo.putRecord":
		n.servePutRecord(w, r)
	case "pds.test/xrpc/com.atproto.repo.createRecord":
		n.serveCreateRecord(w, r)
	case "pds.test/xrpc/com.atproto.repo.uploadBlob":
		n.serveUploadBlob(w, r)
	case "pds.test/xrpc/com.atproto.server.describeServer":
		n.serveDescribeServer(w, r)
	case "pds.test/xrpc/com.atproto.server.getSession":
		if did, ok := n.requirePDSAuth(w, r); ok {
			writeJSON(w, http.StatusOK, map[string]any{"did": did.String(), "handle": "handle.invalid"})
		}
	default:
		http.NotFound(w, r)
	}
}

func (n *Network) serveAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                           n.AuthServerURL,
		"authorization_endpoint":                           n.AuthServerURL + "/oauth/authorize",
		"token_endpoint":                                   n.AuthServerURL + "/oauth/token",
		"revocation_endpoint":                              n.AuthServerURL + "/oauth/revoke",
		"pushed_authorization_request_endpoint":            n.AuthServerURL + "/oauth/par",
		"response_types_supported":                         []string{"code"},
		"grant_types_supported":                            []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":                 []string{"S256"},
		"token_endpoint_auth_methods_supported":            []string{"none", "private_key_jwt"},
		"token_endpoint_auth_signing_alg_values_supported": []string{"ES256"},
		"scopes_supported":                                 []string{"atproto"},
		"authorization_response_iss_parameter_supported":   true,
		"require_pushed_authorization_requests":            true,
		"dpop_signing_alg_values_supported":                []string{"ES256"},
		"client_id_metadata_document_supported":            true,
	})
}

// requireAuthServerDPoP does the DPoP nonce dance an auth server does: a proof without the current
// nonce is refused with use_dpop_nonce and the nonce to use, which the client is expected to retry with.
func (n *Network) requireAuthServerDPoP(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("DPoP-Nonce", n.nonce)
	proof := r.Header.Get("DPoP")
	if proof == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_dpop_proof"})
		return false
	}
	if dpopNonce(proof) != n.nonce {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "use_dpop_nonce"})
		return false
	}
	return true
}

func (n *Network) servePAR(w http.ResponseWriter, r *http.Request) {
	if !n.requireAuthServerDPoP(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	if !n.validClientAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
		return
	}

	requestURI := "urn:ietf:params:oauth:request_uri:" + randomToken()
	n.mu.Lock()
	n.parRequests[requestURI] = parRequest{
		state:         r.PostForm.Get("state"),
		redirectURI:   r.PostForm.Get("redirect_uri"),
		scope:         r.PostForm.Get("scope"),
		codeChallenge: r.PostForm.Get("code_challenge"),
		clientID:      r.PostForm.Get("client_id"),
		loginHint:     r.PostForm.Get("login_hint"),
	}
	n.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"request_uri": requestURI, "expires_in": 60})
}

// validClientAuth checks that a client sends a client assertion issued by itself, unless it is a
// localhost development client, which has no key. The signature is not verified, which is enough for a
// fake: the point is that a confidential client sends one.
func (n *Network) validClientAuth(r *http.Request) bool {
	assertion := r.PostForm.Get("client_assertion")
	if assertion == "" {
		return strings.HasPrefix(r.PostForm.Get("client_id"), "http://localhost")
	}
	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(assertion, &claims); err != nil {
		return false
	}
	return claims.Issuer == r.PostForm.Get("client_id")
}

func (n *Network) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	par, ok := n.parRequests[r.URL.Query().Get("request_uri")]
	n.mu.Unlock()
	if !ok || par.clientID != r.URL.Query().Get("client_id") {
		http.Error(w, "unknown request", http.StatusBadRequest)
		return
	}

	params := url.Values{}
	params.Set("state", par.state)
	params.Set("iss", n.AuthServerURL)
	if n.Deny {
		params.Set("error", "access_denied")
		params.Set("error_description", "the user said no")
		params.Set("error_uri", n.AuthServerURL+"/errors/access_denied")
		http.Redirect(w, r, par.redirectURI+"?"+params.Encode(), http.StatusFound)
		return
	}

	code := randomToken()
	n.mu.Lock()
	n.codes[code] = par
	n.mu.Unlock()
	params.Set("code", code)
	http.Redirect(w, r, par.redirectURI+"?"+params.Encode(), http.StatusFound)
}

func (n *Network) serveToken(w http.ResponseWriter, r *http.Request) {
	if !n.requireAuthServerDPoP(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}
	if !n.validClientAuth(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
		return
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		n.mu.Lock()
		par, ok := n.codes[r.PostForm.Get("code")]
		delete(n.codes, r.PostForm.Get("code"))
		n.mu.Unlock()
		if !ok || par.clientID != r.PostForm.Get("client_id") || par.redirectURI != r.PostForm.Get("redirect_uri") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		if oauth.S256CodeChallenge(r.PostForm.Get("code_verifier")) != par.codeChallenge {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "PKCE verifier mismatch"})
			return
		}

		did, err := n.subject(r.Context(), par.loginHint)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": err.Error()})
			return
		}
		scope := par.scope
		if n.GrantScopes != "" {
			scope = n.GrantScopes
		}
		n.issueTokens(w, did, scope)

	case "refresh_token":
		n.mu.Lock()
		did, ok := n.refreshTokens[r.PostForm.Get("refresh_token")]
		delete(n.refreshTokens, r.PostForm.Get("refresh_token"))
		n.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		n.issueTokens(w, did, "")

	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
	}
}

func (n *Network) subject(ctx context.Context, loginHint string) (syntax.DID, error) {
	atid, err := syntax.ParseAtIdentifier(loginHint)
	if err != nil {
		return "", fmt.Errorf("no subject for login hint %q: %w", loginHint, err)
	}
	ident, err := n.Directory.Lookup(ctx, atid)
	if err != nil {
		return "", err
	}
	return ident.DID, nil
}

func (n *Network) issueTokens(w http.ResponseWriter, did syntax.DID, scope string) {
	accessToken, refreshToken := "access-"+randomToken(), "refresh-"+randomToken()
	n.mu.Lock()
	n.accessTokens[accessToken] = did
	n.refreshTokens[refreshToken] = did
	n.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "DPoP",
		"expires_in":    3600,
		"scope":         scope,
		"sub":           did.String(),
	})
}

func (n *Network) serveRevoke(w http.ResponseWriter, r *http.Request) {
	if !n.requireAuthServerDPoP(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
		return
	}

	n.mu.Lock()
	n.revoked = append(n.revoked, r.PostForm.Get("token"))
	delete(n.accessTokens, r.PostForm.Get("token"))
	delete(n.refreshTokens, r.PostForm.Get("token"))
	n.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{})
}

// requirePDSAuth checks the DPoP-bound access token the way a PDS does, including the nonce dance,
// which arrives as a 401 with a WWW-Authenticate challenge rather than the auth server's 400.
func (n *Network) requirePDSAuth(w http.ResponseWriter, r *http.Request) (syntax.DID, bool) {
	nonce := n.pdsNonce()
	w.Header().Set("DPoP-Nonce", nonce)
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "DPoP ")
	if !ok {
		w.Header().Set("WWW-Authenticate", `DPoP error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "AuthMissing"})
		return "", false
	}
	if dpopNonce(r.Header.Get("DPoP")) != nonce {
		w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce", error_description="Resource server requires nonce in DPoP proof"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "use_dpop_nonce"})
		return "", false
	}

	n.mu.Lock()
	did, ok := n.accessTokens[token]
	n.mu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", `DPoP error="invalid_token", error_description="unknown token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "InvalidToken"})
		return "", false
	}
	return did, true
}

func (n *Network) serveGetRecord(w http.ResponseWriter, r *http.Request) {
	if _, ok := n.requirePDSAuth(w, r); !ok {
		return
	}

	q := r.URL.Query()
	did, err := syntax.ParseDID(q.Get("repo"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "InvalidRequest", "message": err.Error()})
		return
	}
	record, ok := n.GetRecord(model.DID(did), model.NSID(q.Get("collection")), model.RecordKey(q.Get("rkey")))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "RecordNotFound", "message": "Could not locate record"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uri":   fmt.Sprintf("at://%s/%s/%s", did, q.Get("collection"), q.Get("rkey")),
		"cid":   "bafyfake",
		"value": record,
	})
}

func (n *Network) servePutRecord(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	n.putRecordCalls++
	n.mu.Unlock()

	did, ok := n.requirePDSAuth(w, r)
	if !ok {
		return
	}
	if n.PutRecordFails {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "InternalServerError", "message": "the fake PDS is down"})
		return
	}

	var body struct {
		Repo       string          `json:"repo"`
		Collection string          `json:"collection"`
		RKey       string          `json:"rkey"`
		Record     map[string]any  `json:"record"`
		SwapRecord json.RawMessage `json:"swapRecord"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "InvalidRequest", "message": err.Error()})
		return
	}
	if body.Repo != did.String() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "InvalidRequest", "message": "repo does not match the authenticated account"})
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	key := recordKey(did.String(), body.Collection, body.RKey)
	if n.PutRecordRaces {
		n.records[key] = map[string]any{"$type": body.Collection, "createdAt": "2000-01-01T00:00:00.000Z"}
	}
	// A swapRecord of null means the record must not exist yet; a CID means it must be the current
	// one. The fake has no CIDs, so only the null form is honoured.
	if _, exists := n.records[key]; exists && string(body.SwapRecord) == "null" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "InvalidSwap", "message": "Record was at bafyfake"})
		return
	}
	n.records[key] = body.Record

	writeJSON(w, http.StatusOK, map[string]any{
		"uri": fmt.Sprintf("at://%s/%s/%s", did, body.Collection, body.RKey),
		"cid": "bafyfake",
	})
}

func (n *Network) serveCreateRecord(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	n.createRecordCalls++
	n.mu.Unlock()

	did, ok := n.requirePDSAuth(w, r)
	if !ok {
		return
	}
	if n.CreateRecordFails {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "InternalServerError", "message": "the fake PDS is down"})
		return
	}
	if n.CreateRecordScopeMissing {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "ScopeMissingError", "message": "Missing required scope \"repo:com.audioadastra.track?action=create\""})
		return
	}

	var body struct {
		Repo       string          `json:"repo"`
		Collection string          `json:"collection"`
		Record     json.RawMessage `json:"record"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "InvalidRequest", "message": err.Error()})
		return
	}
	if body.Repo != did.String() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "InvalidRequest", "message": "repo does not match the authenticated account"})
		return
	}
	var record map[string]any
	if err := json.Unmarshal(body.Record, &record); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "InvalidRequest", "message": err.Error()})
		return
	}

	n.mu.Lock()
	rkey := n.tids.Next().String()
	n.records[recordKey(did.String(), body.Collection, rkey)] = record
	n.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"uri": fmt.Sprintf("at://%s/%s/%s", did, body.Collection, rkey),
		"cid": fakeCID(cid.DagCBOR, body.Record),
	})
}

func (n *Network) serveUploadBlob(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	n.uploadBlobCalls++
	rotate := n.UploadBlobRotatesNonce && !n.uploadBlobRotated
	n.uploadBlobRotated = n.uploadBlobRotated || rotate
	n.mu.Unlock()
	if rotate {
		n.RotatePDSNonce()
	}

	if _, ok := n.requirePDSAuth(w, r); !ok {
		return
	}
	declared := r.Header.Get("Content-Type")
	switch {
	case n.UploadBlobFails:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "InternalServerError", "message": "the fake PDS is down"})
		return
	case n.UploadBlobScopeMissing:
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "ScopeMissingError", "message": fmt.Sprintf("Missing required scope %q", "blob:"+declared)})
		return
	case n.BlobUploadLimit > 0 && r.ContentLength > n.BlobUploadLimit:
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "PayloadTooLarge", "message": "request entity too large"})
		return
	}

	content, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "InvalidRequest", "message": err.Error()})
		return
	}
	select {
	case <-time.After(n.UploadBlobDelay):
	case <-r.Context().Done():
		return
	}
	mimeType := declared
	if n.DetectedMIMEType != "" {
		mimeType = n.DetectedMIMEType
	}
	ref := fakeCID(cid.Raw, content)

	n.mu.Lock()
	n.blobs[ref] = Blob{MIMEType: mimeType, DeclaredMIMEType: declared, Content: content}
	n.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"blob": map[string]any{"$type": "blob", "ref": map[string]any{"$link": ref}, "mimeType": mimeType, "size": len(content)},
	})
}

func (n *Network) serveDescribeServer(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	n.describeServerCalls++
	n.mu.Unlock()

	if n.DescribeServerFails {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "InternalServerError", "message": "the fake PDS is down"})
		return
	}
	description := map[string]any{"did": "did:web:pds.test", "availableUserDomains": []string{".test"}}
	if n.BlobUploadLimit > 0 {
		description["blobUploadLimit"] = n.BlobUploadLimit
	}
	writeJSON(w, http.StatusOK, description)
}

func recordKey(did, collection, rkey string) string {
	return did + "/" + collection + "/" + rkey
}

// dpopNonce claimed in a DPoP proof, unverified: the fakes check the protocol dance, not signatures.
func dpopNonce(proof string) string {
	var claims struct {
		jwt.RegisteredClaims
		Nonce string `json:"nonce"`
	}
	if _, _, err := jwt.NewParser().ParseUnverified(proof, &claims); err != nil {
		return ""
	}
	return claims.Nonce
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fakeCID of the data, of the given codec, as a PDS computes CIDs: a SHA-256 multihash, here over
// whatever bytes the fake has rather than a canonical encoding.
func fakeCID(codec uint64, data []byte) string {
	c, err := cid.NewPrefixV1(codec, multihash.SHA2_256).Sum(data)
	if err != nil {
		panic(err)
	}
	return c.String()
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// directory of the fake network: the mock directory's identities, unless lookups are made to fail.
type directory struct {
	n *Network
}

func (d *directory) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	if d.n.LookupFails {
		return nil, identity.ErrDIDResolutionFailed
	}
	return d.n.Directory.Lookup(ctx, atid)
}

func (d *directory) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	if d.n.LookupFails {
		return nil, identity.ErrHandleResolutionFailed
	}
	return d.n.Directory.LookupHandle(ctx, handle)
}

func (d *directory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	if d.n.LookupFails {
		return nil, identity.ErrDIDResolutionFailed
	}
	return d.n.Directory.LookupDID(ctx, did)
}

func (d *directory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return d.n.Directory.Purge(ctx, atid)
}
