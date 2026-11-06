package amsonia

import (
	"errors"
	"strings"
	"testing"
)

func TestOpaqueIdentifierContract(t *testing.T) {
	validators := []struct {
		name     string
		validate func(string) error
	}{
		{"tenant", func(v string) error { return TenantID(v).Validate() }},
		{"subject", func(v string) error { return SubjectID(v).Validate() }},
		{"role", func(v string) error { return RoleID(v).Validate() }},
		{"workspace", func(v string) error { return WorkspaceID(v).Validate() }},
		{"resource", func(v string) error { return ResourceID(v).Validate() }},
	}
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "ASCII", value: "invoice-1", valid: true},
		{name: "spaces preserved", value: " invoice 1 ", valid: true},
		{name: "Unicode", value: "租户/用户/账单", valid: true},
		{name: "replacement character", value: "id-\ufffd", valid: true},
		{name: "256 ASCII bytes", value: strings.Repeat("a", 256), valid: true},
		{name: "256 Unicode bytes", value: strings.Repeat("a", 253) + "\ufffd", valid: true},
		{name: "empty"},
		{name: "257 ASCII bytes", value: strings.Repeat("a", 257)},
		{name: "257 Unicode bytes", value: strings.Repeat("a", 254) + "\ufffd"},
		{name: "NUL", value: "id\x00x"},
		{name: "newline", value: "id\nx"},
		{name: "DEL", value: "id\x7fx"},
		{name: "invalid UTF-8", value: string([]byte{0xff})},
		{name: "truncated UTF-8", value: string([]byte{0xe2, 0x82})},
		{name: "surrogate encoding", value: string([]byte{0xed, 0xa0, 0x80})},
	}
	for _, validator := range validators {
		t.Run(validator.name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					err := validator.validate(tt.value)
					if tt.valid && err != nil {
						t.Fatalf("valid identifier %q rejected: %v", tt.value, err)
					}
					if !tt.valid && !errors.Is(err, ErrInvalidInput) {
						t.Fatalf("invalid identifier %q: error = %v; want invalid input", tt.value, err)
					}
				})
			}
		})
	}
}
