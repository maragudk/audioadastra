package atproto

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"maragu.dev/is"

	"app/model"
)

func TestServerCache(t *testing.T) {
	t.Run("should keep a description for a day per host", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var c serverCache
			f := &fetcher{description: model.ServerDescription{BlobUploadLimit: 42}}

			description, hit, err := c.get(t.Context(), "https://pds.test", f.fetch)
			is.NotError(t, err)
			is.True(t, !hit, "hit on the first get")
			is.Equal(t, int64(42), description.BlobUploadLimit)

			time.Sleep(24*time.Hour - time.Second)
			description, hit, err = c.get(t.Context(), "https://pds.test", f.fetch)
			is.NotError(t, err)
			is.True(t, hit, "miss within the day")
			is.Equal(t, int64(42), description.BlobUploadLimit)
			is.Equal(t, 1, f.calls)

			_, hit, _ = c.get(t.Context(), "https://other.test", f.fetch)
			is.True(t, !hit, "hit for another host")
			is.Equal(t, 2, f.calls)

			time.Sleep(time.Second)
			_, hit, _ = c.get(t.Context(), "https://pds.test", f.fetch)
			is.True(t, !hit, "hit after the day")
			is.Equal(t, 3, f.calls)
		})
	})

	t.Run("should keep a failure for a minute", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var c serverCache
			f := &fetcher{err: model.ErrorPDSUnavailable}

			_, hit, err := c.get(t.Context(), "https://pds.test", f.fetch)
			is.Error(t, model.ErrorPDSUnavailable, err)
			is.True(t, !hit, "hit on the first get")

			time.Sleep(time.Minute - time.Second)
			_, hit, err = c.get(t.Context(), "https://pds.test", f.fetch)
			is.Error(t, model.ErrorPDSUnavailable, err)
			is.True(t, hit, "miss within the minute")
			is.Equal(t, 1, f.calls)

			time.Sleep(time.Second)
			f.err = nil
			_, hit, err = c.get(t.Context(), "https://pds.test", f.fetch)
			is.NotError(t, err)
			is.True(t, !hit, "hit after the minute")
			is.Equal(t, 2, f.calls)
		})
	})

	t.Run("should not keep a fetch cut short by the caller", func(t *testing.T) {
		var c serverCache
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		f := &fetcher{err: context.Canceled}

		_, _, err := c.get(ctx, "https://pds.test", f.fetch)
		is.Error(t, context.Canceled, err)

		f.err = nil
		_, hit, err := c.get(t.Context(), "https://pds.test", f.fetch)
		is.NotError(t, err)
		is.True(t, !hit, "kept the cut-short fetch")
		is.Equal(t, 2, f.calls)
	})
}

func TestBlobRefusal(t *testing.T) {
	tests := []struct {
		status int
		name   string
		err    error
	}{
		{status: 401, name: "InvalidToken", err: model.ErrorPDSAuthFailed},
		{status: 403, name: "ScopeMissingError", err: model.ErrorBlobTypeRefused},
		{status: 403, name: "Forbidden", err: model.ErrorBlobRejected},
		{status: 400, name: "InvalidRequest", err: model.ErrorBlobRejected},
		{status: 413, name: "PayloadTooLarge", err: model.ErrorBlobTooLarge},
		{status: 429, name: "RateLimitExceeded", err: model.ErrorPDSUnavailable},
		{status: 500, name: "InternalServerError", err: model.ErrorPDSUnavailable},
		{status: 502, name: "", err: model.ErrorPDSUnavailable},
	}
	for _, test := range tests {
		t.Run(fmt.Sprint("should classify ", test.status, " ", test.name, " as ", test.err), func(t *testing.T) {
			is.Equal(t, test.err, blobRefusal(test.status, test.name))
		})
	}
}

func TestRefreshRefused(t *testing.T) {
	tests := []struct {
		err     error
		refused bool
	}{
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed (HTTP 400): invalid_grant"), refused: true},
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed: auth server request failed (HTTP 400): invalid_grant"), refused: true},
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed (HTTP 503): unavailable"), refused: false},
		{err: errors.New("failed to refresh OAuth tokens: token refresh failed: dial tcp: connection refused"), refused: false},
		{err: errors.New("something else (HTTP 400)"), refused: false},
	}
	for _, test := range tests {
		t.Run(fmt.Sprint("should tell whether ", test.err, " is a refusal"), func(t *testing.T) {
			is.Equal(t, test.refused, refreshRefused(test.err))
		})
	}
}

// fetcher of a fixed description or error, counting its calls.
type fetcher struct {
	description model.ServerDescription
	err         error
	calls       int
}

func (f *fetcher) fetch(ctx context.Context, host string) (model.ServerDescription, error) {
	f.calls++
	if f.err != nil {
		return model.ServerDescription{}, f.err
	}
	return f.description, nil
}
