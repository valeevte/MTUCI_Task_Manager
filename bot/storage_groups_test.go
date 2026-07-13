package bot

import (
	"errors"
	"testing"
)

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

func TestTaskAssignmentAndRecipients(t *testing.T) {
	s := NewStorage()

	s.UpsertVerifiedUser(1, "owner_user", "Owner", "")
	s.UpsertVerifiedUser(2, "member_user", "Member", "")
	s.UpsertVerifiedUser(3, "third_user", "Third", "")

	group, err := s.CreateGroup(1, "QA")
	if err != nil {
		t.Fatalf("CreateGroup error: %v", err)
	}

	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@member_user"); err != nil {
		t.Fatalf("add member error: %v", err)
	}
	if _, err := s.AddGroupMemberByUsername(1, group.ID, "@third_user"); err != nil {
		t.Fatalf("add member error: %v", err)
	}

	commonTask, err := s.CreateGroupTask(1, group.ID, "Common", "", "")
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
	if len(recipients) != 3 {
		t.Fatalf("expected 3 recipients for common task, got %d", len(recipients))
	}

	assignedTask, err := s.CreateGroupTask(1, group.ID, "Assigned", "", "@member_user")
	if err != nil {
		t.Fatalf("CreateGroupTask assigned error: %v", err)
	}
	if assignedTask.AssigneeUserID == nil || *assignedTask.AssigneeUserID != 2 {
		t.Fatalf("expected assignee id=2")
	}

	recipients, err = s.NotificationRecipients(group.ID, assignedTask.AssigneeUserID)
	if err != nil {
		t.Fatalf("NotificationRecipients assigned error: %v", err)
	}
	if len(recipients) != 1 || recipients[0] != 2 {
		t.Fatalf("expected only assignee recipient=2, got %v", recipients)
	}

	if _, err := s.GetGroupTasks(2, group.ID); err != nil {
		t.Fatalf("member should see all tasks, got error: %v", err)
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
