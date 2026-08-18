# T04: tsh-server artifact delivery protocol

Status: complete and verified.

Scope: the artifact protocol changes in `tsh-server` and the corresponding
generated contract/fetch path in Tutti. Other TSH product migration is deferred.

## Objective and outcome

The old protocol exposed a storage object key and required each client to know
how to construct a CDN URL and recompute release identity. The delivered
protocol makes the server authoritative for release identity and location
resolution:

- public Market reads expose an immutable artifact descriptor;
- authenticated resolution accepts only `releaseDigest`;
- the server returns a short-lived HTTPS URL plus the bound descriptor;
- Tutti resolves immediately before fetch and independently verifies media
  type, byte size, and SHA-256 before extraction;
- signed URLs and object locations are never Catalog identity or durable state.

## Public contract

The generated descriptor contains:

```text
releaseDigest
sha256
sizeBytes
mediaType
artifact.key (deprecated response compatibility only)
```

The authenticated resolve operation accepts only `releaseDigest` and returns:

```text
url
expiresAtMs
releaseDigest
sha256
sizeBytes
mediaType
```

Tutti cannot choose an object key or rebuild a URL from the deprecated field.

## Server invariants

- Authenticated account identity is required.
- Only an active, currently listed deployment-market release can resolve.
- Release identity includes immutable release data and canonical archive media
  type; storage object location is excluded, so relocation does not change
  identity.
- The resolve reply is bound to digest, media type, SHA-256, size, and the
  server-selected object key before presigning.
- URL must be HTTPS, bounded in length, and expire within the configured
  five-minute window (with the tested clock tolerance).
- Existing storage presign abstraction owns URL generation.
- Rate limiting is 60 resolutions/minute and fails closed if its backing state
  is unavailable.
- Durable audit evidence is appended before the URL is returned. Signed URL and
  query values are absent from audit records and logs.

The server currently resolves the digest through an O(N) reverse scan. This is
correct but remains a known scale risk for a future indexed lookup.

## Tutti behavior

`packages/connector/market/source` projects only the immutable descriptor.
Installation hands the release digest to the generated authenticated resolver
immediately before download. `DirectFetcher`:

- rejects non-HTTPS, expired, overlong-expiry, overlong URL, redirect-origin,
  digest, media type, size, or SHA mismatch;
- keeps the URL in memory only for the fetch duration;
- streams under explicit bounds and verifies byte count plus SHA-256 before
  extraction/import;
- returns structured retryable failure on expiry/transport uncertainty without
  falling back to the legacy object key.

## Commit chain and generated provenance

The clean server branch `codex/connector-artifact-resolve` contains:

1. `6f9f244ee34314c57b03319c64f78374681d6492` — digest-only authenticated
   resolution, rate limit, audit, and generated API;
2. `ae8f51464b111ec3c1d6bc091156ab579d0045d2` — descriptor/reply media type;
3. `b1bb4de6f71d1068a81e2f3098fdc93a43ca4add` — media type bound into release
   identity while object key remains excluded.

Tutti's `packages/clients/market-go/source.lock.json` pins the final commit and
the exact hashes for both generated protobuf files. The lock, rather than a
copied hand-maintained DTO, is the cross-repository provenance record.

## Verification evidence

Independent read-only server verification passed:

- full `go test ./...`;
- focused normal tests and vet;
- Market protobuf regeneration with byte-for-byte equality;
- `make api-consumers-check` for 180 operations across three repositories;
- clean worktree and DCO sign-off on all three commits.

Focused tests cover stable/changed digest identity, media type participation,
object-location exclusion, unauthenticated/unknown/inactive/unlisted rejection,
arbitrary-key prevention, URL bounds, descriptor/object binding, rate-limit
fail-closed behavior, durable audit ordering, and URL redaction.

On Tutti, the generated source-lock drift check, Market Go client tests,
descriptor projection, verified fetch tests, and relevant runtime/race/Windows
checks passed; the final renderer/static matrix did not alter this protocol.

## Compatibility and removal

The public `artifact.key` response field may remain deprecated for one measured
compatibility release. It is not present in the new Connector domain/download
decision path. Remove it after:

1. every supported Tutti/Desktop/TSH cohort resolves by digest;
2. minimum supported versions contain the generated resolve client;
3. server metrics show no legacy key-based download for one release cycle.

This window does not permit two download use cases, a client CDN-base setting,
or a client release-digest algorithm.
