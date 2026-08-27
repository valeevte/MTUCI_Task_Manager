# Group Roles and Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add explicit Admin/User group roles, enforce the approved permissions for common and assigned tasks, and add editing for personal tasks, group tasks, and group names.

**Architecture:** Keep the current in-memory architecture and centralize every authorization decision in `bot.Storage`. HTTP handlers translate JSON and domain errors only; the Mini App renders controls from server-provided member roles but never becomes an authorization boundary. Preserve existing notification behavior except for the two approved edit-related notifications.

**Tech Stack:** Go 1.23+, `net/http`, `encoding/json`, `sync.RWMutex`, vanilla JavaScript, HTML, CSS, Telegram Mini App SDK.

## Global Constraints

- Group roles are exactly `admin` and `user`.
- The creator is the only immutable Admin in this iteration; role-changing APIs are out of scope.
- A group task is common or assigned to exactly one current group member.
- User defaults assignment to self and may choose common or another User, never Admin.
- Admin defaults assignment to self and may choose any member or common.
- Only Admin can reassign or delete a group task.
- Admin edits any task; User edits title/description only on tasks they created.
- Any User may advance a common task; only its assignee may advance an assigned task.
- User status transitions are only `new -> in_progress`, `in_progress -> done`, and `done -> in_progress`; Admin may choose any status.
- Personal-task editing remains owner-only.
- Do not add dependencies, persistent storage, extra permission flags, role promotion, deadlines, priorities, or notification infrastructure.
- Do not commit unless the user explicitly requests it.

---

## File Map

- `bot/storage.go`: domain types, role-aware membership, task authorization, editing, idempotent mutations.
- `bot/storage_groups_test.go`: role, assignment, group editing, task editing, deletion, and transition tests.
- `bot/storage_tasks_test.go`: new focused personal-task editing and timestamp tests.
- `api/router.go`: register the three PATCH editing routes.
- `api/handlers.go`: decode tri-state assignee IDs, expose edit handlers, map new domain errors, and send approved notifications.
- `api/groups_handlers_test.go`: group API permissions, ID-based assignment, editing, and notifications.
- `api/tasks_handlers_test.go`: personal edit route behavior.
- `web/index.html`: reusable edit forms and member selectors.
- `web/app.js`: role-aware rendering, create/edit flows, ID-based assignment, allowed status controls.
- `web/style.css`: small styles needed by selectors and edit controls.
- `README.md`: update the feature and permission descriptions.

---

### Task 1: Explicit Group Roles and Membership

**Files:**
- Modify: `bot/storage.go`
- Modify: `bot/storage_groups_test.go`

**Interfaces:**
- Produces: `type GroupRole string`, `GroupRoleAdmin`, `GroupRoleUser`.
- Produces: `GroupMember.Role GroupRole` serialized as `role`.
- Produces: `ensureGroupMemberLocked(groupID int, userID int64) (Group, GroupRole, error)`.
- Consumed by: all later role-aware task and HTTP work.

- [ ] **Step 1: Write failing role tests**

Add tests asserting that a creator is returned with `Role == GroupRoleAdmin`, an added member with `Role == GroupRoleUser`, Admin sorts first, User can leave, Admin cannot be removed, and only Admin can add/remove other members.

```go
func TestGroupMembershipExposesExplicitRoles(t *testing.T) {
    s := NewStorage()
    s.UpsertVerifiedUser(1, "owner_user", "Owner", "")
    s.UpsertVerifiedUser(2, "member_user", "Member", "")
    group, _ := s.CreateGroup(1, "Team")
    _, _ = s.AddGroupMemberByUsername(1, group.ID, "@member_user")

    members, err := s.GetGroupMembers(1, group.ID)
    if err != nil { t.Fatal(err) }
    if members[0].UserID != 1 || members[0].Role != GroupRoleAdmin {
        t.Fatalf("creator role = %q", members[0].Role)
    }
    if members[1].UserID != 2 || members[1].Role != GroupRoleUser {
        t.Fatalf("member role = %q", members[1].Role)
    }
}
```

