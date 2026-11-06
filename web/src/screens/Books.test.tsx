import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "../App";
import { example } from "../test/examples";
import { Books } from "./Books";
import { jsonResponse, pathOf } from "../test/http";

type Obj = Record<string, unknown>;

/** deferred は、あとから応答を決められる fetch の結果。 */
function deferred() {
  let resolve: (r: Response) => void = () => undefined;
  const promise = new Promise<Response>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function readingBook(change: Partial<Obj> = {}): Obj {
  const [b] = (example("books") as { books: Obj[] }).books;
  return { ...b, ...change };
}

const neko = readingBook();
const botchan = readingBook({
  isbn: "9784003101100",
  title: "坊っちゃん",
  author: "",
  publisher: "",
  note: "",
  addedOn: "2026-10-22",
});

interface FakeApi {
  books: Obj[];
  libraries: Obj[];
  search: Obj;
  /** 要求ごとに応答を差し替える。undefined を返したら既定の応答。 */
  override?: (
    method: string,
    path: string,
    body: unknown,
  ) => Response | Promise<Response> | undefined;
}

// stubApi は本の API を、api の中身を使って応答する fetch に差し替える。登録・メモ・削除は api.books を書き換える。
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
    const isbnOf = (p: string) => p.replace("/api/books/", "");
    if (path === "/api/session") {
      return Promise.resolve(jsonResponse(200, example("session")));
    }
    if (path === "/api/books" && method === "GET") {
      return Promise.resolve(jsonResponse(200, { books: api.books }));
    }
    if (path === "/api/libraries") {
      return Promise.resolve(jsonResponse(200, { libraries: api.libraries }));
    }
    if (path.startsWith("/api/book-search?")) {
      return Promise.resolve(jsonResponse(200, api.search));
    }
    if (path === "/api/books" && method === "POST") {
      const req = body as { isbn: string; note?: string };
      const b = readingBook({
        isbn: req.isbn,
        title: "登録した本",
        note: req.note ?? "",
        addedOn: "2026-11-05",
      });
      api.books = [...api.books, b];
      return Promise.resolve(jsonResponse(201, b));
    }
    if (path.startsWith("/api/books/") && method === "PATCH") {
      const note = (body as { note: string }).note;
      api.books = api.books.map((b) =>
        b.isbn === isbnOf(path) ? { ...b, note } : b,
      );
      const b = api.books.find((x) => x.isbn === isbnOf(path));
      return Promise.resolve(jsonResponse(200, b));
    }
    if (path.startsWith("/api/books/") && method === "DELETE") {
      api.books = api.books.filter((b) => b.isbn !== isbnOf(path));
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    return Promise.resolve(
      jsonResponse(404, { error: { code: "notFound", message: "x" } }),
    );
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
    books: [neko, botchan],
    libraries: [],
    search: example("book-search") as Obj,
    ...change,
  };
}

afterEach(() => {
  window.location.hash = "";
});

/** bookItem は一覧の中で title の本の項目。 */
async function bookItem(title: string): Promise<HTMLElement> {
  const list = await screen.findByRole("list", { name: "読みかけの本" });
  const item = within(list)
    .getAllByRole("listitem")
    .find((li) => within(li).queryByText(title) !== null);
  if (!item) {
    throw new Error(`一覧に『${title}』がない`);
  }
  return item;
}

describe("一覧", () => {
  test("書名・著者・出版社・メモ・登録日を表示する", async () => {
    stubApi(newApi());
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    expect(within(item).getByText(/夏目漱石/)).toBeInTheDocument();
    expect(within(item).getByText(/岩波書店/)).toBeInTheDocument();
    expect(within(item).getByText("p.120まで")).toBeInTheDocument();
    expect(within(item).getByText(/2026-10-21/)).toBeInTheDocument();
    expect(await bookItem("坊っちゃん")).toBeInTheDocument();
  });

  test("本がなければその旨を表示する", async () => {
    stubApi(
      newApi({ books: [], libraries: [{ id: "Tokyo_Setagaya:中央図書館" }] }),
    );
    render(<Books />);
    expect(
      await screen.findByText("読みかけの本は登録されていません"),
    ).toBeInTheDocument();
  });
});

