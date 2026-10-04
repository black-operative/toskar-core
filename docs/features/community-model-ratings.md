# Toskar Core — Community Model Ratings

## Feature Specification — V1

### Goal

Allow users to rate models with 1–5 stars and use those ratings to improve model recommendations. Ratings should be weighted toward **same or similar hardware**, because local-model quality, speed, stability, and usability depend heavily on the machine and runtime configuration.

The user-facing question is:

> **How well does this model work for people with hardware like mine?**

This feature should directly support Toskar's core product goal: making local AI as easy to choose and use as SaaS AI.

---

## 1. User Experience

A model card may show:

```text
Qwen3 Coder 30B Q4_K_M

★★★★☆ 4.6
18 ratings on hardware similar to yours

Typical speed: 18.7 tok/s
Successful starts: 96%

[Install]
```

If there is not enough similar-hardware data:

```text
★★★★★ 4.8
3 ratings on similar hardware

★★★★☆ 4.4
126 ratings overall
```

The UI must clearly distinguish **similar-hardware ratings** from **global ratings**.

---

## 2. Rating Interaction

The primary rating should remain simple:

```text
How well did this model work for you on this computer?

☆ ☆ ☆ ☆ ☆
```

Optional structured reasons may include:

- Great responses
- Fast
- Slow
- Stable
- Crashed or failed
- Used too much memory
- Great for coding
- Great for general chat
- Good tool use
- Poor tool use

A rating may also say the language the model was used in (multilingual spec §23):

```text
Language you used it in: Spanish
How well did it work for you in Spanish?
```

It starts as the App language, and the person can change it or choose not to say. It is sent only with a shared rating, and the public summary shows a model's ratings by language only with at least 3 of them, for everyone who runs the model the same way, never split by hardware.

Written public reviews are out of scope for V1.

---

## 3. What a Rating Applies To

A rating should be associated with enough context to avoid combining materially different configurations.

At minimum:

```text
model identity
model revision where available
model format
quantization
runtime family
runtime backend
hardware cohort
```

Example:

```text
Model: Qwen/Qwen3-Coder-30B
Format: GGUF
Quantization: Q4_K_M
Runtime: llama.cpp
Backend: Metal
Hardware: Apple M4 Max / 48 GB unified memory
```

A Q4_K_M rating should not automatically be treated as equivalent to Q8_0, nor should Metal, CUDA, ROCm, Vulkan, and CPU-only runs be assumed identical.

---

## 4. Hardware Cohorts

Exact-machine matching would fragment the dataset too much. Toskar should progressively widen the comparison.

Apple example:

```text
M4 Max + 48 GB
    ↓
M4 Max
    ↓
Apple Silicon + 32–64 GB unified memory
    ↓
Metal + comparable memory
    ↓
Global
```

NVIDIA example:

```text
RTX 4090 24 GB
    ↓
RTX 4090
    ↓
RTX 40-series + 20–28 GB VRAM
    ↓
CUDA + comparable VRAM
    ↓
Global
```

Equivalent cohort rules should exist for AMD, Intel, CPU-only systems, and future accelerators.

Suggested normalized hardware data:

```json
{
  "platform": "macos",
  "architecture": "arm64",
  "accelerator": {
    "vendor": "apple",
    "family": "m4-max",
    "memory_type": "unified",
    "memory_bucket_gb": "32-64"
  },
  "runtime": {
    "type": "llamacpp",
    "backend": "metal"
  }
}
```

Avoid unnecessary hardware fingerprinting.

---

## 5. Similarity Tiers

Internally, ratings may be classified as:

```text
Tier 0 — exact or near-exact hardware
Tier 1 — same accelerator model/family
Tier 2 — same accelerator class + memory band
Tier 3 — same runtime backend + comparable memory
Tier 4 — global
```

The UI should never imply strong hardware relevance when only global data is available.

---

## 6. Confidence and Sample Size

Do not sort models by raw average alone.

Example:

```text
Model A: 5.0 ★ (1 rating)
Model B: 4.8 ★ (412 ratings)
```

Model A should not automatically rank above Model B.

Use a confidence-aware method such as a Bayesian weighted average. The UI can still display the ordinary average and count.

Suggested confidence states:

```text
1–2 ratings    Limited data
3–9 ratings    Early community data
10+ ratings    Community rating available
```

Stronger labels such as **Recommended for your hardware** should require both sufficient sample size and acceptable stability/performance data.

---

## 7. Objective Runtime Observations

With explicit user permission, a rating may include anonymous runtime observations already known to Toskar:

- model startup success
- time to first token
- tokens per second
- context-size band
- peak memory estimate
- unexpected runtime exit
- OOM/memory-pressure event
- generation completion
- runtime version
- Toskar version

