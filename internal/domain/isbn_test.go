package domain

import "testing"

func TestParseISBN(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    ISBN
		wantErr bool
	}{
		{name: "13桁", in: "9784003101018", want: "9784003101018"},
		{name: "13桁ハイフン付き", in: "978-4-00-310101-8", want: "9784003101018"},
		{name: "10桁を13桁へ変換", in: "4003101014", want: "9784003101018"},
		{name: "10桁ハイフン付き", in: "4-00-310101-4", want: "9784003101018"},
		{name: "10桁チェックディジットX", in: "480612057X", want: "9784806120575"},
		{name: "10桁チェックディジット小文字x", in: "480612057x", want: "9784806120575"},
		{name: "13桁チェックディジット不正", in: "9784003101019", wantErr: true},
		{name: "10桁チェックディジット不正", in: "4003101015", wantErr: true},
		{name: "桁数不足", in: "978400310101", wantErr: true},
		{name: "桁数超過", in: "97840031010180", wantErr: true},
		{name: "数字以外", in: "978400310101A", wantErr: true},
		{name: "13桁の途中にX", in: "97840031X1018", wantErr: true},
		{name: "空文字", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseISBN(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseISBN(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseISBN(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseISBN(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
