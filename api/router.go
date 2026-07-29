package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	mux.HandleFunc("GET /api/me", s.withAuth(s.handleMe))
	mux.HandleFunc("GET /api/tasks", s.withAuth(s.handleGetTasks))
	mux.HandleFunc("POST /api/tasks", s.withAuth(s.handleCreateTask))
	mux.HandleFunc("PATCH /api/tasks/{id}/status", s.withAuth(s.handleUpdateStatus))
	mux.HandleFunc("DELETE /api/tasks/{id}", s.withAuth(s.handleDeleteTask))

	mux.HandleFunc("GET /api/groups", s.withAuth(s.handleGetGroups))
	mux.HandleFunc("POST /api/groups", s.withAuth(s.handleCreateGroup))
	mux.HandleFunc("DELETE /api/groups/{groupId}", s.withAuth(s.handleDeleteGroup))

	mux.HandleFunc("GET /api/groups/{groupId}/members", s.withAuth(s.handleGetGroupMembers))
	mux.HandleFunc("POST /api/groups/{groupId}/members", s.withAuth(s.handleAddGroupMember))
	mux.HandleFunc("DELETE /api/groups/{groupId}/members/{memberId}", s.withAuth(s.handleDeleteGroupMember))

	mux.HandleFunc("GET /api/groups/{groupId}/tasks", s.withAuth(s.handleGetGroupTasks))
	mux.HandleFunc("POST /api/groups/{groupId}/tasks", s.withAuth(s.handleCreateGroupTask))
	mux.HandleFunc("PATCH /api/groups/{groupId}/tasks/{taskId}/status", s.withAuth(s.handleUpdateGroupTaskStatus))
	mux.HandleFunc("PATCH /api/groups/{groupId}/tasks/{taskId}/assignee", s.withAuth(s.handleSetGroupTaskAssignee))
	mux.HandleFunc("DELETE /api/groups/{groupId}/tasks/{taskId}", s.withAuth(s.handleDeleteGroupTask))

	// ============================================================
	// Статические файлы (Mini App фронтенд)
	// Всё, что не /api/*, отдаётся из папки web/
	// ============================================================
	fs := http.FileServer(http.Dir(findWebDir()))
	mux.Handle("/", fs)

	// Оборачиваем в middleware: логирование → CORS → маршрутизация
	return loggingMiddleware(securityHeadersMiddleware(corsMiddleware(mux)))
}

func findWebDir() string {
	candidates := []string{"web"}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "web"))
	}
	if execPath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(execPath), "web"))
	}

	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	log.Println("⚠️  Папка web/ не найдена, используем относительный путь web")
	return "web"
}

// loggingMiddleware логирует все входящие API-запросы
// Помогает отладить, доходят ли запросы до сервера
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Логируем только API-запросы (не статические файлы)
		if isAPIPath(r.URL.Path) {
			log.Printf("📥 %s %s (Auth: %v)", r.Method, r.URL.Path, r.Header.Get("Authorization") != "")
		}
		next.ServeHTTP(w, r)
		if isAPIPath(r.URL.Path) {
			log.Printf("📤 %s %s — %s", r.Method, r.URL.Path, time.Since(start))
		}
	})
}

func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if isAPIPath(r.URL.Path) {
			// Ответы API зависят от пользователя и не должны попадать в кэш.
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware добавляет заголовки CORS ко всем ответам
// Нужен, чтобы фронтенд мог обращаться к API
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Telegram-Init-Data, ngrok-skip-browser-warning")

		// Preflight-запрос — браузер спрашивает, можно ли отправить запрос
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
