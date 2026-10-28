package atproto

import (
	"fmt"
	"io"
	"net/http"
)

// limitBodies of the responses a copy of the HTTP client receives to the given number of bytes, so
// that a server sending an endless body cannot make a caller read it into memory. Reading past the
// limit is an error rather than an early end, which could leave a truncated body that still parses.
// The client's transport is wrapped, not replaced, so whatever it does, such as refusing to dial
// private addresses, it still does.
func limitBodies(c *http.Client, limit int64) *http.Client {
	limited := *c
	next := c.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	limited.Transport = &bodyLimitTransport{next: next, limit: limit}
	return &limited
}

// bodyLimitTransport limits the body of every response from the transport it wraps.
type bodyLimitTransport struct {
	next  http.RoundTripper
	limit int64
}

func (t *bodyLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	res.Body = &limitedBody{body: res.Body, limit: t.limit, remaining: t.limit}
	return res, nil
}

// limitedBody reads up to limit bytes of a body, and fails a read that finds more.
type limitedBody struct {
	body      io.ReadCloser
	limit     int64
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var one [1]byte
		n, err := b.body.Read(one[:])
		if n > 0 {
			return 0, fmt.Errorf("response body is over %v bytes", b.limit)
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error {
	return b.body.Close()
}
