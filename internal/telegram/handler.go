package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"parfumes_finder/internal/domain"
	"parfumes_finder/internal/enrichment"
	"parfumes_finder/internal/search"
	"parfumes_finder/internal/storage"
)

type Button struct{ Text, Data string }
type Message struct {
	Text     string
	Buttons  []Button
	PhotoPNG []byte
	Caption  string
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
	token  string
	cancel context.CancelFunc
}
type chatOperations struct {
	mu             sync.Mutex
	admitted       bool
	latestUpdateID int64
	active         activeSearch
}
type Handler struct {
	messenger         Messenger
	sessions          Sessions
	searcher          Searcher
	enricher          enrichment.Service
	limiters          *userLimiters
	operations        sync.Map
	syntheticUpdateID atomic.Int64
}

func NewHandler(m Messenger, sessions Sessions, searcher Searcher, enricher enrichment.Service) *Handler {
	return &Handler{messenger: m, sessions: sessions, searcher: searcher, enricher: enricher, limiters: newUserLimiters()}
}

func (h *Handler) HandleMessage(ctx context.Context, chatID int64, text string) error {
	return h.HandleMessageUpdate(ctx, h.syntheticUpdateID.Add(1), chatID, text)
}

func (h *Handler) HandleMessageUpdate(ctx context.Context, updateID, chatID int64, text string) error {
	operationCtx, operationToken, admitted := h.beginUpdate(ctx, chatID, updateID)
	if !admitted {
		return nil
	}
	defer h.finishOwned(chatID, operationToken)
	if !h.limiters.Allow(chatID) {
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		return h.messenger.Send(operationCtx, chatID, Message{Text: "Слишком много запросов. Подожди несколько секунд."})
	}
	sessionID := newSessionID()
	result := h.searcher.Search(operationCtx, domain.SearchQuery{Raw: text})
	if !h.isOwned(chatID, operationToken) {
		return nil
	}
	explicit := domain.SearchQuery{Raw: text, Concentration: domain.ParseConcentration(text), VolumeMicroliters: domain.ParseVolumeMicroliters(text), Kind: domain.ClassifyKind(text)}
	candidates := candidatesFromOffers(result.Offers, explicit)
	if len(candidates) == 0 {
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		return h.messenger.Send(operationCtx, chatID, Message{Text: "Ничего не найдено. Уточни бренд и название аромата."})
	}
	session := storage.Session{ChatID: chatID, ID: sessionID, Query: explicit, Candidates: candidates}
	if len(candidates) > 1 {
		session.Stage = "choose_fragrance"
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		if err := h.sessions.Save(operationCtx, session); err != nil {
			if !h.isOwned(chatID, operationToken) {
				return nil
			}
			return err
		}
		buttons := make([]Button, len(candidates))
		for i, candidate := range candidates {
			buttons[i] = Button{candidateLabel(candidate.Query), callbackData(sessionID, "fragrance", strconv.Itoa(i))}
		}
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		return h.messenger.Send(operationCtx, chatID, Message{Text: "Выбери аромат", Buttons: buttons})
	}
	session.Query = mergeCandidate(explicit, candidates[0].Query)
	session.Concentrations = candidates[0].Concentrations
	return h.advanceOwned(operationCtx, session, operationToken)
}

func (h *Handler) HandleCallback(ctx context.Context, chatID int64, data string) error {
	return h.HandleCallbackUpdate(ctx, h.syntheticUpdateID.Add(1), chatID, data)
}

func (h *Handler) HandleCallbackUpdate(ctx context.Context, updateID, chatID int64, data string) error {
	parts := strings.Split(data, "|")
	if len(parts) != 3 {
		return fmt.Errorf("invalid callback")
	}
	operationCtx, operationToken, admitted := h.beginUpdate(ctx, chatID, updateID)
	if !admitted {
		return nil
	}
	defer h.finishOwned(chatID, operationToken)

	session, ok, err := h.sessions.Load(operationCtx, chatID)
	if !h.isOwned(chatID, operationToken) {
		return nil
	}
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
		return h.advanceOwned(operationCtx, session, operationToken)
	case "concentration":
		if session.Stage != "choose_concentration" {
			return fmt.Errorf("invalid stage")
		}
		if len(session.Concentrations) == 0 {
			session.Query.Concentration = domain.ConcentrationUnknown
			return h.advanceOwned(operationCtx, session, operationToken)
		}
		concentration := domain.Concentration(value)
		if !isRecognizedConcentration(concentration) || !containsConcentration(session.Concentrations, concentration) {
			return fmt.Errorf("invalid concentration")
		}
		session.Query.Concentration = concentration
		return h.advanceOwned(operationCtx, session, operationToken)
	case "volume":
		if session.Stage != "choose_volume" {
			return fmt.Errorf("invalid stage")
		}
		volume, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		session.Query.VolumeMicroliters = volume
		return h.advanceOwned(operationCtx, session, operationToken)
	case "kind":
		if session.Stage != "choose_kind" && session.Stage != "searching" {
			return fmt.Errorf("invalid stage")
		}
		session.Query.Kind = domain.ProductKind(value)
		session.Stage = "searching"
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		if err := h.sessions.Save(operationCtx, session); err != nil {
			if !h.isOwned(chatID, operationToken) {
				return nil
			}
			return err
		}
		result := h.searcher.Search(operationCtx, session.Query)
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		current, ok, err := h.sessions.Load(operationCtx, chatID)
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		if err != nil {
			return err
		}
		if !ok || current.ID != session.ID {
			return fmt.Errorf("stale search result")
		}
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		if err := h.sessions.Delete(operationCtx, chatID); err != nil {
			if !h.isOwned(chatID, operationToken) {
				return nil
			}
			return err
		}
		if !h.isOwned(chatID, operationToken) {
			return nil
		}
		if err := h.messenger.Send(operationCtx, chatID, Message{Text: RenderResult(session.Query, result)}); err != nil {
			return err
		}
		if h.enricher == nil || operationCtx.Err() != nil || !h.isOwned(chatID, operationToken) {
			return nil
		}
		return h.sendEnrichment(operationCtx, chatID, session.Query, operationToken)
	default:
		return fmt.Errorf("unknown callback")
	}
}

