package domain

import (
	"fmt"
	"strings"
)

// ISBN は13桁に正規化した ISBN。ParseISBN でのみ作る。
type ISBN string

// ParseISBN はハイフン付き・10桁の ISBN も受け付け、13桁へ正規化する。
func ParseISBN(s string) (ISBN, error) {
	digits := strings.ToUpper(strings.ReplaceAll(s, "-", ""))
	switch len(digits) {
	case 13:
		if !allDigits(digits) {
			return "", fmt.Errorf("ISBN %q: 数字以外の文字が含まれています", s)
		}
		if isbn13CheckDigit(digits[:12]) != digits[12] {
			return "", fmt.Errorf("ISBN %q: チェックディジットが正しくありません", s)
		}
		return ISBN(digits), nil
	case 10:
		if !allDigits(digits[:9]) || !(isDigit(digits[9]) || digits[9] == 'X') {
			return "", fmt.Errorf("ISBN %q: 数字以外の文字が含まれています", s)
		}
		if isbn10CheckDigit(digits[:9]) != digits[9] {
			return "", fmt.Errorf("ISBN %q: チェックディジットが正しくありません", s)
		}
		body := "978" + digits[:9]
		return ISBN(body + string(isbn13CheckDigit(body))), nil
	default:
		return "", fmt.Errorf("ISBN %q: 10桁または13桁で指定してください", s)
	}
}

func (i ISBN) String() string { return string(i) }

func isbn13CheckDigit(first12 string) byte {
	sum := 0
	for i := range 12 {
		d := int(first12[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}

func isbn10CheckDigit(first9 string) byte {
	sum := 0
	for i := range 9 {
		sum += int(first9[i]-'0') * (10 - i)
	}
	r := (11 - sum%11) % 11
	if r == 10 {
		return 'X'
	}
	return byte('0' + r)
}

func allDigits(s string) bool {
	for i := range len(s) {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func isDigit(b byte) bool { return '0' <= b && b <= '9' }
