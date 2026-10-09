package model

import "maragu.dev/glue/model"

const (
	ErrorUserInactive = model.ErrorUserInactive
	ErrorUserNotFound = model.ErrorUserNotFound

	// ErrorIdentityUnresolved when a login identifier is not a handle or DID, or does not resolve to an
	// identity with a PDS.
	ErrorIdentityUnresolved = Error("identity unresolved")
	// ErrorIdentityUnavailable when looking up a login identifier failed, such as a DNS or DID directory
	// outage or a timeout, so whether it exists is not known.
	ErrorIdentityUnavailable = Error("identity unavailable")
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

	// ErrorTrackTitleMissing when a track upload has no title, or only whitespace.
	ErrorTrackTitleMissing = Error("track title missing")
	// ErrorTrackTitleTooLong when a track title is over the lexicon's limits.
	ErrorTrackTitleTooLong = Error("track title too long")
	// ErrorTrackDescriptionTooLong when a track description is over the lexicon's limits.
	ErrorTrackDescriptionTooLong = Error("track description too long")
	// ErrorAudioMissing when a track upload has no file, or an empty one.
	ErrorAudioMissing = Error("audio missing")
	// ErrorNotAudio when an uploaded file does not read as audio, or is audio in a format the app does
	// not take.
	ErrorNotAudio = Error("not audio")
	// ErrorTrackTextInvalid when a track's title or description is not valid UTF-8.
	ErrorTrackTextInvalid = Error("track text invalid")
	// ErrorUploadTooLarge when an upload is over the app's own size limit.
	ErrorUploadTooLarge = Error("upload too large")
	// ErrorBlobTooLarge when an upload is over the size limit of the account's PDS, whether the PDS said
	// so up front or refused the blob.
	ErrorBlobTooLarge = Error("blob too large")
	// ErrorBlobTypeRefused when the account's PDS refuses a blob's type under the OAuth scopes granted.
	ErrorBlobTypeRefused = Error("blob type refused")
	// ErrorBlobRejected when the account's PDS refuses a blob for any other reason it gives.
	ErrorBlobRejected = Error("blob rejected")
	// ErrorPDSAuthFailed when the account's PDS refuses the session a call is made as.
	ErrorPDSAuthFailed = Error("pds auth failed")
	// ErrorPDSUnavailable when the account's PDS cannot be reached, fails, or stops responding.
	ErrorPDSUnavailable = Error("pds unavailable")
	// ErrorRecordWriteFailed when the account's PDS does not create a record.
	ErrorRecordWriteFailed = Error("record write failed")
)
