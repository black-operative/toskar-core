### Fixed

- Chats no longer pile up as active tasks. A chat's task was never moved past pending, so the Performance overview counted every chat ever sent as an active task and listed the oldest as Pending. A chat's task now ends completed, failed, or cancelled with the reply, and tasks left unfinished from before are settled when Toskar starts. Chats still raise no "task finished" notice.
