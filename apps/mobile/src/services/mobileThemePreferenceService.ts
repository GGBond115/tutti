import { ObservableService } from "./observableService";

export type MobileThemePreference = "system" | "light" | "dark";

export interface MobileThemePreferencePort {
  applyNativeColorScheme(preference: MobileThemePreference): void;
  loadThemePreference(): unknown;
  saveThemePreference(preference: MobileThemePreference): Promise<void>;
}

export interface MobileThemePreferenceSnapshot {
  preference: MobileThemePreference;
}

export class MobileThemePreferenceService extends ObservableService<MobileThemePreferenceSnapshot> {
  private snapshot: MobileThemePreferenceSnapshot;
  private mutationGeneration = 0;

  constructor(private readonly port: MobileThemePreferencePort) {
    super();
    let storedPreference: unknown;
    try {
      storedPreference = port.loadThemePreference();
    } catch {
      storedPreference = "system";
    }
    const preference = normalizeMobileThemePreference(storedPreference);
    this.snapshot = { preference };
    try {
      this.port.applyNativeColorScheme(preference);
    } catch {
      // The controlled Native theme still applies even if the platform override fails.
    }
  }

  getSnapshot = (): MobileThemePreferenceSnapshot => this.snapshot;

  async setPreference(preference: MobileThemePreference): Promise<void> {
    if (preference === this.snapshot.preference) return;
    const previousPreference = this.snapshot.preference;
    const generation = ++this.mutationGeneration;
    this.publish(preference);

    try {
      this.port.applyNativeColorScheme(preference);
      await this.port.saveThemePreference(preference);
    } catch (error) {
      if (generation === this.mutationGeneration) {
        this.publish(previousPreference);
        try {
          this.port.applyNativeColorScheme(previousPreference);
        } catch {
          // The controlled Native theme has already rolled back.
        }
      }
      throw error;
    }
  }

  private publish(preference: MobileThemePreference): void {
    this.snapshot = { preference };
    this.emitChange();
  }
}

export function normalizeMobileThemePreference(
  value: unknown
): MobileThemePreference {
  return value === "light" || value === "dark" || value === "system"
    ? value
    : "system";
}
