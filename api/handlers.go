package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"mtuci-task-manager/bot"
)

// ============================================================
// Server — HTTP API сервер
// Содержит ссылку на общее хранилище задач и токен бота
// ============================================================
type Server struct {
	storage  *bot.Storage // Общее хранилище задач (то же, что использует бот)
	botToken string       // Токен бота (для валидации initData)
}

// NewServer создаёт новый API-сервер
func NewServer(storage *bot.Storage, botToken string) *Server {
	return &Server{
		storage:  storage,
		botToken: botToken,
	}
}

// ============================================================
// handleGetTasks — GET /api/tasks
// Возвращает все задачи текущего пользователя
// ============================================================
func (s *Server) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	tasks := s.storage.GetTasks(user.ID)

	// Если задач нет — возвращаем пустой массив (не null)
	if tasks == nil {
		tasks = []bot.Task{}
	}

	writeJSON(w, http.StatusOK, tasks)
}

// ============================================================
// handleCreateTask — POST /api/tasks
// Создаёт новую задачу
// Тело запроса: {"title": "...", "description": "..."}
// ============================================================
func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный формат запроса",
		})
		return
	}

	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "название задачи обязательно",
		})
		return
	}

	task := s.storage.AddTask(user.ID, req.Title, req.Description)
	writeJSON(w, http.StatusCreated, task)
}

// ============================================================
// handleUpdateStatus — PATCH /api/tasks/{id}/status
// Обновляет статус задачи
// Тело запроса: {"status": "new" | "progress" | "done"}
// ============================================================
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	// Извлекаем ID задачи из URL (Go 1.22+ поддерживает {id} в путях)
	idStr := r.PathValue("id")
	taskID, err := strconv.Atoi(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный ID задачи",
		})
		return
	}

	var req struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный формат запроса",
		})
		return
	}

	// Преобразуем короткий ключ статуса в полный
	var fullStatus string
	switch req.Status {
	case "new":
		fullStatus = bot.StatusNew
	case "progress":
		fullStatus = bot.StatusInProgress
	case "done":
		fullStatus = bot.StatusDone
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный статус (допустимые: new, progress, done)",
		})
		return
	}

	if s.storage.UpdateStatus(user.ID, taskID, fullStatus) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "задача не найдена",
		})
	}
}

// ============================================================
// handleDeleteTask — DELETE /api/tasks/{id}
// Удаляет задачу по ID
// ============================================================
func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	idStr := r.PathValue("id")
	taskID, err := strconv.Atoi(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный ID задачи",
		})
		return
	}

	if s.storage.DeleteTask(user.ID, taskID) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "задача не найдена",
		})
	}
}

// ============================================================
// writeJSON — вспомогательная функция для отправки JSON-ответа
// ============================================================
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
