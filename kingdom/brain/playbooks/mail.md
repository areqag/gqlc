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
