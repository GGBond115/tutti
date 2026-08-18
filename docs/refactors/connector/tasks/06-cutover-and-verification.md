# T06: cutover, deletion, and final verification

Status: implementation, deletion audit, and static/Go/TypeScript verification
are complete. Root-owned DCO rebase, post-rebase smoke, and clean-tree
confirmation remain repository closeout rather than implementation work.

## Objective

Prove that the delivered system contains one Connector implementation at every
layer, all compatibility is bounded and fail closed, and the completed
implementation passes architecture, behavior, generated-contract, performance,
consumer, and platform gates. Root closeout repeats smoke checks after rebase.

## Deletion and single-owner audit

Already delivered; final static searches must confirm they remain absent:

- historical `packages/connector/host` package and imports;
- Connector imports/aliases of Agent runtime process primitives;
- duplicate connection ID/hash/prefix derivation outside the binding resolver;
- duplicate remote Market source, handwritten wire manifest, client release
  digest, client source revision, or artifact-key URL join;
- mutable application root/store exported to renderer or product UI;
- AgentGUI Connector components, types, labels, states, navigation, package
  dependency, or slash/menu fallback;
- a second physical renderer implementation or product `/ui` import;
- constructor-started daemon workers, unregistered watchers, unbounded close,
  dual runtime/source reads, and feature-flag fallback;
- production daemon source imports of runtime/store-sqlite, and any daemon
  require/replace edge that is not classified and proven test-fixture-only;
- runtime/process imports of contracts or application;
- normal-path fallback to legacy installed-release tables/projection/operation
  payloads after the one-time migration marker.

The only permitted temporary compatibility is:

- `/ui` forwarding to `/renderer` for one release;
- deprecated server `artifact.key` response for one measured release cycle;
- one old daemon wire decoded centrally to read-only/unsupported presentation;
- older global mutation revision normalized by one application fence rule.

## Final verification matrix

The completed tree passed the following implementation matrix. Root closeout
will repeat smoke-sensitive gates after history normalization.

| Lane                  | Required evidence                                                                                   | Final result          |
| --------------------- | --------------------------------------------------------------------------------------------------- | --------------------- |
| contracts/application | normal/race/vet, all presentation/action states, command outcomes, policy, revision/cancel replay   | passed                |
| runtime/process       | normal/race/vet, identity, bounded I/O, process fencing/tree close, commands/observation            | passed                |
| daemon/store          | lifecycle rollback/close, health, three-way convergence, retry budget, migration, race/vet          | passed                |
| Market source/client  | generated source lock, manifest/target/descriptor mapping, verified fetch, no key/digest derivation | passed                |
| Connector Market TS   | tests, typecheck, build, canonical presentation/action consumption                                  | passed                |
| AgentGUI              | focused tests/typecheck, neutral slot degradation and import gate                                   | passed                |
| Desktop               | focused tests/typecheck/build, explicit transport mapper and shared bridge/model behavior           | passed                |
| boundaries/generated  | source and go.mod DAG, test-only edge proof, Connector/renderer/UI/i18n/export/API drift/searches   | passed                |
| platform              | Windows Go compile plus native process evidence where runner available                              | passed recorded gates |
| package consumers     | ESM + NodeNext resolution for approved public subpaths                                              | passed                |
| DCO/cleanliness       | every commit signed, post-rebase smoke, final clean Tutti worktree                                  | root closeout pending |

Repository commands include:

```bash
pnpm check:connector-boundaries
pnpm check:renderer-boundaries
pnpm check:agent-gui-degradation
pnpm check:api-generated
pnpm check:i18n

pnpm --filter @tutti-os/connector-market test
pnpm --filter @tutti-os/connector-market typecheck
pnpm --filter @tutti-os/connector-market build
pnpm --filter @tutti-os/agent-gui test
pnpm --filter @tutti-os/agent-gui typecheck
pnpm --filter @tutti-os/desktop test
pnpm --filter @tutti-os/desktop typecheck
pnpm --filter @tutti-os/desktop build
```

