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
		candidates := candidatesFromOffers(result.Offers, explicit)
		if len(candidates) == 0 {
			return h.messenger.Send(ctx, chatID, Message{Text: "Ничего не найдено. Уточни бренд и название аромата."})
		}
		session := storage.Session{ChatID: chatID, ID: sessionID, Query: explicit, Candidates: candidates}
		if len(candidates) > 1 {
			session.Stage = "choose_fragrance"
			if err := h.sessions.Save(ctx, session); err != nil {
				return err
			}
			buttons := make([]Button, len(candidates))
			for i, candidate := range candidates {
				buttons[i] = Button{candidateLabel(candidate.Query), callbackData(sessionID, "fragrance", strconv.Itoa(i))}
			}
			return h.messenger.Send(ctx, chatID, Message{Text: "Выбери аромат", Buttons: buttons})
		}
		session.Query = mergeCandidate(explicit, candidates[0].Query)
		session.Concentrations = candidates[0].Concentrations
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
		if err != nil || index < 0 || index >= len(session.Candidates) {
			return fmt.Errorf("invalid fragrance")
		}
		selected := session.Candidates[index]
		session.Query = mergeCandidate(session.Query, selected.Query)
		session.Concentrations = selected.Concentrations
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
		switch len(session.Concentrations) {
		case 0:
			_ = h.sessions.Delete(ctx, session.ChatID)
			return h.messenger.Send(ctx, session.ChatID, Message{
				Text: "Не удалось определить концентрацию. Повтори запрос целиком, например: Dior Sauvage EDP.",
			})
		case 1:
			session.Query.Concentration = session.Concentrations[0]
			return h.advance(ctx, session)
		default:
			session.Stage = "choose_concentration"
			message = concentrationMessage(session.ID, session.Concentrations)
		}
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

func candidatesFromOffers(offers []domain.Offer, explicit domain.SearchQuery) []domain.FragranceCandidate {
	type aggregate struct {
		query          domain.SearchQuery
		concentrations map[domain.Concentration]struct{}
	}
	byKey := map[string]aggregate{}
	for _, offer := range offers {
		if offer.Brand == "" || offer.Name == "" {
			continue
		}
		key := domain.NormalizeText(offer.Brand) + "|" + domain.NormalizeText(offer.Name) + "|" + domain.NormalizeText(offer.Edition)
		candidate, ok := byKey[key]
		if !ok {
			candidate.query = domain.SearchQuery{Brand: offer.Brand, Name: offer.Name, Edition: offer.Edition, Concentration: explicit.Concentration, VolumeMicroliters: explicit.VolumeMicroliters, Kind: explicit.Kind}
			candidate.concentrations = map[domain.Concentration]struct{}{}
		}
		if isRecognizedConcentration(offer.Concentration) {
			candidate.concentrations[offer.Concentration] = struct{}{}
		}
		byKey[key] = candidate
	}
	result := make([]domain.FragranceCandidate, 0, len(byKey))
	for _, candidate := range byKey {
		concentrations := make([]domain.Concentration, 0, len(candidate.concentrations))
		for concentration := range candidate.concentrations {
			concentrations = append(concentrations, concentration)
		}
		result = append(result, domain.FragranceCandidate{
			Query:          candidate.query,
			Concentrations: domain.SortConcentrations(concentrations),
		})
	}
	sort.Slice(result, func(i, j int) bool { return candidateLabel(result[i].Query) < candidateLabel(result[j].Query) })
	return result
}

func isRecognizedConcentration(value domain.Concentration) bool {
	switch value {
	case domain.ConcentrationEDT, domain.ConcentrationEDP, domain.ConcentrationParfum,
		domain.ConcentrationExtrait, domain.ConcentrationCologne, domain.ConcentrationElixir:
		return true
	default:
		return false
	}
}

func concentrationMessage(sessionID string, concentrations []domain.Concentration) Message {
	labels := map[domain.Concentration]string{
		domain.ConcentrationEDT:     "EDT",
		domain.ConcentrationEDP:     "EDP",
		domain.ConcentrationParfum:  "Parfum",
		domain.ConcentrationExtrait: "Extrait",
		domain.ConcentrationCologne: "Cologne",
		domain.ConcentrationElixir:  "Elixir",
	}
	values := domain.SortConcentrations(concentrations)
	buttons := make([]Button, 0, len(values))
	for _, concentration := range values {
		if label, ok := labels[concentration]; ok {
			buttons = append(buttons, Button{label, callbackData(sessionID, "concentration", string(concentration))})
		}
	}
	return Message{Text: "Выбери концентрацию", Buttons: buttons}
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
