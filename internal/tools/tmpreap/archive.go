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

// drop is one reason the archive refused a file: how many, how big, and which.
// Each category carries its own paths because a count says a log was lost and a
// path says which one.
type drop struct {
	files int
	bytes int64
	paths []string
}

// add records one refused file. The path list is capped and the count is not,
// so the caller can disclose the difference.
func (d *drop) add(path string, size int64) {
	d.files++
	d.bytes += size
	if len(d.paths) < pathsListed {
		d.paths = append(d.paths, path)
	}
}

// archiveStats is what one archive pass took and, more importantly, what it did
// not. Every drop below is a file about to be deleted with no copy anywhere, so
// the caller PRINTS all three: large and binary were computed and read only by
// this package's tests for the whole of round 1, and unreadable was not
// computed at all through round 2, which is indistinguishable from not having
// the category (bd gqlc-osuz).
type archiveStats struct {
	files int
	bytes int64
	// binary is what the archive would not take at any size. Build caches and
	// object files are the bulk of a scratch filesystem's bytes and none of its
	// value, so this is a deliberate loss — but a disclosed one.
	binary drop
	// large is TEXT files dropped only for exceeding maxFileBytes. This is the
	// accidental loss, and the one the operator can do something about by
	// raising the limit and re-running.
	large drop
	// unreadable is files that are present and could not be read. os.RemoveAll
	// deletes them regardless: it needs write permission on the DIRECTORY, not
	// read permission on the file. A file that vanished between the walk and the
	// read is not counted here — see classifyFailure.
	unreadable drop
	// truncated is set when a limit was reached with entries still unread, so
	// the caller can refuse to delete over an incomplete record.
	truncated bool
}

// account records a non-OK verdict. A verdict with no case of its own lands in
// unreadable rather than nowhere: a file deleted with no copy, no count and no
// name is the defect this struct exists to end, and a silent default shipped it
// twice (bd gqlc-osuz rounds 1 and 2).
func (s *archiveStats) account(path string, info fs.FileInfo, verdict takeVerdict) {
	if verdict == takeVanished {
		return
	}
	// info is nil when the stat itself failed. The size of a file nothing can
	// read is not knowable; the count and the path are.
	var size int64
	if info != nil {
		size = info.Size()
	}
	switch verdict {
	case takeLarge:
		s.large.add(path, size)
	case takeBinary:
		s.binary.add(path, size)
	default:
		s.unreadable.add(path, size)
	}
}

// pathsListed bounds the named files per category. The report is read by a human
// deciding whether to raise -archive-max-file and re-run; a thousand paths is
// the same as none.
const pathsListed = 5

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
			if verdict != takeOK {
				stats.account(p, info, verdict)
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

// takeVerdict is why a file was or was not put in the archive. takeVanished is
// the only one the report stays silent about, and it is silent because there is
// nothing left to delete.
type takeVerdict string

const (
	takeOK         takeVerdict = "ok"
	takeLarge      takeVerdict = "large"
	takeBinary     takeVerdict = "binary"
	takeUnreadable takeVerdict = "unreadable"
	takeVanished   takeVerdict = "vanished"
)

func takeFile(path string, d fs.DirEntry, maxFileBytes int64) ([]byte, fs.FileInfo, takeVerdict) {
	info, err := d.Info()
	if err != nil {
		return nil, nil, classifyFailure(err)
	}
	if info.Size() > maxFileBytes {
		// The head is read even though the file is not going in the archive,
		// because the two oversize cases are different losses and reporting
		// them as one number reads as neither: an oversize binary is a build
		// artefact nobody wanted a copy of, an oversize text file is the agent
		// log this archive exists for. 8 KiB, not the whole file — the size
		// limit is here to bound memory and the check must not undo it.
		return nil, info, classifyHead(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, info, classifyFailure(err)
	}
	if !isText(data) {
		return nil, info, takeBinary
	}
	return data, info, takeOK
}

// classifyFailure separates the file that is gone from the file that is there
// and cannot be read.
//
// ENOENT is the race: an exiting agent removed the file between the walk and the
// read, so there is nothing left to delete and nothing to report. EACCES is not
// a race — it is another uid's file inside a directory this uid owns, which is
// the ordinary state of a shared /tmp: 7,071 of the 523,429 files on the one
// this tool was measured against belong to another uid. Nor is EIO. Those files
// are present, unarchivable, and deleted all the same, which makes them exactly
// the files an operator wants named before the deletion runs.
func classifyFailure(err error) takeVerdict {
	if errors.Is(err, fs.ErrNotExist) {
		return takeVanished
	}
	return takeUnreadable
}

// classifyHead decides an oversize file from its head. A head it cannot read
// yields a failure verdict rather than a classification, so such a file is
// neither claimed as recoverable text nor written off as a build artefact.
func classifyHead(path string) takeVerdict {
	f, err := os.Open(path)
	if err != nil {
		return classifyFailure(err)
	}
	defer f.Close() //nolint:errcheck // read-only, and the verdict is already decided.
	return classifyReader(f)
}

// classifyReader is the half of classifyHead that a fixture can fail on demand.
// A file that opens and then fails to read has no path through the filesystem
// that a test can construct portably, and it is the half where a wrong answer
// over-claims: a head of zero bytes holds no NUL, so treating an empty read as
// a classification files an unread agent log under "text, recoverable".
func classifyReader(r io.Reader) takeVerdict {
	var head [textHeadBytes]byte
	n, err := io.ReadFull(r, head[:])
	// A short read is still a classifiable head; only a read that yielded
	// nothing is a failure. The file was oversize a moment ago, so an empty read
	// here means the read failed or the file was truncated under us.
	if n == 0 && err != nil {
		return classifyFailure(err)
	}
	if isText(head[:n]) {
		return takeLarge
	}
	return takeBinary
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
//
// It is kept rather than tightened, and the decision is measured rather than
// assumed. One NUL early in an otherwise-plain transcript would misclassify the
// artefact this archive exists for, so review asked how often that happens (bd
// gqlc-osuz round 2). Read-only over the 523,429 files on the live /tmp: 9,765
// heads say binary, every one of them holds at least one further NUL in its
// body, and the seven whose bodies are >=95% printable are two Go build-cache
// blobs (2.75M and 1.88M NULs), two tars (NUL block padding) and three
// golangci-lint cache blobs. Nothing there resembles a transcript with a stray
// NUL, and no head that says text acquires a NUL later. Trading a measured-zero
// misfire rate for an unmeasured one is the worse bet. What changed instead is
// the consequence: reportUnarchived now names binary drops, so a misfire is
// visible on stdout rather than folded into a count.
func isText(data []byte) bool {
	head := data
	if len(head) > textHeadBytes {
		head = head[:textHeadBytes]
	}
	return bytes.IndexByte(head, 0) < 0
}
