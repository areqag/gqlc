# GQLC

gqlc is an analogue of the well-known sqlc library but for
graph query languages such as Cypher and GQL. It intends to
support gql schema files and queries in both gql and cypher.

## Setup after cloning

Run `just init` once after cloning. This configures git to use the project's
`.githooks/` directory (`git config core.hooksPath .githooks`), which activates
a pre-commit hook that blocks accidental direct commits to `master` or `main`.
The same guard is wired into Claude Code as a `PreToolUse` hook so AI agents are
blocked at the conversation level too. The recipe is idempotent — running it
multiple times is safe.
