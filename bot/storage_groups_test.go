package bot

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newGroupAuthorizationFixture(t *testing.T) (*Storage, Group) {
	t.Helper()

	s := NewStorage()
	for _, user := range []struct {
		id       int64
		username string
	}{
		{1, "admin_user"},
		{2, "author_user"},
		{3, "assignee_user"},
		{4, "other_user"},
		{5, "outside_user"},
	} {
		s.UpsertVerifiedUser(user.id, user.username, user.username, "")
	}

	group, err := s.CreateGroup(1, "Team")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}
	for _, username := range []string{"@author_user", "@assignee_user", "@other_user"} {
		if _, err := s.AddGroupMemberByUsername(1, group.ID, username); err != nil {
			t.Fatalf("AddGroupMemberByUsername(%q) error: %v", username, err)
		}
	}

	return s, group
}

func int64Pointer(value int64) *int64 {
	return &value
}

func TestGroupLifecycleAndMembership(t *testing.T) {
	s := NewStorage()

	s.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	s.UpsertVerifiedUser(2, "member_user", "Member", "")

	group, err := s.CreateGroup(1, "Dev Team")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}

	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@member_user"); err != nil {
		t.Fatalf("AddGroupMemberByUsername error: %v", err)
	}

	members, err := s.GetGroupMembers(1, group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers error: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	if err := s.RemoveGroupMember(1, group.ID, 2); err != nil {
		t.Fatalf("RemoveGroupMember error: %v", err)
	}

	members, err = s.GetGroupMembers(1, group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}
}

func TestGroupMembershipExposesExplicitRoles(t *testing.T) {
	s := NewStorage()
	s.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	s.UpsertVerifiedUser(2, "zebra_user", "Zebra", "")
	s.UpsertVerifiedUser(3, "alpha_user", "Alpha", "")

	group, err := s.CreateGroup(1, "Team")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}
	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@zebra_user"); err != nil {
		t.Fatalf("add zebra member error: %v", err)
	}
	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@alpha_user"); err != nil {
		t.Fatalf("add alpha member error: %v", err)
	}

	members, err := s.GetGroupMembers(1, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}
	if members[0].UserID != 1 || members[0].Role != GroupRoleAdmin {
		t.Fatalf("creator = %+v, want admin first", members[0])
	}
	if members[1].UserID != 3 || members[1].Role != GroupRoleUser {
		t.Fatalf("second member = %+v, want alpha user", members[1])
	}
	if members[2].UserID != 2 || members[2].Role != GroupRoleUser {
		t.Fatalf("third member = %+v, want zebra user", members[2])
	}
}

func TestTaskAssignmentAndRecipients(t *testing.T) {
	s, group := newGroupAuthorizationFixture(t)

	commonTask, err := s.CreateGroupTask(2, group.ID, "Common", "", nil)
	if err != nil {
		t.Fatalf("CreateGroupTask common error: %v", err)
	}
	if commonTask.AssigneeUserID != nil {
		t.Fatalf("expected common task without assignee")
	}

	recipients, err := s.NotificationRecipients(group.ID, commonTask.AssigneeUserID)
	if err != nil {
		t.Fatalf("NotificationRecipients common error: %v", err)
	}
	if len(recipients) != 4 {
		t.Fatalf("expected 4 recipients for common task, got %d", len(recipients))
	}

	assignedTask, err := s.CreateGroupTask(2, group.ID, "Assigned", "", int64Pointer(3))
	if err != nil {
		t.Fatalf("CreateGroupTask assigned error: %v", err)
	}
	if assignedTask.AssigneeUserID == nil || *assignedTask.AssigneeUserID != 3 {
		t.Fatalf("expected assignee id=3")
	}
	if assignedTask.AssigneeUsername != "@assignee_user" {
		t.Fatalf("assignee username = %q, want display-only @assignee_user", assignedTask.AssigneeUsername)
	}

	recipients, err = s.NotificationRecipients(group.ID, assignedTask.AssigneeUserID)
	if err != nil {
		t.Fatalf("NotificationRecipients assigned error: %v", err)
	}
	if len(recipients) != 1 || recipients[0] != 3 {
		t.Fatalf("expected only assignee recipient=3, got %v", recipients)
	}

	for _, actorID := range []int64{1, 2, 3, 4} {
		tasks, err := s.GetGroupTasks(actorID, group.ID)
		if err != nil {
			t.Fatalf("member %d should see all tasks, got error: %v", actorID, err)
		}
		if len(tasks) != 2 {
			t.Fatalf("member %d got %d tasks, want 2", actorID, len(tasks))
		}
	}
	if _, err := s.GetGroupTasks(5, group.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider list error = %v, want ErrForbidden", err)
	}
}

