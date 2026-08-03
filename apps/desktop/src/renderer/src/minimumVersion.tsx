import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MinimumVersionUpgradeApp } from "@tutti-os/desktop-update-admission/react";
import { createMinimumVersionAdmissionI18nRuntime } from "@tutti-os/desktop-update-admission/i18n";
import { createMinimumVersionUpgradeWindowContainer } from "./app/windows/minimumUpgrade/createMinimumVersionUpgradeWindowContainer.ts";
import { getActiveLocale } from "./i18n";
import "./style.css";

const root = document.querySelector<HTMLDivElement>("#app");
if (!root) {
  throw new Error("Minimum-version renderer root '#app' was not found.");
}
const container = createMinimumVersionUpgradeWindowContainer();
const mode =
  new URLSearchParams(window.location.search).get("mode") === "foreground"
    ? "foreground"
    : "startup";
createRoot(root).render(
  <StrictMode>
    <MinimumVersionUpgradeApp
      i18n={createMinimumVersionAdmissionI18nRuntime(getActiveLocale())}
      mode={mode}
      port={container.port}
      productName="Tutti"
    />
  </StrictMode>
);