describe("メモの編集", () => {
  test("「保存」で PATCH を送り、保存中は押せず、成功したら一覧を更新する", async () => {
    const api = newApi();
    const patch = deferred();
    api.override = (method) => {
      if (method !== "PATCH") {
        return undefined;
      }
      api.override = undefined;
      return patch.promise;
    };
    const { calls } = stubApi(api);
    const user = userEvent.setup();
    render(<Books />);

    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    const input = within(item).getByLabelText("メモ");
    expect(input).toHaveValue("p.120まで");
    await user.clear(input);
    await user.type(input, "p.180まで");
    await user.click(within(item).getByRole("button", { name: "保存" }));

    expect(within(item).getByRole("button", { name: "保存" })).toBeDisabled();
    expect(calls().filter((c) => c.method === "PATCH")).toEqual([
      {
        method: "PATCH",
        path: "/api/books/9784003101018",
        body: { note: "p.180まで" },
      },
    ]);

    patch.resolve(jsonResponse(200, { ...neko, note: "p.180まで" }));
    expect(await within(item).findByText("p.180まで")).toBeInTheDocument();
    expect(within(item).queryByLabelText("メモ")).not.toBeInTheDocument();
  });

  test("「取消」で元に戻し、何も送らない", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    await user.type(within(item).getByLabelText("メモ"), "追記");
    await user.click(within(item).getByRole("button", { name: "取消" }));

    expect(within(item).getByText("p.120まで")).toBeInTheDocument();
    expect(within(item).queryByLabelText("メモ")).not.toBeInTheDocument();
    expect(calls().some((c) => c.method === "PATCH")).toBe(false);
  });

  test("空にして保存するとメモを消す", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    await user.clear(within(item).getByLabelText("メモ"));
    await user.click(within(item).getByRole("button", { name: "保存" }));

    await waitFor(() => {
      expect(within(item).queryByText("p.120まで")).not.toBeInTheDocument();
    });
    expect(calls().find((c) => c.method === "PATCH")?.body).toEqual({
      note: "",
    });
  });

  test("保存に失敗したら、入力を残してメッセージを表示する", async () => {
    const api = newApi();
    api.override = (method) =>
      method === "PATCH"
        ? jsonResponse(500, {
            error: { code: "internal", message: "保存できませんでした" },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    await user.type(within(item).getByLabelText("メモ"), "追記");
    await user.click(within(item).getByRole("button", { name: "保存" }));

    expect(
      await within(item).findByText("保存できませんでした"),
    ).toBeInTheDocument();
    expect(within(item).getByLabelText("メモ")).toHaveValue("p.120まで追記");
  });

  test("本人確認が切れたら入力を退避してログイン画面に切り替え、再ログイン後に戻す（research W12）", async () => {
    window.location.hash = "#/books";
    const api = newApi();
    api.override = (method) =>
      method === "PATCH"
        ? jsonResponse(401, example("error-unauthenticated"))
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    const { unmount } = render(<App />);

    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    const input = within(item).getByLabelText("メモ");
    await user.clear(input);
    await user.type(input, "書きかけのメモ");
    await user.click(within(item).getByRole("button", { name: "保存" }));

    expect(
      await screen.findByRole("link", { name: "Google でログイン" }),
    ).toHaveAttribute("href", "/auth/login?return=/books");
    expect(sessionStorage.length).toBe(1);
    expect(JSON.stringify(Object.values(sessionStorage))).toContain(
      "書きかけのメモ",
    );

    // ログインして #/books に戻ってきた。
    unmount();
    api.override = undefined;
    render(<App />);
    const again = await bookItem("吾輩は猫である");
    expect(await within(again).findByLabelText("メモ")).toHaveValue(
      "書きかけのメモ",
    );
    expect(sessionStorage.length).toBe(0);
  });
});

describe("外す", () => {
  test("確認ダイアログで取り消したら何も送らない（FR-108）", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "外す" }));

    expect(confirm).toHaveBeenCalledWith("『吾輩は猫である』を外しますか？");
    expect(calls().some((c) => c.method === "DELETE")).toBe(false);
    expect(await bookItem("吾輩は猫である")).toBeInTheDocument();
  });

  test("確認したら DELETE を送り、一覧から外す", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "外す" }));

    await waitFor(() => {
      expect(screen.queryByText("吾輩は猫である")).not.toBeInTheDocument();
    });
    expect(calls().filter((c) => c.method === "DELETE")).toEqual([
      { method: "DELETE", path: "/api/books/9784003101018", body: undefined },
    ]);
    expect(await bookItem("坊っちゃん")).toBeInTheDocument();
  });
});

