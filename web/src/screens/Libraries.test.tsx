import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "../App";
import { example } from "../test/examples";
import { jsonResponse, pathOf } from "../test/http";
import { Libraries } from "./Libraries";

type Obj = Record<string, unknown>;

function exampleLibrary(change: Partial<Obj> = {}): Obj {
  const [l] = (example("libraries") as { libraries: Obj[] }).libraries;
  return { ...l, ...change };
}

const central = exampleLibrary();
const kyodo = exampleLibrary({
  id: "Tokyo_Setagaya:経堂図書館",
  name: "世田谷区立経堂図書館",
  schedule: null,
  closures: [],
});

interface FakeApi {
  libraries: Obj[];
  search: Obj;
  /** 要求ごとに応答を差し替える。undefined を返したら既定の応答。 */
  override?: (
    method: string,
    path: string,
    body: unknown,
  ) => Response | Promise<Response> | undefined;
}

const libraryPrefix = "/api/libraries/";

// stubApi は図書館の API を、api の中身を使って応答する fetch に差し替える。変更の要求は api.libraries を書き換える。
function stubApi(api: FakeApi) {
  const fn = vi.fn<typeof fetch>((input, init) => {
    const path = pathOf(input);
    const method = init?.method ?? "GET";
    const body: unknown =
      typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    const o = api.override?.(method, path, body);
    if (o !== undefined) {
      return Promise.resolve(o);
    }
    const ok = (status: number, b: unknown) =>
      Promise.resolve(jsonResponse(status, b));
    if (path === "/api/session") {
      return ok(200, example("session"));
    }
    if (path.startsWith("/api/session/") && method === "POST") {
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    if (path === "/api/libraries" && method === "GET") {
      return ok(200, { libraries: api.libraries });
    }
    if (path.startsWith("/api/library-search?")) {
      return ok(200, api.search);
    }
    if (path === "/api/libraries" && method === "POST") {
      const id = (body as { id: string }).id;
      const l = exampleLibrary({
        id,
        name: "登録した館",
        schedule: null,
        closures: [],
      });
      api.libraries = [...api.libraries, l];
      return ok(201, l);
    }
    if (path.startsWith(libraryPrefix)) {
      const [rawID = "", sub, index] = path
        .slice(libraryPrefix.length)
        .split("/");
      const id = decodeURIComponent(rawID);
      const lib = api.libraries.find((l) => l.id === id);
      if (!lib) {
        return ok(404, { error: { code: "notFound", message: "ない館" } });
      }
      const replace = (l: Obj) => {
        api.libraries = api.libraries.map((x) => (x.id === id ? l : x));
        return l;
      };
      if (sub === undefined && method === "DELETE") {
        api.libraries = api.libraries.filter((l) => l.id !== id);
        return Promise.resolve(new Response(null, { status: 204 }));
      }
      if (sub === "schedule" && method === "PUT") {
        const s = body as Obj;
        return ok(
          200,
          replace({ ...lib, schedule: { ...s, updatedOn: "2026-11-05" } }),
        );
      }
      if (sub === "closures" && method === "POST") {
        const c = body as { start: string; end: string; reason?: string };
        const closures = [
          ...(lib.closures as Obj[]),
          { start: c.start, end: c.end, reason: c.reason ?? "" },
        ].sort((a, b) => String(a.start).localeCompare(String(b.start)));
        return ok(201, replace({ ...lib, closures }));
      }
      if (sub === "closures" && method === "DELETE") {
        const closures = (lib.closures as Obj[]).filter(
          (_, i) => i !== Number(index),
        );
        return ok(200, replace({ ...lib, closures }));
      }
    }
    return ok(404, { error: { code: "notFound", message: "x" } });
  });
  vi.stubGlobal("fetch", fn);
  const calls = () =>
    fn.mock.calls.map(([input, init]) => ({
      method: init?.method ?? "GET",
      path: pathOf(input),
      body:
        typeof init?.body === "string"
          ? (JSON.parse(init.body) as unknown)
          : undefined,
    }));
  return { fn, calls };
}

function newApi(change: Partial<FakeApi> = {}): FakeApi {
  return {
    libraries: [central, kyodo],
    search: example("library-search") as Obj,
    ...change,
  };
}

afterEach(() => {
  window.location.hash = "";
});

/** libraryItem は一覧の中で name の館の項目。 */
async function libraryItem(name: string): Promise<HTMLElement> {
  const list = await screen.findByRole("list", { name: "利用図書館" });
  const item = within(list)
    .getAllByRole("listitem")
    .find((li) => within(li).queryByText(name) !== null);
  if (!item) {
    throw new Error(`一覧に${name}がない`);
  }
  return item;
}

const centralPath = `/api/libraries/${encodeURIComponent("Tokyo_Setagaya:中央図書館")}`;
const kyodoPath = `/api/libraries/${encodeURIComponent("Tokyo_Setagaya:経堂図書館")}`;

describe("一覧", () => {
  test("館名・市区町村・曜日ごとの時間・祝日の扱い・最終更新日・臨時休館期間を表示する", async () => {
    stubApi(newApi());
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    const w = within(item);
    expect(w.getByText("世田谷区")).toBeInTheDocument();
    expect(w.getByText("日 09:00〜17:00")).toBeInTheDocument();
    expect(w.getByText("月 休館")).toBeInTheDocument();
    expect(w.getByText("火 09:00〜19:00")).toBeInTheDocument();
    expect(w.getByText("土 09:00〜17:00")).toBeInTheDocument();
    expect(w.getByText("祝日 曜日どおり")).toBeInTheDocument();
    expect(w.getByText("最終更新 2026-10-21")).toBeInTheDocument();
    expect(
      w.getByText("2026-11-10〜2026-11-14（特別整理期間）"),
    ).toBeInTheDocument();
    for (const name of ["開館時間を編集", "臨時休館を追加", "外す", "削除"]) {
      expect(w.getByRole("button", { name })).toBeInTheDocument();
    }

    const other = await libraryItem("世田谷区立経堂図書館");
    expect(within(other).getByText("開館情報未入力")).toBeInTheDocument();
  });

  test("祝日の扱い: 休館・時間の指定", async () => {
    const sched = central.schedule as Obj;
    stubApi(
      newApi({
        libraries: [
          { ...central, schedule: { ...sched, holiday: { mode: "closed" } } },
          {
            ...kyodo,
            schedule: {
              ...sched,
              holiday: { mode: "hours", open: "10:00", close: "16:00" },
            },
          },
        ],
      }),
    );
    render(<Libraries />);
    expect(
      within(await libraryItem("世田谷区立中央図書館")).getByText("祝日 休館"),
    ).toBeInTheDocument();
    expect(
      within(await libraryItem("世田谷区立経堂図書館")).getByText(
        "祝日 10:00〜16:00",
      ),
    ).toBeInTheDocument();
  });

  test("図書館がなければその旨を表示する", async () => {
    stubApi(newApi({ libraries: [] }));
    render(<Libraries />);
    expect(
      await screen.findByText("利用図書館は登録されていません"),
    ).toBeInTheDocument();
  });

  test("一覧を読めなければ、メッセージと「再試行」", async () => {
    const api = newApi();
    let fail = true;
    api.override = (method, path) =>
      fail && path === "/api/libraries"
        ? jsonResponse(500, {
            error: { code: "internal", message: "保存先を読めませんでした" },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Libraries />);
    expect(
      await screen.findByText("保存先を読めませんでした"),
    ).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole("button", { name: "再試行" }));
    expect(await libraryItem("世田谷区立中央図書館")).toBeInTheDocument();
  });
});

/** dayCheckbox は開館時間の編集で、曜日 day（例: "水"）の「開館」の切り替え。 */
function dayCheckbox(form: HTMLElement, day: string): HTMLElement {
  return within(form).getByRole("checkbox", { name: `${day}曜日 開館` });
}

function setTime(form: HTMLElement, label: string, value: string) {
  fireEvent.change(within(form).getByLabelText(label), { target: { value } });
}

describe("開館時間の編集", () => {
  test("初めて入力するときは、すべての曜日が休館、祝日は曜日どおりから始め、「保存」で7曜日すべてを PUT する", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立経堂図書館");
    await user.click(
      within(item).getByRole("button", { name: "開館時間を編集" }),
    );
    const form = within(item).getByRole("form", { name: "開館時間の編集" });
    for (const d of ["日", "月", "火", "水", "木", "金", "土"]) {
      expect(dayCheckbox(form, d)).not.toBeChecked();
    }
    expect(
      within(form).getByRole("radio", { name: "曜日どおり" }),
    ).toBeChecked();

    await user.click(dayCheckbox(form, "水"));
    setTime(form, "水曜日 開館時刻", "10:00");
    setTime(form, "水曜日 閉館時刻", "20:00");
    await user.click(within(form).getByRole("button", { name: "保存" }));

    await waitFor(() => {
      expect(
        within(item).queryByRole("form", { name: "開館時間の編集" }),
      ).not.toBeInTheDocument();
    });
    expect(calls().filter((c) => c.method === "PUT")).toEqual([
      {
        method: "PUT",
        path: `${kyodoPath}/schedule`,
        body: {
          weekly: {
            sun: null,
            mon: null,
            tue: null,
            wed: { open: "10:00", close: "20:00" },
            thu: null,
            fri: null,
            sat: null,
          },
          holiday: { mode: "same" },
        },
      },
    ]);
    const updated = await libraryItem("世田谷区立経堂図書館");
    expect(within(updated).getByText("水 10:00〜20:00")).toBeInTheDocument();
    expect(
      within(updated).getByText("最終更新 2026-11-05"),
    ).toBeInTheDocument();
  });

  test("入力済みの館は今のスケジュールから始め、祝日の時間の指定も送る", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "開館時間を編集" }),
    );
    const form = within(item).getByRole("form", { name: "開館時間の編集" });
    expect(dayCheckbox(form, "日")).toBeChecked();
    expect(dayCheckbox(form, "月")).not.toBeChecked();
    expect(within(form).getByLabelText("日曜日 閉館時刻")).toHaveValue("17:00");

    await user.click(dayCheckbox(form, "日")); // 日曜を休館に
    await user.click(within(form).getByRole("radio", { name: "時間を指定" }));
    setTime(form, "祝日 開館時刻", "10:00");
    setTime(form, "祝日 閉館時刻", "16:00");
    await user.click(within(form).getByRole("button", { name: "保存" }));

    await waitFor(() => {
      expect(calls().some((c) => c.method === "PUT")).toBe(true);
    });
    const put = calls().find((c) => c.method === "PUT");
    expect(put?.body).toEqual({
      weekly: {
        sun: null,
        mon: null,
        tue: { open: "09:00", close: "19:00" },
        wed: { open: "09:00", close: "19:00" },
        thu: { open: "09:00", close: "19:00" },
        fri: { open: "09:00", close: "19:00" },
        sat: { open: "09:00", close: "17:00" },
      },
      holiday: { mode: "hours", open: "10:00", close: "16:00" },
    });
  });

  test("開館時刻が閉館時刻より後なら、送らずにメッセージを表示する", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立経堂図書館");
    await user.click(
      within(item).getByRole("button", { name: "開館時間を編集" }),
    );
    const form = within(item).getByRole("form", { name: "開館時間の編集" });
    await user.click(dayCheckbox(form, "火"));
    setTime(form, "火曜日 開館時刻", "18:00");
    setTime(form, "火曜日 閉館時刻", "09:00");
    await user.click(within(form).getByRole("button", { name: "保存" }));

    expect(
      within(form).getByText("火曜日: 開館時刻は閉館時刻より前にしてください"),
    ).toBeInTheDocument();
    expect(calls().some((c) => c.method === "PUT")).toBe(false);
  });

  test("「取消」で閉じ、何も送らない", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "開館時間を編集" }),
    );
    await user.click(within(item).getByRole("button", { name: "取消" }));
    expect(
      within(item).queryByRole("form", { name: "開館時間の編集" }),
    ).not.toBeInTheDocument();
    expect(calls().every((c) => c.method === "GET")).toBe(true);
  });

  test("保存に失敗したら、入力を残してメッセージを表示する", async () => {
    const api = newApi();
    api.override = (method) =>
      method === "PUT"
        ? jsonResponse(400, {
            error: { code: "invalid", message: "時刻が正しくありません" },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "開館時間を編集" }),
    );
    const form = within(item).getByRole("form", { name: "開館時間の編集" });
    await user.click(dayCheckbox(form, "月"));
    await user.click(within(form).getByRole("button", { name: "保存" }));
    expect(
      await within(form).findByText("時刻が正しくありません"),
    ).toBeInTheDocument();
    expect(dayCheckbox(form, "月")).toBeChecked();
  });
});

