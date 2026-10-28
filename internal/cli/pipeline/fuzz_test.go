package pipeline_test

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/areqag/gqlc/internal/cli/backends"
	"github.com/areqag/gqlc/internal/cli/pipeline"
	"github.com/areqag/gqlc/internal/codegen"
	"github.com/areqag/gqlc/internal/config"
	"github.com/areqag/gqlc/internal/fuzzkit"
	"github.com/areqag/gqlc/internal/fuzzkit/gen"
)

// dirToken stands for the project directory in a generated project's paths
// and in a normalised Result, so two runs in two directories can be compared.
const dirToken = "@DIR@"

// fuzzProcsig is a registry a target may name. gen.Query writes no CALL, so it
// reaches procsig.Load and nothing past it.
const fuzzProcsig = "../../procsig/testdata/canonical.procsig.json"

var (
	fuzzPackages = []string{"people", "userdb", "orderdb", "db", "gen", "main"}
	// badPackages are refused by config.Load: not Go identifiers.
	badPackages = []string{"Bad-Pkg", "type", "9db"}
)

// project is one generated project: every file relative to its root, the
// config the root's gqlc.yaml holds (absent when cfg is nil), and which
// backends the registry handed to Run carries.
type project struct {
	files   map[string]string
	cfg     *config.Config
	drivers []string
}

// genProject draws a project from s. Every decision's first choice is the
// valid one, so an exhausted stream is a one-target project Run accepts. The
// low-rate arms, each a shape the pipeline must answer without breaking its
// Result contract: no config file; a target that shares another's schema or
// query directory; a schema reached through COPY OF; an absolute schema path; a
// missing schema file; a query directory with no query file; a procsig file,
// present or missing; two targets writing one output directory; a package name
// that is not a Go identifier; a registry without one of the backends; and,
// in a quarter of the query directories, files discovery must skip.
func genProject(s *fuzzkit.Stream, procsigSrc string) project {
	p := project{files: map[string]string{}}
	if !rare(s, 64) {
		p.cfg = &config.Config{}
	}
	var vocabs []gen.Vocab
	var copied *gen.Vocab
	n := 1 + s.Int(3)
	for i := range n {
		tgt := config.Target{
			SchemaPath: fmt.Sprintf("s%d.gql", i),
			SchemaLang: config.SchemaLangGQL,
			QueryDir:   fmt.Sprintf("q%d", i),
			QueryLang:  config.QueryLangOpenCypher,
			Go: config.GoGen{
				Package: s.Pick(fuzzPackages),
				Out:     fmt.Sprintf("out%d", i),
				Driver:  config.Driver(s.Pick(driverKeys())),
			},
		}
		var v gen.Vocab
		switch {
		case i > 0 && rare(s, 8):
			tgt.SchemaPath = "s0.gql"
			v = vocabs[0]
		case rare(s, 16):
			if copied == nil {
				var text string
				text, v = gen.Schema(s)
				p.files["G.gql"] = text
				copied = &v
			}
			v = *copied
			p.files[tgt.SchemaPath] = fmt.Sprintf("CREATE GRAPH TYPE S%d COPY OF G\n", i)
		case rare(s, 64):
			// Missing: Run's stage 3 refuses it.
			_, v = gen.Schema(s)
		default:
			var text string
			text, v = gen.Schema(s)
			p.files[tgt.SchemaPath] = text
		}
		if rare(s, 32) {
			tgt.SchemaPath = filepath.Join(dirToken, tgt.SchemaPath)
		}
		vocabs = append(vocabs, v)

		switch {
		case i > 0 && rare(s, 8):
			tgt.QueryDir = "q0"
		case rare(s, 64):
			// Empty: discovery refuses it.
			p.files[filepath.Join(tgt.QueryDir, "README.md")] = "no queries\n"
		default:
			for j := range 1 + s.Int(3) {
				// Query names are a batch-wide namespace, and QueryFile names
				// every file's queries Q0, Q1, …; the prefix keeps its
				// duplicate-name mutation inside one file.
				qf := strings.ReplaceAll(gen.QueryFile(s, v), "// name: Q", fmt.Sprintf("// name: F%dQ", j))
				p.files[filepath.Join(tgt.QueryDir, fmt.Sprintf("f%d.cypher", j))] = qf
			}
			if rare(s, 4) {
				p.files[filepath.Join(tgt.QueryDir, ".probe-skip.cypher")] = skippedText
				p.files[filepath.Join(tgt.QueryDir, "probe-skip.txt")] = skippedText
				p.files[filepath.Join(tgt.QueryDir, "probe-skip.cypher", "f.cypher")] = skippedText
			}
		}

		if rare(s, 16) {
			tgt.ProcsigPath = "procs.procsig.json"
			if !rare(s, 4) {
				p.files[tgt.ProcsigPath] = procsigSrc
			}
		}
		if i > 0 && rare(s, 32) {
			tgt.Go.Out = "out0"
		}
		if rare(s, 64) {
			tgt.Go.Package = s.Pick(badPackages)
		}
		if p.cfg != nil {
			p.cfg.Targets = append(p.cfg.Targets, tgt)
		}
	}

	for _, d := range driverKeys() {
		if !rare(s, 32) {
			p.drivers = append(p.drivers, d)
		}
	}
	return p
}

