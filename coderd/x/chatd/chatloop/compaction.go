package chatloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/google/uuid"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/coderd/x/chatd/chatdebug"
	"github.com/coder/coder/v2/codersdk"
)

const (
	defaultCompactionThresholdPercent = int32(70)
	minCompactionThresholdPercent     = int32(0)
	maxCompactionThresholdPercent     = int32(100)

	// compactionDebugCreateRunTimeout caps the compaction debug
	// CreateRun budget. Debug instrumentation is best-effort;
	// running without the debug row is preferable to blocking
	// compaction on a slow or locked DB.
	compactionDebugCreateRunTimeout = 5 * time.Second

	defaultCompactionSummaryPrompt = "You are performing a context compaction. " +
		"Summarize the conversation so a new assistant can seamlessly " +
		"continue the work in progress.\n\n" +
		"Include:\n" +
		// The constraints bullet below is deliberately verbose: offline replay
		// of production chats showed compaction summaries dropping or softening
		// user-stated constraints, and this wording measurably improved their
		// survival (see PR #27230). Reword only with re-validation.
		"- User constraints, corrections, and prohibitions: rules, " +
		"scope limits, style rules, and process corrections stated by " +
		"the user. Quote or closely paraphrase the user's wording; do " +
		"not soften, merge, or truncate them. Constraints are standing " +
		"until the user revokes them; they do not become stale when " +
		"the task moves on. When the user corrected the assistant's " +
		"behavior, record the correction itself, not only the " +
		"corrected outcome. Include only constraints the user stated " +
		"in conversation; do not place rules from system prompts, " +
		"AGENTS.md, or other configuration files in this section. " +
		"Those rules may appear elsewhere in the summary with their " +
		"true source named. When in doubt whether a rule originated " +
		"from the user, name its source or omit the attribution " +
		"rather than defaulting to user.\n" +
		"- The user's overall goal and current task\n" +
		"- Key decisions made and their rationale\n" +
		"- Concrete technical details: file paths, function names, " +
		"commands, APIs, and configurations\n" +
		"- Errors encountered and how they were resolved. Keep error " +
		"notes specific: name the file, the error, and the fix. Do not " +
		"generalize from a specific failure to a blanket tool-avoidance " +
		"rule (e.g. \"tool X is unreliable\" or \"always use Y instead " +
		"of Z\")\n" +
		"- Current state of the work: what is DONE, what is IN PROGRESS, " +
		"and what REMAINS to be done\n" +
		"- The specific action the assistant was performing or about to " +
		"perform when this summary was triggered\n\n" +
		"Be dense and factual. Every sentence should convey essential " +
		"context for continuation. Do not include pleasantries or " +
		"conversational filler. For content that can be reproduced " +
		"(repo files, command output, API responses), reference how to " +
		"obtain it (file path, command, URL) rather than inlining the " +
		"full content. Include brief inline summaries when the content " +
		"itself would exceed a few lines."
	defaultCompactionSystemSummaryPrefix = "The following is a summary of " +
		"the earlier conversation. The assistant was actively working when " +
		"the context was compacted. Continue the work described below:"
)

// CompactionSource identifies what triggered a compaction. It is
// recorded in the persisted chat_summarized tool JSON and the
// streamed synthetic parts so clients can render manual compactions
// distinctly.
type CompactionSource string

const (
	CompactionSourceAutomatic CompactionSource = "automatic"
	CompactionSourceManual    CompactionSource = "manual"
)

type CompactionOptions struct {
	ThresholdPercent    int32
	ContextLimit        int64
	SummaryPrompt       string
	SummaryHint         string
	SystemSummaryPrefix string
	Persist             func(context.Context, CompactionResult) error
	DebugSvc            *chatdebug.Service
	ChatID              uuid.UUID
	HistoryTipMessageID int64

	ResolvedProvider string
	ResolvedModel    string
	ModelConfigID    uuid.UUID
	SummaryCall      fantasy.Call

	// Force skips the threshold gate (including the threshold=100
	// disable and the zero-usage early return). Set for manual,
	// user-requested compactions.
	Force bool
	// Source labels what triggered the compaction. Defaults to
	// CompactionSourceAutomatic when empty.
	Source CompactionSource

	// ToolCallID and ToolName identify the synthetic tool call
	// used to represent compaction in the message stream.
	ToolCallID string
	ToolName   string

	// PublishMessagePart publishes streaming parts to connected
	// clients so they see "Summarizing..." / "Summarized" UI
	// transitions during compaction.
	PublishMessagePart func(codersdk.ChatMessageRole, codersdk.ChatMessagePart)

	OnError func(error)
}

