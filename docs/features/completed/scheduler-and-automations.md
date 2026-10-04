# Toskar Scheduler & Automations

Implemented. Day-to-day use is in the user guide section “Schedule an automation.” This document is the v1 specification.

## Feature Specification — V1

### Goal

Let Toskar perform useful AI tasks on a schedule without requiring the desktop UI to remain open.

## Core Principle

The scheduler belongs to the Toskar daemon/control plane, not the desktop UI.

Represent automations as scheduled prompts/tasks rather than hard-coded product-specific features. Price checks, stock checks, website monitoring, recurring research, and summaries are all instances of the same primitive.

## Example User Tasks

- Every morning at 8:00 AM, check this product and tell me if the price is below $500.
- Every six hours, check whether this item is back in stock. Notify me only when it becomes available.
- Every Friday, check for new releases of this software and summarize what changed.
- Run this research prompt once tomorrow at 9:00 AM.

## Task Model

Each task should contain:

- ID
- Name
- Enabled/paused state
- Schedule and time zone
- Prompt/instructions
- Selected profile/model policy
- Required tools
- Notification condition
- Created/updated timestamps
- Next run
- Last run
- Execution history

Support one-time and recurring schedules.

## Execution Lifecycle

When a task becomes due:

1. The daemon claims the job.
2. Resolve profile/tools/model requirements.
3. Start a suitable model if necessary.
4. Execute the task.
5. Store the result.
6. Evaluate the notification condition.
7. Return the model to normal lifecycle policy.

A model must not need to remain loaded between scheduled runs.

Scheduler execution should integrate with Norn and existing runtime health/cleanup behavior.

## Notifications

Separate execution from notification.

V1 should support:

- Always notify.
- Notify on condition.
- Notify on change.
- No notification; store result only.

Common conditions:

- price below/above threshold,
- item becomes available,
- content changes,
- model-determined significance.

Use native OS notifications where available.

## Task History & UI

Add an **Automations** or **Scheduled Tasks** view showing:

- task name,
- enabled/paused state,
- schedule,
- last run,
- last result/status,
- next run.

Task details should provide:

- Run Now
- Pause/Resume
- Edit
- Delete
- Run History

Each run record should include:

- scheduled time,
- actual start/end,
- status,
- concise result,
- whether a notification was sent,
- model/node used,
- useful error information.

## Reliability

- Persist schedules and run state.
- Prevent duplicate execution after restart using durable job/run IDs and an atomic claim/lease mechanism.
- Define missed-run behavior.
- V1 default: after restart, run the most recent missed occurrence once when appropriate rather than replaying an unbounded backlog.
- Use bounded retries for transient failures with backoff.
- Do not create retry loops for persistent model/OOM/tool failures.
- Record failures in history.
- Notify users when a task repeatedly cannot run.

## Security & Tool Access

A scheduled task may only use tools/permissions explicitly available to its selected profile or granted for that automation.

Do not silently broaden permissions because execution is unattended.

V1 should favor read-only monitoring/research tasks for unattended execution.

## Relationship to Memory

Scheduled tasks may optionally use Muninn/user memory when configured to do so.

Store enough task-specific context in the automation itself that the task remains understandable and reproducible.

## V1 Scope

- One-time and recurring schedules.
- Natural-language task creation translated into a structured task.
- Scheduled prompts with normal Toskar tools.
- Conditional notifications.
- Run Now and Pause/Resume.
- Persistent run history.
- Daemon-owned execution while GUI is closed.
- Model startup/on-demand execution and normal cleanup.
- Basic retries, missed-run handling, and failure reporting.

## Explicitly Out of Scope for V1

- Zapier-style visual workflow builder.
- Arbitrary multi-step DAG/workflow composition.
- Distributed exactly-once scheduling across a large cluster.
- Complex event buses or webhook marketplaces.
- Unrestricted unattended high-impact external actions.

## Acceptance Criteria

1. A scheduled task executes at the expected local time even when the desktop UI is closed.
2. A task can start a required model on demand and complete without that model being permanently loaded.
3. One-time and recurring tasks survive daemon/application restart.
4. Conditional tasks can run without notifying when false and notify when true.
5. Users can see last run, next run, result/status, and execution history.
6. Run Now, Pause/Resume, Edit, and Delete behave predictably.
7. Transient failures use bounded retries; persistent failures do not loop indefinitely.
8. Restart recovery does not create duplicate executions for the same occurrence.
9. Tool permissions are not broadened for unattended execution.

## Product Outcome

The scheduler succeeds when Toskar can quietly perform recurring AI work in the background and surface only useful results.
