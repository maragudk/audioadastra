package model

import "maragu.dev/glue/model"

const (
	ErrorUserInactive = model.ErrorUserInactive
	ErrorUserNotFound = model.ErrorUserNotFound

	// ErrorIdentityUnresolved when a login identifier is not a handle or DID, or does not resolve to an
	// identity with a PDS.
	ErrorIdentityUnresolved = Error("identity unresolved")
	// ErrorAuthServerUnavailable when the user's auth server cannot be reached, rejects the auth request,
	// or fails the token exchange.
	ErrorAuthServerUnavailable = Error("auth server unavailable")
	// ErrorLoginCancelled when the auth server sends the user back without a code, or with a state that
	// matches no pending auth request.
	ErrorLoginCancelled = Error("login cancelled")
	// ErrorScopeDenied when the auth server granted fewer scopes than the app needs.
	ErrorScopeDenied = Error("scope denied")
	// ErrorProfileWriteFailed when the user's profile record could not be read or written on login.
	ErrorProfileWriteFailed = Error("profile write failed")
	// ErrorOAuthSessionNotFound when no OAuth session exists for the DID and session ID.
	ErrorOAuthSessionNotFound = Error("oauth session not found")
	// ErrorOAuthAuthRequestNotFound when no pending auth request exists for the state.
	ErrorOAuthAuthRequestNotFound = Error("oauth auth request not found")
)
