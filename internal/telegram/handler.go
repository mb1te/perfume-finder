package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/search"
	"parfumes_finder/internal/storage"
)

type Button struct{ Text, Data string }
type Message struct {
	Text    string
	Buttons []Button
}
type Messenger interface {
	Send(context.Context, int64, Message) error
}
type Sessions interface {
	Save(context.Context, storage.Session) error
	Load(context.Context, int64) (storage.Session, bool, error)
	Delete(context.Context, int64) error
}
type Searcher interface {
	Search(context.Context, domain.SearchQuery) search.Result
}
type activeSearch struct {
	id     string
	cancel context.CancelFunc
}
type Handler struct {
	messenger Messenger
	sessions  Sessions
	searcher  Searcher
	limiters  *userLimiters
	activeMu  sync.Mutex
	active    map[int64]activeSearch
}

func NewHandler(m Messenger, sessions Sessions, searcher Searcher) *Handler {
	return &Handler{messenger: m, sessions: sessions, searcher: searcher, limiters: newUserLimiters(), active: map[int64]activeSearch{}}
}

func (h *Handler) HandleMessage(ctx context.Context, chatID int64, text string) error {
	if !h.limiters.Allow(chatID) {
		return h.messenger.Send(ctx, chatID, Message{Text: "Слишком много запросов. Подожди несколько секунд."})
	}
	sessionID := newSessionID()
	searchCtx := h.beginSearch(ctx, chatID, sessionID)
	result := h.searcher.Search(searchCtx, domain.SearchQuery{Raw: text})
	_, err := h.completeOwned(chatID, sessionID, func() error {
		explicit := domain.SearchQuery{Raw: text, Concentration: domain.ParseConcentration(text), VolumeMicroliters: domain.ParseVolumeMicroliters(text), Kind: domain.ClassifyKind(text)}
		options := candidatesFromOffers(result.Offers, explicit)
		if len(options) == 0 {
			return h.messenger.Send(ctx, chatID, Message{Text: "Ничего не найдено. Уточни бренд и название аромата."})
		}
		session := storage.Session{ChatID: chatID, ID: sessionID, Query: explicit, Options: options}
		if len(options) > 1 {
			session.Stage = "choose_fragrance"
			if err := h.sessions.Save(ctx, session); err != nil {
				return err
			}
			buttons := make([]Button, len(options))
			for i, option := range options {
				buttons[i] = Button{candidateLabel(option), callbackData(sessionID, "fragrance", strconv.Itoa(i))}
			}
			return h.messenger.Send(ctx, chatID, Message{Text: "Выбери аромат", Buttons: buttons})
		}
		session.Query = mergeCandidate(explicit, options[0])
		return h.advance(ctx, session)
	})
	return err
}

func (h *Handler) HandleCallback(ctx context.Context, chatID int64, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback")
	}
	session, ok, err := h.sessions.Load(ctx, chatID)
	if err != nil {
		return err
	}
	if !ok || session.ID == "" || parts[0] != session.ID {
		return fmt.Errorf("stale callback")
	}
	action, value := parts[1], parts[2]
	switch action {
	case "fragrance":
		if session.Stage != "choose_fragrance" {
			return fmt.Errorf("invalid stage")
		}
		index, err := strconv.Atoi(value)
		if err != nil || index < 0 || index >= len(session.Options) {
			return fmt.Errorf("invalid fragrance")
		}
		session.Query = mergeCandidate(session.Query, session.Options[index])
		return h.advance(ctx, session)
	case "concentration":
		if session.Stage != "choose_concentration" {
			return fmt.Errorf("invalid stage")
		}
		session.Query.Concentration = domain.Concentration(value)
		return h.advance(ctx, session)
	case "volume":
		if session.Stage != "choose_volume" {
			return fmt.Errorf("invalid stage")
		}
		volume, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		session.Query.VolumeMicroliters = volume
		return h.advance(ctx, session)
	case "kind":
		if session.Stage != "choose_kind" {
			return fmt.Errorf("invalid stage")
		}
		session.Query.Kind = domain.ProductKind(value)
		session.Stage = "searching"
		if err := h.sessions.Save(ctx, session); err != nil {
			return err
		}
		searchCtx := h.beginSearch(ctx, chatID, session.ID)
		result := h.searcher.Search(searchCtx, session.Query)
		_, err := h.completeOwned(chatID, session.ID, func() error {
			current, ok, err := h.sessions.Load(ctx, chatID)
			if err != nil {
				return err
			}
			if !ok || current.ID != session.ID {
				return fmt.Errorf("stale search result")
			}
			if err := h.sessions.Delete(ctx, chatID); err != nil {
				return err
			}
			return h.messenger.Send(ctx, chatID, Message{Text: RenderResult(session.Query, result)})
		})
		return err
	default:
		return fmt.Errorf("unknown callback")
	}
}

