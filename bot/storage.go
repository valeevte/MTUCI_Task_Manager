package bot

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ============================================================
// Статусы задач
// ============================================================
const (
	StatusNew        = "🆕 Новая"
	StatusInProgress = "🔄 В работе"
	StatusDone       = "✅ Выполнена"

	// Ограничения не дают одному запросу бесконтрольно раздувать in-memory
	// хранилище и одновременно оставляют достаточно места для обычных задач.
	MaxTaskTitleLength       = 200
	MaxTaskDescriptionLength = 4000
	MaxGroupNameLength       = 100
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{5,32}$`)

// ============================================================
// Ошибки доменной логики
// ============================================================
var (
	ErrGroupNotFound          = errors.New("group not found")
	ErrGroupTaskNotFound      = errors.New("group task not found")
	ErrForbidden              = errors.New("forbidden")
	ErrInvalidGroupName       = errors.New("invalid group name")
	ErrInvalidTaskTitle       = errors.New("invalid task title")
	ErrTaskNotFound           = errors.New("task not found")
	ErrEmptyTaskPatch         = errors.New("empty task patch")
	ErrTaskTitleTooLong       = errors.New("task title is too long")
	ErrTaskDescriptionTooLong = errors.New("task description is too long")
	ErrGroupNameTooLong       = errors.New("group name is too long")
	ErrInvalidStatus          = errors.New("invalid task status")
	ErrInvalidUsername        = errors.New("invalid username")
	ErrUserNotFound           = errors.New("user not found")
	ErrUserAlreadyInGroup     = errors.New("user already in group")
	ErrMemberNotFound         = errors.New("member not found")
	ErrCreatorCannotBeRemoved = errors.New("creator cannot be removed")
	ErrOnlyCreatorCanDelete   = errors.New("only creator can delete group")
	ErrOnlyCreatorCanManage   = errors.New("only creator can manage group members")
	ErrAssigneeMustBeMember   = errors.New("assignee must be member of group")
	ErrAdminOnly              = errors.New("admin only")
	ErrTaskEditForbidden      = errors.New("task edit forbidden")
	ErrStatusChangeForbidden  = errors.New("status change forbidden")
	ErrStatusTransitionForbidden = errors.New("status transition forbidden")
	ErrCannotAssignAdmin      = errors.New("cannot assign admin")
)

// ============================================================
// Личный режим задач (без изменений)
// ============================================================

type Task struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ============================================================
// Групповой режим
// ============================================================

type VerifiedUser struct {
	ID        int64     `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Username  string    `json:"username"`
	LastSeen  time.Time `json:"last_seen_at"`
}

type Group struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	CreatorID int64     `json:"creator_id"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupRole string

const (
	GroupRoleAdmin GroupRole = "admin"
	GroupRoleUser  GroupRole = "user"
)

type GroupMember struct {
	UserID    int64     `json:"user_id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Username  string    `json:"username"`
	Role      GroupRole `json:"role"`
}

type GroupTask struct {
	ID               int       `json:"id"`
	GroupID          int       `json:"group_id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Status           string    `json:"status"`
	CreatedBy        int64     `json:"created_by"`
	AssigneeUserID   *int64    `json:"assignee_user_id,omitempty"`
	AssigneeUsername string    `json:"assignee_username,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ============================================================
// Storage — общее in-memory хранилище
// ============================================================

type Storage struct {
	// Личные задачи
	tasks  map[int64][]Task
	nextID map[int64]int

	// Верифицированные пользователи (Mini App)
	usersByID        map[int64]VerifiedUser
	userIDByUsername map[string]int64

	// Группы
	groups          map[int]Group
	groupMembers    map[int]map[int64]GroupRole
	groupTasks      map[int][]GroupTask
	nextGroupID     int
	nextGroupTaskID map[int]int

	mu sync.RWMutex
}

func NewStorage() *Storage {
	return &Storage{
		tasks:            make(map[int64][]Task),
		nextID:           make(map[int64]int),
		usersByID:        make(map[int64]VerifiedUser),
		userIDByUsername: make(map[string]int64),
		groups:           make(map[int]Group),
		groupMembers:     make(map[int]map[int64]GroupRole),
		groupTasks:       make(map[int][]GroupTask),
		nextGroupTaskID:  make(map[int]int),
	}
}

// ============================================================
// Личные задачи
// ============================================================

func (s *Storage) AddTask(userID int64, title, description string) Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	s.nextID[userID]++
	id := s.nextID[userID]

	now := time.Now()
	task := Task{
		ID:          id,
		Title:       title,
		Description: description,
		Status:      StatusNew,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.tasks[userID] = append(s.tasks[userID], task)
	return task
}

func (s *Storage) GetTasks(userID int64) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]Task(nil), s.tasks[userID]...)
}

