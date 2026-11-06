import { useCallback, useEffect, useId, useState } from "react";
import { useApiErrorHandler, useLoggedOut } from "../App";
import { ApiError } from "../api/http";
import {
  addClosure,
  addLibrary,
  fetchLibraries,
  parseTimeOfDay,
  prefectures,
  putSchedule,
  removeClosure,
  removeLibrary,
  searchLibraries,
  weekdays,
  type Closure,
  type HolidayInput,
  type HolidayRule,
  type Hours,
  type Library,
  type LibraryCandidate,
  type LibraryID,
  type LibraryQuery,
  type LibrarySearch,
  type Schedule,
  type Weekday,
} from "../api/libraries";
import { logout, revokeAllSessions } from "../api/session";
import { ErrorRetry } from "../components/ErrorRetry";
import {
  keepDraftOnUnauthenticated,
  takeDraft,
  type ClosureForm,
  type DayForm,
  type ScheduleForm,
} from "../draft";

// 図書館の画面（contracts/screens.md「図書館」「設定」）。利用図書館の一覧と開館時間・臨時休館の入力、図書館を探して登録、ログアウト。

function messageOf(e: unknown): string {
  return e instanceof ApiError ? e.message : "通信に失敗しました";
}

function hoursText(h: Hours): string {
  return `${h.open}〜${h.close}`;
}

function holidayText(h: HolidayRule): string {
  switch (h.mode) {
    case "same":
      return "曜日どおり";
    case "closed":
      return "休館";
    case "hours":
      return hoursText(h);
    default:
      return "不明";
  }
}

/** describeClosure は 開始日〜終了日（理由）の形で臨時休館期間を表す（001 と同じ）。 */
function describeClosure(c: Closure): string {
  return `${c.start}〜${c.end}${c.reason === "" ? "" : `（${c.reason}）`}`;
}

// 開館時間の編集の入力欄

const defaultOpen = "09:00";
const defaultClose = "17:00";

/** scheduleFormOf は今のスケジュールから入力欄の値を作る。未入力なら、すべての曜日が休館、祝日は曜日どおり。 */
function scheduleFormOf(s: Schedule | null): ScheduleForm {
  const day = (h: Hours | null | undefined): DayForm =>
    h
      ? { open: true, openAt: h.open, closeAt: h.close }
      : { open: false, openAt: defaultOpen, closeAt: defaultClose };
  const days = {} as Record<Weekday, DayForm>;
  for (const { key } of weekdays) {
    days[key] = day(s?.weekly[key]);
  }
  const holiday = s?.holiday;
  return {
    days,
    holiday:
      holiday?.mode === "closed" || holiday?.mode === "hours"
        ? holiday.mode
        : "same",
    holidayOpen: holiday?.mode === "hours" ? holiday.open : defaultOpen,
    holidayClose: holiday?.mode === "hours" ? holiday.close : defaultClose,
  };
}

/** hoursOf は入力欄の時刻を確かめる。正しくなければ、画面に出す文言を返す。 */
function hoursOf(
  name: string,
  openAt: string,
  closeAt: string,
): Hours | string {
  const open = parseTimeOfDay(openAt);
  const close = parseTimeOfDay(closeAt);
  if (open === undefined || close === undefined) {
    return `${name}: 開館時刻と閉館時刻を入力してください`;
  }
  // HH:MM は文字列の順と時刻の順が同じ。
  if (open >= close) {
    return `${name}: 開館時刻は閉館時刻より前にしてください`;
  }
  return { open, close };
}

/** scheduleInputOf は入力欄の値を送る形にする。正しくなければ、画面に出す文言を返す。 */
function scheduleInputOf(
  form: ScheduleForm,
): { weekly: Record<Weekday, Hours | null>; holiday: HolidayInput } | string {
  const weekly = {} as Record<Weekday, Hours | null>;
  for (const { key, name } of weekdays) {
    const d = form.days[key];
    if (!d.open) {
      weekly[key] = null;
      continue;
    }
    const h = hoursOf(name, d.openAt, d.closeAt);
    if (typeof h === "string") {
      return h;
    }
    weekly[key] = h;
  }
  if (form.holiday !== "hours") {
    return { weekly, holiday: { mode: form.holiday } };
  }
  const h = hoursOf("祝日", form.holidayOpen, form.holidayClose);
  if (typeof h === "string") {
    return h;
  }
  return { weekly, holiday: { mode: "hours", ...h } };
}

