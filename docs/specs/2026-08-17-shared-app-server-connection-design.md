# Shared App-Server Connection Design

Status: Tutti implementation and native test wiring complete; tsh adaptation
pending

## Implementation Status (2026-08-17)

The Tutti worktree now contains the first product implementation of this
design:

- `packages/agent/daemon/runtime` owns the shared connection Registry,
  permanent response/notification router, per-Thread ordered mailbox,
  generation fence, single-flight startup, physical-process retirement, and
  split cleanup consumption;
- `packages/agent/runtimeprep` produces a stable Process Profile and a
  Session-specific Thread overlay for Codex and Tutti Agent;
- Agent Host transports the preparation DTO unchanged and adds no Session,
  Turn, Goal, or recovery semantics;
- `services/tuttid` supplies a durable device identity, a fresh runtime
  generation, and an identity for the concrete ProcessTransport composition,
  then transfers the process and Thread cleanup leases to the daemon adapter;
- a dedicated Thread-scoped model credential/config field writes
  `experimental_bearer_token` into that Thread's provider config and removes
  `env_key`. Codex reads `env_key` from the physical app-server environment,
  while the Thread environment overlay only controls shell subprocesses, so the
  credential never enters the shared process environment;
- scripted subprocess tests cover process reuse, Thread routing, detach,
  cancellation, connection retirement, and generation fencing; product tests
  cross the real temporary filesystem and default tuttid composition seams;
- Composer model probes use a lightweight runtime preparation that skips
  Session Skills; a later live Session upgrades the matching process profile in
  place before creating its own Thread overlay. Capability probes keep the
  complete Skill preparation. All probes receive independent one-time Thread
  lease identities while retaining the same compatible process-profile
  fingerprint, so concurrent probes cannot release each other's Thread
  preparation or spawn a second compatible app-server;
- the native Windows workflow selects the daemon, runtimeprep, Host DTO, and
  tuttid composition evidence.

This status is not the tsh cutover. tsh must first upgrade to the released
package cohort and adapt its VM-side preparation/transport ownership as
described below. Until then, a package bump alone cannot enable sharing there.

## 1. Decision Summary

Tutti should move Codex-protocol providers from one app-server process per
Agent Session to one app-server connection per compatible process profile.
One connection owns one provider process and multiplexes many provider Threads.

The normal steady state is:

```text
tuttid or tsh desktopd runtime generation
  |
  +-- local:codex process profile
  |     `-- 1 x codex app-server
  |           +-- Thread A <-> Agent Session A
  |           +-- Thread B <-> Agent Session B
  |           `-- Thread C <-> Agent Session C
  |
  `-- local:tutti-agent process profile
        `-- 1 x tutti-agent app-server
              +-- Thread D <-> Agent Session D
              `-- Thread E <-> Agent Session E
