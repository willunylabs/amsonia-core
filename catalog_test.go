package amsonia

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePermissionKeyValid(t *testing.T) {
	cases := []string{
		"billing:invoice:read",
		"a:b:c",
		"billing:invoice_line:read_all",
		"ai_usage:event:write",
	}
	for _, raw := range cases {
		key, err := ParsePermissionKey(raw)
		if err != nil {
			t.Fatalf("ParsePermissionKey(%q) unexpected error: %v", raw, err)
		}
		if string(key) != raw {
			t.Fatalf("ParsePermissionKey(%q) = %q, want %q", raw, key, raw)
		}
	}
}

func TestParsePermissionKeyInvalid(t *testing.T) {
	cases := []string{
		"",
		"billing:invoice",
		"billing:invoice:read:extra",
		":invoice:read",
		"billing::read",
		"billing:invoice:",
		"Billing:invoice:read",
		"billing:Invoice:read",
		"billing:invoice:read ",
		" billing:invoice:read",
		"billing:invoice:read*",
		"billing:*:read",
		"1billing:invoice:read",
		"billing:invoice:read-extra",
		"billing:invoice:re ad",
		"billing:inv" + strings.Repeat("o", 70) + "ice:read",
	}
	for _, raw := range cases {
		if _, err := ParsePermissionKey(raw); err == nil {
			t.Fatalf("ParsePermissionKey(%q) expected error, got nil", raw)
		}
	}
}

func TestNewCatalogRejectsDuplicates(t *testing.T) {
	_, err := NewCatalog([]PermissionDefinition{
		{Key: "billing:invoice:read"},
		{Key: "billing:invoice:read"},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestNewCatalogRejectsInvalidKey(t *testing.T) {
	_, err := NewCatalog([]PermissionDefinition{{Key: "invalid"}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCatalogLookup(t *testing.T) {
	cat, err := NewCatalog([]PermissionDefinition{
		{Key: "billing:invoice:read", Description: "read invoices"},
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := cat.Lookup("billing:invoice:read")
	if !ok {
		t.Fatal("expected lookup to succeed")
	}
	if def.Description != "read invoices" {
		t.Fatalf("unexpected description %q", def.Description)
	}
	if _, ok := cat.Lookup("billing:invoice:write"); ok {
		t.Fatal("expected lookup to fail")
	}
}

func TestCatalogKeysSorted(t *testing.T) {
	cat, err := NewCatalog([]PermissionDefinition{
		{Key: "z:y:x"},
		{Key: "a:b:c"},
		{Key: "m:n:o"},
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := cat.Keys()
	want := []string{"a:b:c", "m:n:o", "z:y:x"}
	for i := range want {
		if string(keys[i]) != want[i] {
			t.Fatalf("Keys()[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestOpaqueIDValidation(t *testing.T) {
	if err := TenantID("").Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty tenant, got %v", err)
	}
	if err := TenantID("t1").Validate(); err != nil {
		t.Fatalf("unexpected error for valid tenant: %v", err)
	}
	if err := TenantID(strings.Repeat("x", 257)).Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for oversized ID, got %v", err)
	}
}
