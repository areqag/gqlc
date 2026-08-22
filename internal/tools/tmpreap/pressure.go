package main

import (
	"fmt"
	"syscall"
)

// pressure is how full a filesystem is, in both currencies.
//
// Both, because they run out independently and the one that ran out here was
// inodes: /tmp reached 1048576/1048576 inodes with 4.3G of bytes still free
// (bd gqlc-osuz). `df -h` was green throughout, which is how three agents read
// the consequences as broken trees.
type pressure struct {
	bytesUsed   uint64
	bytesTotal  uint64
	inodesUsed  uint64
	inodesTotal uint64
}

func readPressure(path string) (pressure, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return pressure{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	if st.Blocks == 0 {
		return pressure{}, fmt.Errorf("%s reports zero blocks, so its byte pressure cannot be measured", path)
	}
	if st.Files == 0 {
		// btrfs and some network filesystems report no inode table at all. That
		// is a real answer, but it is not the one this tool was written for, and
		// dividing by it would report 0% used for the resource that ran out.
		return pressure{}, fmt.Errorf("%s reports no inode table, so the exhaustion this tool exists to "+
			"diagnose cannot be measured on it", path)
	}
	return pressure{
		bytesUsed:   (st.Blocks - st.Bfree) * uint64(st.Bsize),
		bytesTotal:  st.Blocks * uint64(st.Bsize),
		inodesUsed:  st.Files - st.Ffree,
		inodesTotal: st.Files,
	}, nil
}

func (p pressure) bytesPct() float64  { return percent(p.bytesUsed, p.bytesTotal) }
func (p pressure) inodesPct() float64 { return percent(p.inodesUsed, p.inodesTotal) }

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

type pressureLevel string

const (
	pressureOK   pressureLevel = "ok"
	pressureWarn pressureLevel = "warn"
	pressureFail pressureLevel = "fail"
)

// gradePressure grades the WORSE of the two currencies. Grading their average,
// or bytes alone, is the failure this tool was written after.
func gradePressure(p pressure, warnPct, failPct float64) (pressureLevel, string) {
	worst, currency := p.bytesPct(), "bytes"
	if p.inodesPct() > worst {
		worst, currency = p.inodesPct(), "inodes"
	}
	line := fmt.Sprintf("scratch: bytes %.0f%% (%s of %s), inodes %.0f%% (%s of %s)",
		p.bytesPct(), humanBytes(int64(p.bytesUsed)), humanBytes(int64(p.bytesTotal)),
		p.inodesPct(), humanCount(int64(p.inodesUsed)), humanCount(int64(p.inodesTotal)))

	switch {
	case worst >= failPct:
		return pressureFail, line + fmt.Sprintf(
			"\nFAIL: %s are %.0f%% used, at or past the %.0f%% threshold."+
				"\n      Symptoms to expect: `git worktree add` failing \"No space left on device\" with bytes"+
				"\n      still free, `just test` reporting a build failure with no error text on a different"+
				"\n      package set each run, and tool processes dying. Those are this, not a broken tree.",
			currency, worst, failPct)
	case worst >= warnPct:
		return pressureWarn, line + fmt.Sprintf("\nWARN: %s are %.0f%% used, at or past the %.0f%% threshold; reap before it bites.", currency, worst, warnPct)
	default:
		return pressureOK, line
	}
}
