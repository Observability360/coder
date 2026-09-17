import type { AgentContextUsage } from "../components/AgentChatInput";

/**
 * Coder-side context-window estimator.
 *
 * `AgentContextUsage.usedTokens` (see chatHelpers.ts:extractContextUsageFromMessage)
 * is retrospective: it only exists once a turn has actually completed and the
 * upstream provider (via OmniRoute) reported real usage. Before that first
 * response -- or for anything typed/queued SINCE the last completed turn --
 * there is nothing real to show, so the indicator either says "unavailable"
 * or reports a number that is already stale by the time the user reads it
 * (the original UX complaint this fixes).
 *
 * This estimator reuses the two primitives already used server-side for the
 * exact same purpose (see OmniRoute's contextManager.ts estimateTokens: a
 * plain chars/4 heuristic) rather than inventing a new one: it takes the last
 * REAL usedTokens as a baseline when one exists, and adds a cheap chars/4
 * estimate for whatever hasn't been reflected in a real response yet (the
 * current draft + any queued messages). The result is real when nothing is
 * pending, and a clearly-labeled estimate otherwise -- never a fabricated
 * "usage" number presented as ground truth.
 */

/** Matches OmniRoute's own CHARS_PER_TOKEN heuristic (contextManager.ts) --
 *  reusing the same constant rather than picking a new one. */
const CHARS_PER_TOKEN = 4;

export type ContextWarningLevel =
	| "normal"
	| "approaching"
	| "warning"
	| "overflow";

export interface ContextEstimate {
	readonly usedTokens: number;
	readonly limitTokens: number;
	readonly remainingTokens: number;
	/** 0-100+, uncapped so overflow is visible as e.g. 112. */
	readonly percentUsed: number;
	/** False only when usedTokens is exactly the last real, completed-turn
	 *  usage with nothing pending since. */
	readonly isEstimated: boolean;
	readonly warningLevel: ContextWarningLevel;
	/** Percent (0-100) at which compaction triggers, when known -- carried
	 *  through so callers can render it without re-deriving. */
	readonly compressionThresholdPercent: number | undefined;
}

function charsToTokens(chars: number): number {
	return Math.ceil(Math.max(0, chars) / CHARS_PER_TOKEN);
}

/** Total character count across a queued message's text parts, mirroring
 *  the same shape used by getQueuedMessageInfo (text + text-bearing parts
 *  only -- files/attachments are not char-estimable here). */
function queuedMessageCharCount(content: readonly { type: string; text?: string }[]): number {
	let total = 0;
	for (const part of content) {
		if ((part.type === "text" || part.type === "hook-notice") && part.text) {
			total += part.text.length;
		}
	}
	return total;
}

export interface EstimateContextUsageInput {
	/** Retrospective usage from the last completed turn, if any. */
	readonly realUsage: Pick<AgentContextUsage, "usedTokens"> | null | undefined;
	/** The currently selected model's context window, from
	 *  chat_model_configs.context_limit -- always known once a model is
	 *  selected, even before the first message. */
	readonly limitTokens: number | undefined;
	/** chat_model_configs.compression_threshold (or the user's override),
	 *  as a percent (0-100) of limitTokens. */
	readonly compressionThresholdPercent: number | undefined;
	/** Characters in the composer draft not yet sent. */
	readonly draftCharCount: number;
	/** Messages already queued (persisted backend state) but not yet part
	 *  of any completed turn's reported usage. */
	readonly queuedMessages: readonly {
		content: readonly { type: string; text?: string }[];
	}[];
}

/**
 * Estimates current context usage against the selected model's window.
 * Returns undefined only when there is no limit to compare against at all
 * (no model selected yet) -- callers should render "unavailable" ONLY in
 * that case, never merely because a real usage number hasn't arrived yet
 * (that case is handled here as an estimate instead).
 */
export function estimateContextUsage(
	input: EstimateContextUsageInput,
): ContextEstimate | undefined {
	const { realUsage, limitTokens, compressionThresholdPercent, draftCharCount, queuedMessages } =
		input;
	if (!limitTokens || limitTokens <= 0) return undefined;
	// No real usage and nothing typed or queued means there is no
	// signal at all: report nothing rather than a fabricated
	// "0% of the limit", which users read as a broken indicator.
	const hasRealTokens =
		typeof realUsage?.usedTokens === "number" && realUsage.usedTokens > 0;
	if (
		!hasRealTokens &&
		draftCharCount === 0 &&
		queuedMessages.every((m) => queuedMessageCharCount(m.content) === 0)
	) {
		return undefined;
	}

	const baselineTokens =
		realUsage && typeof realUsage.usedTokens === "number" ? realUsage.usedTokens : 0;

	const queuedCharCount = queuedMessages.reduce(
		(sum, m) => sum + queuedMessageCharCount(m.content),
		0,
	);
	const pendingCharCount = Math.max(0, draftCharCount) + queuedCharCount;
	const pendingTokens = charsToTokens(pendingCharCount);

	const isEstimated = pendingTokens > 0 || !realUsage;
	const usedTokens = baselineTokens + pendingTokens;
	const remainingTokens = limitTokens - usedTokens;
	const percentUsed = Math.round((usedTokens / limitTokens) * 100);

	const warningLevel = resolveWarningLevel(percentUsed, compressionThresholdPercent);

	return {
		usedTokens,
		limitTokens,
		remainingTokens,
		percentUsed,
		isEstimated,
		warningLevel,
		compressionThresholdPercent,
	};
}

/**
 * Bands derived from the configured compression threshold rather than a new
 * hardcoded number: "approaching" starts at 90% of the threshold itself
 * (e.g. threshold=70% of window -> approaching band starts at 63%), so a
 * combo with a conservative threshold warns earlier and one with a generous
 * threshold warns later, matching that combo's own configuration instead of
 * a one-size-fits-all constant. When no threshold is configured at all, falls
 * back to the model's hard context_limit (100%) as the only real boundary --
 * still zero new hardcoded percentages, just a narrower set of bands.
 */
export function resolveWarningLevel(
	percentUsed: number,
	compressionThresholdPercent: number | undefined,
): ContextWarningLevel {
	if (percentUsed >= 100) return "overflow";
	const threshold =
		typeof compressionThresholdPercent === "number" && compressionThresholdPercent > 0
			? compressionThresholdPercent
			: 100;
	if (percentUsed >= threshold) return "warning";
	if (percentUsed >= threshold * 0.9) return "approaching";
	return "normal";
}
