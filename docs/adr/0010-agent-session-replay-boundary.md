# Agent Session Replay has a shared application core

**Accepted, amended 2026-07-29**

Tutti and TSH both record and replay Agent Sessions, so provider-neutral
Recording and Cassette contracts live in `packages/agent/session-replay`. The
package owns their identities, Recording status transitions and workflow,
fixed-batch replay preparation, Cassette schema, semantic checkpoint model,
allowlist, size policy, and integrity validation. Its ports cover metadata,
semantic Replay State, artifact publication, and provider-tape recording.
Product runtime adapters own
ephemeral replay process identity, progress, cancellation, and settlement.

Recording is a mutable task that produces zero or one Cassette. Its replay
payload is immutable, while its user-visible name may change.
Cancelling a Recording discards its candidate artifact and metadata, so
canceled Recordings do not appear in recording history.
Cassette is a portable artifact whose local database row is only a rebuildable
catalog entry. Cassette is the only persistent Replay artifact. A Replay Surface
executes one Cassette inside a transient Replay Workspace; neither Surface
playback state nor Workspace execution state is stored as product metadata.

The name lives in `cassette.json`, defaults to the Recording creation timestamp,
and may be edited from Desktop. A rename rewrites that manifest, recalculates
its SHA-256, and commits the new Recording and Cassette metadata together.

Each product supplies adapters. Tutti keeps SQLite migrations, semantic state
capture/restore, HTTP, local state-root resolution, daemon composition, and
Electron launch in `services/tuttid` and `apps/desktop`. TSH supplies equivalent
adapters without importing Tutti product code. Tutti's filesystem artifact
adapter lives under `services/tuttid/data/agentsessionreplay`; its service layer
only maps Workspace DTOs and applies local target policy. Replay runtime state
lives only in the isolated Desktop, daemon, and SQLite runtime and is discarded
with that runtime. Cassette content never depends on a product database.

The shared package remains the workflow owner. Tutti's service and Desktop
layers own only their adapters and composition decisions; they must not grow a
second Recording/Cassette state machine.

Final replay verification compares typed Agent, Tutti Mode, Workflow, and Issue
business state. It
does not compare provider-discovered runtime context, capability catalogs, or
usage counters because those values describe the current runtime environment,
not the recorded scenario. Stable semantic identities are restored directly;
comparison reports the first exact business-state path mismatch.

Recording candidates and published Cassettes also have separate physical
locations. A completed candidate is published under its own Cassette id only
after an allowlist audit. The portable artifact contains its sole top-level
manifest, ordered Activity Events, optional semantic initial state, required
semantic expected state, provider protocol tape, and explicitly referenced blobs. It
must reject logs, screenshots, SQLite databases, workspace copies, credentials,
and every other unrecognized file. Provider frames are limited to 8 MiB per
decoded payload and 256 MiB on disk; the complete Cassette is limited to 384
MiB. Manifests record per-file and per-provider-frame size evidence so anomalous
growth is attributable instead of hidden by compression.

Provider protocol tape mechanics remain in `packages/agent/daemon/runtime`.
Tutti Desktop asks the daemon to validate and resolve one fixed Cassette batch,
then launches the separate Electron adapter with Cassette, root Session, and
Cassette-directory bindings. This preparation creates no mutable execution
metadata. The isolated daemon uses a fail-closed replay transport and does not
install the real runtime preparer, provider command resolver, extension runtime
resolver, or provider availability probe. Replay does not add a switch to
`ProcessSpec`.

The JavaScript replay runner remains a temporary Tutti Electron adapter. It
reads the shared `cassette-policy.json`; it does not define a second Cassette
schema or size policy. Activity-engine intents are dispatched into the isolated
renderer engine, their correlated command effects are verified there, and only
operations without an engine entrypoint use direct daemon stimuli. A normal
direct `session.send` waits for canonical Session idle; steer does not.

Desktop owns the recording toolbar, recording list, replay-window controls,
feature gating, and product copy. Completed recording rows launch Replay
directly. The primary workspace window does not own a selected Recording or
render Replay pause/checkpoint controls. AgentGUI exposes only generic host
render slots and contains no recording/replay contracts, state, provider
policy, controls, or copy.

Replay playback is monotonic inside one Replay Surface. Pause and resume keep
the same Surface. Moving to the next stable checkpoint temporarily fast-forwards
recorded timing, but still consumes every provider frame and performs every
outbound assertion. Moving backward or restarting must replace the transient
Surface or Replay Workspace; it must not rewind an already-mutated daemon,
database, or provider cursor in place.

Provider frames and activity events share the daemon playback state. Activity
events advance by their recorded `occurredAtUnixMs` offset, freeze while Replay
is paused, scale with Replay speed, and skip recorded waits during checkpoint
fast-forward. Effect verification starts only when its recorded time is
reached; a long-running Turn must not be shortened into a runner timeout.

Cassette schema v7 is the only accepted schema. It stores the ordered
`activity-events.jsonl` stimulus stream and a required
`checkpoint-plan.json`, plus portable composer defaults under
`replayPrerequisites`; no older reader, migration, or fallback exists.
Published artifacts contain no source Workspace ID. Replay creates a
fresh transient Workspace and binds it only at daemon-restore and product-event
boundaries; user payload strings are not recursively rewritten.
Queue and steer are recorded as activity-engine intents plus correlated command
effects, so replay rebuilds the same transient engine state instead of reducing
those actions to HTTP calls.

Schema v7 serializes Replay Checkpoints. Each checkpoint owns a vector Replay
Cursor, a bootstrap, Activity-boundary, or Provider-observation Trigger,
portable Logical Subjects, and canonical Readiness Predicates. Recording
observes but never pauses. Replay holds every lane at the selected cursor and
reports a checkpoint reached only after its trigger, canonical commit, and
readiness are confirmed. Renderer hydration adds the runtime-only Inspectable
Checkpoint gate.

The replayable interaction contract has a single cross-language source:
`activity-contract.json` in the shared core declares, per intent type, the
allowed effect command types and whether an effect is required. The renderer
keeps one registry module in sync with it for correlation, stable effect
fields, readiness, and rebase rules; the Go core enforces it when an event is
recorded, before a Recording completes, and when a Cassette is validated.
Sealing is fail-closed: a replayable command without a resolvable causing
intent, or still awaiting its result, fails recording completion instead of
publishing a Cassette that can only fail later during replay. Replay bindings,
drivers, and coordinators mount only inside the isolated replay runtime, and
the renderer recorder exists only while a Recording is active; the normal
Desktop path carries no replay machinery.

The default-off `agent.sessionRecording` preference gates process composition
at startup. The renderer waits for initial persisted preference hydration
before constructing the Workspace service container; Replay consumes that
hydrated decision without owning preference loading or update propagation.
Disabled Desktop composition creates no Replay manager, access
adapter, IPC handler, renderer Replay service, recording binding, recorder map,
observer map, or Engine observer. Enabling composition preserves live recorder
attachment only for an active Recording. A preference change does not
retroactively rebuild a running daemon or renderer; it takes effect on the next
process composition.
