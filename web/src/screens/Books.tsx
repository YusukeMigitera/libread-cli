import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useApiErrorHandler } from "../App";
import {
  addBook,
  fetchBooks,
  removeBook,
  searchBooks,
  updateNote,
  type BookCandidate,
  type BookQuery,
  type BookSearch,
  type ReadingBook,
} from "../api/books";
import type { ISBN } from "../api/check";
import { ApiError, asArray, asObject, getJSON } from "../api/http";
import { ErrorRetry } from "../components/ErrorRetry";
import {
  clearDraft,
  keepDraftOnUnauthenticated,
  peekDraft,
  takeDraft,
  type AddDraft,
} from "../draft";
import { Import } from "./Import";

// 本の画面（contracts/screens.md「本」）。読みかけの本の一覧とメモの編集・外す、本を探して登録、001 のデータの取り込み。

function messageOf(e: unknown): string {
  return e instanceof ApiError ? e.message : "通信に失敗しました";
}

interface Lists {
  readonly books: readonly ReadingBook[];
  /** 利用図書館の館数。取り込みの欄を出すか決めるのに使う（FR-118）。 */
  readonly libraryCount: number;
}

function fetchLists(): Promise<Lists> {
  return Promise.all([
    fetchBooks(),
    getJSON(
      "/api/libraries",
      (u) => asArray(asObject(u).libraries, (v) => v).length,
    ),
  ]).then(([books, libraryCount]) => ({ books, libraryCount }));
}

interface ListState {
  /** 最後に読めた一覧。読み直しに失敗しても残す。 */
  readonly lists: Lists | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
}

/** 編集中のメモ。同時に編集するのは1冊だけ。 */
interface Editing {
  readonly isbn: string;
  readonly text: string;
  readonly saving: boolean;
  readonly error?: string;
}

export function Books() {
  const handleError = useApiErrorHandler();
  const [state, setState] = useState<ListState>({
    lists: undefined,
    loading: true,
    error: undefined,
  });
  const [editing, setEditing] = useState<Editing | undefined>(undefined);
  const [removeError, setRemoveError] = useState<string | undefined>(undefined);
  /** 外している途中の本の ISBN。 */
  const [removing, setRemoving] = useState<ReadonlySet<string>>(new Set());

  const load = useCallback(() => {
    fetchLists()
      .then((lists) => {
        setState({ lists, loading: false, error: undefined });
        // 再ログインの前に退避した書きかけのメモを、入力欄に戻す（research W12）。
        const draft = takeDraft("note");
        if (draft && lists.books.some((b) => b.isbn === draft.isbn)) {
          setEditing({ isbn: draft.isbn, text: draft.text, saving: false });
        }
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setState((prev) => ({
          lists: prev.lists,
          loading: false,
          error: messageOf(e),
        }));
      });
  }, [handleError]);

  useEffect(() => {
    load();
  }, [load]);

  const setBooks = (
    change: (books: readonly ReadingBook[]) => ReadingBook[],
  ) => {
    setState((prev) =>
      prev.lists
        ? { ...prev, lists: { ...prev.lists, books: change(prev.lists.books) } }
        : prev,
    );
  };

  const saveNote = (isbn: ISBN, text: string) => {
    setEditing({ isbn, text, saving: true });
    updateNote(isbn, text)
      .then((updated) => {
        setBooks((books) =>
          books.map((b) => (b.isbn === updated.isbn ? updated : b)),
        );
        // 保存中にほかの本のメモを編集し始めていたら、その入力は残す。
        setEditing((prev) => (prev?.isbn === isbn ? undefined : prev));
      })
      .catch(keepDraftOnUnauthenticated({ kind: "note", isbn, text }))
      .catch(handleError)
      .catch((e: unknown) => {
        setEditing((prev) =>
          prev?.isbn === isbn
            ? { isbn, text, saving: false, error: messageOf(e) }
            : prev,
        );
      });
  };

  const remove = (book: ReadingBook) => {
    if (!window.confirm(`『${book.title}』を外しますか？`)) {
      return;
    }
    setRemoveError(undefined);
    setRemoving((prev) => new Set(prev).add(book.isbn));
    const removed = () => {
      setBooks((books) => books.filter((b) => b.isbn !== book.isbn));
    };
    removeBook(book.isbn)
      .then(removed)
      .catch(handleError)
      .catch((e: unknown) => {
        // ほかの端末ですでに外していた。
        if (e instanceof ApiError && e.status === 404) {
          removed();
          return;
        }
        setRemoveError(messageOf(e));
      })
      .finally(() => {
        setRemoving((prev) => {
          const next = new Set(prev);
          next.delete(book.isbn);
          return next;
        });
      });
  };

  const books = state.lists?.books;
  return (
    <main className="screen books">
      <h1>本</h1>
      {state.loading && state.lists === undefined && <p>読み込んでいます…</p>}
      {state.error !== undefined && (
        <ErrorRetry
          message={state.error}
          onRetry={() => {
            setState((prev) => ({ ...prev, loading: true, error: undefined }));
            load();
          }}
        />
      )}
      {books && (
        <section className="reading-books" aria-label="読みかけの本の一覧">
          {books.length === 0 ? (
            <p>読みかけの本は登録されていません</p>
          ) : (
            <ul className="items" aria-label="読みかけの本">
              {books.map((b) => (
                <BookItem
                  key={b.isbn}
                  book={b}
                  editing={editing?.isbn === b.isbn ? editing : undefined}
                  removing={removing.has(b.isbn)}
                  onEdit={() => {
                    setEditing({ isbn: b.isbn, text: b.note, saving: false });
                  }}
                  onChange={(text) => {
                    setEditing({ isbn: b.isbn, text, saving: false });
                  }}
                  onCancel={() => {
                    setEditing(undefined);
                  }}
                  onSave={(text) => {
                    saveNote(b.isbn, text);
                  }}
                  onRemove={() => {
                    remove(b);
                  }}
                />
              ))}
            </ul>
          )}
          {removeError !== undefined && (
            <p className="error" role="alert">
              {removeError}
            </p>
          )}
        </section>
      )}
      {state.lists && (
        <Import
          bookCount={state.lists.books.length}
          libraryCount={state.lists.libraryCount}
          onImported={load}
        />
      )}
      <BookFinder
        registered={books ?? []}
        onAdded={(added) => {
          setBooks((list) =>
            list.some((b) => b.isbn === added.isbn)
              ? [...list]
              : [...list, added],
          );
        }}
      />
    </main>
  );
}

