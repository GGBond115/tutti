package agent

import (
	"context"
	"strings"
)

func (s *Service) Subscribe(ctx context.Context, input StreamInput) (EventStream, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	agentSessionID := strings.TrimSpace(input.AgentSessionID)
	if workspaceID == "" || agentSessionID == "" {
		return EventStream{}, ErrInvalidArgument
	}
	if _, err := s.ensureRuntimeSession(ctx, workspaceID, agentSessionID); err != nil {
		return EventStream{}, err
	}
	events, unsubscribe, ok := s.controller().Subscribe(workspaceID, agentSessionID)
	if !ok {
		return EventStream{}, ErrSessionNotFound
	}
	return EventStream{
		Events:      serviceStreamEvents(ctx, events),
		Unsubscribe: unsubscribe,
	}, nil
}

func (s *Service) controller() RuntimeController {
	return s.Runtime
}