func (s *Storage) GetTask(userID int64, taskID int) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, task := range s.tasks[userID] {
		if task.ID == taskID {
			return task, true
		}
	}
	return Task{}, false
}

func (s *Storage) UpdateStatus(userID int64, taskID int, newStatus string) bool {
	if !IsValidStatus(newStatus) {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, task := range s.tasks[userID] {
		if task.ID == taskID {
			if task.Status != newStatus {
				s.tasks[userID][i].Status = newStatus
				s.tasks[userID][i].UpdatedAt = time.Now()
			}
			return true
		}
	}
	return false
}

// UpdateTask применяет частичное изменение личной задачи пользователя.
func (s *Storage) UpdateTask(userID int64, taskID int, title, description *string) (Task, bool, error) {
	if title == nil && description == nil {
		return Task{}, false, ErrEmptyTaskPatch
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, task := range s.tasks[userID] {
		if task.ID != taskID {
			continue
		}

		newTitle := task.Title
		if title != nil {
			newTitle = *title
		}
		newDescription := task.Description
		if description != nil {
			newDescription = *description
		}

		newTitle, newDescription, err := ValidateTaskText(newTitle, newDescription)
		if err != nil {
			return Task{}, false, err
		}
		if task.Title == newTitle && task.Description == newDescription {
			return task, false, nil
		}

		task.Title = newTitle
		task.Description = newDescription
		task.UpdatedAt = time.Now()
		s.tasks[userID][i] = task
		return task, true, nil
	}

	return Task{}, false, ErrTaskNotFound
}

// ValidateTaskText централизует одинаковую валидацию для бота и HTTP API.
func ValidateTaskText(title, description string) (string, string, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)

	if title == "" {
		return "", "", ErrInvalidTaskTitle
	}
	if utf8.RuneCountInString(title) > MaxTaskTitleLength {
		return "", "", ErrTaskTitleTooLong
	}
	if utf8.RuneCountInString(description) > MaxTaskDescriptionLength {
		return "", "", ErrTaskDescriptionTooLong
	}
	return title, description, nil
}

// IsValidStatus не позволяет записать произвольную строку вместо статуса.
func IsValidStatus(status string) bool {
	switch status {
	case StatusNew, StatusInProgress, StatusDone:
		return true
	default:
		return false
	}
}

func canEditGroupTask(role GroupRole, actorID int64, task GroupTask) bool {
	return role == GroupRoleAdmin || task.CreatedBy == actorID
}

func canChangeGroupTaskStatus(role GroupRole, actorID int64, task GroupTask) bool {
	if role == GroupRoleAdmin {
		return true
	}
	return task.AssigneeUserID == nil || *task.AssigneeUserID == actorID
}

func isAllowedUserStatusTransition(from, to string) bool {
	return (from == StatusNew && to == StatusInProgress) ||
		(from == StatusInProgress && to == StatusDone) ||
		(from == StatusDone && to == StatusInProgress)
}

func (s *Storage) DeleteTask(userID int64, taskID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	tasks := s.tasks[userID]
	for i, task := range tasks {
		if task.ID == taskID {
			s.tasks[userID] = append(tasks[:i], tasks[i+1:]...)
			return true
		}
	}
	return false
}

// ============================================================
// Верификация и пользователи
// ============================================================

func normalizeUsername(username string) string {
	u := strings.TrimSpace(strings.ToLower(username))
	u = strings.TrimPrefix(u, "@")
	return u
}

func formatUsername(username string) string {
	if username == "" {
		return ""
	}
	return "@" + username
}

func normalizeMentionUsername(username string) (string, error) {
	u := strings.TrimSpace(username)
	if !strings.HasPrefix(u, "@") {
		return "", ErrInvalidUsername
	}
	normalized := normalizeUsername(u)
	if !usernamePattern.MatchString(normalized) {
		return "", ErrInvalidUsername
	}
	return normalized, nil
}

func (s *Storage) UpsertVerifiedUser(userID int64, username, firstName, lastName string) VerifiedUser {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := normalizeUsername(username)
	if normalized != "" && !usernamePattern.MatchString(normalized) {
		normalized = ""
	}

	if old, ok := s.usersByID[userID]; ok && old.Username != "" && old.Username != normalized {
		// Username мог уже перейти к другому пользователю. Удаляем индекс только
		// если он всё ещё указывает на обновляемую запись.
		if indexedID, exists := s.userIDByUsername[old.Username]; exists && indexedID == userID {
			delete(s.userIDByUsername, old.Username)
		}
	}

	user := VerifiedUser{
		ID:        userID,
		FirstName: firstName,
		LastName:  lastName,
		Username:  normalized,
		LastSeen:  time.Now(),
	}
	s.usersByID[userID] = user

	if normalized != "" {
		// Telegram usernames уникальны, но могут переходить между аккаунтами.
		// Очищаем устаревшую запись прежнего владельца, чтобы индекс и данные
		// пользователя не противоречили друг другу.
		if previousID, exists := s.userIDByUsername[normalized]; exists && previousID != userID {
			previous := s.usersByID[previousID]
			previous.Username = ""
			s.usersByID[previousID] = previous
		}
		s.userIDByUsername[normalized] = userID
	}

	return s.userForResponse(user)
}

func (s *Storage) userForResponse(user VerifiedUser) VerifiedUser {
	user.Username = formatUsername(user.Username)
	return user
}

func (s *Storage) memberForResponse(user VerifiedUser, role GroupRole) GroupMember {
	return GroupMember{
		UserID:    user.ID,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Username:  formatUsername(user.Username),
		Role:      role,
	}
}

func (s *Storage) taskForResponse(task GroupTask) GroupTask {
	if task.AssigneeUserID == nil {
		task.AssigneeUsername = ""
		return task
	}

	if user, ok := s.usersByID[*task.AssigneeUserID]; ok {
		task.AssigneeUsername = formatUsername(user.Username)
	}
	return task
}

// ============================================================
// Группы
// ============================================================

func validateGroupName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrInvalidGroupName
	}
	if utf8.RuneCountInString(trimmed) > MaxGroupNameLength {
		return "", ErrGroupNameTooLong
	}
	return trimmed, nil
}

