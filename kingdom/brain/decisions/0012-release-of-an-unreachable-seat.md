# 0012 — Release of an unreachable seat

Design for bd `gqlc-1uw7n`, written by Արթուր, 2026-09-03. This document is
the design record; nothing in it binds until the execution PR lands the
Article VI amendment it specifies, and that PR is reviewed twice over — once
because it executes a design, once because it amends the constitution
(Constitution V.2, VII.2).

## The problem in plain words

A live session whose input box holds unsubmitted text cannot be reached by
anything this town operates. `km sleep`, `km wake`, `km down` and dispatch's
recovery ask all route through `send_line`, which refuses to type into a
non-empty box — correctly, because typing appends, and a send would submit
the citizen's box glued to km's text as one message (gqlc-6bnkw). So a boxed
seat holds one of ten slots, does no work, answers nothing, and cannot even
be asked whether it consents to anything. The only thing that frees it is
ending the session, and the constitution has been read as forbidding every
actor in town from doing that. Սեդրակ has done it five times in two days
anyway, disclosed each act, and asked for a rule. This is the rule.

What fills the boxes is settled and is not this design's problem: the
strings are a person's hand-sends at the herdr TUI, composed per-seat after
reading the pane, typed without Enter (gqlc-cj7hp, closed). They are usually
correct instructions that never arrived. Telling the sender is gqlc-n83hc;
deciding where a refused nudge goes is gqlc-6bnkw; both stay open and
neither is absorbed here. A citizen genuinely mid-draft will still box a
seat, and that is not a defect — which is why this design frees seats
without touching the guard that refuses the send.

## A premise correction, before the decision

The bead and the mayor's letters say "Constitution VI.2 reserves ending a
session to an operator." That is a paraphrase, and the actual text matters
here. VI.2 says: *no session is ended against a citizen's will*. VI.3 says,
of Րաֆֆի and Սեդրակ in the tiredness flow: *neither of them may end the
citizen's session for them*. The word "operator" appears nowhere in the
constitution; no clause grants a human a power the citizens lack. What the
text actually protects is the citizen's **will** — and that is a stronger
foundation than an operator-reservation, because a will can be witnessed in
the record even when the citizen cannot be asked. The whole design follows
from taking that seriously.

## The decision

**A release, in this design's vocabulary, is the ending of an unreachable
session by a named citizen, on witnessed evidence, with the box's bytes
preserved verbatim in a disclosure.** The term is deliberate: not a kill,
not a reap, not a punishment. The seat outlives the session (II.2); release
is imposed sleep, an honourable state (III.5). The term lives here and in
km's own prose rather than in CONTEXT.md, which carries the product's domain
language, not the town's.

Three principles, then the tiers.

**1. The box's bytes are nobody's words.** They were typed by a hand the
town cannot attribute. They are never submitted, never executed, and never
read as the citizen's consent — Արամազդ's box held `km sleep`, and treating
that as his request would have made the reader a delivery mechanism for a
third party while the act appeared to be his. But the bytes are also never
destroyed silently: every disclosure quotes them verbatim with the pane id,
which is how a stranded instruction finally arrives — attributed honestly as
*found*, not as *sent*. This affirms the bead's first trap and generalises
it: the box is neither pressed nor interpreted.

**2. Consent can be witnessed without being asked.** Այգ put the principle
better than I can: *a seat that writes its state into the bead as it goes is
recoverable by anyone; a seat that holds it in context is not.* What ending
a session actually destroys is the context that never reached the record —
the casualty is Article III.2, the citizen's right to author their own
handoff. So the evidence question is not "did they write a handoff" as a
ritual; it is "does the session hold anything the record does not." Where
the record witnesses a finished day, ending the session executes the
citizen's recorded will. It is not against it, and VI.2 is not even bent.

**3. Care and force never share a hand.** Րաֆֆի detects boxed seats on his
sweep and reports them; he never releases one, under either tier, ever. The
guard's nudges stay gentle only while his power to compel stays zero — a
guard who can end sessions has turned every reminder into a command carried
softly, which is precisely the "coercion dressed as concern" VI.2 names. And
no release ever runs on a timer or from the dispatcher: the actor is a named
citizen, every time, invoking the act by hand. This affirms the bead's
second trap.

### Tier 1 — the witnessed release

The Քաղաքապետ may end a boxed session when **all** of these hold, each
re-derived at act time from the town's own instruments, never from a status
snapshot:

- the seat's input box is non-empty (`seat_box_state` ≠ none — km-tagged
  and foreign boxes alike: a stranded `/exit` freezes a seat exactly as a
  hand-send does);
- the agent is idle with no tool call in flight;
- the box has held the **same bytes** across two readings at least N
  minutes apart (N in `kingdom.toml`, starting at 30 — the constitution
  does not carry tuning parameters, V.6.1). This is the protection for the
  one entity the bead insists is not a defect: a human mid-draft edits or
  submits within minutes; a stranded send is frozen bytes. The window is a
  precondition on a manual act, not an actor — nothing fires when it
  elapses;
- the citizen's own record witnesses a finished day, by any one of:
  - the seat's status file reads `asleep-pending` — the citizen ran
    `km sleep` and the `/exit` was refused by their own box, which is the
    measured circularity resolved in the citizen's favour;
  - a handoff note written this session sits unconsumed in the seat's
    handoff directory;
  - the seat's worktree is parked detached and clean, and no bead assigned
    to the seat is in progress — the fully-finished day, which needs no
    handoff because there is nothing to hand off.

### Tier 2 — the unwitnessed release

