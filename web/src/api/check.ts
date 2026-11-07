// GET /api/check（contracts/http-api.md「検索」）。
import {
  asArray,
  asBoolean,
  asDateTime,
  asObject,
  asOptional,
  asString,
  DecodeError,
  getJSON,
} from "./http";

/** 13桁に正規化した ISBN。サーバーが返した値からだけ作る。 */
export type ISBN = string & { readonly __brand: "ISBN" };

/** 開館状態（001 data-model の OpenStatus）。知らない値は "unknown" にする（Constitution 原則 III）。 */
export type OpeningStatus =
  | "open"
  | "notYetOpen"
  | "finishedToday"
  | "closedRegular"
  | "closedPeriod"
  | "noSchedule"
  | "holidayUnknown"
  | "unknown";

/** 所蔵状況（001 data-model の Availability）。知らない値は "unknown" にする。 */
export type Availability =
  | "availableToLend"
  | "inLibraryOnly"
  | "onLoan"
  | "reserved"
  | "notHeld"
  | "unknown";

export interface Opening {
  readonly status: OpeningStatus;
  readonly label: string;
  readonly open?: string;
  readonly close?: string;
  readonly closingSoon?: boolean;
}

export interface BookResult {
  readonly isbn: ISBN;
  readonly title: string;
  readonly availability: Availability;
  readonly label: string;
  readonly readable: boolean;
  readonly stale: boolean;
  readonly fetchedAt?: string;
}

export interface LibraryResult {
  readonly library: { readonly id: string; readonly name: string };
  readonly opening: Opening;
  readonly books: readonly BookResult[];
}

export interface Sources {
  readonly holdings: {
    readonly label: string;
    readonly links: readonly { readonly title: string; readonly url: string }[];
  };
  readonly openingInfo: { readonly label: string; readonly note: string };
  readonly holidays: { readonly label: string };
}

export interface CheckResult {
  readonly kind: "result";
  readonly checkedAt: string;
  /** 今日読める図書館。サーバーが並べた順のまま表示する。 */
  readonly readable: readonly LibraryResult[];
  /** 今日は読めない図書館（登録順）。 */
  readonly notReadable: readonly LibraryResult[];
  readonly sources: Sources;
}

export type SetupTarget = "libraries" | "books";

export interface SetupRequired {
  readonly kind: "setupRequired";
  readonly setupRequired: SetupTarget;
}

export type CheckView = CheckResult | SetupRequired;

const openingStatuses: readonly OpeningStatus[] = [
  "open",
  "notYetOpen",
  "finishedToday",
  "closedRegular",
  "closedPeriod",
  "noSchedule",
  "holidayUnknown",
];

const availabilities: readonly Availability[] = [
  "availableToLend",
  "inLibraryOnly",
  "onLoan",
  "reserved",
  "notHeld",
];

/** oneOf は u が values のどれかならその値、文字列だが知らない値なら fallback を返す。 */
function oneOf<T extends string>(
  u: unknown,
  values: readonly T[],
  fallback: T,
): T {
  const s = asString(u);
  return values.find((v) => v === s) ?? fallback;
}

export function asISBN(u: unknown): ISBN {
  const s = asString(u);
  if (!/^\d{13}$/.test(s)) {
    throw new DecodeError("ISBN ではありません");
  }
  return s as ISBN;
}

function decodeOpening(u: unknown): Opening {
  const o = asObject(u);
  const opening: {
    status: OpeningStatus;
    label: string;
    open?: string;
    close?: string;
    closingSoon?: boolean;
  } = {
    status: oneOf(o.status, openingStatuses, "unknown"),
    label: asString(o.label),
  };
  const open = asOptional(o.open, asString);
  const close = asOptional(o.close, asString);
  const closingSoon = asOptional(o.closingSoon, asBoolean);
  if (open !== undefined) {
    opening.open = open;
  }
  if (close !== undefined) {
    opening.close = close;
  }
  if (closingSoon !== undefined) {
    opening.closingSoon = closingSoon;
  }
  return opening;
}

function decodeBook(u: unknown): BookResult {
  const o = asObject(u);
  const book: BookResult = {
    isbn: asISBN(o.isbn),
    title: asString(o.title),
    availability: oneOf(o.availability, availabilities, "unknown"),
    label: asString(o.label),
    readable: asBoolean(o.readable),
    stale: asBoolean(o.stale),
  };
  const fetchedAt = asOptional(o.fetchedAt, asDateTime);
  return fetchedAt === undefined ? book : { ...book, fetchedAt };
}

function decodeLibrary(u: unknown): LibraryResult {
  const o = asObject(u);
  const lib = asObject(o.library);
  return {
    library: { id: asString(lib.id), name: asString(lib.name) },
    opening: decodeOpening(o.opening),
    books: asArray(o.books, decodeBook),
  };
}

function decodeSources(u: unknown): Sources {
  const o = asObject(u);
  const holdings = asObject(o.holdings);
  const openingInfo = asObject(o.openingInfo);
  return {
    holdings: {
      label: asString(holdings.label),
      links: asArray(holdings.links, (l) => {
        const link = asObject(l);
        return { title: asString(link.title), url: asString(link.url) };
      }),
    },
    openingInfo: {
      label: asString(openingInfo.label),
      note: asString(openingInfo.note),
    },
    holidays: { label: asString(asObject(o.holidays).label) },
  };
}

/** decodeCheckView は GET /api/check の応答を検証する。形が契約と違えば例外を投げる。 */
export function decodeCheckView(u: unknown): CheckView {
  const o = asObject(u);
  if (o.setupRequired !== undefined) {
    const target = asString(o.setupRequired);
    if (target !== "libraries" && target !== "books") {
      throw new DecodeError("setupRequired の値が正しくありません");
    }
    return { kind: "setupRequired", setupRequired: target };
  }
  return {
    kind: "result",
    checkedAt: asDateTime(o.checkedAt),
    readable: asArray(o.readable, decodeLibrary),
    notReadable: asArray(o.notReadable, decodeLibrary),
    sources: decodeSources(o.sources),
  };
}

/** fetchCheck は読みかけの本すべて、または isbn の1冊について今日読める図書館を取得する。 */
export function fetchCheck(isbn?: ISBN): Promise<CheckView> {
  const path =
    isbn === undefined
      ? "/api/check"
      : `/api/check?isbn=${encodeURIComponent(isbn)}`;
  return getJSON(path, decodeCheckView);
}
