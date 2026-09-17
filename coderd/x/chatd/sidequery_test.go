package chatd //nolint:testpackage // Shares workerTestFixture and friends from helpers_test.go.

// sidequery_test.go proves the isolation contract documented at the top of
// sidequery.go: a BTW SideQuery call, run against a chat that is actively
// being worked on, never mutates any operational state -- not the main
// chat's own row, not its queued messages, not a subagent's row -- and
// never lets a failure in its own (optional, best-effort) LLM call
// propagate as an error.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/x/chatd/chatstate"
	"github.com/coder/coder/v2/testutil"
)

// T1: SideQuery never bumps generation_attempt or reassigns worker
// ownership on a chat that is actively running under a worker.
func TestSideQuery_LeavesGenerationAttemptAndWorkerUnchanged(t *testing.T) {
	t.Parallel()
	f := newWorkerTestFixture(t)
	server := newUnstartedServer(t, f.pubsub, f.db)
	chat := f.createRunningChat(t)

	workerID := uuid.New()
	runnerID := uuid.New()
	acquireChat(t, f, chat.ID, workerID, runnerID)

	ctx := testutil.Context(t, testutil.WaitShort)
	machine := chatstate.NewChatMachine(f.db, f.pubsub, chat.ID)
	require.NoError(t, machine.Update(ctx, func(tx *chatstate.Tx, _ database.Store) error {
		_, err := tx.RecordGenerationAttempt(chatstate.RecordGenerationAttemptInput{})
		return err
	}))

	before, err := f.db.GetChatByID(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), before.GenerationAttempt)
	require.True(t, before.WorkerID.Valid)

	_, err = server.SideQuery(ctx, chat.ID, "", "")
	require.NoError(t, err)

	after, err := f.db.GetChatByID(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, before.GenerationAttempt, after.GenerationAttempt, "SideQuery must never bump generation_attempt")
	require.Equal(t, before.WorkerID, after.WorkerID, "SideQuery must never reassign worker ownership")
	require.Equal(t, before.RunnerID, after.RunnerID, "SideQuery must never reassign runner ownership")
}

// T2: SideQuery never changes a running chat's status -- not to
// interrupting, not to anything else -- regardless of whether the
// question is simple or triggers the (unreachable, in this fixture)
// interpretive LLM path.
func TestSideQuery_LeavesChatStatusUnchanged(t *testing.T) {
	t.Parallel()
	f := newWorkerTestFixture(t)
	server := newUnstartedServer(t, f.pubsub, f.db)
	chat := f.createRunningChat(t)
	require.Equal(t, database.ChatStatusRunning, chat.Status)

	ctx := testutil.Context(t, testutil.WaitShort)
	_, err := server.SideQuery(ctx, chat.ID, "what are you doing right now and why?", "")
	require.NoError(t, err)

	after, err := f.db.GetChatByID(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, database.ChatStatusRunning, after.Status, "SideQuery must never transition chat status")
}

// T3: SideQuery never touches the queued-message list -- neither its
// count nor the content of any entry -- even though it reads
// CountChatQueuedMessages as part of building its snapshot.
func TestSideQuery_LeavesQueuedMessagesUnchanged(t *testing.T) {
	t.Parallel()
	f := newWorkerTestFixture(t)
	server := newUnstartedServer(t, f.pubsub, f.db)
	chat := f.createRunningChat(t)

	workerID := uuid.New()
	runnerID := uuid.New()
	acquireChat(t, f, chat.ID, workerID, runnerID)

	ctx := testutil.Context(t, testutil.WaitShort)
	machine := chatstate.NewChatMachine(f.db, f.pubsub, chat.ID)
	require.NoError(t, machine.Update(ctx, func(tx *chatstate.Tx, _ database.Store) error {
		_, err := tx.SendMessage(chatstate.SendMessageInput{
			Message:      userTextMessage(t, "queued while busy", f.user.ID, f.model.ID, f.apiKey.ID),
			BusyBehavior: chatstate.BusyBehaviorQueue,
		})
		return err
	}))

	beforeCount, err := f.db.CountChatQueuedMessages(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), beforeCount)
	beforeMessages, err := f.db.GetChatQueuedMessages(ctx, chat.ID)
	require.NoError(t, err)
	require.Len(t, beforeMessages, 1)

	result, err := server.SideQuery(ctx, chat.ID, "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Snapshot.QueuedCount, "snapshot should reflect the queued message it read")

	afterCount, err := f.db.CountChatQueuedMessages(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "SideQuery must never change the queued message count")
	afterMessages, err := f.db.GetChatQueuedMessages(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, beforeMessages, afterMessages, "SideQuery must never alter queued message content or order")
}

