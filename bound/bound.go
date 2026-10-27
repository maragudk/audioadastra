// Package bound cuts text that comes from elsewhere, such as a PDS that can answer with anything, to a
// size fit for span attributes and error messages.
package bound

import (
	"unicode/utf8"
)

// Text of up to 256 bytes as it is, and longer text cut short, at a character boundary when it is
// UTF-8, and marked as cut.
func Text(s string) string {
	const limit = 256
	if len(s) <= limit {
		return s
	}
	end := limit
	for end > limit-utf8.UTFMax && !utf8.RuneStart(s[end]) {
		end--
	}
	if !utf8.RuneStart(s[end]) {
		end = limit
	}
	return s[:end] + "…[truncated]"
}

// Error that wraps the given one, with its text cut as [Text] does.
func Error(err error) error {
	return boundedError{err: err}
}

type boundedError struct {
	err error
}

func (e boundedError) Error() string {
	return Text(e.err.Error())
}

func (e boundedError) Unwrap() error {
	return e.err
}
