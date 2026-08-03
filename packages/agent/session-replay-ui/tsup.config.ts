import { defineConfig } from "tsup";

export default defineConfig({
  clean: true,
  dts: true,
  entry: {
    "activity-event": "src/activity-event.ts",
    index: "src/index.ts",
    "interaction-contract": "src/interaction-contract.ts"
  },
  format: ["esm"],
  sourcemap: true
});
