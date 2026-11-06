// 本人確認が切れたときの入力の退避（research W12）。
// sessionStorage に置くのは入力中の1件だけで、戻したら消す。sessionStorage を使えない環境では何もしない。

import { UnauthenticatedError } from "./api/http";
import { weekdays, type Weekday } from "./api/libraries";

const storageKey = "libread:draft";

/** 本の画面で書きかけのメモ。 */
export interface NoteDraft {
  readonly kind: "note";
  readonly isbn: string;
  readonly text: string;
}

/** 本の画面で登録しかけた本。検索をやり直し、その本のメモの入力欄に戻す。 */
/** 開館時間の編集の入力欄の値。時刻は入力欄の文字列のまま（確かめるのは保存のとき）。 */
export interface ScheduleForm {
  readonly days: Readonly<Record<Weekday, DayForm>>;
  readonly holiday: "same" | "closed" | "hours";
  readonly holidayOpen: string;
  readonly holidayClose: string;
}

export interface DayForm {
  readonly open: boolean;
  readonly openAt: string;
  readonly closeAt: string;
}

/** 臨時休館の追加の入力欄の値。 */
export interface ClosureForm {
  readonly start: string;
  readonly end: string;
  readonly reason: string;
}

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

/** 図書館の画面で入力しかけた開館時間（入力欄の値のまま）。 */
export interface ScheduleDraft {
  readonly kind: "schedule";
  readonly libraryId: string;
  readonly form: ScheduleForm;
}

/** 図書館の画面で追加しかけた臨時休館期間（入力欄の値のまま）。 */
export interface ClosureDraft {
  readonly kind: "closure";
  readonly libraryId: string;
  readonly form: ClosureForm;
}

export type Draft = NoteDraft | AddDraft | ScheduleDraft | ClosureDraft;

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
  const o = asRecord(u);
  if (!o) {
    return undefined;
  }
  switch (o.kind) {
    case "note":
    case "add":
      return parseBookDraft(o);
    case "schedule": {
      const form = parseScheduleForm(o.form);
      return typeof o.libraryId === "string" && form
        ? { kind: "schedule", libraryId: o.libraryId, form }
        : undefined;
    }
    case "closure": {
      const f = asRecord(o.form);
      return typeof o.libraryId === "string" &&
        f &&
        typeof f.start === "string" &&
        typeof f.end === "string" &&
        typeof f.reason === "string"
        ? {
            kind: "closure",
            libraryId: o.libraryId,
            form: { start: f.start, end: f.end, reason: f.reason },
          }
        : undefined;
    }
    default:
      return undefined;
  }
}

function asRecord(u: unknown): Record<string, unknown> | undefined {
  return typeof u === "object" && u !== null && !Array.isArray(u)
    ? (u as Record<string, unknown>)
    : undefined;
}

function parseBookDraft(o: Record<string, unknown>): Draft | undefined {
  if (typeof o.isbn !== "string") {
    return undefined;
  }
  if (o.kind === "note" && typeof o.text === "string") {
    return { kind: "note", isbn: o.isbn, text: o.text };
  }
  if (o.kind === "add" && typeof o.note === "string") {
    const q = asRecord(o.query);
    if (
      q &&
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

function parseScheduleForm(u: unknown): ScheduleForm | undefined {
  const f = asRecord(u);
  const days = asRecord(f?.days);
  if (
    !f ||
    !days ||
    (f.holiday !== "same" && f.holiday !== "closed" && f.holiday !== "hours") ||
    typeof f.holidayOpen !== "string" ||
    typeof f.holidayClose !== "string"
  ) {
    return undefined;
  }
  const parsed: Partial<Record<Weekday, DayForm>> = {};
  for (const { key } of weekdays) {
    const d = asRecord(days[key]);
    if (
      !d ||
      typeof d.open !== "boolean" ||
      typeof d.openAt !== "string" ||
      typeof d.closeAt !== "string"
    ) {
      return undefined;
    }
    parsed[key] = { open: d.open, openAt: d.openAt, closeAt: d.closeAt };
  }
  return {
    days: parsed as Record<Weekday, DayForm>,
    holiday: f.holiday,
    holidayOpen: f.holidayOpen,
    holidayClose: f.holidayClose,
  };
}
