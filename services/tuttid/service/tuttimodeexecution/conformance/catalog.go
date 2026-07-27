package conformance

import (
	"context"
	"fmt"
)

type Scenario struct {
	Name string
	run  func(context.Context, Driver) error
}

func Catalog() []Scenario {
	scenarios := MaterializationCatalog()
	scenarios = append(scenarios, ScheduleCatalog()...)
	return append(scenarios, SettlementCatalog()...)
}

func SettlementCatalog() []Scenario {
	return []Scenario{
		{
			Name: "TerminalSettlementCreatesCheckpointWithoutSuccessor",
			run:  runTerminalSettlementCreatesCheckpointWithoutSuccessor,
		},
		{
			Name: "ParallelSettlementsQueueOrderedCheckpointBacklog",
			run:  runParallelSettlementsQueueOrderedCheckpointBacklog,
		},
		{
			Name: "SettlementReviewCanScheduleDependentNextStep",
			run:  runSettlementReviewCanScheduleDependentNextStep,
		},
		{
			Name: "ScheduleReviewPromotesExistingSettlementBacklog",
			run:  runScheduleReviewPromotesExistingSettlementBacklog,
		},
		{
			Name: "TimedOutRunCreatesFailedCheckpoint",
			run:  runTimedOutRunCreatesFailedCheckpoint,
		},
		{
			Name: "AuthoritativeLaunchFailureSettlesRun",
			run:  runAuthoritativeLaunchFailureSettlesRun,
		},
		{
			Name: "ExpiredLaunchOwnerCannotSettleReclaimedRun",
			run:  runExpiredLaunchOwnerCannotSettleReclaimedRun,
		},
		{
			Name: "RepairRestoresMissingSettlementCheckpoint",
			run:  runRepairRestoresMissingSettlementCheckpoint,
		},
		{
			Name: "AcknowledgeFencesAndDrainsBacklogIntoGoalReview",
			run:  runAcknowledgeFencesAndDrainsBacklogIntoGoalReview,
		},
		{
			Name: "AcknowledgeEligibilityUsesActiveWorkOrBacklog",
			run:  runAcknowledgeEligibilityUsesActiveWorkOrBacklog,
		},
		{
			Name: "MixedTerminalOutcomesReachGoalReview",
			run:  runMixedTerminalOutcomesReachGoalReview,
		},
	}
}

func MaterializationCatalog() []Scenario {
	return []Scenario{
		{
			Name: "MaterializedPlanRequiresInitialSchedule",
			run:  runMaterializedPlanRequiresInitialSchedule,
		},
	}
}

func ScheduleCatalog() []Scenario {
	return []Scenario{
		{
			Name: "SourceSchedulesExactSet",
			run:  runSourceSchedulesExactSet,
		},
		{
			Name: "ScheduleRejectsInvalidSetAtomically",
			run:  runScheduleRejectsInvalidSetAtomically,
		},
		{
			Name: "ScheduleRequestIdentityIsIdempotent",
			run:  runScheduleRequestIdentityIsIdempotent,
		},
		{
			Name: "PreparedLaunchIntentIsRecoverable",
			run:  runPreparedLaunchIntentIsRecoverable,
		},
		{
			Name: "ActiveRunBudgetReservationRejectsWholeSet",
			run:  runActiveRunBudgetReservationRejectsWholeSet,
		},
		{
			Name: "ConcurrentReplayClaimsOneDelivery",
			run:  runConcurrentReplayClaimsOneDelivery,
		},
		{
			Name: "ExpiredLaunchLeaseIsRecoveredOnce",
			run:  runExpiredLaunchLeaseIsRecoveredOnce,
		},
		{
			Name: "IdleRecoveryQueueObservesScheduleAdmission",
			run:  runIdleRecoveryQueueObservesScheduleAdmission,
		},
	}
}

func Run(ctx context.Context, driver Driver, scenario Scenario) error {
	if driver == nil {
		return fmt.Errorf("Tutti mode execution conformance driver is required")
	}
	if scenario.run == nil {
		return fmt.Errorf("Tutti mode execution conformance scenario %q has no runner", scenario.Name)
	}
	return scenario.run(ctx, driver)
}
