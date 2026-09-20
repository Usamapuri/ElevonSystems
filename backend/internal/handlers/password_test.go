package handlers

import (
	"strings"
	"testing"
)

func TestCheckPassword(t *testing.T) {
	cases := []struct{ pw, want string }{
		{"short7c", "weak_password"},
		{"eightchr", ""},
		{strings.Repeat("x", 72), ""},
		{strings.Repeat("x", 73), "password_too_long"},
	}
	for _, c := range cases {
		if got := checkPassword(c.pw); got != c.want {
			t.Errorf("checkPassword(%d bytes) = %q, want %q", len(c.pw), got, c.want)
		}
	}
	if passwordMessage("weak_password") == "" || passwordMessage("password_too_long") == "" {
		t.Fatal("every code needs a human message")
	}
}