// skippedText is what sits in the files discovery must skip: text that, read
// as a query file, would be a diagnostic naming the file.
const skippedText = "this is not a query file\n"

func rare(s *fuzzkit.Stream, n int) bool { return s.Int(n) == n-1 }

func driverKeys() []string {
	keys := make([]string, 0, len(config.DriverValues()))
	for _, d := range config.DriverValues() {
		keys = append(keys, string(d))
	}
	return keys
}

// write lays p down under dir and returns the config path.
func (p project) write(t *testing.T, dir string) string {
	t.Helper()
	for rel, contents := range p.files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range p.queryDirs() {
		if err := os.MkdirAll(filepath.Join(dir, q), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfgPath := filepath.Join(dir, config.DefaultFilename)
	if p.cfg == nil {
		return cfgPath
	}
	b, err := p.cfg.Canonical()
	if err != nil {
		t.Fatalf("config.Canonical: %v", err)
	}
	if err := os.WriteFile(cfgPath, bytes.ReplaceAll(b, []byte(dirToken), []byte(dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func (p project) queryDirs() []string {
	if p.cfg == nil {
		return nil
	}
	dirs := make([]string, 0, len(p.cfg.Targets))
	for _, tgt := range p.cfg.Targets {
		dirs = append(dirs, tgt.QueryDir)
	}
	return dirs
}

// registry is the composed backend table restricted to p.drivers, each entry
// delegating to the composed one.
func (p project) registry(t *testing.T, all codegen.Registry) codegen.Registry {
	t.Helper()
	entries := make([]codegen.Entry, 0, len(p.drivers))
	for _, key := range p.drivers {
		newGen, ok := all.Lookup(key)
		if !ok {
			t.Fatalf("the composed registry has no %q", key)
		}
		entries = append(entries, codegen.Entry{Key: key, New: newGen})
	}
	reg, err := codegen.NewRegistry(entries...)
	if err != nil {
		t.Fatalf("codegen.NewRegistry: %v", err)
	}
	return reg
}

// runResult is one Run call with the project directory replaced by dirToken
// in every string it returns.
type runResult struct {
	res pipeline.Result
	err string
}

func normalise(dir string, res pipeline.Result, err error) runResult {
	rr := runResult{res: res}
	if err != nil {
		rr.err = strings.ReplaceAll(err.Error(), dir, dirToken)
	}
	for i, d := range rr.res.Diagnostics {
		rr.res.Diagnostics[i] = strings.ReplaceAll(d, dir, dirToken)
	}
	for i, w := range rr.res.Warnings {
		rr.res.Warnings[i] = strings.ReplaceAll(w, dir, dirToken)
	}
	for i, tr := range rr.res.Targets {
		rr.res.Targets[i].OutDir = strings.ReplaceAll(tr.OutDir, dir, dirToken)
	}
	return rr
}

// FuzzPipelineRun is target T10 of docs/specs/nightly-fuzzing-plan.md: the
// whole generate pipeline, config file through codegen, over a project drawn
// from the fuzz bytes by genProject and the gen generators. On every input it
// checks the Result contract Run's doc states as exhaustive:
//
//   - property error-zero: err != nil comes with the zero Result, so Targets is
//     nil and Warnings is empty;
//   - property targets-iff: with err nil, Targets is non-nil iff Diagnostics is
//     empty;
//   - property targets: on success, one TargetResult per declared target, each
//     Files non-nil and sorted by Path without repeats, each OutDir the
//     target's out resolved against the config file's directory;
//   - property provenance: every Diagnostics and Warnings line opens with the
//     graph[i] prefix of a declared target, and none names a file discovery
//     skips;
//   - property error-class: no error carries the "bug:" marker, and an
//     "internal:" error names a driver the registry handed to Run lacks — the
//     one internal arm a caller can reach; a missing config file is
//     ErrConfigMissing;
//   - property determinism: the same project laid down in two directories
//     gives equal Results and error text once each directory's path is
//     replaced by a token. Generated file bytes are compared unnormalised, so
//     a project path leaking into emitted code is a finding. A disagreement
//     openFinding names is a known open bead and is not reported again.
//
// Each Run is under fuzzkit.Guard's 2 s limit, so a slow or hung project is a
// finding as well.
func FuzzPipelineRun(f *testing.F) {
	all, err := backends.Registry()
	if err != nil {
		f.Fatal(err)
	}
	procsigSrc, err := os.ReadFile(fuzzProcsig)
	if err != nil {
		f.Fatal(err)
	}
	ramp := make([]byte, 256)
	for i := range ramp {
		ramp[i] = byte(i)
	}
	f.Add([]byte(nil))
	f.Add(ramp)
	f.Add(bytes.Repeat([]byte{0xff}, 256))
	f.Add(bytes.Repeat([]byte{2, 7, 1, 0, 3}, 64))
	f.Add([]byte("FuzzPipelineRun: three targets, every backend"))
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzkit.Skip(t, b)
		p := genProject(fuzzkit.NewStream(b), string(procsigSrc))
		reg := p.registry(t, all)

		var runs [2]runResult
		for k := range runs {
			dir := t.TempDir()
			cfgPath := p.write(t, dir)
			var res pipeline.Result
			var err error
			fuzzkit.Guard(t, 2*time.Second, func() { res, err = pipeline.Run(cfgPath, reg) })
			if msg := resultViolation(p, dir, res, err); msg != "" {
				t.Fatal(msg)
			}
			runs[k] = normalise(dir, res, err)
		}
		if !reflect.DeepEqual(runs[0], runs[1]) {
			if openFinding(runs) != "" {
				return
			}
			t.Fatalf("property determinism: two runs of one project disagree:\n%s\n%s\n%s", describe(runs[0]), describe(runs[1]), p.dump())
		}
	})
}

// openFinding names the open bead that already records a determinism
// disagreement between runs, or "". The fix PR deletes its arm (plan §11).
// What a tolerated input hides: a Warnings or Targets difference that comes
// with the tolerated one, since a query accepted in one run contributes both.
func openFinding(runs [2]runResult) string {
	a, b := runs[0], runs[1]
	if a.err != b.err {
		return ""
	}
	diff := symmetricDiff(a.res.Diagnostics, b.res.Diagnostics)
	if len(diff) == 0 {
		return ""
	}
	for _, d := range diff {
		if !strings.Contains(d, ": ambiguous label: ") && !strings.Contains(d, ": ambiguous binding: ") {
			return ""
		}
	}
	// Refused as ambiguous in one run and accepted in the other.
	return "gqlc-jjot2.28"
}

// symmetricDiff is the lines one of xs and ys holds more often than the other.
func symmetricDiff(xs, ys []string) []string {
	count := map[string]int{}
	for _, x := range xs {
		count[x]++
	}
	for _, y := range ys {
		count[y]--
	}
	var diff []string
	for line, n := range count {
		if n != 0 {
			diff = append(diff, line)
		}
	}
	return diff
}

// dump renders p for a failure message, so that a disagreement whose crasher
// is lost can still be rebuilt from the log.
func (p project) dump() string {
	var b strings.Builder
	if p.cfg != nil {
		cfg, err := p.cfg.Canonical()
		fmt.Fprintf(&b, "--- gqlc.yaml (%v)\n%s", err, cfg)
	}
	names := slices.Sorted(maps.Keys(p.files))
	for _, n := range names {
		fmt.Fprintf(&b, "--- %s\n%s", n, p.files[n])
	}
	fmt.Fprintf(&b, "--- registry: %v\n", p.drivers)
	return b.String()
}

func resultViolation(p project, dir string, res pipeline.Result, err error) string {
	if err != nil {
		return errorViolation(p, res, err)
	}
	if p.cfg == nil {
		return "property error-class: Run accepted a project with no config file"
	}
	declared := len(p.cfg.Targets)
	if (res.Targets != nil) != (len(res.Diagnostics) == 0) {
		return fmt.Sprintf("property targets-iff: Targets non-nil is %v with %d diagnostics", res.Targets != nil, len(res.Diagnostics))
	}
	for _, lines := range [][]string{res.Diagnostics, res.Warnings} {
		for _, l := range lines {
			if msg := provenanceViolation(declared, l); msg != "" {
				return msg
			}
		}
	}
	if res.Targets == nil {
		return ""
	}
	if len(res.Targets) != declared {
		return fmt.Sprintf("property targets: %d TargetResults for %d declared targets", len(res.Targets), declared)
	}
	for i, tr := range res.Targets {
		if tr.Files == nil {
			return fmt.Sprintf("property targets: graph[%d].Files is nil", i)
		}
		if !slices.IsSortedFunc(tr.Files, func(a, b codegen.File) int { return strings.Compare(a.Path, b.Path) }) {
			return fmt.Sprintf("property targets: graph[%d].Files is not sorted by Path: %v", i, paths(tr.Files))
		}
		for j := 1; j < len(tr.Files); j++ {
			if tr.Files[j].Path == tr.Files[j-1].Path {
				return fmt.Sprintf("property targets: graph[%d].Files repeats %q", i, tr.Files[j].Path)
			}
		}
		if want := filepath.Join(dir, p.cfg.Targets[i].Go.Out); tr.OutDir != want {
			return fmt.Sprintf("property targets: graph[%d].OutDir is %q, want %q", i, tr.OutDir, want)
		}
	}
	return ""
}

func errorViolation(p project, res pipeline.Result, err error) string {
	if !reflect.DeepEqual(res, pipeline.Result{}) {
		return fmt.Sprintf("property error-zero: Run returned %d targets, %d diagnostics and %d warnings alongside %v",
			len(res.Targets), len(res.Diagnostics), len(res.Warnings), err)
	}
	msg := err.Error()
	switch {
	case fuzzkit.ClassifyError(err, nil) == fuzzkit.ClassBug:
		return fmt.Sprintf("property error-class: Run's error carries the bug marker: %v", err)
	case p.cfg == nil && !errors.Is(err, pipeline.ErrConfigMissing):
		return fmt.Sprintf("property error-class: no config file, but the error is not ErrConfigMissing: %v", err)
	case strings.Contains(msg, "internal:"):
		for _, d := range driverKeys() {
			if !slices.Contains(p.drivers, d) && strings.Contains(msg, fmt.Sprintf("no pipeline mapping for driver %q", d)) {
				return ""
			}
		}
		return fmt.Sprintf("property error-class: an internal error with every mapping in place: %v", err)
	}
	return ""
}

func provenanceViolation(declared int, line string) string {
	for i := range declared {
		if rest, ok := strings.CutPrefix(line, fmt.Sprintf("graph[%d]: ", i)); ok {
			if strings.Contains(rest, "probe-skip") {
				return fmt.Sprintf("property provenance: %q names a file discovery skips", line)
			}
			return ""
		}
	}
	return fmt.Sprintf("property provenance: %q names no declared target", line)
}

func paths(files []codegen.File) []string {
	ps := make([]string, 0, len(files))
	for _, f := range files {
		ps = append(ps, f.Path)
	}
	return ps
}

func describe(r runResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "err=%q diagnostics=%q warnings=%q", r.err, r.res.Diagnostics, r.res.Warnings)
	for i, tr := range r.res.Targets {
		fmt.Fprintf(&b, "\n  graph[%d] %s: %v", i, tr.OutDir, paths(tr.Files))
	}
	return b.String()
}

// TestPipelineRunOutcomes measures which of Run's three branches a sample of
// genProject's projects lands on, and holds a floor on each: a generator that
// stopped reaching codegen, or stopped reaching the refusals, would leave
// FuzzPipelineRun checking one branch of the contract while it reads green on
// all three. -v prints the measurement.
func TestPipelineRunOutcomes(t *testing.T) {
	all, err := backends.Registry()
	if err != nil {
		t.Fatal(err)
	}
	procsigSrc, err := os.ReadFile(fuzzProcsig)
	if err != nil {
		t.Fatal(err)
	}
	const n = 300
	r := rand.New(rand.NewPCG(1, 2))
	var success, diagnostics, failed int
	for range n {
		b := make([]byte, 64+r.IntN(961))
		for j := range b {
			b[j] = byte(r.UintN(256))
		}
		p := genProject(fuzzkit.NewStream(b), string(procsigSrc))
		res, err := pipeline.Run(p.write(t, t.TempDir()), p.registry(t, all))
		switch {
		case err != nil:
			failed++
		case len(res.Diagnostics) > 0:
			diagnostics++
		default:
			success++
		}
	}
	t.Logf("over %d projects: success %d, diagnostics %d, error %d", n, success, diagnostics, failed)
	for name, got := range map[string]int{"success": success, "diagnostics": diagnostics, "error": failed} {
		if got < n/20 {
			t.Errorf("%s: %d of %d projects, under the 5%% floor", name, got, n)
		}
	}
}
