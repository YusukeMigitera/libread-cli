// POST /api/import（contracts/http-api.md「取り込み」、FR-118）。
import { asNumber, asObject, sendJSONText } from "./http";

export interface ImportResult {
  readonly books: number;
  readonly libraries: number;
  readonly closures: number;
}

export function decodeImportResult(u: unknown): ImportResult {
  const o = asObject(u);
  return {
    books: asNumber(o.books),
    libraries: asNumber(o.libraries),
    closures: asNumber(o.closures),
  };
}

/** importData は 001 の data.json の中身 text を、そのまま送って取り込む。 */
export function importData(text: string): Promise<ImportResult> {
  return sendJSONText("POST", "/api/import", text, decodeImportResult);
}
