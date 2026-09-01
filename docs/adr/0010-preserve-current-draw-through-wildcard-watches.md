# ADR 0010: Preserve the Current draw through Wildcard watches

Status: accepted (2026-08-25)

## Context

The group sometimes wants to watch another movie with a guest or at a separate
time. That choice must not replace the revealed Current draw or rotate Next up.
The alternate movie can already be in a Stash or Pool, or it can come directly
from TMDB. It still needs the same durable Acquisition workflow as a Current
draw.

ADR 0007 defines one durable Acquisition for a Current draw and requires safe,
Admin-routed Radarr work. A Wildcard needs that workflow without a concealed
draw or Reveal phase. Cancellation also needs to end local work without trying
to reverse remote Radarr state.

## Decision

Store each Wildcard as a durable record linked to its host Current draw. Allow
one Active wildcard for the whole group. Any Turn participant can select, watch,
or cancel it. A Guest can observe it but cannot run those commands. The Current
draw cannot be marked Watched until the Active wildcard is watched or canceled.
This keeps the host relationship valid and prevents the turn from moving while
the detour is unresolved.

Each command carries the Current draw or Active wildcard ID that the member saw.
The repository compares that expected ID inside the lifecycle transaction. A
stale picker, watch action, or cancellation fails instead of acting on a newer
group decision.

An existing Pool or Stash movie keeps its Adder and returns to its prior place
when canceled. A direct TMDB selection becomes a movie immediately, uses the
recording member as its Adder, and returns to that member's Stash when canceled.
Selecting an existing Pool movie respects the Pool lock.

Create a visible Pending acquisition in the same transaction that creates the
Active wildcard. Mark its source as `wildcard` and link it to the Wildcard
record. It starts beyond the Reveal boundary and then uses the target selection,
review, handoff, reconciliation, retry, completion, and Admin abandonment rules
from ADR 0007. Marking the Wildcard Watched does not close this Acquisition.

Watching records the movie in the Watched library with its host Current draw.
It does not change the Current draw or Next up. After that transition, the group
can select another Wildcard for the same Current draw.

Canceling returns the movie to its prior place and ends the local Acquisition
requirement. Keep the Acquisition in history with the Admin-facing status
Canceled. Do not delete, stop, or change Radarr data or work. Internally, reuse
the existing `abandoned` terminal mechanics and store a separate cancellation
timestamp. This keeps all worker, reminder, webhook, and target-lock safety
checks closed without treating a group cancellation as an Admin abandonment in
the API.

## Consequences

Movie status gains `wildcard`, and Wildcard history keeps its host draw even
after watch or cancellation. Acquisition records gain a source and an optional
Wildcard link. Canceled Wildcard Acquisitions do not request Admin attention.

Cancellation cannot promise remote rollback. Radarr work that started before
the cancellation can remain in Radarr. The local record stays terminal and no
later Radarr observation reopens it.

The Current draw Hero gains a group-owned detour. Its normal dimensions remain
static across draws. An Active wildcard temporarily takes over its artwork,
movie details, and actions while the Current draw remains accessible as a held
movie.