func (s *Storage) CreateGroup(actorID int64, name string) (Group, error) {
	trimmed, err := validateGroupName(name)
	if err != nil {
		return Group{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextGroupID++
	id := s.nextGroupID
	group := Group{
		ID:        id,
		Name:      trimmed,
		CreatorID: actorID,
		CreatedAt: time.Now(),
	}

	s.groups[id] = group
	s.groupMembers[id] = map[int64]GroupRole{actorID: GroupRoleAdmin}
	return group, nil
}

func (s *Storage) UpdateGroupName(actorID int64, groupID int, name string) (Group, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, role, err := s.ensureGroupMemberLocked(groupID, actorID)
	if err != nil {
		return Group{}, false, err
	}
	if role != GroupRoleAdmin {
		return Group{}, false, ErrAdminOnly
	}

	trimmed, err := validateGroupName(name)
	if err != nil {
		return Group{}, false, err
	}
	if group.Name == trimmed {
		return group, false, nil
	}

	group.Name = trimmed
	s.groups[groupID] = group
	return group, true, nil
}

func (s *Storage) GetGroup(groupID int) (Group, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	group, ok := s.groups[groupID]
	return group, ok
}

func (s *Storage) GetUserGroups(userID int64) []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()

	groups := make([]Group, 0)
	for groupID, members := range s.groupMembers {
		if _, ok := members[userID]; !ok {
			continue
		}
		group, exists := s.groups[groupID]
		if exists {
			groups = append(groups, group)
		}
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].ID < groups[j].ID
	})

	return groups
}

func (s *Storage) DeleteGroup(actorID int64, groupID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.groups[groupID]
	if !ok {
		return ErrGroupNotFound
	}
	if group.CreatorID != actorID {
		return ErrOnlyCreatorCanDelete
	}

	delete(s.groups, groupID)
	delete(s.groupMembers, groupID)
	delete(s.groupTasks, groupID)
	delete(s.nextGroupTaskID, groupID)

	return nil
}

