// Package coresync copies a declared set of files and records deterministic provenance.
//
// Verify checks destination integrity against provenance expected to be reviewed
// and committed with the destination tree. It does not authenticate a maliciously
// modified provenance file.
//
// Sync assumes its source and destination roots are dedicated, exclusively
// controlled worktrees. Hostile concurrent writers inside those roots are outside
// the trust model.
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
		hook func(publishHookPhase, int, string) error
	}
)

type publishHookPhase uint8

const (
	publishHookAfterValidation publishHookPhase = iota + 1
	publishHookBeforeManagedPublication
	publishHookAfterPublication
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

// Sync copies all manifest entries and writes deterministic provenance. Managed
// files are atomically replaced with same-parent renames on supported Unix
// worktree filesystems. Sync assumes dedicated, exclusively controlled worktrees;
// hostile concurrent writers inside either root are outside the trust model.
// It rolls back publication errors returned within the process, but it is not
// process-crash recovery. Crash-consistent or versioned generations are outside
// the scope of this local reviewed-worktree tool.
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
	info    os.FileInfo
}

type publicationItem struct {
	relativePath string
	basename     string
	parent       *stableDirectory
	desired      []byte
	desiredMode  os.FileMode
	previous     fileSnapshot
	provenance   bool
	stagedName   string
	stagedInfo   os.FileInfo
	stagedExists bool
	backupName   string
	backupInfo   os.FileInfo
	backupExists bool
	published    bool
}

type stableDirectory struct {
	root         *os.Root
	parent       *stableDirectory
	name         string
	relativePath string
	info         os.FileInfo
	created      bool
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
		info:    opened,
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
	directories, err := openStablePublicationParents(destinationRoot, items)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = closeStableDirectories(directories, false)
		}
	}()

	for index := range items {
		items[index].stagedName, items[index].stagedInfo, err = createStagedFile(items[index].parent.root, items[index].desired, items[index].desiredMode)
		if err != nil {
			cleanupErr := removePublicationArtifacts(items)
			closeErr := closeStableDirectories(directories, cleanupErr == nil)
			closed = true
			return errors.Join(fmt.Errorf("stage %q: %w", items[index].relativePath, err), cleanupErr, closeErr)
		}
		items[index].stagedExists = true
	}

	for index := range items {
		if err := publishStableItem(directories, &items[index], index); err != nil {
			result := rollbackStableTransaction(items, directories, err)
			closed = true
			return result
		}
	}
	if err := validateStableDirectories(directories); err != nil {
		result := rollbackStableTransaction(items, directories, err)
		closed = true
		return result
	}
	if err := validatePublishedTransaction(items); err != nil {
		result := rollbackStableTransaction(items, directories, err)
		closed = true
		return result
	}
	cleanupErr := removePublicationArtifacts(items)
	closeErr := closeStableDirectories(directories, false)
	closed = true
	return errors.Join(cleanupErr, closeErr)
}

func openStablePublicationParents(destinationRoot *os.Root, items []publicationItem) ([]*stableDirectory, error) {
	rootInfo, err := destinationRoot.Stat(".")
	if err != nil {
		return nil, fmt.Errorf("inspect destination root: %w", err)
	}
	rootDirectory := &stableDirectory{root: destinationRoot, relativePath: ".", info: rootInfo}
	directories := []*stableDirectory{rootDirectory}
	byPath := map[string]*stableDirectory{".": rootDirectory}

	for index := range items {
		parentPath := path.Dir(items[index].relativePath)
		current := rootDirectory
		currentPath := ""
		if parentPath != "." {
			for _, component := range strings.Split(parentPath, "/") {
				currentPath = path.Join(currentPath, component)
				if existing := byPath[currentPath]; existing != nil {
					current = existing
					continue
				}
				directory, openErr := openStableDirectoryComponent(current, component, currentPath)
				if openErr != nil {
					closeErr := closeStableDirectories(directories, true)
					return nil, errors.Join(openErr, closeErr)
				}
				directories = append(directories, directory)
				byPath[currentPath] = directory
				current = directory
			}
		}
		items[index].parent = current
		items[index].basename = path.Base(items[index].relativePath)
	}
	if err := validateStableDirectories(directories); err != nil {
		closeErr := closeStableDirectories(directories, true)
		return nil, errors.Join(err, closeErr)
	}
	return directories, nil
}

