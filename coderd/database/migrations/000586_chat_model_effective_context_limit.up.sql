-- Chat model configs backed by a combo/route (e.g. an OmniRoute combo
-- fanning out to targets with different context windows) can have a
-- context_limit that only reflects the smallest/first configured target,
-- which is wrong for overflow/compaction decisions once a larger target
-- remains eligible for the request. effective_context_limit is an optional
-- override, sourced from the upstream provider's own capability metadata
-- (e.g. OmniRoute's own per-target resolution, exposed via its model
-- catalog) rather than duplicated/hardcoded in Coder. NULL means "no
-- override known -- fall back to context_limit", so every existing
-- single-model config is completely unaffected.
ALTER TABLE chat_model_configs
	ADD COLUMN effective_context_limit bigint;

COMMENT ON COLUMN chat_model_configs.effective_context_limit IS
	'Optional override of context_limit for combo/route-backed models whose effective capacity is larger than their first/smallest target -- sourced from the upstream provider (e.g. OmniRoute), never hardcoded. NULL falls back to context_limit.';
