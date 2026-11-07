import type { ReturnPath } from "../route";

interface Props {
  /** ログイン後に戻る画面。 */
  returnPath: ReturnPath;
  /** Google から本人以外として戻ってきた（#/login?error=denied）。 */
  denied: boolean;
}

/** ログイン画面（contracts/screens.md「ログイン画面」）。利用者データは何も表示しない。 */
export function Login({ returnPath, denied }: Props) {
  return (
    <main className="screen login">
      <h1>libread</h1>
      {denied && (
        <p className="error" role="alert">
          ログインできませんでした。libread に登録した Google
          アカウントでログインしてください
        </p>
      )}
      <a className="button primary" href={`/auth/login?return=${returnPath}`}>
        Google でログイン
      </a>
    </main>
  );
}
