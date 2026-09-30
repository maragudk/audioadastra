package atprototest

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"app/atproto"
)

// Local is the atproto network from the repository's docker compose file: a PLC directory, a PDS
// behind a TLS proxy, and handles under a suffix nothing resolves.
type Local struct {
	PDSURL       string
	PLCURL       string
	CAFile       string
	HandleSuffix string

	client *http.Client
}

// LocalNetwork for tests that need the real thing rather than the fakes. The test is skipped in short
// mode, and fails with the command to run when the network is not up. The proxy's root certificate
// is copied out of the compose stack if it is not on disk yet.
func LocalNetwork(t *testing.T) *Local {
	t.Helper()

	if testing.Short() {
		t.SkipNow()
	}

	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(root, "data", "caddy-root.crt")
	if _, err := os.Stat(caFile); err != nil {
		if err := os.MkdirAll(filepath.Dir(caFile), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("docker", "compose", "cp", "caddy:/data/caddy/pki/authorities/local/root.crt", caFile)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("local atproto network is not up, run make test-up: copying the proxy root certificate: %v: %s", err, out)
		}
	}

	l := &Local{
		PDSURL:       "https://pds.localhost",
		PLCURL:       "http://localhost:2582",
		CAFile:       caFile,
		HandleSuffix: ".test",
	}
	// Dials the PDS host on loopback whether or not the system resolver knows the name, and trusts
	// the proxy's root certificate.
	l.client, err = atproto.NewLocalHTTPClient(l.CAFile, l.HandleSuffix)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{l.PDSURL + "/xrpc/_health", l.PLCURL + "/_health"} {
		res, err := l.client.Get(u)
		if err != nil {
			t.Fatalf("local atproto network is not up, run make test-up: %v", err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("local atproto network is not healthy, run make test-up: %v responded %v", u, res.Status)
		}
	}
	return l
}

// Account on the local PDS.
type Account struct {
	DID      syntax.DID
	Handle   syntax.Handle
	Password string
}

// CreateAccount on the local PDS with a handle no other test run has, and a password.
func (l *Local) CreateAccount(t *testing.T) Account {
	t.Helper()

	handle := syntax.Handle("test-" + randomHandlePart() + l.HandleSuffix)
	password := "correct-horse-" + randomHandlePart()
	body, err := json.Marshal(map[string]string{
		"email":    handle.String() + "@example.com",
		"handle":   handle.String(),
		"password": password,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := l.client.Post(l.PDSURL+"/xrpc/com.atproto.server.createAccount", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var out struct {
		DID     syntax.DID `json:"did"`
		Error   string     `json:"error"`
		Message string     `json:"message"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("creating account %v: %v: %v", handle, out.Error, out.Message)
	}
	return Account{DID: out.DID, Handle: handle, Password: password}
}

// GetRecord from the local PDS, and whether it exists.
func (l *Local) GetRecord(t *testing.T, did syntax.DID, collection, rkey string) (map[string]any, bool) {
	t.Helper()

	res, err := l.client.Get(fmt.Sprintf("%v/xrpc/com.atproto.repo.getRecord?repo=%v&collection=%v&rkey=%v", l.PDSURL, did, collection, rkey))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	var out struct {
		Value map[string]any `json:"value"`
		Error string         `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == http.StatusBadRequest && out.Error == "RecordNotFound" {
		return nil, false
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("getting record: %v: %v", res.Status, out.Error)
	}
	return out.Value, true
}

// repositoryRoot is the nearest ancestor of the working directory with a go.mod, where the compose
// file and the data directory are.
func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod above the working directory")
		}
		dir = parent
	}
}

// randomHandlePart of lowercase letters and digits, which handles allow.
func randomHandlePart() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
