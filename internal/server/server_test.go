package server

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"libread/internal/calil"
	"libread/internal/domain"
	"libread/internal/holiday"
	"libread/internal/ndl"
	"libread/internal/store"
)

func newBareServer(t *testing.T, mutate ...func(*Config)) *server {
	t.Helper()
	ts := newTestServer(t, mutate...)
	return &server{cfg: ts.cfg, api: http.NewServeMux(), auth: http.NewServeMux()}
}

// research W3: 2つの端末からほぼ同時に保存しても、ほかの変更は失われない。
func TestUpdateDataSerializesReadModifyWrite(t *testing.T) {
	s := newBareServer(t)
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.updateData(func(d *store.Data) error {
				isbn, err := domain.ParseISBN(validISBNs[i])
				if err != nil {
					return err
				}
				b, err := domain.NewBook(isbn, "本", "", "")
				if err != nil {
					return err
				}
				d.Books = append(d.Books, domain.ReadingBook{Book: b, AddedOn: domain.DateOf(testNow)})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	d, err := s.loadData()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Books) != n {
		t.Errorf("本 = %d 冊, want %d（同時の保存で失われた）", len(d.Books), n)
	}
}

func TestUpdateDataDoesNotSaveOnError(t *testing.T) {
	s := newBareServer(t)
	want := errors.New("変更できない")
	if _, err := s.updateData(func(d *store.Data) error {
		d.Books = append(d.Books, domain.ReadingBook{})
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	d, err := s.loadData()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Books) != 0 {
		t.Errorf("エラーなのに保存された: %+v", d.Books)
	}
}

// research W9: 外部サービスへの要求は、サービスごとに同時に1つまで。別のサービスは同時に使える。
func TestExternalServicesAreSerializedPerService(t *testing.T) {
	s := newBareServer(t)
	var calls = map[string]func(fn func()) error{
		"calil":    func(fn func()) error { return s.withCalil(func(*calil.Client) error { fn(); return nil }) },
		"ndl":      func(fn func()) error { return s.withNDL(func(*ndl.Client) error { fn(); return nil }) },
		"holidays": func(fn func()) error { return s.withHolidays(func(*holiday.Client) error { fn(); return nil }) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var inFlight, maxInFlight atomic.Int32
			var wg sync.WaitGroup
			for range 5 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					err := call(func() {
						n := inFlight.Add(1)
						for {
							m := maxInFlight.Load()
							if n <= m || maxInFlight.CompareAndSwap(m, n) {
								break
							}
						}
						time.Sleep(5 * time.Millisecond)
						inFlight.Add(-1)
					})
					if err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			if m := maxInFlight.Load(); m != 1 {
				t.Errorf("同時に %d 件の要求があった, want 1", m)
			}
		})
	}

	// カーリルを使っているあいだも NDL サーチは使える。
	done := make(chan struct{})
	go func() {
		s.withCalil(func(*calil.Client) error { <-done; return nil })
	}()
	time.Sleep(5 * time.Millisecond)
	ok := make(chan struct{})
	go func() {
		s.withNDL(func(*ndl.Client) error { return nil })
		close(ok)
	}()
	select {
	case <-ok:
	case <-time.After(time.Second):
		t.Error("カーリルを使っているあいだ NDL サーチが待たされた")
	}
	close(done)
}

func TestWithCalilRequiresAppKey(t *testing.T) {
	s := newBareServer(t, func(c *Config) { c.CalilAppKey = "" })
	called := false
	if err := s.withCalil(func(*calil.Client) error { called = true; return nil }); !errors.Is(err, errNotConfigured) {
		t.Errorf("err = %v, want errNotConfigured", err)
	}
	if called {
		t.Error("キーがないのにカーリルを呼んだ")
	}
}

// validISBNs はチェックディジットの正しい ISBN-13（978400310000? からの連番）。
var validISBNs = func() []string {
	var out []string
	for i := range 20 {
		body := fmt.Sprintf("978400310%03d", i)
		sum := 0
		for j, c := range body {
			d := int(c - '0')
			if j%2 == 1 {
				d *= 3
			}
			sum += d
		}
		out = append(out, body+string(rune('0'+(10-sum%10)%10)))
	}
	return out
}()
