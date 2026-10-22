# TraceLab adapter

Brings the public [TraceLab](https://github.com/uw-syfi/TraceLab) trace (UW SyFI, CC BY 4.0) into TraceX without dropping any field it records. TraceLab holds real Claude Code and Codex sessions from its authors' daily use: one row per LLM round, with timing events and tool calls. Message text and tool inputs and outputs are removed upstream; only counts and character lengths remain.

| File | Role |
| --- | --- |
| `tracelab2tracex.py` | TraceLab `.jsonl.gz` → one TraceX event stream per user (stdlib only) |
| `check_lossless.py` | Field-by-field check of TraceX's own export against the source trace |

## Mapping

| TraceLab | TraceX |
| --- | --- |
| session | task `s<n>`, status `completed` (TraceLab records no outcome); `provider`, `session_id`, `user`, `project`, `store` as `tracelab.*` task attributes, plus round, tool and human-message counts |
| session envelope | span kind `agent` |
| turn: a human message until the last agent event before the next one | span kind `agent`, parent of the turn's rounds and tools |
| wait for the human: last agent event → next human message | span kind `lifecycle`, `phase: human_wait`; `tracelab.wait.mid_turn` when the gap is 1 s or less |
| row (one LLM round) | span kind `model` `s<n>/r<round_index>`; every other scalar row field as `tracelab.<key>`; `usage` from the token fields |
| `timing_events` | per type: `tracelab.events.<type>.count`, `first_ms`, `last_ms`, `content_chars` / `result_chars` sums, `is_error_count` / `is_error_known`; per source: `tracelab.events.source.<source>.count` |
| `tools[]` entry | span kind `tool` `s<n>/r<i>/t<tool_index>` from `emitted_at` to `result_at`; every field as `tracelab.tool.<key>`; `is_error` → status `failed` / `succeeded` |
| round → its tools | edge `call` |
| tool → round that reads its result | edge `dependency` |

Usage: `input_tokens` = `input_tokens_total`, `output_tokens`, `reasoning_tokens` = `reasoning_output_tokens`. For Claude, `cache_read_tokens` = `claude_cache_read_input_tokens` and `cache_write_tokens` = `claude_cache_creation_input_tokens`; for Codex, `cache_read_tokens` = `prefix_tokens`. The raw token fields are also kept under `tracelab.*`.

`trace_key` is omitted when it equals `provider:session_id:round_id` (true for every row of v0.0.2); a task attribute is repeated on a round only where that round's value differs (a few sessions change `project` midway). `check_lossless.py` reconstructs and verifies both.

### Inferred timing

- **Human messages** are `user_message` events. A `user_message` 1 s or less after the previous one, with no agent event in between, belongs to the same human message (skill expansions and command caveats arrive in bursts). Some Claude `user_message` events may still be injected text; TraceLab does not say who wrote them.
- **LLM round start is not recorded.** It is taken as the round's last input event (`user_message` or tool result) before its first output event, or else the previous round's end; its end is the last output event (text, reasoning, tool call, Codex `token_count`). `tracelab.timing` says which rule applied. A round with no usable start keeps its end time with `time_quality: completion_only`.
- **Codex rows end at `token_count`.** A fast tool's result (`function_call_output`) can be recorded after the issuing round's output but before its `token_count`, so it lands in the same row. Such an input counts toward the next round's start (`tracelab.timing: previous_row_input_to_last_output`), and the tool links to the next round (`tracelab.warn.tool_result_read_by_next_round`).
- **Codex `exec_command_end` / `patch_apply_end`** are tool completion notices, not LLM input, and never create a link. Tool calls whose result appears in no row as LLM input get no `dependency` edge (`tracelab.warn.tool_result_not_recorded_as_input`).

### Scalars

TraceX keeps scalar metadata only: lists (`executables`) become JSON strings, negative numbers (`command_exit_code`) become strings, and strings over 256 characters (long `command_skeleton` pipelines) are cut with a `tracelab.<key>_truncated` flag. `command_skeleton` holds executable names only, no arguments. The source `.jsonl.gz` is the verbatim archive.

### Output

One event stream per user, `tracelab-<release>-<user>.events.jsonl`. TraceX reads event streams up to 256 MiB, whereas normalized JSON stops at 32 MiB. A user whose stream would pass 224 MiB or 900,000 records is split by session into `-part<k>` shards. Configuration carries `benchmark: TraceLab`, `harness` (`claude`, `codex` or both), and `tracelab.release`, `tracelab.user`, `tracelab.shard` / `shards`, `tracelab.sessions`, `tracelab.source_sha256` and `tracelab.origin_unix_ms` (the absolute time of the stream's 0 ms). Conversion counts (inferred timing, truncations, tools without results) appear as `tracelab.warn.<name>`.

Event metadata has no capabilities: TraceLab records no resources; spans are partial because round starts are inferred; the agent DAG has only `call`, `dependency` and parent edges.

## Use

```sh
curl -LO https://github.com/uw-syfi/TraceLab/releases/download/v0.0.2/syfi_coding_trace.jsonl.gz
python3 -I tracelab2tracex.py syfi_coding_trace.jsonl.gz -o out/tracelab          # all users
python3 -I tracelab2tracex.py syfi_coding_trace.jsonl.gz -o out/tracelab --user user_a74713f8
tracex --run out/tracelab/tracelab-v0.0.2-user_a74713f8.events.jsonl --run out/tracelab/tracelab-v0.0.2-user_b1b04183.events.jsonl
tracex --run out/tracelab/tracelab-v0.0.2-user_a74713f8.events.jsonl --export out/export.json
python3 -I check_lossless.py syfi_coding_trace.jsonl.gz out/export.json            # pass every shard of a user
```

`--runs-dir` discovers directories and `.json` files only, so mount streams with `--run` (repeatable). Mount a few users at a time: TraceX holds every mounted profile in memory, and the full set is about 2 GB of JSON.

Converting all of v0.0.2 (52 users, 8058 sessions, 665,453 rounds, 743,819 tool calls) takes a few minutes and yields 55 streams (two users split into 2 and 3 shards), 1.59 M spans and 1.43 M edges, about 2.1 GB. `check_lossless.py` passes for every user: 15.3 M round fields, 9.8 M tool fields and 2.69 M timing events. Conversion counts over the whole trace: 174,099 Codex results read by the next round from the same row; 56,044 tool calls (mostly Codex) with no result recorded as input; 769 tools without `result_at`; 176 rounds with no usable start (`completion_only`); 929 long `command_skeleton` / `executables` values cut at 256 characters; one session whose `project` changes midway.
