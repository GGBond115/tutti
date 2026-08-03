---
"@tutti-os/agent-gui": major
"@tutti-os/agent-activity-core": major
---

Make `AgentGUIRuntime` the sole AgentGUI host contract and remove the legacy
`AgentActivityRuntime` interface, Provider, hooks, and test overrides. Desktop
and TSH now use the narrow contract without duplicating lifecycle callbacks
already owned by `AgentSessionEngine`; Mobile was audited and has no dependency
on the removed contract.

Remove the legacy `EngineCommandPort`,
`EngineExternalCommandExceptPlanDecision`, and `dispatchSessionMutation`
exports from `@tutti-os/agent-activity-core`. Hosts must use
`EngineTypedCommandPort`, `AgentSessionEffectPort`, and the semantic mutation
methods on `AgentSessionEngine`.