/** closureFormError は臨時休館の入力を確かめる。正しければ undefined。 */
function closureFormError(f: ClosureForm): string | undefined {
  if (f.start === "" || f.end === "") {
    return "開始日と終了日を入力してください";
  }
  // YYYY-MM-DD は文字列の順と日付の順が同じ。
  if (f.start > f.end) {
    return "開始日は終了日以前にしてください";
  }
  return undefined;
}

/** 開いている入力欄。同時に開くのは1館の1つだけ。 */
type Editor =
  | {
      readonly kind: "schedule";
      readonly id: LibraryID;
      readonly form: ScheduleForm;
      readonly saving: boolean;
      readonly error?: string;
    }
  | {
      readonly kind: "closure";
      readonly id: LibraryID;
      readonly form: ClosureForm;
      readonly saving: boolean;
      readonly error?: string;
    };

interface ListState {
  /** 最後に読めた一覧。読み直しに失敗しても残す。 */
  readonly libraries: readonly Library[] | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
}

export function Libraries() {
  const handleError = useApiErrorHandler();
  const [state, setState] = useState<ListState>({
    libraries: undefined,
    loading: true,
    error: undefined,
  });
  const [editor, setEditor] = useState<Editor | undefined>(undefined);
  const [actionError, setActionError] = useState<string | undefined>(undefined);
  /** 外している途中の館・期間（"<id>" または "<id>#<index>"）。 */
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set());

  const load = useCallback(() => {
    fetchLibraries()
      .then((libraries) => {
        setState({ libraries, loading: false, error: undefined });
        // 再ログインの前に入力しかけた内容を、入力欄に戻す（research W12）。
        const schedule = takeDraft("schedule");
        const closure = takeDraft("closure");
        const target = (id: string) => libraries.find((l) => l.id === id)?.id;
        const sid = schedule && target(schedule.libraryId);
        const cid = closure && target(closure.libraryId);
        if (schedule && sid) {
          setEditor({
            kind: "schedule",
            id: sid,
            form: schedule.form,
            saving: false,
          });
        } else if (closure && cid) {
          setEditor({
            kind: "closure",
            id: cid,
            form: closure.form,
            saving: false,
          });
        }
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setState((prev) => ({
          libraries: prev.libraries,
          loading: false,
          error: messageOf(e),
        }));
      });
  }, [handleError]);

  useEffect(() => {
    load();
  }, [load]);

  const setLibraries = (
    change: (libs: readonly Library[]) => Library[],
  ): void => {
    setState((prev) =>
      prev.libraries ? { ...prev, libraries: change(prev.libraries) } : prev,
    );
  };
  const replaceLibrary = (updated: Library) => {
    setLibraries((libs) =>
      libs.map((l) => (l.id === updated.id ? updated : l)),
    );
  };
  const setBusyKey = (key: string, on: boolean) => {
    setBusy((prev) => {
      const next = new Set(prev);
      if (on) {
        next.add(key);
      } else {
        next.delete(key);
      }
      return next;
    });
  };

  // editorFailed は保存に失敗したとき、入力を残してメッセージを表示する。
  // 保存中にほかの入力欄を開いていたら、その入力は残す。
  const editorFailed = (failed: Editor) => (e: unknown) => {
    setEditor((prev) =>
      prev?.kind === failed.kind && prev.id === failed.id
        ? { ...failed, saving: false, error: messageOf(e) }
        : prev,
    );
  };
  const editorDone = (done: Editor) => (updated: Library) => {
    replaceLibrary(updated);
    setEditor((prev) =>
      prev?.kind === done.kind && prev.id === done.id ? undefined : prev,
    );
  };

  const saveSchedule = (id: LibraryID, form: ScheduleForm) => {
    const input = scheduleInputOf(form);
    if (typeof input === "string") {
      setEditor({ kind: "schedule", id, form, saving: false, error: input });
      return;
    }
    const current: Editor = { kind: "schedule", id, form, saving: true };
    setEditor(current);
    putSchedule(id, input)
      .then(editorDone(current))
      .catch(
        keepDraftOnUnauthenticated({ kind: "schedule", libraryId: id, form }),
      )
      .catch(handleError)
      .catch(editorFailed(current));
  };

  const saveClosure = (id: LibraryID, form: ClosureForm) => {
    const error = closureFormError(form);
    if (error !== undefined) {
      setEditor({ kind: "closure", id, form, saving: false, error });
      return;
    }
    const current: Editor = { kind: "closure", id, form, saving: true };
    setEditor(current);
    addClosure(id, { ...form, reason: form.reason.trim() })
      .then(editorDone(current))
      .catch(
        keepDraftOnUnauthenticated({ kind: "closure", libraryId: id, form }),
      )
      .catch(handleError)
      .catch(editorFailed(current));
  };

  const remove = (lib: Library) => {
    if (!window.confirm(`${lib.name}を外しますか？`)) {
      return;
    }
    setActionError(undefined);
    setBusyKey(lib.id, true);
    const removed = () => {
      setLibraries((libs) => libs.filter((l) => l.id !== lib.id));
      setEditor((prev) => (prev?.id === lib.id ? undefined : prev));
    };
    removeLibrary(lib.id)
      .then(removed)
      .catch(handleError)
      .catch((e: unknown) => {
        // ほかの端末ですでに外していた。
        if (e instanceof ApiError && e.status === 404) {
          removed();
          return;
        }
        setActionError(messageOf(e));
      })
      .finally(() => {
        setBusyKey(lib.id, false);
      });
  };

  const removePeriod = (lib: Library, index: number, c: Closure) => {
    if (!window.confirm(`臨時休館期間 ${describeClosure(c)}を外しますか？`)) {
      return;
    }
    const key = `${lib.id}#${String(index)}`;
    setActionError(undefined);
    setBusyKey(key, true);
    removeClosure(lib.id, index)
      .then(replaceLibrary)
      .catch(handleError)
      .catch((e: unknown) => {
        setActionError(messageOf(e));
        // 位置がずれたかもしれないので、一覧を読み直す。
        if (e instanceof ApiError && e.status === 404) {
          load();
        }
      })
      .finally(() => {
        setBusyKey(key, false);
      });
  };

  const libraries = state.libraries;
  return (
    <main className="screen library-settings">
      <h1>図書館</h1>
      {state.loading && libraries === undefined && <p>読み込んでいます…</p>}
      {state.error !== undefined && (
        <ErrorRetry
          message={state.error}
          onRetry={() => {
            setState((prev) => ({ ...prev, loading: true, error: undefined }));
            load();
          }}
        />
      )}
      {libraries && (
        <section aria-label="利用図書館の一覧">
          {libraries.length === 0 ? (
            <p>利用図書館は登録されていません</p>
          ) : (
            <ul className="items" aria-label="利用図書館">
              {libraries.map((l) => (
                <LibraryItem
                  key={l.id}
                  library={l}
                  editor={editor?.id === l.id ? editor : undefined}
                  removing={busy.has(l.id)}
                  removingPeriod={(i) => busy.has(`${l.id}#${String(i)}`)}
                  onEditSchedule={() => {
                    setEditor({
                      kind: "schedule",
                      id: l.id,
                      form: scheduleFormOf(l.schedule),
                      saving: false,
                    });
                  }}
                  onAddClosure={() => {
                    setEditor({
                      kind: "closure",
                      id: l.id,
                      form: { start: "", end: "", reason: "" },
                      saving: false,
                    });
                  }}
                  onChange={setEditor}
                  onCancel={() => {
                    setEditor(undefined);
                  }}
                  onSaveSchedule={(form) => {
                    saveSchedule(l.id, form);
                  }}
                  onSaveClosure={(form) => {
                    saveClosure(l.id, form);
                  }}
                  onRemove={() => {
                    remove(l);
                  }}
                  onRemovePeriod={(i, c) => {
                    removePeriod(l, i, c);
                  }}
                />
              ))}
            </ul>
          )}
          {actionError !== undefined && (
            <p className="error" role="alert">
              {actionError}
            </p>
          )}
        </section>
      )}
      <LibraryFinder
        registered={libraries ?? []}
        onAdded={(added) => {
          setLibraries((libs) =>
            libs.some((l) => l.id === added.id) ? [...libs] : [...libs, added],
          );
        }}
      />
      <Settings />
    </main>
  );
}

