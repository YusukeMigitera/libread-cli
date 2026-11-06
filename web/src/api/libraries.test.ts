import { example } from "../test/examples";
import { jsonResponse, pathOf } from "../test/http";
import {
  addClosure,
  addLibrary,
  asLibraryID,
  asTimeOfDay,
  decodeLibraries,
  decodeLibrary,
  decodeLibrarySearch,
  fetchLibraries,
  parseTimeOfDay,
  putSchedule,
  removeClosure,
  removeLibrary,
  searchLibraries,
  weekdays,
} from "./libraries";
import { logout, revokeAllSessions } from "./session";

type Obj = Record<string, unknown>;

function firstLibrary(u: unknown): Obj {
  const [l] = (u as { libraries: Obj[] }).libraries;
  if (l === undefined) {
    throw new Error("例に図書館がない");
  }
  return l;
}

function scheduleOf(l: Obj): Obj {
  return l.schedule as Obj;
}

/** jsonBody は fetch に渡した本文（JSON の文字列）を読む。 */
function jsonBody(init: RequestInit | undefined): unknown {
  if (typeof init?.body !== "string") {
    throw new Error("本文が文字列でない");
  }
  return JSON.parse(init.body);
}

function stubFetch(response: () => Response) {
  const fn = vi.fn<typeof fetch>(() => Promise.resolve(response()));
  vi.stubGlobal("fetch", fn);
  return fn;
}

const centralID = asLibraryID("Tokyo_Setagaya:中央図書館");

describe("decodeLibraries", () => {
  test("一覧の例を受け付ける", () => {
    expect(decodeLibraries(example("libraries"))).toEqual([
      {
        id: "Tokyo_Setagaya:中央図書館",
        name: "世田谷区立中央図書館",
        pref: "東京都",
        city: "世田谷区",
        schedule: {
          weekly: {
            sun: { open: "09:00", close: "17:00" },
            mon: null,
            tue: { open: "09:00", close: "19:00" },
            wed: { open: "09:00", close: "19:00" },
            thu: { open: "09:00", close: "19:00" },
            fri: { open: "09:00", close: "19:00" },
            sat: { open: "09:00", close: "17:00" },
          },
          holiday: { mode: "same" },
          updatedOn: "2026-10-21",
        },
        closures: [
          { start: "2026-11-10", end: "2026-11-14", reason: "特別整理期間" },
        ],
      },
    ]);
  });

  test("開館情報未入力（schedule が null）を受け付ける", () => {
    const u = example("libraries");
    firstLibrary(u).schedule = null;
    expect(decodeLibraries(u)[0]?.schedule).toBeNull();
  });

  test("祝日の扱い: 休館と時間の指定", () => {
    const u = example("libraries");
    scheduleOf(firstLibrary(u)).holiday = { mode: "closed" };
    expect(decodeLibraries(u)[0]?.schedule?.holiday).toEqual({
      mode: "closed",
    });
    scheduleOf(firstLibrary(u)).holiday = {
      mode: "hours",
      open: "10:00",
      close: "16:00",
    };
    expect(decodeLibraries(u)[0]?.schedule?.holiday).toEqual({
      mode: "hours",
      open: "10:00",
      close: "16:00",
    });
  });

  test("知らない祝日の扱いは unknown にする（Constitution 原則 III）", () => {
    const u = example("libraries");
    scheduleOf(firstLibrary(u)).holiday = { mode: "sometimes" };
    expect(decodeLibraries(u)[0]?.schedule?.holiday).toEqual({
      mode: "unknown",
    });
  });

  test("空の一覧を受け付ける", () => {
    expect(decodeLibraries({ libraries: [] })).toEqual([]);
  });

  test.each<[string, (l: Obj) => void]>([
    ["ID に : がない", (l) => (l.id = "Tokyo_Setagaya")],
    ["館名がない", (l) => delete l.name],
    ["schedule がない", (l) => delete l.schedule],
    ["曜日が欠けている", (l) => delete (scheduleOf(l).weekly as Obj).mon],
    [
      "時刻が HH:MM でない",
      (l) =>
        ((scheduleOf(l).weekly as Obj).sun = { open: "9:00", close: "17:00" }),
    ],
    [
      "時間の指定の祝日に時刻がない",
      (l) => (scheduleOf(l).holiday = { mode: "hours", open: "10:00" }),
    ],
    ["最終更新日が日付でない", (l) => (scheduleOf(l).updatedOn = "10月21日")],
    ["closures がない", (l) => delete l.closures],
    [
      "臨時休館の理由がない",
      (l) => (l.closures = [{ start: "2026-11-10", end: "2026-11-14" }]),
    ],
  ])("形が違えば例外を投げる: %s", (_, mutate) => {
    const u = example("libraries");
    mutate(firstLibrary(u));
    expect(() => decodeLibraries(u)).toThrow();
  });
});

