# Generated code executes the original query text with bound parameters

The curated query model (ADR 0003) is lossy by design and is **not** sufficient
to reconstruct an executable query. Therefore generated driver code executes the
**original query source verbatim** — with its `$param` placeholders intact — and
supplies parameter values as a binding map to the database driver, which performs
the substitution. The model is used only to derive the typed parameter arguments
and result columns; it never rebuilds the query.

## Considered options

**Reconstruct the executable query from the model.** Rejected: it contradicts the
lossy curation of ADR 0003, forcing the model to become lossless and coupling it
to every Cypher feature a query might use (the exact coupling ADR 0003 avoids).

**Execute the original text, model is type-interface only.** Chosen. The parser
is free to drop everything that doesn't affect the type interface (predicate
structure, `ORDER BY`, `DISTINCT`, …) because execution never depends on the
model — only on the original text.

## Consequences

- The original query text must be carried to codegen alongside the parsed (and
  later validated) query. This is an orchestration concern: `query.Query` itself
  stays a pure type-interface model and does **not** store the source text.
- Parameters are dual-purpose: the `$name` placeholders stay verbatim in the
  executed text, while the `Parameter` model drives the generated argument types
  and the keys of the runtime binding map handed to the driver.
- This is what makes the parser's "accept-and-ignore" treatment of
  interface-neutral constructs safe (see the Stage-0 spec, principle B1).
