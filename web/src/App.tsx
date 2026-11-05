import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ComponentType,
} from "react";
import {
  ApiError,
  decodeSession,
  getJSON,
  UnauthenticatedError,
} from "./api/http";
import { ErrorRetry } from "./components/ErrorRetry";
import {
  parseHash,
  returnPathOf,
  screenPaths,
  type Route,
  type ScreenName,
} from "./route";
import { Login } from "./screens/Login";

type AuthState =
  | { readonly kind: "checking" }
  | { readonly kind: "loggedIn" }
  | { readonly kind: "loggedOut" }
  | { readonly kind: "error"; readonly message: string };

type ApiErrorHandler = (e: unknown) => void;

const ApiErrorContext = createContext<ApiErrorHandler>(() => undefined);

/**
 * useApiErrorHandler は、API の失敗を受け取る共通の処理を返す。
 * UnauthenticatedError（401）ならログイン画面に切り替える。それ以外の失敗は各画面が表示する。
 */
export function useApiErrorHandler(): ApiErrorHandler {
  return useContext(ApiErrorContext);
}

function Placeholder({ title }: { title: string }) {
  return (
    <main className="screen">
      <h1>{title}</h1>
    </main>
  );
}

const defaultScreens: Readonly<Record<ScreenName, ComponentType>> = {
  today: () => <Placeholder title="今日" />,
  books: () => <Placeholder title="本" />,
  libraries: () => <Placeholder title="図書館" />,
};

const tabs: readonly { readonly screen: ScreenName; readonly label: string }[] =
  [
    { screen: "today", label: "今日" },
    { screen: "books", label: "本" },
    { screen: "libraries", label: "図書館" },
  ];

function useHashRoute(): Route {
  const [route, setRoute] = useState(() => parseHash(window.location.hash));
  useEffect(() => {
    const onChange = () => {
      setRoute(parseHash(window.location.hash));
    };
    window.addEventListener("hashchange", onChange);
    return () => {
      window.removeEventListener("hashchange", onChange);
    };
  }, []);
  return route;
}

interface Props {
  /** 画面の差し替え（テスト用）。指定しなかった画面は既定のものを使う。 */
  screens?: Partial<Record<ScreenName, ComponentType>>;
}

/** 画面全体。起動時に本人確認の状態を調べ、ハッシュで画面を切り替える（contracts/screens.md「共通」）。 */
export function App({ screens = {} }: Props) {
  const route = useHashRoute();
  const [auth, setAuth] = useState<AuthState>({ kind: "checking" });

  // fetchSession は GET /api/session の結果で本人確認の状態を決める。状態は応答を受けてから変える。
  const fetchSession = useCallback(() => {
    getJSON("/api/session", decodeSession).then(
      () => {
        setAuth({ kind: "loggedIn" });
      },
      (e: unknown) => {
        if (e instanceof UnauthenticatedError) {
          setAuth({ kind: "loggedOut" });
        } else {
          setAuth({
            kind: "error",
            message: e instanceof ApiError ? e.message : "通信に失敗しました",
          });
        }
      },
    );
  }, []);

  useEffect(() => {
    fetchSession();
  }, [fetchSession]);

  const retrySession = useCallback(() => {
    setAuth({ kind: "checking" });
    fetchSession();
  }, [fetchSession]);

  const handleApiError = useCallback<ApiErrorHandler>((e) => {
    if (e instanceof UnauthenticatedError) {
      setAuth({ kind: "loggedOut" });
    }
  }, []);

  if (route.kind === "login" || auth.kind === "loggedOut") {
    return (
      <Login
        returnPath={returnPathOf(route)}
        denied={route.kind === "login" && route.denied}
      />
    );
  }
  if (auth.kind === "checking") {
    return (
      <main className="screen">
        <p>確認しています…</p>
      </main>
    );
  }
  if (auth.kind === "error") {
    return (
      <main className="screen">
        <ErrorRetry message={auth.message} onRetry={retrySession} />
      </main>
    );
  }

  const Screen = screens[route.screen] ?? defaultScreens[route.screen];
  return (
    <ApiErrorContext.Provider value={handleApiError}>
      <Screen />
      <nav className="tabs" aria-label="画面">
        {tabs.map((t) => (
          <a
            key={t.screen}
            href={`#${screenPaths[t.screen]}`}
            aria-current={t.screen === route.screen ? "page" : undefined}
          >
            {t.label}
          </a>
        ))}
      </nav>
    </ApiErrorContext.Provider>
  );
}
