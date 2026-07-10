# Contributing

## Setup after cloning

Run `just init` once after cloning. This configures git to use the project's
`.githooks/` directory (`git config core.hooksPath .githooks`), which activates
a pre-commit hook that blocks accidental direct commits to `master` or `main`.
The same guard is wired into Claude Code as a `PreToolUse` hook so AI agents are
blocked at the conversation level too. The recipe is idempotent — running it
multiple times is safe.

`just test` and `just doctor` warn on stderr (non-fatal) when `core.hooksPath`
has drifted away from `.githooks` — the only failure mode by which local hooks
silently die. CI cannot see local git config, so this is where drift surfaces.

## Development

Everything runs through `just`, locally and in CI — same recipes, same pinned
tool versions (see the `justfile`). There is nothing else to install or keep
up to date: every lint/fmt recipe first verifies the pinned golangci-lint in
the gitignored `.bin/` and re-provisions it on any mismatch (~3s), so a
version bump in the justfile propagates to every machine automatically.

- `just test` — build + full suite (unit, golden snapshots, godog TCK)
- `just lint` / `just lint-new` — full static analysis / only the diff vs master
- `just fmt` / `just fmt-check` — gofumpt + gci, fix vs check
- `just tidy-check` — go.mod/go.sum drift
- `just vuln` — govulncheck

The hooks split the same checks by budget: pre-commit blocks master commits and
gates formatting (sub-second); pre-push runs the suite and diff-scoped lint
(seconds); CI is the authoritative gate (`lint`, `test`, `tidy`, `actionlint`
and `govulncheck` are required to merge — the vulnerability job reports on
every PR but only scans when go.mod/go.sum changed).