- [ ] **Step 2: Run the focused tests and observe failure**

Run: `GOCACHE=/tmp/mtuci-role-red go test ./bot -run 'TestGroupMembershipExposesExplicitRoles|TestOnlyCreatorCanManageOtherMembers' -count=1`

Expected: compilation fails because `GroupRole`, `Role`, or the new membership representation is absent.

- [ ] **Step 3: Implement explicit roles**

In `bot/storage.go`:

```go
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
```

Change `groupMembers` to `map[int]map[int64]GroupRole`; initialize the creator with `GroupRoleAdmin` and added members with `GroupRoleUser`. Change `memberForResponse` and fallback member construction to read the stored role. Sort by role (`admin` first), then username and ID. Change `ensureGroupMemberLocked` to return both group and role, and update every caller without changing behavior yet.

- [ ] **Step 4: Run role and package tests**

Run: `GOCACHE=/tmp/mtuci-role-green go test ./bot -count=1`

Expected: PASS.

- [ ] **Step 5: Review the focused diff without committing**

Run: `git diff --check && git diff -- bot/storage.go bot/storage_groups_test.go`

Expected: no whitespace errors; no use of `IsCreator` remains in Go domain code.

---

### Task 2: Personal Task Editing and Updated Timestamps

**Files:**
- Modify: `bot/storage.go`
- Create: `bot/storage_tasks_test.go`

**Interfaces:**
- Produces: `Task.UpdatedAt time.Time` serialized as `updated_at`.
- Produces: `UpdateTask(userID int64, taskID int, title, description *string) (Task, bool, error)`.
- Produces: `ErrTaskNotFound` and `ErrEmptyTaskPatch`.
- Consumed by: personal PATCH handler in Task 5.

- [ ] **Step 1: Write failing storage tests**

Cover title-only edit, clearing description, trimming, validation limits, missing task, empty patch, owner isolation, timestamp update, idempotent edit, and status timestamp changes.

```go
func TestUpdateTaskEditsOnlyOwnedTask(t *testing.T) {
    s := NewStorage()
    original := s.AddTask(1, "Before", "Description")
    title := " After "
    updated, changed, err := s.UpdateTask(1, original.ID, &title, nil)
    if err != nil || !changed { t.Fatalf("changed=%v err=%v", changed, err) }
    if updated.Title != "After" || updated.Description != "Description" {
        t.Fatalf("updated task = %#v", updated)
    }
    if _, _, err := s.UpdateTask(2, original.ID, &title, nil); !errors.Is(err, ErrTaskNotFound) {
        t.Fatalf("cross-user edit error = %v", err)
    }
}
```

- [ ] **Step 2: Run focused tests and observe failure**

Run: `GOCACHE=/tmp/mtuci-personal-red go test ./bot -run 'TestUpdateTask|TestPersonalTask' -count=1`

Expected: compilation fails because `UpdateTask`, `UpdatedAt`, and new errors do not exist.

- [ ] **Step 3: Implement personal editing**

Add `UpdatedAt` to `Task`; set both timestamps from the same `now` in `AddTask`. Implement `UpdateTask` under one lock: find only inside `s.tasks[userID]`, apply optional fields through `ValidateTaskText`, return `ErrEmptyTaskPatch` when both pointers are nil, return `(current, false, nil)` for normalized no-op values, and update `UpdatedAt` only on a change. Update personal `UpdateStatus` to avoid timestamp changes on a no-op and set `UpdatedAt` on a real status change while retaining its existing boolean return contract.

- [ ] **Step 4: Run focused and bot tests**

Run: `GOCACHE=/tmp/mtuci-personal-green go test ./bot -count=1`

Expected: PASS.

- [ ] **Step 5: Review without committing**

Run: `git diff --check && git diff -- bot/storage.go bot/storage_tasks_test.go`

Expected: no unrelated changes.

---

### Task 3: Group Assignment, Authorization, Editing, and Status Rules

**Files:**
- Modify: `bot/storage.go`
- Modify: `bot/storage_groups_test.go`

