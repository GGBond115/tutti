# Desktop Screenshot To Qute

The desktop can send an arbitrary rectangular screen region directly to an
Agent as a multimodal prompt. The Agent can optionally be instructed to create
and manage a Qute Task while it works. The default global shortcut is
`CommandOrControl+Shift+S`, and it works while another application has focus as
long as Tutti is running and can resolve a current or startup workspace.

## Ownership And Flow

The feature follows the existing desktop/daemon boundary:

```text
Electron main process
  global shortcut -> display capture -> trusted selection crop
        |
        v
restricted capture preload + renderer
  region selection -> AgentGUI quick composer
        |
        v
workspace renderer
  existing AgentSessionEngine activation
        |
        v
Agent Host
  create Session + initial Turn from text and image blocks
```

- `apps/desktop` owns the operating-system shortcut, screen capture, crop,
  floating window, and narrow IPC bridge. The capture surface does not create a
  second activity Engine. Its main-process adapter sends typed launch requests
  to the renderer that already owns the workspace Engine.
- `packages/agent/gui` owns the reusable `quick-composer` entry. It reuses the
  canonical rich-text Composer, image preview, Agent Target selector, keyboard
  submission behavior, and localized copy while owning no Session lifecycle.
- `packages/workspace/issue-manager` owns the reusable Issue Manager contract,
  attachment presentation, and localized labels.
- `services/tuttid/service/workspace` owns validation, ContextRef lifecycle,
  and projection into an Issue Run launch. `services/tuttid/data/workspace`
  owns the managed attachment-file persistence adapter. The tuttid-local
  immutable launch snapshot and codec live in `services/tuttid/biz/workspace`
  so service and data share an explicit contract without exposing host paths
  through reusable workspace packages.
- `packages/agent/host` remains the lifecycle owner. Screenshot launch uses the
  existing `AgentSessionEngine.activateSession` path, which delegates to Host
  with provider-neutral text and image blocks. The separate explicit
  `startWorkspaceIssueRun` use case resolves attachments and creates the Issue
  Run plus a durable launch intent before the existing adapter delegates Agent
  session/turn creation to Host with provider-neutral text and image prompt
  blocks.

The capture targets the display nearest the pointer. Electron captures that
display before the transparent selection window is shown, then crops the
selected rectangle using the display scale factor. This keeps Retina/HiDPI
pixels intact while positioning the selection UI in display-independent
coordinates. The shortcut contains exactly three keys and avoids shifted
number-row symbols, whose interpretation varies by keyboard layout. Agent
metadata loads concurrently with screen capture but does not delay showing the
selector; the metadata is joined only when selection completes. The heavy
AgentGUI Composer chunk is not part of the selector's initial module graph. It
starts preloading on pointer-down so the drag interval hides most of the
transition cost without delaying the first capture frame.

The capture service retains only the most recently focused workspace identity,
not a renderer reference. If the user closes every visible Tutti window, the
next shortcut resolves that retained identity (or the daemon's startup
workspace) and asks `WorkspaceLaunch` to prepare an invisible standalone Agent
window as the workspace Engine owner. Composer metadata starts loading through
that owner concurrently with screen capture, so closing the main window does
not disable the shortcut or force a visible workspace window back onto the
screen. Successful submission may then reveal that Agent window; cancellation
leaves it hidden.

## Floating Composer

After selection, the same frameless window becomes a compact AgentGUI Composer.
The window is transparent from creation, removes outer padding that could reveal
the native background, and avoids a clipped CSS shadow at the window boundary.
The Composer window requests a `760 × 520` DIP surface and clamps that request
to the current display work area. This gives the portaled mention and Agent
menus enough usable height without allowing them to escape the native window.
Only the post-selection header is an Electron drag region, so the full-screen
selection surface stays fixed while the compact Composer can move. Editor
controls remain no-drag regions. The drag region publishes `grab`/`grabbing`
cursor feedback instead of relying on the native drag region alone. The
Composer uses its in-flow `embedded` layout; timeline-oriented dock overhang is
not valid inside the fixed native window. Portaled Composer menus receive the
header's viewport inset, so collision handling keeps Agent and mention menus
inside the usable window instead of placing them behind the title bar.
The screenshot host opts into the embedded Composer's full-height contract, so
the bordered input surface consumes all space below the draggable header while
attachments and footer controls keep their intrinsic height. The TipTap surface
and content wrappers fill that editor row as well, making the whole empty area
a native input target rather than only the placeholder line. Other embedded
hosts remain content-sized unless they make the same explicit request.