func (s *Storage) ensureGroupMemberLocked(groupID int, userID int64) (Group, GroupRole, error) {
	group, ok := s.groups[groupID]
	if !ok {
		return Group{}, "", ErrGroupNotFound
	}

	members, ok := s.groupMembers[groupID]
	if !ok {
		return Group{}, "", ErrGroupNotFound
	}

	role, exists := members[userID]
	if !exists {
		return Group{}, "", ErrForbidden
	}

	return group, role, nil
}

func (s *Storage) GetGroupMembers(actorID int64, groupID int) ([]GroupMember, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, _, err := s.ensureGroupMemberLocked(groupID, actorID)
	if err != nil {
		return nil, err
	}

	members := make([]GroupMember, 0, len(s.groupMembers[groupID]))
	for memberID, role := range s.groupMembers[groupID] {
		user, ok := s.usersByID[memberID]
		if !ok {
			members = append(members, GroupMember{
				UserID:    memberID,
				FirstName: "Участник",
				Role:      role,
			})
			continue
		}
		members = append(members, s.memberForResponse(user, role))
	}

	sort.Slice(members, func(i, j int) bool {
		if members[i].Role != members[j].Role {
			return members[i].Role == GroupRoleAdmin
		}
		if members[i].Username == members[j].Username {
			return members[i].UserID < members[j].UserID
		}
		return members[i].Username < members[j].Username
	})

	return members, nil
}

