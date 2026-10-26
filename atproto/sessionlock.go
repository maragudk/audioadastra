package atproto

import (
	"context"
	"fmt"
	"sync"

	"app/model"
)

// sessionLocks hold one lock per OAuth session, so that calls as the same session take turns. A call
// refreshes the session's tokens when they have expired and persists what it got, and two calls that
// overlapped would refresh with the same refresh token, which the auth server may refuse the second
// time, or save older tokens over newer ones. A lock exists only while some call holds or waits for
// it. Safe for concurrent use.
type sessionLocks struct {
	mu    sync.Mutex
	locks map[sessionKey]*sessionLock
}

type sessionKey struct {
	did       model.DID
	sessionID model.OAuthSessionID
}

// sessionLock is a channel of one, which a call holds by sending to it, so that waiting for it can
// stop when the waiter's context is done.
type sessionLock struct {
	held  chan struct{}
	users int
}

// lock the session with the given key, waiting for any other call holding it, and return the function
// that unlocks it. The error is the context's when it is done before the lock is had.
func (l *sessionLocks) lock(ctx context.Context, key sessionKey) (func(), error) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[sessionKey]*sessionLock{}
	}
	sl, ok := l.locks[key]
	if !ok {
		sl = &sessionLock{held: make(chan struct{}, 1)}
		l.locks[key] = sl
	}
	sl.users++
	l.mu.Unlock()

	leave := func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		sl.users--
		if sl.users == 0 {
			delete(l.locks, key)
		}
	}

	select {
	case sl.held <- struct{}{}:
		return func() {
			<-sl.held
			leave()
		}, nil
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
}

// lockSession of the account on one device, waiting for any other call as it to finish, and resume it
// from the store once the lock is had, so that it has whatever the last call persisted. The returned
// function unlocks it, and must be called once the call is done with the session.
//
// Errors are the context's when it is done while waiting, and [model.ErrorOAuthSessionNotFound] when
// there is no such session.
func (c *Client) lockSession(ctx context.Context, did model.DID, sessionID model.OAuthSessionID) (*session, func(), error) {
	unlock, err := c.sessionLocks.lock(ctx, sessionKey{did: did, sessionID: sessionID})
	if err != nil {
		return nil, nil, fmt.Errorf("waiting for another call as the OAuth session: %w", err)
	}
	sess, err := c.resumeSession(ctx, did, sessionID)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	return sess, unlock, nil
}
