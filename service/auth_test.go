package service_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"maragu.dev/glue/oteltest"
	"maragu.dev/is"

	"app/atproto"
	"app/lexicons"
	"app/model"
	"app/service"
	"app/servicetest"
	"app/sqlite"
	"app/sqlitetest"
)

// aliceDID is a well-formed example DID: 24 characters of base32 after the method.
const aliceDID = "did:plc:alicealicealicealicealic"

func TestFat_StartLogin(t *testing.T) {
	t.Run("should return the redirect URL and state, recording the account on the span", func(t *testing.T) {
		h := newHarness(t)
		h.flows.startFlow = model.AuthFlow{RedirectURL: "https://auth.test/oauth/authorize?x", State: "s1", DID: aliceDID, Handle: "alice.test", PDSHost: "pds.test", AuthServerHost: "auth.test"}

		ctx, span := h.startSpan(t)
		start, err := h.fat.StartLogin(ctx, "alice.test")
		span.End()
		is.NotError(t, err)
		is.Equal(t, "https://auth.test/oauth/authorize?x", start.RedirectURL)
		is.Equal(t, "s1", start.State)
		is.Equal(t, "alice.test", h.flows.startedWith)

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.did", aliceDID)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.handle", "alice.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.pds_host", "pds.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.auth_server", "auth.test")))
		is.True(t, !oteltest.HasAttributeKey(attrs, "login.condition"))
	})

	t.Run("should pass an identity refusal on with its condition", func(t *testing.T) {
		h := newHarness(t)
		h.flows.startErr = fmt.Errorf("%w: nope", model.ErrorIdentityUnresolved)

		ctx, span := h.startSpan(t)
		_, err := h.fat.StartLogin(ctx, "nobody.test")
		span.End()
		is.Error(t, model.ErrorIdentityUnresolved, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "identity_error")))
	})

	t.Run("should keep what was learned before an auth server refusal", func(t *testing.T) {
		h := newHarness(t)
		h.flows.startFlow = model.AuthFlow{DID: aliceDID, Handle: "alice.test", PDSHost: "pds.test"}
		h.flows.startErr = fmt.Errorf("%w: down", model.ErrorAuthServerUnavailable)

		ctx, span := h.startSpan(t)
		_, err := h.fat.StartLogin(ctx, "alice.test")
		span.End()
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("login.condition", "auth_server_error")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.pds_host", "pds.test")))
	})

	t.Run("should panic naming the wiring function when called unwired", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: StartLogin not wired; call service.StartLogin or service.Setup", fmt.Sprint(recover()))
		}()

		_, _ = servicetest.NewFat(t).StartLogin(t.Context(), "alice.test")
	})

	t.Run("should panic when wired without a flow starter", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: StartLogin needs an auth flow starter", fmt.Sprint(recover()))
		}()

		service.StartLogin(servicetest.NewFat(t), nil)
	})
}