describe("臨時休館の追加", () => {
  test("開始日が終了日より後なら、送らずに「開始日は終了日以前にしてください」", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "臨時休館を追加" }),
    );
    const form = within(item).getByRole("form", { name: "臨時休館の追加" });
    setTime(form, "開始日", "2026-11-20");
    setTime(form, "終了日", "2026-11-19");
    await user.click(within(form).getByRole("button", { name: "追加" }));

    expect(
      within(form).getByText("開始日は終了日以前にしてください"),
    ).toBeInTheDocument();
    expect(calls().some((c) => c.method === "POST")).toBe(false);
  });

  test("開始日・終了日・理由を POST し、一覧に加える", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "臨時休館を追加" }),
    );
    const form = within(item).getByRole("form", { name: "臨時休館の追加" });
    expect(within(form).getByLabelText("開始日")).toHaveAttribute(
      "type",
      "date",
    );
    setTime(form, "開始日", "2026-11-01");
    setTime(form, "終了日", "2026-11-03");
    await user.type(within(form).getByLabelText("理由（任意）"), "蔵書点検");
    await user.click(within(form).getByRole("button", { name: "追加" }));

    const updated = await libraryItem("世田谷区立中央図書館");
    expect(
      await within(updated).findByText("2026-11-01〜2026-11-03（蔵書点検）"),
    ).toBeInTheDocument();
    expect(calls().filter((c) => c.method === "POST")).toEqual([
      {
        method: "POST",
        path: `${centralPath}/closures`,
        body: { start: "2026-11-01", end: "2026-11-03", reason: "蔵書点検" },
      },
    ]);
  });

  test("日付を入れずに「追加」したら、送らずに入力を促す", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "臨時休館を追加" }),
    );
    const form = within(item).getByRole("form", { name: "臨時休館の追加" });
    await user.click(within(form).getByRole("button", { name: "追加" }));
    expect(
      within(form).getByText("開始日と終了日を入力してください"),
    ).toBeInTheDocument();
    expect(calls().some((c) => c.method === "POST")).toBe(false);
  });

  test("本人確認が切れたら入力を退避してログイン画面に切り替え、再ログイン後に戻す（research W12）", async () => {
    window.location.hash = "#/libraries";
    const api = newApi();
    api.override = (method) =>
      method === "POST"
        ? jsonResponse(401, example("error-unauthenticated"))
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    const { unmount } = render(<App />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(
      within(item).getByRole("button", { name: "臨時休館を追加" }),
    );
    const form = within(item).getByRole("form", { name: "臨時休館の追加" });
    setTime(form, "開始日", "2026-11-01");
    setTime(form, "終了日", "2026-11-03");
    await user.type(within(form).getByLabelText("理由（任意）"), "蔵書点検");
    await user.click(within(form).getByRole("button", { name: "追加" }));

    expect(
      await screen.findByRole("link", { name: "Google でログイン" }),
    ).toHaveAttribute("href", "/auth/login?return=/libraries");
    expect(sessionStorage.length).toBe(1);

    unmount();
    api.override = undefined;
    render(<App />);
    const again = await libraryItem("世田谷区立中央図書館");
    const restored = await within(again).findByRole("form", {
      name: "臨時休館の追加",
    });
    expect(within(restored).getByLabelText("開始日")).toHaveValue("2026-11-01");
    expect(within(restored).getByLabelText("終了日")).toHaveValue("2026-11-03");
    expect(within(restored).getByLabelText("理由（任意）")).toHaveValue(
      "蔵書点検",
    );
    expect(sessionStorage.length).toBe(0);
  });

  test("開館時間の保存で本人確認が切れたら、入力を退避して再ログイン後に戻す（research W12）", async () => {
    window.location.hash = "#/libraries";
    const api = newApi();
    api.override = (method) =>
      method === "PUT"
        ? jsonResponse(401, example("error-unauthenticated"))
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    const { unmount } = render(<App />);
    const item = await libraryItem("世田谷区立経堂図書館");
    await user.click(
      within(item).getByRole("button", { name: "開館時間を編集" }),
    );
    const form = within(item).getByRole("form", { name: "開館時間の編集" });
    await user.click(dayCheckbox(form, "金"));
    setTime(form, "金曜日 開館時刻", "10:00");
    await user.click(within(form).getByRole("button", { name: "保存" }));
    await screen.findByRole("link", { name: "Google でログイン" });

    unmount();
    api.override = undefined;
    render(<App />);
    const again = await libraryItem("世田谷区立経堂図書館");
    const restored = await within(again).findByRole("form", {
      name: "開館時間の編集",
    });
    expect(dayCheckbox(restored, "金")).toBeChecked();
    expect(within(restored).getByLabelText("金曜日 開館時刻")).toHaveValue(
      "10:00",
    );
    expect(sessionStorage.length).toBe(0);
  });
});

