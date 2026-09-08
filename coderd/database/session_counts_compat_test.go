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
)

// The six raw session count aggregates report per-app sums instead of fixed
// per-family columns, which moved the sum sites into their own subqueries and
// CTEs. These tests pin the behavior that move can silently break: an agent
// whose latest row reports no sessions must keep its row with zero sessions
// rather than disappear or inherit the previous row's counts, and the byte,
// latency, and connection aggregates must not be multiplied by the number of
// app names in a row. App names here are deliberately generic, so the tests
// describe the queries rather than the family registry.

// appSessionCounts decodes a session_counts column as the queries report it,
// keyed by app name.
func appSessionCounts(t *testing.T, data json.RawMessage) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	if len(data) > 0 {
		require.NoError(t, json.Unmarshal(data, &counts))
	}
	return counts
}

func TestSessionCountsCompatGetWorkspaceAgentStats(t *testing.T) {
	t.Parallel()

	db, _ := dbtestutil.NewDB(t)
	ctx := context.Background()
	now := dbtime.Now()

	// Each agent needs stable owner columns because the query groups by them.
	emptyLatest := statOwner{}.new()
	countsLatest := statOwner{}.new()

	// The latest row reports no sessions at all.
	emptyLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-2 * time.Minute),
		RxBytes:                   10,
		TxBytes:                   1,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 2}),
	})
	emptyLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-time.Minute),
		RxBytes:                   10,
		TxBytes:                   1,
		ConnectionMedianLatencyMS: 5,
	})

	// The latest row does not report latency, so it feeds sessions but not
	// the byte aggregates.
	countsLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-2 * time.Minute),
		RxBytes:                   5,
		TxBytes:                   5,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 1}),
	})
	countsLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:     now.Add(-time.Minute),
		RxBytes:       5,
		TxBytes:       5,
		SessionCounts: dbgen.SessionCounts(t, map[string]int64{"app_one": 3, "app_two": 4}),
	})

	stats, err := db.GetWorkspaceAgentStats(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, stats, 2, "an agent whose latest row reports no sessions must keep its row")

	byAgent := make(map[uuid.UUID]database.GetWorkspaceAgentStatsRow, len(stats))
	for _, stat := range stats {
		byAgent[stat.AgentID] = stat
	}

	empty, ok := byAgent[emptyLatest.agentID]
	require.True(t, ok)
	require.Empty(t, appSessionCounts(t, empty.SessionCounts), "the previous row's sessions must not be resurrected")
	require.Equal(t, int64(20), empty.WorkspaceRxBytes)
	require.Equal(t, int64(2), empty.WorkspaceTxBytes)

	counted, ok := byAgent[countsLatest.agentID]
	require.True(t, ok)
	require.Equal(t, map[string]int64{"app_one": 3, "app_two": 4}, appSessionCounts(t, counted.SessionCounts),
		"sessions come from the latest row regardless of whether it reports latency")
	require.Equal(t, int64(5), counted.WorkspaceRxBytes, "rows without latency stay out of the byte sums")
	require.Equal(t, int64(5), counted.WorkspaceTxBytes)
}

func TestSessionCountsCompatGetWorkspaceAgentUsageStats(t *testing.T) {
	t.Parallel()

	db, _ := dbtestutil.NewDB(t)
	ctx := context.Background()
	// The query ignores the current partial minute, so anchor on the last
	// complete one.
	latestMinute := dbtime.Now().Truncate(time.Minute).Add(-time.Minute)
	previousMinute := latestMinute.Add(-time.Minute)

	emptyLatest := statOwner{}.new()
	countsLatest := statOwner{}.new()

	emptyLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 previousMinute,
		Usage:                     true,
		RxBytes:                   10,
		TxBytes:                   1,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 2}),
	})
	emptyLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 latestMinute,
		Usage:                     true,
		RxBytes:                   10,
		TxBytes:                   1,
		ConnectionMedianLatencyMS: 5,
	})

	// The latest usage minute spans several rows, one of them without
	// latency.
	countsLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 previousMinute,
		Usage:                     true,
		RxBytes:                   1,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 1}),
	})
	countsLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 latestMinute,
		Usage:                     true,
		RxBytes:                   1,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 1, "app_two": 3}),
	})
	countsLatest.insert(t, db, database.WorkspaceAgentStat{
		CreatedAt:     latestMinute.Add(10 * time.Second),
		Usage:         true,
		RxBytes:       1,
		SessionCounts: dbgen.SessionCounts(t, map[string]int64{"app_one": 2}),
	})

	stats, err := db.GetWorkspaceAgentUsageStats(ctx, latestMinute.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, stats, 2, "an agent whose latest usage minute reports no sessions must keep its row")

	byAgent := make(map[uuid.UUID]database.GetWorkspaceAgentUsageStatsRow, len(stats))
	for _, stat := range stats {
		byAgent[stat.AgentID] = stat
	}

	empty, ok := byAgent[emptyLatest.agentID]
	require.True(t, ok)
	require.Empty(t, appSessionCounts(t, empty.SessionCounts), "the previous minute's sessions must not be resurrected")
	require.Equal(t, int64(20), empty.WorkspaceRxBytes)
	require.Equal(t, int64(2), empty.WorkspaceTxBytes)

	counted, ok := byAgent[countsLatest.agentID]
	require.True(t, ok)
	require.Equal(t, map[string]int64{"app_one": 3, "app_two": 3}, appSessionCounts(t, counted.SessionCounts),
		"every row in the latest usage minute contributes its sessions")
	require.Equal(t, int64(2), counted.WorkspaceRxBytes, "rows without latency stay out of the byte sums")
}

