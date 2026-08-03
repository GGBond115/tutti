import assert from "node:assert/strict";
import test from "node:test";
import { desktopStartupFailure } from "./desktopStartupFailureProtocol.ts";

test("serializes a structured Desktop startup failure cause", () => {
  const failure = desktopStartupFailure(
    new Error("tuttid exited before publishing listener info", {
      cause: {
        code: "managed_process_stderr",
        message: "unsupported process cassette schema version 2"
      }
    })
  );

  assert.deepEqual(failure, {
    cause: {
      code: "managed_process_stderr",
      message: "unsupported process cassette schema version 2"
    },
    message: "tuttid exited before publishing listener info"
  });
});