interface LibraryItemProps {
  library: Library;
  editor: Editor | undefined;
  removing: boolean;
  removingPeriod: (index: number) => boolean;
  onEditSchedule: () => void;
  onAddClosure: () => void;
  onChange: (editor: Editor) => void;
  onCancel: () => void;
  onSaveSchedule: (form: ScheduleForm) => void;
  onSaveClosure: (form: ClosureForm) => void;
  onRemove: () => void;
  onRemovePeriod: (index: number, c: Closure) => void;
}

/** 利用図書館の1館。館名・市区町村・開館スケジュールの概要・臨時休館期間と、各操作。 */
function LibraryItem({
  library: l,
  editor,
  removing,
  removingPeriod,
  onEditSchedule,
  onAddClosure,
  onChange,
  onCancel,
  onSaveSchedule,
  onSaveClosure,
  onRemove,
  onRemovePeriod,
}: LibraryItemProps) {
  return (
    <li className="item">
      <p className="title">{l.name}</p>
      {l.city !== "" && <p className="byline">{l.city}</p>}
      <ScheduleSummary schedule={l.schedule} />
      {l.closures.length > 0 && (
        <ul className="closures" aria-label="臨時休館期間">
          {l.closures.map((c, i) => (
            <li key={`${c.start}|${c.end}|${c.reason}`}>
              <span>{describeClosure(c)}</span>{" "}
              <button
                type="button"
                disabled={removingPeriod(i)}
                onClick={() => {
                  onRemovePeriod(i, c);
                }}
              >
                削除
              </button>
            </li>
          ))}
        </ul>
      )}
      {editor?.kind === "schedule" && (
        <ScheduleEditor
          form={editor.form}
          saving={editor.saving}
          error={editor.error}
          onChange={(form) => {
            onChange({ kind: "schedule", id: l.id, form, saving: false });
          }}
          onCancel={onCancel}
          onSave={onSaveSchedule}
        />
      )}
      {editor?.kind === "closure" && (
        <ClosureEditor
          form={editor.form}
          saving={editor.saving}
          error={editor.error}
          onChange={(form) => {
            onChange({ kind: "closure", id: l.id, form, saving: false });
          }}
          onCancel={onCancel}
          onSave={onSaveClosure}
        />
      )}
      {!editor && (
        <div className="actions">
          <button type="button" onClick={onEditSchedule}>
            開館時間を編集
          </button>
          <button type="button" onClick={onAddClosure}>
            臨時休館を追加
          </button>
          <button type="button" onClick={onRemove} disabled={removing}>
            外す
          </button>
        </div>
      )}
    </li>
  );
}

