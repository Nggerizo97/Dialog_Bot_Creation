# ADR 0004: Organizational knowledge and AI nodes

## Status

Proposed

## Context

Rule-based bots answer only from hand-built flows and fuzzy keyword matching. Areas want bots that answer from the organization's own knowledge (policies, product sheets, procedures) and give better answers than the flows alone. Each area's knowledge must stay private to that area (ADR 0003), and bots in regulated businesses must not invent answers or act on instructions hidden in documents.

## Decision

### Retrieval, not training

The platform uses **retrieval-augmented generation (RAG)**. It does not fine-tune or train a model per area. At answer time, the bot retrieves the most relevant passages from the workspace's knowledge base and the model answers only from them, with citations.

| | RAG (chosen) | Fine-tuning per area |
|---|---|---|
| Privacy | One shared model; each area's data stays in its own index | One model per area, or areas' data mixed in training |
| Updating | Immediate when a document is re-ingested | Retrain each time |
| Traceability | Every answer cites its sources | No sources |
| Cost | Embedding plus a model call per answer | Training runs plus hosting |

Fine-tuning can be reconsidered later for narrow tasks such as classification at very high volume. It is not needed for answering questions.

### Knowledge bases

- A workspace can own several knowledge bases, such as "HR policies" and "Credit products". A Knowledge node names the ones it may use.
- Platform admins may publish an organization-wide knowledge base, such as public policies, that every workspace can read but not change.
- Each knowledge base records its `embedding_model`, dimensions and chunking settings. Changing the embedding model creates a new index version and re-embeds everything; the active version switches only after the eval passes.

### Ingestion pipeline (asynchronous)

`upload to S3 (workspaces/{id}/kb/{kb_id}/…)` → `extract text` → `clean and redact PII (policy per workspace)` → `chunk by heading, ~500–800 tokens with overlap` → `embed` → `upsert vectors with metadata` → `activate the document version`.

- Vector metadata includes `workspace_id`, `kb_id`, `doc_id`, `doc_version`, `chunk_no`, `title`, `source_url` and `valid_from`/`valid_to`.
- Updating a document replaces its vectors. Deleting a document deletes its vectors.
- Text extraction from PDF and DOCX may run as a Python worker, which keeps Python limited to ML work. Everything else stays in Go.

### Retrieval

Hybrid search: vector similarity plus keyword search (Postgres full-text search), merged, then optionally reranked. Take the top 5–8 chunks. **If no chunk reaches the knowledge base's minimum score, the model is not called** and the flow takes its no-answer branch. This prevents invented answers and saves the cost of the call.

### New node types

| Node | Behaviour | Outgoing branches |
|---|---|---|
| **Knowledge** | Retrieves from the configured knowledge bases and generates a grounded answer with citations. Citations are rendered per channel, for example as numbered sources on WhatsApp | `answered`, `no_answer`, `handoff` |
| **AI Router** | Classifies the user's free text into the node's own outgoing branches, using each branch's label and description, with a confidence score. Replaces fuzzy keyword matching for open questions | one per declared branch, plus `unclear` below the threshold |
| Extract *(later)* | Fills flow variables from free text (slot filling) | `complete`, `incomplete` |

AI calls are **effects**, consistent with ADR 0001. The engine's step function stays pure: it emits an `ai.answer` or `ai.route` effect, the AI service runs it, and the result returns as an event. Conversations can then be replayed in tests with recorded AI results.

### Models

- The model sits behind an `LLM` port, so the provider can change without touching the engine.
- **Default provider:** Claude on Amazon Bedrock. Prompts and documents stay inside the company's AWS account and are governed by IAM.
- **Default model:** Claude Opus 5.5 (`claude-opus-5-5`; on Bedrock `anthropic.claude-opus-5-5`). Set `effort` explicitly per node type: `low` for the AI Router and `medium` for Knowledge answers. Effort is the first lever for trading quality against cost on the same model.
- Moving the router to a cheaper model (Claude Sonnet 5.5 or Claude Haiku 4.5) is a cost decision. It should be made only after the eval shows routing quality holds, and is an open item.
- **Citations:** retrieved chunks are passed to the model as document blocks with citations enabled. The response returns the cited spans, which are mapped back to `doc_id` and `source_url`. Citations cannot be combined with the structured-output format option, so the Knowledge node uses citations and the AI Router uses structured output.
- **Refusals:** the AI service checks `stop_reason` before reading content. A `refusal` leads to the node's `no_answer` branch, never to an empty reply. On Bedrock, refusal fallback is configured with the SDK's client-side middleware.
- **Embeddings:** Anthropic does not offer an embedding model. Use a multilingual embedding model available on Bedrock, such as Amazon Titan Text Embeddings V2 or Cohere Embed Multilingual. It must handle Spanish well; confirm with the eval.

### Prompt layout and caching

The system prompt is assembled in a fixed order so the stable part can be cached:

1. Platform guardrails (same for everyone)
2. Workspace persona and rules
3. Bot and node instructions

Retrieved chunks and the user's message come **after** the cache breakpoint. Nothing variable (timestamps, request IDs) may appear in the cached part, because any change to it invalidates the cache. Caches are per model and per exact prefix, so each workspace and bot gets its own cache. High-traffic bots benefit most.

### Guardrails

- **Documents and user text are data, not instructions.** Knowledge and Router nodes expose no tools to the model, so text hidden in a document cannot trigger a transaction. Actions happen only in Service nodes that the flow designer placed.
- **Grounding:** answers come only from retrieved sources. Low retrieval scores skip the model call, as described above.
- **PII:** redaction before text reaches the model, where the workspace policy requires it. Redactions are logged.
- **Scope:** each workspace has a list of allowed and blocked topics. Off-topic questions take the `handoff` or `no_answer` branch.
- **Budgets:** monthly token budgets and rate limits per workspace. Usage events feed analytics, so the cost of each area is visible and can be charged back.

### Quality

- Each knowledge base has a golden set of questions with expected answers and expected sources.
- The eval runs on every document change, prompt change and model change, and blocks activation if quality drops.
- Metrics: answer correctness (judged by a model, plus sampled human review), citation precision, no-answer rate and unsupported-claim rate.

## Consequences

- A new `knowledge` service (ingestion plus retrieval) and an `ai` service (model calls) join the deployables. Both are scoped by workspace on every call.
- The contracts gain Knowledge and AI Router node definitions and the AI effect and event messages.
- The vector store sits behind a `VectorIndex` port. pgvector comes first and S3 Vectors later (ADR 0005).
- Every answer carries its sources, so wrong answers can be traced back to a document and fixed there.
