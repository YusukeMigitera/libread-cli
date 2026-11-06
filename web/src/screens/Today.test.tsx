import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { example } from "../test/examples";
import { Today } from "./Today";

type Obj = Record<string, unknown>;

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8" },
  });
}

function upstreamError(): Response {
  return jsonResponse(502, {
    error: { code: "upstream", message: "カーリルに接続できませんでした" },
  });
}

/** checkResult は check.json を元に、checkedAt と館を差し替えた応答を作る。 */
function checkResult(change: (u: Obj) => void = () => undefined): Obj {
  const u = example("check") as Obj;
  change(u);
  return u;
}

function library(name: string, label: string, books: Obj[] = []): Obj {
  return {
    library: { id: `Sys:${name}`, name },
    opening: {
      status: "open",
      label,
      open: "09:00",
      close: "19:00",
      closingSoon: false,
    },
    books,
  };
}

function book(
  isbn: string,
  title: string,
  label: string,
  readable: boolean,
): Obj {
  return {
    isbn,
    title,
    availability: readable ? "availableToLend" : "onLoan",
    label,
    readable,
    stale: false,
    fetchedAt: "2026-10-21T14:04:50+09:00",
  };
}

/** deferred は、あとから応答を決められる fetch の結果。 */
function deferred() {
  let resolve: (r: Response) => void = () => undefined;
  const promise = new Promise<Response>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

// stubCheck は GET /api/check への応答を、呼ばれた順に responses から返す。
function stubCheck(...responses: (() => Response | Promise<Response>)[]) {
  let i = 0;
  const fn = vi.fn<typeof fetch>(() => {
    const next = responses[Math.min(i, responses.length - 1)];
    i++;
    if (!next) {
      throw new Error("応答がない");
    }
    return Promise.resolve(next());
  });
  vi.stubGlobal("fetch", fn);
  return {
    fn,
    paths: () => fn.mock.calls.map(([input]) => input),
  };
}

test("取得中は「所蔵状況を取得しています…」だけを表示する（FR-104）", async () => {
  const pending = deferred();
  stubCheck(() => pending.promise);
  render(<Today />);
  expect(
    await screen.findByText("所蔵状況を取得しています…"),
  ).toBeInTheDocument();
  expect(screen.queryByText("今日読める図書館")).not.toBeInTheDocument();
  expect(screen.queryByText(/時点/)).not.toBeInTheDocument();
  await act(async () => {
    pending.resolve(jsonResponse(200, example("check")));
    await pending.promise;
  });
  expect(await screen.findByText("今日読める図書館")).toBeInTheDocument();
  expect(
    screen.queryByText("所蔵状況を取得しています…"),
  ).not.toBeInTheDocument();
});

test("結果: 基準時刻・読める館（番号付き）・読めない館・出典を API の順のまま表示する", async () => {
  const api = stubCheck(() =>
    jsonResponse(
      200,
      checkResult((u) => {
        u.readable = [
          library("世田谷区立中央図書館", "開館中 〜19:00", [
            book("9784003101018", "吾輩は猫である", "在架（貸出可）", true),
          ]),
          library("世田谷区立奥沢図書館", "開館前 15:00〜20:00", [
            book(
              "9784003101018",
              "吾輩は猫である",
              "在架（館内閲覧のみ）",
              true,
            ),
          ]),
        ];
        u.notReadable = [
          library("世田谷区立烏山図書館", "開館中 〜19:00", [
            book("9784003101018", "吾輩は猫である", "貸出中", false),
          ]),
          {
            library: { id: "Sys:経堂", name: "世田谷区立経堂図書館" },
            opening: { status: "noSchedule", label: "開館情報未入力" },
            books: [],
          },
        ];
      }),
    ),
  );
  render(<Today />);

  expect(
    await screen.findByText(/10月21日\(水\) 14:05 時点/),
  ).toBeInTheDocument();
  expect(api.paths()).toEqual(["/api/check"]);

  const readable = screen.getByRole("region", { name: "今日読める図書館" });
  const readableHeadings = within(readable).getAllByRole("heading", {
    level: 3,
  });
  expect(readableHeadings.map((h) => h.textContent)).toEqual([
    "1. 世田谷区立中央図書館",
    "2. 世田谷区立奥沢図書館",
  ]);
  const [first, second] = readableHeadings.map((h) => {
    const li = h.closest("li");
    if (!li) {
      throw new Error("館が一覧の項目でない");
    }
    return li;
  });
  expect(
    within(first as HTMLElement).getByText("開館中 〜19:00"),
  ).toBeInTheDocument();
  expect(
    within(second as HTMLElement).getByText("在架（館内閲覧のみ）"),
  ).toBeInTheDocument();

  const notReadable = screen.getByRole("region", {
    name: "今日は読めない図書館",
  });
  const headings = within(notReadable)
    .getAllByRole("heading", { level: 3 })
    .map((h) => h.textContent);
  expect(headings).toEqual(["世田谷区立烏山図書館", "世田谷区立経堂図書館"]);
  expect(within(notReadable).getByText("貸出中")).toBeInTheDocument();
  expect(within(notReadable).getByText("開館情報未入力")).toBeInTheDocument();

  expect(
    screen.getByText(
      "所蔵情報: カーリル 14:04 取得（30分以内の取得結果を使用）",
    ),
  ).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "吾輩は猫である" })).toHaveAttribute(
    "href",
    "https://calil.jp/book/9784003101018",
  );
  expect(
    screen.getByText("開館情報: 利用者入力（最終更新 中央図書館 2026-10-21）"),
  ).toBeInTheDocument();
  expect(
    screen.getByText("※ 臨時休館は入力済みの情報のみ反映しています"),
  ).toBeInTheDocument();
  expect(
    screen.getByText("祝日: holidays-jp（https://holidays-jp.github.io/）"),
  ).toBeInTheDocument();
});

