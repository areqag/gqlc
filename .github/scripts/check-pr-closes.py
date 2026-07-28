#!/usr/bin/env python3
"""
Check that a PR body carries 'Closes #N' when its bead has a GH mirror.
Exits 0 (pass) or 1 (fail with diagnostic).
Usage: check-pr-closes.py <jsonl_path> <bead_id_or_empty>
PR body is read from the PR_BODY environment variable.
"""
import sys, re, json, os


def main():
    if len(sys.argv) < 3:
        print("Usage: check-pr-closes.py <jsonl_path> <bead_id_or_empty>")
        sys.exit(1)

    jsonl_path = sys.argv[1]
    bead_id = sys.argv[2]
    pr_body = os.environ.get('PR_BODY', '')

    if not bead_id:
        sys.exit(0)  # No bead on this PR → pass

    # Load bead from JSONL
    bead = None
    try:
        with open(jsonl_path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                d = json.loads(line)
                if d.get('id') == bead_id:
                    bead = d
                    break
    except Exception:
        pass

    if bead is None:
        # Unknown bead — PASS (don't block on stale export)
        print(f"[check-pr-closes] bead {bead_id!r} not in export — skipping")
        sys.exit(0)

    ext = bead.get('external_ref') or ''
    if not ext:
        sys.exit(0)  # No GH mirror → pass

    if bead.get('issue_type') == 'epic':
        print(f"[check-pr-closes] {bead_id} is an epic — skipping (umbrella must not be closed)")
        sys.exit(0)

    # Extract N from URL
    m = re.search(r'/issues/(\d+)$', ext)
    if not m:
        sys.exit(0)  # Can't parse → pass
    expected_n = m.group(1)

    # Scan PR body for Closes/Fixes/Resolves #M
    # Case-insensitive; match the keyword, optional whitespace, #number
    found = re.findall(r'(?i)(?:closes|fixes|resolves)\s+#(\d+)', pr_body)

    if not found:
        print(f"ERROR: PR body is missing 'Closes #{expected_n}'")
        print(f"       Bead {bead_id} maps to GitHub issue #{expected_n}.")
        print(f"       Add 'Closes #{expected_n}' to the PR body so the issue closes on merge.")
        sys.exit(1)

    if expected_n not in found:
        wrong = [n for n in found if n != expected_n]
        print(f"ERROR: PR body closes #{', #'.join(wrong)} but bead {bead_id} maps to #{expected_n}.")
        print(f"       Replace 'Closes #{wrong[0]}' with 'Closes #{expected_n}'.")
        print(f"       Closing the wrong issue is worse than closing none.")
        sys.exit(1)

    # Correct number present
    print(f"[check-pr-closes] {bead_id} → Closes #{expected_n} ✓")
    sys.exit(0)


main()
