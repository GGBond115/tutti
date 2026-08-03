import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger
} from "./context-menu";

describe("ContextMenu", () => {
  it("supports nested action groups", async () => {
    render(
      <ContextMenu>
        <ContextMenuTrigger>
          <span data-testid="nested-target">target</span>
        </ContextMenuTrigger>
        <ContextMenuContent>
          <ContextMenuSub>
            <ContextMenuSubTrigger>Copy</ContextMenuSubTrigger>
            <ContextMenuSubContent>
              <ContextMenuItem>Copy link</ContextMenuItem>
            </ContextMenuSubContent>
          </ContextMenuSub>
        </ContextMenuContent>
      </ContextMenu>
    );

    fireEvent.contextMenu(screen.getByTestId("nested-target"));
    const copy = await screen.findByRole("menuitem", { name: "Copy" });
    fireEvent.keyDown(copy, { key: "ArrowRight" });

    expect(
      await screen.findByRole("menuitem", { name: "Copy link" })
    ).toBeInTheDocument();
  });
});
