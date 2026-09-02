#!/usr/bin/env python3
"""
Refuse a bare "ADR NNNN" citation in kingdom prose (bd gqlc-ktc8e, GH #2279).

Usage: check-adr-citations.py DIR [DIR ...]

TWO SERIES SHARE EVERY NUMBER FROM 0001 TO 0014. docs/adr/ (product
architecture, 39 documents) and kingdom/brain/decisions/ (kingdom law, 14
documents) are both hand-numbered from 0001, so a bare "ADR 0010" resolves to
the codegen-repositories ADR or to the merge-queue decision depending on which
directory the reader happens to open. Measured 2026-09-02: 82 citations under
kingdom/ used the bare form to mean kingdom law, while ci.yml cited kingdom
decision 0010 and product ADR 0026 in one file under one notation. The
ambiguity is silent at the point of reading — both targets exist, so no link
breaks and no gate reddens; the reader just gets the wrong document.

THE CONVENTION THIS ENFORCES, written down in kingdom/README.md: kingdom law
is cited as "decision NNNN"; the bare "ADR NNNN" form belongs to docs/adr/
exclusively; kingdom prose citing a product ADR spells the path
(docs/adr/NNNN-slug.md), which this check deliberately does not match. So
under an enrolled directory any bare "ADR NNNN" is wrong in one of two ways —
a kingdom citation in the reserved form, or a product citation that will
dangle when either series moves — and both take the same remedy.

WHAT THIS DOES NOT CATCH. Files OUTSIDE kingdom/ that cite kingdom law: the
product tree cites docs/adr/ bare in hundreds of places legitimately, so
enrolling it would refuse a tree that is right, and no stateless read can
tell which namespace a bare citation outside kingdom/ means — that is the
ambiguity itself. The instances that existed (.githooks ×10, workflows ×5)
were rewritten in the change that added this file; new ones are governed by
the written convention alone. Nor does this check that a cited "decision
NNNN" resolves to a file — a dangling number is a different defect with a
louder failure (the reader finds nothing, not the wrong thing).

The citation is matched across a line wrap ("under ADR\\n0003" was live in
decision 0004 when this was written), so the pattern reads whole files, not
lines.
"""

import re
import sys
from pathlib import Path

# \s+ rather than a space: two of the 82 live citations wrapped between the
# word and the number. The word boundary keeps "RADR 0001" and slugs like
# "adr-next" out; case-sensitive because the reserved form is the uppercase one.
CITATION = re.compile(r"\bADR\s+\d{4}")

REMEDY = (
    "      Kingdom law is cited as 'decision NNNN'. A product ADR cited from "
    "kingdom prose spells the path, docs/adr/NNNN-slug.md, which this check "
    "does not match. The convention is written down in kingdom/README.md."
)


def offenders(path):
    """Return one 'file:line: excerpt' string per bare citation in path."""
    text = path.read_text(encoding="utf-8", errors="replace")
    found = []
    for match in CITATION.finditer(text):
        line = text.count("\n", 0, match.start()) + 1
        excerpt = " ".join(match.group(0).split())
        found.append(f"  {path}:{line}: {excerpt}")
    return found


def check(directory):
    """Report on one directory, as (error lines, files read).

    No error lines means clean. The count rides back with them so a clean run
    can say what it passed over without walking the directory a second time.
    """
    if not directory.is_dir():
        # Not a skip: a gate that cannot read its input has cleared nothing,
        # and a directory renamed out from under this call site would otherwise
        # make the gate silently stop checking the tree it exists for.
        return [f"error: {directory} is not a directory, so nothing was checked."], 0

    errors = []
    total = 0
    for path in sorted(directory.rglob("*")):
        if not path.is_file() or path.is_symlink():
            continue
        total += 1
        errors.extend(offenders(path))

    if total == 0:
        return [
            f"error: {directory} contains no files, so this gate checked nothing."
        ], 0

    if errors:
        errors.insert(
            0,
            f"error: {len(errors)} bare 'ADR NNNN' citation(s) under {directory}, "
            "where the form is ambiguous between docs/adr/ and "
            "kingdom/brain/decisions/:",
        )
        errors.append(REMEDY)

    return errors, total


def main(argv):
    directories = argv[1:]
    if not directories:
        print(f"usage: {argv[0]} DIR [DIR ...]", file=sys.stderr)
        return 2

    failed = False
    checked = []
    for name in directories:
        errors, total = check(Path(name))
        if errors:
            failed = True
            for line in errors:
                print(line, file=sys.stderr)
            continue
        checked.append((name, total))

    if failed:
        return 1

    # The counts are printed rather than a bare "ok" for the reason the empty
    # directory is refused above: a pass has to say what it passed over.
    for name, total in checked:
        print(f"checked {total} file(s) under {name}: no bare ADR citations")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
