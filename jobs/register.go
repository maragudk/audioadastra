package jobs

import (
	"log/slog"

	"maragu.dev/glue/email/postmark"
	"maragu.dev/glue/jobs"
)

type RegisterOpts struct {
	Log    *slog.Logger
	Sender *postmark.Sender
}

// Register all available jobs with the given dependencies. There are none yet, so this only settles the
// options; the sender waits here for the first job to need it.
func Register(r *jobs.Runner, opts RegisterOpts) {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
}