describe("本を探す", () => {
  function searchResult(): Obj {
    const u = example("book-search") as Obj;
    const books = u.books as Obj[];
    u.books = [
      ...books,
      {
        isbn: "9784003101100",
        title: "坊っちゃん",
        author: "夏目漱石",
        publisher: "岩波書店",
        registered: true,
      },
    ];
    return u;
  }

  test("「書名または ISBN」は必須で、空なら探さない", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    const q = await screen.findByLabelText("書名または ISBN");
    expect(q).toBeRequired();
    await user.click(screen.getByRole("button", { name: "探す" }));
    expect(calls().some((c) => c.path.startsWith("/api/book-search"))).toBe(
      false,
    );
  });

  test("結果に書名・著者・出版社・ISBN と「登録」、登録済みは押せない。hint とクレジットを表示する", async () => {
    const { calls } = stubApi(
      newApi({ books: [botchan], search: searchResult() }),
    );
    const user = userEvent.setup();
    render(<Books />);
    await user.type(
      await screen.findByLabelText("書名または ISBN"),
      "吾輩は猫である",
    );
    await user.type(screen.getByLabelText("著者"), "夏目漱石");
    await user.click(screen.getByRole("button", { name: "探す" }));

    const results = await screen.findByRole("list", { name: "検索結果" });
    const items = within(results).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    const [first, second] = items as [HTMLElement, HTMLElement];
    expect(within(first).getByText("吾輩は猫である")).toBeInTheDocument();
    expect(within(first).getByText(/夏目漱石/)).toBeInTheDocument();
    expect(within(first).getByText(/岩波書店/)).toBeInTheDocument();
    expect(within(first).getByText(/9784003101018/)).toBeInTheDocument();
    expect(within(first).getByRole("button", { name: "登録" })).toBeEnabled();
    expect(
      within(second).getByRole("button", { name: "登録済み" }),
    ).toBeDisabled();

    expect(
      screen.getByText(
        "表示しきれていない本があります。見つからないときは著者や出版社で絞り込むか、ISBN で探してください",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", {
        name: "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています",
      }),
    ).toHaveAttribute("href", "https://ndlsearch.ndl.go.jp/");
    expect(
      screen.getByRole("link", {
        name: "クリエイティブ・コモンズ 表示 4.0 国際",
      }),
    ).toHaveAttribute(
      "href",
      "https://creativecommons.org/licenses/by/4.0/legalcode.ja",
    );

    const search = calls().find((c) => c.path.startsWith("/api/book-search"));
    const url = new URL(search?.path ?? "", "http://x");
    expect(url.searchParams.get("q")).toBe("吾輩は猫である");
    expect(url.searchParams.get("author")).toBe("夏目漱石");
    expect(url.searchParams.has("publisher")).toBe(false);
  });

  test("0件なら「見つかりませんでした」と指定した条件を表示する", async () => {
    const empty = example("book-search") as Obj;
    empty.books = [];
    empty.total = 0;
    empty.truncated = false;
    delete empty.hint;
    stubApi(newApi({ search: empty }));
    const user = userEvent.setup();
    render(<Books />);
    await user.type(
      await screen.findByLabelText("書名または ISBN"),
      "存在しない本",
    );
    await user.type(screen.getByLabelText("出版社"), "岩波書店");
    await user.click(screen.getByRole("button", { name: "探す" }));

    const msg = await screen.findByText(/見つかりませんでした/);
    expect(msg).toHaveTextContent("「存在しない本」");
    expect(msg).toHaveTextContent("出版社: 岩波書店");
    expect(msg).not.toHaveTextContent("著者");
  });

  test("「登録」でメモ（任意）を入力してから POST し、一覧に加えて「登録済み」にする", async () => {
    const api = newApi({ books: [botchan], search: searchResult() });
    const { calls } = stubApi(api);
    const user = userEvent.setup();
    render(<Books />);
    await user.type(
      await screen.findByLabelText("書名または ISBN"),
      "吾輩は猫である",
    );
    await user.click(screen.getByRole("button", { name: "探す" }));
    const results = await screen.findByRole("list", { name: "検索結果" });
    const [first] = within(results).getAllByRole("listitem") as [HTMLElement];
    await user.click(within(first).getByRole("button", { name: "登録" }));

    // メモの入力欄を出してから登録する。まだ送らない。
    expect(calls().some((c) => c.method === "POST")).toBe(false);
    await user.type(within(first).getByLabelText("メモ（任意）"), "p.1から");
    await user.click(within(first).getByRole("button", { name: "登録する" }));

    expect(
      await within(first).findByRole("button", { name: "登録済み" }),
    ).toBeDisabled();
    expect(calls().filter((c) => c.method === "POST")).toEqual([
      {
        method: "POST",
        path: "/api/books",
        body: { isbn: "9784003101018", note: "p.1から" },
      },
    ]);
    expect(await bookItem("登録した本")).toBeInTheDocument();
  });

  test("検索に失敗したら、メッセージと「再試行」", async () => {
    const api = newApi();
    let fail = true;
    api.override = (_, path) =>
      path.startsWith("/api/book-search") && fail
        ? jsonResponse(502, {
            error: {
              code: "upstream",
              message: "国立国会図書館サーチに接続できませんでした",
            },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);
    await user.type(await screen.findByLabelText("書名または ISBN"), "x");
    await user.click(screen.getByRole("button", { name: "探す" }));
    expect(
      await screen.findByText("国立国会図書館サーチに接続できませんでした"),
    ).toBeInTheDocument();

    fail = false;
    await user.click(screen.getByRole("button", { name: "再試行" }));
    expect(
      await screen.findByRole("list", { name: "検索結果" }),
    ).toBeInTheDocument();
  });
});

