# Spec — multi-target config: one schema file, many generated packages

The implementation brief for the change ADR 0013 records: the config
file grows from one generation target to a list of them, and the
loader, the pipeline, the output-write protocol and `gqlc init` follow.
The wire shape, the `graph` key name, the per-entry `schema`, the
version-1 breaking change, the fail-fast-per-entry error posture, and
the `init --add` flag are fixed by ADR 0013 and are not re-argued here.
The design points this spec owns are the document scan and what it keeps
off the version probe (§4), the old-flat-shape detection (§4.2), the
output-overlap rule and its honest limits (§4.3), the entry-prefix rule
(§4.1), the widened pipeline contract (§6), the two-phase write and its
seam (§7), and the branch split that keeps master coherent (§9).

Tracking: epic `gqlc-0gb`, with children `gqlc-0gb.1` (this spec),
`gqlc-0gb.2` (config loader), `gqlc-0gb.3` (pipeline and the write
protocol), `gqlc-0gb.4` (`init --add`). Lands as a four-branch stack in
that order.

---

## 1. Purpose and scope

A **generation target** is one entry of the config file's `graph`
sequence: one schema, one query directory, one language pair, and one
Go package written into one output directory. `gqlc generate` runs
every target the file declares, in document order. The term names the
config-file entry throughout this spec and every message it pins;
"typed repository" (ADR 0010) stays the name of the generated artifact,
of which one target produces a package-full.

In scope: `internal/config` (wire shape, loader, canonical form),
`internal/cli/pipeline` (run every target), `internal/cli` (write
protocol, `init`). Out of scope: `internal/codegen`, `internal/
resolver`, and the front end, none of which learn that targets exist —
each target drives them exactly as the single-target pipeline does
today.

## 2. Wire schema (version 1)

A YAML mapping with two keys. Nested levels are described in §2.2 and
§2.3; the order of every table is the canonical `Save` emission order
(§5).

### 2.1 Document

| # | key       | type     | required | valid values | semantics                                                                 |
|---|-----------|----------|----------|--------------|---------------------------------------------------------------------------|
| 1 | `version` | int      | yes      | `1`          | on-disk format version; wire-only, never part of `Config`; must be a true YAML integer scalar (`!!int`), never coerced (config-file-format §5, §6.2) |
| 2 | `graph`   | sequence | yes      | ≥ 1 entry    | the generation targets, in order (`Config.Targets`); an empty sequence is rejected |

### 2.2 Entry (`graph[i]`)

| # | key               | type    | required | valid values                 | semantics                                                              |
|---|-------------------|---------|----------|------------------------------|-------------------------------------------------------------------------|
| 1 | `schema`          | string  | yes      | non-empty                    | path to this target's schema file (`Target.SchemaPath`)                 |
| 2 | `schema_language` | enum    | yes      | `gql`                        | language the schema file is written in (`Target.SchemaLang`)            |
| 3 | `queries`         | string  | yes      | non-empty                    | directory holding this target's query files (`Target.QueryDir`)         |
| 4 | `query_language`  | enum    | yes      | `opencypher`                 | language the query files are written in (`Target.QueryLang`)            |
| 5 | `procsig`         | string  | no       | non-empty when present       | path to a procedure-signature registry file (`Target.ProcsigPath`); omit the key when unused — an explicit `""` is rejected, a null value equals omission |
| 6 | `gen`             | mapping | yes      | see §2.3                     | the code-generation block for this target                               |

Entries are independent: two targets may name the same schema file,
the same query directory, or the same package name. The one cross-entry
rule is §4.3.

### 2.3 `gen` and `gen.go`

`gen` has exactly one key, `go`, required, a mapping. The level exists
because a second generated language would be a sibling key here, which
is where sqlc puts it; today any other key under `gen` is an unknown
key and rejects.

| # | key       | type   | required | valid values                 | semantics                                                                      |
|---|-----------|--------|----------|------------------------------|----------------------------------------------------------------------------------|
| 1 | `package` | string | yes      | a valid Go identifier        | generated package name (`Target.Go.Package`); `go/token.IsIdentifier`, so Go keywords are rejected; casing is not policed |
| 2 | `out`     | string | yes      | non-empty; §4.3              | directory generated code is written to (`Target.Go.Out`), owned exclusively by gqlc (ADR 0012) |
| 3 | `driver`  | enum   | yes      | `neo4j-go-v5`, `neo4j-go-v6`, `apache-age-pgx-v5` | client library the generated code targets (`Target.Go.Driver`)                  |

The three enum axes keep their exported Go types (`SchemaLang`,
`QueryLang`, `Driver`), their constants, and their `XxxValues()`
functions unchanged: they remain the single source of truth shared by
loader messages and `gqlc init` prompts.

### 2.4 Worked example

The canonical fixture, `internal/config/testdata/canonical.gqlc.yaml`
(§5), is the motivating project: one schema, two modules, two
packages, one driver version each.

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

## 3. In-memory shape

```go
// Config is the canonical in-memory form of a config file: the
// generation targets it declares, in document order.
type Config struct {
	Targets []Target
}

// Target is one generation target: one schema and query directory
// front-ended into one generated Go package.
type Target struct {
	SchemaPath  string
	SchemaLang  SchemaLang
	QueryDir    string
	QueryLang   QueryLang
	ProcsigPath string
	Go          GoGen
}

// GoGen is a target's gen.go block.
type GoGen struct {
	Package string
	Out     string
	Driver  Driver
}
```

Field order per struct is wire order per level. There is still no
`Version` field and no path resolution: `Config` carries the raw
strings the file holds, and relative paths are resolved against the
config file's directory by the CLI (config-file-format §4, unchanged).

`Load`, `Decode`, `Save` and `Canonical` keep their signatures.

## 4. Loader semantics

Everything config-file-format §6 pins holds unchanged except where
this section states otherwise: the `config: ` prefix on every message,
no error sentinels, the `fs.ErrNotExist` wrap on open failures, the
tag-strict version probe, `KnownFields(true)` on the v1 decode
(recursive: an unknown key inside an entry or inside `gen.go` rejects
with the same shape), null-equals-omission for every key, and
omitted-versus-empty distinguished by pointer-typed wire fields at every
scalar key.

The wire structs are `wireV1` (document), `wireTarget`, `wireGen` and
`wireGo`. Their names reach users through yaml's unknown-key messages
(§4.5). `wireV1.Graph` is a plain `[]wireTarget`, not a pointer: the
scalar keys need one because the strict decode is the only pass that
sees them, but `graph`'s absent, null, non-sequence and empty cases are
all settled by the document scan below, before the strict decode runs.
A pointer there would be a distinction nothing reads.

**The document scan.** The version probe is **unchanged**. It stays the
lenient pass whose only job is reporting the version before any shape
complaint (config-file-format §5; config.go `decode`). Immediately
after the version check passes, the loader parses the same bytes into an
untyped node:

```go
var doc yaml.Node
if err := yaml.Unmarshal(body, &doc); err != nil {
	return Config{}, fmt.Errorf("config: %s: %w", src, err)
}
root := doc.Content[0]
```

Three checks read that one scan before the strict decode: the
old-flat-shape detection (§4.2), `graph`'s presence and kind (§4.5),
and the null-entry rejection (§4.4). A fourth outlives it — the count
invariant below runs after the decode against the node the scan
kept. A node decode is type-tolerant by construction — every YAML
document decodes into a `yaml.Node` — so none of the three can degrade
into a library-worded complaint about a shape the loader has not yet
described in its own words.

Two properties make the scan safe to write without shape guards of its
own, both consequences of running it *after* the version check:

