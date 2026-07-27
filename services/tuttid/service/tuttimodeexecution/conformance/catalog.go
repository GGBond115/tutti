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
	return append(scenarios, ScheduleCatalog()...)
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
