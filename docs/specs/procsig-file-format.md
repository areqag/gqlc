# procsig on-disk file format

The implementation brief for the CLI-consumable on-disk format of the
Stage 14 procedure signature registry (`internal/procsig`). Follow-up
to gqlc-im1 (Stage 14 close-out) per gqlc-6rz. Stage 14 shipped the
in-memory `procsig.Registry` API and the godog step that parses TCK
declarations per scenario; this spec adds the CLI-consumable format so
real callers (codegen invocations, batch tooling) can populate the
registry from a file rather than from Go code.

Tracking: bead `gqlc-6rz` (GitHub #185). Lands as one branch
(`feat/gqlc-6rz-procsig-file-format`) with a spec commit and a code
commit.

---

## 1. Deliverables

### 1.1 A JSON on-disk format that round-trips `procsig.Registry`

A JSON schema whose top-level object carries a `signatures` array of
signature declarations. Each signature declaration has `name`,
`params`, and `results`; each column carries `name`, `type`, and
`nullable`. The `type` field is the uppercase TCK token name
(`"INTEGER"`, `"FLOAT"`, `"STRING"`, `"NUMBER"`) — the same vocabulary
the TCK declaration syntax uses, so a human reading the file sees the
same tokens they would write in a `.feature` background step.

Example (one signature, mirrors Call5[3]):

```json
{
    "signatures": [
        {
            "name": "test.my.proc",
            "params": [
                {"name": "in", "type": "INTEGER", "nullable": true}
            ],
            "results": [
                {"name": "a", "type": "STRING", "nullable": true},
                {"name": "b", "type": "STRING", "nullable": true}
            ]
        }
    ]
}
```

### 1.2 A loader that constructs `procsig.Registry` from a file path

- `func Load(path string) (Registry, error)` — reads the file at
  `path`, parses JSON, validates the on-disk shape, and delegates to
  `NewRegistry` for the semantic checks it already owns. Returns the
  zero `Registry` and a wrapping error on any failure (file open,
  malformed JSON, unknown type token, or any error the existing
  `NewRegistry` validation raises).
- `func Decode(r io.Reader) (Registry, error)` — the same, driven by
  an `io.Reader` so tests can round-trip in memory without touching
  the filesystem and so callers with an already-open stream (an HTTP
  body, a `bytes.Reader`) do not have to write a temp file.

### 1.3 A serialiser that writes `Registry` values back to the format

- `func (Registry) MarshalJSON() ([]byte, error)` — the canonical JSON
  encoding, sorted by signature name so the output is deterministic
  regardless of Go's randomised map iteration order (matches the
  `schema.Schema` precedent in `internal/schema/schema.go`). This is
  what makes the format a round-trip: a `Registry` -> JSON -> `Registry`
  cycle produces a Registry with the same `Lookup` behaviour, and a
  JSON -> `Registry` -> JSON cycle produces byte-identical output when
  the input was already canonical (signatures sorted by name; keys in
  the object order below).
- `func (r Registry) Save(path string) error` — a convenience wrapper
  that writes the canonical JSON to `path` with `0o644` permissions.
  Optional; internal callers can use `MarshalJSON` + `os.WriteFile`
  directly, but the shipped helper keeps the round-trip surface
  symmetrical (`Load` + `Save`) and gives file-format doc callers one
  entry per direction to point at.

### 1.4 An example / test fixture that mirrors the TCK's example set

A single fixture file under `internal/procsig/testdata/` (JSON) that
covers every TypeToken variant plus the nullable/non-nullable and
empty-params/empty-results combinations the TCK corpus attests. The
fixture drives the round-trip tests directly — the same file is loaded
once, re-serialised, and byte-diffed against the on-disk source, so a
drift in canonical encoding fails a test.

---

## 2. Format choice — why JSON

Three options were considered:

1. **JSON (chosen).** Standard library (`encoding/json`) — zero new
   deps. Already used pervasively across gqlc (`schema.Schema`,
   `query.Query`, `resolver.ValidatedQuery`, `graph.PropertyType`) for
   the same class of concern (deterministic canonical model
   serialisation). Extending fields is trivial (`omitempty` on new
   fields keeps old files loadable). Tooling story is free: any
   JSON-aware editor, `jq`, HTTP APIs, `diff`.

2. **YAML.** More readable for humans hand-editing signatures.
   Downsides: adds a first non-stdlib dep to this package (`gopkg.in/yaml.v3`
   or equivalent — currently zero YAML in gqlc's dependency graph),
   drags in tab/indent semantics, and has no ecosystem parity with the
   rest of the model's serialisation (schema, query, validated all
   render JSON). Rejected: the human-editing benefit does not clear
   the added-dep bar.

3. **Custom `.procsig` DSL that mirrors the TCK declaration syntax.**
   Highest fidelity to the surface a TCK author already types
   (`test.my.proc(in :: INTEGER?) :: (out :: STRING?)`). Downsides: a
   hand-rolled parser (the existing regex in
   `internal/query/cypher/acceptance_test.go` handles a permissive
   subset for test purposes; it is not a shippable loader — it
   silently accepts `test.my.proc()::()` variants, has no positional
   error reporting for CLI users, and would need a proper lexer to be
   robust). Adds a second dialect the codebase has to keep aligned
   with the TCK. Rejected: the surface is optimised for one keyboard
   moment (writing a `.feature`) and the on-disk file is written once
   and read machine-many-times; the CLI value is machine-load
   correctness, not typing speed.