- **`root` is a mapping.** The version probe is a struct decode, so a
  sequence or scalar root has already failed it (`cannot unmarshal
  !!seq into config.versionProbe`), and a document with no content at
  all — empty, or comments only — has already failed the
  `version`-omitted check. Reaching the scan means `version` was found
  as an `!!int` inside a mapping.
- **The parse cannot fail here.** The version probe ran the same bytes
  through the same parser, so malformed YAML surfaced there. The error
  is wrapped rather than dropped because unreachable is not the same as
  impossible, but no input reaches it.

**Every kind or tag test the scan makes resolves aliases first.** An
alias node (`*anchor`) carries an empty `Tag` and `Kind ==
yaml.AliasNode`; the node it names is on `.Alias`. A test written
against the alias node itself therefore sees neither the tag nor the
kind of the value the document actually supplies, and passes it. The
rule, stated once here and inherited by §4.2, §4.4 and stage 4 of §4.6:
follow `.Alias` until the node is not an alias, test the resolved node,
and **report the alias node's own `Line`**. The resolved node's `Line`
is where the anchor was written, which is not where the mistake is.

The loop iterates at most once: anchoring an alias (`&y *x`) is a YAML
parse error in every syntax, so `.Alias` is never itself an alias. It is
written as a loop because a single dereference that assumes that is a
silent bet on it; no fixture can cover a second iteration.

Two positions in the `graph` sequence take an alias, and the rule covers
both:

- **An element** — `- *none` where `none` anchors a null. Unresolved,
  its `Tag` is `""`, so §4.4's `!!null` test passes it; yaml.v3 then
  drops it exactly as it drops a written `~`, and every later entry's
  `graph[i]:` index silently renumbers.
- **`graph`'s value** — `graph: *g`. The consequence here is bounded:
  the sequence still decodes, so no entry is lost. What is wrong is only
  the message. An unresolved kind test sees `alias` and reports "got a
  YAML alias" for a document whose `graph` is a scalar.

**Resolving aliases is not the same as rejecting them.** An element
aliasing a mapping (`- *t`) resolves to `!!map` and decodes into a full
entry, and the scan must leave it alone. The rule changes what the scan
*looks at*, never what it rejects.

That it survives the scan is not a promise that it loads. An alias
copies the whole mapping, `gen.go.out` included, so `- *t` naming an
earlier entry produces two targets with identical output directories and
§4.3 rejects the file. No spelling escapes that, because the anchor must
precede the alias and every position it can occupy is already answered:

| anchor position | outcome |
|-----------------|---------|
| an earlier entry (`- &t`) | decodes, both entries carry the same `out`, §4.3 rejects |
| a sub-node (`- *gen`) | `field go not found in type config.wireTarget` |
| an unknown top-level key (`base: &base`) | `field base not found in type config.wireV1` |
| the document root mapping (`&root` before the first key) | resolves to `!!map`, so the scan leaves it; `field version not found in type config.wireTarget` |

**An element aliasing an entire earlier entry can never produce a
loadable second target.**

**Key lookup stays literal**, and merge keys (`<<: *anchor`) divide by
position the same way aliases do:

- **On an entry** — `- <<: *base`. Not a gap in anything. yaml.v3
  expands `<<:` *before* field matching, so the element is a mapping to
  the scan and a full entry to the strict decode;
  `KnownFields(true)` raises nothing, and the count check agrees.
- **At the document root** — `<<:` taking an inline mapping or an
  anchor, either way supplying keys the scan cannot see, since `root`'s
  literal keys are `version` and `<<`. Two sub-cases, and they are not
  equally benign:
  - Injecting a **former flat key**: §4.2 looks for `schema` among the
    literal keys, does not find it, and its targeted message does not
    fire. The file is still rejected — the strict decode's unknown-key
    wall reports `field schema not found in type config.wireV1` at the
    line the key is written on — so only the wording is lost.
  - Injecting **`graph` itself**: stage 4 finds no `graph` and reports
    `missing required field "graph"` for a document that declares one
    and would otherwise have decoded into a valid target. That is a
    false rejection whose message contradicts the file, not a
    downgraded wording.

  Both are accepted rather than fixed. §4.2 exists for files written
  before this change and those files have literal keys; the second case
  costs a wrong message on a document nobody writes, and the file is
  rejected either way, so nothing is generated from a config gqlc
  misread.

Chasing merge keys during the scan to close that would mean
reimplementing yaml.v3's merge resolution against the node tree, for a
document nobody writes.

**The scan's element count outlives it.** The loader holds the `graph`
sequence node past the strict decode and checks it against the decoded
length through one unexported helper:

```go
// checkEntryCount reports the §4 count invariant: the decoded target
// count must equal the number of elements the scan saw. No input is
// known to violate it, so the test drives this directly.
func checkEntryCount(graphSeq *yaml.Node, decoded int) error
```

A mismatch means yaml.v3 dropped an element the scan saw, and is an
internal error naming both counts:

```
config: <src>: internal: "graph" declares <n> entries but <m> decoded; the entry indices in any further message would be wrong
```

This is not redundant with §4.4. §4.4 rejects the drop causes the spec
knows about; the count check states the property those rules exist to
preserve. Enumerating yaml.v3's drop paths is an enumeration that is
correct until the next one is found — which is how the alias case above
was found. Counting is a property, and it cannot go stale.

Rejected: giving the version probe a typed `graph` field to get the
same three answers out of one pass. A typed field makes the probe
type-strict about `graph`, which is precisely what the probe must not
be: `version: 2` with `graph: nope` then fails with ``cannot unmarshal
!!str `nope` into []yaml.Node`` *before* the version check, so a v2 file
stops reporting its version, the §4.5 `graph`-not-a-sequence message can
never fire, and a library type leaks into user-facing copy. The probe
guards the version seam and nothing else.

### 4.1 The entry prefix

Every message the loader itself formats about one entry carries a
`graph[i]: ` prefix after the source label, whatever the entry count —
`config: gqlc.yaml: graph[1]: missing required field "queries"`. The
prefix is unconditional by design: a shape that varies with the entry
count is one more thing to test and one fewer thing to grep for.

Messages yaml.v3 formats — unknown key, duplicate key, wrong scalar
type, and the enum errors raised from `UnmarshalYAML` — carry no
prefix. They carry `line <L>` instead, which localises the fault
better than an index does. The split is not an accident of the library:
an error about an *absent* key has no node and therefore no line, so
the index is all there is; an error about a *present* node has a line
and does not need one. Where the loader has both, it prints both, index
first (`graph[1]: line 12: entry is null`).

Rejected: typing the enum wire fields as `*yaml.Node` and validating
them post-decode, purely to get the index onto enum errors. It moves
three vocabularies' validation away from the types that own them and
buys a locator the message already has.

### 4.2 The old flat shape

A file written for the previous format declares `version: 1`
truthfully, so the version probe passes it and the strict decode
reports one unknown-key line for every top-level key except `version`,
which still resolves. The result names each key the file happens to
carry and never says the format changed. The loader detects the shape
instead.

**Rule.** From the document scan, before the strict decode: if `root`
has no `graph` key and carries at least one of the eight former
top-level keys — `schema`, `queries`, `output`, `package`,
`schema_language`, `query_language`, `driver`, `procsig` — the loader
reports the first of those keys in that order, at the line of its **key
node** (the key's line, not the value's, so a block or multi-line value
still points at the key the user must remove):

```
config: <src>: line 2: "schema" is not a top-level key; version 1 declares a "graph" sequence of generation targets, each carrying its own schema, queries, and gen.go block
```

The key list is frozen — it is the version-1-flat wire vocabulary, and
nothing will be added to it. A document that has both `graph` and a
stray top-level `queries` is not this case: it is a new-format file
with a leftover key, and the unknown-key error says so.

