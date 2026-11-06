import { example } from "../test/examples";
import {
  ApiError,
  decodeSession,
  getJSON,
  noContent,
  sendJSON,
  UnauthenticatedError,
} from "./http";
import { jsonResponse } from "../test/http";

function stubFetch(
  impl: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
) {
  const fn = vi.fn(impl);
  vi.stubGlobal("fetch", fn);
  return fn;
}

const identity = (u: unknown): unknown => u;

describe("getJSON", () => {
  test("同じオリジンの Cookie を付けて要求し、検証関数を通した値を返す", async () => {
    const fetchMock = stubFetch(() =>
      Promise.resolve(jsonResponse(200, { a: 1 })),
    );
    await expect(getJSON("/api/x", identity)).resolves.toEqual({ a: 1 });
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe("/api/x");
    expect(init?.credentials).toBe("same-origin");
    expect(init?.method ?? "GET").toBe("GET");
  });

  test("検証関数が失敗したら「通信に失敗しました」の ApiError", async () => {
    stubFetch(() => Promise.resolve(jsonResponse(200, { a: 1 })));
    const err = await getJSON("/api/x", () => {
      throw new Error("形が違う");
    }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe("通信に失敗しました");
  });
});

describe("sendJSON", () => {
  test("書き込みは JSON の本文と Content-Type を送る", async () => {
    const fetchMock = stubFetch(() =>
      Promise.resolve(jsonResponse(201, { ok: true })),
    );
    await expect(
      sendJSON("POST", "/api/books", { isbn: "9784003101018" }, identity),
    ).resolves.toEqual({
      ok: true,
    });
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe("/api/books");
    expect(init?.method).toBe("POST");
    expect(init?.credentials).toBe("same-origin");
    expect(new Headers(init?.headers).get("Content-Type")).toBe(
      "application/json",
    );
    expect(init?.body).toBe(JSON.stringify({ isbn: "9784003101018" }));
  });

  test("204 は noContent で受けられる", async () => {
    stubFetch(() => Promise.resolve(new Response(null, { status: 204 })));
    await expect(
      sendJSON("POST", "/api/session/logout", undefined, noContent),
    ).resolves.toBeUndefined();
  });
});

describe("エラー", () => {
  test("401 は UnauthenticatedError", async () => {
    stubFetch(() =>
      Promise.resolve(jsonResponse(401, example("error-unauthenticated"))),
    );
    await expect(getJSON("/api/books", identity)).rejects.toBeInstanceOf(
      UnauthenticatedError,
    );
    await expect(
      sendJSON("DELETE", "/api/books/1", undefined, noContent),
    ).rejects.toBeInstanceOf(UnauthenticatedError);
  });

  test("ほかのエラーは本文の error.message を持つ ApiError", async () => {
    stubFetch(() =>
      Promise.resolve(
        jsonResponse(409, {
          error: { code: "conflict", message: "すでに登録済みです" },
        }),
      ),
    );
    const err = await sendJSON("POST", "/api/books", {}, identity).catch(
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe("すでに登録済みです");
    expect((err as ApiError).status).toBe(409);
    expect((err as ApiError).code).toBe("conflict");
  });

  test("error.code がなくても error.message は使う", async () => {
    stubFetch(() =>
      Promise.resolve(
        jsonResponse(409, { error: { message: "すでに登録済みです" } }),
      ),
    );
    const err = await sendJSON("POST", "/api/books", {}, identity).catch(
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe("すでに登録済みです");
    expect((err as ApiError).code).toBeUndefined();
  });

  test("本文が契約の形でなければ「通信に失敗しました」", async () => {
    for (const body of [
      "<html>502</html>",
      JSON.stringify({ message: "x" }),
      "",
    ]) {
      stubFetch(() => Promise.resolve(new Response(body, { status: 502 })));
      const err = await getJSON("/api/check", identity).catch(
        (e: unknown) => e,
      );
      expect(err).toBeInstanceOf(ApiError);
      expect((err as ApiError).message).toBe("通信に失敗しました");
    }
  });

  test("通信できなければ「通信に失敗しました」", async () => {
    stubFetch(() => Promise.reject(new TypeError("Failed to fetch")));
    const err = await getJSON("/api/check", identity).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe("通信に失敗しました");
  });
});

describe("decodeSession", () => {
  test("契約の例を受け付ける", () => {
    expect(decodeSession(example("session"))).toEqual({
      expiresAt: "2026-12-05T08:00:00+09:00",
    });
  });

  test("形が違えば例外", () => {
    for (const u of [
      null,
      "x",
      {},
      { expiresAt: 1 },
      { expiresAt: "明日" },
      { expiresAt: "2026-12-05" },
    ]) {
      expect(() => decodeSession(u)).toThrow();
    }
  });
});

describe("タイムアウト", () => {
  // neverResponds は、中止されるまで応答しない fetch。
  function neverResponds() {
    return stubFetch(
      (_, init) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => {
            reject(new DOMException("中止しました", "AbortError"));
          });
        }),
    );
  }

  test("60秒たっても応答がなければ中止し、「通信に失敗しました」の ApiError", async () => {
    vi.useFakeTimers();
    const fetchMock = neverResponds();
    const result = sendJSON(
      "PATCH",
      "/api/books/x",
      { note: "" },
      identity,
    ).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(59_999);
    expect(fetchMock.mock.calls[0]?.[1]?.signal?.aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(1);
    const err = await result;
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).message).toBe("通信に失敗しました");
  });

  test("応答を受けたらタイマーを止める", async () => {
    vi.useFakeTimers();
    stubFetch(() => Promise.resolve(jsonResponse(200, { a: 1 })));
    await getJSON("/api/x", identity);
    expect(vi.getTimerCount()).toBe(0);
  });
});
