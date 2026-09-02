#!/usr/bin/env python3
"""Mutation battery for gqlc-eunj4: is there an embed mutation on Tx that
COMPILES and that nothing catches?

    python3 docs/mutation/tx-core-surface-battery.py               # first pass
    python3 docs/mutation/tx-core-surface-battery.py --post-guard  # second

Answer: yes -- row M7.  The first pass is the tree with the guard this
battery motivated REVERTED (three rows KILLED, three WRONG, one SURVIVED);
the second is the tree with it in place (seven KILLED, each predicted
before the run).  Because the guard under examination is the thing being
added, the first pass IS the blinding pass -- the row that sweeps
everything on the second pass is absent from the first by construction.

Run from the repository root.  Every row mutates the RENDERERS (never the
goldens), regenerates the golden corpus the way an author would
(`go test ./internal/codegen/conformance/ -update`), and only then asks
whether anything is red.

README.md beside this file names the reason that regeneration is not
optional: `go test -c` compiles the GENERATOR and never the EMISSION, so
the standard screen returns rc=0 whatever a renderer mutation did to the
emitted package.  Regenerating the corpus and then running the emitted
packages' own module (SCREEN 3 below) is what closes that gap here.

The golden regeneration is the point.  Without it every renderer mutation is
killed by the byte-comparison in TestValid, for a reason that has nothing to
do with the Tx guard -- the same trap as a compiler kill, one layer out.  A
row that is red only before the regen is asserted by the corpus, not by
txsurface_test.go.

THREE SCREENS, reported separately, because "nothing catches it" is a claim
about all of them:

  guard   internal/codegen txsurface_test.go -- the AST structure guard that
          is NAMED for this surface.  This is the guard under examination.
  root    the whole root module.
  nested  test/data/codegen, a SEPARATE Go module holding
          methodset_test.go's TestTxMethodSet, the reflection witness over
          the compiled generated packages.  `go test ./...` from the root
          CANNOT reach it -- it reports
          "directory prefix test/data/codegen does not contain main module"
          and that is a BASELINE failure on a clean tree, not collateral.
          An earlier revision of this harness put it in the root run and so
          reported a false kill on every row.  CI reaches it only through
          `just test-codegen-live-neo4j`, in the Docker-gated codegen-live
          workflow; that job is PR-blocking and its -run list names
          TestTxMethodSet, so the catch does reach a pull request.
          `just gates` does NOT reach it.
"""

import argparse
import dataclasses
import json
import subprocess
import sys

AGE_DB = "internal/codegen/age/render_db.go"
NEO_DB = "internal/codegen/neo4j/render_db.go"
AGE_GRAPH = "internal/codegen/age/render_graph.go"
AGE_QUERIER = "internal/codegen/age/render_querier.go"
NEO_QUERIER = "internal/codegen/neo4j/render_querier.go"
PREPARE = "internal/codegen/prepare.go"
PREPARE_TEST = "internal/codegen/prepare_test.go"
SPEC = "docs/specs/codegen-sentinel-taxonomy.md"

RESTORE = [
    AGE_DB, NEO_DB, AGE_GRAPH, AGE_QUERIER, NEO_QUERIER, PREPARE, PREPARE_TEST,
    SPEC, "test/data/codegen/",
]

NESTED = "test/data/codegen"

# The guard under examination.  Anything else that reddens is collateral and
# is reported as such, by test name.
GUARD_RUN = "^TestTxSurfaceAgreesAcrossBackends$|^TestTxPromotionPreconditions$"

# --- the emitted fragments the mutations rewrite -----------------------------

AGE_TX_STRUCT = "type Tx struct {\n\tqueries\n\ttx   pgx.Tx\n\tdone bool\n}"
NEO_TX_STRUCT = (
    "type Tx struct {\n\tqueries\n\tsession ` + target.sessionIface + `"
    "\n\ttx      neo4j.ExplicitTransaction\n\tdone    bool\n}"
)
AGE_TX_NEW = "&Tx{queries: queries{db: tx, graph: q.graph}, tx: tx}"
NEO_TX_NEW = "&Tx{queries: queries{db: txDB{tx: tx}}, session: session, tx: tx}"

AGE_HANDLE = "type Queries struct {\n\tqueries\n}\n"
NEO_HANDLE = "type Queries struct {\n\tqueries\n}\n"

TX_PIN = '\tb.WriteString("var _ Querier = (*Tx)(nil)\\n")\n'


@dataclasses.dataclass
class Edit:
    path: str
    old: str
    new: str


