package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"maragu.dev/is"

	"app/model"
)

// stallingFlows never answers until the caller gives up.
type stallingFlows struct{}

func (stallingFlows) StartAuthFlow(ctx context.Context, identifier string) (model.AuthFlow, error) {
	<-ctx.Done()
	return model.AuthFlow{}, ctx.Err()
}

func TestFat_StartLogin_timeout(t *testing.T) {
	t.Run("should give up on a flow starter that never answers, within the login timeout", func(t *testing.T) {
		previous := loginTimeout
		loginTimeout = 100 * time.Millisecond
		t.Cleanup(func() { loginTimeout = previous })

		f := NewFat(NewFatOptions{})
		StartLogin(f, stallingFlows{})

		started := time.Now()
		_, err := f.StartLogin(t.Context(), "alice.test")
		is.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())
		is.True(t, time.Since(started) < 5*time.Second, "took longer than the login timeout")
	})
}
