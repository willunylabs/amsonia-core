// Package coresync copies a declared set of files and records deterministic provenance.
//
// Verify checks destination integrity against provenance expected to be reviewed
// and committed with the destination tree. It does not authenticate a maliciously
// modified provenance file.
package coresync

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const supportedSchemaVersion = 1

var (
	sourceCommitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	sha256Pattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	publishHookState    struct {
		sync.Mutex
		hook func(int, string) error
	}
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
	destinationPaths := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		destinationPaths = append(destinationPaths, entry.Destination)
	}
	if err := validatePathCollisions(destinationPaths, options.ProvenancePath); err != nil {
		return err
	}

	sourceRoot, err := openContainedRoot("source root", options.SourceRoot)
	if err != nil {
		return err
	}
	defer sourceRoot.Close()
	destinationRoot, err := openContainedRoot("destination root", options.DestinationRoot)
	if err != nil {
		return err
	}
	defer destinationRoot.Close()

	existing, provenanceSnapshot, err := loadProvenance(destinationRoot, options.ProvenancePath)
	if err != nil {
		return err
	}
	if provenanceSnapshot.exists && existing.SourceRepository != manifest.SourceRepository {
		return fmt.Errorf("existing provenance belongs to different source repository %q", existing.SourceRepository)
	}

	managed := make(map[string]ProvenanceEntry, len(existing.Entries))
	for _, entry := range existing.Entries {
		managed[entry.Destination] = entry
	}
	current := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		current[entry.Destination] = struct{}{}
	}
	for _, entry := range existing.Entries {
		if _, exists := current[entry.Destination]; !exists {
			return fmt.Errorf("previously managed destination %q was removed from manifest", entry.Destination)
		}
	}

	preparedEntries := make([]preparedEntry, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		content, digest, err := readSourceFile(sourceRoot, entry.Source)
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

	for index := range preparedEntries {
		prepared := &preparedEntries[index]
		previousEntry, isManaged := managed[prepared.entry.Destination]
		snapshot, err := inspectDestination(destinationRoot, prepared.entry.Destination)
		if err != nil {
			return err
		}
		if snapshot.exists && !isManaged {
			return fmt.Errorf("unmanaged destination %q already exists", prepared.entry.Destination)
		}
		if options.Check {
			if !snapshot.exists {
				return fmt.Errorf("content drift for destination %q: file is missing", prepared.entry.Destination)
			}
			if snapshot.digest != prepared.digest {
				return fmt.Errorf("content drift for destination %q", prepared.entry.Destination)
			}
			continue
		}
		if isManaged {
			if !snapshot.exists || snapshot.digest != previousEntry.SHA256 {
				return fmt.Errorf("managed destination drift for %q", prepared.entry.Destination)
			}
			prepared.previous = snapshot
		}
	}

	if options.Check {
		if !provenanceSnapshot.exists {
			return errors.New("provenance drift: provenance file is missing")
		}
		if !bytes.Equal(provenanceSnapshot.content, desiredBytes) {
			return errors.New("provenance drift")
		}
		return nil
	}

	publications := make([]publicationItem, 0, len(preparedEntries)+1)
	for _, prepared := range preparedEntries {
		publications = append(publications, publicationItem{
			relativePath: prepared.entry.Destination,
			desired:      prepared.content,
			desiredMode:  0o644,
			previous:     prepared.previous,
		})
	}
	publications = append(publications, publicationItem{
		relativePath: options.ProvenancePath,
		desired:      desiredBytes,
		desiredMode:  0o644,
		previous:     provenanceSnapshot,
		provenance:   true,
	})
	return publishTransaction(destinationRoot, publications)
}

// Verify checks destination integrity against committed provenance. It treats
// that committed provenance as the trust anchor and does not authenticate a
// maliciously modified provenance file.
func Verify(destinationRootPath, provenancePath string) error {
	if strings.TrimSpace(destinationRootPath) == "" {
		return errors.New("destination root is required")
	}
	if !isSafeRelativePath(provenancePath) {
		return fmt.Errorf("unsafe provenance path %q", provenancePath)
	}

	destinationRoot, err := openContainedRoot("destination root", destinationRootPath)
	if err != nil {
		return err
	}
	defer destinationRoot.Close()
	provenance, snapshot, err := loadProvenance(destinationRoot, provenancePath)
	if err != nil {
		return err
	}
	if !snapshot.exists {
		return errors.New("provenance file is missing")
	}
	for _, entry := range provenance.Entries {
		destination, err := inspectDestination(destinationRoot, entry.Destination)
		if err != nil {
			return fmt.Errorf("hash mismatch for destination %q: %w", entry.Destination, err)
		}
		if !destination.exists || destination.digest != entry.SHA256 {
			return fmt.Errorf("hash mismatch for destination %q", entry.Destination)
		}
	}
	return nil
}

