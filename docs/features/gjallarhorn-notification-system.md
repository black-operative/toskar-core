# Toskar Core — Gjallarhorn Notification System

## Feature Specification — V1

### Status

**Type:** Platform Feature  
**Subsystem name:** Gjallarhorn  
**Primary goal:** Provide a durable, privacy-conscious notification and delivery system for Toskar automations, health events, approvals, long-running jobs, and other background activity.

---

## 1. Goal

Toskar needs a first-class notification subsystem because scheduled jobs, condition watches, long-running tasks, health monitoring, training, downloads, and remote workflows may complete or fail when the user is not actively looking at the application.

The scheduler should not own email, push, webhook, or desktop-notification logic directly.

Instead:

```text
Scheduler / Norn
      │
      ▼
Huginn executes work
      │
      ▼
Event / result
      │
      ▼
Gjallarhorn
      │
      ├── In-app notification center
      ├── Desktop notification
      ├── Mobile push
      ├── Email
      ├── Webhook
      └── future channels
```

The product principle is:

> **Toskar should notify users when something matters without forcing every subsystem to implement its own delivery logic.**

---

## 2. Why a Separate Notification Subsystem

Notifications will be useful to more than the scheduler.

Potential producers include:

- scheduled automations,
- condition watches,
- Heimdall health alerts,
- model download completion,
- model/runtime failures,
- training completion,
- Grid/node failures,
- tool approval requests,
- tool execution completion,
- long-running document/image/video jobs,
- software update availability,
- remote access events,
- security events.

If each feature implements delivery separately, Toskar will accumulate duplicated logic for:

- retries,
- email delivery,
- push notifications,
- quiet hours,
- deduplication,
- unread state,
- history,
- failure handling,
- preferences.

Gjallarhorn should centralize those concerns.

---

## 3. Naming

Recommended subsystem name:

# Gjallarhorn

Gjallarhorn is Heimdall's horn in Norse mythology and is used to signal important events.

That maps cleanly to the Toskar subsystem model:

```text
Norn       scheduling / placement
Huginn     execution / orchestration
Muninn     memory / recall
Gungnir    tools / capabilities
Heimdall   health / diagnostics
Gjallarhorn notifications / delivery
```

---

# Part I — Core Notification Model

## 4. Notification as a Durable Object

Every user-facing notification should first become a durable Toskar notification.

Delivery channels are secondary.

Conceptual model:

```text
Notification
  id
  created_at
  updated_at

  source_type
  source_id

  category
  severity

  title
  body
  rich_content
  artifact_refs

  read_at
  dismissed_at

  dedupe_key
  group_key

  delivery_policy
```

This means:

```text
Automation completed
      ↓
Create Notification
      ↓
Store in Toskar
      ↓
Fan out to configured channels
```

A delivery failure must not erase the notification itself.

---

## 5. Notification Categories

Suggested categories:

```text
automation
health
model
tool
approval
training
download
system
security
general
```

Categories allow:

- filtering,
- per-category preferences,
- quiet-hour exceptions,
- future digest behavior.

---

## 6. Severity

Keep severity simple in V1:

```text
Info
Success
Warning
Error
```

Potential future level:

```text
Urgent
```

`Urgent` should not be available to arbitrary model-generated content without policy controls.

Example uses:

```text
Info
"Daily summary is ready."

Success
"Model download completed."

Warning
"Node has been unavailable for 10 minutes."

Error
"Automation failed after 3 attempts."
```

---

# Part II — Delivery Channels

## 7. V1 Channels

Recommended initial channels:

| Channel | Priority | Purpose |
| --- | --- | --- |
| In-app Notification Center | Required | Canonical durable notification history |
| Desktop OS notification | Required | Immediate notification while Desktop is installed/running |
| Mobile push | Required | Notify iOS/mobile users while away from the app |
| Email | High | Reports, summaries, unattended-server use |
| Webhook | High | Automation and advanced-user integrations |

Future:

```text
SMS
Slack
Discord
Microsoft Teams
Home Assistant
other plugin/provider channels
```

---

## 8. In-App Notification Center

The in-app notification center should be the canonical user-facing history.

Recommended UI:

```text
Notifications

Today

Package status changed
Automation · 3:42 PM
Out for delivery

Model download complete
Models · 2:15 PM
Qwen3 30B is ready to use

Automation failed
Error · 10:06 AM
Daily report failed after 3 attempts
```

Core behaviors:

