package coresync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSourceCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestSyncIsDeterministicAndVerifiable(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "core/z.txt", "zeta\n")
	writeTestFile(t, sourceRoot, "core/a.txt", "alpha\n")

	manifest := validManifest(
		Entry{Source: "core/z.txt", Destination: "lib/z.txt"},
		Entry{Source: "core/a.txt", Destination: "lib/a.txt"},
	)
	options := Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  ".amsonia/provenance.json",
	}

	if err := Sync(manifest, options); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}
	firstProvenance := readTestFile(t, destinationRoot, options.ProvenancePath)

	if err := Verify(destinationRoot, options.ProvenancePath); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	checkOptions := options
	checkOptions.Check = true
	if err := Sync(manifest, checkOptions); err != nil {
		t.Fatalf("Sync(Check) error = %v", err)
	}
	if err := Sync(manifest, options); err != nil {
		t.Fatalf("second Sync() error = %v", err)
	}
	secondProvenance := readTestFile(t, destinationRoot, options.ProvenancePath)
	if string(secondProvenance) != string(firstProvenance) {
		t.Fatalf("provenance changed between identical syncs:\nfirst:  %s\nsecond: %s", firstProvenance, secondProvenance)
	}

	var provenance Provenance
	if err := json.Unmarshal(firstProvenance, &provenance); err != nil {
		t.Fatalf("json.Unmarshal(provenance) error = %v", err)
	}
	if provenance.SourceCommit != testSourceCommit {
		t.Fatalf("SourceCommit = %q, want %q", provenance.SourceCommit, testSourceCommit)
	}
	if got, want := provenance.Entries[0].Destination, "lib/a.txt"; got != want {
		t.Fatalf("first destination = %q, want %q", got, want)
	}
	if got, want := string(readTestFile(t, destinationRoot, "lib/a.txt")), "alpha\n"; got != want {
		t.Fatalf("copied content = %q, want %q", got, want)
	}
}

func TestSyncRejectsUnmanagedDestinationOverwrite(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "source.txt", "new\n")
	writeTestFile(t, destinationRoot, "export.txt", "existing\n")

	err := Sync(validManifest(Entry{Source: "source.txt", Destination: "export.txt"}), Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  "provenance.json",
	})
	assertErrorContains(t, err, "unmanaged destination")
	if got, want := string(readTestFile(t, destinationRoot, "export.txt")), "existing\n"; got != want {
		t.Fatalf("unmanaged file content = %q, want unchanged %q", got, want)
	}
}

func TestValidateManifestRejectsSourceTraversal(t *testing.T) {
	manifest := validManifest(Entry{Source: "../secret.txt", Destination: "secret.txt"})
	assertErrorContains(t, ValidateManifest(manifest), "unsafe source path")
}

func TestValidateManifestRejectsDestinationTraversal(t *testing.T) {
	manifest := validManifest(Entry{Source: "secret.txt", Destination: "../secret.txt"})
	assertErrorContains(t, ValidateManifest(manifest), "unsafe destination path")
}

func TestSyncRejectsSourceSymlink(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "real.txt", "content\n")
	if err := os.Symlink("real.txt", filepath.Join(sourceRoot, "linked.txt")); err != nil {
		t.Skipf("os.Symlink() unavailable: %v", err)
	}

	err := Sync(validManifest(Entry{Source: "linked.txt", Destination: "copied.txt"}), Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  "provenance.json",
	})
	assertErrorContains(t, err, "symlink")
}

func TestSyncCheckDetectsModifiedDestinationContentWithoutWriting(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "source.txt", "expected\n")
	manifest := validManifest(Entry{Source: "source.txt", Destination: "export.txt"})
	options := Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  "provenance.json",
	}
	if err := Sync(manifest, options); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	provenanceBefore := readTestFile(t, destinationRoot, options.ProvenancePath)
	writeTestFile(t, destinationRoot, "export.txt", "modified\n")

	options.Check = true
	assertErrorContains(t, Sync(manifest, options), "content drift")
	if got, want := string(readTestFile(t, destinationRoot, "export.txt")), "modified\n"; got != want {
		t.Fatalf("destination after check = %q, want unchanged %q", got, want)
	}
	if got := readTestFile(t, destinationRoot, options.ProvenancePath); string(got) != string(provenanceBefore) {
		t.Fatalf("check mode changed provenance:\nbefore: %s\nafter:  %s", provenanceBefore, got)
	}
}

func TestVerifyDetectsModifiedDestinationContent(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "source.txt", "expected\n")
	manifest := validManifest(Entry{Source: "source.txt", Destination: "export.txt"})
	options := Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  "provenance.json",
	}
	if err := Sync(manifest, options); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	writeTestFile(t, destinationRoot, "export.txt", "modified\n")

	assertErrorContains(t, Verify(destinationRoot, options.ProvenancePath), "hash mismatch")
}

func TestLoadManifestRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "unknown field",
			content: `{"schema_version":1,"source_repository":"github.com/willunylabs/amsonia","license":"Apache-2.0","entries":[{"source":"a","destination":"b"}],"unknown":true}`,
		},
		{
			name:    "trailing JSON",
			content: `{"schema_version":1,"source_repository":"github.com/willunylabs/amsonia","license":"Apache-2.0","entries":[{"source":"a","destination":"b"}]} {}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "manifest.json")
			if err := os.WriteFile(filename, []byte(test.content), 0o600); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
			if _, err := LoadManifest(filename); err == nil {
				t.Fatal("LoadManifest() error = nil, want rejection")
			}
		})
	}
}

func TestValidateManifestRules(t *testing.T) {
	tests := []struct {
		name       string
		manifest   Manifest
		wantSubstr string
	}{
		{name: "schema", manifest: Manifest{SchemaVersion: 2, SourceRepository: "repo", License: "Apache-2.0", Entries: []Entry{{Source: "a", Destination: "b"}}}, wantSubstr: "schema_version"},
		{name: "source repository", manifest: Manifest{SchemaVersion: 1, License: "Apache-2.0", Entries: []Entry{{Source: "a", Destination: "b"}}}, wantSubstr: "source_repository"},
		{name: "license", manifest: Manifest{SchemaVersion: 1, SourceRepository: "repo", License: "MIT", Entries: []Entry{{Source: "a", Destination: "b"}}}, wantSubstr: "license"},
		{name: "entries", manifest: Manifest{SchemaVersion: 1, SourceRepository: "repo", License: "Apache-2.0"}, wantSubstr: "entry"},
		{name: "absolute source", manifest: validManifest(Entry{Source: "/a", Destination: "b"}), wantSubstr: "unsafe source path"},
		{name: "backslash source", manifest: validManifest(Entry{Source: `a\b`, Destination: "b"}), wantSubstr: "unsafe source path"},
		{name: "unclean source", manifest: validManifest(Entry{Source: "a/../b", Destination: "b"}), wantSubstr: "unsafe source path"},
		{name: "absolute destination", manifest: validManifest(Entry{Source: "a", Destination: "/b"}), wantSubstr: "unsafe destination path"},
		{name: "duplicate source", manifest: validManifest(Entry{Source: "a", Destination: "b"}, Entry{Source: "a", Destination: "c"}), wantSubstr: "duplicate source"},
		{name: "duplicate destination", manifest: validManifest(Entry{Source: "a", Destination: "b"}, Entry{Source: "c", Destination: "b"}), wantSubstr: "duplicate destination"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertErrorContains(t, ValidateManifest(test.manifest), test.wantSubstr)
		})
	}
}

func TestSyncAllowsOnlyManagedFilesAndRejectsRemoval(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "one.txt", "one\n")
	writeTestFile(t, sourceRoot, "two.txt", "two\n")
	options := Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  "provenance.json",
	}
	manifest := validManifest(
		Entry{Source: "one.txt", Destination: "one.txt"},
		Entry{Source: "two.txt", Destination: "two.txt"},
	)
	if err := Sync(manifest, options); err != nil {
		t.Fatalf("first Sync() error = %v", err)
	}

	writeTestFile(t, sourceRoot, "one.txt", "updated\n")
	if err := Sync(manifest, options); err != nil {
		t.Fatalf("managed overwrite Sync() error = %v", err)
	}
	if got, want := string(readTestFile(t, destinationRoot, "one.txt")), "updated\n"; got != want {
		t.Fatalf("managed content = %q, want %q", got, want)
	}

	reducedManifest := validManifest(Entry{Source: "one.txt", Destination: "one.txt"})
	if err := Sync(reducedManifest, options); err == nil {
		t.Fatal("Sync() error = nil, want removed managed destination rejection")
	}
	if got, want := string(readTestFile(t, destinationRoot, "two.txt")), "two\n"; got != want {
		t.Fatalf("removed managed destination = %q, want unchanged %q", got, want)
	}
}

func TestSyncRejectsDestinationSymlinkComponent(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	outsideRoot := t.TempDir()
	writeTestFile(t, sourceRoot, "source.txt", "content\n")
	if err := os.Symlink(outsideRoot, filepath.Join(destinationRoot, "linked")); err != nil {
		t.Skipf("os.Symlink() unavailable: %v", err)
	}

	err := Sync(validManifest(Entry{Source: "source.txt", Destination: "linked/export.txt"}), Options{
		SourceRoot:      sourceRoot,
		DestinationRoot: destinationRoot,
		SourceCommit:    testSourceCommit,
		ProvenancePath:  "provenance.json",
	})
	assertErrorContains(t, err, "symlink")
}

func validManifest(entries ...Entry) Manifest {
	return Manifest{
		SchemaVersion:    1,
		SourceRepository: "github.com/willunylabs/amsonia",
		License:          "Apache-2.0",
		Entries:          entries,
	}
}

func writeTestFile(t *testing.T, root, relativePath, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(filename), err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", filename, err)
	}
}

func readTestFile(t *testing.T, root, relativePath string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", relativePath, err)
	}
	return content
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
