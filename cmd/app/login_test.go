package main

import (
	"context"
	"log/slog"
	"net"
	nethttp "net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/sync/errgroup"
	"maragu.dev/glue/sql"
	"maragu.dev/is"

	"app/atprototest"
	"app/model"
	"app/sqlite"
)

// TestLogin in a real Chrome against the local atproto network from the repository's docker compose
// file, with the app started in-process. It is skipped in short mode.
func TestLogin(t *testing.T) {
	t.Run("should log in through the PDS, write the profile, show the handle, and log out", func(t *testing.T) {
		network := atprototest.LocalNetwork(t)
		account := network.CreateAccount(t)
		app := startApp(t, network)
		b := newBrowser(t)

		b.logIn(t, app, account)

		var heading string
		b.run(t, "open the profile",
			chromedp.Click(`a[href="/profile"]`),
			waitLoaded(`#logout`),
			chromedp.Text(`h1`, &heading),
		)
		is.Equal(t, "@"+account.Handle.String(), heading)

		record, ok := network.GetRecord(t, account.DID, model.CollectionActorProfile, model.RecordKeySelf)
		is.True(t, ok, "no profile record on the PDS")
		is.Equal(t, any(model.CollectionActorProfile.String()), record["$type"])
		is.True(t, record["createdAt"] != nil, "no createdAt")
		is.Equal(t, 1, app.count(t, "oauth_sessions"))

		b.run(t, "log out",
			chromedp.Click(`#logout`),
			chromedp.WaitVisible(`a[href="/login"]`),
		)
		is.Equal(t, 0, app.count(t, "oauth_sessions"))
	})

	t.Run("should come back logged out when the user denies consent", func(t *testing.T) {
		network := atprototest.LocalNetwork(t)
		account := network.CreateAccount(t)
		app := startApp(t, network)
		b := newBrowser(t)

		var message string
		b.run(t, "deny consent",
			chromedp.Navigate(app.baseURL+"/login"),
			chromedp.WaitVisible(`#handle`),
			chromedp.SendKeys(`#handle`, account.Handle.String()),
			chromedp.Click(`form[action="/login"] button[type="submit"]`),
			waitLoaded(`input[type="password"]`),
			chromedp.SendKeys(`input[type="password"]`, account.Password),
			chromedp.Click(`//button[normalize-space()="Sign in"]`),
			chromedp.WaitVisible(`//button[normalize-space()="Deny access"]`),
			chromedp.Click(`//button[normalize-space()="Deny access"]`),
			// Back on the app's login page with the message.
			waitLoaded(`[role="alert"]`),
			chromedp.Text(`[role="alert"]`, &message),
		)
		is.True(t, strings.Contains(message, "cancelled or failed"), message)

		b.run(t, "check that nothing is logged in",
			chromedp.Navigate(app.baseURL+"/profile"),
			chromedp.WaitVisible(`#handle`),
		)
		is.Equal(t, 0, app.count(t, "oauth_sessions"))
		is.Equal(t, 0, app.count(t, "oauth_auth_requests"))
	})
}

// testApp is the real thing, started in-process on a free port with a database and a temporary data
// directory of its own.
type testApp struct {
	baseURL     string
	db          *sqlite.Database
	tempDataDir string
}

func startApp(t *testing.T, network *atprototest.Local) *testApp {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	is.NotError(t, err)
	address := listener.Addr().String()
	is.NotError(t, listener.Close())
	baseURL := "http://" + address

	databasePath := filepath.Join(t.TempDir(), "app.db")
	tempDataDir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	var eg errgroup.Group
	t.Cleanup(func() {
		cancel()
		is.NotError(t, eg.Wait())
	})

	// The app looks for its migrations relative to the working directory, which is the repository root
	// when it runs for real.
	t.Chdir("../..")
	t.Setenv("SERVER_ADDRESS", address)
	t.Setenv("APP_NAME", "test")
	t.Setenv("BASE_URL", baseURL)
	t.Setenv("CSP_ALLOW_UNSAFE_INLINE", "true")
	// Datastar evaluates its expressions with the Function constructor.
	t.Setenv("CSP_ALLOW_UNSAFE_EVAL", "true")
	t.Setenv("DATABASE_PATH", databasePath)
	t.Setenv("TEMP_DATA_DIR", tempDataDir)
	t.Setenv("JOB_QUEUE_TIMEOUT", "10s")
	t.Setenv("SECURE_COOKIE", "false")
	t.Setenv("OAUTH_PRIVATE_KEY", "")
	t.Setenv("OAUTH_KEY_ID", "")
	t.Setenv("ATPROTO_PLC_URL", network.PLCURL)
	t.Setenv("ATPROTO_CA_FILE", network.CAFile)
	t.Setenv("ATPROTO_LOCAL_HANDLE_SUFFIX", network.HandleSuffix)

	// The app makes its logger the default; put the previous one back before this test's logger ends
	// with the test.
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	log := slog.New(slog.NewTextHandler(&testWriter{t: t}, nil))
	is.NotError(t, start(ctx, log, &eg))

	// The server listens in the background; wait until it answers.
	for start := time.Now(); ; time.Sleep(50 * time.Millisecond) {
		res, err := nethttp.Get(baseURL + "/login")
		if err == nil {
			_ = res.Body.Close()
			break
		}
		if time.Since(start) > 10*time.Second {
			t.Fatalf("app did not start listening on %v: %v", address, err)
		}
	}

	h := sql.NewHelper(sql.NewHelperOptions{SQLite: sql.SQLiteOptions{Path: databasePath}})
	is.NotError(t, h.Connect(t.Context()))
	return &testApp{baseURL: baseURL, db: sqlite.NewDatabase(sqlite.NewDatabaseOptions{H: h}), tempDataDir: tempDataDir}
}

