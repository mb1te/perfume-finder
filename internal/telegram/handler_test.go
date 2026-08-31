package telegram

import (
	"context"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/search"
	"parfumes_finder/internal/storage"
	"strings"
	"testing"
)

func TestHandlerGuidesQueryAndRendersGroupedResults(t *testing.T) {
	messenger := &fakeMessenger{}
	sessions := newMemorySessions()
	searcher := fakeSearcher{result: search.Result{Offers: []domain.Offer{{ShopID: "orental", Brand: "Christian Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail, PriceKopecks: 1402800, InStock: true, URL: "https://orental.ru/sauvage"}, {ShopID: "allure", Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindTester, PriceKopecks: 1200000, InStock: true, URL: "https://allureparfum.ru/sauvage"}}, Failures: []search.Failure{{ShopID: "duhirf"}}}}
	h := NewHandler(messenger, sessions, searcher)
	ctx := context.Background()
	if err := h.HandleMessage(ctx, 42, "Dior Sauvage"); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleCallback(ctx, 42, "concentration:edt"); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleCallback(ctx, 42, "volume:100000"); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleCallback(ctx, 42, "kind:all"); err != nil {
		t.Fatal(err)
	}
	last := messenger.messages[len(messenger.messages)-1].Text
	for _, want := range []string{"Флакон", "Тестер", "14 028 ₽", "12 000 ₽", "duhirf", "Без доставки"} {
		if !strings.Contains(last, want) {
			t.Fatalf("message %q lacks %q", last, want)
		}
	}
}

type fakeMessenger struct{ messages []Message }

func (m *fakeMessenger) Send(_ context.Context, _ int64, msg Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

type fakeSearcher struct{ result search.Result }

func (f fakeSearcher) Search(context.Context, domain.SearchQuery) search.Result { return f.result }

type memorySessions struct{ values map[int64]storage.Session }

func newMemorySessions() *memorySessions { return &memorySessions{map[int64]storage.Session{}} }
func (s *memorySessions) Save(_ context.Context, v storage.Session) error {
	s.values[v.ChatID] = v
	return nil
}
func (s *memorySessions) Load(_ context.Context, id int64) (storage.Session, bool, error) {
	v, ok := s.values[id]
	return v, ok, nil
}
func (s *memorySessions) Delete(_ context.Context, id int64) error { delete(s.values, id); return nil }
