# @tutti-os/agent-session-replay

Provider-neutral renderer contracts for Agent Session Replay.

This package owns the portable activity event type and the interaction
contract shared by Tutti Desktop and TSH. Product adapters keep ownership of
scope mapping, persistence, HTTP/Electron integration, replay runners, and
provider/runtime setup.

The package does not enable recording or replay by itself. Hosts must inject
the replay adapter only for an explicitly selected replay session; ordinary
AgentGUI rendering and provider execution remain unchanged.
