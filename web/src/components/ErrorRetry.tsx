interface Props {
  message: string;
  onRetry: () => void;
}

/** 通信に失敗したときの表示（contracts/screens.md「共通」、FR-116）。 */
export function ErrorRetry({ message, onRetry }: Props) {
  return (
    <div className="error-retry" role="alert">
      <p>{message}</p>
      <button type="button" onClick={onRetry}>
        再試行
      </button>
    </div>
  );
}
