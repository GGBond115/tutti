import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentGUIAgentAvatarPresentation } from "./model/agentGuiAgentAvatarPresentation";

const sceneCreate = vi.hoisted(() => vi.fn());

vi.mock("./agentGuiHeroCarouselScene", () => ({
  AgentGuiHeroCarouselScene: {
    create: sceneCreate
  }
}));

import { AgentGUIHeroAgentCarousel } from "./AgentGUIHeroAgentCarousel";

const item: AgentGUIAgentAvatarPresentation = {
  agentTargetId: "agent-a",
  iconUrl: "agent-a.png",
  label: "Agent A",
  provider: "codex",
  targetId: "target-a"
};

function installAnimationFrameQueue(): {
  flushNext(): void;
  pendingIDs(): number[];
} {
  let nextID = 1;
  const callbacks = new Map<number, FrameRequestCallback>();
  vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
    const id = nextID;
    nextID += 1;
    callbacks.set(id, callback);
    return id;
  });
  vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
    callbacks.delete(id);
  });
  return {
    flushNext() {
      const first = callbacks.entries().next().value as
        | [number, FrameRequestCallback]
        | undefined;
      if (!first) {
        throw new Error("Expected a pending animation frame");
      }
      callbacks.delete(first[0]);
      first[1](0);
    },
    pendingIDs() {
      return Array.from(callbacks.keys());
    }
  };
}

function installIdleCallbackQueue(): {
  flushNext(): void;
  pendingIDs(): number[];
} {
  let nextID = 1;
  const callbacks = new Map<number, IdleRequestCallback>();
  vi.stubGlobal(
    "requestIdleCallback",
    (callback: IdleRequestCallback): number => {
      const id = nextID;
      nextID += 1;
      callbacks.set(id, callback);
      return id;
    }
  );
  vi.stubGlobal("cancelIdleCallback", (id: number) => {
    callbacks.delete(id);
  });
  return {
    flushNext() {
      const first = callbacks.entries().next().value as
        | [number, IdleRequestCallback]
        | undefined;
      if (!first) {
        throw new Error("Expected a pending idle callback");
      }
      callbacks.delete(first[0]);
      first[1]({
        didTimeout: false,
        timeRemaining: () => 8
      });
    },
    pendingIDs() {
      return Array.from(callbacks.keys());
    }
  };
}