describe("取り込み（T040）", () => {
  test("本も図書館もなければ取り込みの欄を出し、取り込んだら一覧を読み直して欄を消す", async () => {
    const api = newApi({ books: [] });
    api.override = (method, path) => {
      if (path === "/api/import" && method === "POST") {
        api.books = [neko];
        api.libraries = [{ id: "Tokyo_Setagaya:中央図書館" }];
        return jsonResponse(200, example("import"));
      }
      return undefined;
    };
    const { calls } = stubApi(api);
    const user = userEvent.setup();
    render(<Books />);

    const input = await screen.findByLabelText("CLI のデータを取り込む");
    await user.upload(
      input,
      new File(['{"version": 1}'], "data.json", { type: "application/json" }),
    );

    expect(
      await screen.findByText(
        "取り込みました（本 3冊・図書館 4館・臨時休館 2件）",
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText("CLI のデータを取り込む"),
    ).not.toBeInTheDocument();
    expect(await bookItem("吾輩は猫である")).toBeInTheDocument();
    expect(
      calls().filter((c) => c.path === "/api/books" && c.method === "GET"),
    ).toHaveLength(2);
  });

  test("一覧を読めなければ、メッセージと「再試行」", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(() =>
        Promise.resolve(
          jsonResponse(500, {
            error: { code: "internal", message: "保存先を読めませんでした" },
          }),
        ),
      ),
    );
    render(<Books />);
    expect(
      await screen.findByText("保存先を読めませんでした"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "再試行" })).toBeInTheDocument();
  });

  test("取り込んだあとの読み直しに失敗しても、取り込んだ件数の表示は残す", async () => {
    const api = newApi({ books: [] });
    let imported = false;
    api.override = (method, path) => {
      if (path === "/api/import" && method === "POST") {
        imported = true;
        return jsonResponse(200, example("import"));
      }
      return imported
        ? jsonResponse(500, {
            error: { code: "internal", message: "保存先を読めませんでした" },
          })
        : undefined;
    };
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);
    await user.upload(
      await screen.findByLabelText("CLI のデータを取り込む"),
      new File(['{"version": 1}'], "data.json", { type: "application/json" }),
    );

    expect(
      await screen.findByText("保存先を読めませんでした"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("取り込みました（本 3冊・図書館 4館・臨時休館 2件）"),
    ).toBeInTheDocument();
  });
});

