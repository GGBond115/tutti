# Connector Daemon

`packages/connector/daemon` composes the Connector Host application inside a
long-running desktop daemon. It owns bootstrap fencing, recovery ordering,
operation scheduling, catalog refresh/reconcile scheduling, and durable outbox
delivery. Bootstrap also calibrates releases with explicit MCP/CLI installation
probes before opening capability publication; catalog-only connectors are not
probed.

`NewHost` only validates and assembles dependencies. It does not publish
capabilities, poll, clean up, or start goroutines. The owning composition root
must call `Start(ctx)` before bootstrap or serving Connector commands. `Start`
registers every background worker under one cancellable lifecycle and rolls
back a failed start. `Close(ctx)` is idempotent, cancels the same lifecycle, and
waits for registered workers plus scheduled operations only until the caller's
deadline; a later call continues waiting for that same shutdown.

The host also starts lifecycle maintenance immediately and repeats it hourly.
Defaults retain terminal operation lookup/idempotency results for 24 hours and
published outbox receipts for one hour, with bounded SQLite cleanup batches;
each run drains eligible backlog through repeated transactions. Active
operations and pending events are outside the cleanup contract.

Accepted/running Operations are also scanned every 500 ms. The in-memory
scheduler is a wake-up optimization only: losing a schedule call or restarting
after an external effect cannot strand durable work.

The module schedules the narrow `application.CatalogSource` port, while hosts
inject their catalog source, event publication, persistence, and execution
ports. The generated Market protocol adapter, DTO parsing, pagination, and
manifest projection are owned by `packages/connector/market/source`; this
daemon module does not import generated Market transport code.

Catalog releases retain the server-owned `releaseDigest`, archive media type,
SHA-256, and byte size without recomputing identity. The source adapter's
authenticated generated client also implements the narrow artifact resolver
port: install exchanges only the release digest for a short-lived descriptor,
and no daemon code constructs a download URL from the deprecated storage key.

Hosts with an account-scoped runtime call `BootstrapForScope`; the daemon
reuses that explicit scope for recovery retries. The legacy `Bootstrap` method
retains Tutti's device-global behavior through the default runtime-binding
resolver.

Remote runtimes inject `CapabilityPublicationController`; bootstrap awaits its
fail-closed/open commands. Same-process Tutti runtimes remain compatible with
the synchronous implementation-host publication gate.

Account logout and switching use `Host.FenceForScope` to close remote
publication, fail-close all processes, and force a later bootstrap even when
the same account logs in again. The account-boundary fence never admits or
starts a runtime with retired authority; per-Connector deactivation remains a
normal uninstall/reconcile concern.

The active account scope also bounds authorization receipt polling. Snapshot
sync atomically converges the account Projection and surfaces matching private
receipts, but does not terminalize them. The daemon is the single scheduler for
this recovery path: while holding the lifecycle fence it updates the scoped
Runtime Desired and waits until Observed records that exact generation before
resolving those receipts. Runtime convergence is private durable state and does
not consume the public one-active-Operation slot.
WebSocket events are only refresh hints; a five-minute level-triggered pass
reconciles every installed remote authorized Connector so a lost event or an
interrupted earlier pass cannot leave route state stale.

A continuous scanner also claims due Runtime Desired rows with bounded
cross-Connector concurrency. Same-Connector duplication is prevented by the
durable lease and desired-generation CAS; different Connectors may reconcile in
parallel. A new daemon boot treats every older-boot Observed receipt as stale.
The scanner compares three independent facts: durable Desired, the cached
Observed receipt, and a bounded physical-route Snapshot supplied by the
Connector runtime. Matching Desired/Observed is not convergence when the
physical route is missing or degraded. Physical Watch events only enqueue a
coalesced wake-up; they never execute reconcile in the runtime reader
goroutine. A revision gap, overflow, or closed Watch invalidates the edge stream
and forces a fresh Snapshot, while an independently configurable 30-second
full-jitter pass remains the anti-entropy fallback for lost events. The 500 ms
durable due scan never reads physical Snapshot state. Intentional removal, replacement, and close publish
ordinary topology changes; only loss of the current managed route is reported
as an unexpected exit.

Repair continues to use the existing desired-generation CAS, durable lease,
bounded per-attempt timeout, and persisted full-jitter retry deadline. The
failure budget is scoped to a Desired generation: three consecutive launch or
early-exit failures persist degraded readiness, six persist failed readiness
and suppress automatic starts, and a new explicit generation resets the
budget. Only a periodic exact healthy physical observation resets accumulated
failures; an activation Watch edge cannot erase early-exit history.
The implementation host combines a global admission/fence barrier with a
Connector-keyed lifecycle lane: account switching and FailClosed wait for every
in-flight route transition, while different Connectors may install, authorize,
or reconcile concurrently. Shared download and package-install resources use a
bounded semaphore rather than a long global lifecycle lock.
