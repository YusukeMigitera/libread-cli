import "@testing-library/jest-dom/vitest";

// fetch は vi.stubGlobal("fetch", ...) で差し替える。
// 画面の後片付け（cleanup）は Testing Library が globals: true のときに自動で登録する。
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  sessionStorage.clear();
  vi.useRealTimers();
});
