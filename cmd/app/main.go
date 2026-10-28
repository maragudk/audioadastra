package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"maragu.dev/env"
	"maragu.dev/errors"
	"maragu.dev/glue/app"
	"maragu.dev/glue/email"
	"maragu.dev/glue/email/postmark"
	gluehttp "maragu.dev/glue/http"
	gluejobs "maragu.dev/glue/jobs"
	"maragu.dev/glue/sql"
	"maragu.dev/glue/sqlitestore"

	"app/atproto"
	"app/ffprobe"
	"app/html"
	"app/http"
	"app/jobs"
	"app/lexicons"
	"app/model"
	"app/service"
	"app/sqlite"
)

func main() {
	app.Start(start)
}

func start(ctx context.Context, log *slog.Logger, eg app.Goer) error {
	// Libraries that log through the default logger, such as the atproto SDK, log through the app's
	// handler, in its format.
	slog.SetDefault(log)

	databaseLog := log.With("component", "sql.Database")

	jobTimeout := env.GetDurationOrDefault("JOB_QUEUE_TIMEOUT", 10*time.Second)

	db := sqlite.NewDatabase(sqlite.NewDatabaseOptions{
		H: sql.NewHelper(sql.NewHelperOptions{
			JobQueue: sql.JobQueueOptions{
				Timeout: jobTimeout,
			},
			Log: databaseLog,
			SQLite: sql.SQLiteOptions{
				Path: env.GetStringOrDefault("DATABASE_PATH", "app.db"),
			},
		}),
		Log: databaseLog,
	})
	if err := db.H.Connect(ctx); err != nil {
		return errors.Wrap(err, "error connecting to database")
	}

	if err := db.H.MigrateUp(ctx); err != nil {
		return errors.Wrap(err, "error migrating database")
	}

	runner := gluejobs.NewRunner(gluejobs.NewRunnerOpts{
		Log:   log.With("component", "jobs.Runner"),
		Queue: db.H.JobsQ,
	})

	baseURL := env.GetStringOrDefault("BASE_URL", "http://localhost:8080")
	parsedBaseURL, err := parseAbsoluteURL("BASE_URL", baseURL)
	if err != nil {
		return err
	}

	sender := postmark.NewSender(postmark.NewSenderOptions{
		AppName:                   env.GetStringOrDefault("APP_NAME", "App"),
		BaseURL:                   baseURL,
		Emails:                    email.GetTemplates(),
		Key:                       env.GetStringOrDefault("POSTMARK_KEY", ""),
		Log:                       log.With("component", "email.Sender"),
		MarketingEmailAddress:     model.EmailAddress(env.GetStringOrDefault("MARKETING_EMAIL_ADDRESS", "marketing@example.com")),
		MarketingEmailName:        env.GetStringOrDefault("MARKETING_EMAIL_NAME", "Marketing"),
		ReplyToEmailAddress:       model.EmailAddress(env.GetStringOrDefault("REPLY_TO_EMAIL_ADDRESS", "support@example.com")),
		ReplyToEmailName:          env.GetStringOrDefault("REPLY_TO_EMAIL_NAME", "Support"),
		TransactionalEmailAddress: model.EmailAddress(env.GetStringOrDefault("TRANSACTIONAL_EMAIL_ADDRESS", "transactional@example.com")),
		TransactionalEmailName:    env.GetStringOrDefault("TRANSACTIONAL_EMAIL_NAME", "Transactional"),
	})

	jobs.Register(runner, jobs.RegisterOpts{
		Log:    log.With("component", "jobs"),
		Sender: sender,
	})

	var plcURL *url.URL
	if value := env.GetStringOrDefault("ATPROTO_PLC_URL", ""); value != "" {
		if plcURL, err = parseAbsoluteURL("ATPROTO_PLC_URL", value); err != nil {
			return err
		}
	}

	var termsOfServiceURL *url.URL
	if value := env.GetStringOrDefault("TERMS_OF_SERVICE_URL", ""); value != "" {
		if termsOfServiceURL, err = parseAbsoluteURL("TERMS_OF_SERVICE_URL", value); err != nil {
			return err
		}
	}

	var privacyPolicyURL *url.URL
	if value := env.GetStringOrDefault("PRIVACY_POLICY_URL", ""); value != "" {
		if privacyPolicyURL, err = parseAbsoluteURL("PRIVACY_POLICY_URL", value); err != nil {
			return err
		}
	}

	atprotoClient, err := atproto.NewClient(atproto.NewClientOptions{
		BaseURL:             parsedBaseURL,
		PrivateKeyMultibase: env.GetStringOrDefault("OAUTH_PRIVATE_KEY", ""),
		KeyID:               env.GetStringOrDefault("OAUTH_KEY_ID", ""),
		Store:               db,
		TermsOfServiceURL:   termsOfServiceURL,
		PrivacyPolicyURL:    privacyPolicyURL,
		PLCURL:              plcURL,
		CAFile:              env.GetStringOrDefault("ATPROTO_CA_FILE", ""),
		LocalHandleSuffix:   env.GetStringOrDefault("ATPROTO_LOCAL_HANDLE_SUFFIX", ""),
	})
	if err != nil {
		return errors.Wrap(err, "error configuring atproto client")
	}
	if atprotoClient.Confidential() {
		log.InfoContext(ctx, "Configured confidential OAuth client", "clientID", atprotoClient.ClientID())
	} else {
		log.WarnContext(ctx, "Configured localhost OAuth client; browse the app at the callback's origin", "callbackURL", atprotoClient.CallbackURL())
	}
	if atprotoClient.Local() {
		log.WarnContext(ctx, "Using a local atproto network without SSRF protection", "plcURL", plcURL.String())
	}

	catalog, err := lexicons.NewCatalog()
	if err != nil {
		return errors.Wrap(err, "error loading lexicon catalog")
	}

	prober, err := ffprobe.NewProber()
	if err != nil {
		return errors.Wrap(err, "error finding ffprobe, which comes with ffmpeg")
	}

	// Uploads pass through here on their way to the user's PDS.
	tempDataDir := env.GetStringOrDefault("TEMP_DATA_DIR", "")
	if tempDataDir == "" {
		tempDataDir = os.TempDir()
	}
	uploadsDir, removed, err := prepareUploadsDir(tempDataDir)
	if err != nil {
		return errors.Wrap(err, "error preparing uploads directory")
	}
	if removed > 0 {
		log.InfoContext(ctx, "Removed uploads left over from before the start", "dir", uploadsDir, "count", removed)
	}

	uploadMaxBytes := env.GetIntOrDefault("UPLOAD_MAX_BYTES", 300*1024*1024)
	if uploadMaxBytes <= 0 {
		return fmt.Errorf("UPLOAD_MAX_BYTES must be a positive number of bytes, not %v", uploadMaxBytes)
	}

	svc := service.NewFat()
	service.Setup(svc, db, sender, atprotoClient, catalog, prober)

	store, err := sqlitestore.New(ctx, db.H.DB.DB)
	if err != nil {
		return errors.Wrap(err, "error creating sqlite session store")
	}

	server := gluehttp.NewServer(gluehttp.NewServerOptions{
		Address:  env.GetStringOrDefault("SERVER_ADDRESS", ":8080"),
		BaseURL:  baseURL,
		CSP:      http.CSP(env.GetBoolOrDefault("CSP_ALLOW_UNSAFE_INLINE", false), env.GetBoolOrDefault("CSP_ALLOW_UNSAFE_EVAL", false)),
		HTMLPage: html.GluePage,
		HTTPRouterInjector: http.InjectHTTPRouter(log, svc, http.UploadOptions{
			Dir:     uploadsDir,
			MaxSize: int64(uploadMaxBytes),
		}),
		Log:               log.With("component", "http.Server"),
		PermissionsGetter: svc,
		SecureCookie:      env.GetBoolOrDefault("SECURE_COOKIE", true),
		SessionStore:      store,
		UserActiveChecker: db,
		WriteTimeout:      30 * time.Second,
	})

	eg.Go(func() error {
		return server.Start(ctx)
	})

	eg.Go(func() error {
		runner.Start(ctx)
		return nil
	})

	return nil
}

// prepareUploadsDir as the uploads subdirectory of the temporary data directory, created if missing and
// emptied of whatever an earlier run left there, and return its path and how many entries it removed.
func prepareUploadsDir(tempDataDir string) (string, int, error) {
	dir := filepath.Join(tempDataDir, "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, err
	}
	// The directory is emptied below, so it must not be a link to somewhere else.
	if info, err := os.Lstat(dir); err != nil {
		return "", 0, err
	} else if !info.IsDir() {
		return "", 0, fmt.Errorf("%v is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return "", 0, err
		}
	}
	return dir, len(entries), nil
}

// parseAbsoluteURL from the named environment variable, which must have a scheme and a host.
func parseAbsoluteURL(name, value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("parsing %v: %w", name, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("%v %q is not an absolute URL with a host", name, value)
	}
	return u, nil
}
