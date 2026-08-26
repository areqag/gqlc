# Graph type references resolve against a file catalogue rooted at the schema path

Design for bead `gqlc-pwly`, unblocking `gqlc-h9n.1` (`COPY OF` support).
Written 2026-08-24 by Արթուր. Every file:line below was read in a worktree at
`origin/master` (44ee6224), not recalled.
Amended 2026-08-26 by Արփինէ for `gqlc-pyc6`: §2's inventory omitted
`nonReservedWords`, and §3.3's segment rule was stated over one token class;
both are restated here against the same grammar file, unchanged since
44ee6224 (line numbers re-read at origin/master, 013a2533).

## 1. The problem in plain words

`CREATE GRAPH TYPE Copied COPY OF <graph type reference>` defines a graph type
whose element types are those of another graph type, named by a catalogue
path. gqlc has no catalogue: a generation target names exactly one schema
file (`schema:` in `gqlc.yaml`, a scalar — internal/config/config.go:215),
that file must hold exactly one graph type (`ErrMultipleGraphTypes`,
internal/schema/gql/listener.go:74-81), and that graph type must carry an
inline `AS { ... }` body. So a reference has nowhere to point, and `COPY OF`
is rejected with `ErrCopyOfSource` — honestly, as a recorded deviation
(`COPY OF` is mandatory core surface: no Annex D feature id gates it, and the
corpus entry for `12.6-graph-type-statement/copy_of_source.gql` says
`feature: "mandatory"`).

To support it, the reference must be given something to resolve against. The
smallest catalogue a build-time file-reading tool can have is the one it
already stands in: **the filesystem**. This ADR decides that mapping, the
spellings it supports and permanently declines, what a referenced file must
contain, and how cycles and escapes are refused. ADR 0016 already decided the
sibling question — `LIKE` takes a graph *expression* (session state) and is
declined in principle; nothing here reopens that, and the two continue not to
share a mechanism.

## 2. What exists now, verified

- **One graph type per file is already enforced.** listener.go:74-81 fails
  with `ErrMultipleGraphTypes` on the second `createGraphTypeStatement`.
- **The declared name's parent path is already discarded.** listener.go:102
  reads only the trailing `GraphTypeName().Identifier()`; ADR 0018 §2 decided
  that, and left a note that `COPY OF` must either keep the path or record why
  same-component paths may collide. §5.4 below is the answer that note asked
  for.
