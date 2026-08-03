import { InstantiationService, ServiceCollection } from "@tutti-os/infra/di";
import { createMobileServicePorts } from "./native/createMobileServicePorts";
import { createMobileThemePreferencePort } from "./native/mobileThemePreferencePort";
import { MobileApplicationService } from "./services/mobileApplicationService";
import { IMobileApplicationService } from "./services/mobileServiceIdentifiers";
import { MobileThemePreferenceService } from "./services/mobileThemePreferenceService";

const rootServices = new ServiceCollection();
const rootContainer = new InstantiationService(rootServices);

export const mobileApplicationService = new MobileApplicationService(
  rootContainer,
  createMobileServicePorts()
);
export const mobileThemePreferenceService = new MobileThemePreferenceService(
  createMobileThemePreferencePort()
);
rootServices.set(IMobileApplicationService, mobileApplicationService);

void mobileApplicationService.start();