- unread/read state,
- dismiss,
- filter by category,
- link back to source,
- persistent history,
- optional artifact links.

Recommended navigation:

```text
Bell icon
    ↓
Notification Center
```

---

## 9. Desktop Notifications

Desktop notifications should use native OS notification systems where practical:

```text
macOS
Windows
Linux
```

Desktop notifications should be treated as delivery attempts for an already-created Gjallarhorn notification.

They should not be the only record.

---

## 10. Mobile Push

Mobile push is particularly important because scheduled jobs may run while Toskar Mobile is not active.

Conceptual architecture:

```text
Local Toskar Core
        │
        │ outbound HTTPS
        ▼
Toskar Push Relay
        │
        ▼
APNs
        │
        ▼
Toskar Mobile
```

The push relay should be narrowly scoped.

It should not require access to the user's model conversations.

---

## 11. Push Privacy Modes

Support two conceptual push styles.

### Rich push

Push contains useful content:

```text
Title:
Package status changed

Body:
Your package is now out for delivery.
```

Benefits:

- immediately useful,
- no live Core connection required.

Tradeoff:

- notification content passes through the push relay/APNs.

### Private push

Push contains only:

```text
You have a new Toskar notification.
```

Mobile retrieves the full notification from Core when connectivity is available.

Benefits:

- less content leaves the user's system.

Tradeoff:

- less useful when remote access is unavailable.

Recommended:

Allow the user to choose a privacy level later. V1 may select one clearly documented behavior.

---

## 12. Email

Email should support two provider models.

### User-managed SMTP

The user configures:

```text
SMTP host
port
username
password/token
from address
TLS policy
```

This preserves a fully self-hosted option.

### Toskar-managed relay

Future optional hosted service:

```text
Notifications
Email: user@example.com
[Verify]
```

This improves setup for non-technical users.

Because managed email creates recurring infrastructure cost, it may be appropriate for a future optional cloud/remote tier.

---

## 13. Webhooks

Webhook delivery is valuable for advanced users.

Example:

```text
POST https://example.com/yggdrasil-hook
```

Potential payload:

```json
{
  "version": 1,
  "notification_id": "ntf_1234",
  "created_at": "2026-09-28T21:00:00Z",
  "category": "automation",
  "severity": "success",
  "title": "Price target reached",
  "body": "The tracked item is now below $500.",
  "source": {
    "type": "automation",
    "id": "auto_5678"
  }
}
```

Recommended protections:

- HTTPS required by default,
- signed webhook payloads,
- retry policy,
- timeout,
- per-destination secret.

---

# Part III — Delivery State

## 14. Separate Notification and Delivery Records

Delivery state should be separate from the notification.

Conceptual model:

```text
NotificationDelivery
  id
  notification_id

  channel
  destination_id

  status
  attempts

  first_attempt_at
  last_attempt_at
  delivered_at

  error_code
  error_message
```

Potential statuses:

```text
Pending
Queued
Delivered
Failed
Suppressed
Cancelled
```

This allows email to fail while mobile push succeeds.

---

## 15. Delivery Fan-Out

Example:

```text
Notification
"Daily market summary ready"

Configured channels:
  in-app
  email
  mobile push

Result:
  in-app       stored
  email        delivered
  mobile push  delivered
```

Each delivery is tracked independently.

---

# Part IV — Notification Policies

## 16. Scheduler Notification Policies

Automations should support:

```text
Always notify
Notify when condition is true
Notify when result changes
Store result only
Notify on failure
```

These should remain separate from delivery channels.

Example:

```text
Check UPS package every hour

Notification policy:
Only when result changes

Channels:
Push
In-app
```

---

## 17. Always Notify

Use for things like:

```text
Daily summary
Weekly report
Scheduled reminder
```

Every successful run creates a notification.

---

## 18. Condition-Based Notify

Example:

```text
Check whether model is available
```

Only create a user-facing notification when:

```text
condition == true
```

Runs that do not match the condition should remain in automation history without notifying.

---

## 19. Change-Based Notify

Example:

```text
Track package status
```

Flow:

```text
Current status
      ↓
Compare with previous successful result
      ↓
Changed?
  ├── No → no notification
  └── Yes → notify
```

This is important for preventing repetitive alerts.

---

## 20. Failure Notify

Recommended policy:

```text
Notify:
after N consecutive failures
```

rather than notifying on every transient error.

Example:

```text
Daily report failed after 3 attempts.
```

Potential settings:

