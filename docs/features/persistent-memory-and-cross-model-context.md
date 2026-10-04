# Toskar Persistent Memory & Cross-Model Context

## Feature Specification — V1

### Goal

Make Toskar feel like a persistent AI service rather than a collection of isolated local models. Conversations and durable user memory belong to Toskar, so context can survive model stops, application restarts, and model changes.

## Core Principle

Models are interchangeable inference engines. Toskar owns persistence.

Toskar should maintain two layers of context:

1. **Conversation context** — messages and working state for the current conversation.
2. **Persistent user memory** — a lightweight set of durable facts, preferences, project information, and other user-approved metadata.

This enables cross-model continuity: a conversation started with one model can continue with another because Toskar reconstructs and sends the context.

## User Experience

### Chat Memory Control

Add a simple Memory control to the chat interface.

- **Memory On** — use the current conversation plus relevant persistent user memory.
- **Memory Off** — use the current conversation, but do not inject persistent personal memory.

Memory Off does not delete memory. It only prevents persistent memory from being included in that conversation.

### Memory Management

Provide a Memory settings area where users can:

- Enable or disable memory by default.
- View what Toskar currently remembers.
- Add a memory manually.
- Edit incorrect or outdated memories.
- Remove a memory.
- Enable or disable memory categories.
- See enough source/provenance information to understand where a memory came from.

### Explicit “Remember This” Action

Add a user-controlled action such as **Remember this** to a message/context menu.

## Memory Categories

Suggested initial categories:

- Identity
- Preferences
- Projects
- Technical
- Interests
- People & Relationships
- Other

## Context Assembly

Do not inject the entire memory store into every request.

When Memory is On, construct model context from:

1. System/profile instructions.
2. A small set of core user facts, if configured.
3. Relevant persistent memories for the current request.
4. Current conversation context.
5. The newest user message.

V1 can use simple category/keyword/relevance rules. More sophisticated semantic retrieval can come later.

## Long Conversation Handling

Conversation persistence and context-window size are separate concerns.

- Preserve the full conversation in Toskar.
- Keep recent turns verbatim.
- Summarize older portions when required by the active model’s context window.
- Never delete original persisted history merely because a summary is used for inference.
- Allow different models to receive different-sized assembled context.

## Cross-Model Continuity

Memory and conversation history must be model-independent.

Example:

- Chat with Qwen.
- Stop Qwen.
- Restart Toskar later.
- Start Llama.
- Continue the same conversation.

The new model receives Toskar-owned context.

## Storage Design

Use the existing lightweight local persistence mechanism where practical.

Conceptual memory record:

```text
id
category
content
created_at
updated_at
source_type
source_reference (optional)
enabled
confidence/status (optional, future)
```

SQLite or another existing local store is sufficient for V1. A dedicated vector database is not required.

## Privacy and User Control

- Memory should be local-first.
- Memory Off must prevent persistent personal memory from being injected.
- Users must be able to inspect, edit, and remove memories.
- Do not silently treat every statement as a permanent fact.
- Avoid storing credentials, secrets, tokens, and similarly sensitive transient data as normal memory.
- If a remote/cloud model is used later, clearly define whether persistent memory will be sent.

## Automatic Memory — Later Phase

Automatic extraction is not required for V1.

A later Muninn phase can:

- identify candidate durable memories,
- deduplicate them,
- update superseded facts,
- attach provenance,
- retrieve them by relevance.

## Suggested Internal Ownership

Treat persistent memory/context as a dedicated Toskar subsystem.

**Muninn** is the appropriate internal/product name for this layer.

## V1 Scope

Include:

- Persistent conversation history.
- Lightweight structured persistent memory.
- Memory On/Off control in chat.
- Memory settings page.
- Manual add/edit/remove memory.
- User-selectable memory categories.
- Explicit Remember this action.
- Cross-model conversation continuity.
- Bounded context assembly.
- Basic long-conversation summarization or truncation strategy.

Do not require:

- Dedicated vector database.
- Fully automatic memory extraction.
- Complex embeddings infrastructure.
- Cross-device cloud synchronization.
- Distributed memory replication.
- Predictive memory ranking.
- Perfect automatic conflict resolution.

## Acceptance Criteria

1. A conversation survives stopping and restarting its model runtime.
2. A persisted conversation can continue after switching to a different compatible model.
3. Memory On includes configured/relevant persistent memory.
4. Memory Off excludes persistent memory while preserving current-conversation behavior.
5. Users can view, add, edit, disable, and remove persistent memories.
6. Users can explicitly mark information to be remembered.
7. Persistent memory is not tied to one model/runtime.
8. The full memory store is not blindly injected into every prompt.
9. Conversation history remains persisted even when older content must be summarized.
10. No heavyweight infrastructure dependency is required solely for V1.
11. The UI clearly distinguishes persistent memory from ordinary conversation history.

## Product Outcome

The feature succeeds when users can stop a model, restart Toskar, switch models, and begin new conversations without feeling that the assistant has forgotten who they are.