func TestGroupTaskCreationAssignmentPolicy(t *testing.T) {
	tests := []struct {
		name       string
		actorID    int64
		assigneeID *int64
		wantErr    error
	}{
		{"user creates common task", 2, nil, nil},
		{"user assigns self", 2, int64Pointer(2), nil},
		{"user assigns another user", 2, int64Pointer(3), nil},
		{"user cannot assign admin", 2, int64Pointer(1), ErrCannotAssignAdmin},
		{"admin assigns user", 1, int64Pointer(3), nil},
		{"admin assigns admin", 1, int64Pointer(1), nil},
		{"assignee must be positive", 1, int64Pointer(0), ErrAssigneeMustBeMember},
		{"assignee must be member", 1, int64Pointer(5), ErrAssigneeMustBeMember},
		{"outsider cannot create", 5, nil, ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, group := newGroupAuthorizationFixture(t)
			task, err := s.CreateGroupTask(tt.actorID, group.ID, "Task", "Description", tt.assigneeID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CreateGroupTask error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if task.AssigneeUserID == nil && tt.assigneeID != nil {
				t.Fatal("assigned task lost assignee")
			}
			if task.AssigneeUserID != nil && (tt.assigneeID == nil || *task.AssigneeUserID != *tt.assigneeID) {
				t.Fatalf("assignee = %v, want %v", task.AssigneeUserID, tt.assigneeID)
			}
		})
	}
}

func TestGroupTaskContentAuthorization(t *testing.T) {
	tests := []struct {
		name    string
		actorID int64
		allowed bool
		wantErr error
	}{
		{"admin edits any task", 1, true, nil},
		{"author edits own task", 2, true, nil},
		{"assignee cannot edit another author content", 3, false, ErrTaskEditForbidden},
		{"other user cannot edit", 4, false, ErrTaskEditForbidden},
		{"outsider cannot edit", 5, false, ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, group := newGroupAuthorizationFixture(t)
			task, err := s.CreateGroupTask(2, group.ID, "Original", "Description", int64Pointer(3))
			if err != nil {
				t.Fatal(err)
			}
			updatedTitle := "Updated"
			updated, changed, err := s.UpdateGroupTask(tt.actorID, group.ID, task.ID, &updatedTitle, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateGroupTask error = %v, want %v", err, tt.wantErr)
			}
			if tt.allowed {
				if !changed || updated.Title != updatedTitle {
					t.Fatalf("UpdateGroupTask = (%+v, %v), want changed title", updated, changed)
				}
			} else if changed {
				t.Fatal("forbidden edit reported a change")
			}
		})
	}
}

