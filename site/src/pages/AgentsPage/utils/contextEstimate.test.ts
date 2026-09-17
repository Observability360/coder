import { describe, expect, it } from "vitest";
import { estimateContextUsage } from "./contextEstimate";

// Mirrors AgentChatInput.tsx's selection: prefer effectiveContextLimit
// (the largest window any currently eligible target in a combo can
// serve) over the raw, per-primary-target contextLimit.
function resolveSelectedLimit(option: {
	readonly contextLimit?: number;
	readonly effectiveContextLimit?: number;
}): number | undefined {
	return option.effectiveContextLimit ?? option.contextLimit;
}

describe("estimateContextUsage", () => {
	it("returns undefined when no model/limit is selected yet (the only real 'unavailable' case)", () => {
		expect(
			estimateContextUsage({
				realUsage: null,
				limitTokens: undefined,
				compressionThresholdPercent: 70,
				draftCharCount: 0,
				queuedMessages: [],
			}),
		).toBeUndefined();
	});

	it("before the first response: estimates purely from the draft, marked as estimated", () => {
		const result = estimateContextUsage({
			realUsage: null,
			limitTokens: 200_000,
			compressionThresholdPercent: 70,
			draftCharCount: 400, // 100 tokens at 4 chars/token
			queuedMessages: [],
		});
		expect(result).toEqual({
			usedTokens: 100,
			limitTokens: 200_000,
			remainingTokens: 199_900,
			percentUsed: 0,
			isEstimated: true,
			warningLevel: "normal",
			compressionThresholdPercent: 70,
		});
	});

	it("small chat, real usage only, nothing pending: not estimated", () => {
		const result = estimateContextUsage({
			realUsage: { usedTokens: 5_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.isEstimated).toBe(false);
		expect(result?.usedTokens).toBe(5_000);
		expect(result?.percentUsed).toBe(3);
		expect(result?.warningLevel).toBe("normal");
	});

	it("large chat approaching the compression threshold warns before overflow, not after", () => {
		// threshold=70% of 200k=140k; approaching band starts at 90% of that (126k)
		const result = estimateContextUsage({
			realUsage: { usedTokens: 130_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.warningLevel).toBe("approaching");
	});

	it("past the compression threshold: warning level", () => {
		const result = estimateContextUsage({
			realUsage: { usedTokens: 145_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.warningLevel).toBe("warning");
	});

	it("known overflow (usage already at/over the hard limit): overflow level regardless of threshold", () => {
		const result = estimateContextUsage({
			realUsage: { usedTokens: 235_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.warningLevel).toBe("overflow");
		expect(result?.remainingTokens).toBeLessThan(0);
	});

	it("queued messages (after tool calls / a completed turn) add to the real baseline as an estimate", () => {
		const result = estimateContextUsage({
			realUsage: { usedTokens: 10_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [
				{ content: [{ type: "text", text: "a".repeat(4000) }] }, // 1000 tokens
				{
					content: [
						{ type: "text", text: "b".repeat(400) }, // 100 tokens
						{ type: "hook-notice", text: "c".repeat(400) }, // 100 tokens
						{ type: "file" }, // no text, not char-estimable, ignored
					],
				},
			],
		});
		expect(result?.usedTokens).toBe(10_000 + 1000 + 100 + 100);
		expect(result?.isEstimated).toBe(true);
	});

	it("switching model config changes the limit and re-derives the threshold band", () => {
		const withSmallModel = estimateContextUsage({
			realUsage: { usedTokens: 50_000 },
			limitTokens: 64_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		const withLargeModel = estimateContextUsage({
			realUsage: { usedTokens: 50_000 },
			limitTokens: 1_000_000,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(withSmallModel?.warningLevel).toBe("warning"); // 78% > 70% threshold
		expect(withLargeModel?.warningLevel).toBe("normal"); // 5%, nowhere close
	});

	it("no compression threshold configured: falls back to the hard limit as the only boundary, never a new hardcoded number", () => {
		const belowLimit = estimateContextUsage({
			realUsage: { usedTokens: 195_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: undefined,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(belowLimit?.warningLevel).toBe("approaching"); // 97.5% >= 90% of 100, < 100
		const atLimit = estimateContextUsage({
			realUsage: { usedTokens: 200_000 },
			limitTokens: 200_000,
			compressionThresholdPercent: undefined,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(atLimit?.warningLevel).toBe("overflow");
	});
});


// Coder-UX-package context-capacity fix: o360-coding is an OmniRoute combo
// whose real targets have different windows (cursor/auto-balance=200k
// smallest, cc/claude-sonnet-5 and others much larger). contextLimit alone
// is the smallest target's window; effectiveContextLimit (when the backend
// populates it) is the largest window any currently eligible target can
// serve. These scenarios cover the combo-aware selection end to end.
describe("combo effective context limit (o360-coding-style combos)", () => {
	it("T1: small context, primary target (Cursor 200k) still covers it -- eligible, correct capacity", () => {
		const option = { contextLimit: 200_000, effectiveContextLimit: 200_000 };
		const limitTokens = resolveSelectedLimit(option);
		const result = estimateContextUsage({
			realUsage: { usedTokens: 100_000 },
			limitTokens,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.limitTokens).toBe(200_000);
		expect(result?.warningLevel).toBe("normal");
	});

	it("T2: context exceeds Cursor's 200k but a 1M target covers it -- NOT overflow, no premature warning", () => {
		const option = { contextLimit: 200_000, effectiveContextLimit: 1_000_000 };
		const limitTokens = resolveSelectedLimit(option);
		const result = estimateContextUsage({
			realUsage: { usedTokens: 235_000 },
			limitTokens,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		// Using the stale contextLimit (200k) this would already show
		// percentUsed=117 and warningLevel="overflow" -- the exact bug.
		expect(limitTokens).toBe(1_000_000);
		expect(result?.percentUsed).toBe(24);
		expect(result?.warningLevel).toBe("normal");
	});

	it("T3: context exceeds all but one large target -- effective capacity tracks that model exactly", () => {
		const option = { contextLimit: 200_000, effectiveContextLimit: 500_000 };
		const limitTokens = resolveSelectedLimit(option);
		const result = estimateContextUsage({
			realUsage: { usedTokens: 360_000 },
			limitTokens,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.limitTokens).toBe(500_000);
		expect(result?.percentUsed).toBe(72);
		expect(result?.warningLevel).toBe("warning");
	});

	it("T4: the large target becomes unavailable -- effective capacity drops to the next-largest", () => {
		const beforeOption = { contextLimit: 200_000, effectiveContextLimit: 1_000_000 };
		const afterOption = { contextLimit: 200_000, effectiveContextLimit: 400_000 };
		const usage = {
			realUsage: { usedTokens: 300_000 },
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		};
		const before = estimateContextUsage({
			...usage,
			limitTokens: resolveSelectedLimit(beforeOption),
		});
		const after = estimateContextUsage({
			...usage,
			limitTokens: resolveSelectedLimit(afterOption),
		});
		expect(before?.limitTokens).toBe(1_000_000);
		expect(before?.warningLevel).toBe("normal");
		expect(after?.limitTokens).toBe(400_000);
		expect(after?.warningLevel).toBe("warning");
	});

	it("T5: context exceeds ALL eligible targets -- this is the case that should warn/overflow/compact", () => {
		const option = { contextLimit: 200_000, effectiveContextLimit: 1_000_000 };
		const limitTokens = resolveSelectedLimit(option);
		const result = estimateContextUsage({
			realUsage: { usedTokens: 1_200_000 },
			limitTokens,
			compressionThresholdPercent: 70,
			draftCharCount: 0,
			queuedMessages: [],
		});
		expect(result?.limitTokens).toBe(1_000_000);
		expect(result?.percentUsed).toBeGreaterThanOrEqual(100);
		expect(result?.warningLevel).toBe("overflow");
	});

	it("T6: switching model/route recomputes the effective limit immediately, not the stale one", () => {
		const comboOption = { contextLimit: 200_000, effectiveContextLimit: 1_000_000 };
		const plainOption = { contextLimit: 128_000 }; // non-combo model: no effectiveContextLimit at all
		expect(resolveSelectedLimit(comboOption)).toBe(1_000_000);
		// Switching to a non-combo model falls back to its own contextLimit --
		// zero behavior change for models that never had this field.
		expect(resolveSelectedLimit(plainOption)).toBe(128_000);
	});
});
