// Package relaytransport provides product-neutral byte-stream transport over a
// WebSocket Relay.
//
// It owns WebSocket stream dialing and the reusable owner-tunnel mechanics:
// reference-counted demand, reconnect backoff, WebSocket liveness, continuous
// owner readiness, yamux stream acceptance, and close ordering. Products inject
// authority credentials, lease activation, final release, and stream handlers
// through narrow interfaces. OwnerLifecycle.Activate returns an
// OwnerActivation whose Readiness context must remain live for the complete
// connection generation; it is not merely a one-time readiness barrier.
// The package never interprets rooms, pairings, accounts, target tokens, or
// application payloads.
package relaytransport
