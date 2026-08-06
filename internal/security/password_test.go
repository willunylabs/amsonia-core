package security

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestPasswordHasherEnforcesPasswordRuneLimit(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	validPasswords := []struct {
		name     string
		password string
	}{
		{name: "ascii 128 code points", password: strings.Repeat("a", 128)},
		{name: "multibyte 128 code points", password: strings.Repeat("界", 128)},
	}
	for _, tc := range validPasswords {
		t.Run(tc.name, func(t *testing.T) {
			if got := utf8.RuneCountInString(tc.password); got != 128 {
				t.Fatalf("test password has %d code points, want 128", got)
			}
			encoded, err := hasher.Hash(tc.password)
			if err != nil {
				t.Fatalf("hash 128-code-point password: %v", err)
			}
			if !hasher.Verify(tc.password, encoded) {
				t.Fatal("128-code-point password did not verify")
			}
		})
	}

	validEncoded, err := hasher.Hash(strings.Repeat("a", 128))
	if err != nil {
		t.Fatalf("hash valid password for rejection cases: %v", err)
	}
	for _, tc := range []struct {
		name     string
		password string
	}{
		{name: "ascii 129 code points", password: strings.Repeat("a", 129)},
		{name: "multibyte 129 code points", password: strings.Repeat("界", 129)},
		{name: "invalid UTF-8", password: string([]byte{0xff, 0xfe})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := hasher.Hash(tc.password); err == nil {
				t.Fatal("invalid or overlong password was hashed")
			}
			if hasher.Verify(tc.password, validEncoded) {
				t.Fatal("invalid or overlong password verified")
			}
		})
	}
}

func TestPasswordHasherRejectsWrongKeyLengths(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	for _, tc := range []struct {
		name   string
		keyLen uint32
	}{
		{name: "one-byte", keyLen: 1},
		{name: "oversized", keyLen: 33},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded := testEncodedPasswordHash("19", "m=64,t=1,p=1", "synthetic-password-for-tests", 64, 1, 1, tc.keyLen)
			if hasher.Verify("synthetic-password-for-tests", encoded) {
				t.Fatalf("key length %d was accepted", tc.keyLen)
			}
		})
	}
}

func TestPasswordHasherRejectsWrongEncodedKeyStringLengths(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	baseParts := strings.Split(testEncodedPasswordHash("19", "m=64,t=1,p=1", "synthetic-password-for-tests", 64, 1, 1, 32), "$")
	expectedLength := base64.RawStdEncoding.EncodedLen(32)
	for _, tc := range []struct {
		name string
		key  string
	}{
		{name: "truncated", key: baseParts[4][:expectedLength-1]},
		{name: "oversized", key: baseParts[4] + "A"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := append([]string(nil), baseParts...)
			parts[4] = tc.key
			if hasher.Verify("synthetic-password-for-tests", strings.Join(parts, "$")) {
				t.Fatalf("encoded key string length %d was accepted", len(tc.key))
			}
		})
	}
}

func TestPasswordHasherRejectsOversizedEncodedInput(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	parameters := "m=" + strings.Repeat("9", maxEncodedPasswordHashLength) + ",t=1,p=1"
	encoded := testEncodedPasswordHash("19", parameters, "synthetic-password-for-tests", 64, 1, 1, 32)
	if len(encoded) <= maxEncodedPasswordHashLength {
		t.Fatalf("test input length = %d, want greater than %d", len(encoded), maxEncodedPasswordHashLength)
	}
	if hasher.Verify("synthetic-password-for-tests", encoded) {
		t.Fatal("oversized encoded input was accepted")
	}
}

func TestPasswordHasherRejectsMismatchedParameters(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	for _, tc := range []struct {
		name       string
		parameters string
	}{
		{name: "memory", parameters: "m=65,t=1,p=1"},
		{name: "iterations", parameters: "m=64,t=2,p=1"},
		{name: "parallelism", parameters: "m=64,t=1,p=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded := testEncodedPasswordHash("19", tc.parameters, "synthetic-password-for-tests", 64, 1, 1, 32)
			if hasher.Verify("synthetic-password-for-tests", encoded) {
				t.Fatalf("mismatched %s parameter was accepted", tc.name)
			}
		})
	}
}

