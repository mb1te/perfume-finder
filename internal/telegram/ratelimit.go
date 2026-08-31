package telegram

import (
	"golang.org/x/time/rate"
	"sync"
	"time"
)

type userLimiters struct {
	mu     sync.Mutex
	values map[int64]*rate.Limiter
}

func newUserLimiters() *userLimiters { return &userLimiters{values: map[int64]*rate.Limiter{}} }
func (l *userLimiters) Allow(chatID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	limiter := l.values[chatID]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(6*time.Second), 3)
		l.values[chatID] = limiter
	}
	return limiter.Allow()
}