**Interfaces:**
- Produces: `UpdateGroupName(actorID int64, groupID int, name string) (Group, bool, error)`.
- Produces: `CreateGroupTask(actorID int64, groupID int, title, description string, assigneeUserID *int64) (GroupTask, error)`.
- Produces: `UpdateGroupTask(actorID int64, groupID, taskID int, title, description *string) (GroupTask, bool, error)`.
- Produces: `UpdateGroupTaskStatus(actorID int64, groupID, taskID int, newStatus string) (GroupTask, bool, error)`.
- Produces: `SetGroupTaskAssignee(actorID int64, groupID, taskID int, assigneeUserID *int64) (GroupTask, bool, error)`.
- Produces domain errors mapped by Task 4: `ErrAdminOnly`, `ErrTaskEditForbidden`, `ErrStatusChangeForbidden`, `ErrStatusTransitionForbidden`, `ErrCannotAssignAdmin`, `ErrEmptyTaskPatch`.

- [ ] **Step 1: Replace username-assignment tests with the approved matrix**

Add table-driven cases for Admin, author, assignee, unrelated User, and outsider. Explicitly verify:

```go
tests := []struct {
    name    string
    actorID int64
    task    GroupTask
    allowed bool
}{
    {"admin edits any task", 1, userCreatedTask, true},
    {"author edits own task", 2, userCreatedTask, true},
    {"assignee cannot edit another author content", 3, userCreatedTask, false},
    {"other user cannot edit", 4, userCreatedTask, false},
}
```

Also test User may create a common task or assign any User but not Admin; Admin may assign anyone; only Admin reassigns/deletes; removing an assignee makes a task common; all members can list all tasks.

- [ ] **Step 2: Add status-transition tests**

Test Admin can select all three statuses. For User, test common task access for any member and assigned task access only for assignee. Test exactly the three allowed transitions, forbidden skips/back-to-new transitions, and idempotent same-status behavior with unchanged `UpdatedAt`.

- [ ] **Step 3: Add group and task editing tests**

Test Admin-only group rename, normalization, maximum length, and no-op behavior. Test group task edits by Admin and author; reject assignee-only, other member, and outsider. Verify title/description validation and timestamp behavior.

- [ ] **Step 4: Run focused tests and observe failure**

Run: `GOCACHE=/tmp/mtuci-group-domain-red go test ./bot -run 'TestGroupRole|TestGroupTask|TestUpdateGroup|TestUserStatus|TestAdmin' -count=1`

Expected: compilation failures and authorization assertion failures against the old signatures and member-only policy.

- [ ] **Step 5: Implement centralized role checks**

Add small locked helpers with these responsibilities:

```go
func canEditGroupTask(role GroupRole, actorID int64, task GroupTask) bool {
    return role == GroupRoleAdmin || task.CreatedBy == actorID
}

func canChangeGroupTaskStatus(role GroupRole, actorID int64, task GroupTask) bool {
    if role == GroupRoleAdmin { return true }
    return task.AssigneeUserID == nil || *task.AssigneeUserID == actorID
}

func isAllowedUserStatusTransition(from, to string) bool {
    return (from == StatusNew && to == StatusInProgress) ||
        (from == StatusInProgress && to == StatusDone) ||
        (from == StatusDone && to == StatusInProgress)
}
```

Apply Admin-only checks to group rename, task deletion, and reassignment. Apply author/Admin checks to content editing. Keep the membership check before task lookup.

- [ ] **Step 6: Implement ID-based assignment**

Replace username resolution with membership lookup by positive `userID`. For User actors, reject a target with `GroupRoleAdmin` using `ErrCannotAssignAdmin`. Permit `nil` as common. Update `taskForResponse` to keep `AssigneeUsername` as display-only compatibility data. Update removal logic to clear assignees.

- [ ] **Step 7: Implement idempotent mutations**

Return `changed == false` and preserve `UpdatedAt` when group name, task content, status, or assignee is unchanged. For real changes, update `UpdatedAt` exactly once.

- [ ] **Step 8: Run bot tests**

Run: `GOCACHE=/tmp/mtuci-group-domain-green go test ./bot -count=1`