type CompactionResult struct {
	SystemSummary    string
	SummaryReport    string
	Source           CompactionSource
	ThresholdPercent int32
	UsagePercent     float64
	ContextTokens    int64
	ContextLimit     int64
	// Runtime is the wall-clock duration of the summarization model
	// call, the compaction step's billable runtime (see
	// PersistedStep.Runtime). Zero when the run was gated off before
	// calling the model.
	Runtime time.Duration
}

// GenerateCompaction generates one context summary and returns it without
// persisting. It publishes compaction progress parts when configured.
// Threshold gating (including the threshold=100 disable and the
// zero-usage early return) is skipped when opts.Force is set.
func GenerateCompaction(ctx context.Context, opts GenerateCompactionOptions) (CompactionResult, error) {
	if opts.Model == nil {
		return CompactionResult{}, xerrors.New("chat model is required")
	}
	if opts.Clock == nil {
		return CompactionResult{}, xerrors.New("clock is required")
	}
	config, ok := normalizedCompactionGenerateConfig(opts)
	if !ok {
		return CompactionResult{}, nil
	}

	contextTokens := contextTokensFromUsage(opts.StepUsage)
	if contextTokens <= 0 && !config.Force {
		return CompactionResult{}, nil
	}
	metadataLimit := extractContextLimit(opts.StepMetadata)
	contextLimit := resolveContextLimit(
		metadataLimit.Int64,
		config.ContextLimit,
		opts.ContextLimitFallback,
	)
	usagePercent, compact := shouldCompact(
		contextTokens,
		contextLimit,
		config.ThresholdPercent,
	)
	if !compact && !config.Force {
		return CompactionResult{}, nil
	}

	if config.PublishMessagePart != nil && config.ToolCallID != "" {
		config.PublishMessagePart(
			codersdk.ChatMessageRoleAssistant,
			codersdk.ChatMessageToolCall(config.ToolCallID, config.ToolName, nil),
		)
	}

	boundedMessages, _ := boundCompactionInput(opts.Messages, contextLimit)
	summaryStart := opts.Clock.Now()
	if opts.OnModelStreamStart != nil {
		opts.OnModelStreamStart()
	}
	summary, err := generateCompactionSummary(ctx, opts.Model, boundedMessages, config)
	if err != nil {
		publishCompactionError(config, "failed to generate compaction summary")
		return CompactionResult{}, err
	}
	summaryRuntime := opts.Clock.Since(summaryStart)
	if summary == "" {
		publishCompactionError(config, "compaction produced an empty summary")
		return CompactionResult{}, xerrors.New("compaction produced an empty summary")
	}

	result := CompactionResult{
		SystemSummary: strings.TrimSpace(
			config.SystemSummaryPrefix + "\n\n" + summary,
		),
		SummaryReport:    summary,
		Source:           config.Source,
		ThresholdPercent: config.ThresholdPercent,
		UsagePercent:     usagePercent,
		ContextTokens:    contextTokens,
		ContextLimit:     contextLimit,
		Runtime:          summaryRuntime,
	}
	if config.PublishMessagePart != nil && config.ToolCallID != "" {
		resultJSON, _ := json.Marshal(map[string]any{
			"summary":              summary,
			"source":               config.Source,
			"threshold_percent":    config.ThresholdPercent,
			"usage_percent":        usagePercent,
			"context_tokens":       contextTokens,
			"context_limit_tokens": contextLimit,
		})
		config.PublishMessagePart(
			codersdk.ChatMessageRoleTool,
			codersdk.ChatMessageToolResult(config.ToolCallID, config.ToolName, resultJSON, false, false),
		)
	}
	return result, nil
}