describe("レビューでの指摘", () => {
  test("保存中に別の本のメモを編集し始めても、保存が終わったときにその入力を消さない", async () => {
    const api = newApi();
    const patch = deferred();
    api.override = (method) => {
      if (method !== "PATCH") {
        return undefined;
      }
      api.override = undefined;
      return patch.promise;
    };
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);

    const neko = await bookItem("吾輩は猫である");
    await user.click(within(neko).getByRole("button", { name: "メモを編集" }));
    await user.type(within(neko).getByLabelText("メモ"), "追記");
    await user.click(within(neko).getByRole("button", { name: "保存" }));

    const botchanItem = await bookItem("坊っちゃん");
    await user.click(
      within(botchanItem).getByRole("button", { name: "メモを編集" }),
    );
    await user.type(within(botchanItem).getByLabelText("メモ"), "書きかけ");

    patch.resolve(
      jsonResponse(200, { ...readingBook(), note: "p.120まで追記" }),
    );
    expect(await within(neko).findByText("p.120まで追記")).toBeInTheDocument();
    expect(within(botchanItem).getByLabelText("メモ")).toHaveValue("書きかけ");
  });

  test("保存中はメモを書き換えられず、「保存」も押せないまま", async () => {
    const api = newApi();
    const patch = deferred();
    api.override = (method) => (method === "PATCH" ? patch.promise : undefined);
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);

    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    const input = within(item).getByLabelText("メモ");
    await user.click(within(item).getByRole("button", { name: "保存" }));
    await user.type(input, "さらに");

    expect(input).toHaveValue("p.120まで");
    expect(within(item).getByRole("button", { name: "保存" })).toBeDisabled();
    patch.resolve(jsonResponse(200, readingBook()));
  });

  test("登録で本人確認が切れたら、検索条件とメモを退避し、再ログイン後に戻す（research W12）", async () => {
    window.location.hash = "#/books";
    const api = newApi({ books: [botchan] });
    api.override = (method) =>
      method === "POST"
        ? jsonResponse(401, example("error-unauthenticated"))
        : undefined;
    const { calls } = stubApi(api);
    const user = userEvent.setup();
    const { unmount } = render(<App />);

    await user.type(
      await screen.findByLabelText("書名または ISBN"),
      "吾輩は猫である",
    );
    await user.type(screen.getByLabelText("著者"), "夏目漱石");
    await user.click(screen.getByRole("button", { name: "探す" }));
    const results = await screen.findByRole("list", { name: "検索結果" });
    const [first] = within(results).getAllByRole("listitem") as [HTMLElement];
    await user.click(within(first).getByRole("button", { name: "登録" }));
    await user.type(within(first).getByLabelText("メモ（任意）"), "p.1から");
    await user.click(within(first).getByRole("button", { name: "登録する" }));

    expect(
      await screen.findByRole("link", { name: "Google でログイン" }),
    ).toBeInTheDocument();
    expect(sessionStorage.length).toBe(1);

    unmount();
    api.override = undefined;
    render(<App />);
    const again = await screen.findByRole("list", { name: "検索結果" });
    const [restored] = within(again).getAllByRole("listitem") as [HTMLElement];
    expect(within(restored).getByLabelText("メモ（任意）")).toHaveValue(
      "p.1から",
    );
    expect(screen.getByLabelText("書名または ISBN")).toHaveValue(
      "吾輩は猫である",
    );
    expect(screen.getByLabelText("著者")).toHaveValue("夏目漱石");
    expect(sessionStorage.length).toBe(0);
    const searches = calls().filter((c) =>
      c.path.startsWith("/api/book-search"),
    );
    expect(searches).toHaveLength(2);
    expect(searches[1]?.path).toBe(searches[0]?.path);
  });

  test("空白だけの「書名または ISBN」では探さず、入力を促す", async () => {
    const { calls } = stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    await user.type(await screen.findByLabelText("書名または ISBN"), "   ");
    await user.click(screen.getByRole("button", { name: "探す" }));

    expect(
      screen.getByText("書名または ISBN を入力してください"),
    ).toBeInTheDocument();
    expect(calls().some((c) => c.path.startsWith("/api/book-search"))).toBe(
      false,
    );
  });

  test("外しているあいだは「外す」を押せない。ほかの端末で外し済み（404）なら一覧から外す", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const api = newApi();
    const del = deferred();
    api.override = (method) => (method === "DELETE" ? del.promise : undefined);
    const { calls } = stubApi(api);
    const user = userEvent.setup();
    render(<Books />);

    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "外す" }));
    expect(within(item).getByRole("button", { name: "外す" })).toBeDisabled();

    del.resolve(
      jsonResponse(404, {
        error: {
          code: "notFound",
          message: "この本は読みかけの本に登録されていません",
        },
      }),
    );
    await waitFor(() => {
      expect(screen.queryByText("吾輩は猫である")).not.toBeInTheDocument();
    });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(calls().filter((c) => c.method === "DELETE")).toHaveLength(1);
  });
});

