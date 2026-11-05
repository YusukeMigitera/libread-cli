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

const examples = {
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
} satisfies Record<string, unknown>;

/** 例の名前（contracts/examples/<名前>.json）。打ち間違いは型チェックで分かる。 */
export type ExampleName = keyof typeof examples;

// example は name の例を返す。テストで JSON の応答として使うので、毎回複製する。
// 値は unknown として返し、検証関数のテストに渡す。
export function example(name: ExampleName): unknown {
  return structuredClone(examples[name]);
}