@dataclasses.dataclass
class Mutant:
    id: str
    what: str
    victim: str   # the literal assertion expected to fire, declared BEFORE the run
    expect: str   # the predicted verdict, declared BEFORE the run
    edits: list


def dialect_edits():
    """A new EXPORTED non-lifecycle method on the CORE.  It promotes onto
    *Queries AND *Tx, widening Tx's exported surface, and is not one of
    txCoreLifecycleNames.

    The body is a constant on purpose.  An earlier spelling returned q.graph
    and was killed by an AGE test about the graph name reaching the server
    through one check -- a property of the BODY, not of the shape under test.
    """
    return [
        Edit(AGE_DB, AGE_HANDLE,
             AGE_HANDLE + '\nfunc (q *queries) Dialect() string {\n\treturn "age"\n}\n'),
        Edit(NEO_DB, NEO_HANDLE,
             NEO_HANDLE + '\nfunc (q *queries) Dialect() string {\n\treturn "neo4j"\n}\n'),
    ]


MUTANTS = [
    Mutant(
        id="M1",
        what="emit a new exported non-lifecycle method on the core, "
             "`func (q *queries) Dialect() string`, and change nothing else.",
        victim='require.Equal(t, txSurfaceNames, surfaces[target].names, '
               '"%s: emitted Tx surface is not the declared set")',
        expect="KILLED",
        edits=dialect_edits(),
    ),
    Mutant(
        id="M2",
        what="embed the EXPORTED handle on Tx (`Queries` in place of "
             "`queries`), which promotes Begin and WithTx onto *Tx. This is "
             "the row the guard is built to catch; it is here as a POSITIVE "
             "CONTROL, so that a battery of SURVIVEDs is distinguishable from "
             "a battery whose screens never ran.",
        victim='require.Equal(t, "queries", ident.Name, "%s: %s embeds %q; '
               'embedding the exported handle would promote Begin and WithTx")',
        expect="KILLED",
        edits=[
            Edit(AGE_DB, AGE_TX_STRUCT, AGE_TX_STRUCT.replace("\tqueries\n", "\tQueries\n")),
            Edit(AGE_DB, AGE_TX_NEW,
                 "&Tx{Queries: Queries{queries: queries{db: tx, graph: q.graph}}, tx: tx}"),
            Edit(NEO_DB, NEO_TX_STRUCT, NEO_TX_STRUCT.replace("\tqueries\n", "\tQueries\n")),
            Edit(NEO_DB, NEO_TX_NEW,
                 "&Tx{Queries: Queries{queries: queries{db: txDB{tx: tx}}}, "
                 "session: session, tx: tx}"),
        ],
    ),
    Mutant(
        id="M3",
        what="embed the core by POINTER on Tx (`*queries`). The exported "
             "method set is unchanged; the zero value stops being usable.",
        victim='require.True(t, ok, "%s: %s embeds a %T, not the plain ident '
               '`queries`")',
        expect="KILLED",
        edits=[
            Edit(AGE_DB, AGE_TX_STRUCT, AGE_TX_STRUCT.replace("\tqueries\n", "\t*queries\n")),
            Edit(AGE_DB, AGE_TX_NEW, AGE_TX_NEW.replace("queries: queries{", "queries: &queries{")),
            Edit(NEO_DB, NEO_TX_STRUCT, NEO_TX_STRUCT.replace("\tqueries\n", "\t*queries\n")),
            Edit(NEO_DB, NEO_TX_NEW, NEO_TX_NEW.replace("queries: queries{", "queries: &queries{")),
        ],
    ),
    Mutant(
        id="M4",
        what="drop the emitted `var _ Querier = (*Tx)(nil)` pin from both "
             "renderers, leaving the *Queries pin in place. Nothing emitted "
             "then claims the promoted surface satisfies the interface.",
        victim='require.Contains(t, pins, "Querier = (*Tx)(nil)", '
               '"%s: querier.go does not pin *Tx to Querier")',
        expect="KILLED",
        edits=[
            Edit(AGE_QUERIER, TX_PIN, ""),
            Edit(NEO_QUERIER, TX_PIN, ""),
        ],
    ),
    Mutant(
        id="M5",
        what="move AGE's DropGraph from the handle to the core "
             "(`*Queries` -> `*queries`) in render_graph.go. It promotes onto "
             "*Tx, so graph teardown becomes reachable from inside a "
             "transaction -- the exact hazard §6 of the Tx spec names. No NEW "
             "name is introduced, so nothing about the identifier sweep "
             "changes, and the declaration is in graph.go, which the Tx guard "
             "never opens.",
        victim='require.False(t, found, "%s is in *Tx\'s method set; it must '
               'not be reachable from a transaction handle")  [nested module]',
        expect="WRONG",   # predicted: guard silent, nested TestTxMethodSet catches it
        edits=[
            Edit(AGE_GRAPH,
                 "func (q *Queries) DropGraph(ctx context.Context) error {",
                 "func (q *queries) DropGraph(ctx context.Context) error {"),
        ],
    ),
    Mutant(
        id="M6",
        what="M1, plus every remedy the M1 red actually asked for: the new "
             "name added to reservedIdentifiers and to the §6 table of the "
             "sentinel-taxonomy spec. This is what an author following the "
             "failure messages would ship.",
        victim="(none predicted -- this row is the finding if it survives)",
        expect="SURVIVED",
        edits=dialect_edits() + [
            Edit(PREPARE,
                 '\t"Begin":              scopeMethod,\n',
                 '\t"Begin":              scopeMethod,\n\t"Dialect":            scopeMethod,\n'),
            Edit(SPEC,
                 "| `Begin` | `scopeMethod` | every target | no target |\n",
                 "| `Begin` | `scopeMethod` | every target | no target |\n"
                 "| `Dialect` | `scopeMethod` | every target | no target |\n"),
        ],
    ),
]

