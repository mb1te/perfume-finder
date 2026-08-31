package telegram

import (
	"context"
	"strings"
	"testing"
)

func TestHandlerLimitsImmediateSearchBursts(t *testing.T) {
	m := &fakeMessenger{}
	h := NewHandler(m, newMemorySessions(), fakeSearcher{})
	for range 4 {
		if err := h.HandleMessage(context.Background(), 7, "Dior Sauvage"); err != nil {
			t.Fatal(err)
		}
	}
	last := m.messages[len(m.messages)-1].Text
	if !strings.Contains(last, "Слишком много запросов") {
		t.Fatal(last)
	}
}
