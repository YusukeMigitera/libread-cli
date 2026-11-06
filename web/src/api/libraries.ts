// 利用図書館（contracts/http-api.md「利用図書館」）。
import {
  asArray,
  asBoolean,
  asDate,
  asObject,
  asString,
  DecodeError,
  getJSON,
  noContent,
  sendJSON,
} from "./http";

/** 図書館ID（SystemID:LibKey）。サーバーが返した値からだけ作る。 */
export type LibraryID = string & { readonly __brand: "LibraryID" };

/** 時刻（HH:MM、00:00〜24:00）。 */
export type TimeOfDay = string & { readonly __brand: "TimeOfDay" };

export type Weekday = "sun" | "mon" | "tue" | "wed" | "thu" | "fri" | "sat";

/** 日曜から土曜の順の曜日。label は一覧の概要に、name は入力欄の名前に使う。 */
export const weekdays: readonly {
  readonly key: Weekday;
  readonly label: string;
  readonly name: string;
}[] = [
  { key: "sun", label: "日", name: "日曜日" },
  { key: "mon", label: "月", name: "月曜日" },
  { key: "tue", label: "火", name: "火曜日" },
  { key: "wed", label: "水", name: "水曜日" },
  { key: "thu", label: "木", name: "木曜日" },
  { key: "fri", label: "金", name: "金曜日" },
  { key: "sat", label: "土", name: "土曜日" },
];

export interface Hours {
  readonly open: TimeOfDay;
  readonly close: TimeOfDay;
}

/** 曜日ごとの開館時間。休館の曜日は null。 */
export type Weekly = Readonly<Record<Weekday, Hours | null>>;

/** 祝日の扱い。知らない値は "unknown" にする（Constitution 原則 III）。 */
export type HolidayRule =
  | { readonly mode: "same" }
  | { readonly mode: "closed" }
  | ({ readonly mode: "hours" } & Hours)
  | { readonly mode: "unknown" };

/** 画面から送れる祝日の扱い。 */
export type HolidayInput = Exclude<HolidayRule, { mode: "unknown" }>;

export interface Schedule {
  readonly weekly: Weekly;
  readonly holiday: HolidayRule;
  /** 最終更新日（YYYY-MM-DD）。 */
  readonly updatedOn: string;
}

export interface Closure {
  /** 開始日（YYYY-MM-DD）。 */
  readonly start: string;
  /** 終了日（YYYY-MM-DD）。 */
  readonly end: string;
  /** 理由。なければ空文字。 */
  readonly reason: string;
}

export interface Library {
  readonly id: LibraryID;
  readonly name: string;
  readonly pref: string;
  readonly city: string;
  /** 開館スケジュール。未入力なら null。 */
  readonly schedule: Schedule | null;
  /** 臨時休館期間（開始日の順）。削除ではこの配列の位置で指定する。 */
  readonly closures: readonly Closure[];
}

/** 図書館の検索結果の1件。 */
export interface LibraryCandidate {
  readonly id: LibraryID;
  readonly name: string;
  readonly pref: string;
  readonly city: string;
  readonly address: string;
  readonly registered: boolean;
}

export interface LibrarySearch {
  readonly libraries: readonly LibraryCandidate[];
  /** カーリルへのリンク。表示が利用条件で必須（001 research R1）。 */
  readonly credit: { readonly label: string; readonly url: string };
}

export interface LibraryQuery {
  /** 都道府県（必須）。 */
  readonly pref: string;
  readonly city?: string;
  /** 館名の部分一致。 */
  readonly name?: string;
}

export interface ScheduleInput {
  readonly weekly: Weekly;
  readonly holiday: HolidayInput;
}

export interface ClosureInput {
  readonly start: string;
  readonly end: string;
  /** 理由（任意）。空なら送らない。 */
  readonly reason: string;
}

/** 47都道府県（北から）。図書館の検索で選ぶ。 */
export const prefectures: readonly string[] = [
  "北海道",
  "青森県",
  "岩手県",
  "宮城県",
  "秋田県",
  "山形県",
  "福島県",
  "茨城県",
  "栃木県",
  "群馬県",
  "埼玉県",
  "千葉県",
  "東京都",
  "神奈川県",
  "新潟県",
  "富山県",
  "石川県",
  "福井県",
  "山梨県",
  "長野県",
  "岐阜県",
  "静岡県",
  "愛知県",
  "三重県",
  "滋賀県",
  "京都府",
  "大阪府",
  "兵庫県",
  "奈良県",
  "和歌山県",
  "鳥取県",
  "島根県",
  "岡山県",
  "広島県",
  "山口県",
  "徳島県",
  "香川県",
  "愛媛県",
  "高知県",
  "福岡県",
  "佐賀県",
  "長崎県",
  "熊本県",
  "大分県",
  "宮崎県",
  "鹿児島県",
  "沖縄県",
];

// 検証関数

export function asLibraryID(u: unknown): LibraryID {
  const s = asString(u);
  const i = s.indexOf(":");
  if (i <= 0 || i === s.length - 1) {
    throw new DecodeError("図書館IDではありません");
  }
  return s as LibraryID;
}

