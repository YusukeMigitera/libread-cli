package domain

import (
	"testing"
	"time"
)

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in      string
		want    TimeOfDay
		wantErr bool
	}{
		{in: "00:00", want: 0},
		{in: "09:30", want: 9*60 + 30},
		{in: "23:59", want: 23*60 + 59},
		{in: "24:00", want: 24 * 60},
		{in: "24:01", wantErr: true},
		{in: "25:00", wantErr: true},
		{in: "12:60", wantErr: true},
		{in: "9:00", wantErr: true},
		{in: "09-00", wantErr: true},
		{in: "-1:00", wantErr: true},
		{in: "ab:cd", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTimeOfDay(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTimeOfDay(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimeOfDay(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseTimeOfDay(%q) = %d, want %d", tt.in, got, tt.want)
			}
			if got.String() != tt.in {
				t.Errorf("String() = %q, want %q", got.String(), tt.in)
			}
		})
	}
}

func TestNewHours(t *testing.T) {
	nine, ten := TimeOfDay(9*60), TimeOfDay(10*60)
	if _, err := NewHours(nine, ten); err != nil {
		t.Errorf("NewHours(09:00, 10:00) error: %v", err)
	}
	if _, err := NewHours(ten, nine); err == nil {
		t.Error("NewHours(10:00, 09:00) want error")
	}
	if _, err := NewHours(nine, nine); err == nil {
		t.Error("NewHours(09:00, 09:00) want error")
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{in: "2026-10-21"},
		{in: "2028-02-29"},
		{in: "2026-02-29", wantErr: true},
		{in: "2026-13-01", wantErr: true},
		{in: "2026-1-01", wantErr: true},
		{in: "2026/10/21", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseDate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q) error: %v", tt.in, err)
			}
			if got.String() != tt.in {
				t.Errorf("String() = %q, want %q", got.String(), tt.in)
			}
		})
	}
}

func TestDateOfUsesTokyo(t *testing.T) {
	// UTC 2026-10-20 15:30 は日本時間 2026-10-21 00:30。
	utc := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC)
	if got := DateOf(utc).String(); got != "2026-10-21" {
		t.Errorf("DateOf(%v) = %s, want 2026-10-21", utc, got)
	}
}

func TestDateCompare(t *testing.T) {
	a, _ := ParseDate("2026-10-20")
	b, _ := ParseDate("2026-10-21")
	if !a.Before(b) || b.Before(a) || a.Before(a) {
		t.Error("Before の結果が不正")
	}
	if !b.After(a) || a.After(b) || a.After(a) {
		t.Error("After の結果が不正")
	}
}