func TestSessionCountsCompatGetDeploymentWorkspaceAgentStats(t *testing.T) {
	t.Parallel()

	t.Run("NoRows", func(t *testing.T) {
		t.Parallel()

		db, _ := dbtestutil.NewDB(t)
		ctx := context.Background()

		stats, err := db.GetDeploymentWorkspaceAgentStats(ctx, dbtime.Now().Add(-time.Hour))
		require.NoError(t, err, "an empty deployment must still return one row")
		require.Empty(t, appSessionCounts(t, stats.SessionCounts))
		require.Equal(t, int64(0), stats.WorkspaceRxBytes)
		require.Equal(t, int64(0), stats.WorkspaceTxBytes)
		require.Equal(t, float64(-1), stats.WorkspaceConnectionLatency50)
		require.Equal(t, float64(-1), stats.WorkspaceConnectionLatency95)
	})

	t.Run("LatestRowPerAgent", func(t *testing.T) {
		t.Parallel()

		db, _ := dbtestutil.NewDB(t)
		ctx := context.Background()
		now := dbtime.Now()

		emptyLatest := statOwner{}.new()
		countsLatest := statOwner{}.new()

		emptyLatest.insert(t, db, database.WorkspaceAgentStat{
			CreatedAt:                 now.Add(-2 * time.Minute),
			RxBytes:                   10,
			TxBytes:                   1,
			ConnectionMedianLatencyMS: 4,
			SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 5}),
		})
		emptyLatest.insert(t, db, database.WorkspaceAgentStat{
			CreatedAt: now.Add(-time.Minute),
			RxBytes:   10,
			TxBytes:   1,
		})
		countsLatest.insert(t, db, database.WorkspaceAgentStat{
			CreatedAt:                 now.Add(-time.Minute),
			RxBytes:                   10,
			TxBytes:                   1,
			ConnectionMedianLatencyMS: 6,
			SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 2, "app_two": 1}),
		})

		stats, err := db.GetDeploymentWorkspaceAgentStats(ctx, now.Add(-time.Hour))
		require.NoError(t, err)

		require.Equal(t, map[string]int64{"app_one": 2, "app_two": 1}, appSessionCounts(t, stats.SessionCounts),
			"only the latest row per agent contributes sessions")
		require.Equal(t, int64(30), stats.WorkspaceRxBytes, "every row contributes bytes exactly once")
		require.Equal(t, int64(3), stats.WorkspaceTxBytes)
		// Percentiles over the two rows that report latency, 4 and 6.
		require.Equal(t, float64(5), stats.WorkspaceConnectionLatency50)
		require.InDelta(t, 5.9, stats.WorkspaceConnectionLatency95, 0.0001)
	})
}

