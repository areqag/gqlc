# Mail

File-based maildirs under `$(km state-dir)/mail/<seat>/{inbox,read,sent}`,
shared by all seats, readable by the king. `bd mail` delegates here
(`mail.delegate` → `km mail`), so the tool you already use for beads is the
post office too.

## Commands

```
bd mail send <seat> -s "subject"     # body on stdin (heredoc or pipe)
bd mail send <seat> -s "subject" -m "one-line body"
bd mail inbox                        # your unread
bd mail read <msg-id>                # print + move inbox → read
bd mail sent                         # what you've sent
```

Recipients: any seat name, or `andranik` (the king), or `town` (broadcast:
delivered to every seat's inbox).

## Etiquette

- Subjects carry the point: "review: PR #1088 (gqlc-abcd)" beats "hi".
- Prefix `URGENT:` only for things that should wake someone. The dispatcher
  wakes Սեդրակ for any unread mail; other seats read mail when they wake.
- One topic per message; beads ids and PR numbers in the body so the
  recipient can act without asking.
- Mail is the conversation; beads notes are the record. A decision reached
  by mail gets `--append-notes`'d onto the bead it affects.
- All mail is open record (the king and the guard can read every box).
  Write accordingly.

## When a letter goes unanswered

A letter still in `inbox/` was delivered and not read. For a SEAT that is
evidence of **unseen**, not of declined: a seat drains its box when it wakes, so
a letter that outlived a wake was never in front of anyone. (For the king it is
not even that — his box is never drained, so a count there measures delivery.
Do not withhold a letter on it, gqlc-2abx.)

So repetition escalates. It does not soften.

- **Never write a gentler version of a letter nobody has read.** The last letter
  in a box is the one a reader trusts, so a softened repeat does not merely fail
  to help — it withdraws the ones underneath it.
- **A reading is not revised downward without NEW evidence.** Name the evidence
  and its timestamp in every check-in (`heartbeat.json`'s `updated`, the pane,
  the board's own verdict), so the next letter can be compared against it rather
  than against a mood. Unchanged evidence after more time is *stronger*, not
  weaker.
- **After the second unanswered letter on unchanged evidence, stop writing to
  that seat** and escalate. A third letter costs the recipient nothing and you a
  reader; the metronome shape — 48 unread check-ins — is already town law.
- **Escalate to Սեդրակ — unless Սեդրակ is who the letter is ABOUT.** Then mail
  reaches nobody, because the dispatcher wakes exactly one seat for unread mail
  and it is his. **File a bead instead.** A bead is the only artifact in this
  town that wakes a seat who is not Սեդրակ: unassigned and P1, class inferred
  `warrior`, the evidence in the description. It is a real remedy and not a
  filing gesture — `km wake <seat>` nudges an awake seat's pane, which is
  exactly the recovery a hand performed on 2026-08-24. Mail Անդրանիկ too, as
  the record, knowing that is a delivery and not a wake.

This is written from one incident (`kingdom/brain/postmortems/2026-08-24-every-mechanism-worked-and-nothing-recovered.md`).
Րաֆֆի wrote five times between 04:05Z and 05:05Z about a mayor whose heartbeat
had been frozen since 03:44:54Z. The evidence never changed; the reading got
softer, ending in praise. He was not overruling his own record — each round
wakes a fresh session, so he could not see it. `km guard-sweep` now carries the
count into his wake for that reason, and the rule above is what it is carried
for.

## Message format

One file per message, front-matter + markdown body:

```
20260821T091400Z--sedrak--review-pr-1088.md
---
from: sedrak
to: artur
date: 2026-08-21T09:14:00Z
subject: review: PR #1088 (gqlc-abcd)
---
body…
```

Unread = still in `inbox/`. `read` moves it to `read/`. Nothing is deleted;
the archive is the record.