afterEach(() => {
  sceneCreate.mockReset();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("AgentGUIHeroAgentCarousel", () => {
  it("creates WebGL only after the static Hero has painted", async () => {
    const frames = installAnimationFrameQueue();
    const idle = installIdleCallbackQueue();
    vi.stubGlobal("Image", undefined);
    sceneCreate.mockReturnValue({
      dispose: vi.fn(),
      moveTo: vi.fn(),
      setSize: vi.fn()
    });
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);

    try {
      await act(async () => {
        root.render(<AgentGUIHeroAgentCarousel items={[item]} />);
      });

      expect(container.querySelector("canvas")).not.toBeNull();
      expect(sceneCreate).not.toHaveBeenCalled();

      await act(async () => {
        frames.flushNext();
      });
      expect(sceneCreate).not.toHaveBeenCalled();

      await act(async () => {
        frames.flushNext();
      });
      expect(sceneCreate).not.toHaveBeenCalled();

      await act(async () => {
        idle.flushNext();
      });
      expect(sceneCreate).toHaveBeenCalledOnce();
    } finally {
      await act(async () => {
        root.unmount();
      });
      container.remove();
    }
  });

  it("cancels the deferred WebGL creation on unmount", async () => {
    const frames = installAnimationFrameQueue();
    installIdleCallbackQueue();
    vi.stubGlobal("Image", undefined);
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);

    await act(async () => {
      root.render(<AgentGUIHeroAgentCarousel items={[item]} />);
    });
    await act(async () => {
      frames.flushNext();
    });
    const pendingFrameID = frames.pendingIDs()[0];

    await act(async () => {
      root.unmount();
    });

    expect(window.cancelAnimationFrame).toHaveBeenCalledWith(pendingFrameID);
    expect(sceneCreate).not.toHaveBeenCalled();
    container.remove();
  });

  it("waits for host presentation visibility before scheduling WebGL", async () => {
    const frames = installAnimationFrameQueue();
    const idle = installIdleCallbackQueue();
    vi.stubGlobal("Image", undefined);
    sceneCreate.mockReturnValue({
      dispose: vi.fn(),
      moveTo: vi.fn(),
      setSize: vi.fn()
    });
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);

    try {
      await act(async () => {
        root.render(
          <AgentGUIHeroAgentCarousel isVisible={false} items={[item]} />
        );
      });
      await act(async () => {
        frames.flushNext();
        frames.flushNext();
      });

      expect(idle.pendingIDs()).toHaveLength(0);
      expect(sceneCreate).not.toHaveBeenCalled();

      await act(async () => {
        root.render(<AgentGUIHeroAgentCarousel isVisible items={[item]} />);
      });
      expect(idle.pendingIDs()).toHaveLength(1);
      expect(sceneCreate).not.toHaveBeenCalled();

      await act(async () => {
        idle.flushNext();
      });
      expect(sceneCreate).toHaveBeenCalledOnce();
    } finally {
      await act(async () => {
        root.unmount();
      });
      container.remove();
    }
  });

  it("pauses and resumes an existing WebGL scene with host visibility", async () => {
    const frames = installAnimationFrameQueue();
    const idle = installIdleCallbackQueue();
    vi.stubGlobal("Image", undefined);
    const scene = {
      dispose: vi.fn(),
      moveTo: vi.fn(),
      setRecordSpinActive: vi.fn(),
      setSize: vi.fn(),
      setVisible: vi.fn()
    };
    sceneCreate.mockReturnValue(scene);
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);

    try {
      await act(async () => {
        root.render(<AgentGUIHeroAgentCarousel isVisible items={[item]} />);
      });
      await act(async () => {
        frames.flushNext();
        frames.flushNext();
        idle.flushNext();
      });
      expect(sceneCreate).toHaveBeenCalledOnce();

      await act(async () => {
        root.render(
          <AgentGUIHeroAgentCarousel isVisible={false} items={[item]} />
        );
      });
      expect(scene.setVisible).toHaveBeenLastCalledWith(false);

      await act(async () => {
        root.render(<AgentGUIHeroAgentCarousel isVisible items={[item]} />);
      });
      expect(scene.setVisible).toHaveBeenLastCalledWith(true);
      expect(sceneCreate).toHaveBeenCalledOnce();
      expect(scene.dispose).not.toHaveBeenCalled();
    } finally {
      await act(async () => {
        root.unmount();
      });
      container.remove();
    }
  });

  it("keeps the scene mounted while focus controls record spin", async () => {
    const frames = installAnimationFrameQueue();
    const idle = installIdleCallbackQueue();
    vi.stubGlobal("Image", undefined);
    const scene = {
      dispose: vi.fn(),
      moveTo: vi.fn(),
      setRecordSpinActive: vi.fn(),
      setSize: vi.fn(),
      setVisible: vi.fn()
    };
    sceneCreate.mockReturnValue(scene);
    const container = document.createElement("div");
    document.body.append(container);
    const root = createRoot(container);

    try {
      await act(async () => {
        root.render(
          <AgentGUIHeroAgentCarousel
            isActive={false}
            isVisible
            items={[item]}
          />
        );
      });
      await act(async () => {
        frames.flushNext();
        frames.flushNext();
        idle.flushNext();
      });
      expect(sceneCreate).toHaveBeenCalledWith(
        expect.objectContaining({ recordSpinActive: false })
      );

      await act(async () => {
        root.render(
          <AgentGUIHeroAgentCarousel isActive isVisible items={[item]} />
        );
      });
      expect(scene.setRecordSpinActive).toHaveBeenLastCalledWith(true);
      expect(sceneCreate).toHaveBeenCalledOnce();
      expect(scene.dispose).not.toHaveBeenCalled();
    } finally {
      await act(async () => {
        root.unmount();
      });
      container.remove();
    }
  });
});