```text
On first failure
After 3 consecutive failures
After 5 consecutive failures
Never
```

Default should avoid alert spam.

---

# Part V — Deduplication and Grouping

## 21. Deduplication

Gjallarhorn should support `dedupe_key`.

Example:

```text
health:node-123:offline
```

If an identical condition remains active, avoid creating dozens of separate notifications.

---

## 22. Grouping

Repeated events may be grouped.

Instead of:

```text
Automation failed
Automation failed
Automation failed
Automation failed
```

show:

```text
Automation failed 4 times
```

Potential fields:

```text
group_key
count
first_seen
last_seen
```

---

## 23. State-Change Notifications

Health alerts should ideally notify on transitions:

```text
Healthy → Unavailable
```

and later:

```text
Unavailable → Healthy
```

rather than repeatedly announcing the same state.

---

# Part VI — Quiet Hours and Delivery Preferences

## 24. Quiet Hours

Users should be able to configure:

```text
Quiet hours:
10:00 PM – 7:00 AM
```

Possible behavior:

```text
Hold normal notifications
Deliver errors only
Deliver everything
```

Recommended default:

```text
Hold Info/Success
Allow Error
```

but this should remain user-configurable.

---

## 25. Quiet-Hour Queueing

Held notifications should remain in the Notification Center immediately.

External delivery may wait until quiet hours end.

Example:

```text
2:00 AM
Automation completes

In-app:
available immediately

Push:
held until 7:00 AM
```

---

## 26. Per-Channel Preferences

Example:

```text
Automation success
  In-app    On
  Push      On
  Email     Off

Automation failure
  In-app    On
  Push      On
  Email     On

Model downloads
  In-app    On
  Push      Off
```

V1 may expose simpler settings first.

---

# Part VII — Scheduler Integration

## 27. Scheduler Responsibilities

The scheduler should not know how email or push works.

Scheduler responsibility:

```text
run task
determine notification condition
emit notification request
```

Gjallarhorn responsibility:

```text
persist notification
resolve preferences
fan out delivery
retry delivery
record results
```

---

## 28. Automation Example

```text
Automation:
Check package every hour

Task:
Look up tracking status

Notify:
Only when result changes

Channels:
In-app
Push

Failure:
Notify after 3 consecutive failures
```

Execution:

```text
Norn schedules run
      ↓
Huginn executes
      ↓
status unchanged
      ↓
run recorded
      ↓
no Gjallarhorn notification
```

Later:

```text
status changes
      ↓
Gjallarhorn notification created
      ↓
in-app stored
push delivered
```

---

# Part VIII — Other Subsystem Integrations

## 29. Heimdall

Heimdall may emit:

```text
node offline
node recovered
model repeatedly crashing
provider unhealthy
disk pressure
runtime failure
```

Gjallarhorn decides user delivery based on policy.

---

## 30. Gungnir

Potential notifications:

```text
Tool requires approval
Tool job completed
Tool job failed
Browser workflow awaiting input
```

Future notification actions may allow:

```text
[Approve]
[Deny]
[Retry]
```

---

## 31. Model Management

Potential notifications:

```text
download complete
download failed
model update available
model installation complete
```

---

## 32. Training

Potential notifications:

```text
training started
checkpoint completed
training completed
training failed
evaluation finished
```

Long-running training workflows particularly benefit from push/email.

---

## 33. Grid / Multi-Node

Potential notifications:

```text
node joined
node left
node unhealthy
distributed job failed
Grid capacity degraded
```

---

# Part IX — Mobile Architecture

## 34. Device Registration

Toskar Mobile should register a push destination.

Conceptual flow:

```text
Mobile
  ↓
APNs token
  ↓
Toskar Push Relay registration
  ↓
device destination ID
  ↓
Core associates destination with user/device
```

Avoid exposing APNs device tokens directly to models or arbitrary tools.

---

## 35. Multiple Devices

One Toskar user/environment may have:

```text
iPhone
iPad
MacBook
Desktop
```

Notification preferences should eventually support:

```text
all devices
selected devices
device-class preferences
```

V1 may simply deliver to all registered mobile devices.

---

# Part X — Retry and Failure Handling

## 36. Delivery Retries

Delivery retries should be independent of task retries.

Example:

```text
Task completed successfully
Email provider temporarily unavailable
```

Correct behavior:

```text
Do not rerun task
Retry only email delivery
```

---

## 37. Retry Policy

Recommended:

```text
bounded exponential backoff
```

Example:

