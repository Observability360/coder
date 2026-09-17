package chatd

// sidequery.go implements "BTW" (Ask without interrupting): a read-only
// side-channel query about a chat's current activity, distinct from
// SendMessage in every way that matters for isolation:
//
//   - Never calls chatstate.Tx / machine.Update. Never writes to chats,
//     chat_messages, chat_queued_messages, generation_attempt, retry_state,
//     worker/runner ownership, or the parent/subagent lifecycle. Every DB
//     call in this file is a plain read.
//   - Never wakes a worker, never publishes to chat:ownership, never
//     touches heartbeats.
//   - The optional LLM call (only for questions the structured snapshot
//     can't answer) is a single, isolated one-shot completion over a
//     read-only snapshot, built with the SAME resolveModelCall/
//     generateQuickgenObject primitives quickgen.go already uses for
//     title generation -- not a new provider-calling mechanism, and not
//     routed through the main turn loop. A failure here (or the model
//     rejecting/erroring) degrades to the structured snapshot text
//     instead of failing the request or touching the main chat at all.
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/coder/coder/v2/coderd/database"
)

const (
	sideQueryMaxOutputTokens = int64(400)
	sideQueryRecentMessages  = int32(6)
)

// SideQuerySnapshot is the read-only view of a chat's current activity
// used both as the direct answer for simple status questions and as the
// grounding context for an interpretive LLM answer.
type SideQuerySnapshot struct {
	Status            database.ChatStatus
	RunningFor        time.Duration
	IsRunning         bool
	CurrentActivity   string
	SubagentsRunning  int
	SubagentsDone     int
	QueuedCount       int64
	GenerationAttempt int64
	IsRetrying        bool
}

