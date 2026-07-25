package gql

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestAreaCarrierOwnership answers, per owning area, "is every tag this area owes
// reached by a carrier under THIS area?" -- the question a corpus-wide union cannot ask,
// and the one that let unsignedInteger#1 sit in D2's tag file while only D1 could spell
// it.
//
// The rest of the phase-2 ownership suite is invariant under a permutation of ownership:
// union, disjointness, membership against a golden and per-area red counts are all
// conserved when two areas swap two names. Measured -- restoring the original
// unsignedInteger/fieldName misassignment, and swapping datetimeType#1 with emptyType#1,
// each pass every one of those. This check is not conserved, because its question is "can
// the owner reach it?" and that depends on who the owner is.
//
// A CARRIER IS EITHER SOURCE. A tag is discharged by a corpus file that already exists or
// by a spelling the brief prescribes for an author to write, and the two fail differently:
// a prescribed spelling dies if an author skips the row, a file dies if someone edits or
// deletes it. Scoring the brief alone reports a false orphan for every tag a seed file
// already carries -- measured, three of the four this check first reported were already
// discharged by files on disk, and acting on one of them would have sent an author to
// write a file that exists. So the input is the union, and the verdict names which kind
// of carrier it found.
//
// Files are attributed with corpusAreas, the harness's own prefix map, not a second copy
// of it. Spellings come from /tmp/lead/spellings.sh, which reads the brief's tables.
// Nothing here is a hand copy of a document -- the first version carried the brief's
// tables as a Go literal, which is a second document that nothing checks, and is the same
// shape as the drift it exists to catch.
//
// The verdict is the last line of stdout:
//
//	AREA-CARRIER-OWNERSHIP: areas=5 owned=46 by-file=14 by-brief=31 orphan=1 nowhere=0 inputs=A:3f+6b,B:2f+5b,C:4f+5b,D1:1f+10b,D2:0f+9b
//
// The caller must assert that line exists and says what it expects. Every number in it is
// a vacuity floor, and the breakdown is per area because a corpus-wide total is satisfied
// by four healthy areas and one empty one. `D2:0f` is currently legitimate -- D2 owns no
// file yet -- which is why the per-area floor is on the sum and the per-source floors are
// global.

// areaWrapper is each area's own file shape, from the brief's prose ("Every file here is
// a property value type, so wrap the spelling as `(:A {p :: <TYPE>})`"). Hardcoded, but
// self-checking: a wrong wrapper makes its area's spellings fail to parse or fail to
// enter statementRule, and both are reported per row rather than silently skipped.
var areaWrapper = map[string]string{
	"A":  "CREATE GRAPH TYPE t %s",
	"B":  "CREATE GRAPH TYPE t { %s }",
	"C":  "CREATE GRAPH TYPE t { (a:A), (b:B), %s }",
	"D1": "CREATE GRAPH TYPE t { (:A {p :: %s}) }",
	"D2": "CREATE GRAPH TYPE t { (:A {p :: %s}) }",
}

func areaDocsDir() string {
	if d := os.Getenv("H9N2_DOCS"); d != "" {
		return d
	}
	return "/tmp"
}

// areaNames is corpusAreas' key set, so an area added to the harness cannot be silently
// omitted from the ownership question.
func areaNames() []string {
	names := make([]string, 0, len(corpusAreas))
	for name := range corpusAreas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// areaOfFile attributes a corpus file with the harness's own prefix map. An unattributable
// file is an error, not a skip: a file no area claims is a file no area's check can see.
func areaOfFile(rel string) string {
	for name, area := range corpusAreas {
		for _, prefix := range area.prefixes {
			if strings.HasPrefix(rel, prefix) {
				return name
			}
		}
	}
	return ""
}

// tagOwners reads the per-area tag files. A missing file is fatal rather than skipped:
// four areas loaded and one absent is the exact shape of a vacuous ownership pass.
func tagOwners(t *testing.T, dir string) map[string]string {
	t.Helper()

	owner := map[string]string{}
	for _, area := range areaNames() {
		b, err := os.ReadFile(fmt.Sprintf("%s/h9n2-tags-%s.txt", dir, area))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			tag := strings.TrimSpace(line)
			if tag == "" {
				continue
			}
			if prev, dup := owner[tag]; dup {
				t.Errorf("tag %q is assigned to both %s and %s", tag, prev, area)
			}
			owner[tag] = area
		}
	}
	return owner
}

type tagCarrier struct {
	area string
	kind string // "file" or "brief"
	name string
}