- **The reference grammar** (GQL.g4:1439-1478, 1381-1418): a
  `graphTypeReference` is either a `referenceParameterSpecification` (`$$x`)
  or a `catalogGraphTypeParentAndName` — an optional
  `catalogObjectParentReference` then a `graphTypeName`. The parent is either
  a dotted object chain (`o.`) or a `schemaReference` (absolute `/a/b/s`,
  bare `/`, relative `../s` with optional further `/..` climbs and a
  directory tail, or predefined `HOME_SCHEMA` / `CURRENT_SCHEMA` / `.`), each
  optionally followed by dotted object names. `s/gt` with no anchor is a
   syntax error (pinned in `copy_of_qualified.gql`'s header). Every name in
   these productions is `identifier` (GQL.g4:2956-2962), which admits three
   token classes. `regularIdentifier` (GQL.g4:2963-2966) is either
   `REGULAR_IDENTIFIER` (Unicode ID_Start then ID_Continue*, GQL.g4:3591-3620 —
   never a `/`, a `.`, or empty) or `nonReservedWords` (GQL.g4:3061-3109 — 47
   keyword tokens, `ACYCLIC` through `ZONE`, among them `SOURCE`; all pure
   ASCII letter sequences); the third class is the two delimited spellings.
   All 47 keyword tokens are defined before `REGULAR_IDENTIFIER`
   (GQL.g4:3538-3584), and the lexer is case-insensitive (GQL.g4:3), so
   ANTLR's equal-length tiebreak gives `Source` the `SOURCE` token, never
   `REGULAR_IDENTIFIER`.
- **The parse entry point takes a reader, not a path**
  (internal/schema/gql/parser.go:19, `Parse(r io.Reader)`), and the pipeline
  reads the file itself (internal/cli/pipeline/pipeline.go:185-191).
- **The corpus harness** requires a manifest entry for every `.gql` under the
  corpus root, in both directions, owned by an area prefix
  (corpus_test.go:525, corpusAreas at :271-297; `12.6-graph-type-statement/`
  and `17-references/` are Area A). The outcomes runner calls
  `New().Parse` (corpus_test.go:923). Twelve entries name `gqlc-h9n.1` as the
  bead that flips them.

## 3. The decisions

### 3.1 One graph type per file — kept, and promoted to the catalogue principle

A schema file holds exactly one graph type declaration. This is already the
enforced and pinned behaviour; the decision is to make it load-bearing rather
than incidental: **a graph type's catalogue identity is its file's path**.
There is no name table, no in-file scoping, and no "which declaration did you
mean" — a reference resolves to a file or it fails.

### 3.2 The catalogue is the filesystem, rooted at the target's schema directory

| ISO construct | gqlc mapping |
|---|---|
| catalogue root `/` | the directory containing the generation target's schema file |
| directory, schema | a filesystem directory (the two-level distinction is deliberately flattened — both are just path segments) |
| graph type | the file `<graphTypeName>.gql` in the resolved directory |
| current schema | the directory of the file the reference is written in |

Resolution is lexical, on `fs.FS`-style slash paths relative to the root:

- **absolute** (`/gt`, `/schemas/base/Source`): segments joined from the root.
- **current-schema** (bare `Source`, `./gt`, `CURRENT_SCHEMA/gt` — three
  spellings, one meaning): segments joined from the referencing file's own
  directory. `CURRENT_SCHEMA` has a natural static referent — the schema
  (directory) whose file is being resolved — so this is a translation, not an
  invention.
- **relative climb** (`../s/gt`, `../../s/gt`, `../a/b/s/gt`): pop one
  directory per `..` from the referencing file's directory, then join. A
  climb that pops past the catalogue root is refused
  (`ErrReferenceOutsideCatalogue`): everything a generated package depends on
  lives under the schema tree, mirroring the config-file rule that relative
  paths resolve within the project. Hermeticity here is lexical — this is a
  correctness rule, not a sandbox; symlinks under the root are the user's own
  business.

The extension is fixed: `.gql`. The root file itself is exempt from any
naming rule — it is reached by the config path, and today's schema files keep
working unchanged. No config change is needed at all: the catalogue root
falls out of the existing `schema:` key.

File lookup is byte-exact through the OS; on a case-insensitive filesystem a
wrong-case reference may resolve. Noted, not fought — the same is true of
every path in `gqlc.yaml`, and CI's Linux runners are the arbiter.

The bytes are the ones the user wrote: every name reaches the model through
`GetText()` unnormalised (the package has no case folding anywhere), so under
the case-insensitive lexer `COPY OF SOURCE` looks up `SOURCE.gql`, not
`Source.gql`. No existing test or golden pins what `GetText()` returns for a
keyword-token name; §5.4's loader tests add the pin.

### 3.3 Supported and declined spellings

Supported, lowered to `{anchor: absolute|current|climb(n), segs: [...]}`:
the absolute, current-schema, and relative-climb forms above, with every
segment drawn from the `regularIdentifier` production — either token class,
a plain `REGULAR_IDENTIFIER` or one of `nonReservedWords`' 47 keywords.

Declined permanently, each with its own sentinel and reason (the ADR 0016
pattern — a rejection carries its own justification), all four wrapping the
`ErrUnsupportedSource` class so `errors.Is` matchers keep working:

- **`ErrReferenceParameter`** — `$$gt`, `$$s/gt`
  (`referenceParameterSpecification`, in either the graph-type or the schema
  position). A substituted parameter is bound at execution time; a build-time
  catalogue has no parameter values. The same in-principle argument as
  `LIKE`'s (ADR 0016), applied to a spelling instead of a source.
- **`ErrHomeSchemaReference`** — `HOME_SCHEMA/gt`. The home schema is a
  property of a session, and gqlc has no session. Unlike `CURRENT_SCHEMA`
  there is no natural static referent; equating it with the catalogue root
  would invent a semantic ("the generation run is a session whose home is the
  root") to buy a pure synonym for `/`. Declining costs a user one
  respelling and is reversible; the mapping, once shipped, is forever.
- **`ErrObjectParentReference`** — `s.gt`, `/s/o.gt` (`objectName PERIOD`
  chains). An object parent is a catalogue object containing other objects;
  a directory-backed catalogue has no container between a directory and a
  file for it to be.
- **`ErrDelimitedReferenceSegment`** — any delimited identifier
  (`COPY OF "gt"`) in any segment. The schema package reads every name with
  `GetText()` and has no unquoting story anywhere (verified: no
  delimited-identifier handling in internal/schema/gql); a quoted segment
  would carry its quote characters into a file name, and a delimited
  identifier may legally contain `/`, `..`, or nothing — the path-injection
  shapes. Restricting segments to the `regularIdentifier` production makes
  "every segment is one safe path element" a property of the lexer — both of
  its token classes are ASCII letter sequences, with no `/`, no `.`, never
  empty — with no validation code to get wrong. Accepting more later is
  non-breaking.

These four are spelling judgments, independent of any particular catalogue,
so they fire in the **lowering** (listener) and are reported identically by
`Parse` and by the loader. They are deviations from mandatory core and are
recorded as such: their corpus entries keep `feature: "mandatory"` and move
from bead `gqlc-h9n.1` to `gqlc-0ri`, citing this ADR.

### 3.4 What a referenced file must contain

A referenced file is parsed exactly like any schema file — one graph type,
same sentinels, `LIKE` still refused. Two additional rules:

- **Trailing-name agreement.** The referenced file's declared trailing graph
  type name must equal the reference's `graphTypeName` segment, byte-exact
  (`ErrReferenceNameMismatch`, naming both strings and the file). The lookup
  found the file by name; this check catches the drift where a file is
  renamed and its declaration is not. The root file makes no such promise and
  is exempt.
- **Declared parent paths stay discarded** (ADR 0018 §2 unchanged). This is
  the answer to ADR 0018's open note: the anticipated `/a/b/G` vs `/c/d/G`
  collision never materialises because **resolution is by path, not by
  name** — no name table exists for two truncated names to collide in, and a
  referenced file's `Schema.Name` is never read (only the root's reaches
  `derivePackage`). The truncation remains a display choice. A file cannot
  relocate itself by declaring a parent path; its location is its location.

The resolved model is the chain tail's model with `Name` replaced by the
root file's declared trailing name. Nothing else is rewritten — `COPY OF`
means the same element types under a new name.

### 3.5 Cycles

A graph type has exactly one source and a `COPY OF` source holds exactly one
reference, so reference chains are **linear** — there are no diamonds, and a
cycle is the only way a chain fails to terminate at a nested body. The
loader keeps the ordered list of visited root-relative paths; resolving to a
path already on it is `ErrReferenceCycle`, reported with the whole chain
(`a.gql → b.gql → a.gql`) before any file is reopened. The self-copy is the
one-element cycle and needs no special case. No depth limit: the visited set
is bounded by the files on disk.

### 3.6 Architecture: a loader above the parser, `Parse` unchanged

```go
// internal/schema/gql
type Loader struct{ fsys fs.FS }
func NewLoader(fsys fs.FS) *Loader
func (l *Loader) Load(path string) (schema.Schema, error)
```

- The listener's source guard keeps its shape — test for the supported
  alternatives, reject the rest — with `copyOfGraphType` joining
  `nestedGraphTypeSpecification` as supported: it lowers the reference
  (possibly failing with one of §3.3's four sentinels) and records it on the
  raw model. An alternative added to the grammar later still falls through to
  the bare `ErrUnsupportedSource` class, preserving ADR 0016's
  future-alternative property.
- **`Parse(io.Reader)` keeps rejecting a lowered reference with
  `ErrCopyOfSource`**, whose message — "cannot reach, having no catalogue" —
  is literally true of a bare reader. No caller changes. Internally the
  walk is factored so `Parse` and `Load` share it.
- **`Load`** follows the chain per §3.2–3.5 (paths are `io/fs` slash paths,
  so `fs.ValidPath` shapes fall out naturally), then resolves the tail and
  renames per §3.4. Its errors name the referencing file, the reference as
  written, and the resolved path — the diagnostic points at the line the
  user wrote, not at the file that failed to exist.
- **The pipeline** (pipeline.go:177-191) replaces `os.ReadFile` + `Parse`
  with `gql.NewLoader(os.DirFS(filepath.Dir(schemaPath))).Load(filepath.Base(schemaPath))`
  in the `SchemaLangGQL` arm. No new `schema.Loader` interface until a second
  schema language exists — the axis switch already names `gql` concretely.
- The four loader-only sentinels (`ErrReferenceOutsideCatalogue`,
  `ErrDanglingReference`, `ErrReferenceCycle`, `ErrReferenceNameMismatch`)
  stand alone — they are resolution failures of a *supported* spelling, not
  source rejections, so they do not wrap `ErrUnsupportedSource` and no new
  class is minted for four leaves.

### 3.7 The corpus loads every file with production semantics

The outcomes runner (corpus_test.go:923) switches from `Parse` to
`NewLoader(os.DirFS(<dir of the entry's file>)).Load(<base name>)` — for
every entry, uniformly. Each corpus file is loaded exactly as a generation
target would be, root at its own directory, so the corpus pins production
behaviour rather than a harness-only catalogue. Fixtures land inside the
areas that reference them, keeping the manifest bijection and area ownership
untouched (the corpus starts to nest; corpusArea's "nothing in the corpus
nests" comment is updated — HasPrefix already reaches).

## 4. Considered options

**Several graph types per file, resolved by name.** Rejected: it repeals an
enforced, pinned invariant to build a name table nobody asked for, creates
"which declaration is the target's model" where today that question cannot
arise, and reintroduces the ADR 0018 truncation collision that path-based
resolution dissolves.

**A config-declared catalogue** (the `schema:` key grows a list or a
manifest of path→file bindings). Rejected: it duplicates in YAML what the
directory tree already encodes, adds config surface that can drift from the
tree, and still needs the ISO path grammar mapped onto it. The filesystem
mapping needs zero configuration.

**A search path.** Rejected: order-dependent resolution and invisible
dependencies; the diagnostic for "which candidate won" is a genre of bug
report gqlc does not need.

**`HOME_SCHEMA` as the catalogue root.** Rejected in §3.3: a synonym for
`/` bought by inventing a session.

**Unquote delimited identifiers into file names.** Rejected in §3.3: no
unquoting machinery exists in the package, and the safety argument
(lexer-guaranteed segments) is better than any validator.

**Skip the trailing-name check.** Rejected: one string comparison catches
rename drift; tolerating it makes the tree lie silently.

**One shared harness catalogue rooted at the corpus root.** Rejected: bare
absolute spellings (`/gt` — the SOLIDUS-only alternative cannot take a
directory) would force fixtures to the corpus top level, outside every area
prefix, and the corpus would pin a root-anchoring the pipeline never uses.

## 5. Consequences — the executable plan

Files touched: `internal/schema/gql/{errors,listener,parser,raw?}.go`, new
`internal/schema/gql/loader.go` (+ `loader_test.go`),
`internal/cli/pipeline/pipeline.go`, corpus areas file(s) for Area A,
new/edited files under `test/data/schema/gql/corpus/`, `CONTEXT.md` (done in
this PR), `docs/specs/cli-generate-pipeline.md` §3.2 wording if it spells
ReadFile+Parse.

### 5.1 Sentinels

Eight new exported sentinels as in §3.3/§3.6. `ErrCopyOfSource` stays, for
`Parse` alone. `allSentinels` gains the new leaves;
`TestGraphTypeSourceErrorsWrapTheClass` extends to the four class-wrapped
ones. Note one behaviour change under bare `Parse`: `COPY OF $$gt` et al.
move from `ErrCopyOfSource` to their spelling sentinels (same class).

### 5.2 Existing corpus entries — the twelve dispositions

| file | today | after |
|---|---|---|
| 12.6-graph-type-statement/copy_of_source.gql | ErrCopyOfSource | **resolves** (fixture `schemas/base/Source.gql`) |
| 17-references/copy_of_absolute_bare.gql | " | **resolves** (fixture `gt.gql`) |
| 17-references/copy_of_graph_type_bare.gql | " | **resolves** (fixture `Source.gql`) |
| 17-references/copy_of_current_schema.gql | " | **resolves** (fixture `gt.gql`) |
| 17-references/copy_of_predefined_current.gql | " | **resolves** (fixture `gt.gql`) |
| 17-references/copy_of_relative_up.gql | " | ErrReferenceOutsideCatalogue (its dir IS its root; the climb witness is §5.3's pair) |
| 17-references/copy_of_relative_up_twice.gql | " | ErrReferenceOutsideCatalogue |
| 17-references/copy_of_home_schema.gql | " | ErrHomeSchemaReference, bead → gqlc-0ri |
| 17-references/copy_of_param_graph_type.gql | " | ErrReferenceParameter, bead → gqlc-0ri |
| 17-references/copy_of_param_schema.gql | " | ErrReferenceParameter, bead → gqlc-0ri |
| 17-references/copy_of_qualified.gql | " | ErrObjectParentReference, bead → gqlc-0ri |
| 17-references/copy_of_schema_and_object.gql | " | ErrObjectParentReference, bead → gqlc-0ri |

Resolving entries drop their bead; the escape rows keep `feature:
"mandatory"` and cite gqlc-0ri + this ADR (refusing to climb out of the
catalogue is a deviation from ISO's larger catalogue, chosen here).

### 5.3 New corpus files (all Area A; exact reference spellings are load-bearing, bodies and headers are the author's)

| file | content sketch | outcome |
|---|---|---|
| 12.6-graph-type-statement/schemas/base/Source.gql | nested body, name `Source` | resolves |
| 17-references/gt.gql | nested body, name `gt` | resolves |
| 17-references/Source.gql | nested body, name `Source` | resolves |
| 17-references/copy_of_chain_climb.gql | `Copied COPY OF /sub/climber` | resolves (a two-hop chain ending in a successful climb) |
| 17-references/sub/climber.gql | `climber COPY OF ../s/base` | ErrReferenceOutsideCatalogue |
| 17-references/s/base.gql | nested body, name `base` | resolves |
| 17-references/copy_of_dangling.gql | `Copied COPY OF nowhere` | ErrDanglingReference |
| 17-references/copy_of_name_mismatch.gql | `Copied COPY OF liar` | ErrReferenceNameMismatch |
| 17-references/liar.gql | nested body, name `NotLiar` | resolves |
| 17-references/cycle_self.gql | `cycle_self COPY OF cycle_self` | ErrReferenceCycle |
| 17-references/cycle_a.gql | `cycle_a COPY OF cycle_b` | ErrReferenceCycle |
| 17-references/cycle_b.gql | `cycle_b COPY OF cycle_a` | ErrReferenceCycle |
| 17-references/copy_of_delimited.gql | `Copied COPY OF "gt"` | ErrDelimitedReferenceSegment |

The `climber` pair is the design's sharpest witness (see §6). Arithmetic to
confirm against the harness, not to trust: 13 new files takes
`wantCorpusEntries` 123 → 136; five flips plus six new resolving files takes
`wantCorpusResolving` 67 → 78.

### 5.4 Collateral the implementer must check

- `corpus_resolving_test.go:210-213` keys a 29-name excuse to the `COPY OF`
  sentinel, marked "retire when catalogue/multi-file scoping lands" — the
  schema-reference grammar names now get reached; shrink or retire it.
- `TestCorpusSpellingTraps` asserts grammar coverage, not sentinels
  (verified) — untouched. `TestNoUndeclaredLossiness` and
  `TestSemanticCaseCollisions` call `Parse` on rewritten content; no
  reference-bearing file carries a `TYPE(arg)` shape or a semantic case, so
  they should be unaffected — verify, don't assume.
- The `resolves` outcome asserts through the loader now; the manifest's
  charter comment ("outcome is what Parse does") is updated.
- Loader unit tests on `fstest.MapFS`: chain of two, rename via chain
  (result `Name` is the root's), dangling, escape (climb at root; climb
  inside a chain hop succeeding — the climber shape), cycle (self and
  two-cycle, chain named in the error), name mismatch, each declined
  spelling surfacing identically via `Parse` and `Load`, and a reference
  whose segment is a `nonReservedWords` keyword (`COPY OF NODE` against a
  `NODE.gql` fixture) resolving — the only test that distinguishes "both
  token classes accepted" from "the keyword rows pass by fixture luck",
  and the pin for §3.2's written-bytes lookup claim.

### 5.5 Mutation duty (the implementing PR adds or changes guards, so ADR 0005 rows are owed)

Declare victims before running; the guards worth mutating by name: the
escape pop-guard (blind it → the escape rows report `ErrDanglingReference`
or resolve, both KILLED by sentinel equality), the cycle membership check
(blind it → `Load` loops; the row's verdict is "hangs, killed by
`go test -timeout`" — record it as such rather than pretending it fails
cleanly), the name-match comparison, the delimited-segment refusal, and
the `Parse`-level `copyRef != nil` refusal. Screen with
`go test -c -o /dev/null ./internal/schema/gql`.

## 6. Falsifiability

If §3.2's anchoring is wrong in either direction, a named check fails:
anchor the root at each file's own directory during chains and
`copy_of_chain_climb.gql` goes red (its hop's climb would escape); anchor
the current schema at the root file's directory instead of the referencing
file's and the same file goes red the other way, while
`sub/climber.gql` — the same reference text, loaded standalone — must stay
red as an escape. If my reading of the reference grammar mislabels an
alternative, the corpus's declared-sentinel equality catches it at
implementation time, and `copy_of_qualified.gql`'s pinned claim that bare
`s/gt` is a syntax error guards the lowering table's premise. The claimed
negative "no delimited-identifier handling exists in internal/schema/gql"
was witnessed by grep, and the segment-safety claim rests on both
`regularIdentifier` arms — GQL.g4:3591-3620 and `nonReservedWords` at
GQL.g4:3061-3109, quoted in §2.

One correction to the filing bead's scope note, for the record: `COPY OF` is
**not** an optional Annex D feature — no feature id covers it (verified
against `internal/schema/gql/annexd/codes.go`), and the corpus records it as
`"mandatory"`. Nothing in the priority conclusion changes: gqlc's rejection
is an honestly recorded deviation, and this is still a capability being
added, not an emergency. The deviation register shrinks by one construct
when this lands.
