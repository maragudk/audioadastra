package jobs

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"maragu.dev/glue/email"
	"maragu.dev/glue/jobs"

	"app/model"
)

type emailSender interface {
	SendTransactional(ctx context.Context, opts email.SendOptions) error
}

// SendEmail job, dispatching on the email type in [model.SendEmailJobData]. No type is defined yet, so
// every job errors until the first one is added to the switch.
func SendEmail(log *slog.Logger, sender emailSender) jobs.Func {
	return jobs.WithTracing("jobs.SendEmail", func(ctx context.Context, m []byte) error {
		var jd model.SendEmailJobData
		if err := json.Unmarshal(m, &jd); err != nil {
			panic(err)
		}

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("email.type", jd.Type))

		log.Info("Sending email", "type", jd.Type, "email", jd.Email)

		switch jd.Type {
		default:
			return fmt.Errorf("unknown email type %v", jd.Type)
		}
	})
}
