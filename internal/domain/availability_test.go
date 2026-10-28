package domain

import (
	"testing"
	"time"
)

func TestAvailabilityOf(t *testing.T) {
	fetched := time.Date(2026, 10, 21, 14, 0, 0, 0, Tokyo())
	now := fetched.Add(5 * time.Minute)

	tests := []struct {
		name      string
		status    SystemStatus
		byLibKey  map[string]string
		want      Availability
		wantRaw   string
		wantLabel string
	}{
		{name: "貸出可", status: SystemOK, byLibKey: map[string]string{"中央": "貸出可"}, want: AvailableToLend, wantRaw: "貸出可", wantLabel: "在架（貸出可）"},
		{name: "館内のみ", status: SystemOK, byLibKey: map[string]string{"中央": "館内のみ"}, want: InLibraryOnly, wantRaw: "館内のみ", wantLabel: "在架（館内閲覧のみ）"},
		{name: "貸出中", status: SystemOK, byLibKey: map[string]string{"中央": "貸出中"}, want: OnLoan, wantRaw: "貸出中", wantLabel: "貸出中"},
		{name: "予約中はカーリルの値をそのまま表示", status: SystemOK, byLibKey: map[string]string{"中央": "予約中"}, want: Reserved, wantRaw: "予約中", wantLabel: "予約中"},
		{name: "蔵書なし", status: SystemOK, byLibKey: map[string]string{"中央": "蔵書なし"}, want: NotHeld, wantRaw: "蔵書なし", wantLabel: "所蔵なし"},
		{name: "応答に館がない（OK）", status: SystemOK, byLibKey: map[string]string{"経堂": "貸出可"}, want: NotHeld, wantRaw: "", wantLabel: "所蔵なし"},
		{name: "応答に館がない（Cache）", status: SystemCache, byLibKey: map[string]string{}, want: NotHeld, wantRaw: "", wantLabel: "所蔵なし"},
		{name: "Cache の貸出可", status: SystemCache, byLibKey: map[string]string{"中央": "貸出可"}, want: AvailableToLend, wantRaw: "貸出可", wantLabel: "在架（貸出可）"},
		{name: "蔵書あり", status: SystemOK, byLibKey: map[string]string{"中央": "蔵書あり"}, want: Unknown, wantRaw: "蔵書あり", wantLabel: "不明"},
		{name: "準備中", status: SystemOK, byLibKey: map[string]string{"中央": "準備中"}, want: Unknown, wantRaw: "準備中", wantLabel: "不明"},
		{name: "休館中", status: SystemOK, byLibKey: map[string]string{"中央": "休館中"}, want: Unknown, wantRaw: "休館中", wantLabel: "不明"},
		{name: "未知の値", status: SystemOK, byLibKey: map[string]string{"中央": "整理中"}, want: Unknown, wantRaw: "整理中", wantLabel: "不明"},
		{name: "システムが Error", status: SystemError, byLibKey: map[string]string{"中央": "貸出可"}, want: Unknown, wantRaw: "貸出可", wantLabel: "不明"},
		{name: "システムが Running のまま（タイムアウト）", status: SystemRunning, byLibKey: map[string]string{"中央": "貸出可"}, want: Unknown, wantRaw: "貸出可", wantLabel: "不明"},
		{name: "システムが Error で館がない", status: SystemError, byLibKey: nil, want: Unknown, wantRaw: "", wantLabel: "不明"},
		{name: "未知のシステム status", status: SystemStatus("Busy"), byLibKey: map[string]string{"中央": "貸出可"}, want: Unknown, wantRaw: "貸出可", wantLabel: "不明"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := HoldingSnapshot{
				ISBN:         "9784003101018",
				SystemID:     "Tokyo_Setagaya",
				SystemStatus: tt.status,
				ByLibKey:     tt.byLibKey,
				FetchedAt:    fetched,
			}
			got, raw, stale := AvailabilityOf(snap, "中央", now)
			if got != tt.want {
				t.Errorf("Availability = %v, want %v", got, tt.want)
			}
			if raw != tt.wantRaw {
				t.Errorf("raw = %q, want %q", raw, tt.wantRaw)
			}
			if stale {
				t.Error("stale = true, want false")
			}
			if got.Label() != tt.wantLabel {
				t.Errorf("Label() = %q, want %q", got.Label(), tt.wantLabel)
			}
		})
	}
}

func TestAvailabilityOfStale(t *testing.T) {
	fetched := time.Date(2026, 10, 21, 13, 0, 0, 0, Tokyo())
	snap := HoldingSnapshot{
		ISBN:         "9784003101018",
		SystemID:     "Tokyo_Setagaya",
		SystemStatus: SystemOK,
		ByLibKey:     map[string]string{"中央": "貸出可"},
		FetchedAt:    fetched,
	}
	tests := []struct {
		elapsed   time.Duration
		wantStale bool
	}{
		{elapsed: 0, wantStale: false},
		{elapsed: 30 * time.Minute, wantStale: false},
		{elapsed: 30*time.Minute + time.Second, wantStale: true},
		{elapsed: 2 * time.Hour, wantStale: true},
	}
	for _, tt := range tests {
		t.Run(tt.elapsed.String(), func(t *testing.T) {
			got, raw, stale := AvailabilityOf(snap, "中央", fetched.Add(tt.elapsed))
			if stale != tt.wantStale {
				t.Errorf("stale = %v, want %v", stale, tt.wantStale)
			}
			// 古くても元の値は保持する（表示で取得日時と併記するため）。
			if got != AvailableToLend || raw != "貸出可" {
				t.Errorf("got (%v, %q), want (AvailableToLend, 貸出可)", got, raw)
			}
		})
	}
}