/** 開館スケジュールの概要。曜日ごとの時間、祝日の扱い、最終更新日。未入力なら「開館情報未入力」。 */
function ScheduleSummary({ schedule }: { schedule: Schedule | null }) {
  if (!schedule) {
    return <p className="meta">開館情報未入力</p>;
  }
  return (
    <>
      <ul className="schedule" aria-label="開館時間">
        {weekdays.map(({ key, label }) => {
          const h = schedule.weekly[key];
          return <li key={key}>{`${label} ${h ? hoursText(h) : "休館"}`}</li>;
        })}
        <li>{`祝日 ${holidayText(schedule.holiday)}`}</li>
      </ul>
      <p className="meta">{`最終更新 ${schedule.updatedOn}`}</p>
    </>
  );
}

interface EditorProps<F> {
  form: F;
  saving: boolean;
  error: string | undefined;
  onChange: (form: F) => void;
  onCancel: () => void;
  onSave: (form: F) => void;
}

/** 開館時間の編集。曜日ごとの開館／休館と時刻、祝日の扱い。「保存」で7曜日すべてを送る。 */
function ScheduleEditor({
  form,
  saving,
  error,
  onChange,
  onCancel,
  onSave,
}: EditorProps<ScheduleForm>) {
  const id = useId();
  const setDay = (key: Weekday, change: Partial<DayForm>) => {
    onChange({
      ...form,
      days: { ...form.days, [key]: { ...form.days[key], ...change } },
    });
  };
  return (
    <form
      className="schedule-editor"
      aria-label="開館時間の編集"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(form);
      }}
    >
      <fieldset disabled={saving}>
        {weekdays.map(({ key, name }) => {
          const d = form.days[key];
          return (
            <div className="day" key={key}>
              <label>
                <input
                  type="checkbox"
                  checked={d.open}
                  onChange={(e) => {
                    setDay(key, { open: e.target.checked });
                  }}
                />
                {`${name} 開館`}
              </label>
              {d.open && (
                <span className="hours">
                  <input
                    type="time"
                    aria-label={`${name} 開館時刻`}
                    value={d.openAt}
                    onChange={(e) => {
                      setDay(key, { openAt: e.target.value });
                    }}
                  />
                  〜
                  <input
                    type="time"
                    aria-label={`${name} 閉館時刻`}
                    value={d.closeAt}
                    onChange={(e) => {
                      setDay(key, { closeAt: e.target.value });
                    }}
                  />
                </span>
              )}
            </div>
          );
        })}
        <fieldset className="holiday">
          <legend>祝日の扱い</legend>
          {(
            [
              ["same", "曜日どおり"],
              ["closed", "休館"],
              ["hours", "時間を指定"],
            ] as const
          ).map(([mode, label]) => (
            <label key={mode}>
              <input
                type="radio"
                name={`${id}-holiday`}
                checked={form.holiday === mode}
                onChange={() => {
                  onChange({ ...form, holiday: mode });
                }}
              />
              {label}
            </label>
          ))}
          {form.holiday === "hours" && (
            <span className="hours">
              <input
                type="time"
                aria-label="祝日 開館時刻"
                value={form.holidayOpen}
                onChange={(e) => {
                  onChange({ ...form, holidayOpen: e.target.value });
                }}
              />
              〜
              <input
                type="time"
                aria-label="祝日 閉館時刻"
                value={form.holidayClose}
                onChange={(e) => {
                  onChange({ ...form, holidayClose: e.target.value });
                }}
              />
            </span>
          )}
        </fieldset>
        <div className="actions">
          <button type="submit">保存</button>
          <button type="button" onClick={onCancel}>
            取消
          </button>
        </div>
      </fieldset>
      {error !== undefined && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}

