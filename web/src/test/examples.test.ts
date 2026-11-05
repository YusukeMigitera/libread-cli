import { example } from "./examples";

test("契約の例を読み込める", () => {
  expect(example("session")).toEqual({
    expiresAt: "2026-12-05T08:00:00+09:00",
  });
  expect(example("error-unauthenticated")).toEqual({
    error: { code: "unauthenticated", message: "ログインしてください" },
  });
});