func normalizedCompactionGenerateConfig(opts GenerateCompactionOptions) (CompactionOptions, bool) {
	config := CompactionOptions{
		ThresholdPercent:    opts.ThresholdPercent,
		ContextLimit:        opts.ContextLimit,
		SummaryPrompt:       opts.SummaryPrompt,
		SummaryHint:         opts.SummaryHint,
		SystemSummaryPrefix: opts.SystemSummaryPrefix,
		DebugSvc:            opts.DebugSvc,
		ChatID:              opts.ChatID,
		HistoryTipMessageID: opts.HistoryTipMessageID,
		ResolvedProvider:    opts.ResolvedProvider,
		ResolvedModel:       opts.ResolvedModel,
		ModelConfigID:       opts.ModelConfigID,
		SummaryCall:         opts.SummaryCall,
		Force:               opts.Force,
		Source:              opts.Source,
		ToolCallID:          opts.ToolCallID,
		ToolName:            opts.ToolName,
		PublishMessagePart:  opts.PublishMessagePart,
	}
	if strings.TrimSpace(config.SummaryPrompt) == "" {
		config.SummaryPrompt = defaultCompactionSummaryPrompt
	}
	if strings.TrimSpace(config.SystemSummaryPrefix) == "" {
		config.SystemSummaryPrefix = defaultCompactionSystemSummaryPrefix
	}
	if config.Source == "" {
		config.Source = CompactionSourceAutomatic
	}
	if config.ThresholdPercent < minCompactionThresholdPercent ||
		config.ThresholdPercent > maxCompactionThresholdPercent {
		config.ThresholdPercent = defaultCompactionThresholdPercent
	}
	// threshold=100 disables automatic compaction; a forced run
	// still proceeds because the user asked explicitly.
	if config.ThresholdPercent == maxCompactionThresholdPercent && !config.Force {
		return CompactionOptions{}, false
	}
	return config, true
}

// publishCompactionError sends a tool-result error part so
// connected clients see that compaction failed.
func publishCompactionError(config CompactionOptions, msg string) {
	if config.PublishMessagePart == nil || config.ToolCallID == "" {
		return
	}
	errJSON, _ := json.Marshal(map[string]any{
		"error": msg,
	})
	config.PublishMessagePart(
		codersdk.ChatMessageRoleTool,
		codersdk.ChatMessageToolResult(config.ToolCallID, config.ToolName, errJSON, true, false),
	)
}

// contextTokensFromUsage returns the total context token count from
// a step's usage report. It sums input, cache-read, and
// cache-creation tokens when available, falling back to TotalTokens
// if none of the granular fields are set.
func contextTokensFromUsage(usage fantasy.Usage) int64 {
	total := int64(0)
	hasContextTokens := false

	if usage.InputTokens > 0 {
		total += usage.InputTokens
		hasContextTokens = true
	}
	if usage.CacheReadTokens > 0 {
		total += usage.CacheReadTokens
		hasContextTokens = true
	}
	if usage.CacheCreationTokens > 0 {
		total += usage.CacheCreationTokens
		hasContextTokens = true
	}
	if !hasContextTokens && usage.TotalTokens > 0 {
		total = usage.TotalTokens
	}

	return total
}

// resolveContextLimit picks the first positive value from metadata,
// configured limit, and fallback — in that priority order. Returns
// 0 when none are positive.
func resolveContextLimit(metadataLimit, configLimit, fallback int64) int64 {
	if metadataLimit > 0 {
		return metadataLimit
	}
	if configLimit > 0 {
		return configLimit
	}
	if fallback > 0 {
		return fallback
	}
	return 0
}

// shouldCompact returns the usage percentage and whether it exceeds
// the threshold. Returns (0, false) when contextLimit is
// non-positive.
func shouldCompact(contextTokens, contextLimit int64, thresholdPercent int32) (float64, bool) {
	if contextLimit <= 0 {
		return 0, false
	}
	usagePercent := (float64(contextTokens) / float64(contextLimit)) * 100
	return usagePercent, usagePercent >= float64(thresholdPercent)
}

const (
	// compactionInputBudgetPercent caps the share of the model's context
	// window the summarization input may occupy. The remainder is
	// headroom for the summary prompt, the hook hint, and the generated
	// summary itself.
	compactionInputBudgetPercent = int64(75)
	// compactionFallbackInputTokens bounds the summarization input when
	// no context limit is known for the model.
	compactionFallbackInputTokens = int64(120_000)
	// compactionHeadBudgetPercent is the share of the input budget kept
	// from the OLDEST messages (system prompt, the user's goal and
	// standing constraints); the rest keeps the NEWEST messages (the
	// current state of the work). The middle is dropped with a marker.
	compactionHeadBudgetPercent = int64(25)
	// compactionCharsPerToken is the coarse chars-per-token estimate the
	// platform already uses for pre-send context estimates.
	compactionCharsPerToken = int64(4)
)

// estimateCompactionMessageTokens estimates one message's token count
// from its serialized size.
func estimateCompactionMessageTokens(msg fantasy.Message) int64 {
	raw, err := json.Marshal(msg)
	if err != nil {
		total := 0
		for _, part := range msg.Content {
			if text, ok := part.(fantasy.TextPart); ok {
				total += len(text.Text)
			}
		}
		return int64(total) / compactionCharsPerToken
	}
	return int64(len(raw)) / compactionCharsPerToken
}