func TestUpdateGroupTaskValidationAndNoOp(t *testing.T) {
	s, group := newGroupAuthorizationFixture(t)
	task, err := s.CreateGroupTask(2, group.ID, "Original", "Description", int64Pointer(3))
	if err != nil {
		t.Fatal(err)
	}

	if _, changed, err := s.UpdateGroupTask(2, group.ID, task.ID, nil, nil); !errors.Is(err, ErrEmptyTaskPatch) || changed {
		t.Fatalf("empty patch = (changed %v, error %v), want false, ErrEmptyTaskPatch", changed, err)
	}
	blankTitle := "   "
	if _, changed, err := s.UpdateGroupTask(2, group.ID, task.ID, &blankTitle, nil); !errors.Is(err, ErrInvalidTaskTitle) || changed {
		t.Fatalf("blank title = (changed %v, error %v), want false, ErrInvalidTaskTitle", changed, err)
	}
	longTitle := strings.Repeat("я", MaxTaskTitleLength+1)
	if _, _, err := s.UpdateGroupTask(2, group.ID, task.ID, &longTitle, nil); !errors.Is(err, ErrTaskTitleTooLong) {
		t.Fatalf("long title error = %v, want ErrTaskTitleTooLong", err)
	}
	longDescription := strings.Repeat("я", MaxTaskDescriptionLength+1)
	if _, _, err := s.UpdateGroupTask(2, group.ID, task.ID, nil, &longDescription); !errors.Is(err, ErrTaskDescriptionTooLong) {
		t.Fatalf("long description error = %v, want ErrTaskDescriptionTooLong", err)
	}

	sameTitle := "  Original  "
	sameDescription := "  Description  "
	unchanged, changed, err := s.UpdateGroupTask(2, group.ID, task.ID, &sameTitle, &sameDescription)
	if err != nil || changed {
		t.Fatalf("normalized no-op = (changed %v, error %v), want false, nil", changed, err)
	}
	if !unchanged.UpdatedAt.Equal(task.UpdatedAt) {
		t.Fatalf("no-op UpdatedAt = %v, want %v", unchanged.UpdatedAt, task.UpdatedAt)
	}

	time.Sleep(time.Millisecond)
	newDescription := "  Revised  "
	updated, changed, err := s.UpdateGroupTask(1, group.ID, task.ID, nil, &newDescription)
	if err != nil || !changed {
		t.Fatalf("real edit = (changed %v, error %v), want true, nil", changed, err)
	}
	if updated.Description != "Revised" || !updated.UpdatedAt.After(task.UpdatedAt) {
		t.Fatalf("updated task = %+v, want normalized description and newer timestamp", updated)
	}
}

