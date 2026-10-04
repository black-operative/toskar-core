# Toskar — Train Your Own AI

## Status

Implemented for Apple Silicon. This document is the V1 specification; the notes below record how it was built and what is still open.

- **Knowledge** is Mimir (`internal/mimir`), a new subsystem shared with profiles. The Train page and the Profiles editor attach existing sources, files, or folders. It indexes files, folders, and pasted content with SQLite FTS5 keyword search. A table row is one passage labelled with its columns, so a lookup by SKU or size finds that row. File and folder sources reindex when their files change.
- **Base models** are the catalog GGUFs with a `training` block: the Hugging Face weights the GGUF was converted from, a 4-bit MLX copy for QLoRA, license, and shape. Twelve Qwen 2.5, Llama 3.2, DeepSeek R1 distill, and Mistral models are trainable.
- **Trainer** is `training.Trainer`. The MLX backend runs mlx-lm in a Python environment the daemon installs with uv (`internal/pyenv`), then writes the adapter as a GGUF LoRA. The PEFT backend trains on NVIDIA GPUs: PyTorch and PEFT in their own environment (uv's `--torch-backend=auto` picks the CUDA build), LoRA in bf16 and QLoRA with bitsandbytes 4-bit weights, exporting the same GGUF LoRA.
- **Serving**: llama-server loads each deployed adapter for a base model at scale 0, and every request sets the scales, so the base model and each specialized AI share one process. llama-server applies a loaded adapter when a request omits the scale, so the client always sends it.
- **Training fit** estimates memory from weights, adapter and optimizer state, activations, and logits, calibrated against MLX runs; downloads; disk; and time. When a preset does not fit it tries QLoRA, then a batch of one with gradient checkpointing, then fewer layers.
- **Placement**: Norn estimates the fit on this computer and on each online paired computer (asking the peer over Bifrost whether its trainer environment and weights are already there) and picks the one with the most memory headroom, staying local on a near tie. The Review step can send the job to another eligible computer instead. A remote run receives the examples, trains in that computer's single training slot, reports progress, and hands back the adapter, which is evaluated and served on the computer that owns the AI. Cancel reaches the remote process; a computer that stops answering for 90 seconds fails the job.
- **Evaluation** runs the test prompts through the base model and the specialized AI with the same instructions and knowledge. Default prompts are questions held out of training. A revision must be evaluated before it can be deployed.

Open items:

- NVIDIA training (PEFT) was verified on Apple MPS against llama.cpp; the CUDA path has not yet run on NVIDIA hardware.
- Mimir searches by meaning when an embedding model is installed. PDFs with a text layer are read with the pure-Go `github.com/ledongthuc/pdf` (BSD-3); scanned pages are read with RapidOCR (Apache-2.0) in a managed Python environment installed on first use. Excel workbooks (`.xlsx`) are read without a new dependency: each sheet is a table, and training reads the first sheet as a Q&A table when it has question and answer columns.
- Mimir sources can be a read-only SQL query (SQLite, PostgreSQL, or MySQL, with the pure-Go `jackc/pgx` (MIT) and `go-sql-driver/mysql` (MPL-2.0) drivers) or a web API returning JSON, CSV, or text. They are fetched again when a search uses data older than the source's refresh interval.
- A revision can be exported as a standalone GGUF: `llama-export-lora` merges the adapter into the base model, writing the changed tensors as F16 and keeping the rest quantized. Instructions and knowledge are not part of the file.
- Store builds of the desktop app cannot run a downloaded Python. Core detects the sandbox, uses Python environments bundled beside the daemon, and otherwise offers a paired computer for training (#95, `docs/runtimes.md`). Bundling the environments in the store build is desktop packaging work.

## Specialized Model Training & Knowledge — Feature Specification V1

### Goal

Let power users and small organizations create inexpensive, hyper-specialized local AI assistants without requiring machine-learning expertise.

Toskar should teach users the difference between **training model behavior** and **connecting changing knowledge**, then guide them toward the correct approach.

## Product Principle

Users should describe the AI they want to build, provide their material, and let Toskar handle the implementation details.

The product may legitimately present this as **Train Your Own AI**, while transparently explaining which information is fine-tuned into the model and which information should remain connected knowledge.

The objective is not to expose LoRA hyperparameters. The objective is to help a non-ML expert produce a useful specialized AI.

## Core Educational Model

Make two concepts visually distinct:

### Train how your AI behaves

Use examples to teach:

- role,
- terminology,
- response patterns,
- workflows,
- tone,
- specialized task behavior.

Toskar may use LoRA/QLoRA fine-tuning.

### Connect what your AI knows

Attach information that changes or must remain authoritative:

- inventory,
- prices,
- SKUs,
- databases,
- product catalogs,
- policies,
- documents,
- APIs,
- other business data.

Toskar retrieves this information when needed.

Suggested explanation:

> Training teaches your AI how to do its job. Connected knowledge gives it the current information it needs to do that job.

## Example — Tire Business

A tire business wants a small local assistant that answers customer questions accurately without paying for a large hosted model.

Training examples can teach the assistant how to:

- ask for vehicle year/make/model,
- interpret customer requests,
- use tire terminology,
- explain fitment,
- format recommendations.

Current tire inventory, SKUs, prices, availability, and changing fitment/catalog data should normally remain connected knowledge rather than being baked into model weights.

The deployed assistant combines:

- a small base model,
- optional fine-tuning,
- current business knowledge.

## Guided Build Flow

1. **Describe the AI** — ask what the user wants the specialized assistant to do.
2. **Choose a base model** — recommend based on task, license, hardware, training fit, and deployment target.
3. **Add material** — examples, chats, documents, spreadsheets, structured datasets, and supported knowledge sources.
4. **Classify material** — recommend Training, Knowledge, or Both for each source.
5. **Review plan** — show what will be fine-tuned and what will remain connected.
6. **Prepare** — validate/clean training examples and configure knowledge ingestion.
7. **Train** — run fine-tuning on a suitable node.
8. **Evaluate** — compare base vs specialized model on representative test prompts.
9. **Deploy** — save the resulting specialized AI as a reusable Toskar model/profile/assistant.

## Intelligent Data Guidance

Toskar should proactively detect likely misuse rather than silently accepting every file as training data.

- Frequently changing structured data should usually be recommended as Connected Knowledge.
- High-quality input/output examples or conversations can be recommended as training examples.
- Mixed sources can be split between Training and Knowledge.
- Recommendations should be user-reviewable.
- Advanced users may override when technically supported.

## Training Scope

V1 training should focus on parameter-efficient fine-tuning such as LoRA/QLoRA rather than foundation-model pretraining from scratch.

Training targets include:

- task behavior,
- domain terminology,
- response structure,
- classification/extraction patterns,
- style,
- specialized workflows.

Expose simple presets:

- Quick
- Balanced
- Highest Quality

Keep raw hyperparameters behind Advanced mode.

Initially store customization as a base-model + adapter relationship. Merging/export can be added where supported.

## Knowledge Scope

Integrate with Mimir or the existing Toskar knowledge/RAG layer.

Support useful sources incrementally:

- documents,
- CSV/spreadsheets,
- product catalogs,
- local folders,
- structured data,
- later database/API connectors.

Knowledge should be refreshable without retraining the model.

## Hardware & Training Fit

Training fit must be calculated separately from inference fit.

Estimate before starting:

- required memory,
- storage,
- likely duration,
- eligible Toskar nodes.

Norn should select a capable training node.

Initial implementation should train on one node. Distributed training is out of scope for V1.

## Trainer Abstraction

Keep backend-specific implementation behind a common Trainer interface.

Potential backends include:

- MLX-based training on Apple Silicon.
- PyTorch/PEFT-compatible training on supported GPU platforms.
- Additional optimized backends later.

The UI/job model should not depend directly on one trainer implementation.

## Dataset Preparation

Dataset preparation is part of the product.

Allow users to create training material from:

- existing chats,
- pasted examples,
- imported JSONL,
- documents,
- other supported sources.

Validate formatting, detect obvious duplicates, flag low-quality/empty examples, and let users review/edit/remove examples.

## Training Experience

Show clear states:

- queued,
- preparing dataset,
- loading model,
- training,
- evaluating,
- exporting,
- complete,
- failed,
- cancelled.

Display:

- elapsed time,
- estimated remaining time when reliable,
- node/device,
- progress/epochs,
- useful training metrics.

Allow cancellation and clean up resources on failure/cancel.

## Evaluation

Evaluation is required before deployment.

Provide side-by-side **Base vs Specialized** responses.

Allow users to create a small test set and rerun it after changes.

Do not imply that lower training loss automatically means better real-world behavior.

## Deployment

After successful evaluation:

- let the user name and save the specialized AI,
- associate it with its base model,
- preserve adapter/version,
- preserve profile/system instructions,
- preserve connected knowledge sources,
- make it usable through normal Toskar chat/API surfaces,
- support versioning so retraining creates a new revision.

## UX Language

Primary language should emphasize outcomes:

- Build
- Train
- Knowledge
- Examples
- Test
- Deploy

Explain LoRA, QLoRA, RAG, embeddings, epochs, and similar terminology only when the user opens advanced information.

## V1 Scope

- Dedicated Train/Customize tab.
- Guided specialized-AI creation flow.
- Base-model recommendation.
- Training-vs-knowledge education and source classification.
- LoRA/QLoRA-style fine-tuning through supported trainers.
- Dataset import/preparation and review.
- Separate training-fit estimate.
- Single-node training scheduling.
- Progress/cancel/failure cleanup.
- Base-vs-specialized evaluation.
- Deployment as a reusable specialized AI.
- Connection to existing Toskar knowledge/RAG capabilities.

## Explicitly Out of Scope for V1

- Training foundation models from scratch.
- Large-scale distributed training.
- Automatic creation of a perfect dataset from arbitrary raw data.
- Complex AutoML/hyperparameter search.
- Hosted GPU marketplace.
- Guaranteed factual correctness solely because a model was fine-tuned.
- Duplicating Mimir/RAG infrastructure inside the training subsystem.

## Acceptance Criteria

1. A non-ML user can create a specialized AI without configuring raw training hyperparameters.
2. The UI clearly teaches the difference between training behavior and connecting knowledge.
3. Toskar warns when frequently changing data appears better suited to connected knowledge.
4. Users can review Toskar’s classification before training.
5. Training fit is evaluated independently from inference fit.
6. A supported training job can run, report progress, be cancelled, and clean up resources correctly.
7. The base model remains available after customization.
8. Users can compare base and specialized behavior before deployment.
9. Changing connected business data does not require retraining.
10. A completed specialized AI can be saved and used through normal Toskar interfaces.

## Product Outcome

The feature succeeds when a user can arrive with a business problem—not ML expertise—and leave with a small, specialized local AI that has learned the right behavior while retaining access to current authoritative data.
