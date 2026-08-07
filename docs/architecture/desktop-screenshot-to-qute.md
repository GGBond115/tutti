# Desktop Screenshot To Qute

The desktop can turn an arbitrary rectangular screen region into a Qute Issue
with a durable image attachment. The default global shortcut is
`CommandOrControl+Shift+S`, and it works while another application has focus as
long as Tutti is running and has a workspace window to target.

## Ownership And Flow

The feature follows the existing desktop/daemon boundary:

```text
Electron main process
  global shortcut -> display capture -> trusted selection crop
        |
        v
restricted capture preload + renderer
  region selection -> note/topic/Agent composer
        |
        v
tuttid Issue Manager API
  create Issue + persist image ContextRef
        |
        +-- create only
        |
        `-- start Run -> Agent Host prompt with text + image block
```

- `apps/desktop` owns the operating-system shortcut, screen capture, crop,
  floating window, and narrow IPC bridge. The renderer never receives the
  daemon client and cannot submit arbitrary image bytes; the main process keeps
  the selected image and accepts only the note, topic, Agent target, and action.
- `packages/workspace/issue-manager` owns the reusable Issue Manager contract,
  attachment presentation, and localized labels.
- `services/tuttid/service/workspace` owns validation, ContextRef lifecycle,
  and projection into an Issue Run launch. `services/tuttid/data/workspace`
  owns the managed attachment-file persistence adapter. The tuttid-local
  immutable launch snapshot and codec live in `services/tuttid/biz/workspace`
  so service and data share an explicit contract without exposing host paths
  through reusable workspace packages.
- `packages/agent/host` remains the lifecycle owner. The explicit
  `startWorkspaceIssueRun` use case resolves attachments and creates the Issue
  Run plus a durable launch intent before the existing adapter delegates Agent
  session/turn creation to Host with provider-neutral text and image prompt
  blocks.

The capture targets the display nearest the pointer. Electron captures that
display before the transparent selection window is shown, then crops the
selected rectangle using the display scale factor. This keeps Retina/HiDPI
pixels intact while positioning the selection UI in display-independent
coordinates. The shortcut contains exactly three keys and avoids shifted
number-row symbols, whose interpretation varies by keyboard layout. Topic and
Agent metadata load concurrently with screen capture but do not delay showing
the selector; the metadata is joined only when selection completes.

## Floating Composer

After selection, the same frameless window becomes a compact composer. It uses
UI System `Textarea`, `Select`, and `Button` primitives, semantic color tokens,
and the Agent composer typography and spacing conventions.

The composer always creates the Issue first and then offers two outcomes:

- **Create task** saves the screenshot, optional note, and selected topic.
- **Create and run** also starts an Issue Run with the selected Agent. The
  keyboard equivalent is Command/Ctrl+Enter.

If Agent launch fails after the Issue was saved, the composer retains the
created Issue ID. A retry starts a Run against that Issue instead of creating a
duplicate Issue. Escape cancels before submission. A workspace must already
have a topic, and at least one ready Agent is required only for the
create-and-run path.

## Attachment Contract And Storage

`CreateIssueManagerIssueRequest.attachments` accepts up to eight inline PNG,
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
