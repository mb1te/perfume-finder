package telegram

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type Transport struct {
	bot     *bot.Bot
	handler *Handler
}

func NewTransport(token string, sessions Sessions, searcher Searcher) (*Transport, error) {
	b, err := bot.New(token,
		bot.WithDefaultHandler(func(context.Context, *bot.Bot, *models.Update) {}),
		bot.WithErrorsHandler(func(error) {}),
	)
	if err != nil {
		return nil, err
	}
	t := &Transport{bot: b}
	t.handler = NewHandler(t, sessions, searcher)
	b.RegisterHandler(bot.HandlerTypeMessageText, "", bot.MatchTypePrefix, t.onMessage)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, t.onCallback)
	return t, nil
}
func (t *Transport) Start(ctx context.Context) { t.bot.Start(ctx) }
func (t *Transport) Send(ctx context.Context, chatID int64, message Message) error {
	var keyboard [][]models.InlineKeyboardButton
	for i := 0; i < len(message.Buttons); i += 2 {
		end := i + 2
		if end > len(message.Buttons) {
			end = len(message.Buttons)
		}
		row := make([]models.InlineKeyboardButton, 0, end-i)
		for _, button := range message.Buttons[i:end] {
			row = append(row, models.InlineKeyboardButton{Text: button.Text, CallbackData: button.Data})
		}
		keyboard = append(keyboard, row)
	}
	params := &bot.SendMessageParams{ChatID: chatID, Text: message.Text}
	if len(keyboard) > 0 {
		params.ReplyMarkup = &models.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	}
	_, err := t.bot.SendMessage(ctx, params)
	return err
}
func (t *Transport) onMessage(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update.Message != nil {
		_ = t.handler.HandleMessage(ctx, update.Message.Chat.ID, update.Message.Text)
	}
}
func (t *Transport) onCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	chatID := update.CallbackQuery.From.ID
	if update.CallbackQuery.Message.Message != nil {
		chatID = update.CallbackQuery.Message.Message.Chat.ID
	}
	_ = t.handler.HandleCallback(ctx, chatID, update.CallbackQuery.Data)
	_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: update.CallbackQuery.ID})
}
