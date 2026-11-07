// URL のハッシュでの画面の切り替え（contracts/screens.md「共通」）。ルーターのライブラリは使わない（research W6）。

export type ScreenName = "today" | "books" | "libraries";

/** ログイン後に戻る画面。サーバーの /auth/login?return= が受け付ける値と同じ。 */
export type ReturnPath = "/" | "/books" | "/libraries";

export type Route =
  | { readonly kind: "screen"; readonly screen: ScreenName }
  | { readonly kind: "login"; readonly denied: boolean };

export const screenPaths: Readonly<Record<ScreenName, ReturnPath>> = {
  today: "/",
  books: "/books",
  libraries: "/libraries",
};

/** parseHash は location.hash（例: "#/books"、"#/login?error=denied"）を画面に変換する。知らない値は今日の画面。 */
export function parseHash(hash: string): Route {
  const [path = "", query = ""] = hash.replace(/^#/, "").split("?", 2);
  switch (path) {
    case "/books":
      return { kind: "screen", screen: "books" };
    case "/libraries":
      return { kind: "screen", screen: "libraries" };
    case "/login":
      return {
        kind: "login",
        denied: new URLSearchParams(query).get("error") === "denied",
      };
    default:
      return { kind: "screen", screen: "today" };
  }
}

/** returnPathOf はログイン後に戻る画面。ログイン画面からは今日の画面に戻る。 */
export function returnPathOf(route: Route): ReturnPath {
  return route.kind === "screen" ? screenPaths[route.screen] : "/";
}
