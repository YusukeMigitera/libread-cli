package domain

import "testing"

func TestParseLibraryID(t *testing.T) {
	tests := []struct {
		in      string
		want    LibraryID
		wantErr bool
	}{
		{in: "Tokyo_Setagaya:中央", want: LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "中央"}},
		{in: "Tokyo_Setagaya:分室:A", want: LibraryID{SystemID: "Tokyo_Setagaya", LibKey: "分室:A"}},
		{in: ":中央", wantErr: true},
		{in: "Tokyo_Setagaya:", wantErr: true},
		{in: "Tokyo_Setagaya", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseLibraryID(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLibraryID(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLibraryID(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseLibraryID(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if got.String() != tt.in {
				t.Errorf("String() = %q, want %q", got.String(), tt.in)
			}
		})
	}
}
