# ADR 0007: Route each Radarr acquisition to one Admin-selected target

Status: accepted (2026-08-07)

## Context

A draw happens before the group next meets, so an unavailable winner must stay
the Current draw while an Admin arranges its media file. Radarr instances
represent different variants and collections. The correct instance cannot be
inferred before an Admin sees the winner, and Radarr can already synchronize a
movie between related instances.

## Decision

Create one durable Pending acquisition when the Current draw is committed. Do
not add pooled candidates to Radarr. Conceal its movie identity, Admin state,
and notifications until Reveal. After Reveal, an Admin selects one Acquisition
preset. Snapshot its one Radarr instance and target settings so later preset
edits affect only future Acquisitions. Moviepickarr sends the movie only to that
Acquisition target and does not copy it to other instances.

Before the remote mutation, show an Admin Target review and require explicit
confirmation. If Radarr later deletes the locked movie before completion, allow
an explicit retry only against the same target snapshot. Never recreate it or
retarget it automatically.

Use the movie's stored TMDB ID first. If it is absent, resolve and verify its
stored IMDb ID through the selected Radarr instance. Do not use title and year
as an automatic identity fallback. If the applicable exact path fails, an Admin
may explicitly select a TMDB result from a Radarr title-and-year search for this
Acquisition without changing the movie. Discovery of an existing Radarr movie
for review is read-only. Lock the target only after the Admin confirms adoption
or Radarr accepts a new movie. Moviepickarr cannot retarget it after that remote
state exists. If the locked movie later has a different identity, require an
Admin to restore the exact Radarr movie or abandon the Acquisition.

If the movie already exists in the selected instance, reuse it without changing
its monitoring, root folder, quality profile, tags, or minimum availability.
Observe an existing queue item rather than starting competing work. Acquisition
covers the initial grab and does not maintain later releases.

Claim each add and manual grab before the remote request. An ambiguous unlocked
add remains unlocked. An explicit `Check Radarr add` action reads Radarr and
adopts a found movie. Proven absence returns the target to review, and only a
later confirmation can send another add. An ambiguous manual grab remains
claimed. An explicit `Check Radarr status` action reads the selected movie,
queue, and history without sending the release again.

For Automatic mode, claim the search durably before sending the command. After
a timeout or restart, inspect the selected movie and queue. Also look for an
active or recent `MoviesSearch` command that contains exactly the selected
movie. If the command result remains unknown, require Admin action instead of
risking a duplicate automatic search. This chooses an at-most-once
automatic-search handoff when remote state is ambiguous.

Only the selected target can complete the Acquisition by reporting `hasFile`.
Files copied to other instances by external Radarr synchronization do not count.
Marking the Current draw Watched does not block or close Acquisition. All
Acquisition state and controls are visible only to Admins.

An Admin may instead abandon an impossible Acquisition with a required reason.
This clears its persistent attention state without deleting or changing Radarr
data and remains visible in Acquisition history. Every unresolved local mutation
state can be abandoned. Read a locked movie and its queue again before
abandonment. Treat an unlocked in-progress mutation as activity that cannot be
verified. Warn when Radarr has active work or when current activity cannot be
verified. Require the Admin to acknowledge that exact risk. Use revision
comparison so an old remote observation cannot approve a newer local state.
Abandonment remains terminal if Radarr later imports a file.

## Consequences

Moviepickarr needs a durable per-draw Acquisition lifecycle in addition to the
shared integration run ledger. A draw can remain pending across restarts until
an Admin chooses a preset. It also needs a durable Reveal boundary so Admin
surfaces and outbound notifications cannot disclose the winner early.
Multi-instance synchronization and ongoing release management remain external
Radarr concerns. Archived presets and instances retain their identity so target
snapshots remain understandable in history.

Retain one compact Acquisition summary per draw for the movie's lifetime. This
is domain history, not a full event log or a substitute for integration runs.
Keep individual Acquisition actions and routine status checks out of the shared
Runs page. Future explicit Radarr batch or maintenance operations may use the
integration run ledger. This is an explicit Radarr domain-workflow exception to
ADR 0006's rule for recording ordinary single-subject integration work: one
Acquisition can remain active for days and its interface does not fit a
time-bounded integration run.