func TestUserStatusTransitionMatrix(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		to          string
		wantChanged bool
		wantErr     error
	}{
		{"new stays new", StatusNew, StatusNew, false, nil},
		{"new starts", StatusNew, StatusInProgress, true, nil},
		{"new cannot skip to done", StatusNew, StatusDone, false, ErrStatusTransitionForbidden},
		{"in progress cannot return to new", StatusInProgress, StatusNew, false, ErrStatusTransitionForbidden},
		{"in progress stays in progress", StatusInProgress, StatusInProgress, false, nil},
		{"in progress completes", StatusInProgress, StatusDone, true, nil},
		{"done cannot return to new", StatusDone, StatusNew, false, ErrStatusTransitionForbidden},
		{"done reopens", StatusDone, StatusInProgress, true, nil},
		{"done stays done", StatusDone, StatusDone, false, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, group := newGroupAuthorizationFixture(t)
			task, err := s.CreateGroupTask(2, group.ID, "Task", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.from != StatusNew {
				task, _, err = s.UpdateGroupTaskStatus(1, group.ID, task.ID, tt.from)
				if err != nil {
					t.Fatalf("admin setup status error: %v", err)
				}
			}

			before := task.UpdatedAt
			time.Sleep(time.Millisecond)
			updated, changed, err := s.UpdateGroupTaskStatus(4, group.ID, task.ID, tt.to)
			if !errors.Is(err, tt.wantErr) || changed != tt.wantChanged {
				t.Fatalf("UpdateGroupTaskStatus = (changed %v, error %v), want (%v, %v)", changed, err, tt.wantChanged, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if updated.Status != tt.to {
				t.Fatalf("status = %q, want %q", updated.Status, tt.to)
			}
			if tt.wantChanged && !updated.UpdatedAt.After(before) {
				t.Fatalf("changed UpdatedAt = %v, want after %v", updated.UpdatedAt, before)
			}
			if !tt.wantChanged && !updated.UpdatedAt.Equal(before) {
				t.Fatalf("no-op UpdatedAt = %v, want %v", updated.UpdatedAt, before)
			}
		})
	}
}

func TestGroupTaskStatusAuthorization(t *testing.T) {
	t.Run("admin selects every valid status", func(t *testing.T) {
		for _, status := range []string{StatusNew, StatusInProgress, StatusDone} {
			s, group := newGroupAuthorizationFixture(t)
			task, err := s.CreateGroupTask(2, group.ID, "Task", "", int64Pointer(3))
			if err != nil {
				t.Fatal(err)
			}
			updated, _, err := s.UpdateGroupTaskStatus(1, group.ID, task.ID, status)
			if err != nil || updated.Status != status {
				t.Fatalf("admin status %q = (%q, %v)", status, updated.Status, err)
			}
		}
	})

	tests := []struct {
		name     string
		assignee *int64
		actorID  int64
		wantErr  error
	}{
		{"common task allows any user member", nil, 4, nil},
		{"assigned task allows assignee", int64Pointer(3), 3, nil},
		{"assigned task rejects author", int64Pointer(3), 2, ErrStatusChangeForbidden},
		{"assigned task rejects unrelated user", int64Pointer(3), 4, ErrStatusChangeForbidden},
		{"outsider is rejected before task lookup", int64Pointer(3), 5, ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, group := newGroupAuthorizationFixture(t)
			task, err := s.CreateGroupTask(2, group.ID, "Task", "", tt.assignee)
			if err != nil {
				t.Fatal(err)
			}
			_, changed, err := s.UpdateGroupTaskStatus(tt.actorID, group.ID, task.ID, StatusInProgress)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateGroupTaskStatus error = %v, want %v", err, tt.wantErr)
			}
			if (tt.wantErr == nil) != changed {
				t.Fatalf("changed = %v, want %v", changed, tt.wantErr == nil)
			}
		})
	}

	s, group := newGroupAuthorizationFixture(t)
	task, err := s.CreateGroupTask(2, group.ID, "Task", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdateGroupTaskStatus(2, group.ID, task.ID, "invalid"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidStatus", err)
	}
}

func TestUpdateGroupNameAuthorizationValidationAndNoOp(t *testing.T) {
	s, group := newGroupAuthorizationFixture(t)

	if _, changed, err := s.UpdateGroupName(2, group.ID, "Renamed"); !errors.Is(err, ErrAdminOnly) || changed {
		t.Fatalf("user rename = (changed %v, error %v), want false, ErrAdminOnly", changed, err)
	}
	if _, changed, err := s.UpdateGroupName(5, group.ID, "Renamed"); !errors.Is(err, ErrForbidden) || changed {
		t.Fatalf("outsider rename = (changed %v, error %v), want false, ErrForbidden", changed, err)
	}
	if _, _, err := s.UpdateGroupName(1, group.ID, "   "); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("blank rename error = %v, want ErrInvalidGroupName", err)
	}
	if _, _, err := s.UpdateGroupName(1, group.ID, strings.Repeat("я", MaxGroupNameLength+1)); !errors.Is(err, ErrGroupNameTooLong) {
		t.Fatalf("long rename error = %v, want ErrGroupNameTooLong", err)
	}

	unchanged, changed, err := s.UpdateGroupName(1, group.ID, "  Team  ")
	if err != nil || changed || unchanged != group {
		t.Fatalf("normalized no-op = (%+v, %v, %v), want original, false, nil", unchanged, changed, err)
	}
	updated, changed, err := s.UpdateGroupName(1, group.ID, "  New Team  ")
	if err != nil || !changed {
		t.Fatalf("admin rename = (changed %v, error %v), want true, nil", changed, err)
	}
	if updated.Name != "New Team" || !updated.CreatedAt.Equal(group.CreatedAt) {
		t.Fatalf("updated group = %+v, want normalized name and preserved CreatedAt", updated)
	}
}