Affected Go modules run their actual repository-supported form of:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Generated Market source is checked with
`node tools/scripts/sync-market-go-client.mjs --check`. TSH-server evidence is
the independently verified clean chain ending at
`b1bb4de6f71d1068a81e2f3098fdc93a43ca4add`; do not regenerate Tutti from a
different server revision.

The architecture lane must inspect every Connector Go module's `require` and
`replace` directives as well as production imports. It must prove the parallel
outer-module shape (`daemon`, `runtime`, `store-sqlite`, and `market/source`
over `application/contracts`), prove `runtime/process` imports neither contracts
nor application, and classify every daemon-to-store occurrence as test-fixture-
only. A test-only import is acceptable only when the gate demonstrates that no
non-`_test.go` file depends on it. The daemon's store-sqlite `require`/`replace`
pair must be reported as test-fixture support rather than silently treated as a
production dependency or forbidden without classification.

## Required behavioral assertions

- install, update, authorize, cancel, disconnect, uninstall, and auto update are
  admitted only through canonical semantic actions; Catalog refresh remains a
  separate freshness/command operation;
- accepted/completed/rejected/uncertain command results are structurally valid;
- conflict and uncertainty perform authoritative read without automatic replay;
- route exit/lost event and silent physical drift recover within bounds;
- exact healthy routes do not restart;
- stale catalog is visible/read-only, exact installed ready runtime remains
  usable, and no install/update/new authorization is admitted;
- missing/stale/unknown shared Agent policy fails closed per Connector;
- unknown/malformed presentation is unsupported and never selectable;
- missing AgentGUI slot hides only the entry;
- signed artifact URL is neither persisted nor logged and downloaded bytes are
  media-type/size/SHA verified;
- one-time installed-release migration commits atomically and no normal legacy
  fallback remains.

Authorization-cancel crash recovery is implemented with a durable cancellation
fence. An exact client request resumes unresolved/canceling receipts, completes
projection reset after a terminal provider receipt, and remains recoverable when
the first projection transaction fails; a different client request is rejected
without adopting or canceling the fenced session. The application normal suite
and the full Connector gates passed for this fix.

## Performance and lifecycle assertions

- one renderer model/subscription source per window and one dialog host/window;
- stable slot/model/command/event references across unrelated Session/Turn
  streaming;
- bounded quick lists, snapshots, errors, event queues, worker groups, command
  drain, scheduler wait, and close phases;
- no physical snapshot on the 500 ms durable scan;
- full-jitter backoff and per-Connector failure budget prevent restart/poll
  storms;
- exact healthy snapshots perform no process restart;
- no material Catalog query, refresh transaction, renderer fanout, goroutine,
  or idle wake-up regression.

## Consumer and platform assertions

- Workspace and Standalone use the same bridge/event semantics;
- approved package exports resolve in ESM and NodeNext, and export/build/
  declaration/publish metadata is in parity;
- Connector runtime uses verified absolute argv execution, native path APIs,
  case-insensitive environment keys, and full process-tree cleanup without
  shell-only assumptions;
- Windows compile passes from the final tree; native descendant cleanup/path
  cases require Windows runner evidence and cannot be inferred from POSIX;
- TSH production code remains unchanged by scope. Deferred TSH work consumes
  the released Tutti cohort and shared-Agent allowlist contract without
  reintroducing a Market source or fallback.

## Root-only repository closeout

After this documentation is committed, root will:

1. normalize DCO sign-off once while preserving logical commit boundaries;
2. rerun all checks affected by rewritten history/generated provenance;
3. verify every branch commit contains the expected sign-off;
4. verify Tutti and tsh-server worktrees are clean;
5. report final commit list, exact commands/results, residual risks, and bounded
   compatibility removal gates.

Known residuals that do not change architecture acceptance:

- server digest reverse lookup is currently O(N) and should gain an index;
- `/ui`, deprecated `artifact.key`, and one old daemon wire remain only for the
  documented compatibility windows;
- production TSH consumer migration is a later task;
- an existing onboarding ZIP fixture and constrained-runner process-tree test
  may be flaky; record them explicitly and do not use them to waive deterministic
  Connector-focused failures.

T06 implementation and verification are complete. DCO normalization,
post-rebase smoke, and clean-tree confirmation remain root-owned repository
closeout and must not be reported as complete until performed.
