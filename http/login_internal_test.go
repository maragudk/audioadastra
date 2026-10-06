package http

import (
	"testing"

	"maragu.dev/is"
)

func TestLocalPath(t *testing.T) {
	tests := []struct {
		redirect string
		expected string
	}{
		{redirect: "/", expected: "/"},
		{redirect: "/profile", expected: "/profile"},
		{redirect: "/profile/", expected: "/profile/"},
		{redirect: "/profile?x=1", expected: "/profile?x=1"},
		{redirect: "/a/../profile", expected: "/profile"},
		{redirect: "/a/..//evil.test", expected: "/evil.test"},
		{redirect: "/foo/../\\evil.test", expected: ""},
		{redirect: "/./\\evil.test", expected: ""},
		{redirect: "/\\evil.test", expected: ""},
		{redirect: "/%5Cevil.test", expected: ""},
		{redirect: "/%5cevil.test", expected: ""},
		{redirect: "//evil.test", expected: ""},
		{redirect: "/\t/evil.test", expected: ""},
		{redirect: "/%09/evil.test", expected: ""},
		{redirect: "/\n/evil.test", expected: ""},
		{redirect: "https://evil.test", expected: ""},
		{redirect: "evil.test", expected: ""},
		{redirect: "", expected: ""},
	}

	for _, test := range tests {
		t.Run(test.redirect, func(t *testing.T) {
			is.Equal(t, test.expected, localPath(test.redirect))
		})
	}
}