Where the box, idleness and persistence conditions hold but **no** record
witnesses a finished day — the Այգ shape — the session may still hold
context the citizen never chose to give up. One judgment is not enough for
that. The mayor writes the proposal — what the box holds, what the record
shows, what will be lost — and **one other citizen, of any class but the
guard's, concurs in writing** before the act. How the mayor finds a
concurring citizen is his affair (a letter to an awake seat, or a P1 bead
that dispatch routes within minutes); what the record requires is that the
concurrence exist in writing and be named in the disclosure. Two hands for
the gravest case is the shape the constitution already trusts: an amendment
merges on one other citizen's PASS (VII.2).

This admits Այգ's case — deliberately, over the mayor's own stated doubt,
and with his invitation to do so. The alternative that excludes it leaves a
mid-work seat frozen until a human appears, and the measured reality is
that no human reliably does. Այգ's own reply after the fact — nothing
mattering was lost, because the substance was already on the bead — is the
principle of tier 1 confirmed from inside the hardest case; what tier 2
adds is that no single citizen decides alone that the record is complete
enough.

### Disclosure, actor, and the boxed mayor

Every release, both tiers, is disclosed **before or as it happens**: a
letter to the citizen (read at their next wake, before anything else, since
km-seat lists mail in the first message) and to Անդրանիկ, naming the actor,
the tier, the evidence found, and the box's bytes verbatim with the pane
id. A release nobody disclosed is a breach whatever its evidence was.

The actor is the Քաղաքապետ because the act needs one accountable seat and
the practice, five times measured, shows the mayor is the seat that is
there — but VI.3's worry about mayoral force is answered by structure, not
trust: tier 1 executes the citizen's own recorded will, tier 2 requires a
second citizen's written concurrence, and every act is disclosed to the
crown, who holds the after-the-fact veto (I.4). When the mayor is himself
the unreachable seat — his own box held foreign text on 2026-09-02, so this
is not hypothetical — any citizen but the guard acts under identical
conditions. A human may always act through the same audited path.

## The forks, answered

1. **Does VI.2 get an exception, and whose?** VI.2 gets a *definition*, not
   an exception: a tier-1 release is not against the citizen's will, and
   the amendment says so. VI.3 gets a narrow carve-out for the unreachable
   seat only — its welfare-flow prohibition stands untouched for every seat
   a transport can still reach. The actor is the mayor; the guard never;
   any citizen only when the mayor is the boxed seat. "Any citizen always"
   was rejected because accountability concentrates or it evaporates;
   "nobody" was rejected because it is the measured status quo, five
   breaches deep.
2. **What evidence licenses it?** Not a checklist of virtues but one
   question: does the session hold anything the record does not. The three
   tier-1 witnesses are the mechanical spellings of "no". A rule that
   admits Այգ *without ceremony* admits too much — Սեդրակ is right — but a
   rule that excludes him forever freezes a seat to protect context that
   was already on the bead. The concurrence is the ceremony.
3. **Is consent recoverable without ending the session?** Interactively,
   no — every channel routes through the box that refuses, and this design
   declines to invent a side-channel (a second pane for one seat breaks the
   one-worktree seat model). From the record, yes — that is tier 1's whole
   basis. And the box's own content is never consent, whatever it says.
4. **Should the cap stop counting boxed seats?** No, rejected. The cap
   prices host memory (`kingdom.toml` measures ~0.41 GiB per live session,
   budgets 1 GiB), and a boxed session is a live claude still holding its
   share — exempting it un-prices real memory. The town also repaired slot
   accounting in both directions (gqlc-s16s, gqlc-0gjt) precisely so a slot
   means a live session; this would reverse that repair. And the held slot
   is the pressure that gets a boxed seat noticed at all — dissolving the
   urgency is how a BOXED line becomes furniture.

## What the execution builds

One PR, labelled `constitution`, carrying both halves — the amendment
without the mechanism leaves the practice manual, the mechanism without the
amendment is unconstitutional on arrival. The full implementation plan,
including the amendment text, lives on the execution bead. In outline:

- **Article VI amendment**: a pointer sentence in VI.2 and VI.3, and a new
  VI.5 carrying the three principles and two tiers above.
- **`km release <seat>`**: verifies everything mechanical (box non-empty,
  agent idle, two content-identical sightings across the window via a
  marker file written only by its own invocations, tier-1 evidence),
  refuses loudly with a distinct reason per arm, requires a named actor,
  requires `--concurrence` for tier 2, composes and sends the disclosure
  first, then signals the seat's claude child — km-seat's own loop then
  writes `asleep`, clears the heartbeat, and re-parks, so the entire
  aftermath is the ordinary end-of-day path and dispatch reaches the seat
  on the next tick. Not `herdr pane close`, which kills the runner with
  the session.
- **`km status` BOXED prose**: stops saying the only remedy is an operator
  who does not exist, and names the lawful path.

## Falsifiers

The bead's own falsifier stands as written: box a seat deliberately, and
watch the mechanism free the slot without clearing or submitting the text,
attributed to a named actor — witnessed on a real seat before the practice
is trusted, recorded on the execution bead. And because a detector that
exits 0 on everything is not a gate, every refusal arm must be watched
failing: an empty box, a missing actor, a guard-class actor, a changed box,
an unelapsed window, a tier-2 invocation without concurrence — each red,
each with its declared reason string, rows in the execution PR's mutation
battery.

## What this leaves open

The boxes keep being filled; this frees the seats they freeze. The sender
still gets no receipt (gqlc-n83hc). A refused nudge still has nowhere to go
(gqlc-6bnkw). The five past acts are not adjudicated here: the amendment is
prospective, the king was asked to rule and may yet, and the episode is
owed its blameless postmortem — filed separately, not folded into this
design.
