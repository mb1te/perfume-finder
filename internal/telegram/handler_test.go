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
	searcher := fakeSearcher{result: search.Result{Offers: []domain.Offer{{ShopID: "orental", Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail, PriceKopecks: 1402800, InStock: true, URL: "https://orental.ru/sauvage"}, {ShopID: "allure", Brand: "Christian Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT, VolumeMicroliters: 100000, Kind: domain.ProductKindTester, PriceKopecks: 1200000, InStock: true, URL: "https://allureparfum.ru/sauvage"}}, Failures: []search.Failure{{ShopID: "duhirf"}}}}
	h := NewHandler(messenger, sessions, searcher)
	ctx := context.Background()
	if err := h.HandleMessage(ctx, 42, "Dior Sauvage"); err != nil {
		t.Fatal(err)
	}
	if sessions.values[42].Stage == "choose_fragrance" {
		if err := h.HandleCallback(ctx, 42, buttonData(t, messenger, "|fragrance|0")); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.HandleCallback(ctx, 42, buttonData(t, messenger, "|concentration|edt")); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleCallback(ctx, 42, buttonData(t, messenger, "|volume|100000")); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleCallback(ctx, 42, buttonData(t, messenger, "|kind|all")); err != nil {
		t.Fatal(err)
	}
	last := messenger.messages[len(messenger.messages)-1].Text
	for _, want := range []string{"Флакон", "Тестер", "14 028 ₽", "12 000 ₽", "duhirf", "Без доставки"} {
		if !strings.Contains(last, want) {
			t.Fatalf("message %q lacks %q", last, want)
		}
	}
}

func TestHandlerDiscoversAmbiguousFragrancesBeforeConcentration(t *testing.T) {
	m := &fakeMessenger{}
	sessions := newMemorySessions()
	searcher := fakeSearcher{result: search.Result{Offers: []domain.Offer{{Brand: "Christian Dior", Name: "Sauvage", Edition: "2015"}, {Brand: "Christian Dior", Name: "Eau Sauvage"}}}}
	h := NewHandler(m, sessions, searcher)
	if err := h.HandleMessage(context.Background(), 9, "Dior Sauvage"); err != nil {
		t.Fatal(err)
	}
	session := sessions.values[9]
	if session.Stage != "choose_fragrance" || len(session.Options) != 2 {
		t.Fatalf("session=%+v", session)
	}
	if !strings.Contains(m.messages[len(m.messages)-1].Buttons[0].Data, "|fragrance|") {
		t.Fatal("fragrance choice missing")
	}
}

func TestHandlerParsesCompleteMultiwordBrandQuery(t *testing.T) {
	m := &fakeMessenger{}
	sessions := newMemorySessions()
	searcher := fakeSearcher{result: search.Result{Offers: []domain.Offer{{Brand: "Tom Ford", Name: "Ombre Leather", Concentration: domain.ConcentrationEDP, VolumeMicroliters: 100000, Kind: domain.ProductKindRetail}}}}
	h := NewHandler(m, sessions, searcher)
	if err := h.HandleMessage(context.Background(), 10, "Tom Ford Ombre Leather EDP 100 ml"); err != nil {
		t.Fatal(err)
	}
	session := sessions.values[10]
	if session.Query.Brand != "Tom Ford" || session.Query.Name != "Ombre Leather" || session.Query.Concentration != domain.ConcentrationEDP || session.Query.VolumeMicroliters != 100000 || session.Stage != "choose_kind" {
		t.Fatalf("session=%+v", session)
	}
}

func TestHandlerRejectsCallbackFromSupersededSession(t *testing.T) {
	m := &fakeMessenger{}
	sessions := newMemorySessions()
	searcher := fakeSearcher{result: search.Result{Offers: []domain.Offer{{Brand: "Dior", Name: "Sauvage"}}}}
	h := NewHandler(m, sessions, searcher)
	ctx := context.Background()
	_ = h.HandleMessage(ctx, 11, "Dior Sauvage")
	old := m.messages[len(m.messages)-1].Buttons[0].Data
	_ = h.HandleMessage(ctx, 11, "Dior Sauvage")
	current := sessions.values[11].ID
	if err := h.HandleCallback(ctx, 11, old); err == nil {
		t.Fatal("stale callback accepted")
	}
	if sessions.values[11].ID != current {
		t.Fatal("new session was modified")
	}
}

func buttonData(t *testing.T, messenger *fakeMessenger, suffix string) string {
	t.Helper()
	message := messenger.messages[len(messenger.messages)-1]
	for _, button := range message.Buttons {
		if strings.Contains(button.Data, suffix) {
			return button.Data
		}
	}
	t.Fatalf("button %s not found", suffix)
	return ""
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
