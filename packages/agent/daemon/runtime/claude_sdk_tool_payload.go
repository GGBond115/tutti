package agentruntime

func claudeSDKCanonicalToolMetadata(value map[string]any) map[string]any {
	metadata := clonePayload(value)
	if metadata == nil {
		return nil
	}
	if response := payloadMap(metadata, "claudeToolResponse"); len(response) > 0 {
		for source, target := range map[string]string{
			"agentId":         "agentId",
			"totalDurationMs": "durationMs",
		} {
			if _, exists := metadata[target]; !exists && response[source] != nil {
				metadata[target] = clonePayloadValue(response[source])
			}
		}
	}
	delete(metadata, "claudeToolResponse")
	return metadata
}