/** parseTimeOfDay は HH:MM（00:00〜24:00）なら TimeOfDay、そうでなければ undefined。入力欄の値を確かめるのに使う。 */
export function parseTimeOfDay(s: string): TimeOfDay | undefined {
  const m = /^(\d{2}):(\d{2})$/.exec(s);
  if (!m) {
    return undefined;
  }
  const minutes = Number(m[1]) * 60 + Number(m[2]);
  if (Number(m[2]) >= 60 || minutes > 24 * 60) {
    return undefined;
  }
  return s as TimeOfDay;
}

export function asTimeOfDay(u: unknown): TimeOfDay {
  const t = parseTimeOfDay(asString(u));
  if (t === undefined) {
    throw new DecodeError("時刻ではありません");
  }
  return t;
}

function decodeHours(u: unknown): Hours {
  const o = asObject(u);
  return { open: asTimeOfDay(o.open), close: asTimeOfDay(o.close) };
}

function decodeWeekly(u: unknown): Weekly {
  const o = asObject(u);
  const day = (key: Weekday): Hours | null => {
    if (!(key in o)) {
      throw new DecodeError(`曜日 ${key} がありません`);
    }
    return o[key] === null ? null : decodeHours(o[key]);
  };
  return {
    sun: day("sun"),
    mon: day("mon"),
    tue: day("tue"),
    wed: day("wed"),
    thu: day("thu"),
    fri: day("fri"),
    sat: day("sat"),
  };
}

function decodeHoliday(u: unknown): HolidayRule {
  const o = asObject(u);
  switch (asString(o.mode)) {
    case "same":
      return { mode: "same" };
    case "closed":
      return { mode: "closed" };
    case "hours":
      return { mode: "hours", ...decodeHours(o) };
    default:
      return { mode: "unknown" };
  }
}

function decodeSchedule(u: unknown): Schedule {
  const o = asObject(u);
  return {
    weekly: decodeWeekly(o.weekly),
    holiday: decodeHoliday(o.holiday),
    updatedOn: asDate(o.updatedOn),
  };
}

/** decodeLibrary は利用図書館1館（GET /api/libraries の1件、変更の応答）を検証する。 */
export function decodeLibrary(u: unknown): Library {
  const o = asObject(u);
  if (!("schedule" in o)) {
    throw new DecodeError("schedule がありません");
  }
  return {
    id: asLibraryID(o.id),
    name: asString(o.name),
    pref: asString(o.pref),
    city: asString(o.city),
    schedule: o.schedule === null ? null : decodeSchedule(o.schedule),
    closures: asArray(o.closures, (c) => {
      const co = asObject(c);
      return {
        start: asDate(co.start),
        end: asDate(co.end),
        reason: asString(co.reason),
      };
    }),
  };
}

/** decodeLibraries は GET /api/libraries の応答を検証する。 */
export function decodeLibraries(u: unknown): Library[] {
  return asArray(asObject(u).libraries, decodeLibrary);
}

/** decodeLibrarySearch は GET /api/library-search の応答を検証する。 */
export function decodeLibrarySearch(u: unknown): LibrarySearch {
  const o = asObject(u);
  const credit = asObject(o.credit);
  return {
    libraries: asArray(o.libraries, (v) => {
      const c = asObject(v);
      return {
        id: asLibraryID(c.id),
        name: asString(c.name),
        pref: asString(c.pref),
        city: asString(c.city),
        address: asString(c.address),
        registered: asBoolean(c.registered),
      };
    }),
    credit: { label: asString(credit.label), url: asString(credit.url) },
  };
}

// 要求

export function fetchLibraries(): Promise<Library[]> {
  return getJSON("/api/libraries", decodeLibraries);
}

/** searchLibraries はカーリルで図書館を探す。空の条件は送らない。 */
export function searchLibraries(query: LibraryQuery): Promise<LibrarySearch> {
  const params = new URLSearchParams({ pref: query.pref });
  if (query.city) {
    params.set("city", query.city);
  }
  if (query.name) {
    params.set("name", query.name);
  }
  return getJSON(
    `/api/library-search?${params.toString()}`,
    decodeLibrarySearch,
  );
}

export function addLibrary(id: LibraryID): Promise<Library> {
  return sendJSON("POST", "/api/libraries", { id }, decodeLibrary);
}

export function removeLibrary(id: LibraryID): Promise<undefined> {
  return sendJSON("DELETE", libraryPath(id), undefined, noContent);
}

/** putSchedule は開館スケジュールを丸ごと置き換える。7曜日すべてを送る。 */
export function putSchedule(
  id: LibraryID,
  input: ScheduleInput,
): Promise<Library> {
  return sendJSON("PUT", `${libraryPath(id)}/schedule`, input, decodeLibrary);
}

/** addClosure は臨時休館期間を加え、更新後の館を返す。理由が空なら送らない。 */
export function addClosure(
  id: LibraryID,
  input: ClosureInput,
): Promise<Library> {
  const body =
    input.reason === ""
      ? { start: input.start, end: input.end }
      : { start: input.start, end: input.end, reason: input.reason };
  return sendJSON("POST", `${libraryPath(id)}/closures`, body, decodeLibrary);
}

/** removeClosure は closures の index 番目（0始まり）の期間を外し、更新後の館を返す。 */
export function removeClosure(id: LibraryID, index: number): Promise<Library> {
  return sendJSON(
    "DELETE",
    `${libraryPath(id)}/closures/${String(index)}`,
    undefined,
    decodeLibrary,
  );
}

function libraryPath(id: LibraryID): string {
  return `/api/libraries/${encodeURIComponent(id)}`;
}