describe("decodeLibrarySearch", () => {
  test("検索結果の例を受け付ける", () => {
    expect(decodeLibrarySearch(example("library-search"))).toEqual({
      libraries: [
        {
          id: "Tokyo_Setagaya:中央図書館",
          name: "世田谷区立中央図書館",
          pref: "東京都",
          city: "世田谷区",
          address: "東京都世田谷区弦巻3-16-8",
          registered: false,
        },
      ],
      credit: { label: "図書館情報: カーリル", url: "https://calil.jp/" },
    });
  });

  test.each<[string, (o: Obj) => void]>([
    ["registered がない", (o) => delete firstLibrary(o).registered],
    ["住所がない", (o) => delete firstLibrary(o).address],
    ["credit がない", (o) => delete o.credit],
  ])("形が違えば例外を投げる: %s", (_, mutate) => {
    const u = example("library-search") as Obj;
    mutate(u);
    expect(() => decodeLibrarySearch(u)).toThrow();
  });
});

describe("時刻", () => {
  test("parseTimeOfDay は HH:MM（00:00〜24:00）だけを受け付ける", () => {
    expect(parseTimeOfDay("09:00")).toBe("09:00");
    expect(parseTimeOfDay("24:00")).toBe("24:00");
    expect(parseTimeOfDay("")).toBeUndefined();
    expect(parseTimeOfDay("9:00")).toBeUndefined();
    expect(parseTimeOfDay("24:01")).toBeUndefined();
    expect(parseTimeOfDay("12:60")).toBeUndefined();
    expect(() => asTimeOfDay("25:00")).toThrow();
  });

  test("weekdays は日曜から土曜の順", () => {
    expect(weekdays.map((d) => d.key)).toEqual([
      "sun",
      "mon",
      "tue",
      "wed",
      "thu",
      "fri",
      "sat",
    ]);
    expect(weekdays.map((d) => d.label).join("")).toBe("日月火水木金土");
  });
});

