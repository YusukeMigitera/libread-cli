// 読みかけの本（contracts/http-api.md「読みかけの本」）。
import { asISBN, type ISBN } from "./check";
import {
  asArray,
  asBoolean,
  asDate,
  asNumber,
  asObject,
  asOptional,
  asString,
  getJSON,
  noContent,
  sendJSON,
} from "./http";

export interface ReadingBook {
  readonly isbn: ISBN;
  readonly title: string;
  readonly author: string;
  readonly publisher: string;
  readonly note: string;
  /** 登録日（YYYY-MM-DD）。 */
  readonly addedOn: string;
}

/** 書誌の検索結果の1件。 */
export interface BookCandidate {
  readonly isbn: ISBN;
  readonly title: string;
  readonly author: string;
  readonly publisher: string;
  readonly registered: boolean;
}

export interface Link {
  readonly label: string;
  readonly url: string;
}

/** NDL サーチのクレジット。表示とリンクが利用条件で必須（001 research R10）。 */
export interface NDLCredit extends Link {
  readonly license: Link;
}

export interface BookSearch {
  readonly books: readonly BookCandidate[];
  readonly total: number;
  readonly truncated: boolean;
  readonly hint?: string;
  readonly credit: NDLCredit;
}

export interface BookQuery {
  /** 書名または ISBN（必須）。 */
  readonly q: string;
  readonly author?: string;
  readonly publisher?: string;
}

export function decodeBook(u: unknown): ReadingBook {
  const o = asObject(u);
  return {
    isbn: asISBN(o.isbn),
    title: asString(o.title),
    author: asString(o.author),
    publisher: asString(o.publisher),
    note: asString(o.note),
    addedOn: asDate(o.addedOn),
  };
}

/** decodeBooks は GET /api/books の応答を検証する。 */
export function decodeBooks(u: unknown): ReadingBook[] {
  return asArray(asObject(u).books, decodeBook);
}

function decodeLink(u: unknown): Link {
  const o = asObject(u);
  return { label: asString(o.label), url: asString(o.url) };
}

/** decodeBookSearch は GET /api/book-search の応答を検証する。 */
export function decodeBookSearch(u: unknown): BookSearch {
  const o = asObject(u);
  const credit = asObject(o.credit);
  const result: BookSearch = {
    books: asArray(o.books, (b) => {
      const c = asObject(b);
      return {
        isbn: asISBN(c.isbn),
        title: asString(c.title),
        author: asString(c.author),
        publisher: asString(c.publisher),
        registered: asBoolean(c.registered),
      };
    }),
    total: asNumber(o.total),
    truncated: asBoolean(o.truncated),
    credit: { ...decodeLink(credit), license: decodeLink(credit.license) },
  };
  const hint = asOptional(o.hint, asString);
  return hint === undefined ? result : { ...result, hint };
}

export function fetchBooks(): Promise<ReadingBook[]> {
  return getJSON("/api/books", decodeBooks);
}

/** searchBooks は NDL サーチで書誌を探す。空の条件は送らない。 */
export function searchBooks(query: BookQuery): Promise<BookSearch> {
  const params = new URLSearchParams({ q: query.q });
  if (query.author) {
    params.set("author", query.author);
  }
  if (query.publisher) {
    params.set("publisher", query.publisher);
  }
  return getJSON(`/api/book-search?${params.toString()}`, decodeBookSearch);
}

/** addBook は isbn の本を読みかけの本に登録する。メモが空なら送らない。 */
export function addBook(isbn: ISBN, note: string): Promise<ReadingBook> {
  return sendJSON(
    "POST",
    "/api/books",
    note === "" ? { isbn } : { isbn, note },
    decodeBook,
  );
}

/** updateNote はメモを書き換える。空文字ならメモを消す。 */
export function updateNote(isbn: ISBN, note: string): Promise<ReadingBook> {
  return sendJSON("PATCH", bookPath(isbn), { note }, decodeBook);
}

export function removeBook(isbn: ISBN): Promise<undefined> {
  return sendJSON("DELETE", bookPath(isbn), undefined, noContent);
}

function bookPath(isbn: ISBN): string {
  return `/api/books/${encodeURIComponent(isbn)}`;
}
