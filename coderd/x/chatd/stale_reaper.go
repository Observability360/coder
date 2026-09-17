package chatd

// stale_reaper.go wires the previously-dormant GetStaleChats query into
// an actual recovery loop. Chats can strand in states only their owning
// worker can exit (interrupting is the canonical case: a coderd restart
// mid-interrupt orphaned a production chat on 2026-09-17, and nothing
// ever picked it up because this query had no caller). Every stale row
// is handed to ReconcileInvalidStateChat, the same guarded state-machine
// transition the manual /reconcile-invalid endpoint uses: it re-checks
// state under the machine lock, so a chat that recovered on its own in
// the meantime is left untouched.

import (
	"context"
	"time"

	"cdr.dev/slog/v3"
)

const (
	staleChatReapInterval  = time.Minute
	staleChatReapThreshold = 2 * time.Minute
)

func (p *Server) runStaleChatReaper(ctx context.Context) {
	ticker := time.NewTicker(staleChatReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		p.reapStaleChats(ctx)
	}
}

func (p *Server) reapStaleChats(ctx context.Context) {
	threshold := time.Now().Add(-staleChatReapThreshold)
	stale, err := p.db.GetStaleChats(ctx, threshold)
	if err != nil {
		p.logger.Warn(ctx, "stale chat reaper: list failed", slog.Error(err))
		return
	}
	for _, chat := range stale {
		if ctx.Err() != nil {
			return
		}
		recovered, err := p.ReconcileInvalidStateChat(ctx, chat)
		if err != nil {
			p.logger.Warn(ctx, "stale chat reaper: reconcile failed",
				slog.F("chat_id", chat.ID),
				slog.F("status", chat.Status),
				slog.Error(err))
			continue
		}
		p.logger.Info(ctx, "stale chat reaper: recovered stuck chat",
			slog.F("chat_id", chat.ID),
			slog.F("was_status", chat.Status),
			slog.F("now_status", recovered.Status))
	}
}
