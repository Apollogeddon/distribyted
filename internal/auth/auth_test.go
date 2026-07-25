package auth

import "testing"

func TestCredentialsMatch(t *testing.T) {
	cases := []struct {
		name                           string
		gotUser, gotPass, wantU, wantP string
		expected                       bool
	}{
		{"correct pair", "admin", "secret", "admin", "secret", true},
		{"wrong user", "eve", "secret", "admin", "secret", false},
		{"wrong pass", "admin", "wrong", "admin", "secret", false},
		{"both configured empty", "", "", "", "", false},
		{"empty presented vs configured", "", "", "admin", "secret", false},
		{"configured empty, presented non-empty", "admin", "secret", "", "", false},
		{"differing lengths", "admin", "short", "admin", "muchlongersecret", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CredentialsMatch(c.gotUser, c.gotPass, c.wantU, c.wantP); got != c.expected {
				t.Errorf("CredentialsMatch(%q,%q,%q,%q) = %v, want %v", c.gotUser, c.gotPass, c.wantU, c.wantP, got, c.expected)
			}
		})
	}
}