describe("外す・削除（FR-108）", () => {
  test("「外す」は確認ダイアログで取り消したら何も送らない", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(within(item).getByRole("button", { name: "外す" }));
    expect(confirm).toHaveBeenCalledWith("世田谷区立中央図書館を外しますか？");
    expect(calls().some((c) => c.method === "DELETE")).toBe(false);
  });

  test("「外す」を確認したら DELETE を送り、一覧から外す", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(within(item).getByRole("button", { name: "外す" }));
    await waitFor(() => {
      expect(
        screen.queryByText("世田谷区立中央図書館"),
      ).not.toBeInTheDocument();
    });
    expect(calls().filter((c) => c.method === "DELETE")).toEqual([
      { method: "DELETE", path: centralPath, body: undefined },
    ]);
  });

  test("臨時休館期間の「削除」は確認してから DELETE .../closures/{index} を送る", async () => {
    const confirm = vi
      .spyOn(window, "confirm")
      .mockReturnValueOnce(false)
      .mockReturnValueOnce(true);
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    const item = await libraryItem("世田谷区立中央図書館");
    await user.click(within(item).getByRole("button", { name: "削除" }));
    expect(confirm).toHaveBeenLastCalledWith(
      "臨時休館期間 2026-11-10〜2026-11-14（特別整理期間）を外しますか？",
    );
    expect(calls().some((c) => c.method === "DELETE")).toBe(false);

    await user.click(within(item).getByRole("button", { name: "削除" }));
    await waitFor(() => {
      expect(
        screen.queryByText("2026-11-10〜2026-11-14（特別整理期間）"),
      ).not.toBeInTheDocument();
    });
    expect(calls().filter((c) => c.method === "DELETE")).toEqual([
      { method: "DELETE", path: `${centralPath}/closures/0`, body: undefined },
    ]);
  });
});

