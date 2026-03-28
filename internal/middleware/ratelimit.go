package middleware

import (
	"net/http"
	"sync"
	"time"

	"media-processor/pkg/response"
)

type RateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*clientWindow
	limit    int
	window   time.Duration
}

type clientWindow struct {
	count    int
	resetAt  time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*clientWindow),
		limit:   limit,
		window:  window,
	}

	// Limpiar entradas expiradas cada minuto
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Usar empresaId del form o IP como key
		key := r.FormValue("empresaId")
		if key == "" {
			key = r.RemoteAddr
		}

		rl.mu.Lock()
		client, exists := rl.clients[key]
		now := time.Now()

		if !exists || now.After(client.resetAt) {
			rl.clients[key] = &clientWindow{count: 1, resetAt: now.Add(rl.window)}
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}

		if client.count >= rl.limit {
			rl.mu.Unlock()
			response.Error(w, http.StatusTooManyRequests, "RATE_LIMIT", "Demasiadas solicitudes, intenta en un momento")
			return
		}

		client.count++
		rl.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for k, v := range rl.clients {
		if now.After(v.resetAt) {
			delete(rl.clients, k)
		}
	}
}
