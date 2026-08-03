import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const nodeBodySource = readFileSync(
  new URL(
    "../../workspace-agent/ui/DesktopAgentGUIWorkbenchBody.tsx",
    import.meta.url
  ),
  "utf8"
);
const workspaceSource = readFileSync(
  new URL(
    "../../workspace-workbench/ui/WorkspaceWorkbench.tsx",
    import.meta.url
  ),
  "utf8"
);
const workspaceChromeSource = readFileSync(
  new URL("../../workspace-workbench/ui/WorkspaceChrome.tsx", import.meta.url),
  "utf8"
);
const standaloneSource = readFileSync(
  new URL(
    "../../workspace-workbench/ui/StandaloneAgentWindow.tsx",
    import.meta.url
  ),
  "utf8"
);
const standalonePanelHostsSource = readFileSync(
  new URL(
    "../../workspace-workbench/ui/StandaloneAgentWindowPanelHosts.tsx",
    import.meta.url
  ),
  "utf8"
);

test("activity replay binding is owned by the bootstrapped workspace renderer", () => {
  assert.doesNotMatch(nodeBodySource, /AgentSessionActivityReplayBinding/);
  assert.equal(bindingMountCount(workspaceSource), 1);
  assert.equal(bindingMountCount(standaloneSource), 0);
  assert.doesNotMatch(
    standaloneSource,
    /AgentSessionReplayWorkspace(?:Coordinator|Provider)/
  );
});

test("workspace replay machinery mounts only in the isolated replay runtime", () => {
  // Coordinator construction and every replay binding hang off the
  // synchronous replay-runtime flag; normal windows mount none of it.
  assert.match(workspaceSource, /isAgentSessionReplayRuntime\?\.\(\)/);
  assert.match(
    workspaceSource,
    /\{replayWorkspaceCoordinator \? \(\s*<WorkspaceAgentSessionActivityReplayBinding\b/
  );
  assert.match(
    workspaceSource,
    /\{replayWorkspaceCoordinator && workbenchHost \? \(\s*<AgentSessionReplayWorkspaceBinding\b/
  );
  assert.match(
    workspaceSource,
    /replayRuntimeActive\s*\?\s*new AgentSessionReplayWorkspaceCoordinator\(/
  );
  assert.equal(
    workspaceSource.match(/new AgentSessionReplayWorkspaceCoordinator\(/g)
      ?.length,
    1
  );
});

test("workspace replay runtime suppresses the external Agent import prompt", () => {
  assert.match(
    workspaceSource,
    /externalAgentSessionImportPromptEnabled=\{!replayRuntimeActive\}/
  );
  assert.match(
    workspaceChromeSource,
    /\{externalAgentSessionImportPromptEnabled \? \(\s*<ExternalAgentSessionImportPrompt\b/
  );
  assert.equal(
    workspaceChromeSource.match(/<ExternalAgentSessionImportPrompt\b/g)?.length,
    1
  );
  assert.match(
    standalonePanelHostsSource,
    /<ExternalAgentSessionImportPrompt\b/
  );
});

function bindingMountCount(source: string): number {
  return (
    source.match(/<WorkspaceAgentSessionActivityReplayBinding\b/g)?.length ?? 0
  );
}