The message names no subcommand. `internal/config` stays CLI-agnostic
(config-file-format §1, §8), so the hint that `gqlc init` can rewrite
the file is not the loader's to give — and adding an error sentinel so
the CLI could give it would break the package's no-sentinel posture for
one line of copy.

### 4.3 Output-directory overlap

Two targets whose output directories overlap destroy each other's
output: the later target's ADR 0012 wipe deletes what the earlier one
just wrote. The loader rejects overlap.

**Rule.** For each entry in order, compared against every earlier
entry, `filepath.Rel` is run **both ways** and the first direction that
answers wins. On `filepath.Rel(earlier.Out, later.Out)`:

| result                                        | verdict                          |
|-----------------------------------------------|----------------------------------|
| `.`                                           | the same directory — reject      |
| `..`, or a path prefixed `../`                | fall through to the reverse      |
| any other path                                | later is inside earlier — reject |
| an error                                      | fall through to the reverse      |

Then on `filepath.Rel(later.Out, earlier.Out)`:

| result                                        | verdict                          |
|-----------------------------------------------|----------------------------------|
| `.`                                           | the same directory — reject      |
| `..`, or a path prefixed `../`                | disjoint — accept                |
| any other path                                | later contains earlier — reject  |
| an error                                      | not comparable — accept (below)  |

The two directions are distinct verdicts, not one verdict computed
twice: `internal/db/sub` then `internal/db` is the later entry
*containing* the earlier, and reporting it as "is inside" would state
the containment backwards.

The disjointness test is on a **path component**, not a string prefix:

```go
rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
```

Testing `strings.HasPrefix(rel, "..")` instead would be wrong, and
wrong in the unsafe direction. `filepath.Rel("internal/db",
"internal/db/..foo")` returns `"..foo"` — a directory *inside*
`internal/db`, whose relative path begins with the two characters `..`.
A string-prefix test accepts that pair as disjoint and lets exactly the
nested configuration through that this check exists to reject.
`"..foo/x"` is the same trap one level deeper.

**Escaping bases are rebased first.** `filepath.Rel` refuses a base
that escapes its own root — `Rel("..", "a")` errors, because how far
`..` is from `a` depends on where the working directory sits — and the
reverse direction returns the escaping `"../.."`. Both arms therefore
fall through and a plain containment reads as disjoint, which is the
permanent-failure mode of §7.2 rather than a cosmetic miss. So when
both operands are relative, they are first rebased onto a shared
synthetic absolute directory with as many segments as the deeper
operand has leading `..` components, and the two joined paths are
compared instead:

```go
depth := max(leadingParents(a), leadingParents(b))   // "../.." -> 2
base  := "/" + anchor0 + "/" + ... + anchorDepth-1
a, b   = filepath.Join(base, a), filepath.Join(base, b)
```

The anchor segments carry a NUL byte, which puts them outside the names
a real directory can carry, so an `out` naming a directory cannot spell
one. That is a statement about filesystems, not about YAML: yaml.v3
rejects a *literal* NUL in the stream but decodes the `\0` and `\x00`
escapes of a double-quoted scalar into real NUL bytes, so a colliding
`out` is expressible — just not resolvable to a directory. The bound
that matters is the direction of the error: an operand naming an anchor
segment can only add structure *under* the synthetic base, turning a
disjoint pair into a rejected one and never the reverse, so no overlap
escapes through a collision. The loader does not validate against NUL;
a check for a path no filesystem could hold would exist only to prop up
the sentence above it.

Anchoring stays pure string manipulation — the base is fictional and
nothing is resolved against the filesystem — and it introduces no
inexactness of its own. A pair that overlaps under every working
directory is rejected; a pair that overlaps under none is accepted; a
pair that overlaps under some working directories and not others is
accepted, and the two ways that arises are the two limit classes
below. Only an operand spelling an anchor segment departs from this,
and only by adding a rejection — the direction bounded above. Anchoring
therefore decides strictly more pairs than a bare `Rel` without
deciding any of them wrongly; a mixed absolute/relative pair is left
unanchored, as one of those two classes.

`filepath.Rel` cleans both operands, so `internal/db`,
`internal/db/`, `./internal/db`, `internal//db` and
`./a/../internal/db` are one directory to this check, and
`internal/db` against `internal/dbgen` is correctly two. It is pure
string manipulation with no filesystem access, so config-file-format
§4's rule — the loader resolves no paths and stats nothing — survives
intact. `Config` stores the raw strings; only the comparison sees
cleaned ones.

**The rule is exported.** `internal/cli` needs it too: `init --add`
validates a proposed `out` against the existing entries at the prompt
(§8.2), and a `Validate` hook in `internal/cli` cannot reach an
unexported helper here. Rather than let the CLI carry a second copy of a
rule with a known sharp edge (the `..foo` trap above), `internal/config`
exports the check the loader itself uses:

```go
// CheckOutAgainst reports whether out may be added to c as a new
// generation target's output directory. It returns nil when out overlaps
// no existing target's, and otherwise an error naming the first target
// it overlaps and which way — the §4.5 cross-entry text, unprefixed, so
// the loader can prefix it and a huh Validate hook can render it bare.
// The comparison is the loader's own: lexical, filesystem-free, and
// subject to the same limit.
func (c Config) CheckOutAgainst(out string) error
```

The loader's cross-entry sweep is this function applied to each entry
against the entries before it, so there is exactly one implementation of
overlap in the tree. It joins `SchemaLangValues()`, `QueryLangValues()`
and `DriverValues()` as vocabulary `internal/cli` borrows rather than
restates.

**The honest limits.** What escapes is exactly the pairs whose relation
depends on a name the loader cannot see, and there are two classes:

- **An absolute path against a relative one.** `Rel` cannot relate them
  in either direction without the working directory, so the sweep
  accepts them as disjoint even when they name one directory.
- **An escaping path that re-enters through the working directory's own
  name.** `../b/db` and `db` are one directory when the working
  directory is itself named `b`, and two directories otherwise. Rebasing
  cannot decide it, because the answer is in a name neither path
  states.

Symlinked aliases, two paths differing only by case on a
case-insensitive filesystem, and bind mounts escape too — every one
needs the filesystem, which a loader that is a pure function of the
file's bytes will not touch.

What escapes costs more than a few files, and the spec should not
pretend otherwise:

- An aliased pair naming the **same** directory yields one directory
  holding two targets' output. Files whose names collide (`models.go`)
  end up as the later target's, so the earlier target's package is never
  intact; its non-colliding files survive only on alternating runs (phase
  A computes both wipe lists before either commit runs — §7.2). On those
  runs the directory carries two `package` clauses and does not compile.
  The run itself succeeds (§7.2 step 7).
- An aliased pair where one **contains** the other reproduces exactly
  the permanent failure this check exists to prevent: the parent's sweep
  finds a subdirectory it cannot prove marked and aborts, on this run
  and every run after it.

The invariant that does survive: no hand-written file is ever deleted.
The tripwire refuses to delete anything it cannot prove gqlc wrote,
whatever the loader missed.

### 4.4 Null entries

yaml.v3 drops a null sequence element (`- ~`, or a bare `-`) rather
than decoding it into the zero struct, so a document with a null entry
decodes to a shorter slice and every later entry's index in `Config`
shifts away from the index the error messages print. The loader rejects
it from the document scan, before the strict decode: the sequence node's
`Content` preserves every element, so a null entry has an index and a
line to report.