func TestSessionCountsCompatGetDeploymentWorkspaceAgentUsageStats(t *testing.T) {
	t.Parallel()

	t.Run("NoRows", func(t *testing.T) {
		t.Parallel()

		db, _ := dbtestutil.NewDB(t)
		ctx := context.Background()

		stats, err := db.GetDeploymentWorkspaceAgentUsageStats(ctx, dbtime.Now().Add(-time.Hour))
		require.NoError(t, err, "an empty deployment must still return one row")
		require.Empty(t, appSessionCounts(t, stats.SessionCounts))
		require.Equal(t, int64(0), stats.WorkspaceRxBytes)
		require.Equal(t, int64(0), stats.WorkspaceTxBytes)
		require.Equal(t, float64(-1), stats.WorkspaceConnectionLatency50)
		require.Equal(t, float64(-1), stats.WorkspaceConnectionLatency95)
	})

	t.Run("LatestUsageMinutePerAgent", func(t *testing.T) {
		t.Parallel()

		db, _ := dbtestutil.NewDB(t)
		ctx := context.Background()
		latestMinute := dbtime.Now().Truncate(time.Minute).Add(-time.Minute)
		previousMinute := latestMinute.Add(-time.Minute)

		emptyLatest := statOwner{}.new()
		countsLatest := statOwner{}.new()

		emptyLatest.insert(t, db, database.WorkspaceAgentStat{
			CreatedAt:                 previousMinute,
			Usage:                     true,
			RxBytes:                   10,
			TxBytes:                   1,
			ConnectionMedianLatencyMS: 5,
			SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 5}),
		})
		emptyLatest.insert(t, db, database.WorkspaceAgentStat{
			CreatedAt:                 latestMinute,
			Usage:                     true,
			RxBytes:                   10,
			TxBytes:                   1,
			ConnectionMedianLatencyMS: 5,
		})
		countsLatest.insert(t, db, database.WorkspaceAgentStat{
			CreatedAt:                 latestMinute,
			Usage:                     true,
			RxBytes:                   5,
			TxBytes:                   1,
			ConnectionMedianLatencyMS: 5,
			SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_two": 2}),
		})

		stats, err := db.GetDeploymentWorkspaceAgentUsageStats(ctx, latestMinute.Add(-time.Hour))
		require.NoError(t, err)

		require.Equal(t, map[string]int64{"app_two": 2}, appSessionCounts(t, stats.SessionCounts),
			"only the latest usage minute per agent contributes sessions")
		require.Equal(t, int64(25), stats.WorkspaceRxBytes, "every row contributes bytes exactly once")
		require.Equal(t, int64(3), stats.WorkspaceTxBytes)
	})
}

func TestSessionCountsCompatGetWorkspaceAgentStatsAndLabels(t *testing.T) {
	t.Parallel()

	db, _ := dbtestutil.NewDB(t)
	ctx := context.Background()
	now := dbtime.Now()

	manyApps := newLabeledAgent(t, db)
	noApps := newLabeledAgent(t, db)

	// Several app names in one row must not multiply the connection or byte
	// aggregates.
	manyApps.insertStat(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-time.Minute),
		RxBytes:                   4,
		TxBytes:                   2,
		ConnectionCount:           2,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 1, "app_two": 2, "app_three": 3}),
	})
	noApps.insertStat(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-time.Minute),
		RxBytes:                   1,
		TxBytes:                   1,
		ConnectionCount:           7,
		ConnectionMedianLatencyMS: 5,
	})

	stats, err := db.GetWorkspaceAgentStatsAndLabels(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, stats, 2, "an agent whose latest row reports no sessions must keep its row")

	byAgentName := make(map[string]database.GetWorkspaceAgentStatsAndLabelsRow, len(stats))
	for _, stat := range stats {
		byAgentName[stat.AgentName] = stat
	}

	many, ok := byAgentName[manyApps.agentName]
	require.True(t, ok)
	require.Equal(t, map[string]int64{"app_one": 1, "app_two": 2, "app_three": 3}, appSessionCounts(t, many.SessionCounts))
	require.Equal(t, int64(2), many.ConnectionCount, "app names must not multiply the connection count")
	require.Equal(t, int64(4), many.RxBytes)
	require.Equal(t, int64(2), many.TxBytes)
	require.Equal(t, float64(5), many.ConnectionMedianLatencyMS)

	none, ok := byAgentName[noApps.agentName]
	require.True(t, ok)
	require.Empty(t, appSessionCounts(t, none.SessionCounts))
	require.Equal(t, int64(7), none.ConnectionCount)
	require.Equal(t, int64(1), none.RxBytes)
}

