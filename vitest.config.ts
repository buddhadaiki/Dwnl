import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["extension/tests/**/*.test.ts", "installer/tests/**/*.test.ts", "tests/**/*.test.ts"],
    environment: "node",
  },
});