**Rule.** For each element of `root`'s `graph` sequence in `Content`
order, an element whose **resolved** `Tag` (§4) is `!!null` — the
spellings `~`, `null`, `Null`, `NULL` and an empty value all carry that
tag — is reported by its `Content` index and the element's own line:

```
config: <src>: graph[2]: line 5: entry is null
```

Resolving matters because an alias to a null anchor (`- *none`) is
dropped exactly as a written null is, while carrying an empty `Tag` of
its own. The line reported is the alias's, not the anchor's: the
anchor may be a legitimate value used elsewhere, and the fault is the
element that names it here.

An element that is a non-null scalar (`- ""`, tag `!!str`) is not this
case: it is a present entry of the wrong type, and the strict decode
says so with its own line. An element that resolves to a mapping is not
this case either, however it was written (`- *t`, `- <<: *base`, `- {}`)
— it is an entry, and the entry-level rules judge it.

### 4.5 Error catalogue

Every message the loader can produce, with `<src>` a file path or
`<stream>`. Rows unchanged from config-file-format §6.3 are marked
*(unchanged)*; the rest are new or reshaped by the nesting.

Document level:

| condition                                   | message shape                                                                          |
|---------------------------------------------|-----------------------------------------------------------------------------------------|
| file open failure                           | `config: open <path>: <os error>` (wraps, so `errors.Is` works) *(unchanged)*           |
| stream read failure                         | `config: read <src>: <error>` *(unchanged)*                                             |
| zero-byte input                             | `config: <src> is empty (expected a gqlc config declaring version: 1)` *(unchanged)*    |
| malformed YAML                              | `config: <src>: yaml: ...` *(unchanged)*                                                |
| document is not a mapping                   | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> ... into config.versionProbe` *(unchanged)* |
| `version` omitted (or null)                 | `config: <src>: missing required field "version" (this gqlc supports version 1)` *(unchanged)* |
| `version` not a `!!int` scalar              | `config: <src>: line <L>: field "version" must be a YAML integer (got !!float "1.5")` *(unchanged)* |
| `version` a `!!int` that overflows Go `int` | ``config: <src>: field "version": yaml: unmarshal errors: line <L>: cannot unmarshal !!int `9223372...` into int`` *(unchanged)* |
| `version` ≠ 1                               | `config: <src>: declares version <v>; only version 1 is supported` *(unchanged)*        |
| old flat shape (no `graph`, a former top-level key present) | `config: <src>: line <L>: "<key>" is not a top-level key; version 1 declares a "graph" sequence of generation targets, each carrying its own schema, queries, and gen.go block` |
| `graph` omitted (or null)                   | `config: <src>: missing required field "graph"`                                        |
| `graph` present but an empty sequence       | `config: <src>: line <L>: field "graph" must not be empty; declare at least one generation target` |
| `graph` is not a sequence                   | `config: <src>: line <L>: field "graph" must be a sequence of generation targets (got a YAML <kind>)` |
| a null entry                                | `config: <src>: graph[<i>]: line <L>: entry is null`                                   |
| an entry is not a mapping                   | ``config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal !!str `x` into config.wireTarget`` |
| decoded entry count ≠ scanned entry count   | `config: <src>: internal: "graph" declares <n> entries but <m> decoded; the entry indices in any further message would be wrong` |

`<kind>` is the **resolved** kind (§4), so `graph: *g` aliasing a scalar
reports `got a YAML scalar`, naming what the document supplies rather
than how it was spelled. Every `<L>` in these two rows follows §4's
other half — the line of the node as written, so the alias's own line
when `graph`'s value is an alias, and otherwise the sequence node's
line, which is the line carrying `[]` for both `graph: []` and a `[]`
written under it.

The five rows from "old flat shape" through "a null entry" are the
loader's own, formed from the document scan (§4) before the strict
decode. None of them can be reached by a yaml.v3 type error about
`graph`, which is the point of the scan. The count row is the scan's
too, but it is the only one raised *after* the decode, and the only one
no config file should be able to produce: it fires when yaml.v3 dropped
an element the scan saw and no §4.4 rule caught it, and its wording says
so rather than blaming the document.

Entry level. Every row is prefixed `config: <src>: graph[<i>]: `,
elided below; nested keys are named by their dotted path:

| condition                          | message shape                                                                     |
|------------------------------------|-------------------------------------------------------------------------------------|
| required key omitted (or null)     | `missing required field "<key>"` — one of `schema`, `queries`, `gen`, `gen.go`, `gen.go.package`, `gen.go.out` |
| required enum key omitted          | `missing required field "<key>" (valid values: <list>)` — `schema_language`, `query_language`, `gen.go.driver` |
| path/package key present but empty | `field "<key>" must not be empty`                                                   |
| `procsig` present but empty        | `field "procsig" is empty; omit the key when no procsig file is used`               |
| `gen.go.package` not a Go identifier | `package "<val>" is not a valid Go identifier`                                    |

Entry-level errors yaml.v3 formats, unprefixed (§4.1):

| condition                | message shape                                                                        |
|--------------------------|----------------------------------------------------------------------------------------|
| unknown key              | `config: <src>: yaml: unmarshal errors: line <L>: field <key> not found in type config.<wireV1\|wireTarget\|wireGen\|wireGo>` |
| duplicate key            | `config: <src>: yaml: unmarshal errors: line <L>: mapping key "<key>" already defined at line <M>` |
| non-scalar path/package  | `config: <src>: yaml: unmarshal errors: line <L>: cannot unmarshal <tag> into string` |
| non-scalar enum value    | `config: <src>: line <L>: invalid <key>: expected a scalar value, got a YAML <kind>` |
| invalid enum value       | `config: <src>: line <L>: invalid <key> "<val>" (valid values: <list>)`              |

Cross-entry (§4.3), prefixed with the *later* entry's index:

| condition                | message shape                                                                                   |
|--------------------------|---------------------------------------------------------------------------------------------------|
| two entries share `out`  | `config: <src>: graph[<j>]: out "<p>" is already graph[<i>]'s output directory` |
| the later entry's `out` is inside an earlier one's | `config: <src>: graph[<j>]: out "<p>" is inside graph[<i>]'s output directory "<q>"` |
| the later entry's `out` contains an earlier one's  | `config: <src>: graph[<j>]: out "<p>" contains graph[<i>]'s output directory "<q>"` |

All three are `CheckOutAgainst`'s error text (§4.3) with the loader's
`config: <src>: graph[<j>]: ` prefix in front and nothing else added.
Keeping the loader's wording and the prompt's byte-identical is why the
function exists: a
trailing "each generation target must own its own" would read as
justification in the config error and as noise at the `init --add`
prompt (§8.2), and having it in one place and not the other is how one
message becomes two.

### 4.6 Check order

Checks run in stages and the loader reports the first stage that fails:

1. the version probe, unchanged (so a v2 file reports its version, not
   a shape complaint);
2. the document scan — one `yaml.Unmarshal` into a `yaml.Node`, which
   cannot fail here (§4);
3. the old-flat-shape check (§4.2, before the strict decode, so the
   targeted message beats the unknown-key wall);
4. `graph` present, non-null, a sequence, and non-empty — in that
   order, from the scan, every kind and tag test resolved through
   `.Alias` per §4;
5. the null-entry check (§4.4), which needs stage 4's verdict first:
   there are no elements to index until `graph` is known to be a
   sequence;
6. the strict v1 decode;
7. the count invariant (§4) against the sequence node stage 4
   accepted — an internal error, and the only stage that can fail with
   every earlier stage passing;
8. per-entry post-decode checks, entries in index order and each
   entry's keys in the §2.2/§2.3 wire order;
9. the cross-entry `out` sweep (§4.3), reporting the first overlap in
   `(later, earlier)` index order.

