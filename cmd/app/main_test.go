package main

import (
	"testing"

	"maragu.dev/is"
)

func TestParseAbsoluteURL(t *testing.T) {
	t.Run("should parse an absolute URL with a host", func(t *testing.T) {
		u, err := parseAbsoluteURL("BASE_URL", "https://app.example.com/")
		is.NotError(t, err)
		is.Equal(t, "https://app.example.com/", u.String())
	})

	tests := []struct {
		name  string
		value string
	}{
		{name: "should refuse a URL without a scheme", value: "app.example.com"},
		{name: "should refuse a URL without a host", value: "https:///path"},
		{name: "should refuse an empty value", value: ""},
		{name: "should refuse a value that does not parse", value: ":nope"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAbsoluteURL("BASE_URL", test.value)
			is.True(t, err != nil, "expected an error")
		})
	}
}