The TCK declaration syntax stays canonical for `.feature` files
because godog already parses it there; the JSON format is the
equivalent surface for the same declarations outside a `.feature`.
Both surfaces target the same `procsig.Signature` type.

---

## 3. Wire schema

### 3.1 Top level

The top-level JSON object has exactly one required key, `signatures`,
whose value is an array of signature objects. Additional top-level
keys are rejected by the loader (unknown-field strict decoding — see
§4.2) so a typo in a new field surfaces immediately rather than being
silently dropped.

```json
{
    "signatures": [ ... ]
}
```

Rationale for the wrapper (versus a bare top-level array): the wrapper
leaves room for additive metadata (`"version"`, `"source"`) without a
format break. No fields other than `signatures` are defined today, but
the loader tolerates a `"version"` field with the value `1` for
forward-compat callers that want to pin a schema version; unknown
`"version"` values are rejected with a helpful error naming the
expected value.

### 3.2 Signature object

```json
{
    "name": "test.my.proc",
    "params":  [ ... ],
    "results": [ ... ]
}
```

Keys, in emission order:

- `name` (string, required, non-empty, must contain at least one dot —
  the fully-qualified-name rule `NewRegistry` already enforces).
- `params` (array of column objects, required — the empty array `[]`
  is valid, representing a signature with no parameters).
- `results` (array of column objects, required — the empty array `[]`
  is valid, representing a signature with no result columns; this is
  legal per `procsig` semantics, e.g. `test.doNothing() :: ()`).

Emission order is fixed for round-trip stability. `params` and
`results` slices preserve declaration order — the same ordering
`NewRegistry` receives from Go callers.

### 3.3 Column object

```json
{"name": "in", "type": "INTEGER", "nullable": true}
```

Keys, in emission order:

- `name` (string, required, non-empty, unique within its column list —
  the same rule `NewRegistry` enforces for params and results).
- `type` (string, required, one of `"INTEGER"`, `"FLOAT"`, `"STRING"`,
  `"NUMBER"` — the TCK token vocabulary the parser step also uses).
- `nullable` (boolean, required, always emitted). Nullability is a
  static parser-time fact (every TCK corpus signature marks every
  column nullable, but the format supports `false` for the general
  case — the same posture the Go type has).

Emission is always-emit for all three fields — matching the
`omit-when-zero-value` boundary the rest of the model applies: this
format is the surface a CLI user reads and edits; hiding
`"nullable": false` by convention would make hand-edited files
inconsistent about whether a missing key means "false" or "was
edited out". Explicit is safer.

### 3.4 Type token wire values

The uppercase TCK vocabulary, one-to-one with `procsig.TypeToken`
values:

| JSON `type` value | Go token         |
| ----------------- | ---------------- |
| `"INTEGER"`       | `TokenInteger`   |
| `"FLOAT"`         | `TokenFloat`     |
| `"STRING"`        | `TokenString`    |
| `"NUMBER"`        | `TokenNumber`    |

Any other value is a load error naming the offending token; the error
message identifies both the position (signature name + column name)
and the invalid token so the user can locate and fix the file.

The uppercase form matches the TCK surface deliberately: a user
copy-pasting a signature from a `.feature` file sees the tokens they
would type by hand.

---

## 4. Loader semantics

### 4.1 Delegation to `NewRegistry`

The loader is a thin wrapper: it decodes the JSON into an
intermediate wire type, translates every column's `type` string into a
`TypeToken`, builds the equivalent `[]procsig.Signature`, and calls
`NewRegistry` for the semantic validation. This keeps the file format
and the in-memory API check-set in strict lockstep — a rule
`NewRegistry` enforces (duplicate signature name, empty column name,
unqualified name, and so on) is caught for both surfaces by the same
code path, with the same message. If `NewRegistry` gains a new check
in the future, the file loader inherits it for free.

### 4.2 Strict / unknown-field rejection

`json.Decoder` is used with `DisallowUnknownFields()` so an unknown
key at the top level (`signatures` vs a typo `signaturs`) or inside a
signature or column object surfaces as a decode error rather than
being silently dropped. This is the same posture the format's
`omit-when-zero-value` boundary implies: the file is a
human-editable surface, and a typo that silently produces an empty
registry would waste debugging time downstream (an empty registry
generates `ErrUnknownProcedure` at parse time, which reads as a
parser bug, not a config bug).

### 4.3 Error messages

Loader errors wrap the underlying error (JSON decode error,
`NewRegistry` rejection, unknown token) with a prefix that names the
file path when known (`Load`) or `<stream>` (`Decode`). The wrapping
never uses `errors.Is`-style sentinels — the loader is a build-time
configuration parser, not a runtime error surface, so callers check
the message.

