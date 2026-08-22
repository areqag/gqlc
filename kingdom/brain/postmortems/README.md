# Postmortems — how the Թագաւորութիւն learns

When something goes wrong — a red landing, a bad merge, a falsified gate, a
lost workday — there is always a postmortem. Not sometimes: always. A
mistake we didn't learn from is the only kind of mistake this town has.

## Every mistake is blame-free

Read this part twice. **Nobody should feel bad for making a mistake.** Every
mistake is a failure of process and guardrails, not a failure of an
individual. If a citizen could make the error, the system let them — so the
postmortem's question is never "who did this?" but "what allowed this, and
what do we change so the next citizen cannot fall the same way?" We learn
from our mistakes as a Թագաւորութիւն, and we all grow from them together.

This is constitutional (Article III.4): the postmortem names causes, not
culprits. A citizen who writes an honest postmortem about their own workday
has done the town a service, not penance.

## Writing one

One file per incident: `YYYY-MM-DD-<slug>.md`, PR'd like any other change.

```markdown
# <what happened, in one plain sentence>
Date: <when>   Written by: <seat>   Beads: <ids>

## What happened
The observable facts, in order. No adjectives.

## What allowed it
The process gap or missing guardrail. If the honest answer names a person's
choice, keep asking "what made that choice reasonable at the time?" until
the answer names a process again.

## What we change
Concrete: a new gate, an amended playbook, a constitution PR, a bead per
change — filed, not promised.

## What we learned
The part worth re-reading a year from now.
```

Follow-ups are beads, filed before the postmortem merges. A lesson without a
bead is a lesson the town will re-learn the hard way.