func TestAreaCarrierOwnership(t *testing.T) {
	dir := areaDocsDir()
	// The phase-2 area documents live outside the repo, so a plain `go test ./...` has
	// nothing to check. Skipping is safe only because the caller requires the verdict
	// line below, which a skipped run does not print -- a skip cannot be read as a pass.
	if _, err := os.Stat(dir + "/h9n2-spellings-A.txt"); err != nil {
		t.Skip("no phase-2 area documents in " + dir + "; run /tmp/lead/spellings.sh")
	}
	owner := tagOwners(t, dir)
	areas := areaNames()

	carriers := map[string][]tagCarrier{}
	nFile, nBrief := map[string]int{}, map[string]int{}

	var files []string
	if err := filepath.WalkDir(corpusDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".gql") {
			rel, relErr := filepath.Rel(corpusDir, p)
			if relErr != nil {
				return relErr
			}
			files = append(files, rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)

	for _, rel := range files {
		area := areaOfFile(rel)
		if area == "" {
			t.Errorf("corpus file %s matches no area's prefixes, so no area's ownership check can see it", rel)
			continue
		}
		src, err := os.ReadFile(filepath.Join(corpusDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		c, errs := walkCoverage(t, string(src))
		if len(errs) > 0 {
			t.Errorf("corpus file %s does not parse: %v", rel, errs)
			continue
		}
		if !c.rules[statementRule] {
			t.Errorf("corpus file %s parses but is not a graph type statement", rel)
			continue
		}
		nFile[area]++
		for tag := range c.alternatives {
			carriers[tag] = append(carriers[tag], tagCarrier{area, "file", rel})
		}
	}

	for _, area := range areas {
		b, err := os.ReadFile(fmt.Sprintf("%s/h9n2-spellings-%s.txt", dir, area))
		if err != nil {
			t.Fatalf("%v -- run /tmp/lead/spellings.sh first", err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			spelling, claimed, _ := strings.Cut(line, "\t")
			src := fmt.Sprintf(areaWrapper[area], spelling)

			c, errs := walkCoverage(t, src)
			if len(errs) > 0 {
				t.Errorf("area %s: %q does not parse: %v\n        wrapped: %s", area, spelling, errs, src)
				continue
			}
			if !c.rules[statementRule] {
				t.Errorf("area %s: %q parses but is not a graph type statement: %s", area, spelling, src)
				continue
			}
			nBrief[area]++
			for tag := range c.alternatives {
				carriers[tag] = append(carriers[tag], tagCarrier{area, "brief", spelling})
			}
			for _, tag := range strings.Fields(claimed) {
				if !c.alternatives[tag] {
					t.Errorf("area %s: the brief says %q discharges %s; measured, it does not",
						area, spelling, tag)
				}
			}
		}
	}

	for _, area := range areas {
		if nFile[area]+nBrief[area] == 0 {
			t.Errorf("area %s has neither a corpus file nor a prescribed spelling, so every ownership question about it is vacuous", area)
		}
	}

	tags := make([]string, 0, len(owner))
	for tag := range owner {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	// Named per tag rather than counted, because a green count is trusted and a named
	// carrier is auditable. The orphan this check exists for was first reported against a
	// brief-only input, where three of four were already carried by files on disk.
	byFile, byBrief, orphan, nowhere := 0, 0, 0, 0
	for _, tag := range tags {
		own := owner[tag]
		var inFile, inBrief, outside []string
		for _, c := range carriers[tag] {
			switch {
			case c.area != own:
				outside = append(outside, fmt.Sprintf("%s:%s:%s", c.area, c.kind, c.name))
			case c.kind == "file":
				inFile = append(inFile, c.name)
			default:
				inBrief = append(inBrief, c.name)
			}
		}
		switch {
		case len(inFile) > 0:
			byFile++
			fmt.Printf("CARRIER %-34s %-3s file  %s\n", tag, own, strings.Join(inFile, " "))
		case len(inBrief) > 0:
			byBrief++
			fmt.Printf("CARRIER %-34s %-3s brief %s\n", tag, own, strings.Join(inBrief, " "))
		case len(outside) > 0:
			orphan++
			fmt.Printf("CARRIER %-34s %-3s ORPHAN\n", tag, own)
			t.Errorf("tag %s is owned by %s but carried only outside it:\n        %s",
				tag, own, strings.Join(outside, "\n        "))
		default:
			nowhere++
			fmt.Printf("CARRIER %-34s %-3s NOWHERE\n", tag, own)
			t.Errorf("tag %s is owned by %s and has no carrier anywhere in the corpus or the brief", tag, own)
		}
	}

	var per []string
	for _, area := range areas {
		per = append(per, fmt.Sprintf("%s:%df+%db", area, nFile[area], nBrief[area]))
	}
	// `by-file`/`by-brief` count TAGS; `inputs` counts the files and rows those tags were
	// scored from. Two different units, so they are named differently -- an earlier draft
	// wrote both as `file=`, where 14 tags and 3 files read as the same quantity.
	fmt.Printf("AREA-CARRIER-OWNERSHIP: areas=%d owned=%d by-file=%d by-brief=%d orphan=%d nowhere=%d inputs=%s\n",
		len(areas), len(owner), byFile, byBrief, orphan, nowhere, strings.Join(per, ","))
}
