// 本人確認が切れたときの入力の退避（research W12）。
// sessionStorage に置くのは入力中の1件だけで、戻したら消す。sessionStorage を使えない環境では何もしない。

const storageKey = "libread:draft";

/** 本の画面で書きかけのメモ。 */
export interface NoteDraft {
  readonly isbn: string;
  readonly text: string;
}

interface Stored {
  readonly screen: "books";
  readonly kind: "note";
  readonly isbn: string;
  readonly text: string;
}

/** saveNoteDraft は書きかけのメモを退避する。前に退避した内容は置き換える。 */
export function saveNoteDraft(draft: NoteDraft): void {
  const stored: Stored = { screen: "books", kind: "note", ...draft };
  try {
    sessionStorage.setItem(storageKey, JSON.stringify(stored));
  } catch {
    // 退避できなくても、ログイン画面への切り替えは続ける。
  }
}

/** takeNoteDraft は退避した書きかけのメモを返し、sessionStorage から消す。なければ undefined。 */
export function takeNoteDraft(): NoteDraft | undefined {
  let raw: string | null;
  try {
    raw = sessionStorage.getItem(storageKey);
  } catch {
    return undefined;
  }
  if (raw === null) {
    return undefined;
  }
  let u: unknown;
  try {
    u = JSON.parse(raw);
  } catch {
    u = undefined;
  }
  if (!isNoteDraft(u)) {
    // 壊れた値は残しておいても戻せないので消す。
    removeDraft();
    return undefined;
  }
  removeDraft();
  return { isbn: u.isbn, text: u.text };
}

function removeDraft(): void {
  try {
    sessionStorage.removeItem(storageKey);
  } catch {
    // 消せなくても、次に退避したときに置き換わる。
  }
}

function isNoteDraft(u: unknown): u is Stored {
  if (typeof u !== "object" || u === null) {
    return false;
  }
  const o = u as Record<string, unknown>;
  return (
    o.screen === "books" &&
    o.kind === "note" &&
    typeof o.isbn === "string" &&
    typeof o.text === "string"
  );
}
