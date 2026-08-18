import { Button, Input, SectionTabs } from "@tutti-os/ui-system/components";
import { RefreshIcon, SearchIcon } from "@tutti-os/ui-system/icons";

import { useConnectorMarketServices } from "../ConnectorMarketServicesContext.tsx";

export function ConnectorMarketToolbar() {
  const { i18n, model, snapshot } = useConnectorMarketServices();
  const ui = snapshot.ui;
  const marketView = snapshot.view;
  const showCounts =
    marketView.status === "ready" || marketView.status === "empty";

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <label className="relative min-w-0 flex-1">
          <span className="sr-only">{i18n.t("searchLabel")}</span>
          <SearchIcon className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-[var(--text-tertiary)]" />
          <Input
            className="pl-8"
            placeholder={i18n.t("searchPlaceholder")}
            value={ui.query}
            onChange={(event) =>
              model.commands.setQuery(event.currentTarget.value)
            }
          />
        </label>
        <Button
          aria-label={i18n.t("actionRefresh")}
          disabled={marketView.refreshing}
          size="sm"
          type="button"
          variant="secondary"
          onClick={() =>
            void model.commands.refreshCatalog().catch(() => undefined)
          }
        >
          <RefreshIcon
            className={marketView.refreshing ? "animate-spin" : undefined}
          />
          {i18n.t("actionRefresh")}
        </Button>
      </div>
      <SectionTabs
        ariaLabel={i18n.t("title")}
        tabs={[
          {
            ...(showCounts ? { count: marketView.installedCount } : {}),
            label: i18n.t("installedTab"),
            value: "installed" as const
          },
          {
            ...(showCounts ? { count: marketView.availableCount } : {}),
            label: i18n.t("availableTab"),
            value: "available" as const
          }
        ]}
        value={ui.segment}
        onValueChange={(segment) => model.commands.selectSegment(segment)}
      />
    </div>
  );
}