Stages 3–5 are three reads of the one node tree from stage 2, not three
parses. Stage 7 is why stage 4 keeps that node rather than discarding it
after answering: every message stages 8 and 9 print is indexed, and
stage 7 is what proves those indices refer to the entries the file
declares.

Within the strict decode, ordering is not document order and is
yaml.v3's, exactly as config-file-format §6.3 describes: a
custom-unmarshal failure (an invalid enum) aborts the decode
immediately, while unknown-key, duplicate-key and wrong-type errors
accumulate and surface together only if the decode otherwise completes.

## 5. Canonical `Save` form

`Save` writes `version: 1`, then `graph`, then each target in order
with the keys of §2.2 and §2.3, `procsig` omitted when
`ProcsigPath` is empty; two-space indent, yaml.v3's plain scalars,
sequence items indented two spaces under `graph:`, a trailing newline,
file mode `0o644`. `Canonical` returns those bytes without writing;
`Save` is `Canonical` plus `os.WriteFile`, unchanged.

The §2.4 example is exactly `Canonical`'s output for its `Config` and
becomes the new `internal/config/testdata/canonical.gqlc.yaml`,
pinned byte-for-byte by `TestSaveEmitsFixtureBytes`. It is deliberately
a two-entry document with one `procsig` present and one absent and a
different driver per entry: the fixture that pins the nesting, the
sequence indentation, the `omitempty` behaviour inside a sequence
element, and the per-entry axis in one file.

For any `Config` the loader would accept, `Load(Save(c))` returns `c`
exactly and saving a loaded canonical file reproduces its bytes.

`Canonical` validates nothing, and a zero `Config` emits `graph: []` —
which `Load` rejects with the empty-sequence message (§4.5), the
accurate complaint about a config that declares no targets. That holds
because `wireV1.Graph` is a plain `[]wireTarget` (§4): a nil slice
marshals to `[]`. It would **not** hold for a `*[]wireTarget`, where a
nil pointer marshals to `graph: null` and `Load` would answer `missing
required field "graph"` — a message that says the key is absent about a
`Config` whose field is merely empty. The value type is what makes the
sentence above true rather than a coincidence of how `Canonical` happens
to populate the field, and it is one more reason the pointer is wrong
here. Callers that assemble a `Config` in memory own its validity (§8).

## 6. Pipeline contract

`internal/cli/pipeline` runs every target in document order.

```go
// Result is what a clean or diagnostic-accumulating run yields: one
// TargetResult per generation target the config declares, in document
// order, and the ordered diagnostic lines from every target's
// front-end walk.
//
// Caller invariant, non-negotiable: Targets is non-nil iff
// Diagnostics is empty AND err is nil. Every other outcome is the
// zero Result. Callers MUST NOT write any TargetResult when
// len(Diagnostics) > 0; that state means "errors accumulated, every
// batch discarded". Ignoring the invariant lets the ADR 0012 tripwire
// wipe marked output directories to write zero files.
type Result struct {
	Targets     []TargetResult
	Diagnostics []string
}

// TargetResult is one target's generated batch and the resolved
// directory the caller writes it to.
type TargetResult struct {
	Files  []codegen.File
	OutDir string
}
```

`Run(cfgPath string) (Result, error)` keeps its signature and its
subcommand-agnostic posture, `ErrConfigMissing` included. Return
contract, exhaustive:

- `err != nil` → `Result` is the zero value. This covers the stage-1
  config failures (`ErrConfigMissing` and every other `config.Load`
  error) and every per-target setup failure, which is wrapped
  `graph[<i>]: ` (§6.1).
- `err == nil, len(Diagnostics) > 0` → front-end accumulation.
  `Targets` is nil. The caller prints each line and returns its own
  summary error (CLI-1 §2.3); nothing is written.
- `err == nil, len(Diagnostics) == 0` → success. `Targets` has exactly
  one entry per config target, in document order; each `Files` is
  non-nil and sorted by `Path`, each `OutDir` is resolved against
  `filepath.Dir(cfgPath)`.

No other combinations exist; the caller may rely on this.

The `OutDir`-on-the-error-path clause of the single-target contract is
gone. It was never read — the CLI returns on a non-nil error — and with
N targets a partially populated `Targets` on a failed run is a
wipe-and-write footgun the zero value removes by construction.

### 6.1 Per-target stages and failure classes

For each target `i`, the pipeline runs CLI-1 §3.1 stages 3–8 against
that target's fields, with stage 2's path resolution applied to
`SchemaPath`, `QueryDir`, `ProcsigPath` and `Go.Out`. Every singular
failure — unmapped axis, schema read, schema parse, procsig load, empty
query directory, codegen — is wrapped `fmt.Errorf("graph[%d]: %w", i,
err)` and aborts the whole run at that entry, leaving the messages
CLI-1 §2.3 catalogues otherwise verbatim:

```
graph[1]: schema internal/order/schema.gql: unknown node type "Order"
graph[2]: no query files (*.cypher) in internal/audit/query
graph[0]: internal: no pipeline mapping for driver "neo4j-go-v7"
```

Stage 7 diagnostics accumulate across every target as they do across
every file, each line prefixed with its entry:

```
file-diag  = graph[<i>]: <path>: <message>
query-diag = graph[<i>]: <path>: query <Name>: <message>
```

Two consequences, stated rather than engineered away:

- **A setup failure discards the diagnostics accumulated before it.**
  If target 0 accumulates query diagnostics and target 1's schema is
  unreadable, the run reports the schema error alone; the query
  diagnostics resurface on the next run. A contract in which both a
  non-nil error and diagnostics can be populated buys the user one
  round-trip and costs every caller a fourth branch.
- **Stage 8 is skipped once any diagnostic exists.** The batch is
  discarded wholesale anyway (§6.2), so generating it could only
  produce a codegen error for a run that is already failing.

### 6.2 All-or-nothing

Any diagnostic from any target means nothing is written for any target.
This is the single-target invariant unchanged in spirit: the batch is
written only when the whole run is clean, with "the run" now spanning
every target the file declares.

### 6.3 No schema-parse cache

Each target parses its own schema file, even when ten entries name one
path. The rationale and the condition for revisiting it are ADR 0013's;
the code carries no cache, no memo map, and no comment implying one is
missing.

## 7. CLI output protocol

`runGenerate`'s diagnostic branch is unchanged: print `Diagnostics` in
order, return `generate: <n> error|errors`. The write branch becomes
two phases over `Result.Targets`. Phase A performs **no filesystem
mutation**; every step of phase B is a mutation.

The two phases are two functions, and today's `writeOutput`
(generate.go:113–172, which interleaves stat, read, sweep, wipe and
write) becomes a call to each:

```go
// targetPlan is phase A's finding for one target. Producing it mutates
// nothing, and phase B may do nothing a plan does not authorise.
type targetPlan struct {
	dir    string   // the target's resolved OutDir
	create bool     // dir is absent; phase B must create it
	wipe   []string // basenames phase A proved marked — the only
	                // entries phase B is permitted to remove
}

// inspectOutputs runs §7.1 for every target and returns one plan per
// target, index-parallel to targets. It performs no filesystem
// mutation. A non-nil error means no plan is returned and nothing has
// been touched.
func inspectOutputs(targets []pipeline.TargetResult) ([]targetPlan, error)

// commitOutputs runs §7.2 against plans from inspectOutputs.
// len(plans) must equal len(targets).
func commitOutputs(targets []pipeline.TargetResult, plans []targetPlan) error
```

