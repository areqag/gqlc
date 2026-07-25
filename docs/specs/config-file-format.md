# gqlc config file format

The implementation brief for the gqlc config file loader
(`internal/config`): the hand-written YAML manifest that declares a
project's generation targets, each one a schema path, a query
directory, the three tool axes (schema language, query language,
driver) and a generated Go package. See CONTEXT.md ("Config file") for
the glossary entry. Design fixed in the gqlc-3w2 grill session.

Tracking: bead `gqlc-3w2`. Lands as one branch
(`feat/gqlc-3w2-config-parser`) with the spec and code together.
Version 1's shape was later rewritten from one flat target to a `graph`
sequence of them (ADR 0013, `docs/specs/config-multi-target.md`); that
spec is authoritative wherever the two disagree.

---

## 1. Purpose and scope

The config file is the single input `gqlc generate` reads to learn
everything about a project: for each generation target, where the
schema lives, where the queries live, where generated code goes, and
which language/driver combination it targets. `internal/config` owns
the format:

- `func Load(path string) (Config, error)` — read and validate a file.
- `func Decode(r io.Reader) (Config, error)` — the same from a stream
  (errors label the source `<stream>`), mirroring `procsig.Decode`.
- `func (c Config) Save(path string) error` — write the canonical form.

The package is CLI-agnostic: it never inspects the filesystem beyond
the config file itself, never resolves paths (§4), and returns a plain
`Config` struct the CLI layers policy on top of. The CLI wiring
(flag handling, `gqlc init`, threading `procsig` into codegen) is out
of scope here; `gqlc-aqb` owns the threading.

## 2. File location and naming

The canonical file name is `gqlc.yaml`, exported as
`config.DefaultFilename`. There are **no variants** — no `gqlc.yml`,
no `gqlc.json` — and **no search logic** (no walking up parent
directories, no home-directory fallback). A caller either uses
`DefaultFilename` in the current directory or passes an explicit path.
One name means "is this project gqlc-configured?" has exactly one
answer.

## 3. Wire schema (version 1)

A YAML mapping with exactly two keys: the format version and the list
of generation targets. There is nothing else at the document root —
no hoisted `schema`, no defaults an entry inherits.

| # | key       | type     | required | valid values | semantics                                                             |
|---|-----------|----------|----------|--------------|-----------------------------------------------------------------------|
| 1 | `version` | int      | yes      | `1`          | on-disk format version; wire-only (§5), never part of `Config`; must be a true YAML integer scalar (`!!int`) — floats (`1.0`, `1.5`, `1e0`), quoted strings (`"1"`), and non-scalars are rejected, never coerced (§6.2, §6.3) |
| 2 | `graph`   | sequence | yes      | ≥ 1 entry    | the generation targets, in document order (`Config.Targets`)          |

Each `graph` entry is a mapping. The order below is the canonical
`Save` emission order (§7).

| # | key               | type    | required | valid values           | semantics                                                            |
|---|-------------------|---------|----------|------------------------|----------------------------------------------------------------------|
| 1 | `schema`          | string  | yes      | non-empty              | path to the schema file (`Target.SchemaPath`)                        |
| 2 | `schema_language` | enum    | yes      | `gql`                  | language the schema file is written in (`Target.SchemaLang`)         |
| 3 | `queries`         | string  | yes      | non-empty              | directory holding query files (`Target.QueryDir`)                    |
| 4 | `query_language`  | enum    | yes      | `opencypher`           | language the query files are written in (`Target.QueryLang`)         |
| 5 | `procsig`         | string  | no       | non-empty when present | path to a procedure-signature registry file (`Target.ProcsigPath`); omit the key when unused — an explicit `""` is rejected, while a null value (a dangling `procsig:`) is equivalent to omission (§6.2) |
| 6 | `gen`             | mapping | yes      | one key, `go`          | the code-generation block; the level exists so a second generated language is a sibling of `go` |

`gen.go`:

| # | key       | type   | required | valid values                 | semantics                                                                       |
|---|-----------|--------|----------|------------------------------|---------------------------------------------------------------------------------|
| 1 | `package` | string | yes      | a valid Go identifier        | generated package name (`Target.Go.Package`); `go/token.IsIdentifier`, so Go keywords are rejected; casing is not policed |
| 2 | `out`     | string | yes      | non-empty                    | directory generated code is written to (`Target.Go.Out`), owned exclusively by gqlc (ADR 0012) |
| 3 | `driver`  | enum   | yes      | `neo4j-go-v5`, `neo4j-go-v6` | client library the generated code targets (`Target.Go.Driver`)                  |

Each enum axis is a closed vocabulary with an exported Go type
(`SchemaLang`, `QueryLang`, `Driver`), one constant per member, and a
`XxxValues()` function listing the members. The `XxxValues()` slices
are the single source of truth: loader error messages derive their
"valid values" lists from them, and `gqlc init` derives its prompt
choices from the same slices, so the two surfaces cannot drift.

Example (the canonical fixture,
`internal/config/testdata/canonical.gqlc.yaml`):

```yaml
version: 1
graph:
  - schema: schema.gql
    schema_language: gql
    queries: internal/user/query
    query_language: opencypher
    procsig: procs.procsig.json
    gen:
      go:
        package: userdb
        out: internal/user/gen
        driver: neo4j-go-v5
  - schema: schema.gql
    schema_language: gql
    queries: internal/order/query
    query_language: opencypher
    gen:
      go:
        package: orderdb
        out: internal/order/gen
        driver: neo4j-go-v6
```

## 4. Path semantics

Relative paths (`schema`, `queries`, `procsig`, `gen.go.out`) are
relative to **the config file's directory**, not the invoking process's
working directory. That is documented format semantics, implemented by
the CLI: the loader itself performs **no path resolution and no
filesystem checks** — it returns the raw strings exactly as written.
A config file that names a missing schema loads fine; the failure
belongs to the stage that opens the schema, with its own error surface.
This keeps `Load` deterministic (a pure function of the file's bytes)
and testable without fixture trees.

The one cross-entry rule is the exception that proves it: two entries
may not write to the same output directory, or to nested ones, because
each target's ADR 0012 wipe would destroy the other's output. The
comparison is **lexical** (`filepath.Rel` in both directions, stating
nothing), so it stays inside the no-filesystem rule. `Rel` refuses a
base that escapes its own root, so a pair of relative paths is first
rebased onto a shared synthetic directory deep enough to hold their
leading `..` components — still pure string work, and it makes `..`
against `a` the containment it is. What honestly escapes is the pairs
whose relation depends on a name the loader cannot see: an absolute
path against a relative one, and an escaping path that re-enters
through the working directory's own name (`../b/db` and `db` name one
directory when the working directory is itself named `b`).
`Config.CheckOutAgainst(out string) error` exports the rule so
`gqlc init --add` validates at the prompt with the loader's own
implementation rather than a second one that would drift.

## 5. Version model

`version` is required and only `1` is accepted. Decoding is
**probe-then-dispatch**: a lenient decode reads only the `version` key,
then dispatches to the matching per-version decoder (`decodeV1`
today). Each per-version decoder owns its wire struct and normalises
into the one canonical `Config` — the latest in-memory shape. There is
deliberately **no version interface** and no `Config.Version` field: a
loaded config is always current, `Save` always writes the latest
format, and old files keep loading forever because their decoder stays
behind (a future v2 adds `decodeV2` next to `decodeV1`; nothing else
changes). The seam is the two lines in `decode` that inspect the probe
result.

Version 1 named one target when it was first written and names a list
of them now (ADR 0013). That is a shape change within a version, not a
new one: the old flat form was never released, so there are no files to
migrate and nothing for a `decodeV2` to do. §6.3 carries a dedicated
message for the flat shape all the same, because such a file declares
`version: 1` truthfully and would otherwise reach the strict decode and
get one unknown-key line per key with no mention that the format
changed.

Version errors (§6.3) are raised at the probe, before any v1
strictness applies, so a v2 file with v2-only keys reports "declares
version 2" rather than a misleading unknown-field error. The probe is
lenient about every other key but **tag-strict about `version`
itself** (§6.2): only a `!!int` scalar counts, so the one field that
guards format evolution can never be satisfied — or misreported — by
yaml coercion.