func (s *Storage) AddGroupMemberByUsername(actorID int64, groupID int, username string) (GroupMember, error) {
	normalized, err := normalizeMentionUsername(username)
	if err != nil {
		return GroupMember{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, actorRole, err := s.ensureGroupMemberLocked(groupID, actorID)
	if err != nil {
		return GroupMember{}, err
	}
	if actorRole != GroupRoleAdmin {
		return GroupMember{}, ErrOnlyCreatorCanManage
	}

	targetID, ok := s.userIDByUsername[normalized]
	if !ok {
		return GroupMember{}, ErrUserNotFound
	}

	if _, ok := s.groupMembers[groupID][targetID]; ok {
		return GroupMember{}, ErrUserAlreadyInGroup
	}

	s.groupMembers[groupID][targetID] = GroupRoleUser

	user := s.usersByID[targetID]
	return s.memberForResponse(user, GroupRoleUser), nil
}

func (s *Storage) RemoveGroupMember(actorID int64, groupID int, memberID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, actorRole, err := s.ensureGroupMemberLocked(groupID, actorID)
	if err != nil {
		return err
	}

	memberRole, exists := s.groupMembers[groupID][memberID]
	if !exists {
		return ErrMemberNotFound
	}
	if memberRole == GroupRoleAdmin {
		return ErrCreatorCannotBeRemoved
	}
	// Участник может выйти сам; исключать других может только администратор.
	if actorRole != GroupRoleAdmin && actorID != memberID {
		return ErrOnlyCreatorCanManage
	}

	delete(s.groupMembers[groupID], memberID)

	// Если удалили участника, снимаем его с назначенных задач.
	for i := range s.groupTasks[groupID] {
		task := &s.groupTasks[groupID][i]
		if task.AssigneeUserID != nil && *task.AssigneeUserID == memberID {
			task.AssigneeUserID = nil
			task.AssigneeUsername = ""
			task.UpdatedAt = time.Now()
		}
	}

	return nil
}

// ============================================================
// Групповые задачи
// ============================================================

func (s *Storage) resolveAssigneeLocked(groupID int, assigneeUsername string) (*int64, error) {
	trimmed := strings.TrimSpace(assigneeUsername)
	if trimmed == "" {
		return nil, nil
	}

	normalized, err := normalizeMentionUsername(trimmed)
	if err != nil {
		return nil, err
	}

	userID, ok := s.userIDByUsername[normalized]
	if !ok {
		return nil, ErrUserNotFound
	}

	if _, ok := s.groupMembers[groupID][userID]; !ok {
		return nil, ErrAssigneeMustBeMember
	}

	id := userID
	return &id, nil
}

func (s *Storage) CreateGroupTask(actorID int64, groupID int, title, description, assigneeUsername string) (GroupTask, error) {
	trimmedTitle, trimmedDescription, err := ValidateTaskText(title, description)
	if err != nil {
		return GroupTask{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, _, err := s.ensureGroupMemberLocked(groupID, actorID); err != nil {
		return GroupTask{}, err
	}

	assigneeID, err := s.resolveAssigneeLocked(groupID, assigneeUsername)
	if err != nil {
		return GroupTask{}, err
	}

	s.nextGroupTaskID[groupID]++
	now := time.Now()
	task := GroupTask{
		ID:             s.nextGroupTaskID[groupID],
		GroupID:        groupID,
		Title:          trimmedTitle,
		Description:    trimmedDescription,
		Status:         StatusNew,
		CreatedBy:      actorID,
		AssigneeUserID: assigneeID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	s.groupTasks[groupID] = append(s.groupTasks[groupID], task)
	return s.taskForResponse(task), nil
}

func (s *Storage) GetGroupTasks(actorID int64, groupID int) ([]GroupTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, _, err := s.ensureGroupMemberLocked(groupID, actorID); err != nil {
		return nil, err
	}

	tasks := s.groupTasks[groupID]
	result := make([]GroupTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, s.taskForResponse(task))
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

func (s *Storage) GetGroupTask(actorID int64, groupID, taskID int) (GroupTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, _, err := s.ensureGroupMemberLocked(groupID, actorID); err != nil {
		return GroupTask{}, err
	}
	for _, task := range s.groupTasks[groupID] {
		if task.ID == taskID {
			return s.taskForResponse(task), nil
		}
	}
	return GroupTask{}, ErrGroupTaskNotFound
}

func (s *Storage) UpdateGroupTaskStatus(actorID int64, groupID, taskID int, newStatus string) (GroupTask, error) {
	if !IsValidStatus(newStatus) {
		return GroupTask{}, ErrInvalidStatus
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, _, err := s.ensureGroupMemberLocked(groupID, actorID); err != nil {
		return GroupTask{}, err
	}

	for i, task := range s.groupTasks[groupID] {
		if task.ID == taskID {
			s.groupTasks[groupID][i].Status = newStatus
			s.groupTasks[groupID][i].UpdatedAt = time.Now()
			return s.taskForResponse(s.groupTasks[groupID][i]), nil
		}
	}

	return GroupTask{}, ErrGroupTaskNotFound
}

func (s *Storage) SetGroupTaskAssignee(actorID int64, groupID, taskID int, assigneeUsername string) (GroupTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, _, err := s.ensureGroupMemberLocked(groupID, actorID); err != nil {
		return GroupTask{}, err
	}

	assigneeID, err := s.resolveAssigneeLocked(groupID, assigneeUsername)
	if err != nil {
		return GroupTask{}, err
	}

	for i, task := range s.groupTasks[groupID] {
		if task.ID == taskID {
			s.groupTasks[groupID][i].AssigneeUserID = assigneeID
			s.groupTasks[groupID][i].UpdatedAt = time.Now()
			return s.taskForResponse(s.groupTasks[groupID][i]), nil
		}
	}

	return GroupTask{}, ErrGroupTaskNotFound
}

func (s *Storage) DeleteGroupTask(actorID int64, groupID, taskID int) (GroupTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, _, err := s.ensureGroupMemberLocked(groupID, actorID); err != nil {
		return GroupTask{}, err
	}

	tasks := s.groupTasks[groupID]
	for i, task := range tasks {
		if task.ID == taskID {
			deleted := s.taskForResponse(task)
			s.groupTasks[groupID] = append(tasks[:i], tasks[i+1:]...)
			return deleted, nil
		}
	}

	return GroupTask{}, ErrGroupTaskNotFound
}

func (s *Storage) NotificationRecipients(groupID int, assigneeUserID *int64) ([]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.groups[groupID]; !ok {
		return nil, ErrGroupNotFound
	}

	if assigneeUserID != nil {
		return []int64{*assigneeUserID}, nil
	}

	members := s.groupMembers[groupID]
	result := make([]int64, 0, len(members))
	for memberID := range members {
		result = append(result, memberID)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i] < result[j]
	})

	return result, nil
}