The screenshot appears as an image draft block. The user chooses an Agent,
adds or edits prompt text, and sends with the Composer button or its existing
keyboard behavior. Send creates and starts a visible Agent Session; it does not
create an Issue directly.

The bottom toolbar keeps `+`, `@`, the exact Agent Target selector, the project
selector, the **Create Task and track** switch, and the primary send action on
one alignment baseline. The project selector reuses AgentGUI's canonical
no-project/existing-project control. Its existing-project action opens the
operating system's native folder picker through the restricted capture preload.
The switch is a submit modifier, not an editor mutation. When selected, the capture
controller prepends a localized
instruction to the typed Agent prompt only at submission, while the visible
draft and transcript display prompt remain the user's own text. The instruction
requires the Agent to create a Qute Task as the work record, immediately carry
out the request, and keep the Task status and notes updated; creating the Task
is not a terminal action. This preserves one Agent execution path and does not
give the desktop capture surface a second Task workflow.

The Quick Composer's `+` control opens the operating system's native file
picker through the existing desktop file-dialog adapter. The restricted capture
preload returns only the selected local-file references, which re-enter the
canonical Composer as ordinary file mentions. The `@` control continues to use
the workspace owner rather than a capture-local reference catalog: its typed
`at.query`, `at.queryDirectory`, and `at.resolve` requests travel through the
existing workspace external bridge.

The selected Agent Target is remembered per workspace in a versioned capture UI
preference. A later capture restores the exact Target only while it remains in
the ready catalog; a deleted or unavailable Target falls back to the first ready
entry. The selected project path is remembered by a separate versioned,
workspace-scoped capture preference; choosing no project clears it. Submission
passes a non-empty selected path as the existing activation `cwd`, so the
workspace Engine and Agent Host remain the only Session lifecycle owners.
Storage failures remain non-blocking and never prevent capture. The native title
is the product name, `Tutti`, in every locale.

If Agent activation fails, the composer stays open with its draft intact so the
user can retry. Escape and the close button cancel before submission. When the
shortcut was invoked from another application on macOS, cancellation hides the
Tutti application before closing the capture window so focus returns to the
previous application instead of revealing a Tutti workspace. A shortcut
invoked from a focused Tutti window keeps Tutti active. At least one ready Agent
is required. The capture window closes only after the workspace Engine confirms
activation, then focus returns to the workspace window.

## Attachment Contract And Storage

The Issue Manager attachment contract remains available for Task workflows
created by Agents or other callers. `CreateIssueManagerIssueRequest.attachments`
accepts up to eight inline PNG,
JPEG, or WebP images. Each decoded image is limited to 20 MiB. `tuttid`
validates the declared MIME type against the file signature, allocates or
validates a UUID, and creates the restricted file exclusively so a supplied ID
can never overwrite an existing attachment.

Attachment metadata uses the existing ContextRef relationship instead of a
parallel attachment model:

- `refType` is the image MIME type;
- `path` is the daemon-managed absolute source path;
- `displayName` is shown in Issue and Task attachment sections;
- `contextRefId` is prefixed with `attachment-`.

Issue-level images are inherited by each Issue Run. Task-level image ContextRefs
are appended for task Runs. The Issue adapter projects both as
`PromptContentBlock{Type: "image"}` alongside the textual prompt. The existing
Agent prompt attachment store then copies each validated source into the
session-scoped attachment directory before provider preparation.