describe("図書館を探す", () => {
  function finder(): HTMLElement {
    return screen.getByRole("region", { name: "図書館を探す" });
  }

  test("都道府県は47都道府県から選ぶ必須の項目で、選ばなければ探さない", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Libraries />);
    await libraryItem("世田谷区立中央図書館");
    const pref = within(finder()).getByLabelText("都道府県");
    const options = within(pref)
      .getAllByRole("option")
      .filter((o) => (o as HTMLOptionElement).value !== "");
    expect(options).toHaveLength(47);
    expect(options[0]).toHaveTextContent("北海道");
    expect(options[12]).toHaveTextContent("東京都");
    expect(options[46]).toHaveTextContent("沖縄県");

    await user.click(within(finder()).getByRole("button", { name: "探す" }));
    expect(
      within(finder()).getByText("都道府県を選んでください"),
    ).toBeInTheDocument();
    expect(calls().some((c) => c.path.startsWith("/api/library-search"))).toBe(
      false,
    );
  });

  test("結果に館名・市区町村・住所と「登録」、登録済みは押せない。カーリルへのリンクを表示する", async () => {
    const search = example("library-search") as {
      libraries: Obj[];
      credit: Obj;
    };
    const [first] = search.libraries;
    search.libraries = [
      { ...first, registered: true },
      {
        ...first,
        id: "Tokyo_Setagaya:奥沢図書館",
        name: "世田谷区立奥沢図書館",
        address: "東京都世田谷区奥沢3-26-6",
        registered: false,
      },
    ];
    const { calls } = stubApi(newApi({ search }));
    const user = userEvent.setup();
    render(<Libraries />);
    await libraryItem("世田谷区立中央図書館");
    await user.selectOptions(
      within(finder()).getByLabelText("都道府県"),
      "東京都",
    );
    await user.type(within(finder()).getByLabelText("市区町村"), "世田谷区");
    await user.click(within(finder()).getByRole("button", { name: "探す" }));

    const results = await within(finder()).findByRole("list", {
      name: "検索結果",
    });
    const [registered, other] = within(results).getAllByRole("listitem");
    if (!registered || !other) {
      throw new Error("結果が2件ない");
    }
    expect(
      within(registered).getByText("世田谷区立中央図書館"),
    ).toBeInTheDocument();
    expect(
      within(registered).getByRole("button", { name: "登録済み" }),
    ).toBeDisabled();
    expect(within(other).getByText("世田谷区")).toBeInTheDocument();
    expect(
      within(other).getByText("東京都世田谷区奥沢3-26-6"),
    ).toBeInTheDocument();
    expect(
      within(finder()).getByRole("link", { name: "図書館情報: カーリル" }),
    ).toHaveAttribute("href", "https://calil.jp/");

    const searchCall = calls().find((c) =>
      c.path.startsWith("/api/library-search"),
    );
    const url = new URL(searchCall?.path ?? "", "http://x");
    expect(url.searchParams.get("pref")).toBe("東京都");
    expect(url.searchParams.get("city")).toBe("世田谷区");
    expect(url.searchParams.has("name")).toBe(false);

    // 「登録」で POST し、一覧に加えて「登録済み」にする。
    await user.click(within(other).getByRole("button", { name: "登録" }));
    expect(
      await within(other).findByRole("button", { name: "登録済み" }),
    ).toBeDisabled();
    expect(await libraryItem("登録した館")).toBeInTheDocument();
    expect(calls().filter((c) => c.method === "POST")).toEqual([
      {
        method: "POST",
        path: "/api/libraries",
        body: { id: "Tokyo_Setagaya:奥沢図書館" },
      },
    ]);
  });

  test("0件なら「見つかりませんでした」", async () => {
    stubApi(
      newApi({
        search: { ...(example("library-search") as Obj), libraries: [] },
      }),
    );
    const user = userEvent.setup();
    render(<Libraries />);
    await libraryItem("世田谷区立中央図書館");
    await user.selectOptions(
      within(finder()).getByLabelText("都道府県"),
      "東京都",
    );
    await user.type(within(finder()).getByLabelText("館名"), "存在しない");
    await user.click(within(finder()).getByRole("button", { name: "探す" }));
    expect(
      await within(finder()).findByText(
        "「東京都」で「存在しない」を含む図書館は見つかりませんでした",
      ),
    ).toBeInTheDocument();
  });

  test("検索に失敗したら、メッセージと「再試行」", async () => {
    const api = newApi();
    let fail = true;
    api.override = (_, path) =>
      fail && path.startsWith("/api/library-search")
        ? jsonResponse(502, {
            error: {
              code: "upstream",
              message: "カーリルに接続できませんでした",
            },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Libraries />);
    await libraryItem("世田谷区立中央図書館");
    await user.selectOptions(
      within(finder()).getByLabelText("都道府県"),
      "東京都",
    );
    await user.click(within(finder()).getByRole("button", { name: "探す" }));
    expect(
      await within(finder()).findByText("カーリルに接続できませんでした"),
    ).toBeInTheDocument();
    fail = false;
    await user.click(within(finder()).getByRole("button", { name: "再試行" }));
    expect(
      await within(finder()).findByRole("list", { name: "検索結果" }),
    ).toBeInTheDocument();
  });
});

