# Connector Market Source

`packages/connector/market/source` is the generated Market protocol adapter for
Connector catalog snapshots and artifact download descriptors. It is the only
Connector module allowed to import `packages/clients/market-go`.

The adapter owns generated DTO parsing, pagination, manifest validation, target
implementation selection, and projection into stable Connector contracts. It
implements the narrow `application.CatalogSource` and
`application.ArtifactDownloadResolver` ports; the daemon only schedules those
ports and does not know the generated Market transport.

The current server protocol has no authoritative catalog snapshot revision.
For compatibility, `FetchSnapshot` performs two complete reads and accepts only
structurally equal validated snapshots. This fence does not invent a client-side
source revision, release digest, or artifact identity. Those identities remain
server-owned.