```

This is not one process for the entire machine. The unit of sharing is one
`AppServerConnectionKey`. Codex and Tutti Agent never share a process. Different
execution hosts, executable identities, accounts, process-global configuration,
or replay modes also receive different connections.

The implementation must first replace the current single active message-handler
transport. Sharing a process before that refactor would allow notifications and
server requests from one Thread to be consumed by another Session's handler.

Agent Host remains the owner of canonical Session, Turn, Goal, runtime-operation,
and recovery semantics. The shared connection, provider Thread binding, JSON-RPC
routing, process lifetime, and backpressure remain provider-adapter concerns in
`packages/agent/daemon`.

## 2. Goals

- Reuse one initialized app-server connection across compatible Agent Sessions.
- Preserve a distinct provider Thread identity and ordered event stream for each
  Agent Session.
- Route concurrent responses, notifications, approvals, tool calls, and child
  Thread events without cross-Session leakage.
- Keep provider-global model, account, rate-limit, Skills, plugin, and connection
  state at the connection boundary.
- Keep Thread-local cwd, model, reasoning, permissions, instructions, tools, and
  lifecycle state at the Thread boundary.
- Give `tuttid` and tsh the same implementation through the reusable Agent
  packages.
- Preserve Agent Host's initialization, provider-acceptance, terminal, cancel,
  and recovery contracts.
- Keep process, path, environment, executable, and cleanup behavior portable
  across POSIX and Windows.

## 3. Non-Goals

- Do not merge Codex and Tutti Agent into one process.
- Do not move canonical Agent lifecycle rules into the runtime adapter.
- Do not infer Session ownership from the most recently active handler.
- Do not replay a user prompt after a connection failure.
- Do not make `thread/unsubscribe` mean Turn cancellation.
- Do not keep the old per-Session and new shared implementation as permanent
  parallel paths after the live cutover.
- Do not multiplex existing Session Replay cassettes in the first release.
- Do not expose provider connection identities as canonical Session identities.

## 4. Evidence And Reference Baseline

### 4.1 Official OpenAI protocol

The public [Codex App Server documentation](https://learn.chatgpt.com/docs/app-server)
establishes these protocol facts:

- one transport connection is initialized exactly once;
- the same connection can call `thread/start`, `thread/resume`, and
  `thread/fork` for multiple Threads;
- `turn/start` targets a specific `threadId`;
- `thread/start` automatically subscribes the connection to the new Thread;
- `thread/loaded/list` returns all Threads loaded in the app-server process;
- `thread/unsubscribe` removes the current connection's subscription and may
  lead to a later idle unload;
- every Thread in one app-server process shares the selected Code Mode Host;
- responses correlate by JSON-RPC request ID while notifications have no ID.

The documentation is the public protocol authority. The source references below
explain the server's implementation and tests at one pinned revision.

### 4.2 OpenAI Codex source revision

Repository: [openai/codex](https://github.com/openai/codex)

Inspected revision:

```text
commit: fe614a6304ef804be74a622e482fdd75977abcba
date:   2026-08-13T09:00:23Z
title:  Add Guardian V2 extension scaffold (#38336)
```

Line numbers are anchors for this revision. Symbol names and repository-relative
paths are the durable lookup keys when upstream lines move.

| Source position                                                                                                                                                                                                                                                    | Observed implementation fact                                                                                                                                                                                                                       | Tutti design consequence                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`codex-rs/core/src/thread_manager.rs`, `ThreadManager`, `ThreadManagerState`](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/core/src/thread_manager.rs#L216-L353)                                                        | One process-wide manager owns `HashMap<ThreadId, Arc<CodexThread>>` plus shared auth, models, Skills, plugins, MCP, Code Mode, extensions, and Thread store services. `StartThreadOptions` still carries per-Thread config and tools.              | Tutti must share process-global services through `AppServerConnection`, but keep per-Thread configuration in `ThreadBinding` and request overlays.          |
| [`codex-rs/app-server/src/message_processor.rs`, app-server construction](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/app-server/src/message_processor.rs#L249-L291)                                                    | One `ThreadStateManager`, one process-scoped Thread store, and one `ThreadManager` are created for the app-server. The source explicitly says config reloads may affect per-Thread behavior but must not move the process-scoped persistence root. | The persistence root and provider home belong in the process profile. A Session must not overwrite them during Thread startup.                              |
| [`codex-rs/app-server/src/thread_state.rs`, `ThreadStateManagerInner`](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/app-server/src/thread_state.rs#L303-L390)                                                            | The server tracks live Connections, Threads, and the set of Thread IDs subscribed by each Connection.                                                                                                                                              | Tutti needs explicit `bindingsBySession` and `ownerByThread` indexes. Ownership cannot live in a single current handler.                                    |
| [`codex-rs/app-server/src/request_processors/thread_lifecycle.rs`, `THREAD_UNLOADING_DELAY`](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/app-server/src/request_processors/thread_lifecycle.rs#L1-L117)                 | A loaded Thread is eligible for unload only after it has no subscribers and is inactive. The default delay is 30 minutes.                                                                                                                          | Session detach sends `thread/unsubscribe`; it does not terminate the shared process. Thread memory reclamation remains a server concern.                    |
| [`codex-rs/app-server/src/request_processors/thread_lifecycle.rs`, listener dispatch](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/app-server/src/request_processors/thread_lifecycle.rs#L318-L380)                      | The listener obtains the subscribed Connection IDs for the Thread, creates a `ThreadScopedOutgoingMessageSender`, and emits only to those Connections.                                                                                             | Tutti's client-side equivalent is one permanent router followed by one ordered reducer lane per `ThreadBinding`.                                            |
| [`codex-rs/app-server/src/outgoing_message.rs`, `OutgoingMessageSender` and `ThreadScopedOutgoingMessageSender`](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/app-server/src/outgoing_message.rs#L104-L190)              | Process-wide outgoing state correlates requests by ID. The scoped sender carries both target Connection IDs and a Thread ID.                                                                                                                       | Responses and Thread-scoped messages require separate indexes. A global call mutex or handler slot is the wrong ownership model.                            |
| [`codex-rs/model-provider-info/src/lib.rs`, `ModelProviderInfo::api_key`](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/model-provider-info/src/lib.rs#L282-L298)                                                         | A provider configured with `env_key` resolves its API key with `std::env::var`, so the lookup reads the physical app-server process environment rather than `shell_environment_policy`.                                                            | Session model-plan credentials cannot use the shared process env or the shell overlay. Tutti must send a Thread-scoped provider credential/config override. |
| [`codex-rs/app-server/tests/suite/v2/thread_unsubscribe.rs`, `thread_unsubscribe_during_turn_keeps_turn_running`](https://github.com/openai/codex/blob/fe614a6304ef804be74a622e482fdd75977abcba/codex-rs/app-server/tests/suite/v2/thread_unsubscribe.rs#L91-L235) | The upstream integration test unsubscribes while a deterministic tool call is blocked and proves the Turn remains running.                                                                                                                         | Tutti must never use unsubscribe as cancellation or claim that detaching a Session stopped provider work.                                                   |

### 4.3 ChatGPT desktop production artifact

The inspected local installation was:

```text
ChatGPT.app CFBundleShortVersionString: 26.810.41047
ChatGPT.app CFBundleVersion:            6570
bundled codex:                          codex-cli 0.148.0-alpha.9
artifact: /Applications/ChatGPT.app/Contents/Resources/app.asar
```

The ASAR was extracted directly. Its production bundles were minified and had
versioned filenames. No source map was present. The bundled code exposed these
observable structures and methods:

- `appServerConnectionRegistry.getConnection(hostId)`;
- `getMaybeConnection(hostId)` and enumeration of host IDs;
- one connection offering `startThread`, `startTurn`, `interruptTurn`, and
  `unsubscribeThread`;
- `thread/list`, `thread/loaded/list`, `thread/read`, and other read requests in
  a bounded scheduler with prioritization and coalescing;
- routing and tracing dimensions containing Thread ID and renderer
  `webContentsId`;
- reconnect-capable transports;
- app-server launch containing `features.code_mode_host=true` and `app-server`.

This artifact is implementation evidence, not a public source contract. Names
that survive minification are useful, but reconstructed module boundaries and
types are not authoritative. The target architecture therefore uses the public
protocol and open-source Rust implementation for semantic claims, and uses the
desktop artifact only to validate the client-side Registry/Connection/Scheduler
shape.

To repeat the artifact inspection on a matching installation:

```sh
npx --yes @electron/asar extract \
  /Applications/ChatGPT.app/Contents/Resources/app.asar \
  <temporary-output-directory>

rg 'appServerConnectionRegistry|thread/loaded/list|thread/unsubscribe|supportsReconnect' \
  <temporary-output-directory>
```

Do not commit extracted bundles. Their names and contents change with every app
release.

## 5. Baseline Constraints Addressed By The Implementation

This section records the pre-cutover evidence that motivated the design. The
Tutti implementation status above supersedes the present-tense statements for
the shared app-server path; the isolated ACP path retains its existing
contracts.

### 5.1 Baseline: process ownership was Session-scoped

[`CodexAppServerAdapter`](../../packages/agent/daemon/runtime/codex_appserver_adapter.go)
currently owns:

```go
sessions        map[string]*codexAppServerSession
retiredSessions map[string][]*codexAppServerSession
lifecycleLocks  map[string]*codexAppServerSessionLock
```

The key is the Agent Session identity. `codexAppServerSession` contains both
physical connection ownership and logical Thread state. `Close` and release
paths close the Session's client and therefore its provider process.

This model guarantees at most one process per Agent Session. It cannot express
one process shared by several Sessions.

### 5.2 Baseline: the transport had one active handler slot

[`acp_client.go`](../../packages/agent/daemon/runtime/acp_client.go) contains:

```go
callMu sync.Mutex
active *acpActiveHandler
pending map[int64]*acpPendingCall
```

`Call` and `CallWithTimeout` hold `callMu` for the whole request. A handler-based
call installs one `active` handler. The app-server wrapper documents the same
constraint in
[`codex_appserver_client.go`](../../packages/agent/daemon/runtime/codex_appserver_client.go):
a handler-carrying call claims the single active message-handler slot.

The pending response map already proves that response correlation belongs to
request ID. The remaining notification path must stop depending on the active
caller.

### 5.3 Baseline: runtime preparation created Session-scoped homes

[`packages/agent/runtimeprep`](../../packages/agent/runtimeprep) currently
materializes Session-scoped `CODEX_HOME` and `TUTTI_AGENT_HOME` directories.
That isolation is correct for one process per Session, but it makes the provider
home unsuitable as a shared process root.

[`ProviderLaunchPrepareResult`](../../packages/agent/daemon/runtime/provider_launch_prepare.go)
currently returns one command, environment, cwd, and cleanup callback. It does
not distinguish process-lifetime material from Thread-lifetime material.

The implemented provider-state owner now separates those lifetimes. Shared
Codex/Tutti Agent process profiles point at
`agent/provider-state/<ProviderStateID>/{codex-home,tutti-agent-home}` while
synthetic profile roots and Session Thread overlays remain under
`agent/runs/` and are lease-cleaned. `ProviderStateID` is derived only from
provider, target, and stable account/authority data; runtime generation, model,
cwd, workspace, transport, and Session identity are excluded. Host persists the
ID in canonical `InternalRuntimeContext` and restores it on resume.

Legacy Codex migration is exact and bounded: a persisted legacy home authority
takes precedence, followed by the current managed Session run home, explicit
`agent.codexHome` aliases, and known old `appserver-profile-*` roots. Only a
unique rollout whose first `session_meta` record names the canonical
`provider_session_id` is copied, atomically, into the durable home. No other
Home files are copied. Ordinary missing rollout state is a preparation error;
imported sessions keep the Host-owned recreate mode.

Host tombstone purge currently has no provider-state reference authority. Since
multiple Sessions may share a provider state, permanent Session cleanup retains
the provider-state root and its rollouts rather than risking deletion. This is
the explicit retention policy: the current lifecycle never deletes a
provider-state root or rollout because it has no authoritative reference owner
that can prove the root is unused. Rollout replacement
is delegated to POSIX rename or Windows native `MoveFileEx(REPLACE_EXISTING |
WRITE_THROUGH)`, and the native Windows lane runs the owner test.

### 5.4 Baseline: force cancellation closed the process

[`codex_appserver_cancel.go`](../../packages/agent/daemon/runtime/codex_appserver_cancel.go)
first requests `turn/interrupt`. When the graceful window expires, it closes the
app-server process to preserve a bounded cancellation guarantee.

With a shared process, this fallback has a connection-wide blast radius. The
new design must make that effect explicit and settle every affected active Turn.

### 5.5 Remaining: tsh owns an additional preparation adapter

tsh composes the reusable Agent adapters, but it also owns a VM-side launch
preparer in:

```text
tsh/cmd/desktopd/agent_provider_launch_preparer.go
tsh/cmd/tsh-bundle-services/internal/managedagent/runtime_store.go
```

The current managed runtime path is keyed by Agent Session ID and creates
`runtimeprep/runs/<session-id>/codex-home`. Therefore the first shared-process
release requires one tsh contract update. A package version bump alone cannot
change the existing tsh storage and preparation shape.

## 6. Target Ownership Model

```text
Agent Host
  owns canonical Session / Turn / Goal / runtime-operation lifecycle
       |
       v
CodexAppServerAdapter
  translates Host operations to provider protocol
       |
       v
AppServerConnectionRegistry
  ConnectionKey -> one AppServerConnection generation
       |
       +-- AppServerConnection
       |     +-- provider process and initialized JSON-RPC transport
       |     +-- request scheduler and pending response callbacks
       |     +-- process-global account/model/rate-limit state
       |     +-- bindingsBySession
       |     `-- ownerByThread
       |
       +-- ThreadBinding for Agent Session A
       +-- ThreadBinding for Agent Session B
       `-- ThreadBinding for Agent Session C
```

### 6.1 `AppServerConnectionRegistry`

The Registry owns physical connection identity and process lifetime.

```go
type AppServerConnectionKey struct {
    Provider             string
    ExecutionHostID      string
    RuntimeGeneration    string
    TransportScopeID     string
    ExecutableIdentity   string
    ProcessProfileDigest string
    CaptureMode          AppServerCaptureMode
}
```

The Registry must:

- acquire one connection with single-flight startup;
- return an existing healthy generation for the same key;
- never return a Codex connection for Tutti Agent or the reverse;
- replace a dead generation atomically;
- close each physical process exactly once;
- keep failed-close ownership until cleanup is actually resolved;
- shut down all connections when the owning daemon/runtime generation stops.

`AgentSessionID`, Workspace ID, room ID, cwd, model, reasoning effort, sandbox,
approval policy, instructions, and Turn input are excluded from the key unless
they alter process-global behavior.

### 6.2 `AppServerConnection`

```go
type AppServerConnection struct {
    key        AppServerConnectionKey
    generation uint64
    process    ProcessConnection
    client     *AppServerJSONRPCClient

    pendingRequests   map[RequestID]*PendingRequest
    bindingsBySession map[AgentSessionID]*ThreadBinding
    ownerByThread     map[ProviderThreadID]AgentSessionID
    unknownByThread   *BoundedUnknownThreadBuffer

    scheduler  *AppServerRequestScheduler
    globalState AppServerGlobalState
}
```

It owns:

- process launch and initialization;
- one permanent JSON-RPC read loop;
- request ID allocation and response correlation;
- Thread routing indexes;
- process-global notifications and caches;
- request admission, priority, backpressure, and read coalescing;
- connection diagnostics and generation fencing;
- fan-out when the connection dies.

It does not own canonical Agent lifecycle decisions.

### 6.3 `ThreadBinding`

```go
type ThreadBinding struct {
    AgentSessionID  string
    ProviderThreadID string
    Generation      uint64

    State                codexAppServerSessionState
    ActiveTurn           *codexAppServerTurnState
    PendingInteractions  map[RequestID]*PendingInteraction
    ChildThreadOwners    map[ProviderThreadID]ProviderThreadOwner
    Reducer              *OrderedThreadReducer
    PreparationLease     ThreadPreparationLease
}
```

It owns the existing Session-level provider projection:

- provider Thread ID;
- active provider Turn;
- settings and Thread-local state;
- Goal evidence and provider generation bindings;
- pending approval/input/tool interactions;
- child Thread ownership;
- ordered event reduction;
- Thread-lifetime cleanup.

It does not own a process or JSON-RPC client.

### 6.4 Process profile and Thread overlay

The preparation contract must be split conceptually into:

```go
type ProviderRuntimePreparation struct {
    ProcessProfile ProcessProfile
    ThreadOverlay  ThreadOverlay
}
```

| Process profile: shared connection identity | Thread overlay: one provider Thread |
| ------------------------------------------- | ----------------------------------- |
| provider executable and version             | cwd and canonical rail placement    |
| launch argv and transport mode              | model and reasoning effort          |
| stable provider home                        | sandbox and approval policy         |
| account/auth authority                      | base/developer instructions         |
| upstream endpoint and proxy                 | dynamic tools and Thread-scoped MCP |
| process-global environment                  | per-Session invocation credentials  |
| stable Skills/plugin roots                  | canonical Session context           |
| live/record/replay mode                     | Turn input and output schema        |

The implementation may evolve the existing `ProviderLaunchPrepareResult`
instead of introducing these exact public type names. The semantic split is the
contract.

Session-specific secrets, room bindings, connector proofs, invocation tokens,
and dynamic tools must not enter the shared process environment. They must be
delivered through a Thread- or invocation-scoped protocol field. If a provider
cannot represent a required value below process scope, that value must split the
process profile rather than leak into other Threads.

`skills/extraRoots/set` is connection-global. Only stable roots valid for all
Threads in the profile may be installed there. Project Skills should be resolved
by Thread cwd where the provider supports it. Arbitrary incompatible roots must
split the profile; they must not be overwritten on every Session start.

## 7. Runtime Flows

### 7.1 Acquire and initialize

```text
Host operation
  -> prepare ProcessProfile + ThreadOverlay
  -> Registry.Acquire(ConnectionKey)
       -> existing healthy generation: return it
       -> missing/dead: single-flight launch
            -> start process
            -> start permanent read loop
            -> initialize exactly once
            -> install stable process-global Skills/config
            -> publish connected generation
```

The connection starts lazily on the first operation that needs it. Once
started, a healthy live connection remains available for the daemon/runtime
generation. Detaching the last Agent Session does not immediately close it.

### 7.2 Start a Thread

```text
Host Start
  -> acquire connection
  -> register provisional binding by AgentSessionID
  -> send thread/start with ThreadOverlay
  -> receive response(thread.id)
  -> atomically add ownerByThread[thread.id]
  -> drain buffered early notifications for thread.id
  -> publish provider Session identity through existing Host barrier
```

The provisional binding must exist before `thread/start` is sent. Codex can emit
`thread/started` or other notifications before the request response reaches the
client. A bounded unknown-Thread buffer closes that race without assigning the
message to whichever Session happened to call last.

### 7.3 Resume a Thread

```text
Host Resume
  -> resolve durable provider Thread ID
  -> acquire connection
  -> register provisional binding with expected Thread ID
  -> send thread/resume(threadId)
  -> validate returned identity
  -> install ownerByThread mapping
  -> publish buffered observations through Host initialization barrier
```

Resume must not replay a prompt. A mismatched returned Thread ID fails closed.

### 7.4 Start and steer a Turn

`turn/start`, `turn/steer`, approval responses, tool responses, and
`turn/interrupt` carry the exact provider Thread/Turn identity. The scheduler
may run requests for different Threads concurrently. Mutations for one Thread
remain ordered by its Binding reducer and existing Turn state machine.

The Host provider-acceptance barrier remains unchanged: provider output or
interaction cannot become canonical until the exact provider Turn identity has
been durably associated with the canonical Turn.

### 7.5 Detach or close one Agent Session

```text
Close/ReleaseLiveSession
  -> settle or supersede pending interactions as required
  -> send thread/unsubscribe(threadId)
  -> remove ownerByThread and bindingsBySession entries
  -> run Thread-lifetime cleanup
  -> keep AppServerConnection alive
```

Detaching does not cancel an active Turn. Callers that need cancellation must
invoke the Host cancellation path before detach.

### 7.6 Graceful cancellation

```text
Cancel Session A
  -> turn/interrupt(threadA, turnA)
  -> wait for provider terminal evidence
  -> settle canonical Turn A
  -> preserve Sessions B..N and the connection
```

The critical path scheduler must admit approval responses and
`turn/interrupt` ahead of background catalog reads.

### 7.7 Wedged cancellation and process restart

The existing product contract bounds cancellation by force-closing a provider
process after the interrupt grace period. With a shared connection, the
fallback is deliberately connection-wide:

```text
interrupt A times out
  -> mark connection generation unhealthy
  -> close process once
  -> terminalize A as canceled according to accepted cancel intent
  -> report explicit connection-lost terminal outcomes for other active Turns
  -> fail pending interactions and requests
  -> remove dead generation from Registry
  -> do not replay any prompt
```

The next explicit user operation acquires a new generation and resumes its
exact provider Thread. A successful unsubscribe is never accepted as evidence
that the provider Turn stopped.

### 7.8 Unexpected process death

On EOF, protocol failure, or process exit:

1. increment or retire the connection generation;
2. reject all pending request callbacks with one typed connection error;
3. close every Binding reducer after ordered terminal fan-out;
4. preserve durable provider Thread identities;
5. reject late events carrying the retired generation;
6. reconnect only on a new explicit operation;
7. resume the exact Thread without redispatching prior user input.

An eager replacement used for process-global catalog refresh must not silently
resume Agent Sessions.

## 8. Message Routing

The permanent read loop classifies every incoming frame:

```text
message
  |
  +-- response/error with id
  |     `-- pendingRequests[id]
  |
  +-- server request or notification with threadId
  |     `-- ownerByThread[threadId] -> Binding ordered mailbox
  |
  +-- process-global notification
  |     `-- AppServerGlobalState / registered global observers
  |
  `-- unknown Thread
        `-- bounded temporary buffer or explicit drop telemetry
```

### 8.1 Response correlation

Requests are independent. Remove the global `callMu` from the app-server path.
Each request owns a response channel/callback indexed by request ID. Canceling
one caller removes only its pending entry.

The generic ACP path may keep its current serialization until separately
justified. The first refactor should avoid changing unrelated ACP provider
semantics merely to share app-server connections.

### 8.2 Thread ordering

Messages for different Threads may be processed concurrently. Messages for one
Thread enter one ordered reducer lane. This preserves existing state-machine
ordering without globally blocking unrelated Sessions.

### 8.3 Server-initiated requests

Approvals, dynamic tool calls, user-input requests, and other server-initiated
requests must retain:

- JSON-RPC request ID;
- provider Thread ID;
- provider Turn ID where supplied;
- connection generation;
- canonical Agent Session owner after routing.

The response must be rejected if the Binding or generation no longer matches.

### 8.4 Child Threads

When a provider emits a child-Thread creation event, the router must bind the
child Thread ID to the owning root Binding before forwarding later child events.
Child ownership cannot be recovered from the active window or current handler.

The existing provider-native child Session behavior remains subordinate to Host
contracts. This change only makes transport routing explicit.

### 8.5 Unknown Thread handling

Unknown Thread messages are expected only during bounded start/resume/child-bind
races. The buffer must have count and byte limits plus a short injected-clock
deadline. After the deadline, messages are dropped with sanitized telemetry.
They are never assigned heuristically.

## 9. Request Scheduling And Backpressure

The first release needs three priority classes:

| Priority    | Examples                                                         |
| ----------- | ---------------------------------------------------------------- |
| Critical    | approval/tool/input responses, `turn/interrupt`                  |
| Interactive | `thread/start`, `thread/resume`, `turn/start`, `turn/steer`      |
| Background  | `model/list`, `thread/list`, rate-limit, config and status reads |

The scheduler must:

- bound queued and in-flight requests;
- preserve FIFO order within one priority and Binding mutation lane;
- reserve capacity for critical responses;
- coalesce identical safe read-only requests such as `model/list`;
- return typed overload/backpressure errors;
- never coalesce mutations;
- expose queue depth, wait duration, in-flight count, and rejection telemetry.

This mirrors the client shape observed in the ChatGPT production artifact while
keeping Tutti's implementation independent.

## 10. Global State And Caches

The following are connection/process scoped:

- initialized server information;
- account identity and authentication status;
- rate limits;
- model catalog;
- process-global config and feature catalog;
- stable Skills/plugin/MCP roots;
- process diagnostics and version.

Bindings may snapshot values for ordered Session reporting, but they do not own
separate probes or processes. Concurrent identical `model/list` requests should
share one in-flight result and one cache for the connection profile.

Global updates fan out only through registered observers. They must not be
reported as Thread events unless the existing adapter contract requires a
Session projection.

## 11. Runtime Preparation Contract

### 11.1 Shared provider home

The target storage shape is conceptually:

```text
<agent-state>/provider-profiles/<profile-digest>/
  +-- codex-home/          # process lifetime
  +-- process-manifest
  `-- threads/
        `-- <session-id>/  # only Thread-lifetime prepared artifacts
```

The host derives paths through platform APIs and injected state roots. The
layout above is logical, not permission to hardcode `/`, home directories, or
drive letters.

Cleanup has two leases:

- `ProcessPreparationLease`: released when the Registry closes the connection;
- `ThreadPreparationLease`: released when one Binding detaches.

Session close must not delete a shared provider home.

### 11.2 Instructions

Session-specific managed policy currently materialized through a Session home
must move to app-server Thread or Turn instruction fields where supported.
Process-global instructions may remain in the shared profile only when they are
identical for every Thread using the key.

If the provider cannot accept required Thread-specific instructions, their
digest becomes part of `ProcessProfileDigest`. Tutti must split connections
rather than silently omit or cross-contaminate policy.

### 11.3 Authentication

One process profile has one account/auth authority. Account changes retire that
profile generation and create a new one. Invocation-scoped connector proofs or
room credentials are never written into shared auth files or process env.

### 11.4 tsh adaptation and first sharing boundary

tsh must update its managed-agent preparation RPC and runtime store once:

```text
before:
  prepare(runID = AgentSessionID) -> one complete provider home

after:
  prepareProcessProfile(profileKey) -> shared process lease
  prepareThreadOverlay(AgentSessionID) -> Thread lease
```

Exact RPC names may differ. The required behavior is separate idempotent
ownership and cleanup. After this cutover, future connection-sharing behavior
comes from the released Tutti Agent package cohort instead of being reimplemented
inside tsh.

tsh's current `agentsession.Transport` attaches provider-process ownership to
the Room attachment fence: detaching the first Room can terminate every process
registered to that attachment. Therefore the first tsh Process Profile must
include the attachment/runtime-generation identity and may include `RoomID`.
It may share one app-server only among Agent Sessions inside the same
Room/attachment runtime. Room, Agent Session credentials, cwd, canonical rail
placement, connector proof, MCP headers, and instructions still stay out of the
shared process environment.

Cross-Room sharing is not part of the first tsh adaptation. It requires a new
runtime-generation-level transport registry whose lifetime is independent of
Room detach. The runtime store alone must not claim that broader lifetime.

## 12. Session Replay

The first implementation includes capture mode in `AppServerConnectionKey`:

```text
live              -> share by normal process profile
record or replay  -> include root AgentSessionID and remain isolated
```

This preserves current cassette transport and semantic-state assumptions. It is
an evidence-based capture namespace, not a `SessionIsolated` process fallback.
Live and replay launches alike require an explicit `AppServerProcessProfile`;
missing preparation fails before process start.

A later cassette revision may record one shared connection with:

- request ID remapping;
- Thread/Binding ownership metadata;
- deterministic interleaving;
- connection-generation events;
- connection-wide failure fan-out.

That work is outside the first cutover.

## 13. Agent Host Boundary

This design changes adapter transport and provider resource ownership. It does
not redefine when canonical Sessions, Turns, Goals, or runtime operations are
created, sent, terminal, or recovered.

Therefore:

- shared Registry, connection, router, scheduler, and Binding types belong in
  `packages/agent/daemon`;
- process/Thread preparation materialization belongs in
  `packages/agent/runtimeprep`;
- `services/tuttid` and tsh remain composition and transport adapters;
- existing Host initialization and provider-acceptance barriers must be used;
- new Host semantics are not needed merely to pool a provider process;
- if implementation discovers a lifecycle capability required equally by tsh
  and tuttid, add it to `packages/agent/host` with a conformance scenario first.

## 14. Implementation Slices

### Slice 1: concurrent app-server transport — implemented in Tutti

Primary files:

```text
packages/agent/daemon/runtime/acp_client.go
packages/agent/daemon/runtime/codex_appserver_client.go
packages/agent/daemon/runtime/codex_appserver_events.go
```

Deliver:

- one permanent app-server message router;
- concurrent request-ID response correlation;
- no per-call active handler on the app-server path;
- bounded unknown-Thread handling;
- scheduler priorities and read-only coalescing;
- connection generation on every delivered message.

The production shape may extract an app-server-specific JSON-RPC client instead
of complicating the generic ACP client.

### Slice 2: separate Connection and Binding ownership — implemented in Tutti

Expected new owners:

```text
packages/agent/daemon/runtime/codex_appserver_connection_registry.go
packages/agent/daemon/runtime/codex_appserver_connection.go
packages/agent/daemon/runtime/codex_appserver_thread_binding.go
packages/agent/daemon/runtime/codex_appserver_router.go
packages/agent/daemon/runtime/codex_appserver_request_scheduler.go
```

Initially include Agent Session ID in the connection key. This keeps one process
per Session while the internal ownership split lands. It is a temporary rollout
step inside one implementation, not a second compatibility implementation.

Move process/client/global fields out of `codexAppServerSession`. Move Thread,
Turn, Goal, interaction, and child ownership into `ThreadBinding`.

### Slice 3: split runtime preparation — implemented in Tutti; tsh pending

Primary owners:

```text
packages/agent/daemon/runtime/provider_launch_prepare.go
packages/agent/runtimeprep
docs/architecture/agent-runtime-preparation.md
```

Deliver:

- stable process profile identity;
- process and Thread preparation leases;
- shared provider-home roots;
- Thread-level instruction/config projection;
- no Session credential in shared process env;
- one-time tsh preparation RPC/store adaptation.

### Slice 4: enable live sharing — implemented in Tutti, qualification active

Remove Agent Session ID from ordinary live connection keys. Deliver:

- single-flight process launch;
- multiple Binding registration;
- unsubscribe-on-detach without process close;
- connection-level model/account/rate-limit reuse;
- graceful per-Thread cancellation;
- explicit connection-wide forced-cancel and crash fan-out;
- generation-fenced recovery;
- removal of Session-keyed retired-process ownership.

Do not retain a hidden flag that silently falls back to one process per Session
after the shared implementation is qualified.

### Slice 5: replay and durable documentation — in progress

- keep first-generation record/replay profiles isolated;
- qualify fresh live Session Replay cases for Codex and Tutti Agent;
- update current architecture docs after the implementation lands;
- remove this Spec after its durable decisions move to architecture,
  runtime-preparation, testing, and replay documentation.

## 15. Test Design

### 15.1 Protected contract

When two Agent Sessions use the same compatible live process profile, the
adapter must spawn exactly one app-server process, create or resume two distinct
provider Threads, route every response/interaction/event to the correct
Session, and allow either Binding to detach or cancel without disturbing the
other during graceful operation.

When that shared process dies or must be force-closed, every affected active
Binding must receive an explicit ordered terminal outcome, no prompt may be
replayed, and late messages from the retired generation must have no effect.

### 15.2 Credible faulty implementations

The evidence must fail when:

- `AppServerConnectionKey` accidentally includes Agent Session ID after the
  sharing cutover;
- the old active handler receives another Thread's notification;
- a notification arriving before `thread/start` response is dropped or routed
  to the wrong Session;
- Session A close calls `client.Close` and kills Session B;
- Session A graceful cancel changes Session B's Turn;
- force-closing a wedged process leaves Session B running canonically;
- an old generation's late terminal settles a new Turn;
- a shared process env contains Session-specific credentials;
- incompatible process profiles are merged;
- concurrent catalog calls spawn another process or issue duplicate provider
  reads despite an existing in-flight request.

### 15.3 Owning seam and proof level

Use a narrow subprocess integration test in
`packages/agent/daemon/runtime`. The fixture must be a real scripted app-server
process speaking newline-delimited JSON-RPC. It should implement only:

```text
initialize / initialized
thread/start / thread/resume / thread/unsubscribe
turn/start / turn/steer / turn/interrupt
model/list
one server-initiated approval or dynamic-tool request
interleaved notifications
controlled EOF and wedged interrupt modes
```

Mocks of `codexAppServerClient` alone cannot prove process count, read-loop
interleaving, wire correlation, EOF fan-out, or cleanup ownership.

Agent Host conformance scenarios should be rerun unchanged. Do not duplicate
Host lifecycle assertions in adapter tests unless the adapter transport is the
observable risk.

### 15.4 Required scenarios

1. Two concurrent starts use a single-flight launch and return distinct Thread
   IDs.
2. Interleaved deltas, terminals, approvals, and tool calls route to the exact
   Session and preserve per-Thread order.
3. A notification emitted before the start response is buffered and drained to
   the correct Binding.
4. Detaching A emits `thread/unsubscribe`; B's active Turn continues and the
   process remains alive.
5. Graceful interrupt of A settles A and has no effect on B.
6. A wedged interrupt retires the connection and gives every affected active
   Turn its specified terminal outcome.
7. A late message from generation N cannot mutate generation N+1.
8. The next user action resumes the exact provider Thread and does not replay
   prior input.
9. Different executable, host, account, profile digest, or replay mode produces
   separate processes.
10. Identical concurrent `model/list` requests coalesce; mutations never do.
11. Queue saturation preserves capacity for approvals and interrupt.
12. Process cleanup occurs exactly once; Thread cleanup never removes the
    shared provider home.

Concurrency must use channels, barriers, controlled frames, and injected clocks.
Wall-clock sleeps are not readiness or non-effect evidence.

### 15.5 Negative-control evidence

Before each implementation slice is accepted, run the focused scenario against
the smallest temporary faulty mutation and confirm failure for the intended
reason. Useful negative controls include:

- restore the active-handler dispatch;
- add Agent Session ID to a live connection key;
- call connection close from Binding detach;
- omit the generation comparison;
- route an unknown Thread to the latest Binding.

Do not commit the mutations.

### 15.6 Execution lanes

- The focused Go adapter suite runs through `pnpm test:go:agent-daemon`.
- `pnpm check:changed` is the final repository-selected gate for each slice.
- Agent Host conformance remains selected when Host contracts are touched.
- Native Windows CI must execute process creation, environment-key comparison,
  path ownership, EOF/termination, and cleanup scenarios.
- POSIX CI must execute the same shared protocol scenarios.
- Fresh real-provider qualification belongs in Session Replay or an explicit
  live lane, not in unit tests.

## 16. Windows Impact

This design is platform-sensitive because it changes process lifetime, paths,
environment overlays, executable identity, termination, and cleanup.

Required rules:

- derive provider profile paths from injected native roots;
- use platform path APIs, not slash concatenation;
- compare environment keys with target-platform semantics;
- keep executable resolution and `.exe`/`.cmd` handling in the existing native
  process adapter;
- pass argv to process APIs instead of constructing shell strings;
- define forced connection retirement in terms of the existing process
  capability, not POSIX signals;
- never depend on Unix sockets for the shared design; stdio remains valid on
  every target;
- test paths containing spaces and non-ASCII characters;
- exercise the receiving process/filesystem boundary on native Windows.

No new OS checks belong in Agent Host or Thread lifecycle business logic.

## 17. Observability

Add structured fields without exposing prompts, secrets, or raw provider data:

```text
provider
execution_host_id
connection_key_hash
connection_generation
provider_thread_id
agent_session_id
request_method
request_priority
queue_wait_ms
queue_depth
in_flight_count
binding_count
connection_exit_reason
forced_restart_reason
unknown_thread_drop_count
```

Required operational questions:

- How many app-server processes exist per provider and execution host?
- How many Bindings use each connection?
- Did a second process start because the profile differed or because startup
  single-flight failed?
- Which connection-wide failure affected a Session?
- Were any messages dropped because their Thread owner was unknown?
- Did a critical response wait behind background work?

## 18. Acceptance Criteria

The live cutover is complete only when:

- two compatible Codex Sessions use one Codex app-server process;
- two compatible Tutti Agent Sessions use one Tutti Agent app-server process;
- Codex and Tutti Agent still use separate processes;
- every Thread event and server request has an exact Binding owner;
- no global active-handler slot remains on the shared app-server path;
- Session detach unsubscribes without closing the healthy connection;
- graceful cancellation is Thread-local;
- forced cancellation and crashes have explicit connection-wide outcomes;
- recovery resumes exact Thread IDs and never replays prompts;
- shared homes and Thread overlays have separate cleanup ownership;
- tsh passes the same contract after its one-time preparation adaptation;
- POSIX and native Windows process-boundary tests pass;
- live Session Replay qualification passes for Codex and Tutti Agent;
- current architecture and runtime-preparation documentation reflects the
  implemented result.

## 19. Resulting Package Upgrade Contract For tsh

Before the one-time tsh preparation cutover, upgrading Agent packages is not
sufficient because tsh still creates Session-keyed provider homes.

After the cutover:

1. Tutti releases a coherent Agent package cohort containing the Registry,
   connection router, Binding model, and split preparation contract.
2. tsh updates its desktopd preparer and managed VM runtime store to implement
   process and Thread leases.
3. tsh upgrades that cohort atomically.
4. Later adapter behavior improvements are inherited through ordinary coherent
   package upgrades, unless a future public preparation contract changes again.

The intended first tsh steady state is one Codex process profile and one Tutti
Agent process profile per compatible Room/attachment runtime generation when
both providers are used. A later runtime-generation transport registry may
broaden this to cross-Room sharing; the initial store/preparer adaptation does
not.
