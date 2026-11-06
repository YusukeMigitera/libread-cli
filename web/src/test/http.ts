// テストで fetch の応答を作る補助。

/** jsonResponse は status と JSON の本文を持つ応答を作る。 */
export function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8" },
  });
}

/** pathOf は fetch に渡された input のパス（クエリー文字列を含む）。 */
export function pathOf(input: RequestInfo | URL): string {
  return typeof input === "string"
    ? input
    : input instanceof URL
      ? input.pathname + input.search
      : input.url;
}
