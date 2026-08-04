// Package coresync copies a declared set of files and records deterministic provenance.
package coresync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const supportedSchemaVersion = 1

var (
	sourceCommitPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	sha256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Entry maps one source file to one destination file.
type Entry struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// Manifest declares the files managed by a sync operation.
type Manifest struct {
	SchemaVersion    int     `json:"schema_version"`
	SourceRepository string  `json:"source_repository"`
	License          string  `json:"license"`
	Entries          []Entry `json:"entries"`
}

// ProvenanceEntry records the digest of one managed destination.
type ProvenanceEntry struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
}

// Provenance records the exact source and content managed by a sync operation.
type Provenance struct {
	SchemaVersion    int               `json:"schema_version"`
	SourceRepository string            `json:"source_repository"`
	SourceCommit     string            `json:"source_commit"`
	License          string            `json:"license"`
	Entries          []ProvenanceEntry `json:"entries"`
}

// Options configures a sync operation.
type Options struct {
	SourceRoot      string
	DestinationRoot string
	SourceCommit    string
	ProvenancePath  string
	Check           bool
}

// LoadManifest reads and validates a manifest JSON file.
func LoadManifest(filename string) (Manifest, error) {
	var manifest Manifest
	if err := decodeJSONFile(filename, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("load manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ValidateManifest validates the manifest schema and all declared paths.
func ValidateManifest(manifest Manifest) error {
	if manifest.SchemaVersion != supportedSchemaVersion {
		return fmt.Errorf("schema_version must be %d", supportedSchemaVersion)
	}
	if strings.TrimSpace(manifest.SourceRepository) == "" {
		return errors.New("source_repository must be non-empty")
	}
	if manifest.License != "Apache-2.0" {
		return errors.New("license must be Apache-2.0")
	}
	if len(manifest.Entries) == 0 {
		return errors.New("manifest must contain at least one entry")
	}

	sources := make(map[string]struct{}, len(manifest.Entries))
	destinations := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if !isSafeRelativePath(entry.Source) {
			return fmt.Errorf("unsafe source path %q", entry.Source)
		}
		if !isSafeRelativePath(entry.Destination) {
			return fmt.Errorf("unsafe destination path %q", entry.Destination)
		}
		if _, exists := sources[entry.Source]; exists {
			return fmt.Errorf("duplicate source path %q", entry.Source)
		}
		if _, exists := destinations[entry.Destination]; exists {
			return fmt.Errorf("duplicate destination path %q", entry.Destination)
		}
		sources[entry.Source] = struct{}{}
		destinations[entry.Destination] = struct{}{}
	}
	return nil
}

// Sync copies all manifest entries and writes deterministic provenance.
func Sync(manifest Manifest, options Options) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	if err := validateOptions(options); err != nil {
		return err
	}

	existing, existingBytes, provenanceExists, err := loadProvenance(options.DestinationRoot, options.ProvenancePath)
	if err != nil {
		return err
	}
	if provenanceExists && existing.SourceRepository != manifest.SourceRepository {
		return fmt.Errorf("existing provenance belongs to different source repository %q", existing.SourceRepository)
	}

	managed := make(map[string]struct{}, len(existing.Entries))
	for _, entry := range existing.Entries {
		managed[entry.Destination] = struct{}{}
	}
	current := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		current[entry.Destination] = struct{}{}
		if entry.Destination == options.ProvenancePath {
			return fmt.Errorf("destination %q conflicts with provenance path", entry.Destination)
		}
	}
	for _, entry := range existing.Entries {
		if _, exists := current[entry.Destination]; !exists {
			return fmt.Errorf("previously managed destination %q was removed from manifest", entry.Destination)
		}
	}

	preparedEntries := make([]preparedEntry, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		content, digest, err := readSourceFile(options.SourceRoot, entry.Source)
		if err != nil {
			return err
		}
		preparedEntries = append(preparedEntries, preparedEntry{
			entry:   entry,
			content: content,
			digest:  digest,
		})
	}
	sort.Slice(preparedEntries, func(i, j int) bool {
		return preparedEntries[i].entry.Destination < preparedEntries[j].entry.Destination
	})

	desired := Provenance{
		SchemaVersion:    supportedSchemaVersion,
		SourceRepository: manifest.SourceRepository,
		SourceCommit:     options.SourceCommit,
		License:          manifest.License,
		Entries:          make([]ProvenanceEntry, 0, len(preparedEntries)),
	}
	for _, prepared := range preparedEntries {
		desired.Entries = append(desired.Entries, ProvenanceEntry{
			Source:      prepared.entry.Source,
			Destination: prepared.entry.Destination,
			SHA256:      prepared.digest,
		})
	}
	desiredBytes, err := marshalProvenance(desired)
	if err != nil {
		return err
	}

	for _, prepared := range preparedEntries {
		_, isManaged := managed[prepared.entry.Destination]
		exists, err := inspectDestination(options.DestinationRoot, prepared.entry.Destination)
		if err != nil {
			return err
		}
		if exists && !isManaged {
			return fmt.Errorf("unmanaged destination %q already exists", prepared.entry.Destination)
		}
		if options.Check {
			if !exists {
				return fmt.Errorf("content drift for destination %q: file is missing", prepared.entry.Destination)
			}
			actual, err := hashDestinationFile(options.DestinationRoot, prepared.entry.Destination)
			if err != nil {
				return fmt.Errorf("content drift for destination %q: %w", prepared.entry.Destination, err)
			}
			if actual != prepared.digest {
				return fmt.Errorf("content drift for destination %q", prepared.entry.Destination)
			}
		}
	}

	if options.Check {
		if !provenanceExists {
			return errors.New("provenance drift: provenance file is missing")
		}
		if !bytes.Equal(existingBytes, desiredBytes) {
			return errors.New("provenance drift")
		}
		return nil
	}

	for _, prepared := range preparedEntries {
		_, canOverwrite := managed[prepared.entry.Destination]
		if err := atomicWriteRelative(options.DestinationRoot, prepared.entry.Destination, prepared.content, canOverwrite); err != nil {
			return fmt.Errorf("write destination %q: %w", prepared.entry.Destination, err)
		}
	}
	if err := atomicWriteRelative(options.DestinationRoot, options.ProvenancePath, desiredBytes, provenanceExists); err != nil {
		return fmt.Errorf("write provenance: %w", err)
	}
	return nil
}

