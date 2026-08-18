package agentruntime

func (c *Controller) recordConfigOptionsUpdate(session Session, update AgentSessionConfigOptionsUpdate) {
	if c == nil {
		return
	}
	key := sessionKey(session.RoomID, session.AgentSessionID)
	c.mu.Lock()
	c.configOptionsUpdates[key] = update
	c.mu.Unlock()
}

func (*Controller) completeConfigOptionsUpdate(session Session, update AgentSessionConfigOptionsUpdate) AgentSessionConfigOptionsUpdate {
	if update.RoomID == "" {
		update.RoomID = session.RoomID
	}
	if update.Provider == "" {
		update.Provider = session.Provider
	}
	if update.ProviderSessionID == "" {
		update.ProviderSessionID = session.ProviderSessionID
	}
	if update.OccurredAtUnixMS <= 0 {
		update.OccurredAtUnixMS = unixMS(now())
	}
	return update
}

func configOptionsUpdateStreamEvent(update AgentSessionConfigOptionsUpdate) StreamEvent {
	return StreamEvent{
		EventType: StreamEventConfigOptions,
		Data:      update,
	}
}

func cloneAgentSessionCommands(commands []AgentSessionCommand) []AgentSessionCommand {
	if len(commands) == 0 {
		return []AgentSessionCommand{}
	}
	out := make([]AgentSessionCommand, len(commands))
	copy(out, commands)
	return out
}
