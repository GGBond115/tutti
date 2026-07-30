#!/usr/bin/env bash
#
# rescue-edit-retry-poison-pill.sh
#
# Unbricks a Tutti install that crashes on launch with:
#   "build tuttid server: recover agent host:
#      process runtime operation <id>: agent session not found"
#   "failed to start managed tuttid: tuttid exited before it published its listener info."
#
# Cause: a durable `edit_retry` runtime operation is stuck (rolled back but never
# resent). It is harmless while the daemon runs, but on the next daemon restart
# the cold-recovery pass treats it as fatal, so tuttid exits(1) on every launch.
#
# This script quarantines those stuck operations (marks them `failed`, which
# removes them from the recovery claimable set) so the daemon can boot again.
# It does NOT delete history and it backs the database up first.
#
# Usage:
#   1. Fully quit Tutti (Cmd-Q). The daemon must be stopped.
#   2. bash tools/scripts/rescue-edit-retry-poison-pill.sh
#   3. Reopen Tutti.
#
# Options (env vars):
#   TUTTI_DB=/path/to/tuttid.db   Override DB path (default: ~/.tutti/tuttid.db)
#   MODE=delete                   Delete the rows instead of marking them failed
#   YES=1                         Skip the confirmation prompt
#
set -euo pipefail

DB="${TUTTI_DB:-$HOME/.tutti/tuttid.db}"
MODE="${MODE:-quarantine}"

die() { echo "❌ $*" >&2; exit 1; }

command -v sqlite3 >/dev/null 2>&1 || die "sqlite3 not found (macOS ships it at /usr/bin/sqlite3)."
[[ -f "$DB" ]] || die "database not found: $DB (set TUTTI_DB=... to override)"

# 1. Refuse to run while the daemon/app is alive (avoids a locked DB and racing recovery).
if pgrep -x tuttid >/dev/null 2>&1 || pgrep -f "Tutti.app/Contents/" >/dev/null 2>&1; then
  die "Tutti / tuttid is still running. Fully quit Tutti (Cmd-Q) and re-run this script."
fi

WHERE="kind='edit_retry' AND status IN ('prepared','leased')"

# 2. Show what will change.
CNT="$(sqlite3 "$DB" "SELECT count(*) FROM workspace_agent_runtime_operations WHERE $WHERE;")"
echo "Database: $DB"
echo "Stuck edit_retry operations found: $CNT"
if [[ "$CNT" -eq 0 ]]; then
  echo "✅ Nothing to fix — no stuck edit_retry operations. The crash is caused by something else."
  exit 0
fi
echo
echo "-------------------------------------------------------------------------------"
sqlite3 -header -column "$DB" \
  "SELECT substr(operation_id,1,8) AS op, substr(agent_session_id,1,8) AS session, status, attempt, substr(last_error,1,40) AS last_error
   FROM workspace_agent_runtime_operations WHERE $WHERE;"
echo "-------------------------------------------------------------------------------"
echo
if [[ "$MODE" == "delete" ]]; then
  echo "Action: DELETE these $CNT operation row(s)."
else
  echo "Action: mark these $CNT operation row(s) as 'failed' (quarantine; reversible from backup)."
fi

# 3. Confirm.
if [[ "${YES:-}" != "1" ]]; then
  read -r -p "Proceed? [y/N] " reply
  [[ "$reply" == "y" || "$reply" == "Y" ]] || die "Aborted; no changes made."
fi

# 4. Consistent backup (includes any WAL) before mutating.
BAK="${DB}.bak.$(date +%Y%m%d%H%M%S)"
sqlite3 "$DB" ".backup '$BAK'"
echo "🗄  Backup written: $BAK"

# 5. Apply. Two parts, both required:
#    (a) unfence the affected sessions' effective history back to 'ready' so they
#        can send again — a stuck edit_retry leaves recovery_state at
#        resend_pending/rollback_pending/recovery_required, which blocks every
#        subsequent prompt. This MUST run before deleting the operation rows,
#        because it identifies the fenced sessions via those rows.
#    (b) fail (or delete) the stuck operation rows so recovery no longer trips.
NOW_MS="CAST((julianday('now')-2440587.5)*86400000 AS INTEGER)"
sqlite3 "$DB" ".timeout 5000" "
  UPDATE workspace_agent_session_history
     SET recovery_state='ready', operation_id='', updated_at_unix_ms=$NOW_MS
   WHERE recovery_state != 'ready'
     AND operation_id IN (SELECT operation_id FROM workspace_agent_runtime_operations WHERE kind='edit_retry');"
if [[ "$MODE" == "delete" ]]; then
  sqlite3 "$DB" ".timeout 5000" "DELETE FROM workspace_agent_runtime_operations WHERE $WHERE;"
else
  sqlite3 "$DB" ".timeout 5000" "
    UPDATE workspace_agent_runtime_operations
       SET status='failed', result='failed',
           lease_owner=NULL, lease_expires_at_unix_ms=NULL, next_attempt_at_unix_ms=NULL,
           last_error='quarantined by rescue-edit-retry-poison-pill.sh',
           version=version+1, updated_at_unix_ms=$NOW_MS
     WHERE $WHERE;"
fi

REMAIN="$(sqlite3 "$DB" "SELECT count(*) FROM workspace_agent_runtime_operations WHERE $WHERE;")"
[[ "$REMAIN" -eq 0 ]] || die "expected 0 stuck rows after fix, still $REMAIN — restore from $BAK and report."
echo "✅ Done. Cleared $CNT stuck operation(s). Reopen Tutti now."
echo "   (If anything looks wrong, restore with:  cp '$BAK' '$DB')"
