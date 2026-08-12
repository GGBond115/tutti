---
"@tutti-os/workbench-surface": patch
---

Render focused Workbench windows in isolation before the Dock popup obscures
them, then reuse the revision-scoped image for multi-window previews without
capturing overlapping windows or overlays.
