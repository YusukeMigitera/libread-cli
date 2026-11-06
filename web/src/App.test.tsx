import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useEffect, useState } from "react";
import { getJSON } from "./api/http";
import { App, useApiErrorHandler } from "./App";
import { example } from "./test/examples";
import { jsonResponse, pathOf } from "./test/http";

// stubApi は path ごとの応答を返す fetch に差し替える。
function stubApi(routes: Record<string, () => Response>) {
  const fn = vi.fn((input: RequestInfo | URL) => {
    const path = pathOf(input);
    const route = routes[path];
    return Promise.resolve(
      route
        ? route()
        : jsonResponse(404, { error: { code: "notFound", message: "x" } }),
    );
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

const loggedIn = {
  "/api/session": () => jsonResponse(200, example("session")),
};
const loggedOut = {
  "/api/session": () => jsonResponse(401, example("error-unauthenticated")),
};

beforeEach(() => {
  window.location.hash = "";
});

test("起動時に 401 ならログイン画面を表示し、利用者データの画面は出さない", async () => {
  stubApi(loggedOut);
  render(<App />);
  const link = await screen.findByRole("link", { name: "Google でログイン" });
  expect(link).toHaveAttribute("href", "/auth/login?return=/");
  expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
});

test("ログイン画面の戻り先は今の画面", async () => {
  window.location.hash = "#/books";
  stubApi(loggedOut);
  render(<App />);
  expect(
    await screen.findByRole("link", { name: "Google でログイン" }),
  ).toHaveAttribute("href", "/auth/login?return=/books");
});

test("#/login?error=denied ではログインできなかったことを表示する", async () => {
  window.location.hash = "#/login?error=denied";
  stubApi(loggedOut);
  render(<App />);
  expect(
    await screen.findByText(
      "ログインできませんでした。libread に登録した Google アカウントでログインしてください",
    ),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "Google でログイン" }),
  ).toHaveAttribute("href", "/auth/login?return=/");
});

test("ログイン済みなら下部のタブで今日・本・図書館を切り替える", async () => {
  stubApi(loggedIn);
  const user = userEvent.setup();
  render(<App />);
  const nav = await screen.findByRole("navigation");
  expect(nav).toBeInTheDocument();
  expect(
    screen.queryByRole("link", { name: "Google でログイン" }),
  ).not.toBeInTheDocument();

  await user.click(screen.getByRole("link", { name: "本" }));
  await waitFor(() => {
    expect(window.location.hash).toBe("#/books");
  });
  expect(screen.getByRole("link", { name: "本" })).toHaveAttribute(
    "aria-current",
    "page",
  );

  await user.click(screen.getByRole("link", { name: "図書館" }));
  await waitFor(() => {
    expect(window.location.hash).toBe("#/libraries");
  });
  expect(screen.getByRole("link", { name: "図書館" })).toHaveAttribute(
    "aria-current",
    "page",
  );

  await user.click(screen.getByRole("link", { name: "今日" }));
  await waitFor(() => {
    expect(window.location.hash).toBe("#/");
  });
  expect(screen.getByRole("link", { name: "今日" })).toHaveAttribute(
    "aria-current",
    "page",
  );
});

test("あとの API が 401 を返したらログイン画面に切り替える", async () => {
  stubApi({
    ...loggedIn,
    "/api/books": () => jsonResponse(401, example("error-unauthenticated")),
  });
  // 画面の中で API を呼び、失敗を共通の処理に渡す。
  function BooksThatExpire() {
    const handleError = useApiErrorHandler();
    useEffect(() => {
      getJSON("/api/books", (u) => u).catch(handleError);
    }, [handleError]);
    return <p>本の画面</p>;
  }
  window.location.hash = "#/books";
  render(<App screens={{ books: BooksThatExpire }} />);
  expect(
    await screen.findByRole("link", { name: "Google でログイン" }),
  ).toHaveAttribute("href", "/auth/login?return=/books");
  expect(screen.queryByText("本の画面")).not.toBeInTheDocument();
});

test("401 以外の失敗は共通の処理から投げ直され、画面が表示できる", async () => {
  stubApi({
    ...loggedIn,
    "/api/books": () =>
      jsonResponse(502, {
        error: {
          code: "upstream",
          message: "NDL サーチに接続できませんでした",
        },
      }),
  });
  function BooksThatFail() {
    const handleError = useApiErrorHandler();
    const [message, setMessage] = useState("");
    useEffect(() => {
      getJSON("/api/books", (u) => u)
        .catch(handleError)
        .catch((e: unknown) => {
          setMessage(e instanceof Error ? e.message : "?");
        });
    }, [handleError]);
    return <p>{message}</p>;
  }
  window.location.hash = "#/books";
  render(<App screens={{ books: BooksThatFail }} />);
  expect(
    await screen.findByText("NDL サーチに接続できませんでした"),
  ).toBeInTheDocument();
  expect(screen.getByRole("navigation")).toBeInTheDocument();
});