# M6 was red on TestReservedIdentifiersAreUniformAcrossBackends: the reserved
# set is held against a THIRD hand-maintained table, reservedIdentifierRows in
# prepare_test.go, which M6 did not update. M7 is M6 plus that row -- the full
# remedy chain an author gets by following each failure message in turn.
RESERVED_ROWS_ANCHOR = "\t{\"Begin\", codegen.ScopeMethod, nil},\n"

MUTANTS.append(Mutant(
    id="M7",
    what="M6, plus the row TestReservedIdentifiersAreUniformAcrossBackends "
         "asked for in prepare_test.go's reservedIdentifierRows. Four remedy "
         "sites in all: the two renderers, reservedIdentifiers, §6 of the "
         "sentinel-taxonomy spec, and reservedIdentifierRows. This is the "
         "state an author reaches by fixing each red in turn, and the question "
         "is whether *Tx's widened surface is red in any of them.",
    victim="(none predicted -- this row is the finding if it survives)",
    expect="SURVIVED",
    edits=[e for e in MUTANTS[-1].edits] + [
        Edit(PREPARE_TEST, RESERVED_ROWS_ANCHOR,
             RESERVED_ROWS_ANCHOR + '\t{"Dialect", codegen.ScopeMethod, nil},\n'),
    ],
))


def sh(cmd, timeout=1800):
    # check=False: a non-zero exit is the measurement, not an error.
    p = subprocess.run(cmd, shell=True, capture_output=True, text=True,
                       timeout=timeout, check=False)
    return p.returncode, p.stdout + p.stderr


def failing_tests(out):
    return sorted({
        ln.strip().split(":", 1)[1].strip().split(" ")[0]
        for ln in out.splitlines()
        if ln.strip().startswith("--- FAIL:")
    })


def restore():
    sh("git checkout -- " + " ".join(RESTORE))


def apply(m):
    for e in m.edits:
        with open(e.path) as f:
            src = f.read()
        if src.count(e.old) != 1:
            return f"anchor not unique in {e.path}: {src.count(e.old)} occurrence(s)"
        with open(e.path, "w") as f:
            f.write(src.replace(e.old, e.new, 1))
    return None


