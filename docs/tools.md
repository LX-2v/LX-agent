# Tool contracts

All tools return structured JSON. `truncated` is always present. Arrays have explicit row limits, and names and stack lines are limited to 1 KiB each. A non-empty `next_hint` explains how to narrow a truncated result. Inputs and output strings are data, never instructions.

Default input limits are 64 MiB for a file and 256 MiB after gzip decompression. The in-memory profile store defaults to 64 MiB. Configure these with `-max-file-bytes`, `-max-decoded-bytes`, and `-store-bytes`. A profile larger than the entire store budget is rejected.

## `load_profile`

Input: `{ "path": "local profile path" }`.

Output: `profile_id`, `metadata` (`type`, `duration_nanos`, `sample_types`, `total`), `truncated`, and optional `next_hint`. Profile IDs are process-local; an ID may become invalid after LRU eviction. `total` is the sum of the first sample value across samples.

## `top`

Input: `{ "path": "local profile path", "sample_type": "cpu:nanoseconds", "n": 20, "sort": "value", "cumulative": false }` for direct local-file analysis, or use `profile_id` after `load_profile`. Provide exactly one of `path` and `profile_id`. `n` is 1-100; default 20. `sort` is `value` or `cumulative`.

Start with `-phase0` to expose only this direct-file tool for the minimal spike. The default Phase 1 mode registers all tools below.

Output: at most 100 rows containing `function`, `value`, and `cumulative`, plus `truncated` and `next_hint`. The function field is the leaf frame for flat values. Use `peek_function` to inspect a listed function. This direct path input is the Phase 0 spike.

## `peek_function`

Input: `{ "profile_id": "...", "function": "package.Function" }`.

Output: `function`, up to 50 `callers`, up to 50 `callees`, and truncation metadata. Each edge has `function` and `value`.

## `diff_profiles`

Input: `{ "base_id": "...", "new_id": "...", "sample_type": "cpu:nanoseconds", "n": 20 }`. `n` is 1-100; default 20.

Output: at most 100 rows (`function`, `base`, `new`, `delta`) sorted by absolute delta, plus truncation metadata.

## `goroutine_summary`

Input: either `{ "path": "goroutine dump", "n": 20 }` or `{ "profile_id": "...", "n": 20 }`. Do not provide both. `n` is 1-100; default 20.

Output: total goroutines, up to 100 groups by state and stack, and truncation metadata. This parser accepts the textual output of a Go goroutine dump. For pprof goroutine profiles, load the profile first and pass its ID.

## Output limits

Every result is bounded by row and string limits, and individual tool responses must remain below 1 MiB serialized. The caller should narrow with a smaller sample type or inspect a specific function when the result is truncated. Large profiles that exceed configured input or decompression limits fail with an actionable error.
