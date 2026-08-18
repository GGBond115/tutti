import type { TranslateFn } from "../../../i18n/index";

export function agentSlashPaletteLabels(t: TranslateFn) {
  return {
    slashCommandPalette: t("agentHost.agentGui.slashCommandPalette"),
    skillPickerPalette: t("agentHost.agentGui.skillPickerPalette"),
    slashPaletteCommandsGroup: t(
      "agentHost.agentGui.slashPaletteCommandsGroup"
    ),
    slashPaletteCapabilitiesGroup: t(
      "agentHost.agentGui.slashPaletteCapabilitiesGroup"
    ),
    slashPaletteCapabilitiesLoading: t(
      "agentHost.agentGui.slashPaletteCapabilitiesLoading"
    ),
    slashPaletteSkillsGroup: t("agentHost.agentGui.slashPaletteSkillsGroup"),
    slashPalettePluginsGroup: t("agentHost.agentGui.slashPalettePluginsGroup"),
    slashPaletteMcpGroup: t("agentHost.agentGui.slashPaletteMcpGroup")
  };
}
