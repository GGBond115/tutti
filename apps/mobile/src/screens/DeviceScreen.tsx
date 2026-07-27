import { useState } from "react";
import { useServiceSnapshot } from "../bindings/useServiceSnapshot";
import type { DeviceService } from "../services/deviceService";
import type { AccountSession } from "../services/mobileDomain";
import { DeviceScreenView } from "./DeviceScreenView";

export function DeviceScreen({
  onSignOut,
  service,
  session
}: {
  onSignOut(): Promise<void>;
  service: DeviceService;
  session: AccountSession;
}) {
  const model = useServiceSnapshot(service);
  const [manualPairingCode, setManualPairingCode] = useState("");
  const [manualPairingOpen, setManualPairingOpen] = useState(false);

  const submitManualPairingCode = async () => {
    if (await service.pairWithCode(manualPairingCode)) {
      setManualPairingCode("");
      setManualPairingOpen(false);
    }
  };

  return (
    <DeviceScreenView
      manualPairingCode={manualPairingCode}
      manualPairingOpen={manualPairingOpen}
      model={model}
      onConnect={(pairing, device) => void service.connect(pairing, device)}
      onManualPairingCodeChange={setManualPairingCode}
      onManualPairingOpen={() => setManualPairingOpen(true)}
      onManualPairingSubmit={() => void submitManualPairingCode()}
      onRefresh={() => void service.refresh()}
      onScanPairingCode={() => void service.scanAndPair()}
      onSignOut={() => void onSignOut()}
      accountName={session.name}
    />
  );
}
