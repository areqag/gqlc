# gqlc config file format

The implementation brief for the gqlc config file loader
(`internal/config`): the hand-written YAML manifest that declares a
project's generation pipeline — the schema path, the query directory,
the output target, and the three tool axes (schema language, query
language, driver). See CONTEXT.md ("Config file") for the glossary
entry. Design fixed in the gqlc-3w2 grill session.

Tracking: bead `gqlc-3w2`. Lands as one branch
(`feat/gqlc-3w2-config-parser`) with the spec and code together.

---

## 1. Purpose and scope

The config file is the single input a future `gqlc generate` reads to
learn everything about a project: where the schema lives, where the
queries live, where generated code goes, and which language/driver
combination the pipeline targets. `internal/config` owns the format:

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

A single YAML mapping. Keys are snake_case. The order below is the
canonical `Save` emission order (§7).

| # | key               | type   | required | valid values             | semantics                                                          |
|---|-------------------|--------|----------|--------------------------|--------------------------------------------------------------------|
| 1 | `version`         | int    | yes      | `1`                      | on-disk format version; wire-only (§5), never part of `Config`; must be a true YAML integer scalar (`!!int`) — floats (`1.0`, `1.5`, `1e0`), quoted strings (`"1"`), and non-scalars are rejected, never coerced (§6.2, §6.3) |
| 2 | `schema`          | string | yes      | non-empty                | path to the schema file (`Config.SchemaPath`)                      |
| 3 | `queries`         | string | yes      | non-empty                | directory holding query files (`Config.QueryDir`)                  |
| 4 | `output`          | string | yes      | non-empty                | directory generated code is written to (`Config.OutputDir`)        |
| 5 | `package`         | string | yes      | a valid Go identifier    | generated package name (`Config.OutputPackage`); `go/token.IsIdentifier`, so Go keywords are rejected; casing is not policed |
| 6 | `schema_language` | enum   | yes      | `gql`                    | language the schema file is written in (`Config.SchemaLang`)       |
| 7 | `query_language`  | enum   | yes      | `opencypher`             | language the query files are written in (`Config.QueryLang`)       |
| 8 | `driver`          | enum   | yes      | `neo4j-go-v5`, `neo4j-go-v6` | client library the generated code targets (`Config.Driver`)        |
| 9 | `procsig`         | string | no       | non-empty when present   | path to a procedure-signature registry file (`Config.ProcsigPath`); omit the key when unused — an explicit `""` is rejected, while a null value (a dangling `procsig:`) is equivalent to omission (§6.2) |

Each enum axis is a closed vocabulary with an exported Go type
(`SchemaLang`, `QueryLang`, `Driver`), one constant per member, and a
`XxxValues()` function listing the members. The `XxxValues()` slices
are the single source of truth: loader error messages derive their
"valid values" lists from them, and a future interactive `gqlc init`
derives its prompt choices from the same slices, so the two surfaces
cannot drift.

Example (the canonical fixture,
`internal/config/testdata/canonical.gqlc.yaml`):

```yaml
version: 1
schema: schema.gql
queries: queries/
output: internal/db
package: db
schema_language: gql
query_language: opencypher
driver: neo4j-go-v5
procsig: procs.procsig.json
```

## 4. Path semantics

Relative paths (`schema`, `queries`, `output`, `procsig`) are relative
to **the config file's directory**, not the invoking process's working
directory. That is documented format semantics, implemented by the
future CLI: the loader itself performs **no path resolution and no
filesystem checks** — it returns the raw strings exactly as written.
A config file that names a missing schema loads fine; the failure
belongs to the stage that opens the schema, with its own error surface.
This keeps `Load` deterministic (a pure function of the file's bytes)
and testable without fixture trees.

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

### 6.3 Error catalogue

Every message the loader can produce, with `<src>` a file path or
`<stream>`:

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
| unknown key                              | `config: <src>: yaml: unmarshal errors: line <L>: field <key> not found in type ...` |
| duplicate key                            | `config: <src>: yaml: unmarshal errors: line <L>: mapping key "<key>" already defined at line <M>` |
| non-scalar path/package value            | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> into string` |
| non-scalar enum value                    | `config: <src>: line <L>: invalid <key>: expected a scalar value, got a YAML <kind>` |
| required key omitted                     | `config: <src>: missing required field "<key>"`                                      |
| required enum key omitted                | `config: <src>: missing required field "<key>" (valid values: <list>)`               |
| invalid enum value                       | `config: <src>: line <L>: invalid <key> "<val>" (valid values: <list>)`              |
| path/package key present but empty       | `config: <src>: field "<key>" must not be empty`                                     |
| `procsig` present but empty              | `config: <src>: field "procsig" is empty; omit the key when no procsig file is used` |
| `package` not a Go identifier            | `config: <src>: package "<val>" is not a valid Go identifier`                        |

Checks run in **stages**, and the loader reports the first stage that
fails: the version probe first (so a v2 file reports its version, not
v1 strictness violations); then the strict v1 decode; then the
post-decode checks in the field table's order (required keys in wire
order, then value checks). Within the strict-decode stage, ordering is
**not** document order: a custom-unmarshal failure (an invalid enum, a
wrong-typed version) aborts the decode immediately, while unknown-key,
duplicate-key, and wrong-type errors are accumulated by yaml.v3 and
reported together only when the decode otherwise runs to completion —
so a file with both a typo'd key and a bogus enum value reports the
enum error alone, whichever comes first in the document.

## 7. Canonical Save form

`Save` writes exactly one form: `version: 1` first, then the wire keys
in the §3 order, `procsig` omitted when `ProcsigPath` is empty,
two-space indent, plain (unquoted) scalars as yaml.v3 emits them, a
trailing newline, file mode `0o644`. For any valid `Config`,
`Load(Save(c))` returns `c` exactly, and saving a loaded canonical
file reproduces its bytes.

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
  canonical emitter plus `XxxValues()` are the mechanism a future
  interactive `gqlc init` needs to prompt from the true vocabularies
  and write a file that round-trips — built now so the format's two
  directions are pinned by tests from day one.
