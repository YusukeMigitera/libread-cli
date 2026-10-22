package domain

import (
	"fmt"
	"strings"
)

// LibraryID はカーリルの図書館システムID（systemid）と館キー（libkey）の組。
type LibraryID struct {
	SystemID string
	LibKey   string
}

// ParseLibraryID は "SystemID:LibKey" 形式を読む。最初の ":" で区切るので、LibKey は ":" を含んでよい。
func ParseLibraryID(s string) (LibraryID, error) {
	system, key, ok := strings.Cut(s, ":")
	if !ok || system == "" || key == "" {
		return LibraryID{}, fmt.Errorf("図書館ID %q: SystemID:LibKey 形式で指定してください", s)
	}
	return LibraryID{SystemID: system, LibKey: key}, nil
}

func (id LibraryID) String() string { return id.SystemID + ":" + id.LibKey }