func (h *Handler) sendEnrichment(ctx context.Context, chatID int64, query domain.SearchQuery, operationToken string) error {
	started := time.Now()
	card, ok, err := h.enricher.Enrich(ctx, enrichment.Request{
		Brand: query.Brand, Name: query.Name, Edition: query.Edition, Concentration: query.Concentration,
	})
	if err != nil {
		logEnrichment(chatID, "error", started)
		return nil
	}
	if !ok {
		logEnrichment(chatID, "not_found", started)
		return nil
	}
	if ctx.Err() != nil || !h.isOwned(chatID, operationToken) {
		logEnrichment(chatID, "superseded", started)
		return nil
	}
	if len(card.PNG) == 0 || strings.TrimSpace(card.SourceURL) == "" {
		logEnrichment(chatID, "invalid_card", started)
		return nil
	}
	caption := strings.TrimSpace(card.Title)
	if caption != "" {
		caption += "\n"
	}
	caption += card.SourceURL
	if err := h.messenger.Send(ctx, chatID, Message{PhotoPNG: card.PNG, Caption: caption}); err != nil {
		logEnrichment(chatID, "photo_error", started)
		return nil
	}
	logEnrichment(chatID, "sent", started)
	return nil
}

func logEnrichment(chatID int64, outcome string, started time.Time) {
	log.Printf("fragrantica enrichment chat_id=%d outcome=%s duration=%s", chatID, outcome, time.Since(started).Round(time.Millisecond))
}

func (h *Handler) advance(ctx context.Context, session storage.Session) error {
	return h.advanceChecked(ctx, session, nil)
}

func (h *Handler) advanceOwned(ctx context.Context, session storage.Session, operationToken string) error {
	return h.advanceChecked(ctx, session, func() bool {
		return h.isOwned(session.ChatID, operationToken)
	})
}

func (h *Handler) advanceChecked(ctx context.Context, session storage.Session, owns func() bool) error {
	isOwned := func() bool { return owns == nil || owns() }
	var message Message
	switch {
	case session.Query.Concentration == domain.ConcentrationUnknown:
		switch len(session.Concentrations) {
		case 0:
			if !isOwned() {
				return nil
			}
			_ = h.sessions.Delete(ctx, session.ChatID)
			if !isOwned() {
				return nil
			}
			return h.messenger.Send(ctx, session.ChatID, Message{
				Text: "Не удалось определить концентрацию. Повтори запрос целиком, например: Dior Sauvage EDP.",
			})
		case 1:
			session.Query.Concentration = session.Concentrations[0]
			return h.advanceChecked(ctx, session, owns)
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
	if !isOwned() {
		return nil
	}
	if err := h.sessions.Save(ctx, session); err != nil {
		return err
	}
	if !isOwned() {
		return nil
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

func containsConcentration(values []domain.Concentration, wanted domain.Concentration) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
func newSessionID() string                         { return newRandomID() }

func newOperationToken() string { return newRandomID() }

func newRandomID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}
func (h *Handler) beginUpdate(parent context.Context, chatID, updateID int64) (context.Context, string, bool) {
	operations := h.operationsFor(chatID)
	token := newOperationToken()

	operations.mu.Lock()
	defer operations.mu.Unlock()
	if operations.admitted && updateID <= operations.latestUpdateID {
		return nil, "", false
	}
	operations.admitted = true
	operations.latestUpdateID = updateID
	if operations.active.cancel != nil {
		operations.active.cancel()
	}
	ctx, cancel := context.WithCancel(parent)
	operations.active = activeSearch{token: token, cancel: cancel}
	return ctx, token, true
}

func (h *Handler) operationsFor(chatID int64) *chatOperations {
	operations, _ := h.operations.LoadOrStore(chatID, &chatOperations{})
	return operations.(*chatOperations)
}

func (h *Handler) isOwned(chatID int64, token string) bool {
	value, ok := h.operations.Load(chatID)
	if !ok {
		return false
	}
	operations := value.(*chatOperations)
	operations.mu.Lock()
	defer operations.mu.Unlock()
	return operations.active.token == token && operations.active.cancel != nil
}

func (h *Handler) finishOwned(chatID int64, token string) {
	value, ok := h.operations.Load(chatID)
	if !ok {
		return
	}
	operations := value.(*chatOperations)
	operations.mu.Lock()
	if operations.active.token != token || operations.active.cancel == nil {
		operations.mu.Unlock()
		return
	}
	cancel := operations.active.cancel
	operations.active = activeSearch{}
	operations.mu.Unlock()
	cancel()
}