func TestPasswordHasherRejectsMalformedParameters(t *testing.T) {
	hasher := NewPasswordHasher(64, 1, 1, 32)
	for _, tc := range []struct {
		name       string
		version    string
		parameters string
	}{
		{name: "wrong version", version: "18", parameters: "m=64,t=1,p=1"},
		{name: "unknown parameter", version: "19", parameters: "m=64,x=1,p=1"},
		{name: "duplicate parameter", version: "19", parameters: "m=64,m=64,p=1"},
		{name: "missing parameter", version: "19", parameters: "m=64,t=1"},
		{name: "malformed parameter", version: "19", parameters: "m=64,t=1,p"},
		{name: "non-decimal parameter", version: "19", parameters: "m=0x40,t=1,p=1"},
		{name: "signed parameter", version: "19", parameters: "m=+64,t=1,p=1"},
		{name: "zero memory", version: "19", parameters: "m=0,t=1,p=1"},
		{name: "zero iterations", version: "19", parameters: "m=64,t=0,p=1"},
		{name: "zero parallelism", version: "19", parameters: "m=64,t=1,p=0"},
		{name: "memory overflow", version: "19", parameters: "m=4294967296,t=1,p=1"},
		{name: "iterations overflow", version: "19", parameters: "m=64,t=4294967296,p=1"},
		{name: "parallelism overflow", version: "19", parameters: "m=64,t=1,p=256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded := testEncodedPasswordHash(tc.version, tc.parameters, "synthetic-password-for-tests", 64, 1, 1, 32)
			if hasher.Verify("synthetic-password-for-tests", encoded) {
				t.Fatalf("malformed parameters were accepted: %s", tc.parameters)
			}
		})
	}
}

func TestPasswordHasherVerifyRejectsInvalidConfiguration(t *testing.T) {
	encoded := testEncodedPasswordHash("19", "m=64,t=1,p=1", "synthetic-password-for-tests", 64, 1, 1, 32)
	for _, tc := range []struct {
		name   string
		hasher *PasswordHasher
	}{
		{name: "nil receiver", hasher: nil},
		{name: "zero memory", hasher: NewPasswordHasher(0, 1, 1, 32)},
		{name: "low memory", hasher: NewPasswordHasher(7, 1, 1, 32)},
		{name: "zero iterations", hasher: NewPasswordHasher(64, 0, 1, 32)},
		{name: "zero parallelism", hasher: NewPasswordHasher(64, 1, 0, 32)},
		{name: "zero key length", hasher: NewPasswordHasher(64, 1, 1, 0)},
		{name: "low key length", hasher: NewPasswordHasher(64, 1, 1, 15)},
		{name: "high memory", hasher: NewPasswordHasher(maxArgon2Memory+1, 1, 1, 32)},
		{name: "high iterations", hasher: NewPasswordHasher(64, 33, 1, 32)},
		{name: "high parallelism", hasher: NewPasswordHasher(64, 1, 17, 32)},
		{name: "high key length", hasher: NewPasswordHasher(64, 1, 1, 65)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertPasswordHasherHashRejects(t, tc.hasher)
			if tc.hasher.Verify("synthetic-password-for-tests", encoded) {
				t.Fatal("invalid hasher configuration was accepted")
			}
		})
	}
}

func assertPasswordHasherHashRejects(t *testing.T, hasher *PasswordHasher) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("Hash panicked for invalid configuration: %v", recovered)
		}
	}()
	if _, err := hasher.Hash("synthetic-password-for-tests"); err == nil {
		t.Fatal("Hash accepted invalid configuration")
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

func testEncodedPasswordHash(version, parameters, password string, memory, iterations uint32, parallelism uint8, keyLen uint32) string {
	salt := []byte("synthetic-salt16")
	key := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, keyLen)
	return fmt.Sprintf("argon2id$v=%s$%s$%s$%s", version, parameters,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}
