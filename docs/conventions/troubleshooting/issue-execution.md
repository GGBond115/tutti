# Issue execution

## Stop remains pending while the Agent Turn is already canceled

Check the durable Issue, Run, Agent Turn, runtime operation, and Agent outbox
facts separately. A paused Issue with a running Run and a terminal Agent Turn
usually indicates that synchronous Agent settlement re-entered Issue mutation
while the stop caller still held the same per-Issue mutex.

The invariant is:

> Never call Agent Host, git worktree operations, or another re-entrant module
> while holding the Issue mutation lock.

Cancellation should persist `dispatchPaused=true` and snapshot running Runs
under the lock, release it, then cancel Agent Sessions and idempotently settle
Runs from exact canonical Turn facts. A regression test should use a canceller
that publishes settlement synchronously before returning; a passive recorder
cannot reproduce this deadlock class. A second barrier test should pause while
launch is in flight and verify Stop returns immediately while the non-blocking
launch gate still requests exact-Turn compensation after launch.

Do not fix this symptom by treating the UI test as flaky, adding a timeout, or
introducing another pause flag. Those changes hide the blocked command without
repairing the callback cycle.

For the ownership and data-flow contract, see
[Issue Execution Coordination](../../architecture/issue-execution.md).

## Stopping a Tutti source Turn leaves task Sessions running

First separate source-Turn cancellation from Issue execution cancellation. The
source path is healthy when logs contain `agent_session.cancel.accepted` for the
planning Session. If it is immediately followed by
`workspace_issue.source_session_cancel_failed` with
`tutti-owned issue is managed by its source conversation`, the Agent Turn
stopped but the Issue cascade did not. Confirm that the Issue still has
`dispatchPaused=false` and that its running Runs remain active.

This happens when the already-authorized source-session cascade calls the
generic `CancelIssueExecution` entrypoint. That entrypoint must reject
Tutti-managed graphs; weakening its ownership guard would expose managed Issues
to unrelated mutations.

Keep the generic guard intact. After proving the Issue has
`PlanningSourceTuttiModePlan` and the exact `SourceSessionID`, route the cascade
through `CancelTuttiModeIssueExecution`. That path durably pauses dispatch
before it fans out exact Run Session cancellations. Validate with
`TestCancelIssueExecutionForSourceSessionUsesManagedTuttiStopPath` plus the
focused Issue execution cancellation tests. Desktop's embedded Tutti plan
panel reaches the same managed path through
`POST /v1/workspaces/{workspaceID}/tutti-executions/{issueID}/cancel-execution`;
it must not call the generic Issue Manager cancel endpoint.