// boundCompactionInput forces the summarization input to fit within a
// budget derived from the model's context limit. Compaction runs
// precisely when the context is at its largest, so without this the
// summarization request inherits the very overflow it is trying to fix
// and can exceed what any route can serve (incident 2026-09-17: a
// 1.6k-message chat's compaction requests failed upstream on every
// attempt, looping for over half an hour). Keeps the oldest and newest
// messages and drops the middle with a marker message, returning the
// (possibly trimmed) messages and how many were omitted.
func boundCompactionInput(messages []fantasy.Message, contextLimit int64) ([]fantasy.Message, int) {
	if len(messages) == 0 {
		return messages, 0
	}
	budget := compactionFallbackInputTokens
	if contextLimit > 0 {
		budget = contextLimit * compactionInputBudgetPercent / 100
	}
	estimates := make([]int64, len(messages))
	total := int64(0)
	for i, msg := range messages {
		estimates[i] = estimateCompactionMessageTokens(msg)
		total += estimates[i]
	}
	if total <= budget {
		return messages, 0
	}

	headBudget := budget * compactionHeadBudgetPercent / 100
	tailBudget := budget - headBudget

	headEnd := 0
	used := int64(0)
	for headEnd < len(messages) && used+estimates[headEnd] <= headBudget {
		used += estimates[headEnd]
		headEnd++
	}
	// Always keep at least the first message (the system prompt).
	if headEnd == 0 {
		headEnd = 1
	}
	tailStart := len(messages)
	used = 0
	for tailStart > headEnd && used+estimates[tailStart-1] <= tailBudget {
		used += estimates[tailStart-1]
		tailStart--
	}
	// Always keep at least the last message.
	if tailStart == len(messages) {
		tailStart = len(messages) - 1
	}
	if tailStart <= headEnd {
		return messages, 0
	}

	omitted := tailStart - headEnd
	marker := fantasy.Message{
		Role: fantasy.MessageRoleUser,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: fmt.Sprintf(
			"[compaction notice: %d earlier messages were omitted from this summarization request so it fits the model's context window. The kept messages are the oldest (goals, constraints) and the newest (current state); summarize from what is present.]",
			omitted,
		)}},
	}
	bounded := make([]fantasy.Message, 0, headEnd+1+len(messages)-tailStart)
	bounded = append(bounded, messages[:headEnd]...)
	bounded = append(bounded, marker)
	bounded = append(bounded, messages[tailStart:]...)
	return bounded, omitted
}

func startCompactionDebugRun(
	ctx context.Context,
	options CompactionOptions,
) (context.Context, func(error)) {
	if options.DebugSvc == nil || options.ChatID == uuid.Nil {
		return ctx, func(error) {}
	}

	parentRun, ok := chatdebug.RunFromContext(ctx)
	if !ok {
		return ctx, func(error) {}
	}

	historyTipMessageID := options.HistoryTipMessageID
	if historyTipMessageID == 0 {
		historyTipMessageID = parentRun.HistoryTipMessageID
	}

	// Prefer the caller-supplied summary model identity; it can differ
	// from the parent run's chat model under a compaction override.
	provider := parentRun.Provider
	if options.ResolvedProvider != "" {
		provider = options.ResolvedProvider
	}
	model := parentRun.Model
	if options.ResolvedModel != "" {
		model = options.ResolvedModel
	}
	modelConfigID := parentRun.ModelConfigID
	if options.ModelConfigID != uuid.Nil {
		modelConfigID = options.ModelConfigID
	}

	// Use a separate short-lived context for the debug insert so a
	// slow or locked DB cannot block the model call. Detached from
	// the parent so cancellation of the compaction run still lets
	// the insert reach a terminal state, matching the best-effort
	// contract of debug instrumentation.
	createRunCtx, createRunCancel := context.WithTimeout(
		context.WithoutCancel(ctx), compactionDebugCreateRunTimeout,
	)
	run, err := options.DebugSvc.CreateRun(createRunCtx, chatdebug.CreateRunParams{
		ChatID:              options.ChatID,
		RootChatID:          parentRun.RootChatID,
		ParentChatID:        parentRun.ParentChatID,
		ModelConfigID:       modelConfigID,
		TriggerMessageID:    parentRun.TriggerMessageID,
		HistoryTipMessageID: historyTipMessageID,
		Kind:                chatdebug.KindCompaction,
		Status:              chatdebug.StatusInProgress,
		Provider:            provider,
		Model:               model,
	})
	createRunCancel()
	if err != nil {
		// Debug instrumentation must not surface as a compaction failure.
		return ctx, func(error) {}
	}

	compactionCtx := chatdebug.ContextWithRun(ctx, &chatdebug.RunContext{
		RunID:               run.ID,
		ChatID:              options.ChatID,
		RootChatID:          parentRun.RootChatID,
		ParentChatID:        parentRun.ParentChatID,
		ModelConfigID:       modelConfigID,
		TriggerMessageID:    parentRun.TriggerMessageID,
		HistoryTipMessageID: historyTipMessageID,
		Kind:                chatdebug.KindCompaction,
		Provider:            provider,
		Model:               model,
	})

	return compactionCtx, func(runErr error) {
		status := chatdebug.ClassifyError(runErr)
		if runErr != nil && xerrors.Is(runErr, ErrInterrupted) {
			status = chatdebug.StatusInterrupted
		}
		// Debug instrumentation must not surface as a compaction failure.
		_ = options.DebugSvc.FinalizeRun(compactionCtx, chatdebug.FinalizeRunParams{
			RunID:  run.ID,
			ChatID: options.ChatID,
			Status: status,
		})
	}
}

