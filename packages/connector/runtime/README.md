# Connector Runtime

`packages/connector/runtime` is the reusable same-machine Connector runtime
foundation. Tutti runs it on the desktop host; VM-backed products run it inside
the managed guest. It owns secure artifact preparation, managed runtime
identity, runtime ABI verification, typed Node package installation, the MCP
stdio client, the host-neutral ImplementationHost/CommandRegistry, Connector
Broker discovery/invocation, and verified Connector Skill reading.

Hosts supply the managed runtime resolver, implementation host, process
transport, HTTP client/proxy policy, state roots, and product-facing command
transport. Runtime code must not import `services/tuttid` or expose host
filesystem paths as a cross-machine protocol.

Authorized `managed_stdio` Connectors use the public `CredentialBroker` port.
The host passes only an opaque grant to that port and gives the Connector the
resulting `tutti.connector.credentials.v1` payload through the reserved
`TUTTI_CONNECTOR_FD_CREDENTIALS` inherited descriptor. Grants and credential
payloads must remain memory-only and must not be logged.
`AuthorizationObserver` reports successful runtime credential binding and
expired grants back to the embedding product. An expired grant immediately
retires the published route; the embedding product remains responsible for
projecting that observation to its account authorization authority.

Products whose VM boundary is the Connector authority boundary may explicitly
select `agentruntime.NewPermissiveConnectorProcessTransport()`. It retains
verified-executable, immutable-tree, bounded-output, process-group, and
sensitive-FD behavior while intentionally omitting the OS sandbox. The default
`NewConnectorProcessTransport()` remains fail-closed when no platform sandbox
backend exists.
