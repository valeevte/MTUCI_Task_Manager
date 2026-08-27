package bot

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUpdateTaskEditsOnlyOwnedTask(t *testing.T) {
	s := NewStorage()
	original := s.AddTask(1, "Before", "Description")
	title := " After "

	time.Sleep(time.Millisecond)
	updated, changed, err := s.UpdateTask(1, original.ID, &title, nil)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if updated.Title != "After" || updated.Description != "Description" {
		t.Fatalf("updated task = %#v", updated)
	}
	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("updated_at = %v, want after %v", updated.UpdatedAt, original.UpdatedAt)
	}

	if _, _, err := s.UpdateTask(2, original.ID, &title, nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-user edit error = %v", err)
	}
}

func TestUpdateTaskClearsAndTrimsDescription(t *testing.T) {
	s := NewStorage()
	original := s.AddTask(1, "Title", "Description")
	description := "   "

	updated, changed, err := s.UpdateTask(1, original.ID, nil, &description)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if updated.Title != "Title" || updated.Description != "" {
		t.Fatalf("updated task = %#v", updated)
	}
}

func TestUpdateTaskRejectsInvalidText(t *testing.T) {
	s := NewStorage()
	original := s.AddTask(1, "Title", "Description")

	tooLongTitle := strings.Repeat("я", MaxTaskTitleLength+1)
	if _, _, err := s.UpdateTask(1, original.ID, &tooLongTitle, nil); !errors.Is(err, ErrTaskTitleTooLong) {
		t.Fatalf("long title error = %v", err)
	}

	tooLongDescription := strings.Repeat("я", MaxTaskDescriptionLength+1)
	if _, _, err := s.UpdateTask(1, original.ID, nil, &tooLongDescription); !errors.Is(err, ErrTaskDescriptionTooLong) {
		t.Fatalf("long description error = %v", err)
	}

	emptyTitle := " \t "
	if _, _, err := s.UpdateTask(1, original.ID, &emptyTitle, nil); !errors.Is(err, ErrInvalidTaskTitle) {
		t.Fatalf("empty title error = %v", err)
	}

	stored, ok := s.GetTask(1, original.ID)
	if !ok || stored.Title != "Title" || stored.Description != "Description" {
		t.Fatalf("invalid update modified task = %#v, found=%v", stored, ok)
	}
}

func TestUpdateTaskRejectsEmptyPatchAndMissingTask(t *testing.T) {
	s := NewStorage()

	if _, _, err := s.UpdateTask(1, 1, nil, nil); !errors.Is(err, ErrEmptyTaskPatch) {
		t.Fatalf("empty patch error = %v", err)
	}

	title := "New title"
	if _, _, err := s.UpdateTask(1, 1, &title, nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task error = %v", err)
	}
}

func TestUpdateTaskNoOpKeepsUpdatedAt(t *testing.T) {
	s := NewStorage()
	original := s.AddTask(1, "Title", "Description")
	title := " Title "
	description := " Description "

	updated, changed, err := s.UpdateTask(1, original.ID, &title, &description)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !updated.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("no-op changed updated_at: got %v, want %v", updated.UpdatedAt, original.UpdatedAt)
	}
}

func TestTaskTimestampsChangeOnlyOnMutations(t *testing.T) {
	s := NewStorage()
	original := s.AddTask(1, "Title", "Description")
	if original.CreatedAt.IsZero() || original.UpdatedAt.IsZero() {
		t.Fatalf("created task has zero timestamp: %#v", original)
	}
	if !original.CreatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("created_at = %v, updated_at = %v", original.CreatedAt, original.UpdatedAt)
	}

	if !s.UpdateStatus(1, original.ID, StatusNew) {
		t.Fatal("no-op status update did not find task")
	}
	unchanged, ok := s.GetTask(1, original.ID)
	if !ok {
		t.Fatal("task not found after no-op status update")
	}
	if !unchanged.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("no-op status changed updated_at: got %v, want %v", unchanged.UpdatedAt, original.UpdatedAt)
	}

	time.Sleep(time.Millisecond)
	if !s.UpdateStatus(1, original.ID, StatusDone) {
		t.Fatal("status update did not find task")
	}
	updated, ok := s.GetTask(1, original.ID)
	if !ok {
		t.Fatal("task not found after status update")
	}
	if updated.Status != StatusDone {
		t.Fatalf("status = %q, want %q", updated.Status, StatusDone)
	}
	if !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("updated_at = %v, want after %v", updated.UpdatedAt, original.UpdatedAt)
	}
}