Deleting the Issue or Task is rejected while its explicit launch intent is
prepared or leased; after delivery resolves, deleting the Issue, Task, or
individual ContextRef removes only files inside the daemon-managed Issue
attachment root. External ContextRef paths are never deleted. Cleanup failures are returned instead of discarded. Image bytes are
staged first, then the Issue, all image ContextRefs, and topic activity are
committed in one SQLite transaction. A failed transaction removes the staged
files; a committed Issue is therefore never visible without its attachments.
Before the daemon serves requests, the file adapter reconciles the managed root
against SQLite ContextRefs and removes files orphaned before the transaction,
plus cleanup left behind by a prior delete. ContextRef removal and Run
admission share the Issue mutation lock. Automatic Runs retain a transient
attachment pin until the Agent adapter has copied each source; explicit
prepared and leased intents retain a durable pin across retry and restart.

The explicit create-and-run path atomically commits the Run and a prepared
launch intent containing stable Agent session/client-submit identities plus an
immutable prompt and attachment-path snapshot. Prepared or leased snapshots
pin their managed attachment files even if the user later edits the Issue or
removes its ContextRef. Delivery first leases that intent. A confirmed delivery
marks it dispatched; authoritative evidence that no Agent turn started settles
the intent, Run, task projection, Issue projection, and topic activity in one
transaction. An unknown delivery result releases it to prepared instead of
falsely failing the task. Startup and periodic workspace recovery re-deliver
the snapshot with the same identities, so Agent Host can reconcile or
deduplicate the attempt. The same recovery pass retries automatic Issue
dispatch after transient ContextRef reads. If a parallel pass already committed
earlier Runs before a later lookup fails, those Runs are still published and
delivered before the error is retried, so no running claim is stranded.

The plan-materialization request uses a separate issue schema without inline
attachments. This prevents the API from accepting attachment bytes that the
atomic Issue-and-task materializer does not own.

## Open-Source Component Evaluation

The implementation uses Electron's built-in APIs and adds no screenshot
dependency:

- [Electron `globalShortcut`](https://www.electronjs.org/docs/latest/api/global-shortcut)
  provides operating-system shortcuts while Tutti is unfocused.
- [Electron `desktopCapturer`](https://www.electronjs.org/docs/latest/api/desktop-capturer)
  returns display sources and full-resolution thumbnails. `NativeImage.crop`
  performs the trusted main-process crop.

The following open-source options were evaluated:

| Option                                                                                                                | Strength                                                                                  | Reason not selected                                                                                                                           |
| --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| [`nashaofu/screenshots`](https://github.com/nashaofu/screenshots) (`electron-screenshots` + `react-screenshots`, MIT) | Ready-made region editor with arrows, shapes, text, mosaic, and i18n                      | Brings a second React editing surface plus `node-screenshots`; this first slice needs selection and Qute composition, not annotation tooling  |
| [`nashaofu/node-screenshots`](https://github.com/nashaofu/node-screenshots) (MIT)                                     | Native cross-platform capture, including macOS, Windows, X11, and Wayland implementations | Native/Rust binding increases packaging, ABI, signing, and architecture verification work while Electron already supplies the required pixels |
| [`bencevans/screenshot-desktop`](https://github.com/bencevans/screenshot-desktop) (MIT)                               | Small Promise API for full displays                                                       | It captures whole displays rather than owning region selection, and its Linux path requires an external screenshot utility                    |

`nashaofu/screenshots` is the preferred follow-up if Qute later needs rich
annotation before submission. For the current interaction, Electron primitives
keep the capture path inside the already shipped runtime and let the floating
composer reuse Tutti's UI System.

## Platform Notes

- macOS requires Screen Recording consent for desktop capture.
- On Linux, Electron documents that PipeWire may expose only one selected
  source. Wayland global shortcuts additionally depend on the environment's
  shortcut portal support.
- Global shortcut registration can fail when another application owns the same
  accelerator. The desktop logs that failure; making the accelerator a durable
  user preference is a separate settings slice.
- Region selection currently stays within the display nearest the pointer when
  the shortcut is pressed; it does not span display boundaries.