This can produce useful model details such as:

```text
User rating        ★★★★☆ 4.6
Median speed       18.7 tok/s
Median TTFT        1.4 sec
Successful starts  96%
OOM/crash rate     2%
```

Performance telemetry must remain optional.

---

## 8. Privacy

Submitting ratings must be explicit.

Suggested consent text:

```text
Share this rating with the Toskar community?

We'll share:
• your star rating
• the model and quantization
• general hardware class
• runtime/backend

We won't share:
• your prompts
• model responses
• files
• computer name
• username
• node ID
• network name
```

Never publish or intentionally submit as rating data:

```text
hostname
username
IP address
MAC address
node ID
prompt text
response text
file paths
conversation content
network name
API keys
join tokens
```

Viewing community ratings must not require users to contribute their own data.

---

## 9. Rating Identity and Updates

V1 should not require a GitHub account.

Use a random pseudonymous rating-client identifier so the service can support:

- one current rating per relevant configuration
- rating updates
- basic duplicate-vote resistance

Do not derive identity from machine serial numbers or other hardware identifiers.

A user who rates the same configuration again should update the existing rating rather than create unlimited duplicate votes.

---

## 10. When to Ask

Do not ask for a rating immediately after model installation.

Good triggers include:

- after several successful chats
- after meaningful usage time
- after a benchmark
- when stopping/uninstalling a model
- explicit **Rate model** action

Avoid repetitive prompts.

---

## 11. Recommendation Inputs

Stars should become one signal, not the entire recommendation engine.

Potential inputs:

```text
hardware fit
runtime compatibility
confidence-weighted community rating
observed stability
performance
task suitability
context requirements
license
```

The long-term result should support statements such as:

```text
Recommended for your computer
Highly rated on hardware like yours
Fast on similar systems
Stable on similar systems
Limited community data
```

Avoid calling something the **best model** solely because of star ratings.

---

## 12. Storage Architecture

### Do not use Git as the live transactional database

Git can technically store ratings, but it is a poor fit for frequent concurrent writes because of:

- commit volume
- repository history growth
- merge conflicts
- GitHub API limits
- moderation difficulty
- deletion/privacy problems
- slow aggregation
- schema evolution difficulty

Use a small transactional datastore for live writes.

Conceptually:

```text
Toskar Core
      │
      ▼
Community Ratings API
      │
      ▼
Live Database
```

Possible implementations include PostgreSQL, Supabase, Cloudflare D1, Turso, or another inexpensive relational datastore.

---

## 13. Git as the Public Dataset

Git should still play an important role.

Recommended architecture:

```text
Toskar Core
      │
      ▼
Community Ratings API
      │
      ▼
Live Database
      │
      ▼
Aggregator
      │
      ▼
yeixio/toskar-model-data
```

The Git repository contains **sanitized aggregate snapshots**, not the live write stream.

Recommended separate repository:

```text
yeixio/toskar-model-data
```

Possible structure:

```text
README.md
schema/
  ratings-v1.schema.json
ratings/
  summary.json
models/
  qwen3-coder-30b/
    q4_k_m.json
hardware/
  cohorts.json
snapshots/
  2026-09-28.json
```

Do not place high-frequency generated rating commits into `yggdrasil-core`.

---

## 14. Public Aggregate Format

Example:

```json
{
  "schema_version": 1,
  "model": "Qwen/Qwen3-Coder-30B",
  "quantization": "Q4_K_M",
  "cohorts": [
    {
      "hardware": "apple-m4-max-32-64gb",
      "ratings": 18,
      "average": 4.67,
      "weighted_score": 4.51,
      "median_tokens_per_second": 18.2,
      "successful_start_rate": 0.96
    }
  ]
}
```

Prefer aggregate data over public per-user records.

The dataset schema must be versioned.

---

## 15. Snapshot Generation

The ratings service should periodically generate Git snapshots:

```text
Live DB
   ↓
sanitize
   ↓
aggregate
   ↓
validate schema
   ↓
generate JSON
   ↓
commit with bot
```

A daily snapshot is sufficient initially.

The bot credential should be narrowly scoped.

---

## 16. Offline Behavior

Community ratings must never become a dependency for local inference.

If the hosted service is unavailable:

```text
Core
├── uses cached public snapshot
└── falls back to normal local model-fit metadata
```

The Git dataset provides a useful transparent fallback and cache source.

---

## 17. Abuse Controls

V1 should include basic protections:

- rate limiting
- one current vote per pseudonymous installation/configuration
- server-side schema validation
- impossible-value rejection
- optional minimum-use requirement before rating
- ability to invalidate abusive batches

