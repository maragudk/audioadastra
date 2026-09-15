package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"maragu.dev/is"

	"app/atprototest"
	"app/model"
	"app/sqlitetest"
)

func TestFat_StartLogin_timeout(t *testing.T) {
	t.Run("should give up on an auth server that never answers, within the login timeout", func(t *testing.T) {
		previous := loginTimeout
		loginTimeout = 100 * time.Millisecond
		t.Cleanup(func() { loginTimeout = previous })

		net := atprototest.NewNetwork(t)
		net.AddAccount("did:plc:alice", "alice.test")
		net.Stall = true
		f := NewFat(NewFatOptions{})
		StartLogin(f, net.NewClientApp(t, sqlitetest.NewDatabase(t)))

		started := time.Now()
		_, err := f.StartLogin(t.Context(), "alice.test")
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())
		is.True(t, time.Since(started) < 5*time.Second, "took longer than the login timeout")
	})
}
