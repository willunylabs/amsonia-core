package amsonia

import (
	"fmt"
	"strings"
	"unicode"
)

// segmentLimits define the permission key grammar:
// {namespace}:{resource}:{action}, each segment 1..64 lowercase ASCII
// letters, digits, or underscores, starting with a letter.
const (
	minSegments      = 3
	maxSegments      = 3
	maxSegmentLength = 64
)

// ParsePermissionKey validates raw and returns a canonical PermissionKey.
// Empty segments, uppercase, whitespace, wildcards, and extra segments are
// rejected.
func ParsePermissionKey(raw string) (PermissionKey, error) {
	if strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("%w: permission key must not contain whitespace", ErrInvalidInput)
	}
	key := PermissionKey(raw)
	if err := key.Validate(); err != nil {
		return "", err
	}
	return key, nil
}

// Validate checks the canonical three-segment permission grammar.
func (k PermissionKey) Validate() error {
	raw := string(k)
	if raw == "" {
		return fmt.Errorf("%w: empty permission key", ErrInvalidInput)
	}
	if strings.TrimSpace(raw) != raw {
		return fmt.Errorf("%w: permission key must not contain whitespace", ErrInvalidInput)
	}
	segments := strings.Split(raw, ":")
	if len(segments) != minSegments {
		return fmt.Errorf("%w: permission key must have exactly %d segments, got %d", ErrInvalidInput, minSegments, len(segments))
	}
	for i, seg := range segments {
		if err := validateSegment(seg); err != nil {
			return fmt.Errorf("%w: invalid segment %d: %v", ErrInvalidInput, i+1, err)
		}
	}
	return nil
}

func validateSegment(seg string) error {
	if seg == "" {
		return fmt.Errorf("empty segment")
	}
	if len(seg) > maxSegmentLength {
		return fmt.Errorf("segment longer than %d bytes", maxSegmentLength)
	}
	for i, r := range seg {
		if r > unicode.MaxASCII || r == '*' {
			return fmt.Errorf("segment must be lowercase ASCII, digit, or underscore")
		}
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return fmt.Errorf("segment must be lowercase ASCII, digit, or underscore")
		}
		if i == 0 && !(r >= 'a' && r <= 'z') {
			return fmt.Errorf("segment must start with a letter")
		}
	}
	return nil
}

// Catalog is an immutable, validated set of application permission
// definitions. Construct with NewCatalog; the catalog cannot be mutated
// afterwards.
type Catalog struct {
	byKey map[PermissionKey]PermissionDefinition
}

// NewCatalog validates and indexes permission definitions. Duplicate keys
// cause construction to fail.
func NewCatalog(definitions []PermissionDefinition) (*Catalog, error) {
	if len(definitions) == 0 {
		return nil, fmt.Errorf("%w: catalog requires at least one permission", ErrInvalidInput)
	}
	byKey := make(map[PermissionKey]PermissionDefinition, len(definitions))
	for _, d := range definitions {
		if err := d.Key.Validate(); err != nil {
			return nil, err
		}
		if _, dup := byKey[d.Key]; dup {
			return nil, fmt.Errorf("%w: duplicate permission key %q", ErrInvalidInput, d.Key)
		}
		byKey[d.Key] = d
	}
	return &Catalog{byKey: byKey}, nil
}

// Lookup returns the definition for a key and whether it exists.
func (c *Catalog) Lookup(key PermissionKey) (PermissionDefinition, bool) {
	d, ok := c.byKey[key]
	return d, ok
}

// Keys returns the catalog keys in sorted order.
func (c *Catalog) Keys() []PermissionKey {
	out := make([]PermissionKey, 0, len(c.byKey))
	for k := range c.byKey {
		out = append(out, k)
	}
	sortKeys(out)
	return out
}

func sortKeys(keys []PermissionKey) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}