describe("要求", () => {
  test("fetchLibraries は GET /api/libraries", async () => {
    const fetchMock = stubFetch(() => jsonResponse(200, example("libraries")));
    expect(await fetchLibraries()).toHaveLength(1);
    expect(pathOf(fetchMock.mock.calls[0]?.[0] ?? "")).toBe("/api/libraries");
  });

  test("searchLibraries は指定した条件だけをクエリーに付ける", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(200, example("library-search")),
    );
    const res = await searchLibraries({ pref: "東京都", name: "中央" });
    expect(res.libraries).toHaveLength(1);
    const url = new URL(pathOf(fetchMock.mock.calls[0]?.[0] ?? ""), "http://x");
    expect(url.pathname).toBe("/api/library-search");
    expect(url.searchParams.get("pref")).toBe("東京都");
    expect(url.searchParams.get("name")).toBe("中央");
    expect(url.searchParams.has("city")).toBe(false);
  });

  test("addLibrary は POST /api/libraries に ID を送り、登録した館を返す", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(201, {
        ...firstLibrary(example("libraries")),
        schedule: null,
        closures: [],
      }),
    );
    const lib = await addLibrary(centralID);
    expect(lib.schedule).toBeNull();
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe("/api/libraries");
    expect(init?.method).toBe("POST");
    expect(jsonBody(init)).toEqual({ id: "Tokyo_Setagaya:中央図書館" });
  });

  test("removeLibrary は DELETE /api/libraries/{id}（ID は URL エンコード）", async () => {
    const fetchMock = stubFetch(() => new Response(null, { status: 204 }));
    await expect(removeLibrary(centralID)).resolves.toBeUndefined();
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe(
      `/api/libraries/${encodeURIComponent("Tokyo_Setagaya:中央図書館")}`,
    );
    expect(init?.method).toBe("DELETE");
  });

  test("putSchedule は PUT /api/libraries/{id}/schedule に7曜日すべてと祝日の扱いを送る", async () => {
    const lib = firstLibrary(example("libraries"));
    const fetchMock = stubFetch(() => jsonResponse(200, lib));
    const weekly = decodeLibrary(lib).schedule?.weekly;
    if (!weekly) {
      throw new Error("例に schedule がない");
    }
    const updated = await putSchedule(centralID, {
      weekly,
      holiday: {
        mode: "hours",
        open: asTimeOfDay("10:00"),
        close: asTimeOfDay("16:00"),
      },
    });
    expect(updated.id).toBe("Tokyo_Setagaya:中央図書館");
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe(
      `/api/libraries/${encodeURIComponent("Tokyo_Setagaya:中央図書館")}/schedule`,
    );
    expect(init?.method).toBe("PUT");
    expect(jsonBody(init)).toEqual({
      weekly: (lib.schedule as Obj).weekly,
      holiday: { mode: "hours", open: "10:00", close: "16:00" },
    });
  });

  test("addClosure は POST .../closures。理由が空なら送らない", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(201, firstLibrary(example("libraries"))),
    );
    await addClosure(centralID, {
      start: "2026-11-10",
      end: "2026-11-14",
      reason: "特別整理期間",
    });
    await addClosure(centralID, {
      start: "2026-12-01",
      end: "2026-12-01",
      reason: "",
    });
    const [[input, init], [, init2]] = fetchMock.mock.calls as [
      [string, RequestInit],
      [string, RequestInit],
    ];
    expect(input).toBe(
      `/api/libraries/${encodeURIComponent("Tokyo_Setagaya:中央図書館")}/closures`,
    );
    expect(init.method).toBe("POST");
    expect(jsonBody(init)).toEqual({
      start: "2026-11-10",
      end: "2026-11-14",
      reason: "特別整理期間",
    });
    expect(jsonBody(init2)).toEqual({ start: "2026-12-01", end: "2026-12-01" });
  });

  test("removeClosure は DELETE .../closures/{index} で、更新後の館を返す", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(200, {
        ...firstLibrary(example("libraries")),
        closures: [],
      }),
    );
    const lib = await removeClosure(centralID, 0);
    expect(lib.closures).toEqual([]);
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe(
      `/api/libraries/${encodeURIComponent("Tokyo_Setagaya:中央図書館")}/closures/0`,
    );
    expect(init?.method).toBe("DELETE");
  });

  test("logout は POST /api/session/logout、revokeAllSessions は POST /api/session/revoke-all", async () => {
    const fetchMock = stubFetch(() => new Response(null, { status: 204 }));
    await expect(logout()).resolves.toBeUndefined();
    await expect(revokeAllSessions()).resolves.toBeUndefined();
    const calls = fetchMock.mock.calls.map(([input, init]) => [
      input,
      init?.method,
    ]);
    expect(calls).toEqual([
      ["/api/session/logout", "POST"],
      ["/api/session/revoke-all", "POST"],
    ]);
  });
});