describe("修正のレビューでの指摘", () => {
  test("保存中は「取消」も押せない", async () => {
    const api = newApi();
    const patch = deferred();
    api.override = (method) => (method === "PATCH" ? patch.promise : undefined);
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);
    const item = await bookItem("吾輩は猫である");
    await user.click(within(item).getByRole("button", { name: "メモを編集" }));
    await user.click(within(item).getByRole("button", { name: "保存" }));
    expect(within(item).getByRole("button", { name: "取消" })).toBeDisabled();
    patch.resolve(jsonResponse(200, readingBook()));
    await waitFor(() => {
      expect(within(item).queryByLabelText("メモ")).not.toBeInTheDocument();
    });
  });

  test("登録中は「取消」も押せない", async () => {
    const api = newApi({ books: [botchan] });
    const post = deferred();
    api.override = (method) => (method === "POST" ? post.promise : undefined);
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);
    await user.type(await screen.findByLabelText("書名または ISBN"), "猫");
    await user.click(screen.getByRole("button", { name: "探す" }));
    const results = await screen.findByRole("list", { name: "検索結果" });
    const [first] = within(results).getAllByRole("listitem") as [HTMLElement];
    await user.click(within(first).getByRole("button", { name: "登録" }));
    await user.click(within(first).getByRole("button", { name: "登録する" }));
    expect(within(first).getByRole("button", { name: "取消" })).toBeDisabled();
    post.resolve(jsonResponse(201, readingBook()));
    expect(
      await within(first).findByRole("button", { name: "登録済み" }),
    ).toBeDisabled();
  });

  test("入力し直したら、空白だけで探したときのメッセージを消す", async () => {
    stubApi(newApi());
    const user = userEvent.setup();
    render(<Books />);
    const q = await screen.findByLabelText("書名または ISBN");
    await user.type(q, " ");
    await user.click(screen.getByRole("button", { name: "探す" }));
    expect(
      screen.getByText("書名または ISBN を入力してください"),
    ).toBeInTheDocument();
    await user.type(q, "猫");
    expect(
      screen.queryByText("書名または ISBN を入力してください"),
    ).not.toBeInTheDocument();
  });

  test("登録しかけた本の検索のやり直しに失敗しても、退避したメモは残し、再試行で戻す", async () => {
    sessionStorage.setItem(
      "libread:draft",
      JSON.stringify({
        kind: "add",
        query: { q: "吾輩は猫である", author: "", publisher: "" },
        isbn: "9784003101018",
        note: "p.1から",
      }),
    );
    const api = newApi({ books: [botchan] });
    let fail = true;
    api.override = (_, path) =>
      path.startsWith("/api/book-search") && fail
        ? jsonResponse(502, {
            error: { code: "upstream", message: "接続できませんでした" },
          })
        : undefined;
    stubApi(api);
    const user = userEvent.setup();
    render(<Books />);

    expect(await screen.findByText("接続できませんでした")).toBeInTheDocument();
    expect(sessionStorage.length).toBe(1);

    fail = false;
    await user.click(screen.getByRole("button", { name: "再試行" }));
    const results = await screen.findByRole("list", { name: "検索結果" });
    const [first] = within(results).getAllByRole("listitem") as [HTMLElement];
    expect(within(first).getByLabelText("メモ（任意）")).toHaveValue("p.1から");
    expect(sessionStorage.length).toBe(0);
  });
});
