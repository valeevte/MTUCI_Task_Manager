package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"mtuci-task-manager/bot"
)

type NotificationSender func(userID int64, message string) error

const maxJSONBodySize = 64 << 10

// Server — HTTP API сервер
// Содержит ссылку на общее хранилище задач и токен бота.
type Server struct {
	storage  *bot.Storage
	botToken string
	notify   NotificationSender
}

// NewServer создаёт новый API-сервер.
func NewServer(storage *bot.Storage, botToken string) *Server {
	return &Server{
		storage:  storage,
		botToken: botToken,
	}
}

// SetNotifier подключает callback для отправки уведомлений в Telegram-бот.
func (s *Server) SetNotifier(notifier NotificationSender) {
	s.notify = notifier
}

func statusFromKey(status string) (string, bool) {
	switch status {
	case "new":
		return bot.StatusNew, true
	case "progress":
		return bot.StatusInProgress, true
	case "done":
		return bot.StatusDone, true
	default:
		return "", false
	}
}

func groupIDFromPath(r *http.Request) (int, error) {
	return positiveID(r.PathValue("groupId"))
}

func taskIDFromPath(r *http.Request) (int, error) {
	return positiveID(r.PathValue("taskId"))
}

func positiveID(value string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid positive id")
	}
	return id, nil
}

func writeStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bot.ErrGroupNotFound),
		errors.Is(err, bot.ErrGroupTaskNotFound),
		errors.Is(err, bot.ErrUserNotFound),
		errors.Is(err, bot.ErrMemberNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, bot.ErrForbidden),
		errors.Is(err, bot.ErrOnlyCreatorCanDelete),
		errors.Is(err, bot.ErrOnlyCreatorCanManage):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, bot.ErrInvalidGroupName),
		errors.Is(err, bot.ErrInvalidTaskTitle),
		errors.Is(err, bot.ErrTaskTitleTooLong),
		errors.Is(err, bot.ErrTaskDescriptionTooLong),
		errors.Is(err, bot.ErrGroupNameTooLong),
		errors.Is(err, bot.ErrInvalidStatus),
		errors.Is(err, bot.ErrInvalidUsername):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, bot.ErrUserAlreadyInGroup),
		errors.Is(err, bot.ErrCreatorCannotBeRemoved),
		errors.Is(err, bot.ErrAssigneeMustBeMember):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		log.Printf("❌ internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

func (s *Server) notifyGroupTaskEvent(groupID int, task bot.GroupTask, event string, additionalRecipients ...int64) {
	if s.notify == nil {
		return
	}

	group, ok := s.storage.GetGroup(groupID)
	if !ok {
		return
	}

	recipients, err := s.storage.NotificationRecipients(groupID, task.AssigneeUserID)
	if err != nil {
		log.Printf("❌ notify recipients error: %v", err)
		return
	}
	seen := make(map[int64]struct{}, len(recipients)+len(additionalRecipients))
	orderedRecipients := make([]int64, 0, len(recipients)+len(additionalRecipients))
	for _, userID := range recipients {
		seen[userID] = struct{}{}
		orderedRecipients = append(orderedRecipients, userID)
	}
	for _, userID := range additionalRecipients {
		if userID > 0 {
			if _, exists := seen[userID]; exists {
				continue
			}
			seen[userID] = struct{}{}
			orderedRecipients = append(orderedRecipients, userID)
		}
	}

	assignee := "👥 Общая задача"
	if task.AssigneeUsername != "" {
		assignee = "👤 Ответственный: " + task.AssigneeUsername
	}

	text := fmt.Sprintf(
		"🔔 %s\n\n👥 Группа: %s\n📌 Задача: %s\n%s\n📊 Статус: %s",
		event,
		group.Name,
		task.Title,
		assignee,
		task.Status,
	)

	for _, userID := range orderedRecipients {
		if err := s.notify(userID, text); err != nil {
			log.Printf("❌ notify user=%d failed: %v", userID, err)
		}
	}
}

// ============================================================
// Личные задачи (legacy API)
// ============================================================

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	tasks := s.storage.GetTasks(user.ID)
	if tasks == nil {
		tasks = []bot.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	title, description, err := bot.ValidateTaskText(req.Title, req.Description)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	task := s.storage.AddTask(user.ID, title, description)
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	idStr := r.PathValue("id")
	taskID, err := positiveID(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID задачи"})
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	fullStatus, ok := statusFromKey(req.Status)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный статус (допустимые: new, progress, done)",
		})
		return
	}

	if s.storage.UpdateStatus(user.ID, taskID, fullStatus) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "задача не найдена"})
	}
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	idStr := r.PathValue("id")
	taskID, err := positiveID(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID задачи"})
		return
	}

	if s.storage.DeleteTask(user.ID, taskID) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	} else {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "задача не найдена"})
	}
}