func TestSetGroupTaskAssigneeAdminOnlyAndIdempotent(t *testing.T) {
	s, group := newGroupAuthorizationFixture(t)
	task, err := s.CreateGroupTask(2, group.ID, "Task", "", int64Pointer(3))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name    string
		actorID int64
		wantErr error
	}{
		{"author cannot reassign", 2, ErrAdminOnly},
		{"assignee cannot reassign", 3, ErrAdminOnly},
		{"other user cannot reassign", 4, ErrAdminOnly},
		{"outsider cannot reassign", 5, ErrForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, changed, err := s.SetGroupTaskAssignee(tt.actorID, group.ID, task.ID, nil); !errors.Is(err, tt.wantErr) || changed {
				t.Fatalf("SetGroupTaskAssignee = (changed %v, error %v), want false, %v", changed, err, tt.wantErr)
			}
		})
	}

	unchanged, changed, err := s.SetGroupTaskAssignee(1, group.ID, task.ID, int64Pointer(3))
	if err != nil || changed || !unchanged.UpdatedAt.Equal(task.UpdatedAt) {
		t.Fatalf("same assignee = (%+v, %v, %v), want unchanged timestamp", unchanged, changed, err)
	}
	if _, _, err := s.SetGroupTaskAssignee(1, group.ID, task.ID, int64Pointer(5)); !errors.Is(err, ErrAssigneeMustBeMember) {
		t.Fatalf("outside assignee error = %v, want ErrAssigneeMustBeMember", err)
	}

	time.Sleep(time.Millisecond)
	common, changed, err := s.SetGroupTaskAssignee(1, group.ID, task.ID, nil)
	if err != nil || !changed || common.AssigneeUserID != nil || common.AssigneeUsername != "" {
		t.Fatalf("remove assignee = (%+v, %v, %v), want common changed task", common, changed, err)
	}
	if !common.UpdatedAt.After(task.UpdatedAt) {
		t.Fatalf("reassigned UpdatedAt = %v, want after %v", common.UpdatedAt, task.UpdatedAt)
	}

	adminAssigned, changed, err := s.SetGroupTaskAssignee(1, group.ID, task.ID, int64Pointer(1))
	if err != nil || !changed || adminAssigned.AssigneeUserID == nil || *adminAssigned.AssigneeUserID != 1 {
		t.Fatalf("admin assigns admin = (%+v, %v, %v)", adminAssigned, changed, err)
	}
}

func TestDeleteGroupTaskAdminOnly(t *testing.T) {
	for _, tt := range []struct {
		name    string
		actorID int64
		wantErr error
	}{
		{"admin deletes", 1, nil},
		{"author cannot delete", 2, ErrAdminOnly},
		{"assignee cannot delete", 3, ErrAdminOnly},
		{"other user cannot delete", 4, ErrAdminOnly},
		{"outsider cannot delete", 5, ErrForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, group := newGroupAuthorizationFixture(t)
			task, err := s.CreateGroupTask(2, group.ID, "Task", "", int64Pointer(3))
			if err != nil {
				t.Fatal(err)
			}
			deleted, err := s.DeleteGroupTask(tt.actorID, group.ID, task.ID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DeleteGroupTask error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && deleted.ID != task.ID {
				t.Fatalf("deleted task = %+v, want ID %d", deleted, task.ID)
			}
		})
	}
}

