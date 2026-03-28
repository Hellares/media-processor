package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"media-processor/internal/config"
	"media-processor/internal/handler"
	"media-processor/internal/middleware"
	"media-processor/internal/processor"
	"media-processor/internal/queue"
)

func main() {
	cfg := config.Load()

	log.Printf("🚀 Media Processor starting...")
	log.Printf("   Workers: %d", cfg.MaxWorkers)
	log.Printf("   Queue size: %d", cfg.QueueSize)
	log.Printf("   Max image: %dMB | Max video: %dMB", cfg.MaxImageSizeMB, cfg.MaxVideoSizeMB)
	log.Printf("   Timeout: %ds", cfg.ProcessTimeout)
	log.Printf("   Port: %s", cfg.Port)

	// Worker pool
	pool := queue.NewWorkerPool(
		cfg.MaxWorkers,
		cfg.QueueSize,
		time.Duration(cfg.ProcessTimeout)*time.Second,
	)

	// Limpiador de archivos temporales
	processor.StartTempCleaner(cfg.TempCleanupMins)

	// Handlers
	h := handler.NewProcessHandler(pool, cfg.MaxImageSizeMB, cfg.MaxVideoSizeMB)

	// Rate limiter: 60 requests por empresa por minuto
	rl := middleware.NewRateLimiter(60, time.Minute)

	// Router
	mux := http.NewServeMux()

	// Endpoints públicos
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/presets", h.ListPresets)

	// Endpoints protegidos
	mux.HandleFunc("/process/image", h.ProcessImage)
	mux.HandleFunc("/process/video", h.ProcessVideo)
	mux.HandleFunc("/process/video/thumbnail", h.ProcessVideoThumbnail)

	// Middleware chain: Auth → RateLimit → Handler
	var finalHandler http.Handler = mux
	finalHandler = rl.Middleware(finalHandler)
	finalHandler = middleware.APIKeyAuth(cfg.APIKey)(finalHandler)

	// CORS para desarrollo
	finalHandler = corsMiddleware(finalHandler)

	// Servidor
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      finalHandler,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("⏳ Shutting down gracefully...")
		pool.Shutdown()
		server.Close()
		log.Println("✅ Server stopped")
	}()

	log.Printf("✅ Media Processor ready on http://localhost:%s", cfg.Port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("❌ Server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		w.Header().Set("Access-Control-Expose-Headers", "X-Original-Size, X-Processed-Size, X-Reduction, X-Width, X-Height, X-Format, X-Duration, X-Video-Width, X-Video-Height, X-Video-Duration")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
