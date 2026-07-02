# AGENTS.md

Conventions for working in this repo. gqlc is an sqlc analogue for graph query
languages: it parses GQL/Cypher schemas and queries and generates type-safe Go.

See [CONTEXT.md](./CONTEXT.md) for the domain glossary and `docs/adr/` for
recorded decisions.

## Comments

Before writing a comment, ask: **is this comment useful?** A comment earns its
place by carrying context that is hard to recover from the code itself — a
non-obvious *why*, an invariant, a reference to the spec or grammar, a warning
about a sharp edge. A comment that restates what a name or the code already says
is noise; delete it.

- Explain *why*, not *what*. The code already says what.
- No comments that paraphrase the function name or the next line.
- No narration of changes, TODO dumps, or LLM scaffolding left in place.
- No abstraction leaks. Describe what a thing *is* and its contract — never
  justify it by contrasting rejected designs ("deliberately not an AST",
  "rather than collapsing to X") or by appealing to other layers ("so codegen
  can…", "which is why the resolver…"). That rationale lives in an ADR; a comment
  may *cite* one ("(ADR 0003)") but must not restate it. The code should read the
  same whether or not the reader knows which alternatives were rejected.
- Prefer a clearer name over a comment that explains a bad one.

## Tests

- Use **testify** (`suite.Suite` for setup/grouping, `require` for fail-fast
  assertions). Not bare `testing.T`.
- **Table-driven.** Each case is a named row; the loop is the test body.
- High signal, no fluff: no asserting the obvious, no dead helpers, no cases
  that duplicate each other's coverage. A reader should see what each case
  proves at a glance.
- **Golden files** for parsed results: a `.golden.json` beside each `.gql`
  fixture, regenerated with `go test -update`. The serialisation is the domain
  model's own deterministic `schema.Schema.MarshalJSON` (maps sorted into stable
  slices), so one canonical form serves golden tests today and generated output
  later — calls to the tool stay idempotent.
- Schema fixtures live in `test/data/schema/gql/`. Prefer real GQL schemas
  validated against the grammar; supplement with hand-authored files. Negative
  fixtures pair a `.gql` with the sentinel error it must produce.

## Workflow: stacked branches

- **Never commit to `master` directly.** Every change goes through a feature
  branch. Use `gt` (graphite) to create and manage stacks **locally**:
  `gt create <name>` cuts a new branch stacked on the current tip; `gt log`
  shows the stack; `gt modify` amends the current branch in-place. **Do not
  use `gt submit`** — this repo is on the unauthorised tier of graphite, so
  the stack is a local review aid only.

- **One PR per branch in the stack, pointed at its parent — not master.**
  Each branch becomes its own GitHub PR, opened manually with
  `gh pr create --base <parent-branch>`. The `--base` argument is the
  branch directly below this one in the stack. **Only the bottom-of-stack
  PR has `--base master`.** This keeps each PR's diff scoped to that
  cycle's changes alone; without it, every PR re-shows the work from the
  branches below and review becomes impossible. As parent PRs merge to
  master, GitHub auto-retargets the children's bases.

  Example for a three-branch stack `A → B → C` cut off master:

  ```
  gh pr create --base master --head A --title "..." --body "..."
  gh pr create --base A      --head B --title "..." --body "..."
  gh pr create --base B      --head C --title "..." --body "..."
  ```

- **Each branch in a stack must be independently mergeable.** Apply this
  test to every proposed cycle/branch: *"if only this PR landed on `master` —
  with the branches above and below it deferred — would `just test` still
  pass?"* If the answer is no, the split is wrong: the change is incohesive
  with its tests, its skiplist, its pin updates, or its corpus integration,
  and it must be combined with the dependent piece into one branch. Run this
  check **before** proposing the cycle breakdown, not after.

- **Common failure mode.** Separating a behavior change from the test-suite
  updates that follow from it. Examples: dropping a parser rejection without
  simultaneously updating the godog skiplist + deleting the corresponding
  `mustReject` pin; adding a new sentinel without adding its `mustReject`
  coverage; changing the JSON wire format without regenerating goldens.
  These changes must travel together in one branch.

- **A cohesive cycle includes:** the code change + unit tests + any skiplist
  / golden / Layer-2 pin updates the change implies. If you find yourself
  thinking "the next PR will fix the tests" — combine them.

## Conventions

- **Parse-tree walking:** use the ANTLR **listener** (`ParseTreeWalker` +
  `BaseGQLListener`), overriding only the `Enter*` rules you need. Do not
  hand-roll tree traversal or use the visitor (see ADR 0001).
- **Errors:** unsupported/invalid constructs are package-level **sentinels**
  matched with `errors.Is`. Syntax errors come from a custom
  `antlr.ErrorListener`, not `panic`/`recover`. Parsing fails fast on the first
  error.
- **Generated code:** never edit `internal/grammar/**/gen`. Regenerate it with
  `just build-grammar` after changing a `.g4` grammar.
- **Make illegal states unrepresentable.** Encode invariants in types so
  invalid combinations cannot be constructed at all; prefer a shape the
  compiler enforces over a runtime check or a comment warning the next reader.
  Go techniques:
  - **Closed sum types** — an interface with an unexported marker method
    makes the sum exhaustive: only types in the same package can implement it,
    so callers cannot smuggle in a third variant and the compiler sees every
    case.
  - **Unexported fields + a `New*` constructor** — when an invariant cannot
    live in the types alone (a string that must be non-empty, fields that
    must both be set), hide the fields so the constructor is the only way to
    obtain a non-zero value, and validate the remaining invariants once
    there.
  - **A type per variant, not flags on one struct** — split shapes that
    share fields rather than carrying optional fields and a runtime
    discriminator the compiler cannot see.
  - **A shape that excludes the bad case** — pick the representation that
    makes the invalid value unwriteable (a scalar instead of a slice when
    only one is meaningful; canonicalising on construction so a
    non-canonical form cannot exist; enums over free strings).

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:7510c1e2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