func TestSessionCountsCompatGetWorkspaceAgentUsageStatsAndLabels(t *testing.T) {
	t.Parallel()

	db, _ := dbtestutil.NewDB(t)
	ctx := context.Background()
	// This query only looks at the last minute of usage rows.
	now := dbtime.Now()

	manyApps := newLabeledAgent(t, db)
	noApps := newLabeledAgent(t, db)

	manyApps.insertStat(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-10 * time.Second),
		Usage:                     true,
		RxBytes:                   4,
		TxBytes:                   2,
		ConnectionCount:           2,
		ConnectionMedianLatencyMS: 5,
		SessionCounts:             dbgen.SessionCounts(t, map[string]int64{"app_one": 1, "app_two": 2, "app_three": 3}),
	})
	noApps.insertStat(t, db, database.WorkspaceAgentStat{
		CreatedAt:                 now.Add(-10 * time.Second),
		Usage:                     true,
		RxBytes:                   1,
		TxBytes:                   1,
		ConnectionCount:           7,
		ConnectionMedianLatencyMS: 5,
	})

	stats, err := db.GetWorkspaceAgentUsageStatsAndLabels(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, stats, 2, "an agent whose latest usage row reports no sessions must keep its row")

	byAgentName := make(map[string]database.GetWorkspaceAgentUsageStatsAndLabelsRow, len(stats))
	for _, stat := range stats {
		byAgentName[stat.AgentName] = stat
	}

	many, ok := byAgentName[manyApps.agentName]
	require.True(t, ok)
	require.Equal(t, map[string]int64{"app_one": 1, "app_two": 2, "app_three": 3}, appSessionCounts(t, many.SessionCounts))
	require.Equal(t, int64(2), many.ConnectionCount, "app names must not multiply the connection count")
	require.Equal(t, int64(4), many.RxBytes)
	require.Equal(t, int64(2), many.TxBytes)
	require.Equal(t, float64(5), many.ConnectionMedianLatencyMS)

	none, ok := byAgentName[noApps.agentName]
	require.True(t, ok)
	require.Empty(t, appSessionCounts(t, none.SessionCounts))
	require.Equal(t, int64(7), none.ConnectionCount)
	require.Equal(t, int64(1), none.RxBytes)
}

// statOwner holds the columns the stats queries group by, so repeated rows for
// one agent land in the same group. The queries that do not join labels never
// resolve these IDs, so they do not need matching rows.
type statOwner struct {
	userID      uuid.UUID
	agentID     uuid.UUID
	workspaceID uuid.UUID
	templateID  uuid.UUID
}

func (statOwner) new() statOwner {
	return statOwner{
		userID:      uuid.New(),
		agentID:     uuid.New(),
		workspaceID: uuid.New(),
		templateID:  uuid.New(),
	}
}

func (o statOwner) insert(t *testing.T, db database.Store, stat database.WorkspaceAgentStat) {
	t.Helper()
	stat.UserID = o.userID
	stat.AgentID = o.agentID
	stat.WorkspaceID = o.workspaceID
	stat.TemplateID = o.templateID
	dbgen.WorkspaceAgentStat(t, db, stat)
}

// labeledAgent is a statOwner whose IDs resolve to real users, workspaces, and
// agents, which the two queries that join labels require.
type labeledAgent struct {
	statOwner
	agentName string
}

func newLabeledAgent(t *testing.T, db database.Store) labeledAgent {
	t.Helper()
	user := dbgen.User(t, db, database.User{})
	org := dbgen.Organization(t, db, database.Organization{})
	job := dbgen.ProvisionerJob(t, db, nil, database.ProvisionerJob{
		OrganizationID: org.ID,
	})
	resource := dbgen.WorkspaceResource(t, db, database.WorkspaceResource{
		JobID: job.ID,
	})
	agent := dbgen.WorkspaceAgent(t, db, database.WorkspaceAgent{
		ResourceID: resource.ID,
	})
	template := dbgen.Template(t, db, database.Template{
		OrganizationID: org.ID,
		CreatedBy:      user.ID,
	})
	workspace := dbgen.Workspace(t, db, database.WorkspaceTable{
		OwnerID:        user.ID,
		OrganizationID: org.ID,
		TemplateID:     template.ID,
	})
	return labeledAgent{
		statOwner: statOwner{
			userID:      user.ID,
			agentID:     agent.ID,
			workspaceID: workspace.ID,
			templateID:  template.ID,
		},
		agentName: agent.Name,
	}
}

func (a labeledAgent) insertStat(t *testing.T, db database.Store, stat database.WorkspaceAgentStat) {
	t.Helper()
	a.insert(t, db, stat)
}
