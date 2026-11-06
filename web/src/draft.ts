// 本人確認が切れたときの入力の退避（research W12）。
// sessionStorage に置くのは入力中の1件だけで、戻したら消す。sessionStorage を使えない環境では何もしない。

import { UnauthenticatedError } from "./api/http";

const storageKey = "libread:draft";

/** 本の画面で書きかけのメモ。 */
export interface NoteDraft {
  readonly kind: "note";
  readonly isbn: string;
  readonly text: string;
}

/** 本の画面で登録しかけた本。検索をやり直し、その本のメモの入力欄に戻す。 */
export interface AddDraft {
  readonly kind: "add";
  readonly query: {
    readonly q: string;
    readonly author: string;
    readonly publisher: string;
  };
  readonly isbn: string;
  readonly note: string;
}

export type Draft = NoteDraft | AddDraft;

/** saveDraft は入力中の内容を退避する。前に退避した内容は置き換える。 */
export function saveDraft(draft: Draft): void {
  try {
    sessionStorage.setItem(storageKey, JSON.stringify(draft));
  } catch {
    // 退避できなくても、ログイン画面への切り替えは続ける。
  }
}

/**
 * keepDraftOnUnauthenticated は、保存の要求の catch に渡す処理を返す。
 * 本人確認が切れた失敗（UnauthenticatedError）なら draft を退避し、どの失敗もそのまま投げ直す。
 * 例: updateNote(...).catch(keepDraftOnUnauthenticated(draft)).catch(handleError)
 */
export function keepDraftOnUnauthenticated(
  draft: Draft,
): (e: unknown) => never {
  return (e) => {
    if (e instanceof UnauthenticatedError) {
      saveDraft(draft);
    }
    throw e;
  };
}

/** peekDraft は退避した内容のうち kind のものを返す。消さない（描画中に呼べるように）。 */
export function peekDraft<K extends Draft["kind"]>(
  kind: K,
): Extract<Draft, { kind: K }> | undefined {
  let raw: string | null;
  try {
    raw = sessionStorage.getItem(storageKey);
  } catch {
    return undefined;
  }
  const draft = raw === null ? undefined : parseDraft(raw);
  return draft?.kind === kind
    ? (draft as Extract<Draft, { kind: K }>)
    : undefined;
}

/** takeDraft は peekDraft と同じものを返し、見つかったら sessionStorage から消す。 */
export function takeDraft<K extends Draft["kind"]>(
  kind: K,
): Extract<Draft, { kind: K }> | undefined {
  const draft = peekDraft(kind);
  if (draft) {
    clearDraft();
  }
  return draft;
}

/** clearDraft は退避した内容を消す。 */
export function clearDraft(): void {
  try {
    sessionStorage.removeItem(storageKey);
  } catch {
    // 消せなくても、次に退避したときに置き換わる。
  }
}

function parseDraft(raw: string): Draft | undefined {
  let u: unknown;
  try {
    u = JSON.parse(raw);
  } catch {
    return undefined;
  }
  if (typeof u !== "object" || u === null) {
    return undefined;
  }
  const o = u as Record<string, unknown>;
  if (typeof o.isbn !== "string") {
    return undefined;
  }
  if (o.kind === "note" && typeof o.text === "string") {
    return { kind: "note", isbn: o.isbn, text: o.text };
  }
  if (o.kind === "add" && typeof o.note === "string") {
    const q = o.query as Record<string, unknown> | null | undefined;
    if (
      typeof q === "object" &&
      q !== null &&
      typeof q.q === "string" &&
      typeof q.author === "string" &&
      typeof q.publisher === "string"
    ) {
      return {
        kind: "add",
        isbn: o.isbn,
        note: o.note,
        query: { q: q.q, author: q.author, publisher: q.publisher },
      };
    }
  }
  return undefined;
}