type preparedEntry struct {
	entry    Entry
	content  []byte
	digest   string
	previous fileSnapshot
}

type fileSnapshot struct {
	exists  bool
	content []byte
	mode    os.FileMode
	digest  string
}

type publicationItem struct {
	relativePath string
	desired      []byte
	desiredMode  os.FileMode
	previous     fileSnapshot
	provenance   bool
	stagedPath   string
	backupPath   string
}

func validateOptions(options Options) error {
	if strings.TrimSpace(options.SourceRoot) == "" {
		return errors.New("source root is required")
	}
	if strings.TrimSpace(options.DestinationRoot) == "" {
		return errors.New("destination root is required")
	}
	if !isValidSourceCommit(options.SourceCommit) {
		return errors.New("source commit must be exactly 40 or 64 lowercase hexadecimal characters")
	}
	if !isSafeRelativePath(options.ProvenancePath) {
		return fmt.Errorf("unsafe provenance path %q", options.ProvenancePath)
	}
	return nil
}

func openContainedRoot(name, filename string) (*os.Root, error) {
	before, err := os.Lstat(filename)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", name, err)
	}
	if err := validateRootInfo(name, before); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(filename)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("inspect opened %s: %w", name, err)
	}
	after, err := os.Lstat(filename)
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("reinspect %s: %w", name, err)
	}
	if err := validateRootInfo(name, after); err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(before, opened) || !os.SameFile(after, opened) {
		root.Close()
		return nil, fmt.Errorf("%s changed while opening", name)
	}
	return root, nil
}

func validateRootInfo(name string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symlink", name)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be a directory", name)
	}
	return nil
}

