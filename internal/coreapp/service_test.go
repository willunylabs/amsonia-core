package coreapp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestValidBootstrapPassword(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		password string
		want     bool
	}{
		{name: "minimum", password: strings.Repeat("a", 12), want: true},
		{name: "unicode minimum", password: strings.Repeat("界", 12), want: true},
		{name: "maximum", password: strings.Repeat("a", 128), want: true},
		{name: "too short", password: strings.Repeat("a", 11)},
		{name: "too long", password: strings.Repeat("a", 129)},
		{name: "invalid UTF-8", password: string([]byte{0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validBootstrapPassword(test.password); got != test.want {
				t.Fatalf("validBootstrapPassword() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestBoundedPreservesUTF8(t *testing.T) {
	t.Parallel()
	got := bounded("ab界cd", 4)
	if got != "ab" {
		t.Fatalf("bounded() = %q, want %q", got, "ab")
	}
	if !utf8.ValidString(got) {
		t.Fatal("bounded returned invalid UTF-8")
	}
	if got := bounded(string([]byte{'a', 0xff, 'b'}), 8); got != "ab" {
		t.Fatalf("bounded invalid UTF-8 = %q, want %q", got, "ab")
	}
}
