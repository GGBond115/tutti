# @tutti-os/connector-market

`@tutti-os/connector-market` is the host-neutral connector-market boundary
shared by Tutti and other approved desktop daemon hosts such as TSH.

The package owns the TypeScript and renderer side of the shared boundary:

- `openapi/connector-market.v1.yaml`: the HTTP fragment composed by each host
  daemon's aggregate OpenAPI document
- `contracts`: host-neutral backend, event, domain, and error contracts
- `core`: the application module lifecycle and stable narrow composition ports
- `services`: a module-scoped Root, Runtime, lifecycle, StartupJobs, Valtio
  domain services, and host adapter contracts
- `renderer`: the canonical catalog, authorization dialog, connected-state
  management dialog, and compact composer entry built only from
  `@tutti-os/ui-system`
- `ui`: a one-release compile-time compatibility re-export of `renderer`
- `i18n`: the connector-market resource bundle and scoped runtime factory

The package does not construct an HTTP client, read Electron globals, choose a
catalog endpoint, persist credentials, select install directories, or own a
host database. Those responsibilities remain in the consuming daemon and
renderer adapters.

## Declarative authorization UI

Connector manifests may carry a versioned `authorizationInteraction` value.
The daemon transports this value without interpreting its presentation
semantics. `@tutti-os/connector-authorization-protocol` validates it at the
renderer boundary, and Connector Market's declarative adapter maps the selected
secret field to the existing authorization backend input. Runtime header,
endpoint, environment, and credential-storage bindings never enter the UI
protocol.

Connector Market renders the protocol with its Connector-owned UI System
renderer. A missing interaction on a legacy `api_key` Connector uses the
centralized one-secret compatibility adapter; an explicitly invalid
interaction fails closed. QR payloads are encoded by the application/view
projection and reach React only as a closed image data URL.

When a newly received Authorization View is an `external_link` or
`device_code`, Connector Market opens its activation or verification URL once.
The `device_code` View stays visible so the user can copy its code, and the
dialog action remains available if the browser must be opened again manually.

## Renderer usage

```ts
import {
  ConnectorMarketModule,
  IConnectorMarketModule
} from "@tutti-os/connector-market/services";

const connectorMarketModule = new ConnectorMarketModule({
  market: {
    backend: hostConnectorMarketBackend,
    autoUpdateInstalledConnectors: true,
    canRequest: () => hostAccountState.authenticated,
    events: hostConnectorMarketEvents,
    requestInstallAdmission: () => hostAccountLogin.open()
  },
  scope: {}
});

serviceRegistry.registerInstance(
  IConnectorMarketModule,
  connectorMarketModule,
  "owned"
);

const workspaceServices = new InstantiationService(
  serviceRegistry.makeCollection()
);
await connectorMarketModule.activate(workspaceServices);
```

The backend adapter wraps the host's generated daemon client and maps transport
DTOs into package domain types. Daemon events are invalidation hints; the
service re-reads the authoritative daemon snapshot before publishing new state.
Hosts whose market requires authentication must provide `canRequest`. A false
result keeps startup, reconnect, resume, and command paths transport-silent
while still allowing the module lifecycle to reach `ready`; after the host
observes an authenticated transition it calls
`connectorMarketModule.rendererPorts.market.reload()`.
When an install intent arrives while requests are not admitted, the Market
service invokes the optional `requestInstallAdmission` host hook and rechecks
admission. A host may use this hook to open its account login flow. The install
returns `not_admitted` without calling the backend when authentication remains
unavailable, so UI callers do not report a false installation success.
Hosts may enable `autoUpdateInstalledConnectors` to install a newly observed
compatible release in the background. Tutti enables this policy by default.
Automatic updates never invoke `requestInstallAdmission`, and one running
renderer attempts each release digest at most once; failed updates remain
available for explicit retry and may be retried after restart or after a newer
release appears.
Starting an event subscription and every observed `connected` state trigger an
authoritative reconciliation, including the first connection. Snapshot reads
are coalesced per service generation and a connection/event arriving during
an in-flight read schedules a serialized follow-up. Mutation responses are
revision-fenced so an older response cannot overwrite a newer daemon snapshot;
host generated-client adapters must preserve connector-market error code,
retryability, and structured details when rejecting a command.
Catalog placement is server-owned mutable metadata. The renderer first reads
the daemon's category list, then reads each section with an opaque cursor and
keeps independent Valtio loading and next-page state per section. `featured`
is an overlapping collection; primary category membership is never inferred
from the immutable connector release manifest. A browsed page is cached by the
daemon so a newly observed connector is immediately installable, while the
scheduled authoritative refresh still traverses every primary category for
runtime reconciliation.
The available segment requests `installation=not_installed`. Hosts must apply
that installation projection before page boundaries and next-page calculation,
draining upstream catalog pages as needed so an opaque cursor never advertises
only already-installed Connectors. Omitting the filter preserves the complete
catalog pagination contract for compatibility consumers.
Initial section reads settle independently. Successful sections remain usable
when another section fails, and the failed section exposes its own retry without
moving the whole catalog into the global error state. A global error is reserved
for category discovery failure or an initial load where every non-empty section
fails. Background failures retain the last known good connectors and cursor for
the affected section.
Activation creates a child service container and executes the complete startup
flow before the host renders the module:

```text
created -> starting -> synchronizing -> materializing -> ready
             |              |                 |
             |              |                 +-- ViewServiceStartupJob
             |              +-- MarketServiceStartupJob initial-load barrier
             +-- MarketServiceStartupJob + UiStateServiceStartupJob
```

The outer composition adapts the module's narrow application ports into one
window-scoped readonly `ConnectorRendererModel`. Compact Composer React receives
only that model; it cannot access lifecycle or mutable Root services, construct
services, or own model disposal. The Desktop Connector composition creates that
model once per window module and shares it across Workspace and Standalone Agent
surfaces.

### Composer entry

Hosts render `ConnectorComposerEntry` from
`@tutti-os/connector-market/renderer`. Connector owns the readonly model,
complete status projection, i18n, selection rules, and a closed semantic event
union. AgentGUI supplies only a neutral target/draft slot. Desktop chooses the
catalog or dialog container after receiving an event. Missing Shared Agent
policy fails closed; local Agents use the validated catalog. Only `connected`
items can be newly selected. `/ui` temporarily re-exports `/renderer` and is
not a runtime fallback.

Mount one `ConnectorDialogHost` per renderer window/application
container, not per composer entry or settings page. Multiple entries share the
same Root and therefore the same modal state machine.

## OpenAPI composition

Inside this repository, the aggregate document may use a repository path:

```yaml
x-tutti-openapi-fragments:
  - packages/connector/market/openapi/connector-market.v1.yaml
```

External hosts install an exact released package version and resolve the
exported fragment through package exports:

```yaml
x-tutti-openapi-fragments:
  - package: "@tutti-os/connector-market"
    path: "openapi/connector-market.v1.yaml"
```

Do not copy the fragment into another repository or reference a Tutti worktree.

## Go boundaries

Go is published through responsibility-specific sibling modules rather than
from this npm package directory:

- `github.com/tutti-os/tutti/packages/connector/host`
- `github.com/tutti-os/tutti/packages/connector/daemon`
- `github.com/tutti-os/tutti/packages/connector/store-sqlite`
- `github.com/tutti-os/tutti/packages/connector/runtime`

All connector npm and Go modules ship in the same exact package cohort. See
the sibling module READMEs and `docs/architecture/connector-market.md` for the
ownership and host-adapter boundaries.
