import { useCallback, useEffect, useState } from "react";
import { useApiErrorHandler } from "../App";
import { ApiError, asArray, asObject, getJSON } from "../api/http";
import { ErrorRetry } from "../components/ErrorRetry";
import { Import } from "./Import";

// 本の画面（contracts/screens.md「本」）。一覧・メモ・検索と登録は US2（T046）で加える。
// いまは、001 のデータの取り込みの欄だけを表示する。

type Counts = { readonly books: number; readonly libraries: number };

type State =
  | { readonly kind: "loading" }
  | { readonly kind: "loaded"; readonly counts: Counts }
  | { readonly kind: "error"; readonly message: string };

/** countOf は一覧の応答（{"books": [...]} など）の件数。 */
function countOf(key: "books" | "libraries") {
  return (u: unknown): number => asArray(asObject(u)[key], (v) => v).length;
}

function fetchCounts(): Promise<Counts> {
  return Promise.all([
    getJSON("/api/books", countOf("books")),
    getJSON("/api/libraries", countOf("libraries")),
  ]).then(([books, libraries]) => ({ books, libraries }));
}

export function Books() {
  const handleError = useApiErrorHandler();
  const [state, setState] = useState<State>({ kind: "loading" });

  const load = useCallback(() => {
    fetchCounts()
      .then((counts) => {
        setState({ kind: "loaded", counts });
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setState({
          kind: "error",
          message: e instanceof ApiError ? e.message : "通信に失敗しました",
        });
      });
  }, [handleError]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <main className="screen books">
      <h1>本</h1>
      {state.kind === "loading" && <p>読み込んでいます…</p>}
      {state.kind === "error" && (
        <ErrorRetry
          message={state.message}
          onRetry={() => {
            setState({ kind: "loading" });
            load();
          }}
        />
      )}
      {state.kind === "loaded" && (
        <Import
          bookCount={state.counts.books}
          libraryCount={state.counts.libraries}
          onImported={load}
        />
      )}
    </main>
  );
}
