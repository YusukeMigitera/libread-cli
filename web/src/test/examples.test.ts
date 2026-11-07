import { example } from "./examples";

test("契約の例を読み込める", () => {
  expect(example("session")).toEqual({
    expiresAt: "2026-12-05T08:00:00+09:00",
  });
  expect(example("error-unauthenticated")).toEqual({
    error: { code: "unauthenticated", message: "ログインしてください" },
  });
});

// 知らない名前は型チェック（npm run typecheck）で分かる。実行はしない。
export function typoIsATypeError(): unknown {
  // @ts-expect-error -- "sesion" は例の名前ではない
  return example("sesion");
}
