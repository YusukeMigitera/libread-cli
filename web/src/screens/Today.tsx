import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useApiErrorHandler } from "../App";
import {
  fetchCheck,
  type CheckResult,
  type ISBN,
  type LibraryResult,
  type SetupTarget,
} from "../api/check";
import { ApiError } from "../api/http";
import { ErrorRetry } from "../components/ErrorRetry";

/** 基準時刻からこれを超えたら「古い情報」と表示する（FR-105、001 と同じ30分）。 */
const staleAfterMs = 30 * 60_000;
/** 「古い情報」かどうかを見直す間隔。 */
const recheckIntervalMs = 60_000;

type State =
  | { readonly kind: "loading" }
  | { readonly kind: "result"; readonly result: CheckResult }
  | { readonly kind: "setupRequired"; readonly target: SetupTarget }
  | {
      readonly kind: "error";
      readonly message: string;
      /** 失敗する前に表示していた結果。基準時刻とともに残す（FR-116）。 */
      readonly previous: CheckResult | undefined;
    };

interface BookChoice {
  readonly isbn: ISBN;
  readonly title: string;
}

const weekdays = ["日", "月", "火", "水", "木", "金", "土"] as const;

/** checkedAtLabel は「10月21日(水) 14:05 時点」の形にする。日時はサーバーが日本時間で返すので、そのまま読む。 */
export function checkedAtLabel(checkedAt: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(checkedAt);
  if (!m) {
    return checkedAt;
  }
  const [, y, mo, d, hh, mm] = m.map(Number) as [
    number,
    number,
    number,
    number,
    number,
    number,
  ];
  const weekday = weekdays[new Date(Date.UTC(y, mo - 1, d)).getUTCDay()] ?? "";
  return `${String(mo)}月${String(d)}日(${weekday}) ${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")} 時点`;
}

/** booksOf は結果に含まれる本（登録順）。どの館の結果にも本がなければ空。 */
function booksOf(result: CheckResult): BookChoice[] {
  const lib = [...result.readable, ...result.notReadable].find(
    (l) => l.books.length > 0,
  );
  return (lib?.books ?? []).map((b) => ({ isbn: b.isbn, title: b.title }));
}

