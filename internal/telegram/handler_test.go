package telegram

import (
	"context"
	"errors"
	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
	"parfumes_finder/internal/search"
	"parfumes_finder/internal/storage"
	"path/filepath"
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
	h := NewHandler(messenger, sessions, searcher, nil)
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
	h := NewHandler(m, sessions, searcher, nil)
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
	}}}}, nil)
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
	h := NewHandler(messenger, sessions, fakeSearcher{}, nil)
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

func TestPersistedLegacyConcentrationCallbackRequestsExplicitRetry(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
        INSERT INTO telegram_sessions(chat_id, stage, query_json, updated_at)
        VALUES(?, ?, ?, ?)
    `, 42, "choose_concentration", `{"id":"legacy-session","query":{"Brand":"Dior","Name":"Sauvage"}}`, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}

	messenger := &fakeMessenger{}
	sessions := storage.NewSessions(db)
	h := NewHandler(messenger, sessions, fakeSearcher{}, nil)
	if err := h.HandleCallback(context.Background(), 42, "legacy-session|concentration|edp"); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := sessions.Load(context.Background(), 42); err != nil || ok {
		t.Fatalf("legacy session after callback: ok=%v err=%v", ok, err)
	}
	messages := messenger.snapshot()
	if len(messages) != 1 || !strings.Contains(messages[0].Text, "Dior Sauvage EDP") || len(messages[0].Buttons) != 0 {
		t.Fatalf("retry messages = %+v", messages)
	}
}

func TestConcentrationCallbackRejectsUnobservedAndUnrecognizedValues(t *testing.T) {
	for _, value := range []string{"edt", "bogus"} {
		t.Run(value, func(t *testing.T) {
			sessions := newMemorySessions()
			sessions.values[42] = storage.Session{
				ChatID: 42,
				ID:     "session-1",
				Stage:  "choose_concentration",
				Query: domain.SearchQuery{
					Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationUnknown,
				},
				Concentrations: []domain.Concentration{
					domain.ConcentrationEDP,
				},
			}
			h := NewHandler(&fakeMessenger{}, sessions, fakeSearcher{}, nil)

			if err := h.HandleCallback(context.Background(), 42, "session-1|concentration|"+value); err == nil {
				t.Fatal("invalid concentration callback accepted")
			}
			if got := sessions.values[42].Query.Concentration; got != domain.ConcentrationUnknown {
				t.Fatalf("concentration mutated to %q", got)
			}
		})
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
	h := NewHandler(m, sessions, searcher, nil)
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
	h := NewHandler(m, sessions, searcher, nil)
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
	handler := NewHandler(messenger, sessions, searcher, nil)
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
	handler := NewHandler(messenger, sessions, searcher, nil)
	callback := "session-1|kind|retail"

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- handler.HandleCallbackUpdate(context.Background(), 100, 77, callback)
	}()
	<-searcher.firstStarted
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- handler.HandleCallbackUpdate(context.Background(), 101, 77, callback)
	}()

	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("superseded first callback did not return")
	}
	var secondCtx context.Context
	select {
	case secondCtx = <-searcher.secondStarted:
	case err := <-secondDone:
		t.Fatalf("current callback returned before starting search: %v", err)
	case <-time.After(time.Second):
		t.Fatal("current callback did not start search")
	}
	secondCanceledByFirst := secondCtx.Err() != nil
	close(searcher.releaseSecond)
	if err := <-secondDone; err != nil {
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

func TestOlderCallbackCannotCancelNewerMessageAfterDelayedSessionLoad(t *testing.T) {
	sessions := newDelayedLoadSessions(storage.Session{
		ChatID: 42,
		ID:     "old-session",
		Stage:  "choose_kind",
		Query: domain.SearchQuery{
			Brand: "Dior", Name: "Sauvage", Concentration: domain.ConcentrationEDP,
			VolumeMicroliters: 100000,
		},
	})
	searcher := &callbackMessageOrderingSearcher{
		messageStarted:  make(chan struct{}),
		releaseMessage:  make(chan struct{}),
		messageCanceled: make(chan struct{}, 1),
		callbackCalled:  make(chan struct{}, 1),
	}
	h := NewHandler(&fakeMessenger{}, sessions, searcher, nil)

	callbackDone := make(chan error, 1)
	go func() {
		callbackDone <- h.HandleCallbackUpdate(context.Background(), 100, 42, "old-session|kind|retail")
	}()
	<-sessions.loadStarted

	messageDone := make(chan error, 1)
	go func() {
		messageDone <- h.HandleMessageUpdate(context.Background(), 101, 42, "New Query")
	}()
	<-searcher.messageStarted
	close(sessions.releaseLoad)

	select {
	case err := <-callbackDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(searcher.releaseMessage)
		<-messageDone
		t.Fatal("superseded callback did not return after its session load completed")
	}
	select {
	case <-searcher.messageCanceled:
		close(searcher.releaseMessage)
		<-messageDone
		t.Fatal("older callback canceled the newer message search")
	default:
	}
	select {
	case <-searcher.callbackCalled:
		close(searcher.releaseMessage)
		<-messageDone
		t.Fatal("superseded callback started its search")
	default:
	}

	close(searcher.releaseMessage)
	if err := <-messageDone; err != nil {
		t.Fatal(err)
	}
	got, ok, err := sessions.Load(context.Background(), 42)
	if err != nil || !ok || got.Query.Name != "New" {
		t.Fatalf("current session = %+v, ok=%v, err=%v", got, ok, err)
	}
}

func TestTelegramUpdateIDResetAfterWeek_LowerIDAtSevenDaysMinusNanosecondIsRejectedWithoutCancel(t *testing.T) {
	clock := newFakeClock(time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC))
	searcher := newUpdateEpochProbeSearcher()
	t.Cleanup(searcher.release)
	h := NewHandler(&fakeMessenger{}, newMemorySessions(), searcher, nil)
	h.now = clock.Now

	currentDone := make(chan error, 1)
	go func() {
		currentDone <- h.HandleMessageUpdate(context.Background(), 100, 42, "current epoch")
	}()
	current := searcher.nextCall(t)

	clock.Advance(7*24*time.Hour - time.Nanosecond)
	staleDone := make(chan error, 1)
	go func() {
		staleDone <- h.HandleMessageUpdate(context.Background(), 50, 42, "lower before reset boundary")
	}()

	searcher.assertRejected(t, staleDone)
	if err := current.ctx.Err(); err != nil {
		t.Fatalf("stale lower update canceled the current operation: %v", err)
	}
	searcher.release()
	if err := <-currentDone; err != nil {
		t.Fatal(err)
	}
}

func TestTelegramUpdateIDResetAfterWeek_EqualIDAtSevenDaysMinusNanosecondIsRejectedWithoutCancel(t *testing.T) {
	clock := newFakeClock(time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC))
	searcher := newUpdateEpochProbeSearcher()
	t.Cleanup(searcher.release)
	h := NewHandler(&fakeMessenger{}, newMemorySessions(), searcher, nil)
	h.now = clock.Now

	currentDone := make(chan error, 1)
	go func() {
		currentDone <- h.HandleMessageUpdate(context.Background(), 100, 42, "current epoch")
	}()
	current := searcher.nextCall(t)

	clock.Advance(7*24*time.Hour - time.Nanosecond)
	staleDone := make(chan error, 1)
	go func() {
		staleDone <- h.HandleMessageUpdate(context.Background(), 100, 42, "duplicate before reset boundary")
	}()

	searcher.assertRejected(t, staleDone)
	if err := current.ctx.Err(); err != nil {
		t.Fatalf("equal update before reset boundary canceled the current operation: %v", err)
	}
	searcher.release()
	if err := <-currentDone; err != nil {
		t.Fatal(err)
	}
}

func TestTelegramUpdateIDResetAfterWeek_StaleRejectDoesNotRefreshIdleWindow(t *testing.T) {
	clock := newFakeClock(time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC))
	searcher := newUpdateEpochProbeSearcher()
	t.Cleanup(searcher.release)
	h := NewHandler(&fakeMessenger{}, newMemorySessions(), searcher, nil)
	h.now = clock.Now

	previousDone := make(chan error, 1)
	go func() {
		previousDone <- h.HandleMessageUpdate(context.Background(), 100, 42, "previous epoch")
	}()
	previous := searcher.nextCall(t)

	clock.Advance(7*24*time.Hour - time.Nanosecond)
	staleDone := make(chan error, 1)
	go func() {
		staleDone <- h.HandleMessageUpdate(context.Background(), 50, 42, "stale before reset boundary")
	}()
	searcher.assertRejected(t, staleDone)
	if err := previous.ctx.Err(); err != nil {
		t.Fatalf("stale update canceled the previous epoch operation: %v", err)
	}

	clock.Advance(time.Nanosecond)
	currentDone := make(chan error, 1)
	go func() {
		currentDone <- h.HandleMessageUpdate(context.Background(), 50, 42, "new epoch at original boundary")
	}()
	current := searcher.nextCall(t)
	if current.raw != "new epoch at original boundary" {
		t.Fatalf("admitted search = %q, want new epoch at original boundary", current.raw)
	}
	select {
	case <-previous.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("new epoch at the original boundary did not cancel the previous operation")
	}

	searcher.release()
	if err := <-previousDone; err != nil {
		t.Fatal(err)
	}
	if err := <-currentDone; err != nil {
		t.Fatal(err)
	}
}

func TestTelegramUpdateIDResetAfterWeek_LowerIDAtExactSevenDaysIsAdmittedAndCancelsPrevious(t *testing.T) {
	clock := newFakeClock(time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC))
	searcher := newUpdateEpochProbeSearcher()
	t.Cleanup(searcher.release)
	h := NewHandler(&fakeMessenger{}, newMemorySessions(), searcher, nil)
	h.now = clock.Now

	previousDone := make(chan error, 1)
	go func() {
		previousDone <- h.HandleMessageUpdate(context.Background(), 100, 42, "previous epoch")
	}()
	previous := searcher.nextCall(t)

	clock.Advance(7 * 24 * time.Hour)
	currentDone := make(chan error, 1)
	go func() {
		currentDone <- h.HandleMessageUpdate(context.Background(), 50, 42, "new epoch")
	}()
	current := searcher.nextCall(t)
	if current.raw != "new epoch" {
		t.Fatalf("admitted search = %q, want new epoch", current.raw)
	}
	select {
	case <-previous.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("lower update at the reset boundary did not cancel the previous epoch operation")
	}

	searcher.release()
	if err := <-previousDone; err != nil {
		t.Fatal(err)
	}
	if err := <-currentDone; err != nil {
		t.Fatal(err)
	}
}

func TestTelegramUpdateIDResetAfterWeek_HigherIDRefreshesIdleWindow(t *testing.T) {
	clock := newFakeClock(time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC))
	searcher := newUpdateEpochProbeSearcher()
	t.Cleanup(searcher.release)
	h := NewHandler(&fakeMessenger{}, newMemorySessions(), searcher, nil)
	h.now = clock.Now

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.HandleMessageUpdate(context.Background(), 100, 42, "first high-water mark")
	}()
	first := searcher.nextCall(t)

	clock.Advance(6 * 24 * time.Hour)
	currentDone := make(chan error, 1)
	go func() {
		currentDone <- h.HandleMessageUpdate(context.Background(), 101, 42, "refreshed high-water mark")
	}()
	current := searcher.nextCall(t)
	select {
	case <-first.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("higher update did not supersede the previous operation")
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}

	clock.Advance(24 * time.Hour)
	staleDone := make(chan error, 1)
	go func() {
		staleDone <- h.HandleMessageUpdate(context.Background(), 50, 42, "lower one day after refresh")
	}()
	searcher.assertRejected(t, staleDone)
	if err := current.ctx.Err(); err != nil {
		t.Fatalf("higher update did not refresh the idle window; current operation canceled: %v", err)
	}

	searcher.release()
	if err := <-currentDone; err != nil {
		t.Fatal(err)
	}
}

func TestSlowSendDoesNotBlockAnotherChat(t *testing.T) {
	blocking := newBlockingMessenger(42)
	h := NewHandler(blocking, newMemorySessions(), fixedSearcher(), nil)
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

func TestPriceResultIsSentBeforeFragranticaCard(t *testing.T) {
	messenger := &fakeMessenger{}
	enricher := &fakeEnricher{before: func() {
		messages := messenger.snapshot()
		if len(messages) == 0 || !strings.Contains(messages[len(messages)-1].Text, "Без доставки") {
			t.Fatalf("price was not sent before enrichment: %+v", messages)
		}
	}, card: enrichment.Card{
		SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Title:     "Sauvage Eau de Parfum Dior",
		PNG:       []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}, ok: true}
	h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), enricher)
	if err := runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл"); err != nil {
		t.Fatal(err)
	}

	messages := messenger.snapshot()
	if len(messages) < 2 {
		t.Fatalf("messages = %+v", messages)
	}
	price, photo := messages[len(messages)-2], messages[len(messages)-1]
	if !strings.Contains(price.Text, "Без доставки") || len(price.PhotoPNG) != 0 {
		t.Fatalf("first final message = %+v", price)
	}
	if !reflect.DeepEqual(photo.PhotoPNG, enricher.card.PNG) {
		t.Fatalf("photo PNG = %x, want %x", photo.PhotoPNG, enricher.card.PNG)
	}
	if want := enricher.card.Title + "\n" + enricher.card.SourceURL; photo.Caption != want {
		t.Fatalf("caption = %q, want %q", photo.Caption, want)
	}
	if want := (enrichment.Request{Brand: "Dior", Name: "Sauvage", Edition: "2015", Concentration: domain.ConcentrationEDP}); enricher.request != want {
		t.Fatalf("enrichment request = %+v, want %+v", enricher.request, want)
	}
}

func TestEnrichmentFailureDoesNotReplacePriceResult(t *testing.T) {
	messenger := &fakeMessenger{}
	h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), &fakeEnricher{err: errors.New("challenge")})
	if err := runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл"); err != nil {
		t.Fatal(err)
	}
	messages := messenger.snapshot()
	if last := messages[len(messages)-1]; !strings.Contains(last.Text, "Без доставки") || len(last.PhotoPNG) != 0 {
		t.Fatalf("last message = %+v", last)
	}
}

func TestPhotoFailureDoesNotReplacePriceResult(t *testing.T) {
	messenger := &fakeMessenger{photoErr: errors.New("telegram unavailable")}
	h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), &fakeEnricher{card: enrichment.Card{
		SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		Title:     "Sauvage Eau de Parfum Dior",
		PNG:       []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}, ok: true})
	if err := runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл"); err != nil {
		t.Fatal(err)
	}
	messages := messenger.snapshot()
	if last := messages[len(messages)-1]; !strings.Contains(last.Text, "Без доставки") || len(last.PhotoPNG) != 0 {
		t.Fatalf("last message = %+v", last)
	}
}

func TestSupersededEnrichmentCannotSendPhoto(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	enricher := &blockingEnricher{started: started, release: release, card: enrichment.Card{
		Title:     "Sauvage Eau de Parfum Dior",
		SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
		PNG:       []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}}
	messenger := &fakeMessenger{}
	h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), enricher)

	firstDone := make(chan error, 1)
	go func() { firstDone <- runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл") }()
	<-started
	if err := h.HandleMessage(context.Background(), 42, "Tom Ford Ombre Leather EDP 100 мл"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	for _, message := range messenger.snapshot() {
		if len(message.PhotoPNG) > 0 && strings.Contains(message.Caption, "Sauvage") {
			t.Fatalf("stale photo sent: %+v", message)
		}
	}
}

func TestRateLimitedMessageCancelsPendingSameChatEnrichment(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	enricher := &cancelAwareBlockingEnricher{
		started: started, canceled: canceled, release: release,
		card: enrichment.Card{
			Title:     "Sauvage Eau de Parfum Dior",
			SourceURL: "https://www.fragrantica.ru/perfume/Dior/Sauvage-Eau-de-Parfum-48100.html",
			PNG:       []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
		},
	}
	messenger := &fakeMessenger{}
	h := NewHandler(messenger, newMemorySessions(), completeVariantSearcher(), enricher)
	for range 2 {
		if err := h.HandleMessage(context.Background(), 42, "Dior Sauvage EDP 100 мл"); err != nil {
			t.Fatal(err)
		}
	}

	thirdDone := make(chan error, 1)
	go func() { thirdDone <- runCompleteQuery(h, 42, "Dior Sauvage EDP 100 мл") }()
	<-started
	if err := h.HandleMessage(context.Background(), 42, "Tom Ford Ombre Leather EDP 100 мл"); err != nil {
		t.Fatal(err)
	}
	messagesAfterRejection := messenger.snapshot()
	rejected := len(messagesAfterRejection) > 0 && strings.Contains(messagesAfterRejection[len(messagesAfterRejection)-1].Text, "Слишком много запросов")

	contextCanceled := false
	select {
	case <-canceled:
		contextCanceled = true
	case <-time.After(time.Second):
	}
	close(release)
	if err := <-thirdDone; err != nil {
		t.Fatal(err)
	}

	if !rejected {
		t.Errorf("fourth message was not rate-limited: %+v", messagesAfterRejection)
	}
	if !contextCanceled {
		t.Error("rate-limited message did not cancel pending enrichment context")
	}
	for _, message := range messenger.snapshot() {
		if len(message.PhotoPNG) > 0 {
			t.Errorf("stale photo sent after rate-limit rejection: %+v", message)
		}
	}
}

type fakeEnricher struct {
	before  func()
	card    enrichment.Card
	ok      bool
	err     error
	request enrichment.Request
}

func (service *fakeEnricher) Enrich(_ context.Context, request enrichment.Request) (enrichment.Card, bool, error) {
	if service.before != nil {
		service.before()
	}
	service.request = request
	return service.card, service.ok, service.err
}

type blockingEnricher struct {
	started chan struct{}
	release chan struct{}
	card    enrichment.Card
	once    sync.Once
}

func (service *blockingEnricher) Enrich(context.Context, enrichment.Request) (enrichment.Card, bool, error) {
	service.once.Do(func() { close(service.started) })
	<-service.release
	return service.card, true, nil
}

type cancelAwareBlockingEnricher struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	card     enrichment.Card
}

func (service *cancelAwareBlockingEnricher) Enrich(ctx context.Context, _ enrichment.Request) (enrichment.Card, bool, error) {
	close(service.started)
	select {
	case <-ctx.Done():
		close(service.canceled)
		<-service.release
	case <-service.release:
	}
	return service.card, true, nil
}

func completeVariantSearcher() Searcher {
	return fakeSearcher{result: search.Result{Offers: []domain.Offer{{
		ShopID: "orental", Brand: "Dior", Name: "Sauvage", Edition: "2015",
		Concentration: domain.ConcentrationEDP, VolumeMicroliters: 100000,
		Kind: domain.ProductKindRetail, InStock: true, PriceKopecks: 1000000,
	}}}}
}

func runCompleteQuery(h *Handler, chatID int64, query string) error {
	if err := h.HandleMessage(context.Background(), chatID, query); err != nil {
		return err
	}
	session, ok, err := h.sessions.Load(context.Background(), chatID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("session not found")
	}
	return h.HandleCallback(context.Background(), chatID, callbackData(session.ID, "kind", "retail"))
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(elapsed time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(elapsed)
}

type updateEpochSearchCall struct {
	raw string
	ctx context.Context
}

type updateEpochProbeSearcher struct {
	calls       chan updateEpochSearchCall
	releaseCh   chan struct{}
	releaseOnce sync.Once
}

func newUpdateEpochProbeSearcher() *updateEpochProbeSearcher {
	return &updateEpochProbeSearcher{
		calls:     make(chan updateEpochSearchCall, 3),
		releaseCh: make(chan struct{}),
	}
}

func (searcher *updateEpochProbeSearcher) Search(ctx context.Context, query domain.SearchQuery) search.Result {
	searcher.calls <- updateEpochSearchCall{raw: query.Raw, ctx: ctx}
	select {
	case <-ctx.Done():
	case <-searcher.releaseCh:
	}
	return search.Result{}
}

func (searcher *updateEpochProbeSearcher) nextCall(t *testing.T) updateEpochSearchCall {
	t.Helper()
	select {
	case call := <-searcher.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("admitted update did not start a search")
		return updateEpochSearchCall{}
	}
}

func (searcher *updateEpochProbeSearcher) assertRejected(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case call := <-searcher.calls:
		t.Fatalf("stale update unexpectedly started search %q", call.raw)
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale update did not return")
	}
}

func (searcher *updateEpochProbeSearcher) release() {
	searcher.releaseOnce.Do(func() { close(searcher.releaseCh) })
}

type fakeMessenger struct {
	mu       sync.Mutex
	messages []Message
	photoErr error
}

func (m *fakeMessenger) Send(_ context.Context, _ int64, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(msg.PhotoPNG) > 0 && m.photoErr != nil {
		return m.photoErr
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *fakeMessenger) snapshot() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.messages...)
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
	mu      sync.Mutex
	value   storage.Session
	present bool
}

type delayedLoadSessions struct {
	mu          sync.Mutex
	value       storage.Session
	present     bool
	firstLoad   bool
	loadStarted chan struct{}
	releaseLoad chan struct{}
}

func newDelayedLoadSessions(session storage.Session) *delayedLoadSessions {
	return &delayedLoadSessions{
		value:       session,
		present:     true,
		loadStarted: make(chan struct{}),
		releaseLoad: make(chan struct{}),
	}
}

func (sessions *delayedLoadSessions) Save(_ context.Context, session storage.Session) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.value = session
	sessions.present = true
	return nil
}

func (sessions *delayedLoadSessions) Load(_ context.Context, _ int64) (storage.Session, bool, error) {
	sessions.mu.Lock()
	value, present := sessions.value, sessions.present
	if !sessions.firstLoad {
		sessions.firstLoad = true
		close(sessions.loadStarted)
		release := sessions.releaseLoad
		sessions.mu.Unlock()
		<-release
		return value, present, nil
	}
	sessions.mu.Unlock()
	return value, present, nil
}

func (sessions *delayedLoadSessions) Delete(_ context.Context, _ int64) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.present = false
	return nil
}

type callbackMessageOrderingSearcher struct {
	messageStarted  chan struct{}
	releaseMessage  chan struct{}
	messageCanceled chan struct{}
	callbackCalled  chan struct{}
}

func (searcher *callbackMessageOrderingSearcher) Search(ctx context.Context, query domain.SearchQuery) search.Result {
	if query.Raw == "New Query" {
		close(searcher.messageStarted)
		select {
		case <-searcher.releaseMessage:
			return search.Result{Offers: []domain.Offer{{
				Brand: "Brand", Name: "New", Concentration: domain.ConcentrationEDP,
			}}}
		case <-ctx.Done():
			searcher.messageCanceled <- struct{}{}
			return search.Result{}
		}
	}
	searcher.callbackCalled <- struct{}{}
	return callbackSearchResult("stale-shop")
}

func newCallbackSessions(session storage.Session) *callbackSessions {
	return &callbackSessions{value: session, present: true}
}

func (s *callbackSessions) Save(_ context.Context, session storage.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = session
	s.present = true
	return nil
}

func (s *callbackSessions) Load(_ context.Context, _ int64) (storage.Session, bool, error) {
	s.mu.Lock()
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
