import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { example } from "../test/examples";
import { Books } from "./Books";
import { jsonResponse, pathOf } from "../test/http";

// stubLists は一覧の応答を返す。取り込みが済んだら、一覧を契約の例に差し替える。
function stubLists() {
  let imported = false;
  const fn = vi.fn<typeof fetch>((input, init) => {
    const path = pathOf(input);
    if (path === "/api/import" && init?.method === "POST") {
      imported = true;
      return Promise.resolve(jsonResponse(200, example("import")));
    }
    if (path === "/api/books") {
      return Promise.resolve(
        jsonResponse(200, imported ? example("books") : { books: [] }),
      );
    }
    if (path === "/api/libraries") {
      return Promise.resolve(
        jsonResponse(200, imported ? example("libraries") : { libraries: [] }),
      );
    }
    return Promise.resolve(
      jsonResponse(404, { error: { code: "notFound", message: "x" } }),
    );
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

test("本も図書館もなければ取り込みの欄を出し、取り込んだら一覧を読み直して欄を消す", async () => {
  const fetchMock = stubLists();
  const user = userEvent.setup();
  render(<Books />);

  const input = await screen.findByLabelText("CLI のデータを取り込む");
  await user.upload(
    input,
    new File(['{"version": 1}'], "data.json", { type: "application/json" }),
  );

  expect(
    await screen.findByText(
      "取り込みました（本 3冊・図書館 4館・臨時休館 2件）",
    ),
  ).toBeInTheDocument();
  expect(
    screen.queryByLabelText("CLI のデータを取り込む"),
  ).not.toBeInTheDocument();
  const paths = fetchMock.mock.calls.map(([i]) => pathOf(i));
  expect(paths.filter((p) => p === "/api/books")).toHaveLength(2);
});

test("一覧を読めなければ、メッセージと「再試行」", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(() =>
      Promise.resolve(
        jsonResponse(500, {
          error: { code: "internal", message: "保存先を読めませんでした" },
        }),
      ),
    ),
  );
  render(<Books />);
  expect(
    await screen.findByText("保存先を読めませんでした"),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "再試行" })).toBeInTheDocument();
});
