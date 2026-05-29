# Persistent Task System

## Overview

The persistent task system provides durable, file-based task management that survives context compression and session restarts. Plans and tasks are stored as JSON/Markdown files in the `.tasks/` directory.

## Directory Layout

```
.tasks/
  todo/                          ← Active plans
    2026-05-28-add-auth/
      plan.md                    ← Requirements analysis + approach + steps
      task_1.json                ← Individual task records
      task_2.json
      task_3.json
  done/                          ← Completed plans (archived)
    2026-05-20-fix-login/
      plan.md
      task_1.json
      task_2.json
```

## Workflow

### Step 1: Create a Plan

Analyze requirements and project code, then create a plan with:
- Requirements analysis
- Proposed approach
- Step-by-step execution plan

```
plan_create(name="2026-05-28-add-auth", content="# Add Authentication\n\n## Analysis\n...")
```

### Step 2: Create Tasks from Plan

Split the plan into executable task records with a dependency graph:

```
plan_task_create(plan="2026-05-28-add-auth", subject="Design auth schema", description="...")
plan_task_create(plan="2026-05-28-add-auth", subject="Implement JWT middleware", blockedBy=[1])
plan_task_create(plan="2026-05-28-add-auth", subject="Add login endpoint", blockedBy=[1, 2])
```

### Step 3: Execute Tasks by Dependency Order

Work through tasks respecting their dependency graph:

```
plan_task_update(plan="2026-05-28-add-auth", task_id=1, status="in_progress")
# ... do the work ...
plan_task_update(plan="2026-05-28-add-auth", task_id=1, status="completed")
# task_2's blockedBy is now cleared automatically
```

### Step 4: Complete the Plan

When all tasks are done, archive the plan:

```
plan_complete(plan="2026-05-28-add-auth")
# Moves from .tasks/todo/ → .tasks/done/
```

## Tools Reference

| Tool | Description |
|------|-------------|
| `plan_create` | Create a new plan directory with plan.md |
| `plan_list` | List all plans (active + completed) with progress |
| `plan_task_create` | Add a task to a plan with optional dependencies |
| `plan_task_update` | Update task status, owner, or dependencies |
| `plan_task_list` | List all tasks in a plan with status summary |
| `plan_task_get` | Get full details of a specific task |
| `plan_complete` | Move completed plan from todo/ to done/ |

## Task Record Format

Each `task_N.json` file:

```json
{
  "id": 1,
  "subject": "Design auth schema",
  "description": "Create database migration for users table with email, password_hash, created_at",
  "status": "pending",
  "blockedBy": [],
  "blocks": [2, 3],
  "owner": ""
}
```

### Status Values

- `pending` — Not yet started
- `in_progress` — Currently being worked on
- `completed` — Done (automatically unblocks dependent tasks)
- `deleted` — Removed from plan (also unblocks dependents)

## Dependency Resolution

```
+----------+     +----------+     +----------+
| task 1   | --> | task 2   | --> | task 3   |
| pending  |     | blocked  |     | blocked  |
+----------+     +----------+     +----------+
     |                ^
     +--- completing task 1 removes it from task 2's blockedBy
```

When a task is marked `completed`:
1. Its ID is removed from all other tasks' `blockedBy` lists
2. Tasks whose `blockedBy` becomes empty are now unblocked and ready to start

## Key Design Decisions

- **File-based persistence**: Tasks survive context compression because they live on disk
- **Bidirectional dependencies**: Setting `blockedBy` on a task also updates the blocker's `blocks` list
- **Naming convention**: `YYYY-MM-DD-description` ensures chronological ordering and descriptive names
- **Atomic plan completion**: All tasks must be completed/deleted before a plan can move to done/
- **Thread-safe**: All operations protected by `sync.RWMutex` for concurrent access

## Initialization

The plan system is initialized at startup in `main.go`:

```go
tools.InitPlan(cfg.ProjectDir)
```

This creates `.tasks/todo/` and `.tasks/done/` directories if they don't exist.