/** 今日の画面（contracts/screens.md「今日」）。 */
export function Today() {
  const handleError = useApiErrorHandler();
  const [state, setState] = useState<State>({ kind: "loading" });
  const [books, setBooks] = useState<readonly BookChoice[]>([]);
  const [selected, setSelected] = useState<ISBN | undefined>(undefined);
  const [now, setNow] = useState(() => Date.now());
  // notice は結果に添える案内（選んでいた本が外れていた、など）。
  const [notice, setNotice] = useState<string | undefined>(undefined);
  // lastResult は最後に表示できた結果と、そのときの本の絞り込み。失敗したとき、同じ絞り込みの結果だけを残す。
  const lastResult = useRef<
    | { readonly result: CheckResult; readonly isbn: ISBN | undefined }
    | undefined
  >(undefined);
  const requestId = useRef(0);
  const selectId = useId();

  // request は取得し、応答を受けてから状態を変える。あとから始めた取得があれば、前の応答は捨てる。
  // 404 のときに、すべての本で取得し直すのに使う（request 自身を中から呼ぶため）。
  const requestRef = useRef<(isbn: ISBN | undefined) => void>(() => undefined);
  const request = useCallback(
    (isbn: ISBN | undefined) => {
      const id = ++requestId.current;
      fetchCheck(isbn)
        .then((view) => {
          if (id !== requestId.current) {
            return;
          }
          setNow(Date.now());
          if (view.kind === "setupRequired") {
            // 登録が足りなくなったので、本の選択も前の結果も捨てる。
            lastResult.current = undefined;
            setBooks([]);
            setSelected(undefined);
            setState({ kind: "setupRequired", target: view.setupRequired });
            return;
          }
          lastResult.current = { result: view, isbn };
          if (isbn === undefined) {
            setBooks(booksOf(view));
          }
          setState({ kind: "result", result: view });
        })
        .catch(handleError)
        .catch((e: unknown) => {
          if (id !== requestId.current) {
            return;
          }
          if (isbn !== undefined && e instanceof ApiError && e.status === 404) {
            // 選んでいた本が読みかけの本から外れた。絞り込みをやめ、すべての本で取得し直す。
            setBooks([]);
            setSelected(undefined);
            setNotice(
              "選んでいた本は読みかけの本に登録されていないため、すべての本で調べました",
            );
            requestRef.current(undefined);
            return;
          }
          const last = lastResult.current;
          setState({
            kind: "error",
            message: e instanceof ApiError ? e.message : "通信に失敗しました",
            // 選んでいる本と違う絞り込みの結果は、選んだ本の結果と読み違えるので残さない。
            previous: last && last.isbn === isbn ? last.result : undefined,
          });
        });
    },
    [handleError],
  );
  useEffect(() => {
    requestRef.current = request;
  }, [request]);

  // reload は前の結果を隠して「取得中」にしてから取得し直す（FR-105）。
  const reload = useCallback(
    (isbn: ISBN | undefined) => {
      setState({ kind: "loading" });
      setNotice(undefined);
      request(isbn);
    },
    [request],
  );

  // 画面を開いたとき（ほかの画面から戻ったときを含む）に取得する。タブが前面に戻ったときは取得しない。
  useEffect(() => {
    request(undefined);
  }, [request]);

  // 画面を開いたままでも、1分ごとに「古い情報」かを見直す。
  useEffect(() => {
    const timer = setInterval(() => {
      setNow(Date.now());
    }, recheckIntervalMs);
    return () => {
      clearInterval(timer);
    };
  }, []);

  const onSelect = (value: string) => {
    const isbn = books.find((b) => b.isbn === value)?.isbn;
    setSelected(isbn);
    reload(isbn);
  };

  return (
    <main className="screen today">
      <h1>今日</h1>
      {state.kind !== "loading" && books.length >= 2 && (
        <div className="book-select">
          <label htmlFor={selectId}>調べる本</label>
          <select
            id={selectId}
            value={selected ?? ""}
            onChange={(e) => {
              onSelect(e.target.value);
            }}
          >
            <option value="">すべての本</option>
            {books.map((b) => (
              <option key={b.isbn} value={b.isbn}>
                {b.title}
              </option>
            ))}
          </select>
        </div>
      )}
      {state.kind === "loading" && <p>所蔵状況を取得しています…</p>}
      {state.kind !== "loading" && notice !== undefined && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {state.kind === "setupRequired" && <SetupGuide target={state.target} />}
      {state.kind === "error" && (
        <ErrorRetry
          message={state.message}
          onRetry={() => {
            reload(selected);
          }}
        />
      )}
      {(state.kind === "result" ||
        (state.kind === "error" && state.previous)) && (
        <ResultView
          result={state.kind === "result" ? state.result : state.previous}
          now={now}
          onRefresh={() => {
            reload(selected);
          }}
        />
      )}
    </main>
  );
}

function SetupGuide({ target }: { target: SetupTarget }) {
  return target === "libraries" ? (
    <div className="setup">
      <p>利用図書館が登録されていません</p>
      <a href="#/libraries">図書館の画面で登録する</a>
    </div>
  ) : (
    <div className="setup">
      <p>読みかけの本が登録されていません</p>
      <a href="#/books">本の画面で登録する</a>
    </div>
  );
}

interface ResultProps {
  result: CheckResult | undefined;
  now: number;
  onRefresh: () => void;
}

function ResultView({ result, now, onRefresh }: ResultProps) {
  const readableId = useId();
  const notReadableId = useId();
  if (!result) {
    return null;
  }
  const stale = now - Date.parse(result.checkedAt) > staleAfterMs;
  return (
    <>
      <div className="checked-at">
        <span>{checkedAtLabel(result.checkedAt)}</span>
        {stale && <span className="stale">古い情報</span>}
        <button type="button" onClick={onRefresh}>
          更新
        </button>
      </div>
      {stale && (
        <p className="stale-note">
          基準時刻から30分以上たっています。更新してください
        </p>
      )}

      <section aria-labelledby={readableId}>
        <h2 id={readableId}>今日読める図書館</h2>
        {result.readable.length === 0 ? (
          <p>今日読める図書館はありません</p>
        ) : (
          <ol className="libraries">
            {result.readable.map((l, i) => (
              <LibraryItem key={l.library.id} lib={l} number={i + 1} />
            ))}
          </ol>
        )}
      </section>

      {result.notReadable.length > 0 && (
        <section aria-labelledby={notReadableId}>
          <h2 id={notReadableId}>今日は読めない図書館</h2>
          <ul className="libraries">
            {result.notReadable.map((l) => (
              <LibraryItem key={l.library.id} lib={l} />
            ))}
          </ul>
        </section>
      )}

      <section className="sources" aria-label="出典">
        <p>{result.sources.holdings.label}</p>
        <ul>
          {result.sources.holdings.links.map((link) => (
            <li key={link.url}>
              <a href={link.url} target="_blank" rel="noreferrer">
                {link.title}
              </a>
            </li>
          ))}
        </ul>
        <p>{result.sources.openingInfo.label}</p>
        <p>{result.sources.openingInfo.note}</p>
        <p>{result.sources.holidays.label}</p>
      </section>
    </>
  );
}

function LibraryItem({ lib, number }: { lib: LibraryResult; number?: number }) {
  return (
    <li>
      <h3>
        {number !== undefined && <span>{`${String(number)}. `}</span>}
        <span>{lib.library.name}</span>
      </h3>
      <p className="opening">{lib.opening.label}</p>
      {lib.books.length > 0 && (
        <ul className="holdings">
          {lib.books.map((b) => (
            <li key={b.isbn}>
              <span className="title">『{b.title}』</span>{" "}
              <span>{b.label}</span>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}
