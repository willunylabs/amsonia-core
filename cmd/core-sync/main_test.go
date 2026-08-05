package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSourceCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRunSyncsAndVerifies(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeFile(t, sourceRoot, "core/source.txt", "core source\n")

	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	manifest := `{
  "schema_version": 1,
  "source_repository": "github.com/willunylabs/amsonia",
  "license": "Apache-2.0",
  "entries": [
    {"source": "core/source.txt", "destination": "exported/source.txt"}
  ]
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("os.WriteFile(manifest) error = %v", err)
	}

	provenancePath := ".amsonia/provenance.json"
	if err := run(syncArguments("sync", manifestPath, sourceRoot, destinationRoot, provenancePath)); err != nil {
		t.Fatalf("run(sync) error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destinationRoot, "exported", "source.txt"))
	if err != nil {
		t.Fatalf("os.ReadFile(destination) error = %v", err)
	}
	if got, want := string(content), "core source\n"; got != want {
		t.Fatalf("destination content = %q, want %q", got, want)
	}

	if err := run([]string{
		"--mode", "verify",
		"--destination-root", destinationRoot,
		"--provenance", provenancePath,
	}); err != nil {
		t.Fatalf("run(verify) error = %v", err)
	}
}

func TestRunCheckDetectsDriftWithoutWriting(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeFile(t, sourceRoot, "source.txt", "original\n")

	manifestPath := filepath.Join(t.TempDir(), "manifest.json")
	manifest := `{"schema_version":1,"source_repository":"github.com/willunylabs/amsonia","license":"Apache-2.0","entries":[{"source":"source.txt","destination":"destination.txt"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("os.WriteFile(manifest) error = %v", err)
	}
	provenancePath := "provenance.json"
	if err := run(syncArguments("sync", manifestPath, sourceRoot, destinationRoot, provenancePath)); err != nil {
		t.Fatalf("run(sync) error = %v", err)
	}
	writeFile(t, sourceRoot, "source.txt", "updated\n")

	err := run(syncArguments("check", manifestPath, sourceRoot, destinationRoot, provenancePath))
	assertErrorContains(t, err, "content drift")
	content, readErr := os.ReadFile(filepath.Join(destinationRoot, "destination.txt"))
	if readErr != nil {
		t.Fatalf("os.ReadFile(destination) error = %v", readErr)
	}
	if got, want := string(content), "original\n"; got != want {
		t.Fatalf("destination content after check = %q, want unchanged %q", got, want)
	}
}

func TestRunRejectsUnknownMode(t *testing.T) {
	assertErrorContains(t, run([]string{"--mode", "unknown"}), `unknown mode "unknown"`)
}

func TestRunHelpSucceeds(t *testing.T) {
	if err := run([]string{"--help"}); err != nil {
		t.Fatalf("run(--help) error = %v, want nil", err)
	}
}

func TestRunRequiresSyncAndCheckFlags(t *testing.T) {
	flags := []struct {
		name  string
		value string
	}{
		{name: "manifest", value: "manifest.json"},
		{name: "source-root", value: "source"},
		{name: "destination-root", value: "destination"},
		{name: "source-commit", value: testSourceCommit},
		{name: "provenance", value: "provenance.json"},
	}

	for _, mode := range []string{"sync", "check"} {
		for _, missing := range flags {
			t.Run(mode+" missing "+missing.name, func(t *testing.T) {
				arguments := []string{"--mode", mode}
				for _, flag := range flags {
					if flag.name != missing.name {
						arguments = append(arguments, "--"+flag.name, flag.value)
					}
				}

				assertErrorContains(t, run(arguments), "--"+missing.name+" is required")
			})
		}
	}
}

func TestRunRequiresVerifyFlags(t *testing.T) {
	flags := []struct {
		name  string
		value string
	}{
		{name: "destination-root", value: "destination"},
		{name: "provenance", value: "provenance.json"},
	}

	for _, missing := range flags {
		t.Run("missing "+missing.name, func(t *testing.T) {
			arguments := []string{"--mode", "verify"}
			for _, flag := range flags {
				if flag.name != missing.name {
					arguments = append(arguments, "--"+flag.name, flag.value)
				}
			}

			assertErrorContains(t, run(arguments), "--"+missing.name+" is required")
		})
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	assertErrorContains(t, run([]string{"unexpected"}), "positional arguments are not allowed")
}

func syncArguments(mode, manifestPath, sourceRoot, destinationRoot, provenancePath string) []string {
	return []string{
		"--mode", mode,
		"--manifest", manifestPath,
		"--source-root", sourceRoot,
		"--destination-root", destinationRoot,
		"--source-commit", testSourceCommit,
		"--provenance", provenancePath,
	}
}

func writeFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(filename), err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", filename, err)
	}
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want substring %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err, want)
	}
}
