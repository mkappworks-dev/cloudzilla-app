// Package backup writes and reads the single-tar archive behind
// `cz-admin backup` and `restore`.
package backup

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

const FormatVersion = 1

const (
	manifestName = "cloudzilla-backup.json"
	dumpName     = "database.pgdump"
	reposDir     = "git-repos"
	storageDir   = "storage"
	hostKeyName  = "ssh_host_key"

	// The manifest comes first in the tar but needs counts only known at the
	// end, so it is written into a fixed slot and patched in place.
	manifestSlot = 4096
	// A tar header block; the manifest's data starts one block into the file.
	tarBlock = 512
)

// Section names, as keys of Manifest.Sections.
const (
	SectionManifest = "manifest"
	SectionDatabase = "database"
	SectionGitRepos = "git-repos"
	SectionStorage  = "storage"
	SectionHostKey  = "ssh_host_key"
)

type Section struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// ObjectStorage records a backend whose data the archive does not hold.
type ObjectStorage struct {
	Backend string `json:"backend"`
	Bucket  string `json:"bucket,omitempty"`
}

type Manifest struct {
	FormatVersion     int                `json:"format_version"`
	CloudzillaVersion string             `json:"cloudzilla_version"`
	CreatedAt         time.Time          `json:"created_at"`
	Migration         string             `json:"migration"`
	PgDumpVersion     string             `json:"pg_dump_version"`
	Sections          map[string]Section `json:"sections"`
	ObjectStorage     *ObjectStorage     `json:"object_storage,omitempty"`
}

// slot is the manifest as JSON padded with spaces to exactly manifestSlot
// bytes, which keeps it valid JSON.
func (m Manifest) slot() ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b) > manifestSlot {
		return nil, fmt.Errorf("manifest is %d bytes, over its %d byte slot", len(b), manifestSlot)
	}
	return append(b, bytes.Repeat([]byte{' '}, manifestSlot-len(b))...), nil
}

func decodeManifest(r io.Reader) (*Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		return nil, fmt.Errorf("read %s: %w", manifestName, err)
	}
	if m.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("backup has format version %d; this binary reads version %d", m.FormatVersion, FormatVersion)
	}
	return &m, nil
}

// checkEntry validates a tar entry before anything is extracted. It returns
// the section the entry belongs to and, for git-repos, its path below the
// repos root. Only regular files and directories are accepted, and only at
// clean, relative, slash-separated paths.
func checkEntry(name string, typ byte) (section, rel string, err error) {
	if typ != tar.TypeReg && typ != tar.TypeDir {
		return "", "", fmt.Errorf("entry %q has unsupported type %q: only regular files and directories are restored", name, typ)
	}
	clean := strings.TrimSuffix(name, "/")
	if clean == "" || strings.ContainsRune(clean, 0) || path.IsAbs(clean) || path.Clean(clean) != clean || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "", fmt.Errorf("entry %q has an unsafe path", name)
	}
	isDir := typ == tar.TypeDir
	switch {
	case clean == manifestName && !isDir:
		return SectionManifest, "", nil
	case clean == dumpName && !isDir:
		return SectionDatabase, "", nil
	case clean == hostKeyName && !isDir:
		return SectionHostKey, "", nil
	case clean == reposDir && isDir:
		return SectionGitRepos, "", nil
	case clean == storageDir && isDir:
		return SectionStorage, "", nil
	case strings.HasPrefix(clean, reposDir+"/"):
		return SectionGitRepos, strings.TrimPrefix(clean, reposDir+"/"), nil
	case strings.HasPrefix(clean, storageDir+"/"):
		return SectionStorage, strings.TrimPrefix(clean, storageDir+"/"), nil
	}
	if clean == manifestName || clean == dumpName || clean == hostKeyName || clean == reposDir || clean == storageDir {
		return "", "", fmt.Errorf("entry %q has unsupported type %q", name, typ)
	}
	return "", "", fmt.Errorf("unexpected entry %q in backup", name)
}
