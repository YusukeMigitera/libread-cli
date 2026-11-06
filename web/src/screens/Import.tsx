import { useId, useState } from "react";
import { useApiErrorHandler } from "../App";
import { ApiError } from "../api/http";
import { importData, type ImportResult } from "../api/import";

interface Props {
  /** 今の読みかけの本の冊数。 */
  bookCount: number;
  /** 今の利用図書館の館数。 */
  libraryCount: number;
  /** 取り込みが済んだら呼ぶ。本の画面は一覧を読み直す。 */
  onImported: () => void;
}

type State =
  | { readonly kind: "idle" }
  | { readonly kind: "sending" }
  | { readonly kind: "done"; readonly result: ImportResult }
  | { readonly kind: "error"; readonly message: string };

/**
 * 001 の CLI のデータ（data.json）の取り込み（contracts/screens.md「本」、FR-118）。
 * 読みかけの本と利用図書館がどちらも0件のときだけ、ファイル選択を表示する。
 */
export function Import({ bookCount, libraryCount, onImported }: Props) {
  const handleError = useApiErrorHandler();
  const [state, setState] = useState<State>({ kind: "idle" });
  const inputId = useId();
  const empty = bookCount === 0 && libraryCount === 0;

  const onFile = (file: File | undefined) => {
    if (!file) {
      return;
    }
    setState({ kind: "sending" });
    file
      .text()
      .then(importData)
      .then((result) => {
        setState({ kind: "done", result });
        onImported();
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setState({
          kind: "error",
          message: e instanceof ApiError ? e.message : "通信に失敗しました",
        });
      });
  };

  if (state.kind === "done") {
    const r = state.result;
    return (
      <p className="import-done" role="status">
        {`取り込みました（本 ${String(r.books)}冊・図書館 ${String(r.libraries)}館・臨時休館 ${String(r.closures)}件）`}
      </p>
    );
  }
  if (!empty) {
    return null;
  }
  return (
    <section className="import" aria-label="CLI のデータの取り込み">
      <label htmlFor={inputId}>CLI のデータを取り込む</label>
      <input
        id={inputId}
        type="file"
        accept="application/json,.json"
        disabled={state.kind === "sending"}
        onChange={(e) => {
          onFile(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
      {state.kind === "sending" && <p>取り込んでいます…</p>}
      {state.kind === "error" && (
        <p className="error" role="alert">
          {state.message}
        </p>
      )}
    </section>
  );
}