func TestRemovingMemberMakesAssignedTasksCommon(t *testing.T) {
	s, group := newGroupAuthorizationFixture(t)
	task, err := s.CreateGroupTask(2, group.ID, "Task", "", int64Pointer(3))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if err := s.RemoveGroupMember(1, group.ID, 3); err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetGroupTask(1, group.ID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AssigneeUserID != nil || updated.AssigneeUsername != "" {
		t.Fatalf("task after member removal = %+v, want common", updated)
	}
	if !updated.UpdatedAt.After(task.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v, want after %v", updated.UpdatedAt, task.UpdatedAt)
	}
}

func TestValidationAndPermissions(t *testing.T) {
	s := NewStorage()

	s.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	s.UpsertVerifiedUser(2, "member_user", "Member", "")

	group, err := s.CreateGroup(1, "Ops")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}

	if _, err := s.AddGroupMemberByUsername(1, group.ID, "member_user"); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("expected ErrInvalidUsername, got %v", err)
	}

	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@unknown_user"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	if err := s.DeleteGroup(2, group.ID); !errors.Is(err, ErrOnlyCreatorCanDelete) {
		t.Fatalf("expected ErrOnlyCreatorCanDelete, got %v", err)
	}

	if err := s.RemoveGroupMember(1, group.ID, 1); !errors.Is(err, ErrCreatorCannotBeRemoved) {
		t.Fatalf("expected ErrCreatorCannotBeRemoved, got %v", err)
	}
}

func TestOnlyCreatorCanManageOtherMembers(t *testing.T) {
	s := NewStorage()
	s.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	s.UpsertVerifiedUser(2, "member_user", "Member", "")
	s.UpsertVerifiedUser(3, "third_user", "Third", "")

	group, err := s.CreateGroup(1, "Team")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}
	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@member_user"); err != nil {
		t.Fatalf("add member error: %v", err)
	}
	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@third_user"); err != nil {
		t.Fatalf("admin should add another member: %v", err)
	}

	if _, err := s.AddGroupMemberByUsername(2, group.ID, "@third_user"); !errors.Is(err, ErrOnlyCreatorCanManage) {
		t.Fatalf("user should not add another member, got %v", err)
	}
	if err := s.RemoveGroupMember(2, group.ID, 3); !errors.Is(err, ErrOnlyCreatorCanManage) {
		t.Fatalf("user should not remove another member, got %v", err)
	}
	if err := s.RemoveGroupMember(1, group.ID, 3); err != nil {
		t.Fatalf("admin should remove another member: %v", err)
	}
	if err := s.RemoveGroupMember(1, group.ID, 1); !errors.Is(err, ErrCreatorCannotBeRemoved) {
		t.Fatalf("admin cannot be removed, got %v", err)
	}
	if err := s.RemoveGroupMember(2, group.ID, 2); err != nil {
		t.Fatalf("member should be able to leave: %v", err)
	}
}

func TestUsernameTransferKeepsIndexConsistent(t *testing.T) {
	s := NewStorage()
	s.UpsertVerifiedUser(1, "shared_name", "First", "")
	s.UpsertVerifiedUser(2, "shared_name", "Second", "")
	s.UpsertVerifiedUser(1, "first_new", "First", "")

	group, err := s.CreateGroup(1, "Team")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}
	member, err := s.AddGroupMemberByUsername(1, group.ID, "@shared_name")
	if err != nil {
		t.Fatalf("username should still point to second user: %v", err)
	}
	if member.UserID != 2 {
		t.Fatalf("expected transferred username to resolve to user 2, got %d", member.UserID)
	}
}

func TestTaskAndGroupLengthValidation(t *testing.T) {
	s := NewStorage()

	if _, _, err := ValidateTaskText(strings.Repeat("я", MaxTaskTitleLength+1), ""); !errors.Is(err, ErrTaskTitleTooLong) {
		t.Fatalf("expected ErrTaskTitleTooLong, got %v", err)
	}
	if _, _, err := ValidateTaskText("Title", strings.Repeat("я", MaxTaskDescriptionLength+1)); !errors.Is(err, ErrTaskDescriptionTooLong) {
		t.Fatalf("expected ErrTaskDescriptionTooLong, got %v", err)
	}
	if _, err := s.CreateGroup(1, strings.Repeat("я", MaxGroupNameLength+1)); !errors.Is(err, ErrGroupNameTooLong) {
		t.Fatalf("expected ErrGroupNameTooLong, got %v", err)
	}
}
