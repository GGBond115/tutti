import type { TuttidClient } from "@tutti-os/client-tuttid-ts";
import { MobileUserProjectDirectoryService } from "./mobileUserProjectDirectoryService";

test("retains the last project catalog when a refresh fails", async () => {
  const project = {
    createdAtUnixMs: 1,
    id: "project-1",
    label: "tutti",
    lastUsedAtUnixMs: 1,
    path: "/workspace/tutti",
    pinnedAtUnixMs: 0,
    sectionKey: "project:tutti",
    updatedAtUnixMs: 1
  };
  const listUserProjects = jest
    .fn()
    .mockResolvedValueOnce({ projects: [project] })
    .mockRejectedValueOnce(new Error("offline"));
  const service = new MobileUserProjectDirectoryService({
    listUserProjects
  } as unknown as TuttidClient);

  await service.load();
  await service.load();

  expect(service.getSnapshot()).toEqual({
    errorCode: "request_failed",
    projects: [project],
    status: "ready"
  });
  service.dispose();
});
