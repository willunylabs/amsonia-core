package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argon2SaltLength             = 16
	maxPasswordCodePoints        = 128        // Unicode code points.
	maxEncodedPasswordHashLength = 4096       // bytes.
	minArgon2Memory              = 64         // KiB.
	maxArgon2Memory              = 256 * 1024 // KiB, 256 MiB.
	minArgon2Iterations          = 1
	maxArgon2Iterations          = 32 // rounds.
	minArgon2Parallelism         = 1
	maxArgon2Parallelism         = 16 // lanes.
	minArgon2KeyLength           = 16 // bytes.
	maxArgon2KeyLength           = 64 // bytes.
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
	if !validPasswordInput(password) {
		return "", fmt.Errorf("invalid password input")
	}
	if h == nil || !validArgon2Config(h.memory, h.iterations, h.parallelism, h.keyLen) {
		return "", fmt.Errorf("invalid argon2 configuration")
	}
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", h.memory, h.iterations, h.parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *PasswordHasher) Verify(password, encoded string) bool {
	if !validPasswordInput(password) {
		return false
	}
	if len(encoded) > maxEncodedPasswordHashLength {
		return false
	}
	if h == nil || !validArgon2Config(h.memory, h.iterations, h.parallelism, h.keyLen) {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	params, ok := parseArgon2Parameters(parts[2])
	if !ok || params.memory != h.memory || params.iterations != h.iterations || params.parallelism != h.parallelism {
		return false
	}
	if len(parts[3]) != base64.RawStdEncoding.EncodedLen(argon2SaltLength) {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	if len(salt) == 0 || len(salt) != argon2SaltLength {
		return false
	}
	if len(parts[4]) != base64.RawStdEncoding.EncodedLen(int(h.keyLen)) {
		return false
	}
	stored, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	if len(stored) == 0 {
		return false
	}
	if uint64(len(stored)) != uint64(h.keyLen) {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLen)
	return subtle.ConstantTimeCompare(stored, key) == 1
}

func validPasswordInput(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) <= maxPasswordCodePoints
}

func validArgon2Config(memory, iterations uint32, parallelism uint8, keyLen uint32) bool {
	if memory == 0 || iterations == 0 || parallelism == 0 || keyLen == 0 {
		return false
	}
	if memory < minArgon2Memory || iterations < minArgon2Iterations || parallelism < minArgon2Parallelism || keyLen < minArgon2KeyLength {
		return false
	}
	if memory > maxArgon2Memory || iterations > maxArgon2Iterations || parallelism > maxArgon2Parallelism || keyLen > maxArgon2KeyLength {
		return false
	}
	return memory >= 8*uint32(parallelism)
}

type argon2Parameters struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2Parameters(encoded string) (argon2Parameters, bool) {
	tokens := strings.Split(encoded, ",")
	if len(tokens) != 3 {
		return argon2Parameters{}, false
	}

	var params argon2Parameters
	seen := make(map[string]struct{}, len(tokens))
	for index, token := range tokens {
		parts := strings.Split(token, "=")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return argon2Parameters{}, false
		}
		if _, found := seen[parts[0]]; found {
			return argon2Parameters{}, false
		}
		seen[parts[0]] = struct{}{}

		var bitSize int
		switch parts[0] {
		case "m":
			bitSize = 32
		case "t":
			bitSize = 32
		case "p":
			bitSize = 8
		default:
			return argon2Parameters{}, false
		}
		value, ok := parseDecimalUint(parts[1], bitSize)
		if !ok || value == 0 {
			return argon2Parameters{}, false
		}
		switch index {
		case 0:
			if parts[0] != "m" {
				return argon2Parameters{}, false
			}
			params.memory = uint32(value)
		case 1:
			if parts[0] != "t" {
				return argon2Parameters{}, false
			}
			params.iterations = uint32(value)
		case 2:
			if parts[0] != "p" {
				return argon2Parameters{}, false
			}
			params.parallelism = uint8(value)
		}
	}
	return params, true
}

func parseDecimalUint(value string, bitSize int) (uint64, bool) {
	if value == "" {
		return 0, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseUint(value, 10, bitSize)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