```text
Attempt 1
+1 minute
+5 minutes
+30 minutes
```

Exact values may vary by provider.

---

## 38. Permanent Failure

If a destination is invalid:

```text
invalid SMTP credentials
expired webhook secret
revoked push destination
```

mark delivery failed and surface configuration guidance.

Avoid retrying permanently invalid destinations indefinitely.

---

# Part XI — Security and Privacy

## 39. General Rules

1. Do not expose notification credentials to models.
2. Do not log SMTP passwords, webhook secrets, APNs credentials, or OAuth tokens.
3. Encrypt sensitive provider credentials at rest where supported.
4. Require TLS for hosted relay communication.
5. Validate webhook destinations.
6. Sign webhook payloads.
7. Rate-limit externally accessible notification endpoints.
8. Keep push relay payloads minimal.
9. Separate notification content from delivery credentials.
10. Never make hosted notification infrastructure mandatory for local use.

---

## 40. Self-Hosted Operation

A user should be able to run Toskar without Yeix.io-hosted notification infrastructure.

Self-hosted paths:

```text
In-app
Desktop OS
SMTP
Webhook
```

Mobile push through APNs normally requires some form of relay/service infrastructure, but the rest of Gjallarhorn should remain functional without it.

---

## 41. Hosted Relay Scope

If Yeix.io operates:

```text
push relay
email relay
```

those services should be narrow.

They should not become general proxies for:

```text
model prompts
conversation history
files
model responses
```

Only the content necessary for the configured notification mode should be sent.

---

# Part XII — User Interface

## 42. Notification Center

Recommended top-level UI:

```text
🔔
```

Notification Center should support:

```text
All
Unread
Errors
Automations
System
```

Potential actions:

```text
Mark read
Dismiss
Open source
Retry delivery
```

---

## 43. Notification Settings

Recommended settings structure:

```text
Notifications

General
  Enable notifications
  Quiet hours

Desktop
  Enabled

Mobile Push
  Enabled
  Registered devices

Email
  Enabled
  Delivery mode
  Address

Webhooks
  Destinations

Advanced
  Per-category rules
  Delivery diagnostics
```

---

## 44. Automation-Level Settings

Within an automation:

```text
Notifications

Notify:
  ○ Always
  ● When result changes
  ○ When condition is true
  ○ Store only

On failure:
  ● After 3 consecutive failures

Deliver via:
  ☑ In-app
  ☑ Push
  ☐ Email
  ☐ Webhook
```

---

# Part XIII — API Design

## 45. Conceptual Core API

Potential endpoints:

```text
GET    /api/v1/notifications
GET    /api/v1/notifications/{id}
POST   /api/v1/notifications/{id}/read
POST   /api/v1/notifications/{id}/dismiss

GET    /api/v1/notification-preferences
PUT    /api/v1/notification-preferences

GET    /api/v1/notification-destinations
POST   /api/v1/notification-destinations
DELETE /api/v1/notification-destinations/{id}
```

Internal producer endpoint/event should not necessarily be public.

---

## 46. Internal Notification Request

Conceptual:

```json
{
  "source": {
    "type": "automation",
    "id": "auto_1234"
  },
  "category": "automation",
  "severity": "success",
  "title": "Package status changed",
  "body": "Your package is now out for delivery.",
  "dedupe_key": "automation:auto_1234:status",
  "group_key": "automation:auto_1234",
  "delivery_policy": {
    "channels": ["in_app", "push"]
  }
}
```

---

# Part XIV — Data Model

## 47. Notification

Conceptual:

```text
Notification
  id
  created_at
  updated_at

  source_type
  source_id

  category
  severity

  title
  body
  rich_content_json

  dedupe_key
  group_key

  read_at
  dismissed_at
```

---

## 48. Delivery Destination

Conceptual:

```text
NotificationDestination
  id
  type

  name
  enabled

  config_encrypted
  verified_at

  created_at
  updated_at
```

Types:

```text
desktop
mobile_push
email
webhook
```

---

## 49. Delivery

Conceptual:

```text
NotificationDelivery
  id
  notification_id
  destination_id

  status
  attempts

  queued_at
  last_attempt_at
  delivered_at

  error_code
  error_message
```

---

# Part XV — V1 Scope

## 50. Include

Gjallarhorn V1 should include:

