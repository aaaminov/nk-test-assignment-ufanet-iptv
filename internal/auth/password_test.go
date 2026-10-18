package auth_test

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"nk-test-assignment-ufanet-iptv/internal/auth"
)

func TestPassword(t *testing.T) {
	hash, err := auth.HashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost([]byte(hash)); err != nil || cost != 10 {
		t.Fatalf("bcrypt cost = %d, error = %v; want 10", cost, err)
	}
	for _, tc := range []struct {
		name     string
		hash     string
		password string
		want     bool
	}{
		{"correct password", hash, "test-password", true},
		{"wrong password", hash, "wrong-password", false},
		{"empty password", hash, "", false},
		{"damaged hash", "invalid-hash", "test-password", false},
		{"empty hash", "", "test-password", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := auth.CheckPassword(tc.hash, tc.password); got != tc.want {
				t.Fatalf("CheckPassword = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPasswordByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password string
	}{
		{"ASCII", strings.Repeat("a", 72)},
		{"UTF-8", strings.Repeat("я", 36)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := auth.HashPassword(tc.password)
			if err != nil {
				t.Fatalf("72-byte password rejected: %v", err)
			}
			if !auth.CheckPassword(hash, tc.password) {
				t.Fatal("72-byte password did not match")
			}
			long := tc.password + "a"
			if hash, err := auth.HashPassword(long); err == nil || hash != "" {
				t.Fatal("73-byte password must fail without a hash")
			}
			if auth.CheckPassword(hash, long) {
				t.Fatal("73-byte password must not match its 72-byte prefix")
			}
		})
	}
}