Expected: PASS.

- [ ] **Step 9: Run the race detector for storage**

Run: `GOCACHE=/tmp/mtuci-group-domain-race go test -race ./bot -count=1`

Expected: PASS with no race report.

---

### Task 4: HTTP Parsing and Editing Routes

**Files:**
- Modify: `api/router.go`
- Modify: `api/handlers.go`
- Modify: `api/groups_handlers_test.go`
- Create: `api/tasks_handlers_test.go`

**Interfaces:**
- Consumes all storage methods and errors from Tasks 2–3.
- Produces: `PATCH /api/tasks/{id}`, `PATCH /api/groups/{groupId}`, and `PATCH /api/groups/{groupId}/tasks/{taskId}`.
- Produces: JSON tri-state parsing for `assignee_user_id`: absent, explicit null, positive integer.

- [ ] **Step 1: Write failing personal edit handler tests**

Use `callHandler` with owner context. Cover title-only, description clearing, no-op, empty object, unknown field, invalid/null title, over-length fields, invalid ID, and missing task. Assert response contains the updated task and `updated_at`.

- [ ] **Step 2: Write failing group edit and role tests**

Update group setup to use `assignee_user_id`. Cover Admin/User rename, Admin/author/assignee/other content edits, Admin-only delete and reassignment, User-to-Admin assignment rejection, and status transition HTTP 403 behavior.

- [ ] **Step 3: Write tri-state assignee tests**

Create one request with the field absent and expect the actor as assignee, one with `null` and expect common, and one with a positive member ID. Reject strings, zero, negative values, nonmembers, and Admin targets selected by a User.

- [ ] **Step 4: Run API tests and observe failure**

Run: `GOCACHE=/tmp/mtuci-api-red go test ./api -count=1`

Expected: route/signature compilation failures or failing authorization assertions.

- [ ] **Step 5: Add strict optional patch parsing**

Use a custom optional string field that records key presence and rejects JSON
`null`, plus a custom nullable user-ID field that distinguishes absent,
explicit `null`, and a positive integer. Continue using `decodeJSONBody` with
unknown-field rejection and the existing body limit. Require at least one task
patch field. For create requests, absent assignee defaults to `user.ID`;
explicit null passes `nil`.

- [ ] **Step 6: Add routes and handlers**

Register:

```go
mux.HandleFunc("PATCH /api/tasks/{id}", s.withAuth(s.handleUpdateTask))
mux.HandleFunc("PATCH /api/groups/{groupId}", s.withAuth(s.handleUpdateGroup))
mux.HandleFunc("PATCH /api/groups/{groupId}/tasks/{taskId}", s.withAuth(s.handleUpdateGroupTask))
```

Return the updated resource with HTTP 200. Change creation and assignee handlers to accept IDs. Update status and assignee handlers for the `(resource, changed, error)` domain results.

- [ ] **Step 7: Map domain errors without information leakage**

Map permission and forbidden transition errors to 403, malformed edits to 400, missing objects to 404, and nonmember assignees to 409. Keep outsider membership checks ahead of nested task lookup.

- [ ] **Step 8: Add only approved notifications**

On a changed group-task edit, send the existing task-event notification while excluding the actor from recipients. On a changed group rename, notify all other members with old and new names. Do not send on no-op. Do not introduce queues, retries, preferences, or changes to unrelated event routing.

- [ ] **Step 9: Run API and all Go tests**

Run: `GOCACHE=/tmp/mtuci-api-green go test ./... -count=1`

Expected: PASS.

---

### Task 5: Role-Aware Mini App and Editing UI

**Files:**
- Modify: `web/index.html`
- Modify: `web/app.js`
- Modify: `web/style.css`

**Interfaces:**
- Consumes: member `role`, task `created_by`, nullable `assignee_user_id`, and the three new PATCH routes.
- Produces: role-aware controls and ID-based assignee requests.

- [ ] **Step 1: Update HTML forms**

