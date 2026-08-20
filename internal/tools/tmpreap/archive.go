package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// archiveLimits bounds what the archive will take, so a single enormous scratch
// directory cannot turn a reclamation into an out-of-space failure somewhere
// else.
type archiveLimits struct {
	maxFileBytes  int64
	maxTotalBytes int64
}

type archiveStats struct {
	files  int
	bytes  int64
	binary int
	large  int
	// truncated is set when a limit was reached with entries still unread, so
	// the caller can refuse to delete over an incomplete record.
	truncated bool
}

// archiveEntries writes every text artefact under the given entries to a
// gzipped tar at dest.
//
// The point is that a reap is reversible in the only way that matters: 690 MiB
// of agent logs compressed to 43 MiB in the manual remediation this tool
// replaces (bd gqlc-osuz), which is cheap enough to take unconditionally.
//
// Registered worktrees are measured but not archived. A worktree only reaches
// this function after `git status --porcelain` came back empty, which means no
// modified and no untracked file: everything in it is either committed, and
// therefore in the object store, or ignored build output.
func archiveEntries(dest, root string, entries []entry, lim archiveLimits) (archiveStats, error) {
	if err := destOutsideRoot(dest, root); err != nil {
		return archiveStats{}, err
	}
	if lim.maxTotalBytes <= 0 || lim.maxFileBytes <= 0 {
		return archiveStats{}, fmt.Errorf("archive limits must be positive, got max-file=%d max-total=%d", lim.maxFileBytes, lim.maxTotalBytes)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return archiveStats{}, err
	}
	f, err := os.Create(dest)
	if err != nil {
		return archiveStats{}, err
	}
	stats, writeErr := writeArchive(f, root, entries, lim)
	return stats, errors.Join(writeErr, f.Close())
}

func writeArchive(w io.Writer, root string, entries []entry, lim archiveLimits) (stats archiveStats, err error) {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	defer func() {
		err = errors.Join(err, tw.Close(), gz.Close())
	}()

	for _, e := range entries {
		if e.kind == kindWorktree {
			continue
		}
		if stats.truncated {
			break
		}
		walkErr := filepath.WalkDir(e.path, func(p string, d fs.DirEntry, walkErr error) error {
			// An unreadable or vanished child is skipped rather than fatal: the
			// archive is insurance, and abandoning the other 99% of it because
			// one file went away mid-walk is the worse trade.
			if skip := walkErr != nil || d == nil || !d.Type().IsRegular(); skip {
				return nil
			}
			if stats.bytes >= lim.maxTotalBytes {
				stats.truncated = true
				return filepath.SkipAll
			}
			data, info, verdict := takeFile(p, d, lim.maxFileBytes)
			switch verdict {
			case takeOK:
			case takeLarge:
				stats.large++
				return nil
			case takeBinary:
				stats.binary++
				return nil
			default:
				return nil
			}
			// Unlike a vanished file, a failure here is structural — it means the
			// walked path is not under root at all — so it would repeat for every
			// entry and yield an archive that is silently empty.
			name, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return fmt.Errorf("archive name for %s under %s: %w", p, root, relErr)
			}
			hdr := &tar.Header{
				Name:    filepath.ToSlash(name),
				Mode:    int64(info.Mode().Perm()),
				Size:    int64(len(data)),
				ModTime: info.ModTime(),
			}
			if hdrErr := tw.WriteHeader(hdr); hdrErr != nil {
				return hdrErr
			}
			if _, writeErr := tw.Write(data); writeErr != nil {
				return writeErr
			}
			stats.files++
			stats.bytes += int64(len(data))
			return nil
		})
		if walkErr != nil {
			return stats, walkErr
		}
	}
	return stats, nil
}

// takeVerdict is why a file was or was not put in the archive. The unreadable
// case is deliberately silent in the stats: it is the racy one — a scratch file
// an exiting agent removed between the walk and the read — and counting it would
// put noise where the two decisions worth reading are.
type takeVerdict string

const (
	takeOK         takeVerdict = "ok"
	takeLarge      takeVerdict = "large"
	takeBinary     takeVerdict = "binary"
	takeUnreadable takeVerdict = "unreadable"
)

func takeFile(path string, d fs.DirEntry, maxFileBytes int64) ([]byte, fs.FileInfo, takeVerdict) {
	info, err := d.Info()
	if err != nil {
		return nil, nil, takeUnreadable
	}
	if info.Size() > maxFileBytes {
		return nil, info, takeLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, info, takeUnreadable
	}
	if !isText(data) {
		return nil, info, takeBinary
	}
	return data, info, takeOK
}

// destOutsideRoot refuses an archive written into the tree it is insurance
// against. The archive is created before the deletion runs, so a dest under root
// would be deleted by the very pass it exists to make reversible — and it would
// also be counted as reclaimed space that never came back.
func destOutsideRoot(dest, root string) error {
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if absDest == absRoot || strings.HasPrefix(absDest, absRoot+string(filepath.Separator)) {
		return fmt.Errorf("archive destination %s is inside the scan root %s, so the reap would delete "+
			"the record of what it deleted", absDest, absRoot)
	}
	return nil
}

// isText is the classic NUL heuristic over the head of a file. Agent scratch is
// logs, diffs and notes; the binaries beside them are build caches and object
// files, which are the bulk of the bytes and none of the value.
func isText(data []byte) bool {
	head := data
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	return bytes.IndexByte(head, 0) < 0
}
