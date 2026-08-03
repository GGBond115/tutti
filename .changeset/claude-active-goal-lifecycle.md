---
"@tutti-os/agent-gui": patch
"@tutti-os/claude-sdk-sidecar": patch
---

Normalize Claude SDK `active_goal` messages and native `/goal` Stop-hook `goal_status` attachments, stop inferring Goal completion from ordinary Turn settlement, and settle exact Goal control operations through the Host-owned lifecycle lane instead of transient session runtime context.
