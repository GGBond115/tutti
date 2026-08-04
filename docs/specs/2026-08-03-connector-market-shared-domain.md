# Connector Market Shared Domain

Status: shared core, signed release delivery, Tutti daemon implementation host,
and renderer service implemented; exact package-cohort release and downstream
TSH pin remain rollout gates.

## Goal

Make Tutti the source repository for one connector-market capability that runs
inside both `tuttid` and the TSH desktop daemon. Both hosts share domain,
artifact, operation, recovery, local HTTP, and renderer semantics while keeping
independent durable state and runtime-specific adapters.

## Non-Goals

- Renderers do not call the remote Connector Market API.
- The shared package does not own product endpoints, credentials, state-root
  selection, SQLite schema, OS integration, or runtime process policy.
- Tutti does not infer missing release metadata from untyped raw JSON.
- This phase does not add placeholder daemon handlers or UI backed by fixture
  success responses.

## Two API Contracts

### Remote Connector Market API

The Connector Market service owns the authoritative, versioned schema and
publishes a generated Go client. It exposes immutable published releases and
attempt-scoped artifact download resolution.

This is the first production connector release contract. The existing
unreleased connector publish and read shape is changed in place and remains
`schemaVersion: "1"`; there is no connector release v2 or dual-read period.

A release descriptor must bind at least:

- connector key and version
- immutable release identity and manifest digest
- artifact key, digest, size, and media type
- typed implementation kind and configuration schema version
- permissions and authorization kind
- product, platform, and minimum-host-version constraints
- publication and revocation state

Release authenticity must be defined explicitly. The preferred root solution
is a signed release envelope in addition to authenticated transport and digest
verification. Download URLs are short-lived responses and are not persisted.

The shared package may provide a default `CatalogSource` and artifact resolver
over the generated market client. It must not copy the remote schema into the
local daemon OpenAPI fragment.

### Local Daemon API

`packages/connector/market/openapi/connector-market.v1.yaml` is the shared
local daemon fragment. It exposes accepted catalog state, local installation,
authorization, compatibility, workspace bindings, revisions, and durable
operations to renderers.

Each host composes this fragment into its aggregate OpenAPI document, generates
its own server and client, and provides transport mapping. The local daemon is
the authoritative source for renderer state.

## Package And Host Ownership

The shared package owns:

- domain contracts, validation, state transitions, errors, and compatibility
  semantics
- application orchestration, command idempotency, operation exclusion, and
  recovery rules
- the default remote catalog domain adapter
- bounded artifact download, digest and size verification, safe extraction,
  packaged-manifest verification, staging layout, atomic promotion mechanics,
  cleanup, rollback, and reconcile rules
- ports for repository, implementation hosting, credentials, scheduling, outbox,
  transport, and diagnostics
- the local daemon OpenAPI fragment and Valtio renderer service

Each Host owns:

- SQLite repository, migrations, transactions, operation leases, and attempts
- remote base URL, authentication, HTTP client, proxy, TLS, logging, and tracing
- state-root configuration
- implementation hosting and observation, including sandbox, permission, process,
  OS, and credential integration
- secure credentials and authorization callbacks
- durable outbox storage and integration with the existing event fanout
- generated local daemon handlers and renderer backend/event adapters

Default package implementations remain behind ports so special hosts can
replace environment behavior without replacing shared semantics.

## Artifact Preparation And Activation

`ArtifactPreparer` and the daemon `ImplementationHost` use immutable requests
and verifiable receipts. Their durable identity carries at least:

```text
operationId
connectorKey
version
releaseIdentity
releaseDigest
artifact key/digest/size/mediaType
validated manifest
```

The execution boundary is:

```text
operation executor
    -> package ArtifactPreparer
    -> prepared artifact and receipt
    -> daemon ImplementationHost
    -> generation-fenced MCP/CLI routes and observed process result
    -> repository completion transaction and outbox
```

Package archive handling rejects absolute paths, parent traversal, symlink and
hardlink escape, and archives that exceed file-count, individual-size,
expanded-size, or compression-ratio limits. It validates redirects, content
type, timeout, declared size, digest, and the packaged manifest before any
artifact code can execute.

Staging and active directories use the same filesystem when relying on atomic
rename. Updates preserve the previous active version until new activation is
observed. Install, update, uninstall, cleanup, and rollback are idempotent for
the operation and immutable release identity.

## Operation And Recovery Model

Execution is at least once because SQLite, filesystem mutation, runtime
activation, and process restart cannot form one atomic transaction.

The durable installation stages are:

```text
accepted -> downloading -> prepared -> activating -> completed
     |            |            |             |
     +------------+------------+-------------+-> failed
```

The Repository stores lease owner, lease expiry, attempt, stage, immutable
release identity, and error details. Recovery inspects durable operation state,
staging and activation markers, and the Host runtime's observed state before
resuming a stage. A running operation is never blindly replayed from the
beginning.

`clientRequestId` has a database uniqueness constraint with a documented
scope. Artifact and activation work is idempotent for:

```text
operationId + connectorKey + version + releaseDigest
```

The operation executor, not the artifact preparer, owns repository state
transitions and event creation.

## Authorization Recovery

Authorization start is a recoverable operation. The provider uses the
operation or client request identity to return or resume one external
authorization session, and the callback completes authorization in a separate
fenced transaction.

