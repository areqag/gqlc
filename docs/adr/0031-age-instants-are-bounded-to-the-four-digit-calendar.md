# AGE instants are bounded to the four-digit calendar

The AGE backend's emitted `agtypeInstant` decodes a stored count of microseconds
since the Unix epoch. It now refuses a count outside
`[-62135596800000000, 253402300799999999]` — year 1 midnight UTC through the
last microsecond of year 9999, inclusive — instead of handing the caller
whatever `time.UnixMicro` makes of it.

Before this, `math.MaxInt64` decoded to an instant in the year 294247 and was
returned with no error (bead `gqlc-grbh`).

## Why a bound at all, when gqlc wrote the value

The counter-argument, recorded when this was deferred off PR #848, is that
gqlc's own encode (`agtypeMicros`) writes the micros, so the value read back is
one this encoding produced — not the untrusted external input the offset
sidecar is.

That argument does not survive contact with where the value lives. agtype has
no temporal value, so a gqlc instant is stored as an ordinary agtype **integer
property on a vertex**. Any writer that can touch the graph can write any int64
there: another application, a `psql` session, an AGE query nobody generated.
The provenance claim is about the writer gqlc knows about, and the property is
not private to that writer. This is the same argument that already bounds
`agtypeZone`'s sidecar, one property over on the same vertex — the two are
neighbours, and treating one as trusted and the other as hostile was an
asymmetry with nothing behind it.

## Why this range, and not a narrower or wider one

The bead's objection was that any bound is a **calendar-range policy decision**
and there is no equivalent of "a zone no clock keeps" to key it on. There is
one, and it is the wire:

- **RFC 3339 has four year digits.** So does every timestamp text a SQL client
  prints, and so does `time.Time.Format` under any of the standard layouts. An
  instant in the year 294247 formats to a string no other system reads back.
  Year 10000 and beyond is therefore not a date this system can hand to
  another; it is a number that happens to fit in an int64.
- **The lower edge is the same fact mirrored.** Year 0 and negative years are
  spellings ISO 8601 admits only by prior agreement between the parties.

So the admitted set is the range every consumer of a gqlc instant can already
spell, which is a property of the wire rather than a taste.

**Nothing that exists is refused.** The bead's second objection was that the
live corpus deliberately carries year 9999 and pre-epoch instants, so a
candidate bound has to be argued against rows that already exist. It is: the
corpus's far-end row, `253402300799999999`, **is** the upper bound exactly, and
its pre-epoch row (1969) sits far inside the lower one. The bound was chosen so
that the widest instant the encoding was designed to carry is the last one it
admits, and the fixtures under `test/data/codegen/valid/` regenerate byte-for-
byte apart from the helper's own text.

## What was rejected

- **A narrower range** — e.g. PostgreSQL's `timestamp` bounds, or the range
  Neo4j's `datetime` admits. Both would refuse rows the corpus already carries
  or would key the bound on one particular consumer rather than on the text
  format they all share.
- **No bound, with a documented caveat.** Silence is the failure mode ADR 0030
  was written against: a value that is wrong and is returned anyway costs the
  caller a debugging session downstream, where the count is no longer in hand.
- **Bounding the encode side too.** `agtypeMicros` takes a `time.Time` a Go
  caller already holds, and refusing there would change a total function into a
  fallible one for a value the caller can inspect. The read is where an
  unknowable value arrives.

## Consequence

The refusal is a decode error on that row, naming the count it read. A graph
holding an out-of-calendar instant fails on the rows that carry one and serves
the rest, which is how every other malformed-value refusal in this backend
behaves.

The neo4j targets are unaffected: they carry the driver's own temporal types
and never see a raw micros count.

Pinned by `TestAgtypeInstantRefusesACountOutsideTheCalendar` in
`internal/codegen/age/testdata/corpus_test.go.txt`, which reads both admitted
edges, both one-microsecond-outside rows, and both int64 extremes.
