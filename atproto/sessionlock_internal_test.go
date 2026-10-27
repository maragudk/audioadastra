package atproto

import (
	"context"
	"testing"
	"testing/synctest"

	"maragu.dev/is"

	"app/model"
)

func TestSessionLocks(t *testing.T) {
	alice := sessionKey{did: "did:plc:alicealicealicealicealic", sessionID: "one"}
	aliceOtherDevice := sessionKey{did: "did:plc:alicealicealicealicealic", sessionID: "two"}

	t.Run("should make a second call as the same session wait for the first to unlock", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var l sessionLocks
			unlock, err := l.lock(t.Context(), alice)
			is.NotError(t, err)

			locked := make(chan func())
			go func() {
				unlock, err := l.lock(t.Context(), alice)
				if err != nil {
					panic(err)
				}
				locked <- unlock
			}()
			synctest.Wait()
			select {
			case <-locked:
				t.Fatal("second call got the lock while the first held it")
			default:
			}

			unlock()
			unlockSecond := <-locked
			unlockSecond()
			is.Equal(t, 0, len(l.locks))
		})
	})

	t.Run("should not make calls as different sessions wait for each other", func(t *testing.T) {
		var l sessionLocks
		unlockOne, err := l.lock(t.Context(), alice)
		is.NotError(t, err)
		unlockTwo, err := l.lock(t.Context(), aliceOtherDevice)
		is.NotError(t, err)

		unlockOne()
		unlockTwo()
		is.Equal(t, 0, len(l.locks))
	})

	t.Run("should stop waiting when the context is done, and leave no lock behind", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var l sessionLocks
			unlock, err := l.lock(t.Context(), alice)
			is.NotError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error)
			go func() {
				_, err := l.lock(ctx, alice)
				done <- err
			}()
			synctest.Wait()
			cancel()
			err = <-done
			is.Error(t, model.ErrorOAuthSessionBusy, err)
			is.Error(t, context.Canceled, err)

			unlock()
			is.Equal(t, 0, len(l.locks))
		})
	})

	t.Run("should have a lock nobody holds at once, even with a context that is already done", func(t *testing.T) {
		var l sessionLocks
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// Repeated, since a select between two ready cases would pick either.
		for range 100 {
			unlock, err := l.lock(ctx, alice)
			is.NotError(t, err)
			unlock()
		}
		is.Equal(t, 0, len(l.locks))
	})
}
