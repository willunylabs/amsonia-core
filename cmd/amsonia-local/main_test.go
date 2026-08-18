package main

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureLocalEnvironmentCreatesSecureConsistentSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".amsonia", "local.env")
	created, err := ensureLocalEnvironment(path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("environment was not created")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	values := readEnvironment(t, path)
	runtimeHex, err := hex.DecodeString(values["AMSONIA_RUNTIME_SECRET_HEX"])
	if err != nil {
		t.Fatalf("decode runtime hex: %v", err)
	}
	runtimeBinding, err := base64.RawURLEncoding.DecodeString(values["AMSONIA_TENANT_BINDING_SECRET"])
	if err != nil {
		t.Fatalf("decode runtime binding: %v", err)
	}
	if string(runtimeHex) != string(runtimeBinding) {
		t.Fatal("runtime binding does not encode the configured runtime secret")
	}
	if values["AMSONIA_RUNTIME_SECRET_HEX"] == values["AMSONIA_MAINTENANCE_SECRET_HEX"] {
		t.Fatal("runtime and maintenance binding secrets must differ")
	}
	for _, name := range []string{
		"AMSONIA_POSTGRES_OWNER_PASSWORD",
		"AMSONIA_POSTGRES_RUNTIME_PASSWORD",
		"AMSONIA_POSTGRES_MAINTENANCE_PASSWORD",
	} {
		if len(values[name]) < 40 || strings.Contains(values[name], "CHANGE_ME") {
			t.Fatalf("%s was not generated securely", name)
		}
	}
	if !strings.Contains(values["AMSONIA_DATABASE_DSN"], values["AMSONIA_POSTGRES_RUNTIME_PASSWORD"]) {
		t.Fatal("local runtime DSN does not use the generated runtime password")
	}
}

func TestEnsureLocalEnvironmentDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".amsonia", "local.env")
	if _, err := ensureLocalEnvironment(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := ensureLocalEnvironment(path)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("existing environment was replaced")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("existing environment changed")
	}
}

func TestEnsureLocalEnvironmentRejectsBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.env")
	if err := os.WriteFile(path, []byte("not-a-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureLocalEnvironment(path); err == nil || !strings.Contains(err.Error(), "must not be readable") {
		t.Fatalf("expected insecure permissions error, got %v", err)
	}
}

func TestComposeArgsAlwaysUsesLocalOverride(t *testing.T) {
	got := strings.Join(composeArgs("ps"), " ")
	want := "compose --env-file .amsonia/local.env --file compose.yaml --file compose.local.yaml ps"
	if got != want {
		t.Fatalf("compose args = %q, want %q", got, want)
	}
}

func readEnvironment(t *testing.T, path string) map[string]string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid environment line %q", line)
		}
		values[name] = value
	}
	return values
}
