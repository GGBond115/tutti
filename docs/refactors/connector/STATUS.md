# Connector refactor execution status

Updated: 2026-08-18

Implementation, deletion audit, and the Go/TypeScript/static verification matrix
are complete. Tutti commit hashes are intentionally omitted because root will
normalize DCO sign-off once. Post-rebase smoke and final clean-tree confirmation
remain root-owned repository closeout.

| Task                           | State                 | Verified outcome                                                                                                           |
| ------------------------------ | --------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| T01 contracts/boundaries       | complete and verified | closed contracts, narrow application facets, source/`go.mod`/import/export gates                                           |
| T02 runtime/process            | complete and verified | Connector-owned process layer, sole binding identity, independent commands/observation, three-way convergence              |
| T03 application/daemon/catalog | complete and verified | local last-good catalog, structured commands, canonical presentation, scoped lifecycle, health, one-time release migration |
| T04 server artifact delivery   | complete and verified | server-owned digest/descriptor/resolve, generated source lock, verified digest-based fetch                                 |
| T05 renderer/AgentGUI/hosts    | complete and verified | canonical presentation/actions, explicit Desktop mapper, one-model shared policy, neutral AgentGUI slot/draft split        |
| T06 cutover/verification       | complete and verified | legacy paths removed; final static, TS, and affected Go slice matrix passed                                                |

## Verified implementation

### Contracts and application

- `packages/connector/contracts` owns closed state, command, presentation,
  policy, runtime binding, revision, operation, and error values.
- `application.New` returns one composition: public `Root` exposes narrow
  query/command facets and daemon-only maintenance is split into
  responsibility-specific ports.
- Public mutations normalize to exactly `accepted`, `completed`, `rejected`, or
  `uncertain`. Revision conflict is non-retryable; dispatch uncertainty requires
  authoritative GET and cannot trigger automatic mutation replay.
- The application is the sole projector of ten presentation states and ten
  semantic actions: `details`, `install`, `update`, `authorize`, `cancel`,
  `select`, `remove_selection`, `manage`, `disconnect`, and `uninstall`. Generic
  retry was removed
  because no corresponding command exists.
- Local Agent policy uses the accepted catalog. Shared support and grant are
  explicit per Connector; missing, stale, unknown, or undeclared facts fail
  closed.
- Authorization cancellation persists a client-request fence. An exact request
  resumes unresolved/canceling/terminal receipts across interruption and
  completes projection reset; a different request cannot adopt the session.
  Recovery remains possible when provider cancellation succeeded but the first
  projection transaction failed.

### Runtime, daemon, and health

- `packages/connector/runtime/process` owns process specs, verification, NDJSON,
  bounded I/O, process groups, and tree shutdown. It is a runtime-internal
  neutral leaf with no contracts/application import, and Connector has no Agent
  process dependency.
- `RuntimeBindingResolver` is the only connection-identity and one-shot-grant
  authority. Planning cannot mint credentials.
- `ImplementationCommands` and `RouteObservation` remain separate. The daemon
  compares durable Desired, durable Observed, and physical Snapshot/Watch facts.
- Watch loss/revision gaps force snapshots; jittered anti-entropy repairs silent
  drift. Exact healthy routes do not restart.
- Retry health is per Connector/generation. Three failures degrade, six fail
  and exhaust automatic starts; only new generation or exact healthy
  observation resets the budget.
- Construction is inert. `Start(ctx, initialScope)` owns the initial scope and
  all worker registration/bootstrap. `Close(ctx)` closes admission/publication,
  cancels workers immediately, and completes bounded shutdown phases without an
  older delayed enable winning.
- Worker health reports last success, consecutive failures, backoff, optional
  budget/exhaustion, and stable failure code. Runtime retry health is not copied
  from an aggregate scanner value.

### Catalog, artifact, and persistence

- Catalog queries read the local accepted snapshot. Refresh validates a complete
  generated-protocol read before atomic acceptance and preserves last-good data
  on failure.
- Stale data remains visible/read-only. Install, update, and new authorization
  admission are closed while application-approved cleanup and exact installed
  runtime behavior remain.
