package api

import (
	"log"
	"net/http"
	"time"
)

// Router создаёт и настраивает HTTP-маршрутизатор
// Все /api/* маршруты требуют авторизации через Telegram initData
// Всё остальное отдаётся как статические файлы из папки web/
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// ============================================================
	// Диагностический endpoint (без авторизации)
	// Позволяет проверить, доступен ли сервер
	// ============================================================
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// ============================================================
	// API-маршруты (требуют авторизации)
	// ============================================================
	mux.HandleFunc("GET /api/tasks", s.withAuth(s.handleGetTasks))
	mux.HandleFunc("POST /api/tasks", s.withAuth(s.handleCreateTask))
	mux.HandleFunc("PATCH /api/tasks/{id}/status", s.withAuth(s.handleUpdateStatus))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.withAuth(s.handleDeleteTask))

	// ============================================================
	// Статические файлы (Mini App фронтенд)
	// Всё, что не /api/*, отдаётся из папки web/
	// ============================================================
	fs := http.FileServer(http.Dir("web"))
	mux.Handle("/", fs)

	// Оборачиваем в middleware: логирование → CORS → маршрутизация
	return loggingMiddleware(corsMiddleware(mux))
}

// loggingMiddleware логирует все входящие API-запросы
// Помогает отладить, доходят ли запросы до сервера
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Логируем только API-запросы (не статические файлы)
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
			log.Printf("📥 %s %s (Auth: %v)", r.Method, r.URL.Path, r.Header.Get("Authorization") != "")
		}
		next.ServeHTTP(w, r)
		if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
			log.Printf("📤 %s %s — %s", r.Method, r.URL.Path, time.Since(start))
		}
	})
}

// corsMiddleware добавляет заголовки CORS ко всем ответам
// Нужен, чтобы фронтенд мог обращаться к API
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, ngrok-skip-browser-warning")

		// Preflight-запрос — браузер спрашивает, можно ли отправить запрос
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
