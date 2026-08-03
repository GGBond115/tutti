// Package relaytransport provides product-neutral byte-stream transport over a
// WebSocket Relay.
//
// It owns WebSocket stream dialing and the reusable owner-tunnel mechanics:
// reference-counted demand, reconnect backoff, WebSocket liveness, yamux stream
// acceptance, and close ordering. Products inject authority credentials, lease
// activation, final release, and stream handlers through narrow interfaces.
// The package never interprets rooms, pairings, accounts, target tokens, or
// application payloads.
package relaytransport