This is the seam `TestGenerateWipeListIsPhaseAs` (§10) drives: the test
lives in `internal/cli`, calls `inspectOutputs`, creates a file in an
output directory, then calls `commitOutputs` with the plan it already
holds, and asserts the new file survives. Both functions stay
unexported; the test is an in-package caller, not a client.

Rejected: a package-level `var betweenPhases = func() {}` for the test
to swap. It puts a hook in shipped code whose only caller is a test, and
it proves less — a plan value makes "phase B removes exactly phase A's
list" a property of the signature, which a hook between two halves of
one function does not.

### 7.1 Phase A — inspect

For each target `i` in order, with `dir` its `OutDir`:

1. `os.Stat(dir)`. Absent → record `create` for this target and
   continue with the next target. Present but not a directory → abort
   `graph[<i>]: output: <dir> is not a directory`. Any other stat
   failure → abort `graph[<i>]: output: <os error>`.
2. `os.ReadDir(dir)`; a failure aborts `graph[<i>]: output: <os
   error>`.
3. Sweep every entry in `ReadDir` order under the CLI-1 §5.1 step-3
   marker rule, unchanged and still version-agnostic: an entry is
   marked iff it is a regular file whose first line starts with
   `// Code generated by gqlc ` and ends with ` DO NOT EDIT.`. A read
   failure aborts `graph[<i>]: output: <os error>`. The sweep collects
   every offender, not the first.
4. Offenders present → abort
   `graph[<i>]: output directory <dir> contains entries not generated by gqlc: <e1>, <e2>; move or delete them and re-run`
   (CLI-1 §5.3's message, prefixed; basenames in `ReadDir` order,
   subdirectories suffixed `/`).
5. Record this target's swept entries as its wipe list.

Phase A aborts at the first target that fails. Within a target the
abort still names every offender — the two are different questions and
CLI-1 §5.3's answer to the second is unchanged.

### 7.2 Phase B — commit

Only after every target passed phase A, for each target `i` in order,
using the plan phase A recorded:

6. `create` → `os.MkdirAll(dir, 0o755)`; failure aborts
   `graph[<i>]: output: <os error>`.
7. `os.Remove` each entry on this target's wipe list — **exactly** the
   entries phase A proved marked, never a superset. A removal that
   returns `fs.ErrNotExist` is **success, not failure**: the wipe's
   purpose is that the entry is gone, and it is. Any other error aborts
   `graph[<i>]: output: <os error>`.
8. Write each `File` to `filepath.Join(dir, f.Path)`, mode `0o644`, in
   slice order.

The `fs.ErrNotExist` tolerance in step 7 is not defensive padding; the
§4.3 limit guarantees a path to it. Phase A computes every wipe list
before any target commits, so an aliased `out` pair the loader cannot
compare gives two targets overlapping wipe lists: target 0's commit
deletes files still listed for target 1, and an intolerant `os.Remove`
would then abort mid-commit with a bare errno and a tree already half
rewritten. The tolerance is also correct on its own terms — a
concurrently deleted file, or a wipe list stale for any other reason,
satisfies the same reasoning.

Wipe and write are interleaved per target rather than wiping every
target and then writing every target: each directory then spends the
shortest possible time empty.

### 7.3 What the split guarantees, and what it does not

- **An abort under the tripwire mutates nothing.** Every stat, read and
  sweep for every target precedes every mkdir, remove and write, so the
  half-generated project the interleaved algorithm allows cannot
  happen. With one target this is CLI-1 §5.2's guarantee restated; with
  N it is the only way to keep it, because `codegen.Generate`'s purity
  no longer orders target 2's checks before target 0's writes.
- **gqlc still deletes only what it proved it generated.** Step 7 uses
  phase A's list, so a file that appears in the gap between the phases
  is not deleted — it survives the run and the next run's sweep names
  it. That is the one-directional guarantee: nothing unproven is
  deleted. It is not a claim that the directories cannot change
  underneath a run. gqlc takes no locks, and two concurrent `generate`
  runs over one config are outside the contract.
- **A commit-phase failure leaves a partial write.** Disk full at
  target 2's third file leaves targets 0 and 1 rewritten and target 2
  partial; a truncated file may lack the marker, so the next run aborts
  naming it and the remedy is deleting it. This is CLI-1 §5.2's
  residual window, widened from one directory to a prefix of them, and
  accepted on the same terms — atomic write-and-rename stays a non-goal
  (§11).
- **An output directory that cannot be created is discovered in phase
  B**, after earlier targets were rewritten, because `MkdirAll` is a
  mutation and phase A performs none. Pre-creating directories in
  phase A would shrink that window at the cost of the property the
  split exists for: that a run which aborts before commit leaves every
  byte of the tree unchanged, which is what the tests assert.

## 8. `gqlc init`

The wizard, its two groups, its eight prompts, the validators, the
preview/confirm split, the TTY guard, the abort contract, and the
accessible-mode wiring are CLI-2's and are unchanged. What changes is
what the wizard is bound to and how many targets a run may touch.

### 8.1 One target per run

`initDefaults()` returns a `Config` with exactly one `Target`; the
per-field defaults of CLI-2 §3.2 are unchanged except that `output`
is now `gen.go.out`. The form binds that target's fields, so a FRESH
run writes a one-entry `graph` list.

An EDIT run classifies as today and then checks the count. With one
target it prefills from it, exactly as CLI-2 §3.3 describes. With more
than one it **refuses before any form renders** — exit 1, nothing
written:

```
<path> declares <n> generation targets; init edits only a single-target config (edit it by hand, or run gqlc init --add to append another)
```

The wizard can express one target; prefilling from the first and
writing the canonical form would silently delete the rest.

**Branch 2 ships a shorter parenthetical.** §9 puts the refusal on
branch 2 and `--add` on branch 4, so branch 2's binary has no `--add`
flag and must not name one: a hint whose answer is `unknown flag: --add`
is worse than no hint. Branch 2 ships

```
<path> declares <n> generation targets; init edits only a single-target config (edit it by hand)
```

and branch 4 extends the parenthetical to the form above, as part of
adding the flag it names. The cost is one string constant and one test
expectation written twice (§10) — the price of every branch in the stack
being shippable alone.

Moving `--add` onto branch 2 to avoid that cost was considered and
rejected. `--add` is wizard work with no dependency on the loader beyond
the type change, so hosting it on branch 2 would put the widest-blast-
radius change in the tree (the config rewrite) in the same PR as the
most user-visible one (a new flag) and leave branch 4 empty — collapsing
the stack rather than balancing it. The refusal itself cannot move the
other way: without it, branch 2's `init` silently drops targets from a
file the loader now accepts (§9.1).

### 8.2 `--add`

```go
cmd.Flags().BoolVar(&add, "add", false,
	"append a generation target to the existing config file")
```

`--add` prompts for one target and appends it to the file's existing
list. Flow selection is stricter than bare `init`'s, because appending
presupposes a file that loads:

| classification              | behaviour                                                                 |
|-----------------------------|---------------------------------------------------------------------------|
| loadable (any target count) | run the wizard, append, preview, confirm, write                           |
| `fs.ErrNotExist`            | `no config file at <path> (run gqlc init to create one)` — the message `generate` uses, from one shared unexported helper in `internal/cli` |
| any other load failure      | the `config.Load` error verbatim, exit 1                                  |

There is no broken-config dialogue under `--add`: "start fresh" would
replace the file the flag promises to append to.

**Prefill.** The appended target starts from the file's **last** entry
for the fields a second target usually shares — `schema`,
`schema_language`, `query_language`, `procsig`, `gen.go.driver` — and
**empty** for the three fields a second target normally gives its own:
`queries`, `gen.go.out`, `gen.go.package`. An empty prompt is the
honest default; only `gen.go.out` is enforced by the loader — shared
query directories and package names are legal (§11).

