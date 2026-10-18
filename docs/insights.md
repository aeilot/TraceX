# Insights

The run workspace includes automatic diagnostics, model/tool aggregates, scoped interpretation and an investigation notebook. Overview shows the first diagnostic cards. Comparison includes task-paired regressions and an action to interpret the differences.

## Evidence and metric definitions

Diagnostics flag recorded failures; the longest timed span on an available critical path; tasks above twice the run median when at least five measured task durations exist; observed-work coverage below 70%; missing model usage; and CPU throttling of at least 10% of measured enforcement periods. These are inspection prompts, not causal conclusions. Unobserved time is never labeled waiting.

Aggregates exclude same-kind ancestor call envelopes to avoid counting both an enclosing call and its children. Models group by recorded model identity; missing identity is labeled explicitly, rather than inferred from run configuration. Tools use recorded tool name, falling back to span name. Timings with unknown or completion-only quality are excluded from duration statistics but remain in call counts. Measured zero tokens remain distinct from missing usage.

Summed work includes overlap. Union time removes overlap within one group; different groups can still overlap. P50/P95 use measured call durations with linear interpolation. Full-run results are cached per immutable snapshot revision and ignore search and pagination. Window/span scopes retain overlapping spans' full intervals, rather than claiming clipped call cost. Scoped reports omit whole-task coverage and slow-task findings. Group tables retain at most 200 groups and a few example evidence links.

Task regression pairs logical task IDs, falling back to exact occurrence IDs. Repeated occurrences remain in counts and use arithmetic means of measured durations and known scores. Missing sides and missing measurements remain visible; counts show occurrences, timed and scored samples. Different inputs or conditions can explain differences; these comparisons do not isolate causality.

## Optional LLM interpretation

Configure the server with a full OpenAI-compatible Chat Completions endpoint and model. API credentials stay on the server:

```sh
export TRACEX_INSIGHT_ENDPOINT=https://your-provider.example/v1/chat/completions
export TRACEX_INSIGHT_MODEL=your-model
# Set TRACEX_INSIGHT_API_KEY in the server environment if required.
./bin/tracex --addr 127.0.0.1:4320
```

The endpoint must use HTTPS, or HTTP on loopback for local providers. URLs with embedded credentials, query strings or fragments are rejected. The provider must support JSON object responses and `max_completion_tokens`; see the official [Chat Completions reference](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create).

Choose a full run, current time window, selected span interval or compared runs. Review the exact evidence preview and acknowledge sending it before pressing Interpret. There are no background requests. Raw prompts, logs, tool arguments and notebook text are excluded. Names, IDs, metrics and recorded comparison conditions are included and may still be sensitive.

The packet is bounded to 8 findings, 12 groups, 12 tasks, 12 comparison entries and 12 conditions, with capped example references and a 64 KiB request limit. Comparisons include baseline aggregates, paired task summaries and recorded conditions. The server permits one active interpretation, times out after 60 seconds, does not follow redirects or automatically retry, and caps output at 2,048 tokens. Cancellation cannot guarantee a provider request has not begun or incurred charges.

Output separates reported facts, hypotheses, next experiments and limitations. Evidence IDs are checked against the submitted packet; unknown citations and incomplete responses are rejected. Links navigate back to recorded evidence. The browser renders returned content as text. Local diagnostics work without any provider configuration.

`GET /api/insights` returns local evidence; `GET /api/insights/ai` returns readiness and public endpoint/model; `POST /api/insights/ai` requires same-origin JSON and explicit acknowledgment. Provider access is configured only at server startup.

## Investigation notebook

Save a finding or interpretation, or write a manual note. Notes retain the run, snapshot/scope version and evidence references; saved interpretations also retain model identity. Notes do not silently update when source evidence changes. Edit, remove with Undo, and export as JSON are available.

Storage is local to this browser origin, with at most 100 notes. Another port or browser has a separate notebook; clearing browser data removes it. Export important notes. Source evidence must still be mounted for links to resolve. Browser-imported evidence must be imported again after reload.

## Verification

Go tests cover diagnostic arithmetic, missing measurements, scope, comparison pairing, consent/origin checks, credential exclusion and citation validation. JavaScript tests cover imported profiles, paired task means and notebook recovery. A local synthetic provider fixture verified the complete browser interpretation/save/reload/edit/undo/evidence flow. No real provider was called during implementation.
