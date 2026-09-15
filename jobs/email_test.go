package jobs_test

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"strings"
	"testing"

	"maragu.dev/glue/email"
	"maragu.dev/is"

	"app/jobs"
	"app/model"
)

type senderStub struct {
	err  error
	opts email.SendOptions
}

func (s *senderStub) SendTransactional(ctx context.Context, opts email.SendOptions) error {
	s.opts = opts
	return s.err
}

func TestSendEmail(t *testing.T) {
	t.Run("should error on an unknown email type without sending", func(t *testing.T) {
		sender := &senderStub{}

		err := jobs.SendEmail(newLog(), sender)(t.Context(), marshalJobData(t, model.SendEmailJobData{
			Type:  "nope",
			Email: "me@example.com",
		}))
		is.True(t, err != nil, "expected an error")
		is.True(t, strings.Contains(err.Error(), "nope"), err.Error())
		is.Equal(t, model.EmailAddress(""), sender.opts.To)
	})
}

func newLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func marshalJobData(t *testing.T, jd model.SendEmailJobData) []byte {
	t.Helper()

	m, err := json.Marshal(jd)
	is.NotError(t, err)
	return m
}
