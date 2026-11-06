import { useCallback, useEffect, useState } from "react";
import { useApiErrorHandler } from "../App";
import { ApiError, asArray, asObject, getJSON } from "../api/http";
import { ErrorRetry } from "../components/ErrorRetry";
import { Import } from "./Import";

// 本の画面（contracts/screens.md「本」）。一覧・メモ・検索と登録は US2（T046）で加える。
// いまは、001 のデータの取り込みの欄だけを表示する。

type Counts = { readonly books: number; readonly libraries: number };

interface State {
  /** 最後に読めた件数。読み直しに失敗しても残し、取り込みの欄（と取り込んだ件数の表示）を消さない。 */
  readonly counts: Counts | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
}

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
  const [state, setState] = useState<State>({
    counts: undefined,
    loading: true,
    error: undefined,
  });

  const load = useCallback(() => {
    fetchCounts()
      .then((counts) => {
        setState({ counts, loading: false, error: undefined });
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setState((prev) => ({
          counts: prev.counts,
          loading: false,
          error: e instanceof ApiError ? e.message : "通信に失敗しました",
        }));
      });
  }, [handleError]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <main className="screen books">
      <h1>本</h1>
      {state.loading && state.counts === undefined && <p>読み込んでいます…</p>}
      {state.error !== undefined && (
        <ErrorRetry
          message={state.error}
          onRetry={() => {
            setState((prev) => ({ ...prev, loading: true, error: undefined }));
            load();
          }}
        />
      )}
      {state.counts && (
        <Import
          bookCount={state.counts.books}
          libraryCount={state.counts.libraries}
          onImported={load}
        />
      )}
    </main>
  );
}
