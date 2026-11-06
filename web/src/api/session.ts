// 本人確認（contracts/http-api.md「本人確認」）のうち、ログアウトの要求。
import { noContent, sendJSON } from "./http";

/** logout はこの端末のセッションを消す。 */
export function logout(): Promise<undefined> {
  return sendJSON("POST", "/api/session/logout", undefined, noContent);
}

/** revokeAllSessions は、この端末も含むすべての端末の本人確認を取り消す（FR-111）。 */
export function revokeAllSessions(): Promise<undefined> {
  return sendJSON("POST", "/api/session/revoke-all", undefined, noContent);
}
