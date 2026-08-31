package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
type Handler struct {
	messenger Messenger
	sessions  Sessions
	searcher  Searcher
	limiters  *userLimiters
}

func NewHandler(m Messenger, sessions Sessions, searcher Searcher) *Handler {
	return &Handler{messenger: m, sessions: sessions, searcher: searcher, limiters: newUserLimiters()}
}

func (h *Handler) HandleMessage(ctx context.Context, chatID int64, text string) error {
	if !h.limiters.Allow(chatID) {
		return h.messenger.Send(ctx, chatID, Message{Text: "Слишком много запросов. Подожди несколько секунд."})
	}
	query := parseUserQuery(text)
	if err := h.sessions.Save(ctx, storage.Session{ChatID: chatID, Stage: "choose_concentration", Query: query}); err != nil {
		return err
	}
	return h.messenger.Send(ctx, chatID, Message{Text: "Выбери концентрацию", Buttons: []Button{{"EDT", "concentration:edt"}, {"EDP", "concentration:edp"}, {"Parfum", "concentration:parfum"}, {"Elixir", "concentration:elixir"}}})
}

func (h *Handler) HandleCallback(ctx context.Context, chatID int64, data string) error {
	session, ok, err := h.sessions.Load(ctx, chatID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("session not found")
	}
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid callback")
	}

	switch parts[0] {
	case "concentration":
		session.Query.Concentration, session.Stage = domain.Concentration(parts[1]), "choose_volume"
		if err := h.sessions.Save(ctx, session); err != nil {
			return err
		}
		return h.messenger.Send(ctx, chatID, Message{Text: "Выбери объём", Buttons: []Button{{"30 мл", "volume:30000"}, {"50 мл", "volume:50000"}, {"60 мл", "volume:60000"}, {"100 мл", "volume:100000"}, {"200 мл", "volume:200000"}}})
	case "volume":
		volume, err := strconv.Atoi(parts[1])
		if err != nil {
			return err
		}
		session.Query.VolumeMicroliters, session.Stage = volume, "choose_kind"
		if err := h.sessions.Save(ctx, session); err != nil {
			return err
		}
		return h.messenger.Send(ctx, chatID, Message{Text: "Выбери вид", Buttons: []Button{{"Флакон", "kind:retail"}, {"Тестер", "kind:tester"}, {"Отливант", "kind:decant"}, {"Миниатюра", "kind:miniature"}, {"Пробник", "kind:sample"}, {"Все виды", "kind:all"}}})
	case "kind":
		session.Query.Kind = domain.ProductKind(parts[1])
		result := h.searcher.Search(ctx, session.Query)
		if err := h.sessions.Delete(ctx, chatID); err != nil {
			return err
		}
		return h.messenger.Send(ctx, chatID, Message{Text: RenderResult(session.Query, result)})
	default:
		return fmt.Errorf("unknown callback")
	}
}

func parseUserQuery(text string) domain.SearchQuery {
	fields := strings.Fields(text)
	query := domain.SearchQuery{Raw: text}
	if len(fields) < 2 {
		return query
	}
	if len(fields) >= 3 && strings.EqualFold(fields[0], "Christian") && strings.EqualFold(fields[1], "Dior") {
		query.Brand, query.Name = "Christian Dior", strings.Join(fields[2:], " ")
	} else {
		query.Brand, query.Name = fields[0], strings.Join(fields[1:], " ")
	}
	return query
}
