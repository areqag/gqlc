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

// archiveStats is what one archive pass took and, more importantly, what it did
// not. Every field except files/bytes counts an artefact that is about to be
// deleted with no copy anywhere, so the caller PRINTS them: large and binary
// were computed and read only by this package's tests for the whole of round 1,
// which is indistinguishable from not computing them (bd gqlc-osuz).
type archiveStats struct {
	files int
	bytes int64
	// binary counts what the archive would not take at any size. Build caches
	// and object files are the bulk of a scratch filesystem's bytes and none of
	// its value, so this is a deliberate loss — but a disclosed one.
	binary      int
	binaryBytes int64
	// large counts TEXT files dropped only for exceeding maxFileBytes. This is
	// the accidental loss, and the one the operator can do something about by
	// raising the limit and re-running.
	large      int
	largeBytes int64
	// largePaths names the first largePathsListed of them: a count says a log
	// was lost, a path says which.
	largePaths []string
	// truncated is set when a limit was reached with entries still unread, so
	// the caller can refuse to delete over an incomplete record.
	truncated bool
}

// largePathsListed bounds the named oversize files. The report is read by a
// human deciding whether to raise -archive-max-file and re-run; a thousand
// paths is the same as none.
const largePathsListed = 5

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
				stats.largeBytes += info.Size()
				if len(stats.largePaths) < largePathsListed {
					stats.largePaths = append(stats.largePaths, p)
				}
				return nil
			case takeBinary:
				stats.binary++
				stats.binaryBytes += info.Size()
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
		// The head is read even though the file is not going in the archive,
		// because the two oversize cases are different losses and reporting
		// them as one number reads as neither: an oversize binary is a build
		// artefact nobody wanted a copy of, an oversize text file is the agent
		// log this archive exists for. 8 KiB, not the whole file — the size
		// limit is here to bound memory and the check must not undo it.
		if headIsText(path) {
			return nil, info, takeLarge
		}
		return nil, info, takeBinary
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

// headIsText applies isText to the head of a file too large to read whole. An
// unreadable head reports binary, so a file this cannot classify is not claimed
// as recoverable text.
func headIsText(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only, and the verdict is already decided.
	var head [textHeadBytes]byte
	n, err := io.ReadFull(f, head[:])
	if n == 0 && err != nil {
		return false
	}
	return isText(head[:n])
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

// textHeadBytes is how much of a file the text heuristic looks at.
const textHeadBytes = 8 << 10

// isText is the classic NUL heuristic over the head of a file. Agent scratch is
// logs, diffs and notes; the binaries beside them are build caches and object
// files, which are the bulk of the bytes and none of the value.
func isText(data []byte) bool {
	head := data
	if len(head) > textHeadBytes {
		head = head[:textHeadBytes]
	}
	return bytes.IndexByte(head, 0) < 0
}