// ============================================================
// Группы
// ============================================================

func (s *Server) handleGetGroups(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groups := s.storage.GetUserGroups(user.ID)
	if groups == nil {
		groups = []bot.Group{}
	}
	writeJSON(w, http.StatusOK, groups)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)

	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	group, err := s.storage.CreateGroup(user.ID, req.Name)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, group)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	if err := s.storage.DeleteGroup(user.ID, groupID); err != nil {
		writeStorageError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetGroupMembers(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	members, err := s.storage.GetGroupMembers(user.ID, groupID)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	if members == nil {
		members = []bot.GroupMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	var req struct {
		Username string `json:"username"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username обязателен"})
		return
	}

	member, err := s.storage.AddGroupMemberByUsername(user.ID, groupID, req.Username)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, member)
}

func (s *Server) handleDeleteGroupMember(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	memberID, err := strconv.ParseInt(r.PathValue("memberId"), 10, 64)
	if err != nil || memberID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID участника"})
		return
	}

	if err := s.storage.RemoveGroupMember(user.ID, groupID, memberID); err != nil {
		writeStorageError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetGroupTasks(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	tasks, err := s.storage.GetGroupTasks(user.ID, groupID)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	if tasks == nil {
		tasks = []bot.GroupTask{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleCreateGroupTask(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	var req struct {
		Title            string `json:"title"`
		Description      string `json:"description"`
		AssigneeUsername string `json:"assignee_username"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	task, err := s.storage.CreateGroupTask(user.ID, groupID, req.Title, req.Description, req.AssigneeUsername)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	s.notifyGroupTaskEvent(groupID, task, "Создана групповая задача")
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleUpdateGroupTaskStatus(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID задачи"})
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	fullStatus, ok := statusFromKey(req.Status)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "неверный статус (допустимые: new, progress, done)",
		})
		return
	}

	task, err := s.storage.UpdateGroupTaskStatus(user.ID, groupID, taskID, fullStatus)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	s.notifyGroupTaskEvent(groupID, task, "Изменён статус групповой задачи")
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleSetGroupTaskAssignee(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID задачи"})
		return
	}

	var req struct {
		AssigneeUsername string `json:"assignee_username"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	previousTask, err := s.storage.GetGroupTask(user.ID, groupID, taskID)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	task, err := s.storage.SetGroupTaskAssignee(user.ID, groupID, taskID, req.AssigneeUsername)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	var previousAssignee []int64
	if previousTask.AssigneeUserID != nil {
		previousAssignee = append(previousAssignee, *previousTask.AssigneeUserID)
	}
	s.notifyGroupTaskEvent(groupID, task, "Изменён ответственный групповой задачи", previousAssignee...)
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleDeleteGroupTask(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*TelegramUser)
	groupID, err := groupIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID группы"})
		return
	}

	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный ID задачи"})
		return
	}

	task, err := s.storage.DeleteGroupTask(user.ID, groupID, taskID)
	if err != nil {
		writeStorageError(w, err)
		return
	}

	s.notifyGroupTaskEvent(groupID, task, "Групповая задача удалена")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// writeJSON — вспомогательная функция для отправки JSON-ответа.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("❌ encode JSON response: %v", err)
	}
}

// decodeJSONBody ограничивает память, запрещает опечатки в именах полей и
// отклоняет несколько JSON-объектов в одном запросе.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "тело запроса слишком велико"})
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "неверный формат запроса"})
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "в запросе должен быть один JSON-объект"})
		return false
	}
	return true
}
