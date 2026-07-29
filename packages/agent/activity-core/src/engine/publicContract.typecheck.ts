import type {
  AgentSessionEngine,
  AgentSessionEngineState,
  EngineExtensionCommand,
  EngineIntent
} from "../index.ts";

type Assert<T extends true> = T;
type IsNever<T> = [T] extends [never] ? true : false;

export type PromptExecutionIntentRemainsPrivate = Assert<
  IsNever<Extract<EngineIntent, { type: "prompt/executionRequested" }>>
>;

export type PromptExecutionStateRemainsPrivate = Assert<
  "promptExecutions" extends keyof AgentSessionEngineState ? false : true
>;

export type RenameRemainsTypedEffect = Assert<
  IsNever<Extract<EngineExtensionCommand, { type: "session/rename" }>>
>;

type SessionMutationOperation =
  | "deleteSessions"
  | "renameSession"
  | "setSessionPinned";

export type SessionMutationsUseSemanticEngineOperations = Assert<
  SessionMutationOperation extends keyof AgentSessionEngine ? true : false
>;