/** 臨時休館の追加。開始日・終了日・理由（任意）。 */
function ClosureEditor({
  form,
  saving,
  error,
  onChange,
  onCancel,
  onSave,
}: EditorProps<ClosureForm>) {
  const ids = { start: useId(), end: useId(), reason: useId() };
  return (
    <form
      className="closure-editor"
      aria-label="臨時休館の追加"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(form);
      }}
    >
      <fieldset disabled={saving}>
        <label htmlFor={ids.start}>開始日</label>
        <input
          id={ids.start}
          type="date"
          value={form.start}
          onChange={(e) => {
            onChange({ ...form, start: e.target.value });
          }}
        />
        <label htmlFor={ids.end}>終了日</label>
        <input
          id={ids.end}
          type="date"
          value={form.end}
          onChange={(e) => {
            onChange({ ...form, end: e.target.value });
          }}
        />
        <label htmlFor={ids.reason}>理由（任意）</label>
        <input
          id={ids.reason}
          type="text"
          value={form.reason}
          onChange={(e) => {
            onChange({ ...form, reason: e.target.value });
          }}
        />
        <div className="actions">
          <button type="submit">追加</button>
          <button type="button" onClick={onCancel}>
            取消
          </button>
        </div>
      </fieldset>
      {error !== undefined && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}

// 図書館を探す

type SearchState =
  | { readonly kind: "idle" }
  | { readonly kind: "loading"; readonly query: LibraryQuery }
  | {
      readonly kind: "done";
      readonly query: LibraryQuery;
      readonly result: LibrarySearch;
    }
  | {
      readonly kind: "error";
      readonly query: LibraryQuery;
      readonly message: string;
    };

/** notFoundText は 0件のときの文言（001 の lib find と同じ）。 */
function notFoundText(q: LibraryQuery): string {
  const area = [q.pref, q.city ?? ""].filter((s) => s !== "").join(" ");
  return q.name
    ? `「${area}」で「${q.name}」を含む図書館は見つかりませんでした`
    : `「${area}」の図書館は見つかりませんでした。市区町村の表記（例: 世田谷区）を確かめてください`;
}

interface FinderProps {
  /** 利用図書館。検索結果のうち、ここにある館は「登録済み」にする。 */
  registered: readonly Library[];
  onAdded: (library: Library) => void;
}