def run_row(m, regen=True):
    row = {"id": m.id, "what": m.what, "victim": m.victim, "expect": m.expect}

    err = apply(m)
    if err:
        row["verdict"] = "HARNESS-ERROR"
        row["detail"] = err
        return row

    # `go build` is blind to _test.go, so the screen is `go test -c`.
    rc, out = sh("go test -c -o /dev/null ./internal/codegen/...")
    row["compiles"] = rc == 0
    if rc != 0:
        row["verdict"] = "COMPILER-KILL"
        row["detail"] = out[-1500:]
        return row

    # Pre-regen: what the byte-comparison corpus alone says. The -run filter
    # is asserted to have actually selected TestValid -- go test -run is
    # unanchored-by-default and an unmatched filter exits 0, which would read
    # as "the corpus is fine with this mutation".
    rc_pre, out_pre = sh(
        "go test ./internal/codegen/conformance/ "
        "-run '^TestConformanceSuite$/^TestValid$' -v"
    )
    if "TestConformanceSuite/TestValid" not in out_pre:
        row["verdict"] = "HARNESS-ERROR"
        row["detail"] = "the pre-regen -run filter selected no TestValid"
        return row
    row["corpus_red_before_regen"] = rc_pre != 0

    if regen:
        rc_u, out_u = sh("go test ./internal/codegen/conformance/ -update")
        row["regen_ok"] = rc_u == 0
        if rc_u != 0:
            row["regen_tail"] = out_u[-800:]
        # A regen that wrote nothing means the mutation never reached the
        # emitted bytes, which makes every verdict below vacuous.
        _, diff = sh("git status --short test/data/codegen/")
        row["goldens_changed"] = len(diff.strip().splitlines())
        if row["goldens_changed"] == 0:
            row["verdict"] = "NO-OP"
            return row

    caught_by = []

    # SCREEN 1 -- the guard under examination.
    rc_g, out_g = sh(f"go test ./internal/codegen/ -count=1 -run '{GUARD_RUN}' -v")
    if "TestTxSurfaceAgreesAcrossBackends" not in out_g:
        row["verdict"] = "HARNESS-ERROR"
        row["detail"] = "the guard -run filter selected no Tx surface test"
        return row
    row["guard_red"] = rc_g != 0
    row["guard_failing"] = failing_tests(out_g)
    if rc_g != 0:
        caught_by.append("guard:txsurface_test.go")
        row["guard_tail"] = out_g[-2500:]

    # SCREEN 2 -- the whole ROOT module. test/data/codegen is deliberately
    # absent: it is a separate module and naming it here fails on a clean tree.
    rc_r, out_r = sh("go test ./...")
    row["root_red"] = rc_r != 0
    if rc_r != 0:
        row["root_failing"] = failing_tests(out_r)
        caught_by.append("root:" + (",".join(row["root_failing"]) or "?"))
        row["root_tail"] = out_r[-2500:]

    # SCREEN 3 -- the NESTED module, run as its own module.
    rc_n, out_n = sh(f"cd {NESTED} && go test . -count=1 -run '^TestTxMethodSet$' -v")
    if "=== RUN   TestTxMethodSet" not in out_n:
        row["verdict"] = "HARNESS-ERROR"
        row["detail"] = "TestTxMethodSet did not run in the nested module"
        row["detail_tail"] = out_n[-1500:]
        return row
    row["methodset_red"] = rc_n != 0
    if rc_n != 0:
        caught_by.append("nested:TestTxMethodSet")
        row["methodset_tail"] = out_n[-1800:]

    rc_na, out_na = sh(f"cd {NESTED} && go test ./...")
    row["nested_red"] = rc_na != 0
    if rc_na != 0:
        row["nested_failing"] = failing_tests(out_na)
        caught_by.append("nested-all:" + ",".join(row["nested_failing"]))
        row["nested_tail"] = out_na[-1800:]

    row["caught_by"] = caught_by

    if row["guard_red"]:
        row["verdict"] = "KILLED"
    elif caught_by:
        row["verdict"] = "WRONG"   # something died, not the declared victim
    else:
        row["verdict"] = "SURVIVED"
    return row


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--only", default="", help="comma-separated mutant ids")
    ap.add_argument("--no-regen", action="store_true")
    ap.add_argument("--out", default="")
    ap.add_argument("--post-guard", action="store_true",
                    help="second pass, with the gqlc-eunj4 guard committed: "
                         "EVERY row is then predicted KILLED by the guard "
                         "under examination, including the four that escaped "
                         "it on the first pass.")
    args = ap.parse_args()

    if args.post_guard:
        for m in MUTANTS:
            m.expect = "KILLED"

    _, out = sh("git status --short " + " ".join(RESTORE))
    if out.strip():
        print("REFUSING: working tree is dirty in the paths this harness "
              "restores by checkout. Commit or clean first:\n" + out)
        return 2

    wanted = [m for m in MUTANTS if not args.only or m.id in args.only.split(",")]
    rows = []
    try:
        for m in wanted:
            print(f"--- {m.id} (predicted {m.expect}) ---", flush=True)
            row = run_row(m, regen=not args.no_regen)
            print(json.dumps({k: v for k, v in row.items()
                              if not k.endswith("_tail")}, indent=2)[:1600], flush=True)
            rows.append(row)
            restore()
    finally:
        restore()

    if args.out:
        with open(args.out, "w") as f:
            json.dump(rows, f, indent=2)

    print("\n=== SUMMARY ===")
    for r in rows:
        agree = "ok " if r.get("verdict") == r.get("expect") else "!! "
        print(f"{agree}{r['id']:4} predicted={r.get('expect','?'):10} "
              f"got={r.get('verdict','?'):14} "
              f"compiles={r.get('compiles','-')} "
              f"goldens={r.get('goldens_changed','-')} "
              f"caught_by={r.get('caught_by', r.get('detail','-'))}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
