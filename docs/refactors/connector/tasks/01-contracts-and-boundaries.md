# T01: contracts and dependency boundaries

Status: complete and verified.

## Objective and outcome

This task established the vocabulary and dependency rules required by every
other lane. The pre-refactor `packages/connector/host` package is historical
baseline only. Its application responsibilities now live in
`packages/connector/application`, and the old package has been deleted.

## Delivered contracts

`packages/connector/contracts` owns the stable host-neutral values for:

- Connector identity, scope, runtime binding, transport, generation and
  revisions;
- catalog freshness, category/listing/release descriptors, installation,
  authorization, runtime convergence and readiness;
- operations, domain errors and closed command results;
- local/shared Agent support and grant policy;
- canonical Connector presentation and actions.

Unknown transport, scope, state, readiness, presentation, or policy values fail
closed. They cannot become a selectable or setup-ready default.

### Structured mutation boundary

Every public mutation result is exactly one of:

```text
accepted   durable asynchronous operation accepted
completed  effect already or synchronously completed
rejected   known precondition/domain/revision failure
uncertain  dispatch may have crossed the durable boundary; authoritative read required
```

The result validator rejects invalid combinations of outcome, operation,
failure, and revision. Revision conflict is non-retryable. TypeScript does not
automatically replay a mutation after uncertainty or conflict.

### Canonical presentation boundary

The application emits one closed `ConnectorPresentation`:

```text
unavailable
loading
setup_required
authorization_required
connecting
connected
degraded
disabled
unsupported
failed
```

It also emits a validated set of semantic actions:

```text
details, install, update, authorize, cancel, select, remove_selection,
disconnect, uninstall, restart_runtime
```

Application state transition and action admission are authoritative. Market,
renderer, Agent policy, Desktop, and AgentGUI cannot repeat the derivation.

### Agent policy contract

- Local Agent support is the accepted validated catalog.
- Shared Agent support is an explicit allowlist; empty means no support.
- Support and viewer/execution grant are independent inputs.
- Missing, loading, stale, unavailable, unknown, or undeclared shared facts fail
  closed per Connector.
- The application combines policy with authorization and exact runtime
  readiness into the final presentation.

## Narrow application interfaces

`application.New` returns one `Composition` with two deliberately different
surfaces:

- public `Root`: `StateQueries`, `CatalogQueries`, `CatalogCommands`,
  `InstallationCommands`, `RuntimeCommands` (typed user recovery),
  `AuthorizationCommands`, `OperationQueries`, and
  `AgentConnectorPolicyQueries`;
- daemon-only `DaemonPorts`: separate recovery, operation, catalog,
  installation, authorization, and runtime maintenance interfaces.

Consumers inject one facet. HTTP, renderer adapters, and product code cannot
retain the concrete service, repository, daemon controls, or mutable root.

## Implemented boundary gates

Repository checks enforce:

- Connector packages cannot import Agent packages, AgentGUI, Desktop, TSH,
  preload/client product state, or another package's private deep paths;
- AgentGUI production code cannot import or mention Connector packages;
- application/services cannot import React or renderer modules;
- renderer cannot import daemon transports, generated host clients, Desktop
  stores, AgentGUI, or global window state;
- only `packages/connector/market/source` may import the generated Market Go
  client;
- Go source plus `go.mod` require/replace edges preserve parallel outer modules:
  daemon, runtime, store-sqlite, and Market source depend on
  application/contracts but not on each other in production;
- `runtime/process` remains a runtime-internal neutral leaf with no contracts or
  application import; daemon-to-store imports are test-fixture-only;
- generated wire DTOs terminate at adapters and cannot masquerade as domain
  values;
- workspace exports, build/declaration entries, `publishConfig`, and
  `typesVersions` remain in parity;
- `/renderer` is canonical and `/ui` forwards only.

Primary commands:

```bash
pnpm check:connector-boundaries
pnpm check:renderer-boundaries
pnpm check:api-generated
pnpm check:agent-gui-degradation
```

The final T06 run also uses negative `rg` controls for deleted packages,
forbidden imports, duplicate identity construction, client digest computation,
and product `/ui` imports.

## Verification evidence

The completed implementation passed contracts/application tests, affected Go
race/vet and Windows compilation, generated-client drift, source plus `go.mod`
subsystem boundaries, TypeScript canonical-decoder coverage, Renderer/AgentGUI
static gates, and the final TS test/typecheck/build matrix. Decoder coverage
includes legacy read-only behavior and unknown-presentation fail closed.

## Acceptance

- Stable contracts are owned once and validated exhaustively.
- Consumers receive narrow facets and cannot bypass lifecycle or repositories.
- Presentation, action, policy, identity, authorization, and availability are
  not independently re-derived outside Connector application.
- Forbidden edges fail in durable repository checks.
- Final implementation/static checks pass; post-rebase smoke is root closeout.
