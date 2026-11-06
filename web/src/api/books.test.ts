import { example } from "../test/examples";
import { jsonResponse, pathOf } from "../test/http";
import {
  addBook,
  decodeBookSearch,
  decodeBooks,
  fetchBooks,
  removeBook,
  searchBooks,
  updateNote,
} from "./books";
import { asISBN } from "./check";

type Obj = Record<string, unknown>;

function firstBook(u: unknown): Obj {
  const books = (u as { books: Obj[] }).books;
  const [b] = books;
  if (b === undefined) {
    throw new Error("例に本がない");
  }
  return b;
}

const isbn = asISBN("9784003101018");

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

describe("decodeBooks", () => {
  test("一覧の例を受け付ける", () => {
    expect(decodeBooks(example("books"))).toEqual([
      {
        isbn: "9784003101018",
        title: "吾輩は猫である",
        author: "夏目漱石",
        publisher: "岩波書店",
        note: "p.120まで",
        addedOn: "2026-10-21",
      },
    ]);
  });

  test("空の一覧を受け付ける", () => {
    expect(decodeBooks({ books: [] })).toEqual([]);
  });

  test.each<[string, (b: Obj) => void]>([
    ["ISBN が13桁でない", (b) => (b.isbn = "4003101014")],
    ["書名がない", (b) => delete b.title],
    ["メモが文字列でない", (b) => (b.note = null)],
    ["登録日が日付でない", (b) => (b.addedOn = "10月21日")],
  ])("形が違えば例外を投げる: %s", (_, mutate) => {
    const u = example("books");
    mutate(firstBook(u));
    expect(() => decodeBooks(u)).toThrow();
  });
});

describe("decodeBookSearch", () => {
  test("検索結果の例を受け付ける", () => {
    expect(decodeBookSearch(example("book-search"))).toEqual({
      books: [
        {
          isbn: "9784003101018",
          title: "吾輩は猫である",
          author: "夏目漱石",
          publisher: "岩波書店",
          registered: false,
        },
      ],
      total: 345,
      truncated: true,
      hint: "表示しきれていない本があります。見つからないときは著者や出版社で絞り込むか、ISBN で探してください",
      credit: {
        label:
          "書誌情報: 国立国会図書館サーチ API から取得した「国立国会図書館蔵書」（国立国会図書館が運営）のデータを利用しています",
        url: "https://ndlsearch.ndl.go.jp/",
        license: {
          label: "クリエイティブ・コモンズ 表示 4.0 国際",
          url: "https://creativecommons.org/licenses/by/4.0/legalcode.ja",
        },
      },
    });
  });

  test("hint がなければ hint を持たない", () => {
    const u = example("book-search") as Obj;
    delete u.hint;
    expect(decodeBookSearch(u)).not.toHaveProperty("hint");
  });

  test.each<[string, (o: Obj) => void]>([
    ["registered がない", (o) => delete firstBook(o).registered],
    ["total が数でない", (o) => (o.total = "345")],
    ["credit のライセンスがない", (o) => delete (o.credit as Obj).license],
    ["hint が文字列でない", (o) => (o.hint = true)],
  ])("形が違えば例外を投げる: %s", (_, mutate) => {
    const u = example("book-search") as Obj;
    mutate(u);
    expect(() => decodeBookSearch(u)).toThrow();
  });
});

describe("要求", () => {
  test("fetchBooks は GET /api/books", async () => {
    const fetchMock = stubFetch(() => jsonResponse(200, example("books")));
    const books = await fetchBooks();
    expect(books).toHaveLength(1);
    expect(pathOf(fetchMock.mock.calls[0]?.[0] ?? "")).toBe("/api/books");
  });

  test("searchBooks は指定した条件だけをクエリーに付ける", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(200, example("book-search")),
    );
    const res = await searchBooks({ q: "吾輩は猫である", author: "夏目漱石" });
    expect(res.total).toBe(345);
    const url = new URL(pathOf(fetchMock.mock.calls[0]?.[0] ?? ""), "http://x");
    expect(url.pathname).toBe("/api/book-search");
    expect(url.searchParams.get("q")).toBe("吾輩は猫である");
    expect(url.searchParams.get("author")).toBe("夏目漱石");
    expect(url.searchParams.has("publisher")).toBe(false);
  });

  test("addBook は POST /api/books に ISBN とメモを送り、登録した本を返す", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(201, firstBook(example("books"))),
    );
    const book = await addBook(isbn, "p.120まで");
    expect(book.note).toBe("p.120まで");
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe("/api/books");
    expect(init?.method).toBe("POST");
    expect(jsonBody(init)).toEqual({
      isbn: "9784003101018",
      note: "p.120まで",
    });
  });

  test("addBook はメモが空なら note を送らない", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(201, firstBook(example("books"))),
    );
    await addBook(isbn, "");
    const [, init] = fetchMock.mock.calls[0] ?? [];
    expect(jsonBody(init)).toEqual({ isbn: "9784003101018" });
  });

  test("updateNote は PATCH /api/books/{isbn} でメモを送る", async () => {
    const fetchMock = stubFetch(() =>
      jsonResponse(200, { ...firstBook(example("books")), note: "" }),
    );
    const book = await updateNote(isbn, "");
    expect(book.note).toBe("");
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe("/api/books/9784003101018");
    expect(init?.method).toBe("PATCH");
    expect(jsonBody(init)).toEqual({ note: "" });
  });

  test("removeBook は DELETE /api/books/{isbn}", async () => {
    const fetchMock = stubFetch(() => new Response(null, { status: 204 }));
    await expect(removeBook(isbn)).resolves.toBeUndefined();
    const [input, init] = fetchMock.mock.calls[0] ?? [];
    expect(input).toBe("/api/books/9784003101018");
    expect(init?.method).toBe("DELETE");
  });
});
