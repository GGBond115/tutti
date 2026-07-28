package tuttimodeexecution

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	agenthost "github.com/tutti-os/tutti/packages/agent/host"
	executionbiz "github.com/tutti-os/tutti/services/tuttid/biz/tuttimodeexecution"
)

type SourceDeletionAdmissionStore interface {
	AdmitSourceSessionDeletion(
		context.Context,
		executionbiz.SourceSessionDeletionAdmission,
	) (executionbiz.SourceSessionDeletionAdmission, error)
	ReportSourceSessionDeletion(
		context.Context,
		executionbiz.SourceSessionDeletionAdmission,
		bool,
		time.Time,
	) error
	ReconcileSourceSessionDeletionAdmissions(context.Context, time.Time) error
}

type SourceDeletionGuard struct {
	Store SourceDeletionAdmissionStore
	Clock func() time.Time

	lockMu       sync.Mutex
	closureLocks map[string]*sourceDeletionClosureLock
}

type sourceDeletionClosureLock struct {
	mu   sync.Mutex
	refs int
}

func (guard *SourceDeletionGuard) AdmitDeleteSessions(
	ctx context.Context, plan agenthost.DeleteSessionsPlan,
) error {
	if guard == nil || guard.Store == nil {
		return ErrServiceUnavailable
	}
	workspaceID, sessionIDs, closureKey := normalizedSourceDeletionPlan(plan)
	if len(sessionIDs) == 0 {
		return nil
	}
	guard.acquireClosure(closureKey)
	_, err := guard.Store.AdmitSourceSessionDeletion(ctx, executionbiz.SourceSessionDeletionAdmission{
		WorkspaceID: workspaceID,
		SessionIDs:  sessionIDs,
		Now:         guard.now(),
	})
	if err != nil {
		guard.releaseClosure(closureKey)
	}
	return err
}

func (guard *SourceDeletionGuard) ReportDeleteSessions(
	ctx context.Context, report agenthost.DeleteSessionsReport,
) {
	workspaceID, sessionIDs, closureKey := normalizedSourceDeletionPlan(report.Plan)
	if len(sessionIDs) == 0 {
		return
	}
	defer guard.releaseClosure(closureKey)
	if guard == nil || guard.Store == nil {
		return
	}
	admission := executionbiz.SourceSessionDeletionAdmission{
		WorkspaceID: workspaceID,
		SessionIDs:  sessionIDs,
		Now:         guard.now(),
	}
	_ = guard.Store.ReportSourceSessionDeletion(ctx, admission, report.Err == nil, guard.now())
}

func (guard *SourceDeletionGuard) Recover(ctx context.Context) error {
	if guard == nil || guard.Store == nil {
		return ErrServiceUnavailable
	}
	return guard.Store.ReconcileSourceSessionDeletionAdmissions(ctx, guard.now())
}

func (guard *SourceDeletionGuard) now() time.Time {
	if guard.Clock != nil {
		return guard.Clock().UTC()
	}
	return time.Now().UTC()
}

func (guard *SourceDeletionGuard) acquireClosure(key string) {
	guard.lockMu.Lock()
	if guard.closureLocks == nil {
		guard.closureLocks = make(map[string]*sourceDeletionClosureLock)
	}
	lock := guard.closureLocks[key]
	if lock == nil {
		lock = &sourceDeletionClosureLock{}
		guard.closureLocks[key] = lock
	}
	lock.refs++
	guard.lockMu.Unlock()
	lock.mu.Lock()
}

func (guard *SourceDeletionGuard) releaseClosure(key string) {
	if guard == nil {
		return
	}
	guard.lockMu.Lock()
	lock := guard.closureLocks[key]
	guard.lockMu.Unlock()
	if lock == nil {
		return
	}
	lock.mu.Unlock()
	guard.lockMu.Lock()
	lock.refs--
	if lock.refs == 0 && guard.closureLocks[key] == lock {
		delete(guard.closureLocks, key)
	}
	guard.lockMu.Unlock()
}

func normalizedSourceDeletionPlan(
	plan agenthost.DeleteSessionsPlan,
) (string, []string, string) {
	workspaceID := strings.TrimSpace(plan.WorkspaceID)
	sessionIDs := make([]string, 0, len(plan.SessionIDs))
	seen := make(map[string]struct{}, len(plan.SessionIDs))
	for _, sessionID := range plan.SessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		if _, duplicate := seen[sessionID]; duplicate {
			continue
		}
		seen[sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, sessionID)
	}
	slices.Sort(sessionIDs)
	return workspaceID, sessionIDs, workspaceID + "\x00" + strings.Join(sessionIDs, "\x00")
}

var _ agenthost.SessionDeletionGuard = (*SourceDeletionGuard)(nil)