func openStableDirectoryComponent(parent *stableDirectory, name, relativePath string) (*stableDirectory, error) {
	info, err := parent.root.Lstat(name)
	created := false
	if errors.Is(err, os.ErrNotExist) {
		if err := parent.root.Mkdir(name, 0o755); err != nil {
			return nil, fmt.Errorf("create destination directory %q: %w", relativePath, err)
		}
		created = true
		info, err = parent.root.Lstat(name)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect destination directory %q: %w", relativePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlink path component %q is not allowed", name)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path component %q is not a directory", name)
	}
	root, err := parent.root.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("open destination directory %q: %w", relativePath, err)
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("inspect opened destination directory %q: %w", relativePath, err)
	}
	after, err := parent.root.Lstat(name)
	if err != nil || !os.SameFile(info, opened) || !os.SameFile(after, opened) {
		root.Close()
		return nil, fmt.Errorf("destination parent changed while opening %q", relativePath)
	}
	return &stableDirectory{
		root:         root,
		parent:       parent,
		name:         name,
		relativePath: relativePath,
		info:         opened,
		created:      created,
	}, nil
}

func validateStableDirectories(directories []*stableDirectory) error {
	for _, directory := range directories[1:] {
		info, err := directory.parent.root.Lstat(directory.name)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(info, directory.info) {
			return fmt.Errorf("destination parent changed at %q", directory.relativePath)
		}
	}
	return nil
}

func closeStableDirectories(directories []*stableDirectory, removeCreated bool) error {
	var closeErrors []error
	for index := len(directories) - 1; index >= 1; index-- {
		directory := directories[index]
		if err := directory.root.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close destination directory %q: %w", directory.relativePath, err))
		}
		if !removeCreated || !directory.created {
			continue
		}
		info, err := directory.parent.root.Lstat(directory.name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !os.SameFile(info, directory.info) {
			closeErrors = append(closeErrors, fmt.Errorf("destination parent changed before removing %q", directory.relativePath))
			continue
		}
		if err := directory.parent.root.Remove(directory.name); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("remove destination directory %q: %w", directory.relativePath, err))
		}
	}
	return errors.Join(closeErrors...)
}

func createStagedFile(parent *os.Root, content []byte, mode os.FileMode) (string, os.FileInfo, error) {
	for range 100 {
		name, err := randomArtifactName(".coresync-stage-")
		if err != nil {
			return "", nil, err
		}
		file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		createdInfo, err := file.Stat()
		if err != nil {
			file.Close()
			return "", nil, err
		}
		cleanupFailedStage := func() {
			file.Close()
			_ = removeArtifactIfUnchanged(parent, name, createdInfo)
		}
		if err := file.Chmod(mode.Perm()); err != nil {
			cleanupFailedStage()
			return "", nil, err
		}
		if _, err := file.Write(content); err != nil {
			cleanupFailedStage()
			return "", nil, err
		}
		if err := file.Sync(); err != nil {
			cleanupFailedStage()
			return "", nil, err
		}
		info, err := file.Stat()
		if err != nil {
			cleanupFailedStage()
			return "", nil, err
		}
		if err := file.Close(); err != nil {
			_ = removeArtifactIfUnchanged(parent, name, createdInfo)
			return "", nil, err
		}
		return name, info, nil
	}
	return "", nil, errors.New("could not allocate unique staged filename")
}

func publishStableItem(directories []*stableDirectory, item *publicationItem, index int) error {
	if err := validateStableDirectories(directories); err != nil {
		return err
	}
	if err := validateStablePublicationTarget(*item); err != nil {
		return err
	}
	if err := runPublishHook(publishHookAfterValidation, index, item.relativePath); err != nil {
		return fmt.Errorf("after validation for %q: %w", item.relativePath, err)
	}
	if err := validateStableDirectories(directories); err != nil {
		return err
	}
	if item.previous.exists {
		if err := createManagedBackup(item); err != nil {
			return err
		}
		if err := validateStableDirectories(directories); err != nil {
			return err
		}
		if err := validateBackupArtifact(*item); err != nil {
			return err
		}
	}
	if err := validateStagedArtifact(*item); err != nil {
		return err
	}
	if item.previous.exists {
		if err := runPublishHook(publishHookBeforeManagedPublication, index, item.relativePath); err != nil {
			return fmt.Errorf("before managed publication for %q: %w", item.relativePath, err)
		}
		if err := item.parent.root.Rename(item.stagedName, item.basename); err != nil {
			return fmt.Errorf("atomically publish managed destination %q: %w", item.relativePath, err)
		}
		item.stagedExists = false
	} else if err := item.parent.root.Link(item.stagedName, item.basename); err != nil {
		return fmt.Errorf("publish new destination %q without clobbering: %w", item.relativePath, err)
	}
	item.published = true
	if err := runPublishHook(publishHookAfterPublication, index, item.relativePath); err != nil {
		return fmt.Errorf("after publication for %q: %w", item.relativePath, err)
	}
	return nil
}

