import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["extension/tests/**/*.test.ts", "installer/tests/**/*.test.ts", "tests/**/*.test.ts"],
    environment: "node",
    // Installer tests start several real pwsh processes (~1-1.5 s each on
    // Windows); with test files running in parallel the 5 s default times out.
    testTimeout: 15000,
  },
});
