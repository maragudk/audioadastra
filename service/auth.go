package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"app/model"
)

// authFlowStarter starts OAuth flows for accounts.
type authFlowStarter interface {
	StartAuthFlow(ctx context.Context, identifier string) (model.AuthFlow, error)
}

// StartLogin wires [Fat.StartLogin] to the given flow starter.
func StartLogin(f *Fat, flows authFlowStarter) {
	if f.startLogin != nil {
		panic("service: StartLogin already wired")
	}
	if flows == nil {
		panic("service: StartLogin needs an auth flow starter")
	}

	f.startLogin = func(ctx context.Context, identifier string) (start model.LoginStart, err error) {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()

		event := newLoginEvent(ctx)
		defer func() { event.finish(f.log, "Login start failed", err) }()

		flow, err := flows.StartAuthFlow(ctx, identifier)
		// A flow that failed partway carries only what was learned before the failure.
		if flow.DID != "" {
			event.set(attribute.String("atproto.did", flow.DID.String()), attribute.String("atproto.handle", flow.Handle.String()))
		}
		if flow.PDSURL != nil {
			event.set(attribute.String("atproto.pds_host", flow.PDSURL.Host))
		}
		if flow.AuthServerURL != nil {
			event.set(attribute.String("oauth.auth_server", flow.AuthServerURL.Host))
		}
		if err != nil {
			return model.LoginStart{}, err
		}

		return model.LoginStart{RedirectURL: flow.RedirectURL, State: flow.State}, nil
	}
}

// StartLogin for the account with the given identifier, a handle or a DID. It returns the URL to send
// the user to for consent, and the state that [Fat.FinishLogin] must be given with the callback, so a
// callback can only finish the flow that was started for it.
//
// Errors are [model.ErrorIdentityUnresolved] when the identifier is not one or does not resolve to an
// account on a PDS, and [model.ErrorAuthServerUnavailable] when the auth server cannot be discovered
// or refuses the request.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) StartLogin(ctx context.Context, identifier string) (model.LoginStart, error) {
	if f.startLogin == nil {
		panic("service: StartLogin not wired; call service.StartLogin or service.Setup")
	}

	return f.startLogin(ctx, identifier)
}

// userCreator is the store a user is looked up or created in by DID.
type userCreator interface {
	CreateUserIfMissing(ctx context.Context, did model.DID) (model.User, bool, error)
}

// callbackProcessor finishes OAuth flows, checks what they granted, and deletes the sessions they
// establish.
type callbackProcessor interface {
	ProcessCallback(ctx context.Context, callback model.OAuthCallback, state model.OAuthState) (model.OAuthSession, error)
	CheckScopes(granted []string) error
	DeleteSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error
}

// recordGetPutter reads and writes records in an account's repository, as the account on the device
// with the given OAuth session.
type recordGetPutter interface {
	GetRecord(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, rkey model.RecordKey) (map[string]any, bool, error)
	PutRecordIfMissing(ctx context.Context, did model.DID, sessionID model.OAuthSessionID, collection model.NSID, rkey model.RecordKey, record map[string]any) (bool, error)
}

// recordValidator validates records against their lexicon.
type recordValidator interface {
	ValidateRecord(record map[string]any, collection model.NSID) error
}

// FinishLogin wires [Fat.FinishLogin] to the given store, callback processor, record reader and
// writer, and record validator.
func FinishLogin(f *Fat, db userCreator, flows callbackProcessor, records recordGetPutter, validator recordValidator) {
	if f.finishLogin != nil {
		panic("service: FinishLogin already wired")
	}
	if db == nil || flows == nil || records == nil || validator == nil {
		panic("service: FinishLogin needs a store, a callback processor, a record reader and writer and a record validator")
	}

	f.finishLogin = func(ctx context.Context, callback model.OAuthCallback, state model.OAuthState) (user model.User, sessionID model.OAuthSessionID, err error) {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()

		event := newLoginEvent(ctx)
		defer func() { event.finish(f.log, "Login failed", err) }()

		oauthSession, err := flows.ProcessCallback(ctx, callback, state)
		if err != nil {
			return model.User{}, "", err
		}

		// The OAuth session is persisted from here on, so any failure below must take it with it: its ID
		// is never returned, so nothing else would ever delete it. The delete outlives a cancelled or
		// expired context, so it does not depend on anything below having worked.
		defer func() {
			if err == nil {
				return
			}
			if deleteErr := flows.DeleteSession(context.WithoutCancel(ctx), oauthSession.DID, oauthSession.SessionID); deleteErr != nil {
				f.log.ErrorContext(ctx, "Error deleting OAuth session after failed login", "error", deleteErr, "did", oauthSession.DID, "sessionID", oauthSession.SessionID)
			}
		}()

		event.set(
			attribute.String("atproto.did", oauthSession.DID.String()),
			attribute.String("atproto.pds_host", oauthSession.HostURL.Host),
			attribute.String("oauth.auth_server", oauthSession.AuthServerURL.Host),
			attribute.String("oauth.scopes_granted", strings.Join(oauthSession.Scopes, " ")),
		)

		if err := flows.CheckScopes(oauthSession.Scopes); err != nil {
			return model.User{}, "", err
		}

		user, created, err := db.CreateUserIfMissing(ctx, oauthSession.DID)
		if err != nil {
			return model.User{}, "", fmt.Errorf("getting or creating user for %v: %w", oauthSession.DID, err)
		}
		event.set(semconv.EnduserPseudoID(string(user.ID)), attribute.Bool("login.first_login", created))
		if !user.Active {
			return model.User{}, "", model.ErrorUserInactive
		}

		profileCreated, err := ensureProfile(ctx, records, oauthSession.DID, oauthSession.SessionID, validator)
		if err != nil {
			return model.User{}, "", fmt.Errorf("%w: %w", model.ErrorProfileWriteFailed, err)
		}
		event.set(attribute.Bool("login.profile_created", profileCreated))

		return user, oauthSession.SessionID, nil
	}
}

