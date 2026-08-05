package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
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
	if h == nil || h.memory == 0 || h.iterations == 0 || h.parallelism == 0 || h.keyLen == 0 {
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
	if uint64(len(stored)) != uint64(h.keyLen) {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLen)
	return subtle.ConstantTimeCompare(stored, key) == 1
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
		value, err := strconv.ParseUint(parts[1], 10, bitSize)
		if err != nil || value == 0 {
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
