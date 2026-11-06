import { useCallback, useEffect, useId, useState } from "react";
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
import {
  ApiError,
  asArray,
  asObject,
  getJSON,
  UnauthenticatedError,
} from "../api/http";
import { ErrorRetry } from "../components/ErrorRetry";
import { saveNoteDraft, takeNoteDraft } from "../draft";
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

  const load = useCallback(() => {
    fetchLists()
      .then((lists) => {
        setState({ lists, loading: false, error: undefined });
        // 再ログインの前に退避した書きかけのメモを、入力欄に戻す（research W12）。
        const draft = takeNoteDraft();
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
        setEditing(undefined);
      })
      .catch((e: unknown) => {
        if (e instanceof UnauthenticatedError) {
          saveNoteDraft({ isbn, text });
        }
        throw e;
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setEditing({ isbn, text, saving: false, error: messageOf(e) });
      });
  };

  const remove = (book: ReadingBook) => {
    if (!window.confirm(`『${book.title}』を外しますか？`)) {
      return;
    }
    setRemoveError(undefined);
    removeBook(book.isbn)
      .then(() => {
        setBooks((books) => books.filter((b) => b.isbn !== book.isbn));
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setRemoveError(messageOf(e));
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
            onChange={(e) => {
              onChange(e.target.value);
            }}
          />
          <div className="actions">
            <button type="submit" disabled={editing.saving}>
              保存
            </button>
            <button type="button" onClick={onCancel}>
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
          <button type="button" onClick={onRemove}>
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
  const [form, setForm] = useState({ q: "", author: "", publisher: "" });
  const [search, setSearch] = useState<SearchState>({ kind: "idle" });

  const run = (query: BookQuery) => {
    setSearch({ kind: "loading", query });
    searchBooks(query)
      .then((result) => {
        setSearch({ kind: "done", query, result });
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setSearch({ kind: "error", query, message: messageOf(e) });
      });
  };

  const onSubmit = () => {
    const query = {
      q: form.q.trim(),
      author: form.author.trim(),
      publisher: form.publisher.trim(),
    };
    if (query.q === "") {
      return;
    }
    run(query);
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
      </form>
      {search.kind === "loading" && <p>探しています…</p>}
      {search.kind === "error" && (
        <ErrorRetry
          message={search.message}
          onRetry={() => {
            run(search.query);
          }}
        />
      )}
      {search.kind === "done" && (
        <SearchResult
          query={search.query}
          result={search.result}
          registered={registered}
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
  onAdded: (book: ReadingBook) => void;
}

function SearchResult({ query, result, registered, onAdded }: ResultProps) {
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
              candidate={c}
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
  candidate: BookCandidate;
  registered: boolean;
  onAdded: (book: ReadingBook) => void;
}

/** 検索結果の1件。「登録」を押すとメモ（任意）の入力欄を出し、「登録する」で登録する。 */
function Candidate({ candidate, registered, onAdded }: CandidateProps) {
  const handleError = useApiErrorHandler();
  const noteId = useId();
  const [add, setAdd] = useState<AddState>({ kind: "idle" });
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