- durable notification records,
- in-app Notification Center,
- unread/read state,
- dismissal,
- notification categories,
- severity,
- desktop OS notifications,
- mobile push architecture,
- SMTP email,
- webhooks,
- delivery records,
- delivery retries,
- deduplication,
- grouping,
- quiet hours,
- automation notification policies,
- failure notifications,
- per-automation channel selection,
- basic global preferences,
- audit/diagnostic state.

---

## 51. Explicitly Out of Scope for V1

Do not require:

- SMS,
- Slack,
- Teams,
- Discord,
- Home Assistant,
- complex digest scheduling,
- escalation trees,
- acknowledgment workflows,
- arbitrary user-written notification templates,
- notification marketplace,
- rich mobile actions,
- guaranteed remote retrieval of private push content,
- mandatory Toskar cloud accounts.

These can be added later through providers/plugins.

---

# Part XVI — Suggested Implementation Phases

## 52. Phase G0 — Core Notification Store

Implement:

- Notification model,
- category/severity,
- durable persistence,
- unread/read state,
- source linkage,
- in-app list API.

Success condition:

A scheduler run can create a durable notification.

---

## 53. Phase G1 — Notification Center

Implement:

- bell indicator,
- unread count,
- list/detail view,
- mark read,
- dismiss,
- source navigation.

---

## 54. Phase G2 — Desktop Delivery

Implement native desktop notification providers.

Support:

```text
macOS
Windows
Linux where practical
```

---

## 55. Phase G3 — Email and Webhooks

Implement:

```text
SMTP email
signed HTTPS webhooks
```

Add:

- per-destination configuration,
- delivery records,
- retry logic.

---

## 56. Phase G4 — Scheduler Integration

Add automation policies:

```text
always
condition true
result changed
store only
failure
```

Add per-automation channel selection.

---

## 57. Phase G5 — Quiet Hours, Deduplication, Grouping

Implement:

- quiet hours,
- severity exceptions,
- dedupe keys,
- grouped repeats,
- state-change notification patterns.

---

## 58. Phase G6 — Mobile Push

Implement:

- device registration,
- push relay,
- APNs delivery,
- push destination management,
- privacy mode decision.

---

## 59. Phase G7 — Cross-Subsystem Integration

Integrate:

```text
Heimdall
Gungnir
model management
training
Grid
downloads
system updates
```

---

## 60. Phase G8 — Provider/Plugin Expansion

Future providers:

```text
SMS
Slack
Discord
Teams
Home Assistant
other messaging systems
```

---

# Part XVII — Acceptance Criteria

## 61. Core Acceptance Criteria

The feature is complete when:

1. Toskar can persist a notification independently of delivery.
2. Notifications appear in an in-app Notification Center.
3. Users can mark notifications read.
4. Users can dismiss notifications.
5. Notifications retain source linkage.
6. Desktop notifications can be delivered where supported.
7. SMTP email delivery works.
8. Webhook delivery works.
9. Delivery success/failure is tracked separately per channel.
10. Failed deliveries can retry without rerunning the originating task.
11. Automations can notify always.
12. Automations can notify only when a condition is true.
13. Automations can notify only when a result changes.
14. Automations can store results without notifying.
15. Automations can notify after repeated failures.
16. Quiet hours suppress or defer configured deliveries.
17. Repeated identical events can be deduplicated/grouped.
18. Notification secrets are not exposed to models.
19. Hosted notification services are optional for local operation.
20. The architecture supports mobile push without placing scheduler logic inside the mobile app.

---

## 62. Mobile Push Acceptance Criteria

Mobile push is complete when:

1. Toskar Mobile can register a push destination.
2. Core can request push delivery through the relay.
3. Push delivery does not require an inbound connection to Core.
4. APNs credentials are never exposed to Core users/models.
5. Push destination revocation is handled.
6. Push delivery is represented in the normal delivery record model.
7. Mobile push can be disabled without affecting local notifications.

---

# Part XVIII — Product Outcome

The target experience is:

```text
User creates automation
      ↓
Automation runs unattended
      ↓
Something important happens
      ↓
Gjallarhorn creates one durable notification
      ↓
User receives it where they prefer
      ↓
Notification remains available later
```

Example:

```text
Check package every hour
      ↓
No change
      ↓
No notification

Later:
Status changes to "Out for delivery"
      ↓
Gjallarhorn
      ├── stores notification
      ├── sends push
      └── shows it in Notification Center
```

The user should not need to understand APNs, SMTP, retry queues, webhook signatures, or delivery-state machines.

> **Toskar decides when something matters. Gjallarhorn makes sure the user hears about it.**