**Validation.** The `out` input's `Validate` hook calls
`cfg.CheckOutAgainst(out)` (§4.3) on the loaded config, so `--add`
cannot write a config the loader would then reject — the same reason
CLI-2 §4.3 made the wizard enforce codegen's package grammar rather than
warn. It calls the loader's rule rather than restating it: a second
implementation would drift, and the first thing it would drift on is the
`..foo` case. Message shape at the prompt:

```
out "internal/user/gen" is already graph[0]'s output directory
out "internal/user/gen/sub" is inside graph[0]'s output directory "internal/user/gen"
```

**Preview, warnings, epilogue.** The preview block shows the whole
resulting file — every entry, canonical — at the existing confirm gate,
so an append is reviewed in the context it lands in. The comment notice
(CLI-2 §5.3) applies as always. The soft warnings and the epilogue
name the target the run authored: under `--add` the appended one,
under bare `init` the only one.

## 9. Branches and the documents each one updates

`AGENTS.md` requires every branch to be independently mergeable and
green. `internal/config`'s type change breaks its two consumers at
compile time, so branch 2 necessarily carries a mechanical adaptation
of each (§9.1). Branch 3 replaces the `pipeline` half; the `init` half
is permanent, because refusing a multi-target edit is a ratified
behaviour (§8.1), not a stopgap.

| branch | bead | code | documents |
|--------|------|------|-----------|
| 1 | `gqlc-0gb.1` | none | ADR 0013, this spec |
| 2 | `gqlc-0gb.2` | `internal/config` (§2–§5), `CheckOutAgainst` (§4.3); consumers adapted mechanically (§9.1); `classifyTarget` → `classifyConfig` | `config-file-format.md`, `README.md`, `cli-stage-2.md`, CONTEXT.md |
| 3 | `gqlc-0gb.3` | `internal/cli/pipeline` (§6) **and** the two-phase write (§7); the single-target guard deleted | `cli-generate-pipeline.md`, `cli-stage-1.md` |
| 4 | `gqlc-0gb.4` | `init --add` (§8.2); the §8.1 refusal message extended | `cli-stage-2.md` |

The N-target pipeline and the two-phase write are one branch because
neither is shippable without the other. A `Result` carrying N targets
driven through the single-target write path writes and wipes target 0
before target 2 is inspected, which is exactly the half-generated abort
ADR 0013 forbids; and a two-phase write over a one-target `Result` is
a rename with no second target to protect. Splitting them would produce
two branches that only make sense merged — the shape `AGENTS.md`'s
independent-mergeability rule exists to prevent.

Branch 4 is `--add` alone. The one-target wizard binding and the
multi-target-edit refusal (§8.1) sit on branch 2, where they are
load-bearing rather than deferrable (§9.1).

Branch 2 carries more documents than its code footprint suggests,
because renaming a wire key invalidates every document that quotes one.
Specifically:

| document | what branch 2 must change |
|----------|---------------------------|
| `config-file-format.md` | the wire table, the canonical form (§7), the §6.3 catalogue, the §5 note on what version 1 means |
| `README.md` | the `gqlc.yaml` sample (README.md:21–30), which shows the flat `output: internal/db` |
| `cli-stage-2.md` | the `output` key in all three places it is named — the defaults table (:232), the group-1 field list (:319–320) and the prompt table (:337) — and the flow table (:203–208), which has no refusal path |
| CONTEXT.md | the "Config file" entry, the "Output directory" entry (:508–518, which names both the `output` and `package` keys), a new "Generation target" entry, and the "Flagged ambiguities" list |

`cli-stage-2.md` appears on branches 2 and 4 because both change it: 2
for the renamed keys and the refusal path, 4 for the flag. `README.md`
does not appear on branch 3 — its sample is a config file, not pipeline
behaviour, and it is already correct once branch 2 lands.

**Terminology.** "Generation target" collides with two existing uses
inside the code this change touches: `classifyTarget` (init.go:89), whose
"target" is the config file being classified, and config-file-format §1's
"the output target" for the `output` key. Branch 2 renames the function
to `classifyConfig` and drops the phrase from config-file-format, and
CONTEXT.md's new "Generation target" entry carries an `_Avoid_` clause in
the house style:

```
_Avoid_: target (unqualified — collides with the config file being
classified and with the output directory); output target (the output
directory's old name); typed repository (ADR 0010 — the generated
artifact one target produces, not the config entry).
```

Each behaviour document travels with the branch that makes it true.
This spec and ADR 0013 land first and describe the whole change ahead of
the code, which is what a spec is for; the rule is about the *behaviour*
documents. No merge may leave `README.md`, the CLI specs,
`config-file-format.md` or CONTEXT.md describing a wire key, a message
or a flow the merged binary does not have.

### 9.1 Branch 2's mechanical adaptation

Branch 2 rewrites the loader and leaves the rest of the tree
compiling, green, and honest about what it can do:

- `pipeline.Run` keeps its single-target `Result` and gains one guard
  after stage 1: `len(cfg.Targets) != 1` returns
  `config declares <n> generation targets; this gqlc runs one (multi-target generation lands in the next release)`.
  Its body reads `cfg.Targets[0]`. Branch 3 deletes the guard with the
  loop that replaces it.
- `init` binds the wizard to one target and refuses a multi-target
  edit (§8.1). The refusal is not deferrable: without it, branch 2's
  `init` would drop targets from a file the loader now accepts.
  `--add` stays on branch 4.
- The CLI, pipeline and config test fixtures are rewritten to the new
  wire shape.

At every point in the stack, `generate` either runs one target through
the existing write path (branches 1–2) or runs N through the two-phase
one (branch 3 onward). No merge leaves a version of `gqlc` that can
half-generate a project.

## 10. Test obligations

Per branch, the cases without which the change is not proven. Each
branch's PR carries the full suite in its package's existing style.

