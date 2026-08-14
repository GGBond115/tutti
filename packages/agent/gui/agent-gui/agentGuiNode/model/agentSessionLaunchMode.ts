export type AgentGUISessionLaunchMode = "local" | "worktree" | "cloud";

/** Durable project preference; Cloud is selected through an exact Agent target. */
export type AgentGUISessionLaunchPreferenceMode = Exclude<
  AgentGUISessionLaunchMode,
  "cloud"
>;
