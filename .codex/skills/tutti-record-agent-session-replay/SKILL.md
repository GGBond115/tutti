---
name: tutti-record-agent-session-replay
description: Record, inspect, and replay deterministic Tutti AgentGUI Session Replay cassettes. Use when Codex must turn a repository-owned test case into a real Provider-backed cassette, add or run an external named `--scenario`, diagnose recording or replay mismatches, verify Provider frames and portable semantic state, or capture AgentGUI evidence.
---

# Record Tutti Agent Session Replay

Produce evidence in this order:

`case -> deterministic scenario -> real record -> cassette audit -> real replay -> case artifact update`

Do not report a cassette as recorded before both the recording and a fresh
Replay succeed.

## Prepare

1. Read the root and closest `AGENTS.md`.
2. Read `docs/architecture/agent-session-replay.md`.
3. For AgentGUI interactions, read `docs/architecture/agent-gui-node.md` and
   `packages/agent/gui/AGENTS.md`.
4. For lifecycle semantics, read `packages/agent/host/README.md` and keep those
   semantics in Host.
5. Inspect `git status --short`. Preserve pre-existing work.
6. Read the case repository's `case.json`, `scenario.mjs`, fixtures, current
   Cassette, and evidence. The repository is the case source of truth.

Use CDP through the repository runner. Do not use Computer Use unless the user
explicitly requests it.

## Design the scenario

Add or update `cases/<scenario-id>/scenario.mjs` in the QA case repository.
Do not add a case registry or case-specific scenario to the Tutti repository.

Make the scenario deterministic:

- Use one exact prompt with stable markers.
- Use an exact final token when the case expects an assistant reply.
- Use a harmless, reversible command for approval cases.
- Set every behavior-affecting composer default explicitly: permission mode,
  plan mode, model, and reasoning effort as applicable.
- Match the real accessible label, test ID, or semantic DOM state. Do not
  depend on layout coordinates.
- Wait for the interaction to exist before acting.
- Submit each approval, answer, or plan decision once.
- Require the expected terminal state and absence of enabled stale controls.
- Cassette directory name is `{scenarioId}_{provider}` (for example
  `c01_codex`), derived by `defineRecordScenario`.

For question cards, trigger the Provider's real user-input request. For Codex
plan cases, enable Plan Mode, wait for the completed plan and implementation
card, then drive the real plan decision or feedback action.

## Record

Run:

```bash
pnpm e2e:agent-gui -- \
  --record .tmp/cassettes/<scenario-id>_codex \
  --scenario <scenario-id> \
  --scenario-file ../tutti-agent-session-replay-cases/cases/<scenario-id>/scenario.mjs \
  --agent-target-id local:codex \
  --keep-runtime \
  --timeout-ms 300000
```

Omit `--headless` to show the Electron window while debugging; add it for
unattended runs. Keep `--keep-runtime` while developing. Inspect:

- `artifacts/record-agent-gui.png`
- `logs/desktop.log`
- `state/logs/tuttid.log`
- `state/tuttid.db`
- the incomplete cassette and Provider frames

Fix root causes. Do not weaken transport matching, semantic validation, or
terminal-state assertions to make a cassette pass.

## Audit

Run the bundled deterministic audit:

```bash
node .codex/skills/tutti-record-agent-session-replay/scripts/audit-cassette.mjs \
  .tmp/cassettes/<scenario-id>_codex
```

Require:

- cassette inventory and hashes verify;
- Provider manifest is `complete`;
- global and per-connection frame sequences are continuous;
- the expected interaction or plan decision appears once;
- tool execution count and exit status match the case;
- all expected Turns are terminal;
- activity sequences are contiguous and intent→effect causality satisfies
  `packages/agent/session-replay/activity-contract.json`: every effect
  references an earlier intent with a compatible correlationId and a declared
  effect type, every `requiresEffect` intent is answered, and direct-stimulus
  events carry no cause;
- the final assistant token or expected no-text state matches the case.

Inspect a small decoded Provider-frame window around the interaction. Do not
dump the entire Provider stream or expose account data.

## Replay

Run a fresh isolated Replay:

```bash
pnpm e2e:agent-gui -- \
  --replay .tmp/cassettes/<scenario-id>_codex \
  --keep-runtime \
  --timeout-ms 300000
```

Omit `--headless` to show the Electron window; add it for unattended runs.
Require the runner's `replay passed` result. Inspect
`artifacts/replay-agent-gui.png` and confirm the case's UI terminal state.

Provider transport remains fail-closed. Only repository-declared
observer-only probes may be absent or yield to causal traffic.

## Update the case artifacts

Only after record and Replay pass, replace the case repository's
`cases/<scenario-id>/cassette` and `evidence` directories and update its
`case.json` status when required. Do not read or write a remote catalog.

## Finish

Run the repository validation selection policy from
`docs/conventions/testing.md`.

Report:

- cassette names and artifact links;
- record and Replay evidence per case;
- Provider frame, interaction, tool, Turn, and final-response summaries;
- case repository artifacts changed;
- implementation changes and why they changed;
- changed-line distribution, separating pre-existing dirty work;
- documentation impact;
- remaining failed gates or unimplemented scope.
