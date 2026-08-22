# Account Device Presence

`tuttid` starts an account-scoped device-presence worker after its local HTTP
listener is ready. The worker is independent of Personal/Mobile Remote feature
flags.

The worker uses the daemon-wide stable `device_id`, reports hostname, platform,
architecture, and client version through the existing current-device endpoint,
then opens a process-session lease and immediately sends the activation
heartbeat. It renews at the server-provided interval (currently 30 seconds).

On a late timer after sleep, or when a lease is no longer found, the worker
re-registers metadata and opens a new lease. Open requests are idempotent for
the process-session identifier, so a lost response cannot create unbounded
leases. Logout stops and best-effort closes the exact lease before account auth
is cleared; a failed logout restarts the worker. Daemon shutdown follows the
same bounded close path. If the close cannot reach the server, the server-side
90-second logical TTL remains authoritative.

The default control-plane base URL is `https://tutti.sh/api/desktop/v1`.
`TUTTI_DESKTOP_CONTROL_PLANE_BASE_URL` overrides it. For compatibility,
`TUTTI_MOBILE_CONTROL_PLANE_BASE_URL` is used when the generic override is
unset.