test("読める館がなければ「今日読める図書館はありません」", async () => {
  stubCheck(() =>
    jsonResponse(
      200,
      checkResult((u) => {
        u.readable = [];
      }),
    ),
  );
  render(<Today />);
  expect(
    await screen.findByText("今日読める図書館はありません"),
  ).toBeInTheDocument();
});

test("「更新」は前の結果を隠して取得し直し、新しい基準時刻を表示する（FR-105）", async () => {
  const second = deferred();
  const api = stubCheck(
    () => jsonResponse(200, example("check")),
    () => second.promise,
  );
  const user = userEvent.setup();
  render(<Today />);
  await screen.findByText(/14:05 時点/);

  await user.click(screen.getByRole("button", { name: "更新" }));
  expect(screen.getByText("所蔵状況を取得しています…")).toBeInTheDocument();
  expect(screen.queryByText(/14:05 時点/)).not.toBeInTheDocument();
  expect(screen.queryByText("世田谷区立中央図書館")).not.toBeInTheDocument();

  await act(async () => {
    second.resolve(
      jsonResponse(
        200,
        checkResult((u) => {
          u.checkedAt = "2026-10-21T14:35:00+09:00";
        }),
      ),
    );
    await second.promise;
  });
  expect(
    await screen.findByText(/10月21日\(水\) 14:35 時点/),
  ).toBeInTheDocument();
  expect(api.paths()).toEqual(["/api/check", "/api/check"]);
});

test("基準時刻から30分を超えたら、取得し直さずに「古い情報」と「更新してください」を表示する", async () => {
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  vi.setSystemTime(new Date("2026-10-21T14:05:00+09:00"));
  const api = stubCheck(() => jsonResponse(200, example("check")));
  render(<Today />);
  await screen.findByText(/14:05 時点/);

  act(() => {
    vi.advanceTimersByTime(30 * 60_000);
  });
  expect(screen.queryByText("古い情報")).not.toBeInTheDocument();
  expect(screen.queryByText(/更新してください/)).not.toBeInTheDocument();

  act(() => {
    vi.advanceTimersByTime(60_000);
  });
  expect(screen.getByText("古い情報")).toBeInTheDocument();
  expect(screen.getByText(/更新してください/)).toBeInTheDocument();
  expect(screen.getByText(/14:05 時点/)).toBeInTheDocument();
  expect(api.fn).toHaveBeenCalledTimes(1);
});

