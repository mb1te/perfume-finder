package telegram

import (
	"context"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/search"
	"parfumes_finder/internal/storage"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
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
	if sessions.values[42].Stage == "choose_concentration" {
		if err := h.HandleCallback(ctx, 42, buttonData(t, messenger, "|concentration|edt")); err != nil {
			t.Fatal(err)
		}
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
	if session.Stage != "choose_fragrance" || len(session.Candidates) != 2 {
		t.Fatalf("session=%+v", session)
	}
	if !strings.Contains(m.messages[len(m.messages)-1].Buttons[0].Data, "|fragrance|") {
		t.Fatal("fragrance choice missing")
	}
}

func TestCandidatesCarryOnlyObservedConcentrations(t *testing.T) {
	offers := []domain.Offer{
		{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP},
		{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDT},
		{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationUnknown},
		{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP},
	}
	got := candidatesFromOffers(offers, domain.SearchQuery{})
	want := []domain.Concentration{domain.ConcentrationEDT, domain.ConcentrationEDP}
	if len(got) != 1 || !reflect.DeepEqual(got[0].Concentrations, want) {
		t.Fatalf("candidates = %+v, want concentrations %v", got, want)
	}
}

func TestSingleObservedConcentrationIsSelectedAutomatically(t *testing.T) {
	messenger := &fakeMessenger{}
	sessions := newMemorySessions()
	h := NewHandler(messenger, sessions, fakeSearcher{result: search.Result{Offers: []domain.Offer{{
		Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
	}}}})
	if err := h.HandleMessage(context.Background(), 42, "Dior Sauvage"); err != nil {
		t.Fatal(err)
	}
	if sessions.values[42].Query.Concentration != domain.ConcentrationEDP {
		t.Fatalf("query = %+v", sessions.values[42].Query)
	}
	if strings.Contains(messenger.messages[len(messenger.messages)-1].Text, "Выбери концентрацию") {
		t.Fatal("single concentration prompted the user")
	}
}

func TestMultipleObservedConcentrationsAreTheOnlyButtons(t *testing.T) {
	message := concentrationMessage("session-1", []domain.Concentration{
		domain.ConcentrationElixir, domain.ConcentrationEDP,
	})
	got := make([]string, 0, len(message.Buttons))
	for _, button := range message.Buttons {
		got = append(got, button.Text)
	}
	if want := []string{"EDP", "Elixir"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("buttons = %v, want %v", got, want)
	}
}

func TestNoObservedConcentrationRequestsExplicitRetry(t *testing.T) {
	messenger := &fakeMessenger{}
	sessions := newMemorySessions()
	h := NewHandler(messenger, sessions, fakeSearcher{})
	session := storage.Session{ChatID: 42, ID: "session-1", Query: domain.SearchQuery{
		Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationUnknown,
	}}
	sessions.values[42] = session
	if err := h.advance(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	got := messenger.messages[len(messenger.messages)-1]
	if !strings.Contains(got.Text, "Dior Sauvage EDP") || len(got.Buttons) != 0 {
		t.Fatalf("message = %+v", got)
	}
	if _, ok := sessions.values[42]; ok {
		t.Fatal("retry session was not deleted")
	}
}

func TestExplicitConcentrationWinsWhenDiscoveryHasAnotherValue(t *testing.T) {
	candidates := candidatesFromOffers([]domain.Offer{{
		Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDT,
	}}, domain.SearchQuery{Concentration: domain.ConcentrationEDP})
	got := mergeCandidate(domain.SearchQuery{Concentration: domain.ConcentrationEDP}, candidates[0].Query)
	if got.Concentration != domain.ConcentrationEDP {
		t.Fatalf("concentration = %q", got.Concentration)
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
	searcher := fakeSearcher{result: search.Result{Offers: []domain.Offer{{Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP}}}}
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

func TestConcurrentNewSearchCannotBeOverwrittenByCanceledDiscovery(t *testing.T) {
	started := make(chan struct{})
	searcher := &supersedingSearcher{started: started}
	sessions := newMemorySessions()
	messenger := &fakeMessenger{}
	handler := NewHandler(messenger, sessions, searcher)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() { defer wait.Done(); _ = handler.HandleMessage(context.Background(), 12, "Old Query") }()
	<-started
	if err := handler.HandleMessage(context.Background(), 12, "New Query"); err != nil {
		t.Fatal(err)
	}
	wait.Wait()
	if got := sessions.values[12].Query.Name; got != "New" {
		t.Fatalf("session overwritten with %q", got)
	}
	if got := len(messenger.messages); got != 1 {
		t.Fatalf("sent %d messages, want only the current search message", got)
	}
}

func TestRepeatedKindCallbackSupersedesPriorSearchOperation(t *testing.T) {
	searcher := &callbackSupersedingSearcher{
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan context.Context, 1),
		releaseSecond: make(chan struct{}),
	}
	sessions := newCallbackSessions(storage.Session{
		ChatID: 77,
		ID:     "session-1",
		Stage:  "choose_kind",
		Query: domain.SearchQuery{
			Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
			VolumeMicroliters: 100000,
		},
	})
	messenger := &fakeMessenger{}
	handler := NewHandler(messenger, sessions, searcher)
	callback := "session-1|kind|retail"

	done := make(chan error, 2)
	go func() {
		done <- handler.HandleCallback(context.Background(), 77, callback)
	}()
	go func() {
		done <- handler.HandleCallback(context.Background(), 77, callback)
	}()
	<-searcher.firstStarted
	secondCtx := <-searcher.secondStarted

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(searcher.releaseSecond)
		<-done
		t.Fatal("superseded first callback did not return")
	}
	secondCanceledByFirst := secondCtx.Err() != nil
	close(searcher.releaseSecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if secondCanceledByFirst {
		t.Fatal("first callback canceled the current callback operation")
	}
	if got := len(messenger.messages); got != 1 {
		t.Fatalf("sent %d messages, want only the current callback result", got)
	}
	if got := messenger.messages[0].Text; !strings.Contains(got, "current-shop") || strings.Contains(got, "stale-shop") {
		t.Fatalf("sent stale callback result: %q", got)
	}
}

func TestSlowSendDoesNotBlockAnotherChat(t *testing.T) {
	blocking := newBlockingMessenger(42)
	h := NewHandler(blocking, newMemorySessions(), fixedSearcher())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.HandleMessage(context.Background(), 42, "Dior Sauvage EDP 100 мл")
	}()
	<-blocking.started

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- h.HandleMessage(context.Background(), 84, "Dior Sauvage EDP 100 мл")
	}()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		close(blocking.release)
		<-firstDone
		t.Fatal("chat 84 blocked behind chat 42 I/O")
	}
	close(blocking.release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
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

type supersedingSearcher struct{ started chan struct{} }

func (s *supersedingSearcher) Search(ctx context.Context, query domain.SearchQuery) search.Result {
	if strings.HasPrefix(query.Raw, "Old") {
		close(s.started)
		<-ctx.Done()
		return search.Result{Offers: []domain.Offer{{Brand: "Brand", Name: "Old", Concentration: domain.ConcentrationEDP}}}
	}
	return search.Result{Offers: []domain.Offer{{Brand: "Brand", Name: "New", Concentration: domain.ConcentrationEDP}}}
}

type callbackSupersedingSearcher struct {
	mu            sync.Mutex
	calls         int
	firstStarted  chan struct{}
	secondStarted chan context.Context
	releaseSecond chan struct{}
}

func (s *callbackSupersedingSearcher) Search(ctx context.Context, _ domain.SearchQuery) search.Result {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()

	if call == 1 {
		close(s.firstStarted)
		<-ctx.Done()
		return callbackSearchResult("stale-shop")
	}
	s.secondStarted <- ctx
	<-s.releaseSecond
	return callbackSearchResult("current-shop")
}

func callbackSearchResult(shopID string) search.Result {
	return search.Result{Offers: []domain.Offer{{
		ShopID: shopID, Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
		VolumeMicroliters: 100000, Kind: domain.ProductKindRetail, PriceKopecks: 10000, InStock: true,
	}}}
}

type callbackSessions struct {
	mu                sync.Mutex
	value             storage.Session
	present           bool
	initialLoads      int
	initialLoadsReady chan struct{}
}

func newCallbackSessions(session storage.Session) *callbackSessions {
	return &callbackSessions{
		value:             session,
		present:           true,
		initialLoadsReady: make(chan struct{}),
	}
}

func (s *callbackSessions) Save(_ context.Context, session storage.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = session
	s.present = true
	return nil
}

func (s *callbackSessions) Load(ctx context.Context, _ int64) (storage.Session, bool, error) {
	s.mu.Lock()
	if s.initialLoads < 2 {
		s.initialLoads++
		value, present := s.value, s.present
		if s.initialLoads == 2 {
			close(s.initialLoadsReady)
		}
		ready := s.initialLoadsReady
		s.mu.Unlock()
		select {
		case <-ready:
			return value, present, nil
		case <-ctx.Done():
			return storage.Session{}, false, ctx.Err()
		}
	}
	defer s.mu.Unlock()
	return s.value, s.present, nil
}

func (s *callbackSessions) Delete(_ context.Context, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.present = false
	return nil
}

type blockingMessenger struct {
	blockedChat int64
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func newBlockingMessenger(chatID int64) *blockingMessenger {
	return &blockingMessenger{
		blockedChat: chatID,
		started:     make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (m *blockingMessenger) Send(ctx context.Context, chatID int64, _ Message) error {
	if chatID != m.blockedChat {
		return nil
	}
	m.once.Do(func() { close(m.started) })
	select {
	case <-m.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func fixedSearcher() Searcher {
	return fakeSearcher{result: search.Result{Offers: []domain.Offer{{
		Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
		VolumeMicroliters: 100000, Kind: domain.ProductKindRetail,
	}}}}
}

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
