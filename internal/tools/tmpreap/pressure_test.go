package main

import (
	"strings"
	"testing"
)

// The whole diagnostic. Bytes at 27% with inodes exhausted is the exact shape
// /tmp was in when three agents read the consequences as broken trees, and it is
// the shape a bytes-only grade calls healthy (bd gqlc-osuz).
func TestGradePressure_InodeExhaustionFailsWithBytesFree(t *testing.T) {
	p := pressure{
		bytesUsed:   11_700_000_000,
		bytesTotal:  16_000_000_000,
		inodesUsed:  1_048_576,
		inodesTotal: 1_048_576,
	}
	level, summary := gradePressure(p, 85, 95)
	if level != pressureFail {
		t.Fatalf("level = %q, want %q for a full inode table", level, pressureFail)
	}
	if !strings.Contains(summary, "inodes") {
		t.Errorf("summary does not name inodes: %s", summary)
	}
	if !strings.Contains(summary, "No space left on device") {
		t.Errorf("summary does not name the symptom this presents as: %s", summary)
	}
}

func TestGradePressure_ByteExhaustionFailsWithInodesFree(t *testing.T) {
	p := pressure{
		bytesUsed:   15_900_000_000,
		bytesTotal:  16_000_000_000,
		inodesUsed:  10_000,
		inodesTotal: 1_048_576,
	}
	if level, _ := gradePressure(p, 85, 95); level != pressureFail {
		t.Fatalf("level = %q, want %q for a full byte budget", level, pressureFail)
	}
}

func TestGradePressure_Bands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		used   uint64
		want   pressureLevel
		total  uint64
		inodes uint64
	}{
		{name: "quiet", used: 100, total: 1000, inodes: 100, want: pressureOK},
		{name: "at the warn threshold", used: 850, total: 1000, inodes: 100, want: pressureWarn},
		{name: "at the fail threshold", used: 950, total: 1000, inodes: 100, want: pressureFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := pressure{bytesUsed: tc.used, bytesTotal: tc.total, inodesUsed: tc.inodes, inodesTotal: 1000}
			if got, _ := gradePressure(p, 85, 95); got != tc.want {
				t.Errorf("level = %q, want %q", got, tc.want)
			}
		})
	}
}

// The -apply-above gate grades the same number gradePressure does, and this is
// the case that distinguishes it from a bytes-only reading: the filesystem that
// stalled this town was 27% bytes and 100% inodes, so a cadence gated on bytes
// would have sat out the whole incident.
func TestPressureWorst_TakesTheFullerCurrencyNotBytes(t *testing.T) {
	inodeBound := pressure{bytesUsed: 27, bytesTotal: 100, inodesUsed: 99, inodesTotal: 100}
	if worst, currency := inodeBound.worst(); worst != 99 || currency != "inodes" {
		t.Errorf("worst() = %.0f%% %s, want 99%% inodes", worst, currency)
	}
	byteBound := pressure{bytesUsed: 99, bytesTotal: 100, inodesUsed: 1, inodesTotal: 100}
	if worst, currency := byteBound.worst(); worst != 99 || currency != "bytes" {
		t.Errorf("worst() = %.0f%% %s, want 99%% bytes", worst, currency)
	}
}

func TestReadPressure_ReportsBothCurrencies(t *testing.T) {
	p, err := readPressure(t.TempDir())
	if err != nil {
		t.Fatalf("readPressure: %v", err)
	}
	if p.bytesTotal == 0 {
		t.Error("bytesTotal is zero, so byte pressure is being reported off nothing")
	}
	if p.inodesTotal == 0 {
		t.Error("inodesTotal is zero, so inode pressure is being reported off nothing")
	}
}

func TestReadPressure_MissingPathIsAnError(t *testing.T) {
	if _, err := readPressure(t.TempDir() + "/does-not-exist"); err == nil {
		t.Fatal("statfs of a path that does not exist produced a pressure reading")
	}
}