## 6. Loader semantics

### 6.1 Entry points and error posture

`Load` reads the file with `os.ReadFile` and wraps failures as
`config: open <path>: %w` — the underlying error is preserved, so
`errors.Is(err, fs.ErrNotExist)` holds for a missing file (a future
`gqlc init` branches on it to offer creating the file). `Decode`
consumes any `io.Reader` and labels the source `<stream>`.

There are **no error sentinels** anywhere in the package: every error
is a `fmt.Errorf` with the `config: ` prefix, `%w`-wrapping an
underlying error where one exists. The loader is a build-time
configuration parser; callers read messages, they do not branch on
error identity (the single deliberate exception being the wrapped fs
error above).

### 6.2 Strictness

- **Empty input** (zero bytes) is rejected with a dedicated message
  naming the source — a truncation or stub, never a valid config,
  because every field is required.
- **The version probe is tag-strict.** yaml.v3 coerces numerics by
  default (`1.5` decodes into an `int` field as 1, `0.9` as 0); the
  probe's version field requires a `!!int` scalar, so `version: 1.5`,
  `1.0`, `1e0`, and `"1"` are all rejected with a message naming the
  actual tag and value — never loaded as a version the file did not
  declare.
- **Unknown keys reject.** The v1 decode runs `yaml.Decoder` with
  `KnownFields(true)`, so a typo (`packge:`) surfaces as an error with
  the offending key and line rather than silently dropping the value
  and then reporting the real key missing. Duplicate keys are likewise
  rejected (yaml.v3's own check).
- **Omitted vs empty are distinguished.** The wire struct's fields are
  all pointers; an omitted key produces the missing-field error, an
  explicit empty string produces the must-not-be-empty error.
- **Null equals omission, uniformly.** A key with a YAML null value —
  a dangling `schema:`, an explicit `~` or `null` — is treated exactly
  like an omitted key for **every** field: required fields report the
  missing-field error, and a null `procsig` means no registry. An
  empty string `""` is different: a present, empty value, rejected as
  below.
- **Explicit-empty `procsig` is rejected**, not treated as absent: an
  empty string is ambiguous (a placeholder? a deliberate "none"?), so
  the error tells the user to omit the key when unused.
  Reject-don't-guess.
- **Enum membership is validated at decode time** by
  `UnmarshalYAML(*yaml.Node)` on the typed strings, so the error
  carries the offending node's line number. A non-scalar value (a
  sequence, a mapping) is named as such — it is not misreported as the
  empty string, which is what its `Node.Value` would read as.
- **`graph`'s own shape is settled by an untyped document scan**, not
  by the strict decode. yaml.v3 *drops* a null sequence element rather
  than decoding it into a zero struct, so a document with one decodes
  to a shorter slice and every later entry's index shifts away from the
  index the messages print. The scan sees every element and rejects a
  null one by index and line; a count invariant after the decode fails
  loudly on any residual drop rather than renumbering silently.
- **Aliases are resolved before every kind and tag test the scan
  makes.** An alias node carries an empty `Tag` and its own `AliasNode`
  kind, so an unresolved `!!null` test passes `- *none` — the exact
  element yaml.v3 then drops. The test runs on the resolved node; the
  line reported is the node's as written, so an alias reports its own
  line and not its anchor's.
- **Merge keys resolve as YAML says they do.** `<<: *base` supplies
  keys yaml.v3 merges at decode time, which the literal-lookup scan
  cannot see; the resolution rule changes what the scan looks at, never
  what it rejects, and a merged entry loads.

### 6.3 Error catalogue

Every message the loader can produce, with `<src>` a file path or
`<stream>`. Document level:

