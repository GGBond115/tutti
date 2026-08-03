import assert from "node:assert/strict";
import { Writable } from "node:stream";
import test from "node:test";
import { createBestEffortProcessSink } from "./logging.ts";

test("process log sink stops writing after a broken pipe", async () => {
  let writes = 0;
  const stream = new Writable({
    write(_chunk, _encoding, callback) {
      writes += 1;
      const error = Object.assign(new Error("write EPIPE"), {
        code: "EPIPE"
      });
      callback(error);
    }
  });
  const sink = createBestEffortProcessSink(stream);

  await sink("first");
  await sink("second");

  assert.equal(writes, 1);
});
