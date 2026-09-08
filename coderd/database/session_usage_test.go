package database_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/database/dbgen"
	"github.com/coder/coder/v2/coderd/database/dbtestutil"
	"github.com/coder/coder/v2/coderd/database/dbtime"
	"github.com/coder/coder/v2/codersdk"
)

func TestSessionUsageMapNull(t *testing.T) {
	t.Parallel()
	m := database.StringMapOfInt{"cursor": 1}
	require.NoError(t, m.Scan(nil))
	require.Nil(t, m)
	require.NoError(t, m.Scan([]byte(`{}`)))
	require.NotNil(t, m)
	require.Empty(t, m)
}

func TestSessionUsageRollupOverlapAndRegistry(t *testing.T) {
	t.Parallel()
	db, _ := dbtestutil.NewDB(t)
	ctx := context.Background()
	start := dbtime.Now().Add(-3 * time.Hour).Truncate(30 * time.Minute)
	user, template := uuid.New(), uuid.New()
	insert := func(offset time.Duration, userID, templateID uuid.UUID, connected int64, counts map[string]int64) {
		dbgen.WorkspaceAgentStat(t, db, database.WorkspaceAgentStat{
			CreatedAt: start.Add(offset), UserID: userID, TemplateID: templateID, AgentID: uuid.New(),
			ConnectionCount: connected, SessionCounts: dbgen.SessionCounts(t, counts),
		})
	}
	insert(0, user, template, 1, map[string]int64{"cursor": 2, "vscode": 1, "new_app": 1})
	insert(30*time.Second, user, template, 0, map[string]int64{"cursor": 1, "new_app": 1})
	insert(time.Minute, user, template, 0, map[string]int64{"cursor": 1})
	insert(29*time.Minute, user, template, 0, map[string]int64{"vscode": 1})
	insert(30*time.Minute, user, template, 1, map[string]int64{"cursor": 1})
	insert(0, uuid.New(), template, 1, map[string]int64{"vscode": 1})
	insert(0, user, uuid.New(), 1, map[string]int64{"cursor": 1})
	disconnected := uuid.New()
	insert(0, user, disconnected, 0, map[string]int64{"cursor": 1})
	empty := uuid.New()
	insert(0, user, empty, 1, map[string]int64{})

	var registry map[string]string
	require.NoError(t, json.Unmarshal(codersdk.SessionCountAppFamiliesJSON(), &registry))
	registry["new_app"] = "new_family"
	mapping, err := json.Marshal(registry)
	require.NoError(t, err)
	require.NoError(t, db.UpsertTemplateUsageStats(ctx, mapping))
	params := database.GetTemplateUsageStatsParams{StartTime: start, EndTime: start.Add(time.Hour)}
	rows, err := db.GetTemplateUsageStats(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	found := false
	for _, row := range rows {
		require.NotEqual(t, disconnected, row.TemplateID)
		require.NotEqual(t, empty, row.TemplateID)
		if row.UserID == user && row.TemplateID == template && row.StartTime.Equal(start) {
			found = true
			require.EqualValues(t, 3, row.UsageMins)
			require.Equal(t, database.StringMapOfInt{"cursor": 2, "vscode": 2, "new_app": 1}, row.SessionAppUsageMins)
			require.Equal(t, database.StringMapOfInt{"vscode": 3, "new_family": 1}, row.SessionFamilyUsageMins)
		}
	}
	require.True(t, found, "the overlapping app bucket must be present")
	// The existing watermark deliberately recomputes recent buckets.
	require.NoError(t, db.UpsertTemplateUsageStats(ctx, mapping))
	repeated, err := db.GetTemplateUsageStats(ctx, params)
	require.NoError(t, err)
	require.ElementsMatch(t, rows, repeated)
	registry["new_app"] = "renamed_family"
	mapping, err = json.Marshal(registry)
	require.NoError(t, err)
	require.NoError(t, db.UpsertTemplateUsageStats(ctx, mapping))
	repeated, err = db.GetTemplateUsageStats(ctx, params)
	require.NoError(t, err)
	require.Len(t, repeated, 4)
	found = false
	for _, row := range repeated {
		if row.UserID == user && row.TemplateID == template && row.StartTime.Equal(start) {
			found = true
			require.EqualValues(t, 1, row.SessionFamilyUsageMins["renamed_family"])
			require.NotContains(t, row.SessionFamilyUsageMins, "new_family")
			require.EqualValues(t, 1, row.SessionAppUsageMins["new_app"])
		}
	}
	require.True(t, found, "the recomputed app bucket must be present")
	// Live insights deduplicate the overlapping apps without half-hour caps.
	live, err := db.GetTemplateInsightsByTemplate(ctx, database.GetTemplateInsightsByTemplateParams{
		StartTime: start.Add(30 * time.Second), EndTime: start.Add(30 * time.Minute), AppFamilies: mapping,
	})
	require.NoError(t, err)
	require.Len(t, live, 0, "no connected report occurs in this partial request window")
}