func TestFat_FinishLogin(t *testing.T) {
	t.Run("should create the user, write the profile and return the session on first login", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		user, sessionID, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.NotError(t, err)
		is.Equal(t, model.DID(aliceDID), user.DID)
		is.True(t, user.Active)
		is.Equal(t, "s1", sessionID)

		record, ok := h.session.records["self"]
		is.True(t, ok, "no profile record")
		is.Equal(t, model.CollectionActorProfile, record["$type"])
		is.True(t, record["createdAt"] != nil, "no createdAt")
		is.Equal(t, 0, h.session.deletes)

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("enduser.pseudo.id", string(user.ID))))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.did", aliceDID)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("atproto.pds_host", "pds.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.auth_server", "auth.test")))
		is.True(t, oteltest.HasAttribute(attrs, attribute.String("oauth.scopes_granted", strings.Join(h.flows.scopes, " "))))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.first_login", true)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.profile_created", true)))
		is.True(t, !oteltest.HasAttributeKey(attrs, "login.condition"))
	})

	t.Run("should keep the user and profile on second login", func(t *testing.T) {
		h := newHarness(t)
		first, _, err := h.fat.FinishLogin(t.Context(), h.callback(), "s1")
		is.NotError(t, err)

		ctx, span := h.startSpan(t)
		second, _, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.NotError(t, err)
		is.Equal(t, first.ID, second.ID)
		is.Equal(t, 1, h.session.puts)

		attrs := h.requestSpanAttributes(t)
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.first_login", false)))
		is.True(t, oteltest.HasAttribute(attrs, attribute.Bool("login.profile_created", false)))
	})

	t.Run("should not count a profile written concurrently as created", func(t *testing.T) {
		h := newHarness(t)
		h.session.putRaces = true

		ctx, span := h.startSpan(t)
		_, _, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.NotError(t, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.Bool("login.profile_created", false)))
	})

	t.Run("should refuse when a required scope was not granted, leaving no session or user", func(t *testing.T) {
		h := newHarness(t)
		h.flows.granted = []string{"atproto", "blob:audio/*"}

		ctx, span := h.startSpan(t)
		_, _, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.Error(t, model.ErrorScopeDenied, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "scope_denied")))
		is.Equal(t, 1, h.session.deletes)
		is.Equal(t, 0, h.count(t, "users"))
	})

	t.Run("should refuse when the granted scopes lack atproto", func(t *testing.T) {
		h := newHarness(t)
		h.flows.granted = h.flows.scopes[1:]

		_, _, err := h.fat.FinishLogin(t.Context(), h.callback(), "s1")
		is.Error(t, model.ErrorScopeDenied, err)
		is.Equal(t, 1, h.session.deletes)
	})

	t.Run("should refuse an inactive user, leaving no session", func(t *testing.T) {
		h := newHarness(t)
		user, _, err := h.db.CreateUserIfMissing(t.Context(), aliceDID)
		is.NotError(t, err)
		is.NotError(t, h.db.H.Exec(t.Context(), `update users set active = 0 where id = ?`, user.ID))

		ctx, span := h.startSpan(t)
		_, _, err = h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.Error(t, model.ErrorUserInactive, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "user_inactive")))
		is.Equal(t, 1, h.session.deletes)
		is.Equal(t, 0, h.session.puts)
	})

	t.Run("should refuse when the profile cannot be written, leaving no session", func(t *testing.T) {
		h := newHarness(t)
		h.session.putErr = errors.New("the PDS is down")

		ctx, span := h.startSpan(t)
		_, _, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.Error(t, model.ErrorProfileWriteFailed, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "profile_write_failed")))
		is.Equal(t, 1, h.session.deletes)
	})

	t.Run("should refuse when the profile cannot be read, leaving no session", func(t *testing.T) {
		h := newHarness(t)
		h.session.getErr = errors.New("the PDS is down")

		_, _, err := h.fat.FinishLogin(t.Context(), h.callback(), "s1")
		is.Error(t, model.ErrorProfileWriteFailed, err)
		is.Equal(t, 1, h.session.deletes)
		is.Equal(t, 0, h.session.puts)
	})

	t.Run("should pass a cancelled callback on with its condition", func(t *testing.T) {
		h := newHarness(t)
		h.flows.callbackErr = fmt.Errorf("%w: denied", model.ErrorLoginCancelled)

		ctx, span := h.startSpan(t)
		_, _, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.Error(t, model.ErrorLoginCancelled, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "callback_error")))
		is.Equal(t, 0, h.count(t, "users"))
	})

	t.Run("should pass a failed token exchange on with its condition", func(t *testing.T) {
		h := newHarness(t)
		h.flows.callbackErr = fmt.Errorf("%w: rejected", model.ErrorAuthServerUnavailable)

		ctx, span := h.startSpan(t)
		_, _, err := h.fat.FinishLogin(ctx, h.callback(), "s1")
		span.End()
		is.Error(t, model.ErrorAuthServerUnavailable, err)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("login.condition", "auth_server_error")))
	})

	t.Run("should panic naming the wiring function when called unwired", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: FinishLogin not wired; call service.FinishLogin or service.Setup", fmt.Sprint(recover()))
		}()

		_, _, _ = servicetest.NewFat(t).FinishLogin(t.Context(), url.Values{}, "")
	})
}

