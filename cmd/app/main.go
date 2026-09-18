package main

import (
	"context"
	"log/slog"
	"time"

	"maragu.dev/env"
	"maragu.dev/glue/app"

	"app/boot"
	"app/model"
)

func main() {
	app.Start(start)
}

func start(ctx context.Context, log *slog.Logger, eg app.Goer) error {
	return boot.Start(ctx, log, eg, boot.Options{
		Address:              env.GetStringOrDefault("SERVER_ADDRESS", ":8080"),
		AppName:              env.GetStringOrDefault("APP_NAME", "App"),
		BaseURL:              env.GetStringOrDefault("BASE_URL", "http://localhost:8080"),
		CSPAllowUnsafeEval:   env.GetBoolOrDefault("CSP_ALLOW_UNSAFE_EVAL", false),
		CSPAllowUnsafeInline: env.GetBoolOrDefault("CSP_ALLOW_UNSAFE_INLINE", false),
		DatabasePath:         env.GetStringOrDefault("DATABASE_PATH", "app.db"),
		JobQueueTimeout:      env.GetDurationOrDefault("JOB_QUEUE_TIMEOUT", 10*time.Second),
		SecureCookie:         env.GetBoolOrDefault("SECURE_COOKIE", true),

		MarketingEmailAddress:     model.EmailAddress(env.GetStringOrDefault("MARKETING_EMAIL_ADDRESS", "marketing@example.com")),
		MarketingEmailName:        env.GetStringOrDefault("MARKETING_EMAIL_NAME", "Marketing"),
		PostmarkKey:               env.GetStringOrDefault("POSTMARK_KEY", ""),
		ReplyToEmailAddress:       model.EmailAddress(env.GetStringOrDefault("REPLY_TO_EMAIL_ADDRESS", "support@example.com")),
		ReplyToEmailName:          env.GetStringOrDefault("REPLY_TO_EMAIL_NAME", "Support"),
		TransactionalEmailAddress: model.EmailAddress(env.GetStringOrDefault("TRANSACTIONAL_EMAIL_ADDRESS", "transactional@example.com")),
		TransactionalEmailName:    env.GetStringOrDefault("TRANSACTIONAL_EMAIL_NAME", "Transactional"),

		OAuthPrivateKey: env.GetStringOrDefault("OAUTH_PRIVATE_KEY", ""),
		OAuthKeyID:      env.GetStringOrDefault("OAUTH_KEY_ID", ""),

		ATProtoPLCURL:            env.GetStringOrDefault("ATPROTO_PLC_URL", ""),
		ATProtoCAFile:            env.GetStringOrDefault("ATPROTO_CA_FILE", ""),
		ATProtoLocalHandleSuffix: env.GetStringOrDefault("ATPROTO_LOCAL_HANDLE_SUFFIX", ""),
	})
}
