import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  vi.unstubAllGlobals();
  sessionStorage.clear();
  vi.useRealTimers();
});
