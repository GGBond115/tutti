import type { WorkspaceSummary } from "@tutti-os/client-tuttid-ts";
import { useServiceSnapshot } from "../bindings/useServiceSnapshot";
import type { ConnectedDevice } from "../services/deviceService";
import type { MobileApplicationService } from "../services/mobileApplicationService";
import type { MobileQuickPromptLibraryService } from "../services/mobileQuickPromptLibraryService";
import type { WorkspaceActivityService } from "../services/workspaceActivityService";
import type { WorkspaceCatalogService } from "../services/workspaceCatalogService";
import {
  ConversationWorkspaceView,
  WorkspacePickerView
} from "./WorkspaceScreenView";

export function WorkspaceScreen({
  application,
  device,
  workspace
}: {
  application: MobileApplicationService;
  device: ConnectedDevice;
  workspace: WorkspaceSummary | null;
}) {
  return workspace ? (
    <ConversationBinding
      application={application}
      device={device}
      quickPrompts={application.quickPromptLibraryService!}
      service={application.workspaceActivityService!}
    />
  ) : (
    <WorkspacePickerBinding
      application={application}
      device={device}
      service={application.workspaceCatalogService!}
    />
  );
}

function WorkspacePickerBinding({
  application,
  device,
  service
}: {
  application: MobileApplicationService;
  device: ConnectedDevice;
  service: WorkspaceCatalogService;
}) {
  const model = useServiceSnapshot(service);
  return (
    <WorkspacePickerView
      deviceName={device.name}
      model={model}
      onDisconnect={() => void application.disconnectDevice()}
      onRetry={() => void service.load()}
      onSelect={(workspace) => void application.selectWorkspace(workspace)}
    />
  );
}

function ConversationBinding({
  application,
  device,
  quickPrompts,
  service
}: {
  application: MobileApplicationService;
  device: ConnectedDevice;
  quickPrompts: MobileQuickPromptLibraryService;
  service: WorkspaceActivityService;
}) {
  const model = useServiceSnapshot(service);
  const media = useServiceSnapshot(service.media);
  const quickPromptLibrary = useServiceSnapshot(quickPrompts);
  return (
    <ConversationWorkspaceView
      deviceName={device.name}
      media={media}
      model={model}
      onBack={() => application.showWorkspacePicker()}
      onDraftChange={(value) => service.setDraft(value)}
      onDeleteSession={(id) => service.deleteSession(id)}
      onLoadOlder={() => void service.loadOlderMessages()}
      onLoadMoreSessions={(sectionId) =>
        void service.loadMoreSessions(sectionId)
      }
      onRefreshSessions={() => service.refreshSessions()}
      onRefreshQuickPrompts={() => quickPrompts.refresh()}
      onNewSession={() => service.startCreating()}
      onRenameSession={(id, title) => service.renameSession(id, title)}
      onRespond={(interaction, input) =>
        service.respondToInteraction(interaction, input)
      }
      onSelectSession={(id) => service.selectSession(id)}
      onSelectTarget={(id) => service.selectTarget(id)}
      onSend={() => void service.send()}
      onStop={() => service.stop()}
      onUpdateComposerSettings={(settings) =>
        service.updateComposerSettings(settings)
      }
      onTogglePinned={(id) => service.toggleSessionPinned(id)}
      quickPromptLibrary={quickPromptLibrary}
      workspace={service.workspace}
    />
  );
}