describe("設定", () => {
  test("「ログアウト」は POST /api/session/logout のあとログイン画面へ", async () => {
    window.location.hash = "#/libraries";
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<App />);
    await libraryItem("世田谷区立中央図書館");
    await user.click(screen.getByRole("button", { name: "ログアウト" }));
    expect(
      await screen.findByRole("link", { name: "Google でログイン" }),
    ).toBeInTheDocument();
    expect(calls().filter((c) => c.method === "POST")).toEqual([
      { method: "POST", path: "/api/session/logout", body: undefined },
    ]);
  });

  test("「すべての端末からログアウト」は確認してから POST /api/session/revoke-all、ログイン画面へ（FR-111）", async () => {
    window.location.hash = "#/libraries";
    const confirm = vi
      .spyOn(window, "confirm")
      .mockReturnValueOnce(false)
      .mockReturnValueOnce(true);
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<App />);
    await libraryItem("世田谷区立中央図書館");
    const button = screen.getByRole("button", {
      name: "すべての端末からログアウト",
    });
    await user.click(button);
    expect(confirm).toHaveBeenCalledWith(
      "すべての端末からログアウトしますか？",
    );
    expect(calls().some((c) => c.method === "POST")).toBe(false);

    await user.click(button);
    expect(
      await screen.findByRole("link", { name: "Google でログイン" }),
    ).toBeInTheDocument();
    expect(calls().filter((c) => c.method === "POST")).toEqual([
      { method: "POST", path: "/api/session/revoke-all", body: undefined },
    ]);
  });

  test("ログアウトに失敗したら、メッセージを表示して画面に残る", async () => {
    const api = newApi();
    api.override = (method, path) =>
      method === "POST" && path === "/api/session/logout"
        ? jsonResponse(500, {
            error: { code: "internal", message: "保存できませんでした" },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Libraries />);
    await libraryItem("世田谷区立中央図書館");
    await user.click(screen.getByRole("button", { name: "ログアウト" }));
    expect(await screen.findByText("保存できませんでした")).toBeInTheDocument();
    expect(await libraryItem("世田谷区立中央図書館")).toBeInTheDocument();
  });
});
