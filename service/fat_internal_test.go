package service

import (
	"log/slog"
	"reflect"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"maragu.dev/is"

	"app/sqlite"
)

func TestNewFat(t *testing.T) {
	t.Run("discards log output when given no logger", func(t *testing.T) {
		f := NewFat(NewFatOptions{})

		is.True(t, f.log.Handler() == slog.DiscardHandler)
	})

	t.Run("stocks the tracer the package traces with", func(t *testing.T) {
		f := NewFat(NewFatOptions{})

		is.True(t, f.tracer != nil)
	})
}

func TestSetup(t *testing.T) {
	t.Run("wires every operation", func(t *testing.T) {
		// From inside the package and over the fields themselves, because a delegate only knows whether
		// its own field was set, and a hand-written list only covers the operations someone remembered:
		// one that gets a wiring function but never a line in Setup would go missing in production and
		// nowhere else. The capabilities are empty values, since nothing calls them, but present, since the
		// wiring functions refuse a missing one.
		f := NewFat(NewFatOptions{})
		Setup(f, &sqlite.Database{}, nil, &oauth.ClientApp{}, identity.NewMockDirectory(), lexicon.NewBaseCatalog())

		fields := reflect.ValueOf(f).Elem()
		var operations int
		for i := range fields.NumField() {
			if fields.Field(i).Kind() != reflect.Func {
				continue
			}
			operations++
			is.True(t, !fields.Field(i).IsNil(), "operation "+fields.Type().Field(i).Name+" is not wired")
		}
		is.True(t, operations > 0, "expected at least one operation")
	})
}
