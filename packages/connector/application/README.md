# Connector Application

`packages/connector/application` is the host-neutral Connector application
core. It depends only on `packages/connector/contracts` and owns catalog
acceptance, manifest validation, installation and authorization transitions,
durable operation recovery, runtime intent, compatibility evaluation, and
Agent Connector policy projection.

The package contains no HTTP client, generated transport DTO, SQLite driver,
Electron API, absolute state root, or operating-system process policy. Outer
modules such as `connector/daemon`, `connector/runtime`,
`connector/store-sqlite`, `connector/market/source`, and product composition
adapters consume or implement its narrow ports.

## Composition and facets

`New` returns one `Composition`. Its public `Root` exposes only
responsibility-specific interfaces:

- `StateQueries`
- `CatalogQueries` and `CatalogCommands`
- `InstallationCommands`
- `AuthorizationCommands`
- `OperationQueries`
- `AgentConnectorPolicyQueries`

Daemon workers receive a separate `DaemonPorts` group, split into recovery,
operation, catalog, installation, authorization, and runtime maintenance
interfaces. Consumers keep the facet they need; they do not retain the private
application implementation or gain worker controls through a public query or
command surface.

## State authority

Installation is device-scoped truth. Authorization is an account-scoped
projection. `RuntimeBindingResolver` is the only execution port that may derive
a connection identity and obtain a one-shot credential grant;
`RuntimeIntentResolver` is its side-effect-free planning counterpart and cannot
mint credentials. Grants are cleared after the physical command returns and are
never persisted as operation state.

The application combines catalog membership, installation, authorization,
compatibility, exact runtime convergence, local/shared Agent support, and
explicit grants into `AgentConnectorPolicySnapshot`. Adapters map that
projection to their own DTOs; they must not independently derive connection
identity, authorization, availability, or Agent admission.

## Command and observation boundaries

Physical runtime mutation is exposed through the command-only
`ImplementationCommands` port. Physical route truth is exposed separately
through `RouteObservation`, whose level-triggered `Snapshot` is authoritative
and whose `Watch` stream only reduces repair latency. The application core does
not treat a command return value, a registry lookup, or an edge event as
physical observation.

`ReleaseInstallationManager` similarly owns the complete physical
install/inspect/commit/uninstall boundary. Installation does not imply runtime
publication: Candidate and Runtime Desired are committed first, and Current is
promoted only after an exact-generation Runtime Observed receipt for the current
boot.

Connector mutations use a per-Connector revision fence, with the global
snapshot revision retained as a compatibility input for older clients. Durable
install, update, uninstall, authorization, and reconcile flows use short
transactions around idempotent effects. Updates retain Current and Candidate
evidence simultaneously; disconnect and uninstall wait for an exact disabled
observation before completing.