interface BookItemProps {
  book: ReadingBook;
  editing: Editing | undefined;
  removing: boolean;
  onEdit: () => void;
  onChange: (text: string) => void;
  onCancel: () => void;
  onSave: (text: string) => void;
  onRemove: () => void;
}

/** 読みかけの本の1冊。書名・著者・出版社・メモ・登録日と、「メモを編集」「外す」。 */
function BookItem({
  book,
  editing,
  removing,
  onEdit,
  onChange,
  onCancel,
  onSave,
  onRemove,
}: BookItemProps) {
  const inputId = useId();
  const byline = [book.author, book.publisher].filter((s) => s !== "");
  return (
    <li className="item">
      <p className="title">{book.title}</p>
      {byline.length > 0 && <p className="byline">{byline.join(" / ")}</p>}
      {editing ? (
        <form
          className="note-editor"
          onSubmit={(e) => {
            e.preventDefault();
            onSave(editing.text);
          }}
        >
          <label htmlFor={inputId}>メモ</label>
          <input
            id={inputId}
            type="text"
            value={editing.text}
            readOnly={editing.saving}
            onChange={(e) => {
              onChange(e.target.value);
            }}
          />
          <div className="actions">
            <button type="submit" disabled={editing.saving}>
              保存
            </button>
            <button type="button" onClick={onCancel} disabled={editing.saving}>
              取消
            </button>
          </div>
          {editing.error !== undefined && (
            <p className="error" role="alert">
              {editing.error}
            </p>
          )}
        </form>
      ) : (
        book.note !== "" && <p className="note">{book.note}</p>
      )}
      <p className="meta">登録日 {book.addedOn}</p>
      {!editing && (
        <div className="actions">
          <button type="button" onClick={onEdit}>
            メモを編集
          </button>
          <button type="button" onClick={onRemove} disabled={removing}>
            外す
          </button>
        </div>
      )}
    </li>
  );
}

type SearchState =
  | { readonly kind: "idle" }
  | { readonly kind: "loading"; readonly query: BookQuery }
  | {
      readonly kind: "done";
      readonly query: BookQuery;
      readonly result: BookSearch;
    }
  | {
      readonly kind: "error";
      readonly query: BookQuery;
      readonly message: string;
    };

/** describeQuery は「書名」（著者: …、出版社: …）の形で検索条件を表す。指定しなかった条件は省く（001 と同じ）。 */
function describeQuery(q: BookQuery): string {
  const filters: string[] = [];
  if (q.author) {
    filters.push(`著者: ${q.author}`);
  }
  if (q.publisher) {
    filters.push(`出版社: ${q.publisher}`);
  }
  return `「${q.q}」${filters.length > 0 ? `（${filters.join("、")}）` : ""}`;
}

