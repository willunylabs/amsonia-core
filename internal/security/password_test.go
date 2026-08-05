package security

import (
	"encoding/base64"
	"fmt"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPasswordHasherRoundTrip(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	password := "synthetic-password-for-tests"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if !hasher.Verify(password, encoded) {
		t.Fatal("correct password did not verify")
	}
	if hasher.Verify("different-synthetic-password", encoded) {
		t.Fatal("wrong password verified")
	}
}

func TestPasswordHasherRejectsEmptySaltWithDerivedKey(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	password := "synthetic-password-for-tests"
	key := argon2.IDKey([]byte(password), []byte{}, 1, 64, 1, 32)
	encoded := fmt.Sprintf("argon2id$v=19$m=64,t=1,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString([]byte{}),
		base64.RawStdEncoding.EncodeToString(key))

	if hasher.Verify(password, encoded) {
		t.Fatal("password verified with an empty salt")
	}
}

func TestPasswordHasherRejectsInvalidEncodedValues(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	invalid := []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "unsupported bcrypt", encoded: "$2a$10$abcdefghijklmnopqrstuuJ4h7m3n2Q8zQ9p5xXv3bW1cD9eS2"},
		{name: "malformed salt base64", encoded: "argon2id$v=19$m=64,t=1,p=1$%%%$AQ"},
		{name: "malformed hash base64", encoded: "argon2id$v=19$m=64,t=1,p=1$AQ$%%%"},
		{name: "empty salt", encoded: "argon2id$v=19$m=64,t=1,p=1$$AQ"},
		{name: "empty hash", encoded: "argon2id$v=19$m=64,t=1,p=1$AQ$"},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if hasher.Verify("synthetic-password-for-tests", tc.encoded) {
				t.Fatalf("invalid encoded value verified: %q", tc.encoded)
			}
		})
	}
}