func validateStablePublicationTarget(item publicationItem) error {
	snapshot, err := readRootRegularFile(item.parent.root, item.basename, true, "publication target")
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

func createManagedBackup(item *publicationItem) error {
	backupName, err := uniqueUnusedArtifactName(item.parent.root, ".coresync-backup-")
	if err != nil {
		return fmt.Errorf("allocate backup for %q: %w", item.relativePath, err)
	}
	item.backupName = backupName
	if err := item.parent.root.Link(item.basename, item.backupName); err != nil {
		return fmt.Errorf("create live backup for %q: %w", item.relativePath, err)
	}
	item.backupExists = true
	backup, err := readRootRegularFile(item.parent.root, item.backupName, false, "backup")
	if err == nil {
		item.backupInfo = backup.info
		liveInfo, liveErr := item.parent.root.Lstat(item.basename)
		if liveErr != nil {
			err = liveErr
		} else if !os.SameFile(liveInfo, backup.info) || !bytes.Equal(backup.content, item.previous.content) || backup.mode != item.previous.mode {
			err = fmt.Errorf("managed destination drift for %q after backup", item.relativePath)
		}
	}
	if err != nil {
		cleanupErr := discardManagedBackup(item)
		return errors.Join(err, wrapRollbackError(cleanupErr))
	}
	return nil
}

func discardManagedBackup(item *publicationItem) error {
	if !item.backupExists {
		return nil
	}
	if item.backupInfo == nil {
		info, err := item.parent.root.Lstat(item.backupName)
		if err != nil {
			return fmt.Errorf("inspect backup recovery artifact %q: %w", item.backupName, err)
		}
		item.backupInfo = info
	}
	if err := removeArtifactIfUnchanged(item.parent.root, item.backupName, item.backupInfo); err != nil {
		return fmt.Errorf("remove backup recovery artifact %q: %w", item.backupName, err)
	}
	item.backupExists = false
	item.backupName = ""
	item.backupInfo = nil
	return nil
}

func uniqueUnusedArtifactName(parent *os.Root, prefix string) (string, error) {
	for range 100 {
		name, err := randomArtifactName(prefix)
		if err != nil {
			return "", err
		}
		_, err = parent.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not allocate unique artifact filename")
}

func randomArtifactName(prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random), nil
}

func validateStagedArtifact(item publicationItem) error {
	if !item.stagedExists {
		return nil
	}
	return validateArtifact(item.parent.root, item.stagedName, item.stagedInfo, item.desired, item.desiredMode, "staged file")
}

func validateBackupArtifact(item publicationItem) error {
	if !item.backupExists {
		return nil
	}
	return validateArtifact(item.parent.root, item.backupName, item.backupInfo, item.previous.content, item.previous.mode, "backup")
}

func validateArtifact(parent *os.Root, name string, expectedInfo os.FileInfo, content []byte, mode os.FileMode, label string) error {
	snapshot, err := readRootRegularFile(parent, name, false, label)
	if err != nil {
		return err
	}
	if !os.SameFile(snapshot.info, expectedInfo) || !bytes.Equal(snapshot.content, content) || snapshot.mode != mode.Perm() {
		return fmt.Errorf("%s %q changed", label, name)
	}
	return nil
}

func validatePublishedTransaction(items []publicationItem) error {
	for _, item := range items {
		if !item.published {
			continue
		}
		if err := validatePublishedTarget(item); err != nil {
			return err
		}
		if err := validateStagedArtifact(item); err != nil {
			return err
		}
		if err := validateBackupArtifact(item); err != nil {
			return err
		}
	}
	return nil
}

func validatePublishedTarget(item publicationItem) error {
	snapshot, err := readRootRegularFile(item.parent.root, item.basename, false, "published target")
	if err != nil {
		return err
	}
	if !os.SameFile(snapshot.info, item.stagedInfo) || !bytes.Equal(snapshot.content, item.desired) || snapshot.mode != item.desiredMode.Perm() {
		return fmt.Errorf("published target %q changed", item.relativePath)
	}
	return nil
}

func rollbackStableTransaction(items []publicationItem, directories []*stableDirectory, publicationErr error) error {
	rollbackErr := rollbackStableItems(items)
	if rollbackErr != nil {
		closeErr := closeStableDirectories(directories, false)
		return errors.Join(publicationErr, fmt.Errorf("rollback: %w", rollbackErr), closeErr)
	}
	cleanupErr := removePublicationArtifacts(items)
	closeErr := closeStableDirectories(directories, cleanupErr == nil)
	return errors.Join(publicationErr, wrapRollbackError(cleanupErr), closeErr)
}

func rollbackStableItems(items []publicationItem) error {
	var rollbackErrors []error
	for index := len(items) - 1; index >= 0; index-- {
		item := &items[index]
		if item.previous.exists {
			if item.published {
				if err := restoreManagedPublicationIfSafe(item); err != nil {
					rollbackErrors = append(rollbackErrors, err)
				}
				continue
			}
			if item.backupExists {
				if err := discardUnpublishedManagedBackupIfSafe(item); err != nil {
					rollbackErrors = append(rollbackErrors, err)
				}
			}
			continue
		}
		if item.published {
			if err := removeNewPublishedTargetIfSafe(item); err != nil {
				rollbackErrors = append(rollbackErrors, err)
				continue
			}
			item.published = false
		}
	}
	return errors.Join(rollbackErrors...)
}

func removeNewPublishedTargetIfSafe(item *publicationItem) error {
	snapshot, err := readRootRegularFile(item.parent.root, item.basename, true, "rollback target")
	if err != nil {
		return err
	}
	if !snapshot.exists {
		return nil
	}
	if !os.SameFile(snapshot.info, item.stagedInfo) || !bytes.Equal(snapshot.content, item.desired) || snapshot.mode != item.desiredMode.Perm() {
		return fmt.Errorf("unsafe rollback for %q: concurrent target retained", item.relativePath)
	}
	if err := item.parent.root.Remove(item.basename); err != nil {
		return fmt.Errorf("remove published target %q during rollback: %w", item.relativePath, err)
	}
	return nil
}

func restoreManagedPublicationIfSafe(item *publicationItem) error {
	if !item.backupExists {
		return fmt.Errorf("cannot rollback managed destination %q: backup recovery artifact is missing", item.relativePath)
	}
	if err := validateBackupArtifact(*item); err != nil {
		return fmt.Errorf("backup recovery artifact %q is unsafe: %w", item.backupName, err)
	}
	live, err := readRootRegularFile(item.parent.root, item.basename, true, "rollback target")
	if err != nil {
		return err
	}
	if !live.exists || !os.SameFile(live.info, item.stagedInfo) || !bytes.Equal(live.content, item.desired) || live.mode != item.desiredMode.Perm() {
		return fmt.Errorf("unsafe rollback for %q: concurrent target retained with backup recovery artifact %q", item.relativePath, item.backupName)
	}
	if err := item.parent.root.Rename(item.backupName, item.basename); err != nil {
		return fmt.Errorf("atomically restore backup recovery artifact %q for %q: %w", item.backupName, item.relativePath, err)
	}
	item.backupExists = false
	item.backupName = ""
	item.backupInfo = nil
	item.published = false
	return nil
}

func discardUnpublishedManagedBackupIfSafe(item *publicationItem) error {
	if err := validateBackupArtifact(*item); err != nil {
		return fmt.Errorf("backup recovery artifact %q is unsafe: %w", item.backupName, err)
	}
	live, err := readRootRegularFile(item.parent.root, item.basename, false, "managed destination")
	if err != nil {
		return err
	}
	if !os.SameFile(live.info, item.backupInfo) || !bytes.Equal(live.content, item.previous.content) || live.mode != item.previous.mode {
		return fmt.Errorf("cannot discard backup recovery artifact %q for %q: live target changed", item.backupName, item.relativePath)
	}
	return discardManagedBackup(item)
}

func removePublicationArtifacts(items []publicationItem) error {
	var cleanupErrors []error
	for index := range items {
		if items[index].backupExists {
			if err := removeArtifactIfUnchanged(items[index].parent.root, items[index].backupName, items[index].backupInfo); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove backup recovery artifact %q: %w", items[index].backupName, err))
			} else {
				items[index].backupExists = false
			}
		}
		if items[index].stagedExists {
			if err := removeArtifactIfUnchanged(items[index].parent.root, items[index].stagedName, items[index].stagedInfo); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove staged recovery artifact %q: %w", items[index].stagedName, err))
			} else {
				items[index].stagedExists = false
			}
		}
	}
	return errors.Join(cleanupErrors...)
}

func removeArtifactIfUnchanged(parent *os.Root, name string, expected os.FileInfo) error {
	if name == "" || expected == nil {
		return nil
	}
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(info, expected) {
		return errors.New("artifact changed; retained")
	}
	return parent.Remove(name)
}

func wrapRollbackError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("rollback: %w", err)
}

func isValidSourceCommit(commit string) bool {
	return sourceCommitPattern.MatchString(commit)
}

func setPublishHookForTest(hook func(publishHookPhase, int, string) error) func() {
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

func runPublishHook(phase publishHookPhase, index int, relativePath string) error {
	publishHookState.Lock()
	hook := publishHookState.hook
	publishHookState.Unlock()
	if hook == nil {
		return nil
	}
	return hook(phase, index, relativePath)
}