func (h *Handler) advance(ctx context.Context, session storage.Session) error {
	var message Message
	switch {
	case session.Query.Concentration == domain.ConcentrationUnknown:
		session.Stage = "choose_concentration"
		message = Message{Text: "Выбери концентрацию", Buttons: []Button{{"EDT", callbackData(session.ID, "concentration", "edt")}, {"EDP", callbackData(session.ID, "concentration", "edp")}, {"Parfum", callbackData(session.ID, "concentration", "parfum")}, {"Elixir", callbackData(session.ID, "concentration", "elixir")}}}
	case session.Query.VolumeMicroliters == 0:
		session.Stage = "choose_volume"
		message = Message{Text: "Выбери объём", Buttons: []Button{{"30 мл", callbackData(session.ID, "volume", "30000")}, {"50 мл", callbackData(session.ID, "volume", "50000")}, {"60 мл", callbackData(session.ID, "volume", "60000")}, {"100 мл", callbackData(session.ID, "volume", "100000")}, {"200 мл", callbackData(session.ID, "volume", "200000")}}}
	default:
		session.Stage = "choose_kind"
		message = Message{Text: "Выбери вид", Buttons: []Button{{"Флакон", callbackData(session.ID, "kind", "retail")}, {"Тестер", callbackData(session.ID, "kind", "tester")}, {"Отливант", callbackData(session.ID, "kind", "decant")}, {"Миниатюра", callbackData(session.ID, "kind", "miniature")}, {"Пробник", callbackData(session.ID, "kind", "sample")}, {"Все виды", callbackData(session.ID, "kind", "all")}}}
	}
	if err := h.sessions.Save(ctx, session); err != nil {
		return err
	}
	return h.messenger.Send(ctx, session.ChatID, message)
}

func candidatesFromOffers(offers []domain.Offer, explicit domain.SearchQuery) []domain.SearchQuery {
	byKey := map[string]domain.SearchQuery{}
	for _, offer := range offers {
		if offer.Brand == "" || offer.Name == "" {
			continue
		}
		key := domain.NormalizeText(offer.Brand) + "|" + domain.NormalizeText(offer.Name) + "|" + domain.NormalizeText(offer.Edition)
		candidate := domain.SearchQuery{Brand: offer.Brand, Name: offer.Name, Edition: offer.Edition, Concentration: explicit.Concentration, VolumeMicroliters: explicit.VolumeMicroliters, Kind: explicit.Kind}
		byKey[key] = candidate
	}
	result := make([]domain.SearchQuery, 0, len(byKey))
	for _, candidate := range byKey {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return candidateLabel(result[i]) < candidateLabel(result[j]) })
	return result
}
func mergeCandidate(explicit, candidate domain.SearchQuery) domain.SearchQuery {
	candidate.Raw = explicit.Raw
	if explicit.Concentration != domain.ConcentrationUnknown {
		candidate.Concentration = explicit.Concentration
	}
	if explicit.VolumeMicroliters != 0 {
		candidate.VolumeMicroliters = explicit.VolumeMicroliters
	}
	if explicit.Kind != domain.ProductKindUnknown {
		candidate.Kind = explicit.Kind
	}
	return candidate
}
func candidateLabel(q domain.SearchQuery) string {
	return strings.TrimSpace(q.Brand + " " + q.Name + " " + q.Edition)
}
func callbackData(id, action, value string) string { return id + "|" + action + "|" + value }
func newSessionID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}
func (h *Handler) beginSearch(parent context.Context, chatID int64, id string) context.Context {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	if previous, ok := h.active[chatID]; ok {
		previous.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	h.active[chatID] = activeSearch{id, cancel}
	return ctx
}
func (h *Handler) completeOwned(chatID int64, id string, action func() error) (bool, error) {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	current, ok := h.active[chatID]
	if !ok || current.id != id {
		return false, nil
	}
	err := action()
	delete(h.active, chatID)
	current.cancel()
	return true, err
}
