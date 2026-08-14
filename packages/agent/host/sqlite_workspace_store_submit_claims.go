package agenthost

import (
	"context"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func (s *SQLiteWorkspaceStore) PrepareSubmitClaim(
	ctx context.Context,
	input storesqlite.SubmitClaimPrepare,
) (storesqlite.SubmitClaim, bool, error) {
	store, err := s.store(input.WorkspaceID)
	if err != nil {
		return storesqlite.SubmitClaim{}, false, err
	}
	return store.PrepareSubmitClaim(ctx, input)
}

func (s *SQLiteWorkspaceStore) RecordSubmitClaimGuidanceDisposition(
	ctx context.Context,
	input storesqlite.SubmitClaimGuidanceDispositionRecord,
) (storesqlite.SubmitClaim, bool, error) {
	store, err := s.store(input.WorkspaceID)
	if err != nil {
		return storesqlite.SubmitClaim{}, false, err
	}
	return store.RecordSubmitClaimGuidanceDisposition(ctx, input)
}

func (s *SQLiteWorkspaceStore) AcceptSubmitClaim(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	clientSubmitID string,
	turnID string,
	now int64,
) (storesqlite.SubmitClaim, bool, error) {
	store, err := s.store(workspaceID)
	if err != nil {
		return storesqlite.SubmitClaim{}, false, err
	}
	return store.AcceptSubmitClaim(
		ctx, workspaceID, sessionID, clientSubmitID, turnID, now,
	)
}

func (s *SQLiteWorkspaceStore) RejectSubmitClaim(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	clientSubmitID string,
	turnID string,
	now int64,
) (storesqlite.SubmitClaim, bool, error) {
	store, err := s.store(workspaceID)
	if err != nil {
		return storesqlite.SubmitClaim{}, false, err
	}
	return store.RejectSubmitClaim(
		ctx, workspaceID, sessionID, clientSubmitID, turnID, now,
	)
}

func (s *SQLiteWorkspaceStore) DeleteSubmitClaim(
	ctx context.Context,
	workspaceID string,
	sessionID string,
	clientSubmitID string,
) (bool, error) {
	store, err := s.store(workspaceID)
	if err != nil {
		return false, err
	}
	return store.DeleteSubmitClaim(
		ctx, workspaceID, sessionID, clientSubmitID,
	)
}
