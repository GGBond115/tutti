# Connector Market release rollout

The Connector Market crosses three repositories and must be released in trust
dependency order. A downstream service must never pin a Tutti pseudo-version
or use a local `replace` directive to make CI green.

## Required order

1. Merge the Tutti shared-domain and daemon-host change into `main`.
2. Run the Tutti `npm-package-release` workflow on `main`. It publishes one
   exact package cohort and creates the matching
   `packages/connector/market/vX.Y.Z` Go module tag.
3. Verify that the released Go verifier accepts the production
   `Ed25519-SHA256` golden catalog and release fixtures.
4. Atomically update every governed Tutti Go module in TSH to that same exact
   `vX.Y.Z`; run `bash tests/check_tutti_dependencies.sh --go-module-graph`.
5. Merge and deploy the ZK publisher authority before enabling TSH projection
   traffic. ZK must pass its trust preflight with real deployment-supplied
   OIDC policy, provenance key, release KMS key, and versioned artifact bucket.
6. Merge TSH projection and artifact-grant support, then roll out Tutti clients
   with the matching embedded market keyring.

## Trust-root rotation

Market signing keys are compiled into a version-controlled client keyring.
Rotation is two-phase:

1. ship clients containing `current + next` keys while ZK still signs with
   `current`;
2. switch ZK to `next`, wait until the minimum supported client population has
   the overlap keyring, then remove `current` in a later client release.

Environment variables and remote configuration may not replace production
trust roots. Catalog sequence and signed-payload high-water state remain in
force across a key change; a different payload at an already accepted
sequence is equivocation and must be rejected.

## Rollback

Rolling back ZK means restoring the previous signer and previously accepted
catalog only while supported clients still contain that key. Rolling back a
desktop binary does not roll back its catalog high-water state. Connector
artifacts remain addressed by signed digest and object version; never replace
an S3 object in place or synthesize a signature for a legacy release.