func (a *testApp) count(t *testing.T, table string) int {
	t.Helper()

	var count int
	is.NotError(t, a.db.H.Get(t.Context(), &count, `select count(*) from `+table))
	return count
}

// browser is a headless Chrome for one test, which trusts no certificate in particular: the local
// proxy's is self-issued.
type browser struct {
	ctx context.Context
}

func newBrowser(t *testing.T) *browser {
	t.Helper()

	execPath := os.Getenv("CHROME_PATH")
	if execPath == "" {
		switch runtime.GOOS {
		case "darwin":
			execPath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		default:
			execPath = "/usr/bin/google-chrome"
		}
	}
	if _, err := os.Stat(execPath); err != nil {
		t.Fatalf("no Chrome at %v (set CHROME_PATH): %v", execPath, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(execPath), chromedp.IgnoreCertErrors)
	ctx, cancel = chromedp.NewExecAllocator(ctx, opts...)
	t.Cleanup(cancel)
	ctx, cancel = chromedp.NewContext(ctx, chromedp.WithLogf(t.Logf))
	t.Cleanup(cancel)
	return &browser{ctx: ctx}
}

// logIn as the account through the PDS's sign-in and consent pages, ending back on the app.
func (b *browser) logIn(t *testing.T, app *testApp, account atprototest.Account) {
	t.Helper()

	b.run(t, "log in",
		chromedp.Navigate(app.baseURL+"/login"),
		chromedp.WaitVisible(`#handle`),
		chromedp.SendKeys(`#handle`, account.Handle.String()),
		chromedp.Click(`form[action="/login"] button[type="submit"]`),
		// The PDS sign-in page, with the identifier prefilled from the login hint.
		waitLoaded(`input[type="password"]`),
		chromedp.SendKeys(`input[type="password"]`, account.Password),
		chromedp.Click(`//button[normalize-space()="Sign in"]`),
		// The consent page.
		chromedp.WaitVisible(`//button[normalize-space()="Authorize"]`),
		chromedp.Click(`//button[normalize-space()="Authorize"]`),
		// Back on the app, logged in.
		waitLoaded(`a[href="/profile"]`),
	)
}

// run the actions as one step, and on failure keep a screenshot of where the browser was.
func (b *browser) run(t *testing.T, step string, actions ...chromedp.Action) {
	t.Helper()

	if err := chromedp.Run(b.ctx, actions...); err != nil {
		var screenshot []byte
		var location string
		if err := chromedp.Run(b.ctx, chromedp.Location(&location), chromedp.CaptureScreenshot(&screenshot)); err == nil {
			path := filepath.Join(t.TempDir(), "failure.png")
			if err := os.WriteFile(path, screenshot, 0o644); err == nil {
				t.Logf("screenshot of %v at %v", location, path)
			}
		}
		t.Fatalf("%v: %v", step, err)
	}
}

// waitLoaded until the element matching sel is visible and its document has finished loading.
//
// Chrome drops the IDs of all nodes in a document when the document's DOMContentLoaded fires, and
// pushes the document to chromedp again. Deferred scripts hold DOMContentLoaded back, so an element
// can be visible before then, and an action on it that straddles the event fails with "Could not
// find node with given id". [chromedp.Navigate] waits for the load event, but a click that navigates
// doesn't, so wait with this after one before acting on nodes.
func waitLoaded(sel string) chromedp.Action {
	return chromedp.Tasks{
		// Visible first, so that the poll runs in the new document: it doesn't survive a navigation.
		chromedp.WaitVisible(sel),
		chromedp.Poll(`document.readyState === "complete"`, nil),
	}
}

// testWriter logs through the test it belongs to.
type testWriter struct {
	t *testing.T
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}
