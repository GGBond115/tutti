# Issue Execution Coordination

Issue execution combines two independent domains:

- Workspace Issue owns Issue, Task, Run, dependency, acceptance, budget, and
  dispatch-pause facts.
- Agent Host owns Session, Turn, runtime-operation, terminal outcome, and
  lifecycle recovery semantics.

Neither domain mirrors the other's state. `IssueExecutionCoordinator` in
`services/tuttid/service/workspace` is the product-owned integration seam that
maps user intent and canonical Agent facts into Issue commands.

This generic dispatch flow applies to manual and `traditional_plan` Issues.
Accepted `tutti_mode_plan` Issues instead materialize atomically with a
Tutti-owned execution aggregate and active `initial_schedule` checkpoint.
Their materialization creates no Run and never enters the generic eligible-task
dispatcher; later work requires an explicit Tutti execution schedule command.
Every active Tutti checkpoint instead owns one durable main-conversation wake.
The wake asks the source Agent to review canonical execution state and choose a
fenced `schedule` or `acknowledge` command; settling a task never mechanically
dispatches a successor.

## Execution flow

Generic Issue dispatch is split into two phases:

1. Under the per-Issue mutation lock, Issue Manager rechecks policy and creates
   a durable running Run. That Run is the claim that prevents duplicate
   dispatch.
2. After releasing the lock, the Issue run launcher prepares any worktree and
   creates the Agent Session. A launch failure settles the claimed Run through
   the normal idempotent completion command.

Stopping is also split:

1. Under the Issue lock, set `dispatchPaused=true` and snapshot the Issue's
   running Runs.
2. After releasing the lock, request cancellation of each bound Agent Session
   by resolving the exact `issue-run:<runID>` Turn.
3. Settle a Run as canceled only from exact canonical Turn settlement, or from
   a typed adapter result that carries authoritative canceled evidence.

Agent cancellation may synchronously publish a canonical settled-Turn fact.
Because no Issue lock is held across the Agent call, that callback can safely
settle the Run. A failed cancellation leaves the Run running, keeps dispatch
paused, and returns an error; Issue intent must not fabricate Agent outcome.

The non-blocking Run launch gate closes the claim-to-launch race without
holding a mutex across external work. Stop records cancel intent and returns
without waiting for an in-flight Agent create call. Launch revalidates the
durable Run and Issue pause fact before external work; when it completes, it
observes any concurrent cancel intent and performs exact-Turn compensation. If
pause wins before launch begins, the unlaunched claim is canceled without
creating an Agent Session.

Tutti-owned launch intents add a cross-process fence to that local gate. Run
terminalization seals any `prepared` or `leased` intent in the same database
transaction as its settlement checkpoint; prepared scans and lease claims also
require the Run to remain `running`. `MarkDispatched` is a strict current-owner
CAS and is the successful delivery linearization point. If an external create
returns after another replica terminalized the Run, either success or an
ambiguous error performs exact-Turn cancellation. A stale owner whose lease was
reclaimed while the Run is still running does not cancel the valid same-ID
Turn; it schedules reconciliation instead. Startup repair repeats the intent
seal even when the settlement checkpoint already exists. When a leased or
dispatched launch may have created a Turn, the settlement transaction also
prepares a durable cancel-compensation operation. Those operations have their
own owner/lease/attempt state, use the same deterministic submit identity, and
are retried on startup and the regular reconciliation cadence until exact
cancellation is accepted. Compensation uses a bounded context detached from
the original delivery cancellation signal. A retryable cancellation outcome
keeps the operation prepared and queues another pass without aborting startup
or starving launch/running-Run recovery in the same workspace. A successful
stale owner also revalidates the Run before compensating: lease reclaim while
the Run remains running is recovery work, not a cancellation signal.

Tutti main-conversation delivery follows the same durable-operation pattern.
Creating or promoting an active checkpoint prepares its wake in the same
SQLite transaction. The canonical identities are
`<checkpointID>:wake:main:1` and
`clientSubmitID=tutti-execution-wake:<wakeID>`; they are interoperability
contracts, not presentation values. An existing row with different checkpoint,
target, sequence, client-submit, or source-session identity fails closed.

