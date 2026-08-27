# Group Roles and Editing Design

## Scope

This change introduces explicit `Admin` and `User` roles for groups, defines
authorization rules for common and assigned group tasks, replaces manual
assignee username input with member selection by Telegram user ID, and adds
editing for personal tasks, group tasks, and group names.

The following are explicitly out of scope for this iteration:

- granting additional permissions to individual users;
- promoting another member to Admin or supporting multiple Admins;
- persistent storage or database migrations;
- notification queues, preferences, retries, or other notification redesign;
- deadlines, priorities, filters, invitations, audit history, and unrelated
  roadmap items.

## Roles and Membership

```go
type GroupRole string

const (
    GroupRoleAdmin GroupRole = "admin"
    GroupRoleUser  GroupRole = "user"
)
```

Group membership stores the role explicitly:

```go
groupMembers map[int]map[int64]GroupRole
```

The group creator is its only Admin and receives `GroupRoleAdmin`
automatically. Members added later receive `GroupRoleUser`. This iteration
does not expose any operation that changes a role. The Admin cannot leave the
group or be removed; they can only delete the group. `CreatorID` remains an
immutable record of who created the group.

The group-member API response contains `role: "admin" | "user"`. The old
`is_creator` presentation flag is removed. Authorization and UI decisions use
the explicit role.

## Group Task Model

A group task may be common or assigned to exactly one current group member.
`CreatedBy` remains immutable. `AssigneeUserID` remains optional. Display data
for the assignee may be included in API responses, but mutations identify the
assignee by Telegram user ID rather than username.

All group members can see all tasks in their group.

When a User creates a task, the default assignee is that User. They may instead
make it common or select any member whose role is User. A User cannot assign an
Admin. When an Admin creates a task, the default assignee is that Admin; the
Admin may select any group member or make the task common.

Only the Admin may change the assignee after task creation. Removing an
assignee from the group makes their assigned tasks common.

## Authorization Rules

| Action | Admin | User |
| --- | --- | --- |
| View group, members, and all tasks | Allowed | Allowed |
| Rename group | Allowed | Denied |
| Delete group | Allowed | Denied |
| Add or remove other members | Allowed | Denied |
| Leave group | Denied | Allowed |
| Create group task | Allowed | Allowed |
| Edit task title and description | Any task | Only a task they created |
| Change assignee after creation | Allowed | Denied |
| Delete group task | Allowed | Denied |
| Change status of a common task | Allowed | Allowed |
| Change status of an assigned task | Allowed | Only when assigned to them |

For a User, content-editing rights follow authorship and do not follow the
assignee. Therefore, an assignee who did not create the task may change its
status but not its title or description. Conversely, the User who created a
task assigned to someone else may edit its title and description but may not
change its status.

Admin may set any valid status. A User who is authorized to change a status
may only make these transitions:

- `new -> in_progress`;
- `in_progress -> done`;
- `done -> in_progress`.

Setting the current status again is an idempotent success: it does not update
`UpdatedAt` or send a notification. A forbidden transition or operation
returns HTTP 403. Group membership is checked before task lookup so an
outsider cannot operate on group resources.

All authorization is enforced in the domain/storage layer. The Mini App may
hide unavailable actions, but it is not an authorization boundary.

## Editing

The following endpoints are added:

```text
PATCH /api/tasks/{id}
PATCH /api/groups/{groupId}
PATCH /api/groups/{groupId}/tasks/{taskId}
```

Personal-task and group-task edit requests accept optional `title` and
`description` string fields and require at least one of them. An empty
description clears it. An omitted title leaves it unchanged; an explicit null
or whitespace-only title is rejected. Existing rune-length limits remain
authoritative.

The group edit request requires a non-empty `name` string and uses the existing
group-name length limit.

Personal tasks gain `UpdatedAt`; group tasks continue using their existing
`UpdatedAt`. A real content or status change updates it. Submitting values that
are identical after normalization is an idempotent success without a timestamp
or notification change.

Only the owner can edit a personal task. Only the Admin can rename a group.
The group-task rules are defined in the authorization table above.

## Assignee API and Mini App

Group-task creation accepts an optional `assignee_user_id`:

- field absent: assign the acting user by default;
- explicit `null`: create a common task;
- positive integer: assign that group member, subject to role restrictions.

The existing assignee-update endpoint remains, but its request uses required
`assignee_user_id`, which may be `null` for a common task. Only Admin can call
it.

The Mini App replaces free-form username entry with a member selector. It
defaults to the current user and includes a separate `Common task` choice. For
a User, Admin members are excluded from assignable choices. For an Admin, all
members are available.

The Mini App adds:

- editing controls for a personal task;
- a group-renaming control visible to Admin;
- group-task editing visible to Admin or the task author;
- assignee management and deletion visible only to Admin;
- status controls only when the current user is allowed to perform the
  corresponding transition.

The server remains authoritative if a client manually invokes a hidden action.

## Existing Notifications Affected by This Scope

No general notification redesign is included. Only the already approved
effects of the new editing operations are added:

- editing a group task sends a Telegram notification according to its common
  or assigned state and excludes the actor;
- renaming a group notifies all other group members and includes the old and
  new names;
- an idempotent edit sends no notification.

Existing task-event notifications remain otherwise unchanged. Broader routing,
delivery guarantees, preferences, retries, and queues will be designed later.

## API Errors

- HTTP 400: malformed input, invalid IDs, empty names/titles, invalid status,
  or length-limit violations;
- HTTP 403: insufficient role, forbidden assignee choice, forbidden status
  transition, or operation on a group resource without permission;
- HTTP 404: group, task, user, or member does not exist;
- HTTP 409: a selected assignee exists but is not a member of the group, or an
  existing membership conflict occurs.

Errors do not expose data from groups the caller cannot access.

## Verification

Tests must cover:

- role assignment for creator and added members;
- the complete authorization matrix for Admin, task author, assignee, another
  User, and outsider;
- allowed, forbidden, and idempotent status transitions;
- User creation defaults and assignee restrictions;
- Admin assignment and reassignment;
- making tasks common and removing an assigned member;
- personal-task, group-task, and group-name editing, including validation and
  unchanged values;
- notification emission for task editing and group renaming, including actor
  exclusion;
- HTTP status mapping and route integration;
- Mini App behavior for both roles and for author/assignee distinctions where
  practical with the project's available frontend test tooling.

The final verification commands are:

```sh
go test ./...
go test -race ./...
go vet ./...
```