More sophisticated anomaly detection may come later.

---

## 18. Community Ratings API

Conceptual hosted endpoints:

```text
POST /v1/ratings
PUT  /v1/ratings/{rating-key}
GET  /v1/models/{model}/ratings
```

The hosted service should remain separate from the local Core control API.

Conceptually:

```text
Local:
http://127.0.0.1:7331

Hosted:
https://ratings.toskar.ai
```

Core only needs outbound communication to participate.

---

## 19. Model Catalog Example

```text
Qwen3 Coder 30B Q4_K_M

★★★★☆ 4.6
18 ratings on similar hardware

Typical speed
18.7 tok/s

Stability
Very good

Fit
Good

[Install]
```

If there is insufficient data:

```text
★★★★☆ 4.4
126 ratings overall

Not enough ratings from hardware similar to yours yet.
```

---

## 20. Future Benchmark Integration

Later, combine community aggregates with the user's own benchmark:

```text
Community rating
★★★★☆ 4.7

Your benchmark
21.3 tok/s

Similar-system median
19.8 tok/s

Your system is performing
8% above similar systems
```

This may also help diagnose configuration or driver problems.

---

## 21. Future Recommendation Example

```text
Llama 70B Q4

Fit:
Tight

Community on similar systems:
★★★☆☆ 3.2
Often slow

Recommended alternative:
Qwen 32B Q5
★★★★★ 4.8
Faster and more stable on your hardware

[Install recommended model]
```

This is the long-term value of the feature: model selection becomes a Toskar decision rather than an infrastructure research project for the user.

---

## 22. V1 Scope

Include:

- 1–5 star ratings
- optional structured feedback tags
- model/quantization/runtime identity
- normalized hardware cohorts
- similar-hardware and global scores
- sample counts
- confidence-adjusted ranking
- explicit consent
- pseudonymous rating identity
- rating updates
- hosted Community Ratings API
- transactional live datastore
- cached/offline aggregate support
- public aggregate Git dataset
- versioned dataset schema
- basic abuse prevention

---

## 23. Explicitly Out of Scope for V1

Do not require:

- written public reviews
- social profiles
- GitHub authentication
- comments/followers
- public per-user histories
- task-specific star matrices
- complex reputation systems
- hardware serial numbers
- prompt/response collection
- mandatory telemetry
- ratings-service availability for normal Core operation

---

## 24. Suggested Implementation Phases

### Phase R0 — Schema

Define:

- canonical model identity
- quantization identity
- hardware cohorts
- runtime identity
- rating payload
- aggregate schema

### Phase R1 — Local UX

Implement:

- stars
- optional reason tags
- consent flow
- local rating state

### Phase R2 — Hosted Service

Implement:

- submission/update
- aggregation
- rate limiting
- cohort queries

### Phase R3 — Catalog Integration

Display:

- similar-hardware score
- global score
- sample count
- limited-data state

### Phase R4 — Public Dataset

Create:

```text
yeixio/toskar-model-data
```

with automated aggregate snapshots.

### Phase R5 — Optional Runtime Observations

Add opt-in:

- tokens/sec
- TTFT
- startup success
- OOM/crash observations

### Phase R6 — Recommendations

Use community data as one input to hardware-aware model recommendations.

---

## 25. Acceptance Criteria

The feature is successful when:

1. Users can rate a model from 1–5 stars.
2. Submission is optional.
3. Ratings are tied to the correct model and quantization.
4. Hardware is submitted only in normalized, privacy-safe form.
5. Prompts, responses, filenames, hostnames, usernames, and node IDs are not submitted.
6. Similar-hardware ratings are distinguishable from global ratings.
7. The UI displays sample counts.
8. Low-sample ratings do not dominate recommendations.
9. Users can update an existing rating.
10. Viewing ratings does not require contributing data.
11. The hosted service is not required for local inference.
12. Core can use cached aggregate data when the service is unavailable.
13. Live writes use a transactional datastore rather than Git.
14. Sanitized aggregate data can be published to a public Git repository.
15. The public dataset contains no per-user private information.
16. The dataset schema is versioned.
17. Basic rate limiting and abuse controls exist.
18. Community rating is one recommendation signal rather than the sole ranking factor.

---

## 26. Product Outcome

The feature succeeds when a user browsing models can quickly answer:

```text
Will this fit my computer?
Do people with hardware like mine like it?
How fast does it usually run?
Is it stable?
Is there a better option for my hardware?
```

without needing to understand local-AI benchmarking, quantization tradeoffs, VRAM planning, or runtime tuning.

The intended result is:

> **Toskar already knows what tends to work well on a computer like yours.**