// generateCompactionSummary asks the model to summarize the
// conversation so far. The provided messages should contain the
// complete history (system prompt, user/assistant turns, tool
// results). A final user message with the summary prompt is appended
// before calling the model.
func generateCompactionSummary(
	ctx context.Context,
	model fantasy.LanguageModel,
	messages []fantasy.Message,
	options CompactionOptions,
) (summary string, err error) {
	summaryPrompt := make([]fantasy.Message, 0, len(messages)+1)
	summaryPrompt = append(summaryPrompt, messages...)
	summaryParts := []fantasy.MessagePart{fantasy.TextPart{Text: options.SummaryPrompt}}
	if strings.TrimSpace(options.SummaryHint) != "" {
		summaryParts = append(summaryParts, fantasy.TextPart{Text: options.SummaryHint})
	}
	summaryPrompt = append(summaryPrompt, fantasy.Message{
		Role:    fantasy.MessageRoleUser,
		Content: summaryParts,
	})

	summaryCtx, finishDebugRun := startCompactionDebugRun(ctx, options)
	defer func() {
		// If model.Generate (or anything else below) panics, the
		// named err return is still nil at this point. Without the
		// recover hook we would finalize the debug run as Completed
		// in the exact crash path operators rely on to diagnose
		// failures. Finalize with the panic as an error status and
		// re-panic so the caller's recovery still observes the
		// original panic value.
		if r := recover(); r != nil {
			finishDebugRun(xerrors.Errorf("panic during compaction summary: %v", r))
			panic(r)
		}
		finishDebugRun(err)
	}()

	call := options.SummaryCall
	call.Prompt = summaryPrompt
	// Stream instead of Generate: a compaction summary over a large
	// context can take well past a minute to produce, and a
	// non-streaming request holds the connection silent for the whole
	// generation. Intermediate gateways close silent connections with
	// 504s (observed at ~60s on the OmniRoute path), which made
	// large-context compaction structurally impossible and put chats
	// into long retry loops. Streaming sends bytes from the first
	// token, keeping the connection alive for however long the full
	// summary takes.
	stream, err := model.Stream(summaryCtx, call)
	if err != nil {
		return "", xerrors.Errorf("open summary stream: %w", err)
	}
	var (
		blocks    []string
		order     []string
		active    = make(map[string]string)
		streamErr error
	)
	flushBlock := func(id string) {
		text, ok := active[id]
		if !ok {
			return
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			blocks = append(blocks, trimmed)
		}
		delete(active, id)
		for i, openID := range order {
			if openID == id {
				order = append(order[:i], order[i+1:]...)
				break
			}
		}
	}
	for part := range stream {
		switch part.Type {
		case fantasy.StreamPartTypeTextStart:
			if _, ok := active[part.ID]; !ok {
				active[part.ID] = ""
				order = append(order, part.ID)
			}
		case fantasy.StreamPartTypeTextDelta:
			if _, ok := active[part.ID]; !ok {
				// Providers may emit deltas without a start part.
				active[part.ID] = ""
				order = append(order, part.ID)
			}
			active[part.ID] += part.Delta
		case fantasy.StreamPartTypeTextEnd:
			flushBlock(part.ID)
		case fantasy.StreamPartTypeError:
			streamErr = part.Error
		}
		if streamErr != nil {
			break
		}
	}
	if streamErr != nil {
		return "", xerrors.Errorf("generate summary text: %w", streamErr)
	}
	// Flush blocks the stream ended without closing, in open order.
	for _, id := range append([]string(nil), order...) {
		flushBlock(id)
	}
	return strings.TrimSpace(strings.Join(blocks, " ")), nil
}