- `packages/connector/market/source` is the sole generated Market-client owner.
  It validates manifest/target/descriptor data and never computes release
  identity. Without a server catalog revision it accepts only two structurally
  equal complete reads.
- Installation resolves the server-owned release digest immediately before
  download, validates HTTPS/expiry/descriptor identity, and verifies media type,
  byte size, and SHA-256 before extraction.
- Canonical installed-release history is separate from operation retention. A
  transactional one-time migration commits canonical rows and a marker; normal
  paths never consult the legacy evidence afterward.

### Renderer, Desktop, and AgentGUI

- `/renderer` is canonical and `/ui` is one-release forwarding only. Renderer
  owns every Connector component, dialog, status, action, selection rule, and
  string.
- Market services, cards, composer, dialogs, view builder, and automatic update
  consume canonical freshness/presentation/allowed actions. Raw installation,
  authorization, operation, compatibility, and runtime fields are details only,
  not an alternative decision engine.
- Unknown/malformed presentation fails closed as unsupported. Stale exact-ready
  Connectors remain usable without install/update/new-authorization actions.
- Desktop owns an explicit generated-client transport mapper that validates
  canonical DTOs/results before they enter Connector services.
- Local and shared policy feed one cached renderer model. Shared policy is bound
  once before subscription; missing policy is unavailable rather than a second
  model or renderer-side allowlist computation.
- AgentGUI contains only the neutral optional `primaryCapability` slot and
  generic target/draft data. Generic primary-capability extraction/update and
  opaque identity are split into a neutral helper; AgentGUI has no Connector
  import, type, copy, state, component, or navigation branch.
- Workspace and Standalone share bridge semantics and one model/dialog host per
  window.

## Verification evidence

The completed implementation passed:

- Connector application normal tests, including interruption-safe authorization
  cancellation and request-fence recovery;
- affected Connector Go normal/race/vet suites and recorded Windows compilation
  gates for contracts/application/runtime/daemon/store/source slices;
- Connector Market tests, typecheck, and build;
- AgentGUI focused tests and typecheck;
- Desktop focused tests, typecheck, and build;
- canonical decoder and generated daemon client tests;
- i18n, Connector boundary, renderer boundary, AgentGUI degradation, generated
  API/source-lock, module DAG, export parity, and negative static gates;
- explicit `go.mod` require/replace classification: daemon, runtime,
  store-sqlite, and Market source are parallel outer modules over
  application/contracts; daemon-to-store is test-fixture-only.

The clean server branch `codex/connector-artifact-resolve` independently passed
full `go test ./...`, focused tests/vet, byte-for-byte protobuf regeneration,
and API consumer checking (180 operations across three repositories). Its signed
commit chain is:

1. `6f9f244ee34314c57b03319c64f78374681d6492`
2. `ae8f51464b111ec3c1d6bc091156ab579d0045d2`
3. `b1bb4de6f71d1068a81e2f3098fdc93a43ca4add`

Tutti's Market source lock pins the final server commit and generated-file
hashes.

## Root-only repository closeout

No implementation slice remains. Root will:

1. commit these final execution documents;
2. normalize DCO sign-off while preserving logical commit boundaries;
3. rerun post-rebase smoke-sensitive checks;
4. verify every branch commit sign-off and final clean worktree;
5. publish the final commit/evidence handoff.

Do not report the DCO rebase or Tutti clean tree as complete until root performs
those steps.

## Compatibility and residual risks

- `/ui` is permitted for one release as compile-time forwarding only.
- Deprecated server `artifact.key` remains for one measured release cycle; new
  Tutti code cannot consume it.
- One older daemon wire can omit presentation. The central client decoder makes
  it read-only/unsupported rather than deriving connected/selectable state.
- Server reverse digest lookup is O(N) until a durable index is added.
- Production TSH migration is deferred; it must consume the released Tutti
  cohort and explicit shared-Agent support without adding a fallback.
- Native process-tree coverage can be noisy on constrained CI, and the existing
  onboarding ZIP fixture is an unrelated known flake; neither waives
  deterministic Connector-focused failures.