func validatePathCollisions(destinations []string, provenancePath string) error {
	paths := make([]string, 0, len(destinations)+1)
	paths = append(paths, destinations...)
	paths = append(paths, provenancePath)
	pathSet := make(map[string]struct{}, len(paths))
	for _, relativePath := range paths {
		if _, exists := pathSet[relativePath]; exists {
			return fmt.Errorf("path collision at %q", relativePath)
		}
		pathSet[relativePath] = struct{}{}
	}
	for _, descendant := range paths {
		for ancestor := path.Dir(descendant); ancestor != "."; ancestor = path.Dir(ancestor) {
			if _, exists := pathSet[ancestor]; exists {
				return fmt.Errorf("path collision between %q and %q", ancestor, descendant)
			}
		}
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
	return decodeJSON(file, target)
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
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
	if !isValidSourceCommit(provenance.SourceCommit) {
		return errors.New("provenance source_commit must be exactly 40 or 64 lowercase hexadecimal characters")
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

func loadProvenance(destinationRoot *os.Root, provenancePath string) (Provenance, fileSnapshot, error) {
	snapshot, err := readRootRegularFile(destinationRoot, provenancePath, true, "provenance")
	if err != nil {
		return Provenance{}, fileSnapshot{}, err
	}
	if !snapshot.exists {
		return Provenance{}, snapshot, nil
	}
	var provenance Provenance
	if err := decodeJSON(bytes.NewReader(snapshot.content), &provenance); err != nil {
		return Provenance{}, fileSnapshot{}, fmt.Errorf("decode provenance: %w", err)
	}
	if err := validateProvenance(provenance); err != nil {
		return Provenance{}, fileSnapshot{}, err
	}
	destinations := make([]string, 0, len(provenance.Entries))
	for _, entry := range provenance.Entries {
		destinations = append(destinations, entry.Destination)
	}
	if err := validatePathCollisions(destinations, provenancePath); err != nil {
		return Provenance{}, fileSnapshot{}, fmt.Errorf("invalid provenance: %w", err)
	}
	return provenance, snapshot, nil
}

func marshalProvenance(provenance Provenance) ([]byte, error) {
	content, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal provenance: %w", err)
	}
	return append(content, '\n'), nil
}

func readSourceFile(sourceRoot *os.Root, sourcePath string) ([]byte, string, error) {
	snapshot, err := readRootRegularFile(sourceRoot, sourcePath, false, "source")
	if err != nil {
		return nil, "", err
	}
	return snapshot.content, snapshot.digest, nil
}

func inspectDestination(destinationRoot *os.Root, destinationPath string) (fileSnapshot, error) {
	return readRootRegularFile(destinationRoot, destinationPath, true, "destination")
}

func readRootRegularFile(root *os.Root, relativePath string, allowMissing bool, label string) (fileSnapshot, error) {
	if err := rejectSymlinkComponents(root, relativePath, allowMissing); err != nil {
		return fileSnapshot{}, fmt.Errorf("inspect %s %q: %w", label, relativePath, err)
	}
	before, err := root.Lstat(relativePath)
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		return fileSnapshot{}, nil
	}
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("inspect %s %q: %w", label, relativePath, err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return fileSnapshot{}, fmt.Errorf("inspect %s %q: symlink is not allowed", label, relativePath)
	}
	if !before.Mode().IsRegular() {
		return fileSnapshot{}, fmt.Errorf("%s %q must be a regular file", label, relativePath)
	}

	file, err := root.Open(relativePath)
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("open %s %q: %w", label, relativePath, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("inspect opened %s %q: %w", label, relativePath, err)
	}
	if !opened.Mode().IsRegular() {
		return fileSnapshot{}, fmt.Errorf("%s %q must be a regular file", label, relativePath)
	}
	if !os.SameFile(before, opened) {
		return fileSnapshot{}, fmt.Errorf("%s %q changed during inspection", label, relativePath)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("read %s %q: %w", label, relativePath, err)
	}
	after, err := root.Lstat(relativePath)
	if err != nil || !os.SameFile(before, after) {
		return fileSnapshot{}, fmt.Errorf("%s %q changed during inspection", label, relativePath)
	}
	digest := sha256.Sum256(content)
	return fileSnapshot{
		exists:  true,
		content: content,
		mode:    opened.Mode().Perm(),
		digest:  hex.EncodeToString(digest[:]),
	}, nil
}

func rejectSymlinkComponents(root *os.Root, relativePath string, allowMissing bool) error {
	current := ""
	components := strings.Split(relativePath, "/")
	for index, component := range components {
		current = path.Join(current, component)
		info, err := root.Lstat(current)
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

func publishTransaction(destinationRoot *os.Root, items []publicationItem) error {
	stagingDirectory, err := createStagingDirectory(destinationRoot)
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			_ = destinationRoot.RemoveAll(stagingDirectory)
		}
	}()

	for index := range items {
		items[index].stagedPath = path.Join(stagingDirectory, fmt.Sprintf("desired-%06d", index))
		if err := writeStagedFile(destinationRoot, items[index].stagedPath, items[index].desired, items[index].desiredMode); err != nil {
			return fmt.Errorf("stage %q: %w", items[index].relativePath, err)
		}
	}
	for index := range items {
		if !items[index].previous.exists {
			continue
		}
		items[index].backupPath = path.Join(stagingDirectory, fmt.Sprintf("backup-%06d", index))
		if err := writeStagedFile(destinationRoot, items[index].backupPath, items[index].previous.content, items[index].previous.mode); err != nil {
			return fmt.Errorf("stage backup for %q: %w", items[index].relativePath, err)
		}
	}

	createdDirectories, err := ensurePublicationParents(destinationRoot, items)
	if err != nil {
		cleanupErr := removeCreatedDirectories(destinationRoot, createdDirectories)
		return errors.Join(err, cleanupErr)
	}
	if err := validatePublicationState(destinationRoot, items); err != nil {
		cleanupErr := removeCreatedDirectories(destinationRoot, createdDirectories)
		return errors.Join(err, cleanupErr)
	}

	published := 0
	for index := range items {
		if err := validatePublicationItem(destinationRoot, items[index]); err != nil {
			return rollbackPublicationFailure(destinationRoot, items, published, createdDirectories, stagingDirectory, err, &cleanupStaging)
		}
		if items[index].previous.exists {
			err = destinationRoot.Rename(items[index].stagedPath, items[index].relativePath)
		} else {
			err = destinationRoot.Link(items[index].stagedPath, items[index].relativePath)
		}
		if err != nil {
			publicationErr := fmt.Errorf("publish %q: %w", items[index].relativePath, err)
			return rollbackPublicationFailure(destinationRoot, items, published, createdDirectories, stagingDirectory, publicationErr, &cleanupStaging)
		}
		published++
		if err := runPublishHook(index, items[index].relativePath); err != nil {
			publicationErr := fmt.Errorf("publish %q: %w", items[index].relativePath, err)
			return rollbackPublicationFailure(destinationRoot, items, published, createdDirectories, stagingDirectory, publicationErr, &cleanupStaging)
		}
	}

	if err := destinationRoot.RemoveAll(stagingDirectory); err != nil {
		return fmt.Errorf("remove staging directory: %w", err)
	}
	cleanupStaging = false
	return nil
}

func createStagingDirectory(root *os.Root) (string, error) {
	for range 100 {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", err
		}
		name := ".coresync-stage-" + hex.EncodeToString(random)
		if err := root.Mkdir(name, 0o700); err == nil {
			return name, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("could not allocate unique staging directory")
}

func writeStagedFile(root *os.Root, relativePath string, content []byte, mode os.FileMode) error {
	file, err := root.OpenFile(relativePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func ensurePublicationParents(root *os.Root, items []publicationItem) ([]string, error) {
	created := make([]string, 0)
	for _, item := range items {
		parent := path.Dir(item.relativePath)
		if parent == "." {
			continue
		}
		current := ""
		for _, component := range strings.Split(parent, "/") {
			current = path.Join(current, component)
			info, err := root.Lstat(current)
			if errors.Is(err, os.ErrNotExist) {
				if err := root.Mkdir(current, 0o755); err != nil {
					if !errors.Is(err, os.ErrExist) {
						return created, fmt.Errorf("create destination directory %q: %w", current, err)
					}
				} else {
					created = append(created, current)
				}
				info, err = root.Lstat(current)
			}
			if err != nil {
				return created, fmt.Errorf("inspect destination directory %q: %w", current, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return created, fmt.Errorf("symlink path component %q is not allowed", component)
			}
			if !info.IsDir() {
				return created, fmt.Errorf("path component %q is not a directory", component)
			}
		}
	}
	return created, nil
}

func validatePublicationState(root *os.Root, items []publicationItem) error {
	for _, item := range items {
		if err := validatePublicationItem(root, item); err != nil {
			return err
		}
	}
	return nil
}

func validatePublicationItem(root *os.Root, item publicationItem) error {
	snapshot, err := readRootRegularFile(root, item.relativePath, true, "publication target")
	if err != nil {
		return err
	}
	if !item.previous.exists {
		if !snapshot.exists {
			return nil
		}
		if item.provenance {
			return errors.New("provenance drift: provenance file appeared before publication")
		}
		return fmt.Errorf("unmanaged destination %q appeared before publication", item.relativePath)
	}
	if !snapshot.exists || !bytes.Equal(snapshot.content, item.previous.content) || snapshot.mode != item.previous.mode {
		if item.provenance {
			return errors.New("provenance drift before publication")
		}
		return fmt.Errorf("managed destination drift for %q before publication", item.relativePath)
	}
	return nil
}

func rollbackPublicationFailure(root *os.Root, items []publicationItem, published int, createdDirectories []string, stagingDirectory string, publicationErr error, cleanupStaging *bool) error {
	rollbackErr := rollbackPublished(root, items[:published])
	directoryErr := removeCreatedDirectories(root, createdDirectories)
	if rollbackErr != nil || directoryErr != nil {
		*cleanupStaging = false
		return errors.Join(
			publicationErr,
			fmt.Errorf("rollback from staging directory %q: %w", stagingDirectory, errors.Join(rollbackErr, directoryErr)),
		)
	}
	return publicationErr
}

func rollbackPublished(root *os.Root, published []publicationItem) error {
	var rollbackErrors []error
	for index := len(published) - 1; index >= 0; index-- {
		item := published[index]
		if item.previous.exists {
			if err := root.Rename(item.backupPath, item.relativePath); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore %q: %w", item.relativePath, err))
			}
			continue
		}
		if err := root.Remove(item.relativePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove %q: %w", item.relativePath, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func removeCreatedDirectories(root *os.Root, directories []string) error {
	var cleanupErrors []error
	for index := len(directories) - 1; index >= 0; index-- {
		if err := root.Remove(directories[index]); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove directory %q: %w", directories[index], err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func isValidSourceCommit(commit string) bool {
	return sourceCommitPattern.MatchString(commit)
}

func setPublishHookForTest(hook func(int, string) error) func() {
	publishHookState.Lock()
	previous := publishHookState.hook
	publishHookState.hook = hook
	publishHookState.Unlock()
	return func() {
		publishHookState.Lock()
		publishHookState.hook = previous
		publishHookState.Unlock()
	}
}

func runPublishHook(index int, relativePath string) error {
	publishHookState.Lock()
	defer publishHookState.Unlock()
	if publishHookState.hook == nil {
		return nil
	}
	return publishHookState.hook(index, relativePath)
}
