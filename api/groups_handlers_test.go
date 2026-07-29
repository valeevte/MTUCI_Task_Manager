package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"mtuci-task-manager/bot"
)

func callHandler(t *testing.T, handler http.HandlerFunc, user *TelegramUser, method, path string, body []byte, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	if body == nil {
		body = []byte{}
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), userContextKey, user)
	req = req.WithContext(ctx)
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}

	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func decodeBody[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

func TestGroupTaskNotificationRouting(t *testing.T) {
	storage := bot.NewStorage()
	storage.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	storage.UpsertVerifiedUser(2, "member_user", "Member", "")

	srv := NewServer(storage, "token")

	notified := make([]int64, 0)
	srv.SetNotifier(func(userID int64, message string) error {
		notified = append(notified, userID)
		return nil
	})

	owner := &TelegramUser{ID: 1, Username: "owner_user", FirstName: "Owner"}

	rr := callHandler(
		t,
		srv.handleCreateGroup,
		owner,
		http.MethodPost,
		"/api/groups",
		[]byte(`{"name":"Team"}`),
		nil,
	)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create group status=%d body=%s", rr.Code, rr.Body.String())
	}
	group := decodeBody[bot.Group](t, rr)

	rr = callHandler(
		t,
		srv.handleAddGroupMember,
		owner,
		http.MethodPost,
		"/api/groups/1/members",
		[]byte(`{"username":"@member_user"}`),
		map[string]string{"groupId": strconv.Itoa(group.ID)},
	)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add member status=%d body=%s", rr.Code, rr.Body.String())
	}

	notified = notified[:0]
	rr = callHandler(
		t,
		srv.handleCreateGroupTask,
		owner,
		http.MethodPost,
		"/api/groups/1/tasks",
		[]byte(`{"title":"Common","description":"","assignee_username":""}`),
		map[string]string{"groupId": strconv.Itoa(group.ID)},
	)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create common task status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(notified) != 2 || notified[0] != 1 || notified[1] != 2 {
		t.Fatalf("expected notifications [1 2] for common task, got %v", notified)
	}

	notified = notified[:0]
	rr = callHandler(
		t,
		srv.handleCreateGroupTask,
		owner,
		http.MethodPost,
		"/api/groups/1/tasks",
		[]byte(`{"title":"Assigned","description":"","assignee_username":"@member_user"}`),
		map[string]string{"groupId": strconv.Itoa(group.ID)},
	)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create assigned task status=%d body=%s", rr.Code, rr.Body.String())
	}
	assigned := decodeBody[bot.GroupTask](t, rr)
	if len(notified) != 1 || notified[0] != 2 {
		t.Fatalf("expected notification [2] for assigned task, got %v", notified)
	}

	notified = notified[:0]
	rr = callHandler(
		t,
		srv.handleUpdateGroupTaskStatus,
		owner,
		http.MethodPatch,
		"/api/groups/1/tasks/1/status",
		[]byte(`{"status":"done"}`),
		map[string]string{
			"groupId": strconv.Itoa(group.ID),
			"taskId":  strconv.Itoa(assigned.ID),
		},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("update status status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(notified) != 1 || notified[0] != 2 {
		t.Fatalf("expected notification [2] on status update for assigned task, got %v", notified)
	}

	notified = notified[:0]
	rr = callHandler(
		t,
		srv.handleSetGroupTaskAssignee,
		owner,
		http.MethodPatch,
		"/api/groups/1/tasks/1/assignee",
		[]byte(`{"assignee_username":""}`),
		map[string]string{
			"groupId": strconv.Itoa(group.ID),
			"taskId":  strconv.Itoa(assigned.ID),
		},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear assignee status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(notified) != 2 || notified[0] != 1 || notified[1] != 2 {
		t.Fatalf("expected notifications [1 2] after clear assignee, got %v", notified)
	}

	// При прямом переназначении уведомление получают и новый, и прежний
	// ответственные: иначе прежний пользователь не узнает, что задача снята.
	storage.UpsertVerifiedUser(3, "third_user", "Third", "")
	rr = callHandler(
		t,
		srv.handleAddGroupMember,
		owner,
		http.MethodPost,
		"/api/groups/1/members",
		[]byte(`{"username":"@third_user"}`),
		map[string]string{"groupId": strconv.Itoa(group.ID)},
	)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add third member status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = callHandler(
		t,
		srv.handleSetGroupTaskAssignee,
		owner,
		http.MethodPatch,
		"/api/groups/1/tasks/1/assignee",
		[]byte(`{"assignee_username":"@member_user"}`),
		map[string]string{
			"groupId": strconv.Itoa(group.ID),
			"taskId":  strconv.Itoa(assigned.ID),
		},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("assign member status=%d body=%s", rr.Code, rr.Body.String())
	}

	notified = notified[:0]
	rr = callHandler(
		t,
		srv.handleSetGroupTaskAssignee,
		owner,
		http.MethodPatch,
		"/api/groups/1/tasks/1/assignee",
		[]byte(`{"assignee_username":"@third_user"}`),
		map[string]string{
			"groupId": strconv.Itoa(group.ID),
			"taskId":  strconv.Itoa(assigned.ID),
		},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("reassign status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(notified) != 2 || notified[0] != 3 || notified[1] != 2 {
		t.Fatalf("expected notifications [3 2] after reassignment, got %v", notified)
	}
}

func TestGroupHandlersErrors(t *testing.T) {
	storage := bot.NewStorage()
	storage.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	storage.UpsertVerifiedUser(2, "other_user", "Other", "")

	srv := NewServer(storage, "token")

	owner := &TelegramUser{ID: 1, Username: "owner_user", FirstName: "Owner"}
	other := &TelegramUser{ID: 2, Username: "other_user", FirstName: "Other"}

	rr := callHandler(
		t,
		srv.handleCreateGroup,
		owner,
		http.MethodPost,
		"/api/groups",
		[]byte(`{"name":"Team"}`),
		nil,
	)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create group status=%d body=%s", rr.Code, rr.Body.String())
	}
	group := decodeBody[bot.Group](t, rr)

	rr = callHandler(
		t,
		srv.handleAddGroupMember,
		owner,
		http.MethodPost,
		"/api/groups/1/members",
		[]byte(`{"username":"@unknown_user"}`),
		map[string]string{"groupId": strconv.Itoa(group.ID)},
	)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown username, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr = callHandler(
		t,
		srv.handleDeleteGroup,
		other,
		http.MethodDelete,
		"/api/groups/1",
		nil,
		map[string]string{"groupId": strconv.Itoa(group.ID)},
	)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on delete by non-creator, got %d body=%s", rr.Code, rr.Body.String())
	}
}