// Verify checks every destination against the committed provenance.
func Verify(destinationRoot, provenancePath string) error {
	if err := validateRoot("destination root", destinationRoot); err != nil {
		return err
	}
	if !isSafeRelativePath(provenancePath) {
		return fmt.Errorf("unsafe provenance path %q", provenancePath)
	}

	provenance, _, exists, err := loadProvenance(destinationRoot, provenancePath)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("provenance file is missing")
	}
	for _, entry := range provenance.Entries {
		if entry.Destination == provenancePath {
			return fmt.Errorf("destination %q conflicts with provenance path", entry.Destination)
		}
		digest, err := hashDestinationFile(destinationRoot, entry.Destination)
		if err != nil {
			return fmt.Errorf("hash mismatch for destination %q: %w", entry.Destination, err)
		}
		if digest != entry.SHA256 {
			return fmt.Errorf("hash mismatch for destination %q", entry.Destination)
		}
	}
	return nil
}

type preparedEntry struct {
	entry   Entry
	content []byte
	digest  string
}

func validateOptions(options Options) error {
	if err := validateRoot("source root", options.SourceRoot); err != nil {
		return err
	}
	if err := validateRoot("destination root", options.DestinationRoot); err != nil {
		return err
	}
	if !sourceCommitPattern.MatchString(options.SourceCommit) {
		return errors.New("source commit must be 40-64 lowercase hexadecimal characters")
	}
	if !isSafeRelativePath(options.ProvenancePath) {
		return fmt.Errorf("unsafe provenance path %q", options.ProvenancePath)
	}
	return nil
}

func validateRoot(name, root string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("%s is required", name)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symlink", name)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be a directory", name)
	}
	return nil
}