func TestFat_Logout(t *testing.T) {
	t.Run("should log out of the session, recording the DID on the span", func(t *testing.T) {
		h := newHarness(t)

		ctx, span := h.startSpan(t)
		is.NotError(t, h.fat.Logout(ctx, aliceDID, "s1"))
		span.End()
		is.Equal(t, aliceDID+"/s1", h.flows.loggedOut)
		is.True(t, oteltest.HasAttribute(h.requestSpanAttributes(t), attribute.String("atproto.did", aliceDID)))
	})

	t.Run("should pass not found on", func(t *testing.T) {
		h := newHarness(t)
		h.flows.logoutErr = model.ErrorOAuthSessionNotFound

		err := h.fat.Logout(t.Context(), aliceDID, "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestFat_PDSSession(t *testing.T) {
	t.Run("should resume the session", func(t *testing.T) {
		h := newHarness(t)

		sess, err := h.fat.PDSSession(t.Context(), aliceDID, "s1")
		is.NotError(t, err)
		is.Equal(t, model.DID(aliceDID), sess.DID())
	})

	t.Run("should pass not found on", func(t *testing.T) {
		h := newHarness(t)
		h.flows.resumeErr = model.ErrorOAuthSessionNotFound

		_, err := h.fat.PDSSession(t.Context(), aliceDID, "nope")
		is.Error(t, model.ErrorOAuthSessionNotFound, err)
	})
}

func TestFat_ResolveHandle(t *testing.T) {
	t.Run("should resolve the handle", func(t *testing.T) {
		h := newHarness(t)
		h.flows.handle = "alice.test"

		handle, err := h.fat.ResolveHandle(t.Context(), aliceDID)
		is.NotError(t, err)
		is.Equal(t, model.Handle("alice.test"), handle)
	})

	t.Run("should panic when wired without a resolver", func(t *testing.T) {
		defer func() {
			is.Equal(t, "service: ResolveHandle needs a handle resolver", fmt.Sprint(recover()))
		}()

		service.ResolveHandle(servicetest.NewFat(t), nil)
	})
}

// harness wires a Fat to a database and a stub of the atproto client, with a span recorder in place
// before the Fat is made so its tracer records into it.
type harness struct {
	db      *sqlite.Database
	flows   *flowsStub
	session *sessionStub
	fat     *service.Fat
	sr      *tracetest.SpanRecorder
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		sr:      oteltest.NewSpanRecorder(t),
		db:      sqlitetest.NewDatabase(t),
		session: &sessionStub{did: aliceDID, records: map[string]map[string]any{}},
	}
	h.flows = &flowsStub{
		scopes:  []string{"atproto", "repo:" + model.CollectionActorProfile, "blob:audio/*", "blob:image/*"},
		session: h.session,
	}
	h.flows.granted = h.flows.scopes

	catalog, err := lexicons.NewCatalog()
	is.NotError(t, err)

	h.fat = servicetest.NewFat(t)
	service.StartLogin(h.fat, h.flows)
	service.FinishLogin(h.fat, h.db, h.flows, catalog)
	service.Logout(h.fat, h.flows)
	service.PDSSession(h.fat, h.flows)
	service.ResolveHandle(h.fat, h.flows)
	return h
}

// callback query for the flow with state s1, as the auth server would send it.
func (h *harness) callback() url.Values {
	return url.Values{"state": {"s1"}, "code": {"c"}, "iss": {"https://auth.test"}}
}

// startSpan standing in for the request span the operations write their attributes to.
func (h *harness) startSpan(t *testing.T) (context.Context, trace.Span) {
	t.Helper()

	return otel.Tracer("test").Start(t.Context(), "request")
}

func (h *harness) requestSpanAttributes(t *testing.T) []attribute.KeyValue {
	t.Helper()

	for _, span := range h.sr.Ended() {
		if span.Name() == "request" {
			return span.Attributes()
		}
	}
	t.Fatal("no request span ended")
	return nil
}

func (h *harness) count(t *testing.T, table string) int {
	t.Helper()

	var count int
	is.NotError(t, h.db.H.Get(t.Context(), &count, `select count(*) from `+table))
	return count
}

// flowsStub stands in for the atproto client: what each operation returns is set up front, and what
// it was called with is kept.
type flowsStub struct {
	scopes  []string
	granted []string
	session *sessionStub

	startFlow   model.AuthFlow
	startErr    error
	startedWith string

	callbackErr error

	resumeErr error

	loggedOut string
	logoutErr error

	handle model.Handle
}

func (s *flowsStub) StartAuthFlow(ctx context.Context, identifier string) (model.AuthFlow, error) {
	s.startedWith = identifier
	return s.startFlow, s.startErr
}

func (s *flowsStub) ProcessCallback(ctx context.Context, params url.Values, state string) (model.OAuthSession, error) {
	if s.callbackErr != nil {
		return model.OAuthSession{}, s.callbackErr
	}
	if params.Get("state") != state {
		return model.OAuthSession{}, fmt.Errorf("%w: state", model.ErrorLoginCancelled)
	}
	return model.OAuthSession{DID: aliceDID, SessionID: state, HostURL: "https://pds.test", AuthServerURL: "https://auth.test", Scopes: s.granted}, nil
}

// CheckScopes as the client would, by string: the stub grants either everything or a strict subset.
func (s *flowsStub) CheckScopes(granted []string) error {
	for _, scope := range s.scopes {
		if !slices.Contains(granted, scope) {
			return fmt.Errorf("%w: %v not granted", model.ErrorScopeDenied, scope)
		}
	}
	return nil
}

func (s *flowsStub) ResumeSession(ctx context.Context, did model.DID, sessionID string) (atproto.Session, error) {
	if s.resumeErr != nil {
		return nil, s.resumeErr
	}
	return s.session, nil
}

func (s *flowsStub) Logout(ctx context.Context, did model.DID, sessionID string) error {
	s.loggedOut = did.String() + "/" + sessionID
	return s.logoutErr
}

func (s *flowsStub) ResolveHandle(ctx context.Context, did model.DID) (model.Handle, error) {
	return s.handle, nil
}

// sessionStub stands in for a resumed session: a repository of one collection, keyed by record key.
type sessionStub struct {
	did      model.DID
	records  map[string]map[string]any
	getErr   error
	putErr   error
	putRaces bool
	puts     int
	deletes  int
}

func (s *sessionStub) DID() model.DID {
	return s.did
}

func (s *sessionStub) GetRecord(ctx context.Context, collection, rkey string) (map[string]any, bool, error) {
	if s.getErr != nil {
		return nil, false, s.getErr
	}
	record, ok := s.records[rkey]
	return record, ok, nil
}

func (s *sessionStub) PutRecordIfMissing(ctx context.Context, collection, rkey string, record map[string]any) (bool, error) {
	s.puts++
	if s.putErr != nil {
		return false, s.putErr
	}
	if s.putRaces {
		s.records[rkey] = map[string]any{"$type": collection}
	}
	if _, exists := s.records[rkey]; exists {
		return false, nil
	}
	s.records[rkey] = record
	return true, nil
}

func (s *sessionStub) Revoke(ctx context.Context) error {
	return nil
}

func (s *sessionStub) Delete(ctx context.Context) error {
	s.deletes++
	return nil
}
