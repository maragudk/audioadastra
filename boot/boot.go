// Package boot wires the app together and starts it: the database and its migrations, the job runner,
// the atproto clients, the service, and the HTTP server.
package boot

import (
	"context"
	"log/slog"
	"time"

	"maragu.dev/errors"
	"maragu.dev/glue/email"
	"maragu.dev/glue/email/postmark"
	gluehttp "maragu.dev/glue/http"
	gluejobs "maragu.dev/glue/jobs"
	"maragu.dev/glue/sql"
	"maragu.dev/glue/sqlitestore"

	"app/atproto"
	"app/html"
	"app/http"
	"app/jobs"
	"app/lexicons"
	"app/model"
	"app/service"
	"app/sqlite"
)

// Goer runs a function in the background and reports its error, as an errgroup does.
type Goer interface {
	Go(f func() error)
}

// Options for [Start], one per configuration value.
type Options struct {
	Address              string
	AppName              string
	BaseURL              string
	CSPAllowUnsafeEval   bool
	CSPAllowUnsafeInline bool
	DatabasePath         string
	JobQueueTimeout      time.Duration
	SecureCookie         bool

	MarketingEmailAddress     model.EmailAddress
	MarketingEmailName        string
	PostmarkKey               string
	ReplyToEmailAddress       model.EmailAddress
	ReplyToEmailName          string
	TransactionalEmailAddress model.EmailAddress
	TransactionalEmailName    string

	OAuthPrivateKey string
	OAuthKeyID      string

	ATProtoPLCURL            string
	ATProtoCAFile            string
	ATProtoLocalHandleSuffix string
}

// Start the app with the given options, running the server and the job runner on the given Goer
// until the context is done.
func Start(ctx context.Context, log *slog.Logger, eg Goer, opts Options) error {
	databaseLog := log.With("component", "sql.Database")

	db := sqlite.NewDatabase(sqlite.NewDatabaseOptions{
		H: sql.NewHelper(sql.NewHelperOptions{
			JobQueue: sql.JobQueueOptions{
				Timeout: opts.JobQueueTimeout,
			},
			Log: databaseLog,
			SQLite: sql.SQLiteOptions{
				Path: opts.DatabasePath,
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

	sender := postmark.NewSender(postmark.NewSenderOptions{
		AppName:                   opts.AppName,
		BaseURL:                   opts.BaseURL,
		Emails:                    email.GetTemplates(),
		Key:                       opts.PostmarkKey,
		Log:                       log.With("component", "email.Sender"),
		MarketingEmailAddress:     opts.MarketingEmailAddress,
		MarketingEmailName:        opts.MarketingEmailName,
		ReplyToEmailAddress:       opts.ReplyToEmailAddress,
		ReplyToEmailName:          opts.ReplyToEmailName,
		TransactionalEmailAddress: opts.TransactionalEmailAddress,
		TransactionalEmailName:    opts.TransactionalEmailName,
	})

	jobs.Register(runner, jobs.RegisterOpts{
		Log:    log.With("component", "jobs"),
		Sender: sender,
	})

	atprotoClient, err := atproto.New(atproto.NewOptions{
		BaseURL:             opts.BaseURL,
		PrivateKeyMultibase: opts.OAuthPrivateKey,
		KeyID:               opts.OAuthKeyID,
		Store:               db,
		PLCURL:              opts.ATProtoPLCURL,
		CAFile:              opts.ATProtoCAFile,
		LocalHandleSuffix:   opts.ATProtoLocalHandleSuffix,
	})
	if err != nil {
		return errors.Wrap(err, "error configuring atproto clients")
	}
	oauthConfig := atprotoClient.OAuth.Config
	if oauthConfig.IsConfidential() {
		log.InfoContext(ctx, "Configured confidential OAuth client", "clientID", oauthConfig.ClientID)
	} else {
		log.WarnContext(ctx, "Configured localhost OAuth client; browse the app at the callback's origin", "callbackURL", oauthConfig.CallbackURL)
	}
	if atprotoClient.Local {
		log.WarnContext(ctx, "Using a local atproto network without SSRF protection", "plcURL", opts.ATProtoPLCURL)
	}

	catalog, err := lexicons.NewCatalog()
	if err != nil {
		return errors.Wrap(err, "error loading lexicon catalog")
	}

	svc := service.NewFat(service.NewFatOptions{
		Log: log.With("component", "service.Fat"),
	})
	service.Setup(svc, db, sender, atprotoClient.OAuth, atprotoClient.Directory, catalog)

	store, err := sqlitestore.New(ctx, db.H.DB.DB)
	if err != nil {
		return errors.Wrap(err, "error creating sqlite session store")
	}

	server := gluehttp.NewServer(gluehttp.NewServerOptions{
		Address:            opts.Address,
		BaseURL:            opts.BaseURL,
		CSP:                http.CSP(opts.CSPAllowUnsafeInline, opts.CSPAllowUnsafeEval),
		HTMLPage:           html.Page,
		HTTPRouterInjector: http.InjectHTTPRouter(log, svc, oauthConfig, opts.BaseURL),
		Log:                log.With("component", "http.Server"),
		SecureCookie:       opts.SecureCookie,
		SessionStore:       store,
		UserActiveChecker:  db,
		// Login handlers make several outbound calls in a row; see the service's login timeout.
		WriteTimeout: 30 * time.Second,
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