| condition                                | message shape                                                                        |
|------------------------------------------|--------------------------------------------------------------------------------------|
| file open failure                        | `config: open <path>: <os error>` (wraps, so `errors.Is` works)                      |
| stream read failure                      | `config: read <src>: <error>`                                                        |
| zero-byte input                          | `config: <src> is empty (expected a gqlc config declaring version: 1)`               |
| malformed YAML                           | `config: <src>: yaml: ...` (yaml.v3's message, which carries line info)              |
| document is not a mapping                | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> ... into config.versionProbe` |
| `version` omitted (or null)              | `config: <src>: missing required field "version" (this gqlc supports version 1)`     |
| `version` not a `!!int` scalar           | `config: <src>: line <L>: field "version" must be a YAML integer (got !!float "1.5")`; non-scalars read `(got a YAML sequence)` |
| `version` a `!!int` that overflows Go `int` | ``config: <src>: field "version": yaml: unmarshal errors: line <L>: cannot unmarshal !!int `9223372...` into int`` (yaml.v3 truncates the literal) |
| `version` ≠ 1                            | `config: <src>: declares version <v>; only version 1 is supported`                   |
| old flat shape (no `graph`, a former top-level key present) | `config: <src>: line <L>: "<key>" is not a top-level key; version 1 declares a "graph" sequence of generation targets, each carrying its own schema, queries, and gen.go block` |
| `graph` omitted (or null)                | `config: <src>: missing required field "graph"`                                      |
| `graph` present but an empty sequence    | `config: <src>: line <L>: field "graph" must not be empty; declare at least one generation target` |
| `graph` is not a sequence                | `config: <src>: line <L>: field "graph" must be a sequence of generation targets (got a YAML <kind>)` |
| a null entry                             | `config: <src>: graph[<i>]: line <L>: entry is null`                                 |
| decoded entry count ≠ scanned entry count | `config: <src>: internal: "graph" declares <n> entries but <m> decoded; the entry indices in any further message would be wrong` |

`<kind>` is the **resolved** kind (§6.2), so `graph: *g` aliasing a
scalar reports `got a YAML scalar`, naming what the document supplies
rather than how it was spelled; every `<L>` is the line of the node as
written. The count row is the only one no config file should be able to
produce: it fires when yaml.v3 dropped an element the scan saw, and its
wording says so rather than blaming the document.

Entry level. Every row is prefixed `config: <src>: graph[<i>]: `,
elided below; nested keys are named by their dotted path:

| condition                              | message shape                                                       |
|----------------------------------------|---------------------------------------------------------------------|
| required key omitted (or null)         | `missing required field "<key>"`                                    |
| required enum key omitted (or null)    | `missing required field "<key>" (valid values: <list>)`             |
| path/package key present but empty     | `field "<key>" must not be empty`                                   |
| `procsig` present but empty            | `field "procsig" is empty; omit the key when no procsig file is used` |
| `gen.go.package` not a Go identifier   | `package "<val>" is not a valid Go identifier`                      |

Entry-level errors yaml.v3 formats carry no `graph[<i>]:` prefix — they
are raised inside the decode, which does not know the loader's index
scheme, and they carry a line number instead:

| condition                                | message shape                                                                        |
|------------------------------------------|--------------------------------------------------------------------------------------|
| unknown key                              | `config: <src>: yaml: unmarshal errors: line <L>: field <key> not found in type config.<wireV1\|wireTarget\|wireGen\|wireGo>` |
| duplicate key                            | `config: <src>: yaml: unmarshal errors: line <L>: mapping key "<key>" already defined at line <M>` |
| an entry is not a mapping                | ``config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal !!str `x` into config.wireTarget`` |
| non-scalar path/package value            | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> into string` |
| non-scalar enum value                    | `config: <src>: line <L>: invalid <key>: expected a scalar value, got a YAML <kind>` |
| invalid enum value                       | `config: <src>: line <L>: invalid <key> "<val>" (valid values: <list>)`              |

Cross-entry (§4), prefixed with the **later** entry's index:

| condition                                     | message shape                                                            |
|-----------------------------------------------|--------------------------------------------------------------------------|
| two entries share `gen.go.out`                | `config: <src>: graph[<j>]: out "<p>" is already graph[<i>]'s output directory` |
| the later entry's `out` is inside an earlier one's | `config: <src>: graph[<j>]: out "<p>" is inside graph[<i>]'s output directory "<q>"` |
| the later entry's `out` contains an earlier one's  | `config: <src>: graph[<j>]: out "<p>" contains graph[<i>]'s output directory "<q>"` |

These three are `CheckOutAgainst`'s error text with the loader's
`config: <src>: graph[<j>]: ` prefix in front and nothing else added,
so the `init --add` prompt renders the same sentence bare.

Checks run in **stages**, and the loader reports the first stage that
fails: the version probe (so a v2 file reports its version, not a shape
complaint); the document scan's old-flat-shape check, then `graph`
present/non-null/sequence/non-empty, then null entries; the strict v1
decode; the count invariant; the per-entry post-decode checks in the
field tables' order (required keys in wire order, then value checks),
entries in index order; and finally the cross-entry output-overlap
sweep, each entry against the entries before it. Within the
strict-decode stage, ordering is **not** document order: a
custom-unmarshal failure (an invalid enum, a wrong-typed version)
aborts the decode immediately, while unknown-key, duplicate-key, and
wrong-type errors are accumulated by yaml.v3 and reported together only
when the decode otherwise runs to completion — so a file with both a
typo'd key and a bogus enum value reports the enum error alone,
whichever comes first in the document.

## 7. Canonical Save form

`Save` writes exactly one form: `version: 1` first, then `graph` with
one entry per target in `Config.Targets` order, each entry's keys in
the §3 order, `procsig` omitted when `ProcsigPath` is empty, two-space
indent (sequence dashes indented under their key, as yaml.v3's encoder
emits them at `SetIndent(2)`), plain (unquoted) scalars, a trailing
newline, file mode `0o644`. For any valid `Config`, `Load(Save(c))`
returns `c` exactly, and saving a loaded canonical file reproduces its
bytes.

`Canonical() ([]byte, error)` returns those bytes without writing, so
`gqlc init`'s preview and its write are the same encoder rather than
two that could drift. A zero `Config` marshals to `graph: []`, which
`Load` then rejects as the empty sequence it is — the honest complaint,
where a nil pointer would have emitted `graph: null` and drawn the
missing-key error instead.

The fixture `internal/config/testdata/canonical.gqlc.yaml` is the
source of truth for the canonical form; a byte-equality test
(`TestSaveEmitsFixtureBytes`) pins `Save`'s output against it, so any
encoder drift fails visibly.

## 8. Rationale

- **CLI-agnostic package boundary.** `internal/config` parses bytes
  into a struct and back; it takes no position on working directories,
  flag precedence, or when a missing file is an error. Policy lives in
  the CLI, mechanism lives here — the same boundary `internal/procsig`
  drew, and the reason both are independently testable.
- **Required enums, no defaults.** Every axis started with exactly one
  member, and each is still a required field. Explicit over implicit: a
  config file states the whole pipeline, so the file is
  self-describing (a reader learns the query language from the file,
  not from gqlc's release notes) and there are no silent default
  semantics to break when a second member arrives — files written
  today already say what they meant. That payoff arrived with
  gqlc-suj: the driver axis grew to two members (`neo4j-go-v6`), and
  the required-no-default posture meant zero migration and no
  implied-default question.
- **Seam, not interface, for versioning.** One accepted version does
  not justify a `versionDecoder` interface; the probe-then-dispatch
  shape in `decode` is the whole abstraction. Adding v2 is a new
  function and one dispatch line, and every historical version keeps
  loading by normalising into the latest `Config`.
- **No error sentinels.** Message-carrying `fmt.Errorf` errors are the
  procsig posture and enough for a build-time parser; the one identity
  callers need (`fs.ErrNotExist`) rides `%w` for free.
- **`Save` and the exported vocabulary exist for `gqlc init`.** The
  canonical emitter plus `XxxValues()` are the mechanism the
  interactive `gqlc init` needs to prompt from the true vocabularies
  and write a file that round-trips — built before the wizard so the
  format's two directions are pinned by tests from day one.
- **A document scan alongside the typed decode, not instead of it.**
  The strict decode owns everything a struct can express — unknown
  keys, duplicates, types, enum vocabularies — and the scan owns only
  what it cannot see: that `graph` is a non-empty sequence, that no
  element is null, and that the format is not the old flat one. Both
  passes over the same bytes is the price of `graph[i]` indices that
  are the file's own (ADR 0013, `docs/specs/config-multi-target.md`
  §4).
