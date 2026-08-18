# @tutti-os/connector-market

`@tutti-os/connector-market` is the host-neutral TypeScript and renderer
boundary for the Connector domain shared by Tutti and other approved desktop
daemon hosts.

The package exposes:

- `openapi/connector-market.v1.yaml`: the local daemon HTTP fragment composed
  by each host
- `contracts`: host-neutral backend, event, and renderer-domain contracts
- `core` and `services`: lifecycle integration and narrow application ports
  used to create one window-scoped Connector module
- `composition`: adaptation from those ports to the readonly renderer model
- `renderer`: Connector-owned catalog, dialogs, management UI, and compact
  Agent composer entry built with `@tutti-os/ui-system`
- `ui`: a one-release compile-time compatibility re-export of `renderer`
- `i18n`: the Connector resource bundle and scoped runtime factory

The package does not construct a daemon HTTP client, call the remote Market,
read Electron globals, choose an endpoint, persist credentials, select install
directories, or own a host database. Hosts adapt their generated local-daemon
client and event transport to the exported backend contracts.

## Integration boundary

A renderer host creates one `ConnectorMarketModule` for a window/application
container, activates it through the host DI lifecycle, and adapts
`module.rendererPorts` with `getConnectorRendererModel`. React surfaces receive
only that stable readonly `ConnectorRendererModel`; they do not receive the
module's internal services or lifecycle controls.

Daemon events are invalidation hints. The module re-reads the authoritative
local-daemon snapshot on startup, reconnect, resume, and relevant events, and
revision-fences asynchronous responses. A host whose Market requires account
authentication supplies `canRequest`; when it returns false, startup can become
ready without issuing transport requests. Install admission remains a host
hook, so the package can request login without owning account UI.

The local daemon remains the authority for installation, authorization,
compatibility, revisions, operation state, and runtime readiness. Renderer
normalization fails closed for unknown values. Renderer-local busy state may
make a command immediately visible, but it never replaces daemon truth.

## Renderer ownership

Hosts render `ConnectorComposerEntry`, `ConnectorMarketPanel`, and one
`ConnectorDialogHost` from `@tutti-os/connector-market/renderer`.
Connector owns its UI System mapping, i18n, selection behavior, and closed
semantic event union. The host decides which product container or navigation
surface handles an event; it does not infer Connector command success from
navigation.

AgentGUI contributes only a neutral `primaryCapability` slot with target and
draft identity. It does not import Connector types, labels, status logic,
dialogs, or navigation. Missing Shared Agent support fails closed; local Agent
support and shared Agent support/grants are supplied through the Connector
policy boundary.

Mount one dialog host per renderer window/application container, not per
composer entry or settings surface. `/renderer` is canonical. `/ui` is a
temporary compile-time forwarding entry and contains no second implementation
or runtime fallback.

## Declarative authorization UI

Connector manifests may carry a versioned `authorizationInteraction` value.
The daemon transports it without interpreting presentation semantics.
`@tutti-os/connector-authorization-protocol` validates it at the renderer
boundary, and the Connector renderer maps the selected secret field to the
authorization backend input. Runtime headers, endpoints, environments, and
credential-storage bindings never enter the UI protocol.

A missing interaction on a legacy `api_key` Connector uses the centralized
one-secret compatibility adapter; an explicitly invalid interaction fails
closed. QR payloads reach React only as a closed image data URL. External-link
and device-code interactions may open their URL once automatically while
keeping an explicit user action available.

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

## Go module cohort and Market protocol termination

The Go side is published as responsibility-specific sibling modules:

- `github.com/tutti-os/tutti/packages/connector/contracts`
- `github.com/tutti-os/tutti/packages/connector/application`
- `github.com/tutti-os/tutti/packages/connector/daemon`
- `github.com/tutti-os/tutti/packages/connector/store-sqlite`
- `github.com/tutti-os/tutti/packages/connector/runtime`
- `github.com/tutti-os/tutti/packages/connector/market/source`

`packages/connector/market/source` is the only Connector module allowed to
import the generated `packages/clients/market-go` client. It owns remote DTO
parsing, pagination, manifest validation, exact execution-target selection, and
projection into stable contracts. The daemon sees only
`application.CatalogSource` and `application.ArtifactDownloadResolver`.

The source preserves server-owned release digests and immutable artifact
descriptors. It exchanges a release digest for a short-lived authenticated
download descriptor immediately before installation; signed URLs are not
catalog identity and are never persisted. Because the current server protocol
does not expose an authoritative catalog snapshot revision, the adapter accepts
only two structurally equal complete validated reads and does not synthesize a
client revision, release digest, or artifact identity.

All Connector npm and Go modules ship as one exact package cohort. See
`docs/architecture/connector-market.md` for the full ownership and data-flow
contract.
