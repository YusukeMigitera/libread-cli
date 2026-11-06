import { example } from "../test/examples";
import { decodeCheckView, fetchCheck } from "./check";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8" },
  });
}

type Obj = Record<string, unknown>;

// at は u の中の path（キーと配列の位置）にあるオブジェクトを返す。テストで例を書き換えるのに使う。
function at(u: unknown, ...path: (string | number)[]): Obj {
  let cur: unknown = u;
  for (const key of path) {
    cur = (cur as Obj)[key];
  }
  if (typeof cur !== "object" || cur === null) {
    throw new Error(`例に ${path.join(".")} がない`);
  }
  return cur as Obj;
}

describe("decodeCheckView", () => {
  test("通常の応答の例を受け付ける", () => {
    const view = decodeCheckView(example("check"));
    expect(view.kind).toBe("result");
    if (view.kind !== "result") {
      return;
    }
    expect(view.checkedAt).toBe("2026-10-21T14:05:00+09:00");
    const [central] = view.readable;
    expect(central?.library).toEqual({
      id: "Tokyo_Setagaya:中央図書館",
      name: "世田谷区立中央図書館",
    });
    expect(central?.opening).toEqual({
      status: "open",
      label: "開館中 〜19:00",
      open: "09:00",
      close: "19:00",
      closingSoon: false,
    });
    expect(central?.books).toEqual([
      {
        isbn: "9784003101018",
        title: "吾輩は猫である",
        availability: "availableToLend",
        label: "在架（貸出可）",
        readable: true,
        stale: false,
        fetchedAt: "2026-10-21T14:04:50+09:00",
      },
    ]);
    expect(view.notReadable[0]?.opening).toEqual({
      status: "noSchedule",
      label: "開館情報未入力",
    });
    expect(view.notReadable[0]?.books).toEqual([]);
    expect(view.sources.holdings.label).toBe(
      "所蔵情報: カーリル 14:04 取得（30分以内の取得結果を使用）",
    );
    expect(view.sources.holdings.links).toEqual([
      { title: "吾輩は猫である", url: "https://calil.jp/book/9784003101018" },
    ]);
    expect(view.sources.openingInfo).toEqual({
      label: "開館情報: 利用者入力（最終更新 中央図書館 2026-10-21）",
      note: "※ 臨時休館は入力済みの情報のみ反映しています",
    });
    expect(view.sources.holidays.label).toBe(
      "祝日: holidays-jp（https://holidays-jp.github.io/）",
    );
  });

  test("登録の案内の例を受け付ける", () => {
    expect(decodeCheckView(example("check-setup-libraries"))).toEqual({
      kind: "setupRequired",
      setupRequired: "libraries",
    });
    expect(decodeCheckView(example("check-setup-books"))).toEqual({
      kind: "setupRequired",
      setupRequired: "books",
    });
  });

  test("知らない開館状態・所蔵状況は unknown として受け付ける（Constitution 原則 III）", () => {
    const u = example("check");
    at(u, "readable", 0, "opening").status = "openLate";
    at(u, "readable", 0, "books", 0).availability = "inTransit";
    const view = decodeCheckView(u);
    if (view.kind !== "result") {
      throw new Error("結果として読めていない");
    }
    expect(view.readable[0]?.opening.status).toBe("unknown");
    expect(view.readable[0]?.opening.label).toBe("開館中 〜19:00");
    expect(view.readable[0]?.books[0]?.availability).toBe("unknown");
    expect(view.readable[0]?.books[0]?.label).toBe("在架（貸出可）");
  });

  test("fetchedAt のない本を受け付ける", () => {
    const u = example("check");
    delete at(u, "readable", 0, "books", 0).fetchedAt;
    const view = decodeCheckView(u);
    if (view.kind !== "result") {
      throw new Error("結果として読めていない");
    }
    expect(view.readable[0]?.books[0]?.fetchedAt).toBeUndefined();
  });

  test("必要な項目がなければ例外", () => {
    const cases: ((u: unknown) => void)[] = [
      (u) => {
        delete at(u).checkedAt;
      },
      (u) => {
        at(u).checkedAt = "14:05";
      },
      (u) => {
        delete at(u).readable;
      },
      (u) => {
        delete at(u).sources;
      },
      (u) => {
        delete at(u, "sources", "holdings").links;
      },
      (u) => {
        delete at(u, "readable", 0, "opening").label;
      },
      (u) => {
        delete at(u, "readable", 0, "books", 0).isbn;
      },
      (u) => {
        at(u, "readable", 0, "books", 0).readable = "yes";
      },
      (u) => {
        delete at(u, "notReadable", 0, "opening").status;
      },
    ];
    for (const mutate of cases) {
      const u = example("check");
      mutate(u);
      expect(() => decodeCheckView(u)).toThrow();
    }
    for (const u of [
      null,
      "x",
      {},
      { setupRequired: "everything" },
      { setupRequired: 1 },
    ]) {
      expect(() => decodeCheckView(u)).toThrow();
    }
  });
});

describe("fetchCheck", () => {
  test("すべての本、または指定した1冊で GET /api/check を呼ぶ", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(jsonResponse(200, example("check"))),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchCheck()).resolves.toMatchObject({ kind: "result" });
    const view = await fetchCheck();
    const isbn =
      view.kind === "result" ? view.readable[0]?.books[0]?.isbn : undefined;
    if (isbn === undefined) {
      throw new Error("ISBN を取り出せない");
    }
    await fetchCheck(isbn);
    const calls = fetchMock.mock.calls as unknown as [string, RequestInit][];
    expect(calls.map(([input]) => input)).toEqual([
      "/api/check",
      "/api/check",
      "/api/check?isbn=9784003101018",
    ]);
  });
});