func isSafeRelativePath(value string) bool {
	if value == "" || value == "." || strings.Contains(value, `\`) || strings.ContainsRune(value, 0) {
		return false
	}
	if path.IsAbs(value) || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	return !(len(value) >= 2 && value[1] == ':' && isASCIIAlpha(value[0]))
}

func isASCIIAlpha(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func decodeJSONFile(filename string, target any) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func validateProvenance(provenance Provenance) error {
	if provenance.SchemaVersion != supportedSchemaVersion {
		return fmt.Errorf("provenance schema_version must be %d", supportedSchemaVersion)
	}
	if strings.TrimSpace(provenance.SourceRepository) == "" {
		return errors.New("provenance source_repository must be non-empty")
	}
	if !sourceCommitPattern.MatchString(provenance.SourceCommit) {
		return errors.New("provenance source_commit must be 40-64 lowercase hexadecimal characters")
	}
	if provenance.License != "Apache-2.0" {
		return errors.New("provenance license must be Apache-2.0")
	}
	if len(provenance.Entries) == 0 {
		return errors.New("provenance must contain at least one entry")
	}

	sources := make(map[string]struct{}, len(provenance.Entries))
	destinations := make(map[string]struct{}, len(provenance.Entries))
	for _, entry := range provenance.Entries {
		if !isSafeRelativePath(entry.Source) {
			return fmt.Errorf("unsafe provenance source path %q", entry.Source)
		}
		if !isSafeRelativePath(entry.Destination) {
			return fmt.Errorf("unsafe destination path %q in provenance", entry.Destination)
		}
		if !sha256Pattern.MatchString(entry.SHA256) {
			return fmt.Errorf("invalid SHA-256 for destination %q", entry.Destination)
		}
		if _, exists := sources[entry.Source]; exists {
			return fmt.Errorf("duplicate provenance source path %q", entry.Source)
		}
		if _, exists := destinations[entry.Destination]; exists {
			return fmt.Errorf("duplicate provenance destination path %q", entry.Destination)
		}
		sources[entry.Source] = struct{}{}
		destinations[entry.Destination] = struct{}{}
	}
	return nil
}

func loadProvenance(destinationRoot, provenancePath string) (Provenance, []byte, bool, error) {
	if err := rejectSymlinkComponents(destinationRoot, provenancePath, true); err != nil {
		return Provenance{}, nil, false, fmt.Errorf("inspect provenance: %w", err)
	}
	filename := joinRelative(destinationRoot, provenancePath)
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return Provenance{}, nil, false, nil
	}
	if err != nil {
		return Provenance{}, nil, false, fmt.Errorf("inspect provenance: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Provenance{}, nil, false, errors.New("provenance must be a regular file")
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		return Provenance{}, nil, false, fmt.Errorf("read provenance: %w", err)
	}
	var provenance Provenance
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&provenance); err != nil {
		return Provenance{}, nil, false, fmt.Errorf("decode provenance: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Provenance{}, nil, false, errors.New("decode provenance: trailing JSON value")
		}
		return Provenance{}, nil, false, fmt.Errorf("decode provenance trailing JSON: %w", err)
	}
	if err := validateProvenance(provenance); err != nil {
		return Provenance{}, nil, false, err
	}
	return provenance, content, true, nil
}

func marshalProvenance(provenance Provenance) ([]byte, error) {
	content, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal provenance: %w", err)
	}
	return append(content, '\n'), nil
}

func readSourceFile(sourceRoot, sourcePath string) ([]byte, string, error) {
	if err := rejectSymlinkComponents(sourceRoot, sourcePath, false); err != nil {
		return nil, "", fmt.Errorf("inspect source %q: %w", sourcePath, err)
	}
	filename := joinRelative(sourceRoot, sourcePath)
	file, err := os.Open(filename)
	if err != nil {
		return nil, "", fmt.Errorf("open source %q: %w", sourcePath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("inspect source %q: %w", sourcePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("source %q must be a regular file", sourcePath)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, "", fmt.Errorf("read source %q: %w", sourcePath, err)
	}
	digest := sha256.Sum256(content)
	return content, hex.EncodeToString(digest[:]), nil
}

func inspectDestination(destinationRoot, destinationPath string) (bool, error) {
	if err := rejectSymlinkComponents(destinationRoot, destinationPath, true); err != nil {
		return false, fmt.Errorf("inspect destination %q: %w", destinationPath, err)
	}
	info, err := os.Lstat(joinRelative(destinationRoot, destinationPath))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect destination %q: %w", destinationPath, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("destination %q must be a regular file", destinationPath)
	}
	return true, nil
}

func hashDestinationFile(destinationRoot, destinationPath string) (string, error) {
	if err := rejectSymlinkComponents(destinationRoot, destinationPath, false); err != nil {
		return "", err
	}
	file, err := os.Open(joinRelative(destinationRoot, destinationPath))
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("destination must be a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func rejectSymlinkComponents(root, relativePath string, allowMissing bool) error {
	current := root
	components := strings.Split(filepath.FromSlash(relativePath), string(filepath.Separator))
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && allowMissing {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component %q is not allowed", component)
		}
		if index < len(components)-1 && !info.IsDir() {
			return fmt.Errorf("path component %q is not a directory", component)
		}
	}
	return nil
}

func atomicWriteRelative(root, relativePath string, content []byte, canOverwrite bool) error {
	if err := ensureParentDirectories(root, relativePath); err != nil {
		return err
	}
	if err := rejectSymlinkComponents(root, relativePath, true); err != nil {
		return err
	}

	filename := joinRelative(root, relativePath)
	if info, err := os.Lstat(filename); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("destination symlink is not allowed")
		}
		if !canOverwrite {
			return errors.New("unmanaged destination already exists")
		}
		if !info.Mode().IsRegular() {
			return errors.New("destination must be a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(filename), ".coresync-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()

	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if canOverwrite {
		if err := os.Rename(temporaryName, filename); err != nil {
			return err
		}
	} else {
		// A hard link publishes the complete temporary file without a racy overwrite.
		if err := os.Link(temporaryName, filename); err != nil {
			if errors.Is(err, os.ErrExist) {
				return errors.New("unmanaged destination already exists")
			}
			return err
		}
		if err := os.Remove(temporaryName); err != nil {
			return err
		}
	}
	removeTemporary = false
	return nil
}

func ensureParentDirectories(root, relativePath string) error {
	parent := path.Dir(relativePath)
	if parent == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(filepath.FromSlash(parent), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component %q is not allowed", component)
		}
		if !info.IsDir() {
			return fmt.Errorf("path component %q is not a directory", component)
		}
	}
	return nil
}

func joinRelative(root, relativePath string) string {
	return filepath.Join(root, filepath.FromSlash(relativePath))
}
