package tuttimodeexecution

import (
	"fmt"

	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
)

func MainWakePrompt(wake executionbiz.Wake) string {
	schedule := fmt.Sprintf(
		"tutti plan issue schedule --issue-id %s --checkpoint-id %s --expected-graph-revision %d",
		wake.IssueID, wake.CheckpointID, wake.CheckpointRevision,
	)
	header := fmt.Sprintf(`A durable Tutti Mode execution checkpoint requires your review.

Issue: %s
Checkpoint: %s
Kind: %s
Graph revision: %d

Review the current Issue, task results, and evidence before choosing the next action. The daemon does not dispatch a successor automatically.

To schedule an exact next set, use:
%s --task-ids-json '<json-array>' --request-id '<stable-request-id>'`,
		wake.IssueID, wake.CheckpointID, wake.CheckpointKind,
		wake.CheckpointRevision, schedule,
	)
	switch wake.CheckpointKind {
	case executionbiz.CheckpointKindTaskSettled,
		executionbiz.CheckpointKindTaskFailed,
		executionbiz.CheckpointKindTaskCanceled:
		return header + fmt.Sprintf(`

If another Run is active or a later checkpoint is pending, you may resolve this review without adding work:
tutti plan issue acknowledge --issue-id %s --checkpoint-id %s --expected-graph-revision %d --request-id '<stable-request-id>'`,
			wake.IssueID, wake.CheckpointID, wake.CheckpointRevision,
		)
	default:
		return header + `

Do not use acknowledge for initial scheduling or Goal Review; choose a legal checkpoint-fenced command.`
	}
}
