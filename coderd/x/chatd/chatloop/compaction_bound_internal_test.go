package chatloop

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/coderd/x/chatd/chattest"
	"github.com/coder/quartz"
)

func repeatTextMessage(role fantasy.MessageRole, text string) fantasy.Message {
	return fantasy.Message{
		Role:    role,
		Content: []fantasy.MessagePart{fantasy.TextPart{Text: text}},
	}
}

func containsCompactionMarker(messages []fantasy.Message) bool {
	for _, msg := range messages {
		for _, part := range msg.Content {
			if text, ok := part.(fantasy.TextPart); ok &&
				strings.Contains(text.Text, "[compaction notice:") {
				return true
			}
		}
	}
	return false
}

func TestBoundCompactionInput(t *testing.T) {
	t.Parallel()

	t.Run("UnderBudgetUntouched", func(t *testing.T) {
		t.Parallel()
		messages := []fantasy.Message{
			repeatTextMessage(fantasy.MessageRoleSystem, "system prompt"),
			repeatTextMessage(fantasy.MessageRoleUser, "hello"),
			repeatTextMessage(fantasy.MessageRoleAssistant, "hi"),
		}
		bounded, omitted := boundCompactionInput(messages, 100_000)
		require.Zero(t, omitted)
		require.Equal(t, messages, bounded)
	})

	t.Run("OverBudgetKeepsHeadAndTail", func(t *testing.T) {
		t.Parallel()
		// 50 messages of ~1000 estimated tokens each (~50k total)
		// against a 20k context limit (15k input budget).
		big := strings.Repeat("x", 4000)
		messages := make([]fantasy.Message, 0, 50)
		messages = append(messages, repeatTextMessage(fantasy.MessageRoleSystem, "system prompt "+big))
		for i := 1; i < 50; i++ {
			messages = append(messages, repeatTextMessage(fantasy.MessageRoleUser, big))
		}
		bounded, omitted := boundCompactionInput(messages, 20_000)
		require.Positive(t, omitted)
		require.Less(t, len(bounded), len(messages))
		// The oldest message (system prompt) survives.
		first, ok := bounded[0].Content[0].(fantasy.TextPart)
		require.True(t, ok)
		require.Contains(t, first.Text, "system prompt")
		// The newest message survives.
		require.Equal(t, messages[len(messages)-1], bounded[len(bounded)-1])
		// The omission is marked so the summarizer knows content is missing.
		require.True(t, containsCompactionMarker(bounded))
		// Structure: kept-head + marker + kept-tail accounts for every
		// original message exactly once.
		require.Equal(t, len(messages)-omitted+1, len(bounded))
	})

	t.Run("UnknownLimitUsesFallbackBudget", func(t *testing.T) {
		t.Parallel()
		big := strings.Repeat("x", 4*4000)
		messages := make([]fantasy.Message, 0, 150)
		for i := 0; i < 150; i++ {
			messages = append(messages, repeatTextMessage(fantasy.MessageRoleUser, big))
		}
		// ~600k estimated tokens with no known limit: the fallback
		// budget must still bound the input.
		bounded, omitted := boundCompactionInput(messages, 0)
		require.Positive(t, omitted)
		require.Less(t, len(bounded), len(messages))
	})
}

func TestGenerateCompactionSummary_StreamError(t *testing.T) {
	t.Parallel()

	model := &chattest.FakeModel{
		ProviderName: "fake",
		StreamFn: func(_ context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
			return func(yield func(fantasy.StreamPart) bool) {
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t1"}) {
					return
				}
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: "partial"}) {
					return
				}
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: xerrors.New("upstream boom")})
			}, nil
		},
	}

	_, err := generateCompactionSummary(context.Background(), model,
		[]fantasy.Message{textMessage(fantasy.MessageRoleUser, "hello")},
		CompactionOptions{SummaryPrompt: "summarize"},
	)
	require.ErrorContains(t, err, "upstream boom")
}

func TestGenerateCompaction_BoundsOversizedInput(t *testing.T) {
	t.Parallel()

	var promptLen int
	var sawMarker bool
	model := &chattest.FakeModel{
		ProviderName: "fake",
		ModelName:    "fake-model",
		StreamFn: func(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
			promptLen = len(call.Prompt)
			for _, msg := range call.Prompt {
				for _, part := range msg.Content {
					if text, ok := part.(fantasy.TextPart); ok &&
						strings.Contains(text.Text, "[compaction notice:") {
						sawMarker = true
					}
				}
			}
			return func(yield func(fantasy.StreamPart) bool) {
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t1"}) {
					return
				}
				if !yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: "bounded summary"}) {
					return
				}
				yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "t1"})
			}, nil
		},
	}

	big := strings.Repeat("x", 4000)
	messages := make([]fantasy.Message, 0, 50)
	for i := 0; i < 50; i++ {
		messages = append(messages, repeatTextMessage(fantasy.MessageRoleUser, big))
	}

	result, err := GenerateCompaction(context.Background(), GenerateCompactionOptions{
		Model:            model,
		Messages:         messages,
		ThresholdPercent: 70,
		ContextLimit:     20_000,
		StepUsage:        fantasy.Usage{InputTokens: 19_000},
		Clock:            quartz.NewMock(t),
	})
	require.NoError(t, err)
	require.Equal(t, "bounded summary", result.SummaryReport)
	// The model saw a bounded prompt: fewer messages than the raw
	// history (plus the summary-request message) and the omission marker.
	require.Less(t, promptLen, len(messages)+1)
	require.True(t, sawMarker, "bounded prompt must carry the omission marker")
}
