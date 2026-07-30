import { type JSX } from "react";
import type {
  AgentProviderRuntimeCandidate,
  AgentProviderRuntimeCatalogResponse
} from "@tutti-os/client-tuttid-ts";
import { LoadingIcon, RadioIndicator } from "@tutti-os/ui-system";
import type { useTranslation } from "@renderer/i18n";

type Translate = ReturnType<typeof useTranslation>["t"];

function sourceLabel(source: string, t: Translate): string {
  switch (source) {
    case "bun_global":
      return t("workspace.agentEnv.runtimeSourceBun");
    case "pnpm_global":
      return t("workspace.agentEnv.runtimeSourcePnpm");
    case "npm_global":
      return t("workspace.agentEnv.runtimeSourceNpm");
    case "homebrew":
      return t("workspace.agentEnv.runtimeSourceHomebrew");
    default:
      return t("workspace.agentEnv.runtimeSourcePath");
  }
}

// The source (Homebrew, PATH, Bun global…) is the most human-meaningful way to
// tell two installations apart, so it leads the row. Two installs frequently
// share a version, which is why the version alone cannot disambiguate them.
function sourceSummary(
  candidate: AgentProviderRuntimeCandidate,
  t: Translate
): string {
  const labels = candidate.sources.map((source) => sourceLabel(source, t));
  const unique = labels.filter(
    (label, index) => labels.indexOf(label) === index
  );
  return unique.length > 0
    ? unique.join(" · ")
    : t("workspace.agentEnv.runtimeSourcePath");
}

function candidateSummary(
  candidate: AgentProviderRuntimeCandidate,
  t: Translate
): string {
  return [
    sourceSummary(candidate, t),
    candidate.version ?? t("workspace.agentEnv.valueUnknown"),
    candidate.launcherPath
  ].join(" · ");
}

export function CodexRuntimePicker({
  catalog,
  loading,
  pendingCandidateId,
  selectionError,
  onSelect,
  t
}: {
  catalog: AgentProviderRuntimeCatalogResponse | null;
  loading: boolean;
  pendingCandidateId: string | null;
  selectionError: string | null;
  onSelect(candidateId: string): void;
  t: Translate;
}): JSX.Element | null {
  if (!catalog) {
    return loading ? (
      <p className="m-0 text-[13px] text-[var(--text-secondary)]">
        {t("workspace.agentEnv.runtimeDiscovering")}
      </p>
    ) : selectionError ? (
      <p className="m-0 text-[12px] text-[var(--state-danger)]">
        {selectionError}
      </p>
    ) : null;
  }

  const readyCandidates = catalog.candidates.filter(
    (candidate) => candidate.state === "ready"
  );
  const unavailableCandidates = catalog.candidates.filter(
    (candidate) => candidate.state !== "ready"
  );
  const selectionNeedsInput =
    catalog.selection.state === "selection_required" ||
    catalog.selection.state === "stale";
  const canChangeSelection = readyCandidates.length > 1;
  if (!selectionNeedsInput && !canChangeSelection) {
    return null;
  }

  return (
    <section className="rounded-[8px] border border-[var(--border-1)] bg-[var(--transparency-block)] p-3">
      <div className="flex flex-col gap-1">
        <strong className="text-[13px] font-semibold text-[var(--text-primary)]">
          {t("workspace.agentEnv.runtimeTitle")}
        </strong>
        <p className="m-0 text-[12px] leading-5 text-[var(--text-secondary)]">
          {selectionNeedsInput
            ? catalog.selection.state === "stale"
              ? t("workspace.agentEnv.runtimeStaleDescription")
              : t("workspace.agentEnv.runtimeSelectionRequiredDescription")
            : t("workspace.agentEnv.runtimeChangeDescription")}
        </p>
      </div>

      <div className="mt-3 flex flex-col gap-2">
        {readyCandidates.map((candidate) => {
          const selected = catalog.selection.candidateId === candidate.id;
          const selecting = pendingCandidateId === candidate.id;
          return (
            <button
              key={candidate.id}
              type="button"
              aria-pressed={selected}
              disabled={pendingCandidateId !== null}
              onClick={() => onSelect(candidate.id)}
              className="flex w-full items-start gap-2 rounded-[6px] border border-[var(--border-1)] bg-[var(--background-fronted)] px-2.5 py-2 text-left transition-colors hover:border-[var(--tutti-purple)] disabled:cursor-not-allowed disabled:opacity-60"
            >
              {selecting ? (
                <LoadingIcon className="mt-0.5 size-4 shrink-0 animate-spin text-[var(--tutti-purple)]" />
              ) : (
                <RadioIndicator checked={selected} className="mt-0.5" />
              )}
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] font-medium text-[var(--text-primary)]">
                  <span>{sourceSummary(candidate, t)}</span>
                  <span className="text-[11px] font-normal text-[var(--text-secondary)]">
                    {candidate.version ?? t("workspace.agentEnv.valueUnknown")}
                  </span>
                  {selected ? (
                    <span className="text-[var(--tutti-purple)]">
                      {t("workspace.agentEnv.runtimeSelected")}
                    </span>
                  ) : null}
                </span>
                {/* Full launcher path — not truncated — so two same-version
                    installs remain distinguishable. */}
                <span
                  title={candidate.launcherPath}
                  className="mt-0.5 block break-all text-[11px] text-[var(--text-secondary)]"
                >
                  {candidate.launcherPath}
                </span>
              </span>
            </button>
          );
        })}
      </div>

      {unavailableCandidates.length > 0 ? (
        <div className="mt-3 border-t border-[var(--border-1)] pt-2">
          <span className="text-[11px] font-medium text-[var(--text-secondary)]">
            {t("workspace.agentEnv.runtimeUnavailableTitle")}
          </span>
          <div className="mt-1.5 flex flex-col gap-1">
            {unavailableCandidates.map((candidate) => (
              <span
                key={candidate.id}
                title={candidate.launcherPath}
                className="block break-all text-[11px] text-[var(--text-secondary)]"
              >
                {candidateSummary(candidate, t)}
                {candidate.reasonCode ? ` · ${candidate.reasonCode}` : ""}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {selectionError ? (
        <p className="mb-0 mt-2 text-[12px] text-[var(--state-danger)]">
          {selectionError}
        </p>
      ) : null}
    </section>
  );
}