// FinishLogin with the callback the auth server sent, for the flow with the given state, which is the
// State of the [model.LoginStart] that [Fat.StartLogin] returned for the same user: a callback for any
// other flow is refused. It exchanges the code for tokens, checks that every requested scope was
// granted, gets or creates the user for the DID, refuses inactive users, and makes sure the account's
// profile record exists, writing an empty one on first login. The OAuth session ID returned is what
// [Fat.CheckOAuthSession] and [Fat.Logout] take.
//
// Errors are [model.ErrorLoginCancelled] when the callback is for another flow, carries no code, or
// comes from another auth server than the flow was started with, [model.ErrorAuthServerUnavailable]
// when the token exchange fails, [model.ErrorScopeDenied], [model.ErrorUserInactive] and
// [model.ErrorProfileWriteFailed]. No OAuth session is left behind on any error.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) FinishLogin(ctx context.Context, callback model.OAuthCallback, state model.OAuthState) (model.User, model.OAuthSessionID, error) {
	if f.finishLogin == nil {
		panic("service: FinishLogin not wired; call service.FinishLogin or service.Setup")
	}

	return f.finishLogin(ctx, callback, state)
}

// ensureProfile exists in the account's repository, as the account on the device with the given OAuth
// session, writing an empty one if not, and reports whether it wrote one.
func ensureProfile(ctx context.Context, records recordGetPutter, did model.DID, sessionID model.OAuthSessionID, validator recordValidator) (bool, error) {
	if _, exists, err := records.GetRecord(ctx, did, sessionID, model.CollectionActorProfile, model.RecordKeySelf); err != nil {
		return false, err
	} else if exists {
		return false, nil
	}

	record := map[string]any{
		"$type":     model.CollectionActorProfile.String(),
		"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := validator.ValidateRecord(record, model.CollectionActorProfile); err != nil {
		return false, fmt.Errorf("validating profile record: %w", err)
	}
	return records.PutRecordIfMissing(ctx, did, sessionID, model.CollectionActorProfile, model.RecordKeySelf, record)
}

// logouter ends OAuth sessions.
type logouter interface {
	Logout(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error
}

// Logout wires [Fat.Logout] to the given logouter.
func Logout(f *Fat, flows logouter) {
	if f.logout != nil {
		panic("service: Logout already wired")
	}
	if flows == nil {
		panic("service: Logout needs a logouter")
	}

	f.logout = func(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()

		trace.SpanFromContext(ctx).SetAttributes(attribute.String("atproto.did", did.String()))
		return flows.Logout(ctx, did, sessionID)
	}
}

// Logout of the given OAuth session: its tokens are revoked at the auth server when it supports that,
// which is best effort, and the session is deleted. Other sessions of the same account are untouched.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) Logout(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	if f.logout == nil {
		panic("service: Logout not wired; call service.Logout or service.Setup")
	}

	return f.logout(ctx, did, sessionID)
}

// GetPermissions of the user with the given ID: [model.PermissionView], which every user holds.
func (f *Fat) GetPermissions(ctx context.Context, id model.UserID) ([]model.Permission, error) {
	return []model.Permission{model.PermissionView}, nil
}

// sessionChecker checks that OAuth sessions still exist.
type sessionChecker interface {
	CheckSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error
}

// CheckOAuthSession wires [Fat.CheckOAuthSession] to the given session checker.
func CheckOAuthSession(f *Fat, flows sessionChecker) {
	if f.checkOAuthSession != nil {
		panic("service: CheckOAuthSession already wired")
	}
	if flows == nil {
		panic("service: CheckOAuthSession needs a session checker")
	}

	f.checkOAuthSession = flows.CheckSession
}

// CheckOAuthSession that the account's OAuth session on one device still exists.
//
// The error is [model.ErrorOAuthSessionNotFound] when there is no such session.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) CheckOAuthSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) error {
	if f.checkOAuthSession == nil {
		panic("service: CheckOAuthSession not wired; call service.CheckOAuthSession or service.Setup")
	}

	return f.checkOAuthSession(ctx, did, sessionID)
}

