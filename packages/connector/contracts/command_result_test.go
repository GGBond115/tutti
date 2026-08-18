package contracts

import "testing"

func TestCommandResultValidation(t *testing.T) {
	operation := Operation{OperationID: "operation-1", State: OperationStateAccepted}
	completedOperation := Operation{OperationID: "operation-2", State: OperationStateCompleted}
	failure := CommandFailure{Code: ErrorCodeRevisionConflict, Message: "revision conflict"}
	uncertainFailure := CommandFailure{Code: ErrorCodeUnavailable, Message: "acceptance unknown", Retryable: true}
	tests := []struct {
		name    string
		result  CommandResult
		wantErr bool
	}{
		{name: "accepted", result: CommandResult{Outcome: CommandAccepted, Operation: &operation}},
		{name: "completed", result: CommandResult{Outcome: CommandCompleted, Operation: &completedOperation}},
		{name: "rejected", result: CommandResult{Outcome: CommandRejected, Failure: &failure}},
		{name: "uncertain", result: CommandResult{Outcome: CommandUncertain, Failure: &uncertainFailure}},
		{name: "accepted failure", result: CommandResult{Outcome: CommandAccepted, Operation: &operation, Failure: &failure}, wantErr: true},
		{name: "rejected operation", result: CommandResult{Outcome: CommandRejected, Operation: &operation, Failure: &failure}, wantErr: true},
		{name: "retryable conflict", result: CommandResult{Outcome: CommandRejected, Failure: &CommandFailure{Code: ErrorCodeRevisionConflict, Message: "conflict", Retryable: true}}, wantErr: true},
		{name: "unknown", result: CommandResult{Outcome: "future"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.result.Validate(); (got != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", got, test.wantErr)
			}
		})
	}
}