| branch | test | proves |
|--------|------|--------|
| 2 | `TestLoadCanonicalFixture`         | the §2.4 fixture loads into the expected two-`Target` `Config` |
| 2 | `TestSaveEmitsFixtureBytes`        | `Canonical` reproduces the fixture byte-for-byte (nesting, sequence indent, omitted `procsig`) |
| 2 | `TestLoadPreservesRawPaths`        | trailing slashes and `./` prefixes survive into `Config` unaltered |
| 2 | `TestRejectOldFlatShape`           | the previous format's canonical file produces the §4.2 message, not an unknown-key list |
| 2 | `TestRejectionTable`               | every §4.5 row a config file can reach, message-exact, including the `graph[i]:` prefix on entry 1 of a two-entry document. The internal count row is the one exception — no known input reaches it, which is the point; `TestEntryCountInvariant` pins its wording instead |
| 2 | `TestOutOverlap` (table, 23 rows)  | *Cleaning, all rejected as the same directory:* `internal/db` against itself, `internal/db/`, `./internal/db`, `internal//db`, `./a/../internal/db`. *Nesting, rejected naming both indices:* `internal/db` vs `internal/db/sub` and the reverse, `../a` vs `../a/b`, and the string-prefix trap **`internal/db` vs `internal/db/..foo`** with `internal/db/..foo/x` one level deeper. *Plainly disjoint, accepted:* `internal/db` vs `internal/user` and vs `internal/dbgen`. The remaining rows are §4.3's rebasing and its limits. *Rejected, each with the direction the anchored comparison found and each read as disjoint by an unanchored `Rel`:* `..` vs `a`, `../..` vs `../a`, `..` vs `.`, `a` vs `..`, `.` vs `..`. *Accepted, each rejected by a plausible weakening of the anchor:* `../db` vs `db` (a base fixed at the root reads it as one directory) and `gqlc-anchor-0` vs `../gqlc-anchor-0` (an anchor segment without its NUL reads it as containment). *Accepted as instances of the two limit classes:* `/tmp/gqlc/db` vs `internal/db` and `/internal/db` vs `internal/db` (absolute against relative); `../b/db` vs `db` and `b` vs `../a` (re-entry through a name the loader cannot see — the second overlaps whenever the working directory is itself named `a`) |
| 2 | `TestCheckOutAgainst`              | the exported §4.3 seam returns the catalogue message for the first overlapping index and nil for a disjoint `out`, and agrees with the loader's sweep on every `TestOutOverlap` row |
| 2 | `TestVersionProbeUnaffected`       | `version: 2` with `graph: nope` reports `declares version 2`, not a `graph` shape complaint — the failure the document scan exists to avoid (§4) |
| 2 | `TestGraphNotASequence`            | `graph: nope` under `version: 1` produces the loader's own §4.5 message naming a YAML kind, with no yaml.v3 or Go type name in it; `graph: *g` aliasing a scalar reports the **resolved** kind (`scalar`, never `alias`) at the **alias's** line, not the anchor's (§4) |
| 2 | `TestNullEntryRejected`            | `- ~` between two entries reports index 1, not a shifted index; `~`, `null`, `Null`, `NULL` and a bare `-` all report; `- ""` does not (a wrong-typed entry, not a null one — §4.4) |
| 2 | `TestAliasEntryToNullRejected`     | `- *none` aliasing a null anchor reports the same §4.4 message at the alias's line, not the anchor's. Fails against a `Tag == "!!null"` test written on the unresolved node, which sees `""` and lets the entry be dropped (§4) |
| 2 | `TestMergeKeyEntryLoads`           | an element built with `<<: *base`, its `out` distinct, loads into a full target — the resolution rule changes what the scan looks at, never what it rejects (§4) |
| 2 | `TestAliasEntryToMappingReachesOverlap` | `- *t` aliasing an earlier entry is **not** rejected as null and instead fails at §4.3 with the overlap message naming 0 and 1, because the alias copies `out` (§4). Asserting that it loads would be asserting what §4.3 forbids, and the tempting repair — exempting aliased entries from the overlap sweep — is the bug |
| 2 | `TestEntryCountInvariant`          | the §4 count check, driven through its unexported helper (the sequence node and the decoded length) rather than a document, since §4 establishes that no input reaches it; asserts it names both counts, and does not fire for any accepted fixture |
| 2 | `TestInitRefusesMultiTargetEdit`   | pinned §8.1 **branch-2** message (no `--add` hint), exit 1, file byte-untouched |
| 3 | `TestRunEveryTarget`               | a two-target config yields two `TargetResult`s in document order, each with the right package clause and driver import |
| 3 | `TestRunSetupFailureFailsFast`     | an unreadable schema at entry 1 → `graph[1]: schema: ...`, zero `Result` |
| 3 | `TestRunDiagnosticsSpanTargets`    | broken queries in entries 0 and 1 → every line present, in target-then-file-then-annotation order, each `graph[i]:`-prefixed; `Targets` nil |
| 3 | `TestRunAllOrNothing`              | one broken query in entry 1 → entry 0's files are not returned |
| 3 | `TestRunStateIsPerTarget`          | **two different schemas**: entry 1's query resolves against entry 1's schema and would fail against entry 0's, and entry 1 has no `procsig` while entry 0 does, so a `CALL` in entry 1 fails `ErrUnknownProcedure`. Fails if `sch`, `reg`, `queryParser` or `res` (pipeline.go:122–159) is hoisted out of the loop |
| 3 | `TestGenerateMultiTarget`          | both packages written; a second run byte-identical |
| 3 | `TestGenerateAbortsBeforeAnyWrite` | an unmarked file in entry 1's output directory → entry 0's directory byte-identical after the failed run, entry 1 untouched |
| 3 | `TestGenerateAbortLeavesAbsentDirAbsent` | same abort with entry 0's `out` **not existing** beforehand → it still does not exist afterwards (§12.5's other half; the case §7.3's `MkdirAll` bullet turns on) |
| 3 | `TestGenerateWipeListIsPhaseAs`    | `inspectOutputs`, then a file created in an output directory, then `commitOutputs` with the held plan → the new file survives (§7's seam, §7.3's guarantee) |
| 3 | `TestCommitToleratesVanishedEntry` | a wipe-list entry deleted between the two calls → `commitOutputs` succeeds (§7.2 step 7) |
| 4 | `TestInitRefusalNamesAddFlag`      | the §8.1 refusal message, now extended with the `--add` hint the branch has made real |
| 4 | `TestInitAdd`                      | append round-trip: prefilled fields carried from the last entry, blank distinguishing fields, preview shows every entry, written file loads |
| 4 | `TestInitAddOverlapRejectedAtPrompt`| an `out` equal to an existing target's re-prompts with the §8.2 message and never writes |
| 4 | `TestInitAddNoConfig`              | missing file → the `generate` hint, verbatim, from the shared helper |

## 11. Non-goals

- **A version 2 of the format.** Version 1 changes shape (ADR 0013);
  the probe-then-dispatch seam is untouched and unused by this change.
- **A hoisted document-root `schema` key**, or a document-root default
  for any other per-entry key. Every entry states its own inputs.
- **Per-target overrides of anything outside the entry.** There is
  nothing outside the entry but `version`.
- **A schema-parse cache** (§6.3), and any other cross-target sharing
  of parsed or generated state.
- **Cross-target codegen deduplication.** Two targets over one schema
  generate identical `models.go` bytes into two directories; that is
  the expected outcome of two independent packages, not duplication to
  factor out.
- **A multi-target `init` wizard loop.** One target per invocation:
  `init` or `init --add`.
- **Cross-entry checks beyond output overlap.** Shared query
  directories, repeated package names, and repeated schema paths are
  all legal.
- **Concurrent or parallel target execution.** Targets run in document
  order in one goroutine; the ordering contracts of CLI-1 §3.3 and §4
  survive unchanged because of it.
- **Atomic output writes** (§7.3), **file-absolute query positions**,
  **recursive query discovery**, and every other CLI-1 §8 non-goal.
- **A migration command** for the old flat shape. The §4.2 message
  states the new shape, and the old file is short enough to retype.

## 12. Acceptance criteria

1. The §2.4 example loads into a two-target `Config`, round-trips
   through `Save` byte-identically, and drives `gqlc generate` to two
   generated packages in two directories; a second run is
   byte-identical.
2. A config file in the previous format fails with the §4.2 message —
   naming the shape change, not the version, and not a list of unknown
   keys.
3. Two entries whose `out` values are equal or nested are rejected at
   load, naming both entries.
4. Every `graph[i]:` a message prints indexes the entry the file
   declares at that position. No accepted config decodes to fewer
   entries than it wrote, however the dropped one was spelled — written
   null or alias to a null — and a decode that ever loses one fails
   loudly instead of renumbering.
5. A run that aborts under the tripwire — for any target — leaves every
   output directory exactly as the run found it: byte-identical if it
   existed, still absent if it did not. The abort message names the
   entry and every offending file.
6. Broken queries in several targets report every diagnostic, each
   prefixed with its entry, and write nothing anywhere.
7. `gqlc init` writes a one-entry file, refuses to edit a multi-target
   one, and `gqlc init --add` appends a second target whose output
   directory cannot overlap an existing one.
8. `just test`, `just fmt-check`, `just lint` and `just tidy-check`
   green on every branch of the stack; `test/data/codegen/`
   byte-identical to master throughout.