Add stable IDs to the personal task form heading/submit button so the same form can support create and edit. Add an Admin-only group-name edit control and hidden form. Replace group-task assignee text inputs in create/detail views with `<select>` controls. Add a group-task edit button that reuses the title/description form while hiding the assignee field in edit mode.

- [ ] **Step 2: Add current-role helpers**

Derive the role only from the current user's `GroupMember.role`:

```js
function currentGroupRole() {
    return groupMembers.find(member => member.user_id === currentUserId)?.role || '';
}

function isCurrentUserGroupAdmin() {
    return currentGroupRole() === 'admin';
}
```

Remove UI authorization comparisons against `creator_id` and `is_creator`.

- [ ] **Step 3: Build assignee selectors from members**

Render `Common task` plus eligible member options. For User, omit members with `role === 'admin'`; for Admin, include all. Select `currentUserId` by default for creation. Send `Number(value)` or `null` as `assignee_user_id`; never send username as the mutation identifier.

- [ ] **Step 4: Implement personal edit flow**

Populate the reusable form from `currentPersonalTask`, switch its mode to edit, call `PATCH /tasks/{id}` with normalized title/description, and return to updated detail. Preserve create behavior and submission locking.

- [ ] **Step 5: Implement group rename and task edit flows**

Show rename only to Admin and call `PATCH /groups/{id}`. Show content editing to Admin or `task.created_by === currentUserId` and call `PATCH /groups/{groupId}/tasks/{taskId}`. Refresh the relevant in-memory list before reopening detail.

- [ ] **Step 6: Enforce role-aware visible actions**

Show delete and reassignment only to Admin. Render status choices as follows: Admin gets every noncurrent status; User gets choices only when task is common or assigned to them, and only the exact approved next transitions. Do not rely on these checks for server security.

- [ ] **Step 7: Review frontend syntax and references**

Run: `rg -n "is_creator|creator_id.*currentUserId|assignee_username.*api|group-task-assignee.*type=\"text\"" web`

Expected: no old role authorization or username-based assignee mutation remains. If `node` is available, run `node --check web/app.js`; otherwise record that JavaScript parser verification was unavailable.

- [ ] **Step 8: Run Go regression tests after static asset edits**

Run: `GOCACHE=/tmp/mtuci-web-regression go test ./... -count=1`

Expected: PASS.

---

### Task 6: Documentation and Final Verification

**Files:**
- Modify: `README.md`
- Review: every modified file

**Interfaces:**
- Consumes: completed behavior from Tasks 1–5.
- Produces: accurate user-facing documentation and verification evidence.

- [ ] **Step 1: Update README behavior**

Document explicit Admin/User roles, the single-Admin limitation, common versus assigned status permissions, assignment defaults/restrictions, Admin-only reassignment/deletion, and editing capabilities. Remove the old creator-only wording where role terminology supersedes it.

- [ ] **Step 2: Format Go and inspect all changes**

Run: `gofmt -w main.go api/*.go bot/*.go`

Then: `git diff --check && git status --short && git diff --stat`

Expected: no formatting errors and only scoped files changed.

- [ ] **Step 3: Run focused tests freshly**

Run: `GOCACHE=/tmp/mtuci-final-focused go test ./bot ./api -count=1`

Expected: PASS.

- [ ] **Step 4: Run full tests freshly**

Run: `GOCACHE=/tmp/mtuci-final-tests go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: Run race detector freshly**

Run: `GOCACHE=/tmp/mtuci-final-race go test -race ./... -count=1`

Expected: PASS with no race report.

- [ ] **Step 6: Run static analysis freshly**

Run: `GOCACHE=/tmp/mtuci-final-vet go vet ./...`

Expected: exit code 0 with no findings.

- [ ] **Step 7: Review the complete diff**

Run: `git diff -- README.md api bot web docs/superpowers`

Verify every specification rule has implementation and test coverage, no secret or generated artifact was added, and no unrelated user change was overwritten.

- [ ] **Step 8: Report without committing**

Summarize files changed, tests actually run, security implications, remaining frontend verification limitations, and the explicitly deferred roadmap. Do not commit, push, deploy, or create a pull request.