test("失敗したらメッセージと「再試行」を表示し、前の結果は基準時刻とともに残す（FR-116）", async () => {
  const api = stubCheck(
    () => jsonResponse(200, example("check")),
    upstreamError,
    () => jsonResponse(200, example("check")),
  );
  const user = userEvent.setup();
  render(<Today />);
  await screen.findByText(/14:05 時点/);

  await user.click(screen.getByRole("button", { name: "更新" }));
  expect(
    await screen.findByText("カーリルに接続できませんでした"),
  ).toBeInTheDocument();
  expect(screen.getByText(/14:05 時点/)).toBeInTheDocument();
  expect(screen.getByText("世田谷区立中央図書館")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "再試行" }));
  await screen.findByText(/14:05 時点/);
  expect(
    screen.queryByText("カーリルに接続できませんでした"),
  ).not.toBeInTheDocument();
  expect(api.paths()).toHaveLength(3);
});

test("初めの取得に失敗したら、メッセージと「再試行」だけを表示する", async () => {
  stubCheck(
    () => Promise.reject(new TypeError("Failed to fetch")),
    () => jsonResponse(200, example("check")),
  );
  const user = userEvent.setup();
  render(<Today />);
  expect(await screen.findByText("通信に失敗しました")).toBeInTheDocument();
  expect(screen.queryByText(/時点/)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "再試行" }));
  expect(await screen.findByText(/14:05 時点/)).toBeInTheDocument();
});

test("利用図書館がなければ、登録の案内と図書館の画面へのリンク", async () => {
  stubCheck(() => jsonResponse(200, example("check-setup-libraries")));
  render(<Today />);
  expect(
    await screen.findByText("利用図書館が登録されていません"),
  ).toBeInTheDocument();
  expect(screen.getByRole("link", { name: /図書館/ })).toHaveAttribute(
    "href",
    "#/libraries",
  );
});

test("読みかけの本がなければ、登録の案内と本の画面へのリンク", async () => {
  stubCheck(() => jsonResponse(200, example("check-setup-books")));
  render(<Today />);
  expect(
    await screen.findByText("読みかけの本が登録されていません"),
  ).toBeInTheDocument();
  expect(screen.getByRole("link", { name: /本/ })).toHaveAttribute(
    "href",
    "#/books",
  );
});

test("本が2冊以上なら本を選べ、選ぶと ?isbn= で取得し直す（US1 シナリオ5）", async () => {
  const twoBooks = checkResult((u) => {
    u.readable = [
      library("世田谷区立中央図書館", "開館中 〜19:00", [
        book("9784003101018", "吾輩は猫である", "在架（貸出可）", true),
        book("9784003101100", "坊っちゃん", "貸出中", false),
      ]),
    ];
  });
  const onlyBotchan = checkResult((u) => {
    u.readable = [
      library("世田谷区立中央図書館", "開館中 〜19:00", [
        book("9784003101100", "坊っちゃん", "在架（貸出可）", true),
      ]),
    ];
  });
  const api = stubCheck(
    () => jsonResponse(200, twoBooks),
    () => jsonResponse(200, onlyBotchan),
    () => jsonResponse(200, twoBooks),
  );
  const user = userEvent.setup();
  render(<Today />);
  const select = await screen.findByRole("combobox", { name: "調べる本" });
  expect(
    within(select)
      .getAllByRole("option")
      .map((o) => o.textContent),
  ).toEqual(["すべての本", "吾輩は猫である", "坊っちゃん"]);

  await user.selectOptions(select, "坊っちゃん");
  await screen.findByText("在架（貸出可）");
  expect(api.paths()).toEqual(["/api/check", "/api/check?isbn=9784003101100"]);
  // 1冊に絞っても、選べる本は変わらない。
  expect(
    within(screen.getByRole("combobox", { name: "調べる本" }))
      .getAllByRole("option")
      .map((o) => o.textContent),
  ).toEqual(["すべての本", "吾輩は猫である", "坊っちゃん"]);

  await user.selectOptions(
    screen.getByRole("combobox", { name: "調べる本" }),
    "すべての本",
  );
  await screen.findByText("貸出中");
  expect(api.paths()[2]).toBe("/api/check");
});

test("本が1冊なら本の選択は表示しない", async () => {
  stubCheck(() => jsonResponse(200, example("check")));
  render(<Today />);
  await screen.findByText(/14:05 時点/);
  expect(
    screen.queryByRole("combobox", { name: "調べる本" }),
  ).not.toBeInTheDocument();
});