interface FinderProps {
  /** 読みかけの本。検索結果のうち、ここにある本は「登録済み」にする。 */
  registered: readonly ReadingBook[];
  onAdded: (book: ReadingBook) => void;
}

/** 本を探して登録する（contracts/screens.md「本」の「本を探す」）。 */
function BookFinder({ registered, onAdded }: FinderProps) {
  const handleError = useApiErrorHandler();
  const ids = { q: useId(), author: useId(), publisher: useId() };
  // 再ログインの前に登録しかけた本があれば、同じ条件で探し直し、その本のメモの入力欄に戻す（research W12）。
  const [restore, setRestore] = useState<AddDraft | undefined>(() =>
    peekDraft("add"),
  );
  const [form, setForm] = useState(
    () => restore?.query ?? { q: "", author: "", publisher: "" },
  );
  const [search, setSearch] = useState<SearchState>(() =>
    restore ? { kind: "loading", query: restore.query } : { kind: "idle" },
  );
  const [formError, setFormError] = useState<string | undefined>(undefined);
  /** 登録しかけた本を検索結果に戻せなかったときの、入力していたメモ。 */
  const [missed, setMissed] = useState<AddDraft | undefined>(undefined);

  // 画面を離れたあとに検索が終わっても、退避した内容を消さない（戻ってきたときに戻せるように）。
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // restoring は、登録しかけた本の検索のやり直しなら、その退避した内容。
  // 結果を表示できたら退避した内容を消す。失敗したら消さずに残し、再試行や次のログインのあとでも戻せるようにする。
  // その本が結果にないか登録済みでメモの入力欄に戻せないときは、入力していたメモを表示する。
  const fetchResult = useCallback(
    (query: BookQuery, restoring: AddDraft | undefined) => {
      searchBooks(query)
        .then((result) => {
          if (!mounted.current) {
            return;
          }
          setSearch({ kind: "done", query, result });
          if (restoring) {
            const target = result.books.find((c) => c.isbn === restoring.isbn);
            if (!target || target.registered) {
              setMissed(restoring);
              setRestore(undefined);
            }
            clearDraft();
          }
        })
        .catch(handleError)
        .catch((e: unknown) => {
          setSearch({ kind: "error", query, message: messageOf(e) });
        });
    },
    [handleError],
  );

  const restored = useRef(false);
  useEffect(() => {
    if (restore && !restored.current) {
      restored.current = true;
      fetchResult(restore.query, restore);
    }
  }, [restore, fetchResult]);

  const run = (query: BookQuery, restoring: AddDraft | undefined) => {
    setSearch({ kind: "loading", query });
    fetchResult(query, restoring);
  };

  const onSubmit = () => {
    const query = {
      q: form.q.trim(),
      author: form.author.trim(),
      publisher: form.publisher.trim(),
    };
    if (query.q === "") {
      setFormError("書名または ISBN を入力してください");
      return;
    }
    setFormError(undefined);
    setMissed(undefined);
    if (restore) {
      // 登録しかけた本はやめて、新しい条件で探す。
      setRestore(undefined);
      clearDraft();
    }
    run(query, undefined);
  };

  return (
    <section className="book-finder" aria-label="本を探す">
      <h2>本を探す</h2>
      <form
        className="search-form"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
      >
        <label htmlFor={ids.q}>書名または ISBN</label>
        <input
          id={ids.q}
          type="search"
          required
          value={form.q}
          onChange={(e) => {
            setForm({ ...form, q: e.target.value });
            setFormError(undefined);
          }}
        />
        <label htmlFor={ids.author}>著者</label>
        <input
          id={ids.author}
          type="text"
          value={form.author}
          onChange={(e) => {
            setForm({ ...form, author: e.target.value });
          }}
        />
        <label htmlFor={ids.publisher}>出版社</label>
        <input
          id={ids.publisher}
          type="text"
          value={form.publisher}
          onChange={(e) => {
            setForm({ ...form, publisher: e.target.value });
          }}
        />
        <button type="submit" disabled={search.kind === "loading"}>
          探す
        </button>
        {formError !== undefined && (
          <p className="error" role="alert">
            {formError}
          </p>
        )}
      </form>
      {missed && (
        <p className="notice" role="status">
          {`登録しかけた本（ISBN ${missed.isbn}）は検索結果にないか、すでに登録されています。入力していたメモ: ${missed.note}`}
        </p>
      )}
      {search.kind === "loading" && <p>探しています…</p>}
      {search.kind === "error" && (
        <ErrorRetry
          message={search.message}
          onRetry={() => {
            // 新しい条件で探すと restore は消える（onSubmit）ので、restore があれば同じ条件での再試行。
            run(search.query, restore);
          }}
        />
      )}
      {search.kind === "done" && (
        <SearchResult
          query={search.query}
          result={search.result}
          registered={registered}
          restore={restore}
          onAdded={onAdded}
        />
      )}
    </section>
  );
}