// T4: SideQuery correctly reflects running vs completed subagents in its
// snapshot, and never touches any subagent chat's own row while doing so.
func TestSideQuery_ReflectsSubagentsWithoutMutatingThem(t *testing.T) {
	t.Parallel()
	f := newWorkerTestFixture(t)
	server := newUnstartedServer(t, f.pubsub, f.db)
	parent := f.createRunningChat(t)
	runningChild := f.createRunningSubagentChat(t, parent.ID)
	doneChild := f.createRunningSubagentChat(t, parent.ID)

	ctx := testutil.Context(t, testutil.WaitShort)
	doneChild = forceExecutionState(t, f, doneChild.ID, database.ChatStatusWaiting, false)
	require.Equal(t, database.ChatStatusWaiting, doneChild.Status)

	beforeRunning, err := f.db.GetChatByID(ctx, runningChild.ID)
	require.NoError(t, err)
	beforeDone, err := f.db.GetChatByID(ctx, doneChild.ID)
	require.NoError(t, err)

	result, err := server.SideQuery(ctx, parent.ID, "", "")
	require.NoError(t, err)
	require.Equal(t, 1, result.Snapshot.SubagentsRunning)
	require.Equal(t, 1, result.Snapshot.SubagentsDone)

	afterRunning, err := f.db.GetChatByID(ctx, runningChild.ID)
	require.NoError(t, err)
	afterDone, err := f.db.GetChatByID(ctx, doneChild.ID)
	require.NoError(t, err)
	require.Equal(t, beforeRunning, afterRunning, "SideQuery must never mutate a running subagent's own row")
	require.Equal(t, beforeDone, afterDone, "SideQuery must never mutate a completed subagent's own row")
}

// T5: when the interpretive LLM call fails (as it always will in this
// fixture, whose model config points at an unreachable base URL), SideQuery
// fails open to the structured snapshot text instead of propagating an
// error to the caller.
func TestSideQuery_FailsOpenToSnapshotOnLLMError(t *testing.T) {
	t.Parallel()
	f := newWorkerTestFixture(t)
	server := newUnstartedServer(t, f.pubsub, f.db)
	chat := f.createRunningChat(t)

	ctx := testutil.Context(t, testutil.WaitShort)
	result, err := server.SideQuery(ctx, chat.ID, "please explain in detail what you have accomplished so far", "")
	require.NoError(t, err, "a side-channel LLM failure must never surface as an error")
	require.Equal(t, "snapshot", result.Source, "must fall back to the structured snapshot when the LLM call fails")
	require.Equal(t, result.Snapshot.FormatText(), result.Answer)
	require.NotEmpty(t, result.Answer)
}

// T6: SideQuery performs zero writes to the chat row -- UpdatedAt,
// RetryState, RetryStateVersion, and SnapshotVersion are the four fields
// any write to a chat's operational state would be expected to touch, and
// all four must be bit-for-bit identical before and after.
func TestSideQuery_PerformsNoWritesToChatRow(t *testing.T) {
	t.Parallel()
	f := newWorkerTestFixture(t)
	server := newUnstartedServer(t, f.pubsub, f.db)
	chat := f.createRunningChat(t)

	ctx := testutil.Context(t, testutil.WaitShort)
	wantRetryState := []byte(`{"kind":"timeout","status_code":524,"retryable":true,"message":"gateway timeout"}`)
	machine := chatstate.NewChatMachine(f.db, f.pubsub, chat.ID)
	require.NoError(t, machine.Update(ctx, func(tx *chatstate.Tx, _ database.Store) error {
		if _, err := tx.RecordGenerationAttempt(chatstate.RecordGenerationAttemptInput{}); err != nil {
			return err
		}
		_, err := tx.RecordRetryState(chatstate.RecordRetryStateInput{
			RetryState: pqtype.NullRawMessage{RawMessage: wantRetryState, Valid: true},
		})
		return err
	}))

	before, err := f.db.GetChatByID(ctx, chat.ID)
	require.NoError(t, err)
	require.True(t, before.RetryState.Valid)

	_, err = server.SideQuery(ctx, chat.ID, "what's the status?", "")
	require.NoError(t, err)

	after, err := f.db.GetChatByID(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, before.UpdatedAt, after.UpdatedAt, "SideQuery must never touch updated_at")
	require.Equal(t, before.RetryState, after.RetryState, "SideQuery must never touch retry_state")
	require.Equal(t, before.RetryStateVersion, after.RetryStateVersion, "SideQuery must never bump retry_state_version")
	require.Equal(t, before.SnapshotVersion, after.SnapshotVersion, "SideQuery must never bump snapshot_version")
	require.Equal(t, before, after, "SideQuery must leave the entire chat row byte-for-byte unchanged")
}