// oauthDocumenter has the documents an OAuth client publishes.
type oauthDocumenter interface {
	ClientMetadata() any
	JWKS() any
}

// OAuthClientMetadata wires [Fat.OAuthClientMetadata] to the given documenter.
func OAuthClientMetadata(f *Fat, docs oauthDocumenter) {
	if f.oauthClientMetadata != nil {
		panic("service: OAuthClientMetadata already wired")
	}
	if docs == nil {
		panic("service: OAuthClientMetadata needs an OAuth documenter")
	}

	f.oauthClientMetadata = docs.ClientMetadata
}

// OAuthClientMetadata document the client ID points at, for serving as JSON. Auth servers fetch it,
// so it is public.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) OAuthClientMetadata() any {
	if f.oauthClientMetadata == nil {
		panic("service: OAuthClientMetadata not wired; call service.OAuthClientMetadata or service.Setup")
	}

	return f.oauthClientMetadata()
}

// OAuthJWKS wires [Fat.OAuthJWKS] to the given documenter.
func OAuthJWKS(f *Fat, docs oauthDocumenter) {
	if f.oauthJWKS != nil {
		panic("service: OAuthJWKS already wired")
	}
	if docs == nil {
		panic("service: OAuthJWKS needs an OAuth documenter")
	}

	f.oauthJWKS = docs.JWKS
}

// OAuthJWKS with the public half of the client assertion key, for serving as JSON; an empty key set
// for a client without a key. Auth servers fetch it, so it is public.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) OAuthJWKS() any {
	if f.oauthJWKS == nil {
		panic("service: OAuthJWKS not wired; call service.OAuthJWKS or service.Setup")
	}

	return f.oauthJWKS()
}

// handleResolver resolves handles from DIDs.
type handleResolver interface {
	ResolveHandle(ctx context.Context, did model.DID) (model.Handle, error)
}

// ResolveHandle wires [Fat.ResolveHandle] to the given handle resolver.
func ResolveHandle(f *Fat, identities handleResolver) {
	if f.resolveHandle != nil {
		panic("service: ResolveHandle already wired")
	}
	if identities == nil {
		panic("service: ResolveHandle needs a handle resolver")
	}

	f.resolveHandle = identities.ResolveHandle
}

// ResolveHandle of the given DID, bidirectionally verified, which is [model.HandleInvalid] when the
// account's declared handle does not point back at it.
//
// Panics unless the operation was wired, by [Setup] or by the function of the same name.
func (f *Fat) ResolveHandle(ctx context.Context, did model.DID) (model.Handle, error) {
	if f.resolveHandle == nil {
		panic("service: ResolveHandle not wired; call service.ResolveHandle or service.Setup")
	}

	return f.resolveHandle(ctx, did)
}

// loginEvent gathers the attributes of one login attempt on the span in the context as they become
// known, so the same set lands on the span as a wide event and in the warning logged when the attempt
// fails. A key set again replaces its earlier value, as it does on the span.
type loginEvent struct {
	ctx   context.Context
	span  trace.Span
	attrs []attribute.KeyValue
}

func newLoginEvent(ctx context.Context) *loginEvent {
	return &loginEvent{ctx: ctx, span: trace.SpanFromContext(ctx)}
}

func (e *loginEvent) set(attrs ...attribute.KeyValue) {
	for _, attr := range attrs {
		e.attrs = slices.DeleteFunc(e.attrs, func(existing attribute.KeyValue) bool { return existing.Key == attr.Key })
		e.attrs = append(e.attrs, attr)
	}
	e.span.SetAttributes(attrs...)
}

// finish the attempt: a known refusal is recorded as login.condition and every failure is logged with
// the gathered attributes. Nothing is recorded on success.
func (e *loginEvent) finish(log *slog.Logger, msg string, err error) {
	if err == nil {
		return
	}

	if condition := loginCondition(err); condition != "" {
		e.set(attribute.String("login.condition", condition))
	}

	args := []any{"error", err}
	for _, attr := range e.attrs {
		args = append(args, string(attr.Key), attr.Value.AsInterface())
	}
	log.WarnContext(e.ctx, msg, args...)
}

// loginCondition for the error, as the login.condition attribute value, or the empty string if it is
// not a known refusal.
func loginCondition(err error) string {
	conditions := []struct {
		err       error
		condition string
	}{
		{model.ErrorIdentityUnresolved, "identity_error"},
		{model.ErrorIdentityUnavailable, "identity_unavailable"},
		{model.ErrorAuthServerUnavailable, "auth_server_error"},
		{model.ErrorLoginCancelled, "callback_error"},
		{model.ErrorScopeDenied, "scope_denied"},
		{model.ErrorUserInactive, "user_inactive"},
		{model.ErrorProfileWriteFailed, "profile_write_failed"},
	}
	for _, c := range conditions {
		if errors.Is(err, c.err) {
			return c.condition
		}
	}
	return ""
}