A temporary synchronous implementation is allowed only if it cannot leave a
recoverable `running` operation after the request ends.

## Event Consistency

Connector state changes and invalidation events are inserted into a durable
outbox in the same SQLite transaction. The Host publisher forwards outbox
entries through its existing event stream and records progress.

Events carry monotonic revision or sequence. Reconnect supports replay from a
known sequence; a retention gap forces a full snapshot reload. Events never
replace the authoritative local snapshot.

Before durable replay is available, a transitional Host may use best-effort
events only with full reload on daemon reconnect, window resume, and command
completion. Renderer sequence, workspace-generation, and revision fencing stay
enabled.

## Pre-Release Breaking Transition

The current connector-market changes are not released, so the connector
publish, storage, and public read contracts are updated together instead of
maintaining a legacy connector payload. The outer market service may continue
to support other item types, but connector payload validation and projection
move directly to the initial typed release contract.

The coordinated transition is:

- change the connector publish manifest and validation in the market service
- regenerate the market service and consumer clients in the same delivery
  sequence
- update the shared package mapper and manifest contract to the same
  `schemaVersion: "1"`
- update or republish development connector fixtures; disposable local records
  may be reset
- reject any remaining untyped connector record instead of dual-reading it

No connector v1-to-v2 migration, legacy catalog adapter, synthesized field, or
production fallback is introduced. A failed or invalid remote refresh still
preserves the last-known-good accepted daemon catalog; network failure never
becomes an empty catalog.

## Renderer Integration

Each Host renderer injects a backend adapter over its generated local daemon
client. The shared service owns the Valtio store, initial load, command flow,
workspace switching, revision fencing, and coalesced invalidation reloads.

The Host lifecycle starts the service and connects it to an existing daemon
event fanout. React does not create clients, connect WebSockets, start loads,
dispose services, or merge daemon business state.

Daemon reconnect performs a full reload. An accepted command is followed
through events or the operation endpoint until terminal state.

## Revised Delivery Sequence

1. Harden the shared operation, artifact, authorization, and event contracts;
   add conformance scenarios for crash windows and idempotent replay.
2. Replace the current unreleased connector publish/read shape with the
   authoritative initial `schemaVersion: "1"` release schema and regenerate its
   clients.
3. Add the package default catalog adapter, artifact preparation engine, and
   reconcile rules.
4. Implement `tuttid` SQLite persistence, leases, implementation hosting,
   credentials, durable outbox, scheduler, and startup recovery.
5. Compose the local fragment, implement handlers, and regenerate Go and
   TypeScript clients.
6. Register the shared renderer service with Tutti's generated-client and event
   adapters; validate the headless end-to-end flow before adding UI.
7. Add the Tutti connector-market UI using the shared renderer service.
8. Publish one exact package release cohort.
9. Install the exact Go and npm versions in TSH and implement only its Host
   adapters.
10. Remove disposable pre-release fixtures and temporary transition paths after
    all repositories consume the initial contract.

## Host Integration Gate

Do not add the local fragment to a production daemon aggregate until all of the
following exist:

- an authoritative catalog source using the initial typed release contract
- real persistence and atomic revision transactions
- operation identity, lease, recovery, and terminal-state handling
- safe artifact preparation and a real implementation host for every installable
  implementation
- authorization recovery for every advertised authorization kind
- event delivery with either durable replay or the documented transitional
  refreshes
- handler, persistence, recovery, and renderer-adapter tests

## Current Implementation Checkpoint

The shared package includes:

- public package and release registration
- domain types, state transitions, manifest validation, ports, and an
  application core with revision fencing, request idempotency, immutable
  targets, execution receipts, per-connector exclusion, operation execution,
  authorization recovery, leases, and restart recovery
- secure artifact download/preparation with archive safety limits, packaged
  manifest verification, content addressing, and atomic promotion
- the shared local daemon OpenAPI fragment
- package-resolved fragment support for cross-repository hosts
- the class/interface/dataStore/start/dispose Valtio renderer service with
  stale-response fencing, refresh singleflight, mutation locks, workspace
  switching, invalidation reload, and reconnect reconciliation

The Tutti Host includes:

- a typed remote catalog adapter for the TSH Connector Market endpoint
- SQLite persistence for metadata, accepted releases, bindings, operations,
  leases, and the transactional changed-event outbox
- operation scheduling and startup recovery
- aggregate OpenAPI composition, generated Go handlers and TypeScript client
- a generated-client/event-stream Renderer adapter registered in the workspace
  service container

Tutti now advertises the production `managed_stdio` implementation when its
signed platform/runtime constraints match. It verifies ZK signatures and TSH
artifact grants, prepares an immutable artifact snapshot, and hosts MCP as a
daemon-owned long-lived child or CLI as a sandboxed one-shot Node/Python child.
Routes and every child process are fenced by workspace generation, boot epoch,
catalog freshness, and security revocation.

Authorization remains intentionally limited to `none`; connectors that require
credentials remain visible as unsupported until the daemon credential broker
lands. Durable event replay and connector-market UI presentation remain outside
this checkpoint. The renderer domain service and Tutti adapter are wired and
use authoritative reloads on reconnect, resume, and command completion.
