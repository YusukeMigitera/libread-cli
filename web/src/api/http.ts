// サーバーの API（specs/002-web-mobile-access/contracts/http-api.md）を呼ぶ。
// 応答は unknown として受け、検証関数で型付きの値に変換する（Constitution 原則 III）。

const networkErrorMessage = "通信に失敗しました";

/** 本人確認ができない（401）。画面はログイン画面に切り替える。 */
export class UnauthenticatedError extends Error {
  constructor() {
    super("ログインしてください");
    this.name = "UnauthenticatedError";
  }
}

/** 401 以外の失敗。message は画面にそのまま表示できる。 */
export class ApiError extends Error {
  readonly status: number | undefined;
  readonly code: string | undefined;

  constructor(message: string, status?: number, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export type Decoder<T> = (u: unknown) => T;

/** 本文のない応答（204）を受ける。 */
export const noContent: Decoder<undefined> = () => undefined;

export type WriteMethod = "POST" | "PUT" | "PATCH" | "DELETE";

export function getJSON<T>(path: string, decode: Decoder<T>): Promise<T> {
  return request(path, { method: "GET" }, decode);
}

export function sendJSON<T>(
  method: WriteMethod,
  path: string,
  body: unknown,
  decode: Decoder<T>,
): Promise<T> {
  const init: RequestInit = { method };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }
  return request(path, init, decode);
}

async function request<T>(
  path: string,
  init: RequestInit,
  decode: Decoder<T>,
): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { ...init, credentials: "same-origin" });
  } catch {
    throw new ApiError(networkErrorMessage);
  }
  if (res.status === 401) {
    throw new UnauthenticatedError();
  }
  const body = await readBody(res);
  if (!res.ok) {
    throw errorFromBody(res.status, body);
  }
  try {
    return decode(body);
  } catch {
    throw new ApiError(networkErrorMessage, res.status);
  }
}

async function readBody(res: Response): Promise<unknown> {
  const text = await res.text().catch(() => "");
  if (text === "") {
    return undefined;
  }
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}

function errorFromBody(status: number, body: unknown): ApiError {
  try {
    const error = asObject(asObject(body).error);
    // message があれば code がなくても使う（画面にそのまま出せる文言なので）。
    const code = typeof error.code === "string" ? error.code : undefined;
    return new ApiError(asString(error.message), status, code);
  } catch {
    return new ApiError(networkErrorMessage, status);
  }
}

// 検証関数。形が合わなければ例外を投げる。

export class DecodeError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DecodeError";
  }
}

export function asObject(u: unknown): Record<string, unknown> {
  if (typeof u !== "object" || u === null || Array.isArray(u)) {
    throw new DecodeError("オブジェクトではありません");
  }
  return u as Record<string, unknown>;
}

export function asString(u: unknown): string {
  if (typeof u !== "string") {
    throw new DecodeError("文字列ではありません");
  }
  return u;
}

export function asNumber(u: unknown): number {
  if (typeof u !== "number" || !Number.isFinite(u)) {
    throw new DecodeError("数ではありません");
  }
  return u;
}

export function asBoolean(u: unknown): boolean {
  if (typeof u !== "boolean") {
    throw new DecodeError("真偽値ではありません");
  }
  return u;
}

export function asArray<T>(u: unknown, item: Decoder<T>): T[] {
  if (!Array.isArray(u)) {
    throw new DecodeError("配列ではありません");
  }
  return u.map((v: unknown) => item(v));
}

/** u が undefined なら undefined、そうでなければ decode の結果。 */
export function asOptional<T>(u: unknown, decode: Decoder<T>): T | undefined {
  return u === undefined ? undefined : decode(u);
}

// 日時は RFC 3339（例: 2026-12-05T08:00:00+09:00）。
const rfc3339 =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

export function asDateTime(u: unknown): string {
  const s = asString(u);
  if (!rfc3339.test(s) || Number.isNaN(Date.parse(s))) {
    throw new DecodeError("日時ではありません");
  }
  return s;
}

// GET /api/session

export interface Session {
  readonly expiresAt: string;
}

export function decodeSession(u: unknown): Session {
  const o = asObject(u);
  return { expiresAt: asDateTime(o.expiresAt) };
}
