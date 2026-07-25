import type { WorkspaceSummary } from "@tutti-os/client-tuttid-ts";
import { useServiceSnapshot } from "../bindings/useServiceSnapshot";
import type { ConnectedDevice } from "../services/deviceService";
import type { MobileApplicationService } from "../services/mobileApplicationService";
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
  service
}: {
  application: MobileApplicationService;
  device: ConnectedDevice;
  service: WorkspaceActivityService;
}) {
  const model = useServiceSnapshot(service);
  return (
    <ConversationWorkspaceView
      deviceName={device.name}
      model={model}
      onBack={() => application.showWorkspacePicker()}
      onDraftChange={(value) => service.setDraft(value)}
      onDeleteSession={(id) => service.deleteSession(id)}
      onLoadOlder={() => void service.loadOlderMessages()}
      onLoadMoreSessions={(sectionId) =>
        void service.loadMoreSessions(sectionId)
      }
      onRefreshSessions={() => service.refreshSessions()}
      onNewSession={() => service.startCreating()}
      onRenameSession={(id, title) => service.renameSession(id, title)}
      onRespond={(interaction, input) =>
        service.respondToInteraction(interaction, input)
      }
      onSelectSession={(id) => service.selectSession(id)}
      onSelectTarget={(id) => service.selectTarget(id)}
      onSend={() => void service.send()}
      onStop={() => service.stop()}
      onTogglePinned={(id) => service.toggleSessionPinned(id)}
      workspace={service.workspace}
    />
  );
}