interface ResultProps {
  query: BookQuery;
  result: BookSearch;
  registered: readonly ReadingBook[];
  /** 再ログインの前に登録しかけた本。その本のメモの入力欄を開いておく。 */
  restore: AddDraft | undefined;
  onAdded: (book: ReadingBook) => void;
}

function SearchResult({
  query,
  result,
  registered,
  restore,
  onAdded,
}: ResultProps) {
  const { credit } = result;
  return (
    <div className="search-result">
      {result.hint !== undefined && <p className="hint">{result.hint}</p>}
      {result.books.length === 0 ? (
        <p>{`${describeQuery(query)}に一致する本は見つかりませんでした`}</p>
      ) : (
        <ul className="items" aria-label="検索結果">
          {result.books.map((c) => (
            <Candidate
              key={c.isbn}
              query={query}
              candidate={c}
              restoredNote={restore?.isbn === c.isbn ? restore.note : undefined}
              registered={
                c.registered || registered.some((b) => b.isbn === c.isbn)
              }
              onAdded={onAdded}
            />
          ))}
        </ul>
      )}
      <p className="credit">
        <a href={credit.url} target="_blank" rel="noreferrer">
          {credit.label}
        </a>
        <br />
        ライセンス:{" "}
        <a href={credit.license.url} target="_blank" rel="noreferrer">
          {credit.license.label}
        </a>
      </p>
    </div>
  );
}

type AddState =
  | { readonly kind: "idle" }
  | {
      readonly kind: "noting";
      readonly note: string;
      readonly saving: boolean;
      readonly error?: string;
    };

interface CandidateProps {
  /** この結果を探した条件。本人確認が切れたときに退避する。 */
  query: BookQuery;
  candidate: BookCandidate;
  /** 再ログインの前に入力していたメモ。あればメモの入力欄を開いておく。 */
  restoredNote: string | undefined;
  registered: boolean;
  onAdded: (book: ReadingBook) => void;
}

/** 検索結果の1件。「登録」を押すとメモ（任意）の入力欄を出し、「登録する」で登録する。 */
function Candidate({
  query,
  candidate,
  restoredNote,
  registered,
  onAdded,
}: CandidateProps) {
  const handleError = useApiErrorHandler();
  const noteId = useId();
  const [add, setAdd] = useState<AddState>(() =>
    restoredNote === undefined
      ? { kind: "idle" }
      : { kind: "noting", note: restoredNote, saving: false },
  );
  const byline = [candidate.author, candidate.publisher].filter(
    (s) => s !== "",
  );

  const submit = (note: string) => {
    setAdd({ kind: "noting", note, saving: true });
    addBook(candidate.isbn, note)
      .then((book) => {
        setAdd({ kind: "idle" });
        onAdded(book);
      })
      .catch(
        keepDraftOnUnauthenticated({
          kind: "add",
          query: {
            q: query.q,
            author: query.author ?? "",
            publisher: query.publisher ?? "",
          },
          isbn: candidate.isbn,
          note,
        }),
      )
      .catch(handleError)
      .catch((e: unknown) => {
        setAdd({ kind: "noting", note, saving: false, error: messageOf(e) });
      });
  };

  return (
    <li className="item">
      <p className="title">{candidate.title}</p>
      {byline.length > 0 && <p className="byline">{byline.join(" / ")}</p>}
      <p className="meta">ISBN {candidate.isbn}</p>
      {registered ? (
        <button type="button" disabled>
          登録済み
        </button>
      ) : add.kind === "idle" ? (
        <button
          type="button"
          onClick={() => {
            setAdd({ kind: "noting", note: "", saving: false });
          }}
        >
          登録
        </button>
      ) : (
        <form
          className="note-editor"
          onSubmit={(e) => {
            e.preventDefault();
            submit(add.note);
          }}
        >
          <label htmlFor={noteId}>メモ（任意）</label>
          <input
            id={noteId}
            type="text"
            value={add.note}
            readOnly={add.saving}
            onChange={(e) => {
              setAdd({ kind: "noting", note: e.target.value, saving: false });
            }}
          />
          <div className="actions">
            <button type="submit" disabled={add.saving}>
              登録する
            </button>
            <button
              type="button"
              disabled={add.saving}
              onClick={() => {
                setAdd({ kind: "idle" });
              }}
            >
              取消
            </button>
          </div>
          {add.error !== undefined && (
            <p className="error" role="alert">
              {add.error}
            </p>
          )}
        </form>
      )}
    </li>
  );
}