### 4.4 File extension and MIME

The conventional extension is `.procsig.json`; the double-dotted form
makes the file's role immediate (a procsig file, encoded as JSON) and
leaves room for a `.procsig.yaml` or a `.procsig` DSL surface without
a name collision. The loader does not enforce the extension — any
readable file whose contents parse under §3 loads.

### 4.5 Empty input

An empty `signatures` array yields the empty registry (all lookups
miss, no error) — the same result `NewRegistry(nil)` produces from
Go. This is the intentional zero: a caller can point a build at an
empty file and get a well-defined "no procedures known" registry,
with `CALL` in the query surface then raising `ErrUnknownProcedure`
at the parser's fail-site as designed.

An entirely empty file (zero bytes) is rejected with a helpful error
— zero bytes is more likely a truncation bug than a deliberate
"no procedures" declaration. A caller that wants "no procedures"
writes `{"signatures":[]}`.

---

## 5. Round-trip guarantees

### 5.1 In-memory round trip

For any `[]procsig.Signature` that `NewRegistry` accepts, the pipeline

```
sigs -> NewRegistry -> MarshalJSON -> Decode -> lookup
```

must produce a `Registry` whose `Lookup` behaviour is identical to the
original `NewRegistry(sigs)` for every input name — same set of
signatures registered, same params/results in the same order, same
name/token/nullable values. This is the loss-freeness invariant; a
round-trip test in `procsig_test.go` asserts it for a fixture that
covers every TypeToken variant.

### 5.2 Canonical byte-identity round trip

For any file that is already in canonical form (signatures sorted by
name; keys emitted in §3.2 / §3.3 order; no whitespace variations
beyond the encoder's default two-space indent), the pipeline

```
bytes -> Decode -> MarshalJSON -> bytes
```

produces byte-identical output. This is the canonical-form invariant;
a fixture in `internal/procsig/testdata/` is the source of truth for
the canonical form, and a test asserts the round-trip on that file.

### 5.3 Cross-surface round trip (TCK step -> JSON -> Registry)

A signature parsed by the existing godog step
(`parseProcedureSignature` in `acceptance_test.go`) and one loaded
from a JSON file must produce equivalent registries. Practically: a
`.feature` background line `test.my.proc(in :: INTEGER?) :: (out ::
STRING?)` and the JSON

```json
{
    "signatures": [{
        "name": "test.my.proc",
        "params":  [{"name": "in",  "type": "INTEGER", "nullable": true}],
        "results": [{"name": "out", "type": "STRING",  "nullable": true}]
    }]
}
```

both yield a Registry whose `Lookup("test.my.proc")` returns the same
`Signature` struct. The godog step keeps its regex loader (it needs to
consume the specific `.feature` grammar), but a spot-check test
constructs both, asserts equality, and locks the cross-surface
mapping.

---

## 6. Test plan

1. **Wire schema tests** — `TestMarshalJSONCanonical` on a
   Registry built from a hand-written fixture, comparing against the
   golden `.procsig.json` bytes.
2. **Round-trip tests** — `TestJSONRoundTripLossless` decodes the
   fixture, re-marshals, and asserts byte-equality against the source
   bytes; and `TestSemanticRoundTrip` builds a Registry, marshals,
   decodes, and asserts `Lookup` equality for every registered name.
3. **Loader rejection tests** — parameterised, covering: unknown top-
   level field, unknown signature field, unknown column field,
   unknown type token, empty file (zero bytes), malformed JSON, an
   unqualified signature name (round-tripped through `NewRegistry`'s
   check), a duplicate signature name, a duplicate column name.
4. **Cross-surface parity test** — assert
   `parseProcedureSignature("test.my.proc(in :: INTEGER?) :: (out ::
   STRING?)")` and `Decode(strings.NewReader(<equivalent JSON>))`
   produce structurally-equal `Signature` values.
5. **Path loader test** — `TestLoadFromFile` writes a temp file,
   loads it, asserts `Lookup`, and covers the file-open failure path
   (non-existent path) via a table-driven negative case.

Fixtures live in `internal/procsig/testdata/`. The `testdata/` name
is the standard Go convention that keeps them off the compile path.

---

## 7. What's out of scope

- **Wiring the loader into `main.go`.** This bead is the format +
  loader; the CLI entry point that consumes the loader is a follow-up
  (codegen already accepts `procsig.Registry` values via its
  `Input`; a downstream bead can add a `-procsig <path>` flag when
  there is a real CLI to bolt it onto). Filed as gqlc-6rz's residual
  if the current session does not spawn it.
- **A YAML surface.** Reject-and-revisit: adding a second surface has
  a strictly larger maintenance cost than the readability gain, and
  the JSON surface is friendly enough for hand-editing today.
- **A `.procsig` DSL surface.** Same reasoning; the TCK declaration
  syntax stays the source of truth for `.feature` files, and the
  JSON surface serves everywhere else.
- **Versioning.** The `"version"` field is accepted at value `1` for
  forward-compat but not required; a future incompatible format
  change would introduce a required `"version": 2` and gate the
  loader on it. Today the format is v1 by default.
