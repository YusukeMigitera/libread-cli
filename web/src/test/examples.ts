// API の応答の例（specs/002-web-mobile-access/contracts/examples）。サーバーのテストも同じファイルを使う（Constitution 原則 III）。
// 値は unknown として扱い、検証関数のテストに渡す。
import bookSearch from "../../../specs/002-web-mobile-access/contracts/examples/book-search.json";
import books from "../../../specs/002-web-mobile-access/contracts/examples/books.json";
import checkSetupBooks from "../../../specs/002-web-mobile-access/contracts/examples/check-setup-books.json";
import checkSetupLibraries from "../../../specs/002-web-mobile-access/contracts/examples/check-setup-libraries.json";
import check from "../../../specs/002-web-mobile-access/contracts/examples/check.json";
import errorUnauthenticated from "../../../specs/002-web-mobile-access/contracts/examples/error-unauthenticated.json";
import importResult from "../../../specs/002-web-mobile-access/contracts/examples/import.json";
import libraries from "../../../specs/002-web-mobile-access/contracts/examples/libraries.json";
import librarySearch from "../../../specs/002-web-mobile-access/contracts/examples/library-search.json";
import session from "../../../specs/002-web-mobile-access/contracts/examples/session.json";

export const examples: Readonly<Record<string, unknown>> = {
  session,
  check,
  "check-setup-libraries": checkSetupLibraries,
  "check-setup-books": checkSetupBooks,
  books,
  "book-search": bookSearch,
  libraries,
  "library-search": librarySearch,
  import: importResult,
  "error-unauthenticated": errorUnauthenticated,
};

// example は name の例を返す。テストで JSON の応答として使うので、毎回複製する。
export function example(name: keyof typeof examples): unknown {
  const value = examples[name];
  if (value === undefined) {
    throw new Error(`example ${name} がありません`);
  }
  return structuredClone(value);
}