// FormatText renders the snapshot the same way for both the no-LLM fast
// path and the LLM prompt's grounding context, so what a human sees for a
// simple "/btw status" and what the interpretive LLM path is told about
// the chat are never two different sources of truth.
func (s SideQuerySnapshot) FormatText() string {
	var b strings.Builder
	if s.IsRunning {
		fmt.Fprintf(&b, "Status: running for %s\n", formatDuration(s.RunningFor))
	} else {
		fmt.Fprintf(&b, "Status: %s\n", s.Status)
	}
	if s.CurrentActivity != "" {
		fmt.Fprintf(&b, "Now: %s\n", s.CurrentActivity)
	}
	fmt.Fprintf(&b, "Subagents: %d running, %d completed\n", s.SubagentsRunning, s.SubagentsDone)
	fmt.Fprintf(&b, "Queued messages: %d\n", s.QueuedCount)
	if s.IsRetrying {
		fmt.Fprintf(&b, "Retry: attempt %d\n", s.GenerationAttempt)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// BuildSideQuerySnapshot assembles the read-only snapshot for chatID from
// existing primitives only (GetChatByID, CountChatQueuedMessages,
// GetChildChatsByParentIDs, GetChatMessagesByChatIDDescPaginated) -- no new
// tracked state, no writes.
func (p *Server) BuildSideQuerySnapshot(ctx context.Context, chatID uuid.UUID) (SideQuerySnapshot, database.Chat, error) {
	chat, err := p.db.GetChatByID(ctx, chatID)
	if err != nil {
		return SideQuerySnapshot{}, database.Chat{}, fmt.Errorf("load chat: %w", err)
	}

	snapshot := SideQuerySnapshot{
		Status:            chat.Status,
		GenerationAttempt: chat.GenerationAttempt,
		IsRetrying:        chat.RetryState.Valid,
	}
	switch chat.Status {
	case database.ChatStatusRunning, database.ChatStatusInterrupting:
		snapshot.IsRunning = true
		if chat.StartedAt.Valid {
			snapshot.RunningFor = time.Since(chat.StartedAt.Time)
		}
	}

	queuedCount, err := p.db.CountChatQueuedMessages(ctx, chatID)
	if err != nil {
		return SideQuerySnapshot{}, database.Chat{}, fmt.Errorf("count queued messages: %w", err)
	}
	snapshot.QueuedCount = queuedCount

	children, err := p.db.GetChildChatsByParentIDs(ctx, database.GetChildChatsByParentIDsParams{
		ParentIds: []uuid.UUID{chatID},
	})
	if err != nil {
		return SideQuerySnapshot{}, database.Chat{}, fmt.Errorf("list subagents: %w", err)
	}
	for _, child := range children {
		switch child.Chat.Status {
		case database.ChatStatusRunning, database.ChatStatusInterrupting:
			snapshot.SubagentsRunning++
		default:
			snapshot.SubagentsDone++
		}
	}

	messages, err := p.db.GetChatMessagesByChatIDDescPaginated(ctx, database.GetChatMessagesByChatIDDescPaginatedParams{
		ChatID:   chatID,
		LimitVal: sideQueryRecentMessages,
	})
	if err != nil {
		return SideQuerySnapshot{}, database.Chat{}, fmt.Errorf("load recent messages: %w", err)
	}
	snapshot.CurrentActivity = currentActivityFromMessages(messages)

	return snapshot, chat, nil
}

// sideQueryContentPart is a minimal, LOCAL mirror of the fields of
// codersdk.ChatMessagePart this file actually reads (type, tool_name,
// parsed_commands). Defined here instead of importing codersdk's decoder
// because the full conversion lives in package coderd, which imports
// chatd -- importing it back would cycle. Field names match the wire
// format codersdk.ChatMessagePart already produces.
type sideQueryContentPart struct {
	Type           string     `json:"type"`
	ToolName       string     `json:"tool_name"`
	ParsedCommands [][]string `json:"parsed_commands"`
}

// parseSideQueryMessageContent decodes a chat_messages row's content
// column into the minimal shape above. content.Content may be NULL
// (system-only rows, deleted messages) or hold a JSON array of parts.
func parseSideQueryMessageContent(content database.ChatMessage) ([]sideQueryContentPart, error) {
	if !content.Content.Valid || len(content.Content.RawMessage) == 0 {
		return nil, fmt.Errorf("empty content")
	}
	var parts []sideQueryContentPart
	if err := json.Unmarshal(content.Content.RawMessage, &parts); err != nil {
		return nil, fmt.Errorf("unmarshal content parts: %w", err)
	}
	return parts, nil
}

// currentActivityFromMessages derives a short "what is it doing right now"
// hint from the most recent tool-call part it can find, preferring a
// parsed shell command (execute tool) since that is the most literal,
// least-speculative summary available. Falls back to the bare tool name,
// then to nothing (never a fabricated guess).
func currentActivityFromMessages(messages []database.ChatMessage) string {
	for _, m := range messages {
		parts, err := parseSideQueryMessageContent(m)
		if err != nil {
			continue
		}
		for i := len(parts) - 1; i >= 0; i-- {
			part := parts[i]
			if part.Type != "tool-call" && part.Type != "tool-result" {
				continue
			}
			if len(part.ParsedCommands) > 0 {
				cmd := part.ParsedCommands[len(part.ParsedCommands)-1]
				return "running " + strings.Join(cmd, " ")
			}
			if part.ToolName != "" {
				return "using " + part.ToolName
			}
		}
	}
	return ""
}

// isSimpleStatusQuestion reports whether question can be answered directly
// from the structured snapshot without an LLM call -- an empty question
// (plain "/btw" or the "Ask without interrupting" button) or a short,
// clearly status-shaped question in English or Portuguese. Deliberately
// conservative: anything not recognized falls through to the interpretive
// LLM path rather than risk answering a real question with a canned
// status blurb.
func isSimpleStatusQuestion(question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return true
	}
	statusPhrases := []string{
		"status", "como estamos", "o que esta fazendo", "o que está fazendo",
		"o que ta fazendo", "what's happening", "whats happening",
		"what is happening", "how's it going", "hows it going",
		"progress", "andamento",
	}
	for _, phrase := range statusPhrases {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
}

// SideQueryResult is the outcome of a BTW query: the answer text, whether
// it came from the structured snapshot or an LLM call, and the snapshot
// itself so the caller can render both.
type SideQueryResult struct {
	Answer   string
	Source   string // "snapshot" or "llm"
	Snapshot SideQuerySnapshot
}

// SideQuery answers a BTW question about chatID without ever touching the
// main agent's operational state (see the package-level doc comment
// above). Simple status questions are answered directly from the
// snapshot; anything else attempts one isolated, read-only LLM call and
// falls back to the plain snapshot text on ANY failure (resolve error,
// generation error, or the model declining) -- a side-channel failing
// must never surface as an error to a caller who was only asking a
// question, and must never affect the main chat.
func (p *Server) SideQuery(ctx context.Context, chatID uuid.UUID, question string) (SideQueryResult, error) {
	snapshot, chat, err := p.BuildSideQuerySnapshot(ctx, chatID)
	if err != nil {
		return SideQueryResult{}, err
	}

	if isSimpleStatusQuestion(question) {
		return SideQueryResult{Answer: snapshot.FormatText(), Source: "snapshot", Snapshot: snapshot}, nil
	}

	answer, ok := p.generateSideQueryAnswer(ctx, chat, snapshot, question)
	if !ok {
		return SideQueryResult{Answer: snapshot.FormatText(), Source: "snapshot", Snapshot: snapshot}, nil
	}
	return SideQueryResult{Answer: answer, Source: "llm", Snapshot: snapshot}, nil
}

const sideQuerySystemPrompt = "You are a read-only status assistant for a coding agent chat. " +
	"You can only observe the snapshot provided below -- you cannot take any action, " +
	"run any tool, send any message, or affect the agent's work in any way. " +
	"Answer the user's question about what the agent is currently doing or has done so far, " +
	"using only the snapshot. Do not speculate beyond it, and never claim to have taken " +
	"or to be about to take any action. If the snapshot does not contain enough information " +
	"to answer, say so plainly. Populate the answer field with your reply in 1-4 sentences."

type sideQueryAnswerObject struct {
	Answer string `json:"answer"`
}

// generateSideQueryAnswer attempts one isolated, read-only LLM completion
// for an interpretive BTW question. It reuses resolveModelCall (the same
// spec-to-client pipeline every other chatd call site uses) and
// generateQuickgenObject (the same retrying structured-generation helper
// quickgen.go uses for title generation) -- no new provider-calling
// mechanism. ok is false on ANY failure (model resolution, generation
// error, empty answer); callers must fall back to the structured snapshot
// rather than surface an error, per this file's isolation contract.
func (p *Server) generateSideQueryAnswer(
	ctx context.Context,
	chat database.Chat,
	snapshot SideQuerySnapshot,
	question string,
) (string, bool) {
	resolved, err := p.resolveModelCall(ctx, modelCallSpec{
		purpose: "btw-side-query",
		chat:    chat,
	})
	if err != nil {
		return "", false
	}

	call := resolved.newObjectCall(
		"btw_answer",
		"Answer the user's side question about the current agent activity, based only on the provided snapshot.",
		sideQueryMaxOutputTokens,
	)
	userInput := fmt.Sprintf(
		"Current chat activity snapshot:\n%s\n\nUser's question: %s",
		snapshot.FormatText(),
		question,
	)
	call.Prompt = quickgenPrompt(sideQuerySystemPrompt, userInput)

	result, err := generateQuickgenObject[sideQueryAnswerObject](ctx, resolved.model.LanguageModel(), call)
	if err != nil || result == nil {
		return "", false
	}
	answer := strings.TrimSpace(result.Object.Answer)
	if answer == "" {
		return "", false
	}
	return answer, true
}
