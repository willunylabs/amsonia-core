package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordHasher struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	keyLen      uint32
}

func NewPasswordHasher(memory uint32, iterations uint32, parallelism uint8, keyLen uint32) *PasswordHasher {
	return &PasswordHasher{memory: memory, iterations: iterations, parallelism: parallelism, keyLen: keyLen}
}

func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", h.memory, h.iterations, h.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *PasswordHasher) Verify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	if len(salt) == 0 {
		return false
	}
	stored, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	if len(stored) == 0 {
		return false
	}
	keyLen, ok := checkedUint32Len(len(stored))
	if !ok {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, keyLen)
	return subtle.ConstantTimeCompare(stored, key) == 1
}

func checkedUint32Len(size int) (uint32, bool) {
	if size < 0 {
		return 0, false
	}
	u := uint64(size)
	if u > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(u), true
}