/** 図書館を探して登録する（contracts/screens.md「図書館」の「図書館を探す」）。 */
function LibraryFinder({ registered, onAdded }: FinderProps) {
  const handleError = useApiErrorHandler();
  const ids = { pref: useId(), city: useId(), name: useId() };
  const [form, setForm] = useState({ pref: "", city: "", name: "" });
  const [formError, setFormError] = useState<string | undefined>(undefined);
  const [search, setSearch] = useState<SearchState>({ kind: "idle" });

  const run = (query: LibraryQuery) => {
    setSearch({ kind: "loading", query });
    searchLibraries(query)
      .then((result) => {
        setSearch({ kind: "done", query, result });
      })
      .catch(handleError)
      .catch((e: unknown) => {
        setSearch({ kind: "error", query, message: messageOf(e) });
      });
  };

  const onSubmit = () => {
    if (form.pref === "") {
      setFormError("都道府県を選んでください");
      return;
    }
    setFormError(undefined);
    run({ pref: form.pref, city: form.city.trim(), name: form.name.trim() });
  };

  return (
    <section className="library-finder" aria-label="図書館を探す">
      <h2>図書館を探す</h2>
      <form
        className="search-form"
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
      >
        <label htmlFor={ids.pref}>都道府県</label>
        <select
          id={ids.pref}
          value={form.pref}
          onChange={(e) => {
            setForm({ ...form, pref: e.target.value });
            setFormError(undefined);
          }}
        >
          <option value="">選択してください</option>
          {prefectures.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
        <label htmlFor={ids.city}>市区町村</label>
        <input
          id={ids.city}
          type="text"
          value={form.city}
          onChange={(e) => {
            setForm({ ...form, city: e.target.value });
          }}
        />
        <label htmlFor={ids.name}>館名</label>
        <input
          id={ids.name}
          type="text"
          value={form.name}
          onChange={(e) => {
            setForm({ ...form, name: e.target.value });
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
        <div className="search-result">
          {search.result.libraries.length === 0 ? (
            <p>{notFoundText(search.query)}</p>
          ) : (
            <ul className="items" aria-label="検索結果">
              {search.result.libraries.map((c) => (
                <Candidate
                  key={c.id}
                  candidate={c}
                  registered={
                    c.registered || registered.some((l) => l.id === c.id)
                  }
                  onAdded={onAdded}
                />
              ))}
            </ul>
          )}
          <p className="credit">
            <a href={search.result.credit.url} target="_blank" rel="noreferrer">
              {search.result.credit.label}
            </a>
          </p>
        </div>
      )}
    </section>
  );
}

interface CandidateProps {
  candidate: LibraryCandidate;
  registered: boolean;
  onAdded: (library: Library) => void;
}

/** 検索結果の1館。館名・市区町村・住所と「登録」。 */
function Candidate({ candidate: c, registered, onAdded }: CandidateProps) {
  const handleError = useApiErrorHandler();
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const add = () => {
    setAdding(true);
    setError(undefined);
    addLibrary(c.id)
      .then(onAdded)
      .catch(handleError)
      .catch((e: unknown) => {
        setError(messageOf(e));
      })
      .finally(() => {
        setAdding(false);
      });
  };

  return (
    <li className="item">
      <p className="title">{c.name}</p>
      {c.city !== "" && <p className="byline">{c.city}</p>}
      {c.address !== "" && <p className="meta">{c.address}</p>}
      {registered ? (
        <button type="button" disabled>
          登録済み
        </button>
      ) : (
        <button type="button" onClick={add} disabled={adding}>
          登録
        </button>
      )}
      {error !== undefined && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </li>
  );
}

// 設定（図書館の画面の下部）

/** ログアウトと、すべての端末からのログアウト（FR-111）。 */
function Settings() {
  const handleError = useApiErrorHandler();
  const loggedOut = useLoggedOut();
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const run = (request: () => Promise<undefined>) => {
    setSending(true);
    setError(undefined);
    request()
      .then(loggedOut)
      .catch(handleError)
      .catch((e: unknown) => {
        setError(messageOf(e));
        setSending(false);
      });
  };

  return (
    <section className="settings" aria-label="設定">
      <h2>設定</h2>
      <div className="actions">
        <button
          type="button"
          disabled={sending}
          onClick={() => {
            run(logout);
          }}
        >
          ログアウト
        </button>
        <button
          type="button"
          disabled={sending}
          onClick={() => {
            if (window.confirm("すべての端末からログアウトしますか？")) {
              run(revokeAllSessions);
            }
          }}
        >
          すべての端末からログアウト
        </button>
      </div>
      {error !== undefined && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