Delivery first checks the exact workspace/source Session and leaves the wake
prepared while that Session is busy. A daemon-unique owner leases the wake,
then sends through Agent Service while Host remains the authority for canonical
Session liveness and `clientSubmitID` lookup. Response loss is recovered by
looking up the same deterministic submit ID. Only the current lease owner may
record dispatch, so restart or replica races converge on one canonical Turn.
The exact Session/Turn settlement changes the wake to `turn_settled`, but does
not resolve the checkpoint: only a correctly fenced checkpoint command may
atomically acknowledge that wake and promote the next backlog checkpoint.

Execution states `orphaned_source`, `completed`, `archiving`, and `archived`
suppress every still-open wake, including `turn_settled`. Suppression is stored
as `canceled` and clears any lease rather than deleting recovery evidence.

## Identity and settlement

Every dispatched Run stamps `clientSubmitID=issue-run:<runID>`. A settled Agent
Turn may complete a Run only when the coordinator resolves that submit ID and
the exact initiating Turn ID matches the settled Turn ID.

Missing, failed, or ambiguous identity resolution is fail-closed: the Run
remains running and reconciliation is scheduled. Reconciliation combines the
same `FindTurnByClientSubmitID` and canonical `GetTurn` queries to recover the
exact settled fact. A different Turn in the same Agent Session must never
settle the Run.

The coordinator consumes `IssueRunSettlement`, a narrow typed fact. Translation
from Agent canonical projection DTOs is isolated at the coordinator adapter;
Issue Manager does not interpret Agent Session or Turn state.

## Lock and transaction rules

The per-Issue mutex serializes local read-modify-write commands in one daemon
process. It must never be held while invoking Agent Host, creating a git
worktree, notifying another Agent conversation, or performing another
potentially re-entrant cross-module action.

The mutex is not a durable transaction boundary. Store commands still need
database-level atomicity or revision/CAS protection for invariants spanning
Run, Task, Issue projection, and budget. Until those store commands are
introduced, the mutex remains a local serialization aid and must not be
described as sufficient cross-process correctness.

## State model

This flow does not need a general-purpose state-machine framework. Durable
facts remain small and direct:

- Issue: `dispatchPaused`, execution policy, budget
- Task: status, acceptance state, latest Run
- Run: running or terminal outcome, Agent Session binding
- Agent Host: Session and Turn lifecycle

UI and orchestration phases are derived from those facts. New boolean flags
must not be used to simulate transactions or hide incomplete cross-domain
operations.

## Recovery

The reconciliation queue is daemon-context-bound and retries transient
failures. It is a fallback for delayed or missed projection delivery, not the
authority for Agent lifecycle semantics. Product timeouts may fail an Issue
Run, but Agent terminal outcomes should come from exact canonical Turn facts.

Durable main wakes enter that queue after Tutti Issue materialization and Run
settlement. Root-Turn settlement also enqueues rather than sending inline, so a
source conversation that was busy is reconsidered without re-entering Agent
delivery from the projection callback. Every queue pass first performs
idempotent suppression and expired-lease repair, then attempts delivery.
Released delivery failures return a pending signal so the existing bounded
queue cadence retains the workspace even when it has no running Runs.

During daemon construction, startup performs only the local durable repair.
It does not call Agent `SendInput` or start the queue while CLI routes and the
listener are unavailable. A one-shot listener-ready hook enqueues the
workspaces after listener information has been published. A readiness gate also
turns any earlier internal enqueue into a pending retry without reaching
`SendInput`. A transient repair or Session observation is retained for queue
retry instead of preventing the daemon from serving other workspaces. Startup
never reclaims an unexpired lease owned by another process.

The former in-memory Tutti Issue completion notifier and dispatcher are not
orchestration authorities. Checkpoint/wake rows plus canonical Agent Host
queries are the restart-safe source of truth.
