import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { example } from "../test/examples";
import { Import } from "./Import";
import { jsonResponse } from "../test/http";

function stubImport(response: () => Response) {
  const fn = vi.fn<typeof fetch>(() => Promise.resolve(response()));
  vi.stubGlobal("fetch", fn);
  return fn;
}

const cliData = `{
  "version": 1,
  "books": [{"isbn": "9784003101018", "title": "吾輩は猫である", "addedOn": "2026-10-01"}],
  "libraries": []
}
`;

function dataFile(text = cliData): File {
  return new File([text], "data.json", { type: "application/json" });
}

test("本も図書館もないときだけ、取り込みの欄を表示する（FR-118）", () => {
  const onImported = vi.fn();
  const { rerender } = render(
    <Import bookCount={0} libraryCount={0} onImported={onImported} />,
  );
  expect(screen.getByLabelText("CLI のデータを取り込む")).toBeInTheDocument();

  for (const [books, libraries] of [
    [1, 0],
    [0, 1],
    [2, 3],
  ] as const) {
    rerender(
      <Import
        bookCount={books}
        libraryCount={libraries}
        onImported={onImported}
      />,
    );
    expect(
      screen.queryByLabelText("CLI のデータを取り込む"),
    ).not.toBeInTheDocument();
  }
});

test("ファイルを選ぶと中身をそのまま POST /api/import に送り、取り込んだ件数を表示する", async () => {
  const fetchMock = stubImport(() => jsonResponse(200, example("import")));
  const onImported = vi.fn();
  const user = userEvent.setup();
  const { rerender } = render(
    <Import bookCount={0} libraryCount={0} onImported={onImported} />,
  );

  await user.upload(
    screen.getByLabelText("CLI のデータを取り込む"),
    dataFile(),
  );

  expect(
    await screen.findByText(
      "取り込みました（本 3冊・図書館 4館・臨時休館 2件）",
    ),
  ).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [input, init] = fetchMock.mock.calls[0] ?? [];
  expect(input).toBe("/api/import");
  expect(init?.method).toBe("POST");
  expect(init?.body).toBe(cliData);
  expect(new Headers(init?.headers).get("Content-Type")).toBe(
    "application/json",
  );
  expect(onImported).toHaveBeenCalledTimes(1);

  // 一覧を更新して件数が増えても、取り込んだ件数は表示したまま。取り込みの欄は消える。
  rerender(<Import bookCount={3} libraryCount={4} onImported={onImported} />);
  expect(
    screen.getByText("取り込みました（本 3冊・図書館 4館・臨時休館 2件）"),
  ).toBeInTheDocument();
  expect(
    screen.queryByLabelText("CLI のデータを取り込む"),
  ).not.toBeInTheDocument();
});

test.each([
  [400, "invalid", "data.json の形式が正しくありません"],
  [409, "conflict", "すでにデータがあるため取り込めません"],
])(
  "%d ならエラーのメッセージを表示し、取り込みの欄を残す",
  async (status, code, message) => {
    stubImport(() => jsonResponse(status, { error: { code, message } }));
    const onImported = vi.fn();
    const user = userEvent.setup();
    render(<Import bookCount={0} libraryCount={0} onImported={onImported} />);

    await user.upload(
      screen.getByLabelText("CLI のデータを取り込む"),
      dataFile("壊れた中身"),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
    expect(onImported).not.toHaveBeenCalled();
    expect(screen.getByLabelText("CLI のデータを取り込む")).toBeInTheDocument();
  },
);
