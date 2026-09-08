package migrations_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/database/migrations"
	"github.com/coder/coder/v2/testutil"
)

// TestMigration000591TemplateUsageStatsSessionUsage covers the conversion of
// the fixed per-family minute columns into the family map, which the
// testdata/fixtures run does not reach: its template_usage_stats rows record
// no session minutes, so the backfill matches zero rows in CI.
//
//nolint:tparallel,paralleltest // Subtests share one database with transaction-local fixtures.
func TestMigration000591TemplateUsageStatsSessionUsage(t *testing.T) {
	t.Parallel()

	const priorMigrationVersion = 590

	sqlDB := testSQLDB(t)
	next, err := migrations.Stepper(sqlDB)
	require.NoError(t, err)
	for {
		version, more, err := next()
		require.NoError(t, err)
		if !more {
			t.Fatalf("migration %d not found", priorMigrationVersion)
		}
		if version == priorMigrationVersion {
			break
		}
	}

	ctx := testutil.Context(t, testutil.WaitSuperLong)
	migrationSQL, err := os.ReadFile("000591_template_usage_stats_session_usage.up.sql")
	require.NoError(t, err)
	downSQL, err := os.ReadFile("000591_template_usage_stats_session_usage.down.sql")
	require.NoError(t, err)

	// insertUsageStats writes one row per minute set, keyed by
	// (ssh, sftp, reconnecting_pty, vscode, jetbrains).
	insertUsageStats := func(t *testing.T, tx *sql.Tx, mins ...[5]int) {
		t.Helper()

		for i, m := range mins {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO template_usage_stats (
					start_time, end_time, template_id, user_id, median_latency_ms,
					usage_mins, ssh_mins, sftp_mins, reconnecting_pty_mins,
					vscode_mins, jetbrains_mins, app_usage_mins
				) VALUES (
					date_trunc('hour', statement_timestamp()) + $1::bigint * interval '30 minutes',
					date_trunc('hour', statement_timestamp()) + $1::bigint * interval '30 minutes' + interval '30 minutes',
					gen_random_uuid(), gen_random_uuid(), NULL, 30, $2, $3, $4, $5, $6, NULL
				)
			`, i, m[0], m[1], m[2], m[3], m[4])
			require.NoError(t, err)
		}
	}

	t.Run("up", func(t *testing.T) {
		// wantFamilyMins is ordered by start_time, matching the insert order.
		// Per-app minutes stay null: the fixed columns only ever recorded the
		// family, so there is nothing to attribute to an app name. The rollup
		// re-upserts its rewind window and can later write a map for such a
		// bucket, which this migration does not try to prevent.
		tests := []struct {
			name           string
			mins           [][5]int
			wantFamilyMins []string
		}{
			{name: "no rows"},
			{
				name:           "no session minutes",
				mins:           [][5]int{{0, 0, 0, 0, 0}},
				wantFamilyMins: []string{`{}`},
			},
			{
				name:           "every family",
				mins:           [][5]int{{1, 2, 3, 4, 5}},
				wantFamilyMins: []string{`{"ssh": 1, "sftp": 2, "reconnecting_pty": 3, "vscode": 4, "jetbrains": 5}`},
			},
			{
				// The rollup has never written sftp_mins, but a row that has
				// a value must not lose it.
				name:           "sftp only",
				mins:           [][5]int{{0, 7, 0, 0, 0}},
				wantFamilyMins: []string{`{"sftp": 7}`},
			},
			{
				name:           "zero minutes are dropped per family",
				mins:           [][5]int{{5, 0, 0, 6, 0}},
				wantFamilyMins: []string{`{"ssh": 5, "vscode": 6}`},
			},
			{
				name:           "mixed rows",
				mins:           [][5]int{{0, 0, 0, 0, 0}, {2, 0, 0, 0, 0}, {0, 0, 0, 0, 3}},
				wantFamilyMins: []string{`{}`, `{"ssh": 2}`, `{"jetbrains": 3}`},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tx, err := sqlDB.BeginTx(ctx, nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = tx.Rollback() })

				insertUsageStats(t, tx, tt.mins...)

				_, err = tx.ExecContext(ctx, string(migrationSQL))
				require.NoError(t, err)

				rows, err := tx.QueryContext(ctx, `
					SELECT session_family_usage_mins, session_app_usage_mins
					FROM template_usage_stats
					ORDER BY start_time
				`)
				require.NoError(t, err)
				defer rows.Close()
				var gotFamilyMins []string
				for rows.Next() {
					var familyMins []byte
					var appMins []byte
					require.NoError(t, rows.Scan(&familyMins, &appMins))
					require.Nil(t, appMins, "per-app minutes are unknown for rows the fixed columns produced")
					gotFamilyMins = append(gotFamilyMins, string(familyMins))
				}
				require.NoError(t, rows.Err())
				require.Len(t, gotFamilyMins, len(tt.wantFamilyMins))
				for i, want := range tt.wantFamilyMins {
					require.JSONEq(t, want, gotFamilyMins[i])
				}
			})
		}
	})

	// The map is written before the fixed columns are dropped, so a round trip
	// keeps the minutes the fixed columns have room for.
	t.Run("round trip", func(t *testing.T) {
		tx, err := sqlDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })

		insertUsageStats(t, tx, [5]int{1, 2, 3, 4, 5})

		_, err = tx.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(downSQL))
		require.NoError(t, err)

		var ssh, sftp, reconnectingPTY, vscode, jetbrains int64
		err = tx.QueryRowContext(ctx, `
			SELECT ssh_mins, sftp_mins, reconnecting_pty_mins, vscode_mins, jetbrains_mins
			FROM template_usage_stats
		`).Scan(&ssh, &sftp, &reconnectingPTY, &vscode, &jetbrains)
		require.NoError(t, err)
		require.EqualValues(t, 1, ssh)
		require.EqualValues(t, 2, sftp)
		require.EqualValues(t, 3, reconnectingPTY)
		require.EqualValues(t, 4, vscode)
		require.EqualValues(t, 5, jetbrains)
	})

	// The fixed columns have room for five families and no room at all for
	// per-app minutes, so anything else the maps recorded is lost.
	t.Run("down restores known families only", func(t *testing.T) {
		tx, err := sqlDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })

		_, err = tx.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err)

		_, err = tx.ExecContext(ctx, `
			INSERT INTO template_usage_stats (
				start_time, end_time, template_id, user_id, median_latency_ms,
				usage_mins, app_usage_mins, session_app_usage_mins,
				session_family_usage_mins
			) VALUES (
				date_trunc('hour', statement_timestamp()),
				date_trunc('hour', statement_timestamp()) + interval '30 minutes',
				gen_random_uuid(), gen_random_uuid(), NULL, 30, NULL,
				'{"vscode": 2, "some_future_ide": 3}'::jsonb,
				'{"vscode": 2, "some_future_family": 3}'::jsonb
			)
		`)
		require.NoError(t, err)

		_, err = tx.ExecContext(ctx, string(downSQL))
		require.NoError(t, err)

		var ssh, sftp, reconnectingPTY, vscode, jetbrains int64
		err = tx.QueryRowContext(ctx, `
			SELECT ssh_mins, sftp_mins, reconnecting_pty_mins, vscode_mins, jetbrains_mins
			FROM template_usage_stats
		`).Scan(&ssh, &sftp, &reconnectingPTY, &vscode, &jetbrains)
		require.NoError(t, err)
		require.EqualValues(t, 2, vscode)
		require.EqualValues(t, 0, ssh)
		require.EqualValues(t, 0, sftp)
		require.EqualValues(t, 0, reconnectingPTY)
		require.EqualValues(t, 0, jetbrains)

		// The columns the up migration dropped are the only ones left, so the
		// per-app minutes and the unknown family have nowhere to be read from.
		var columns int
		err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = 'template_usage_stats'
				AND column_name IN ('session_app_usage_mins', 'session_family_usage_mins')
		`).Scan(&columns)
		require.NoError(t, err)
		require.Zero(t, columns)
	})

	// A row the rollup writes with no session activity records an empty map
	// rather than null, which the NOT NULL column enforces.
	t.Run("family map is not null", func(t *testing.T) {
		tx, err := sqlDB.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tx.Rollback() })

		_, err = tx.ExecContext(ctx, string(migrationSQL))
		require.NoError(t, err)

		// A failed statement aborts the transaction, so keep it inside a
		// savepoint the rest of the test can roll back to.
		_, err = tx.ExecContext(ctx, `SAVEPOINT before_null_insert`)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO template_usage_stats (
				start_time, end_time, template_id, user_id, median_latency_ms,
				usage_mins, app_usage_mins, session_family_usage_mins
			) VALUES (
				date_trunc('hour', statement_timestamp()),
				date_trunc('hour', statement_timestamp()) + interval '30 minutes',
				gen_random_uuid(), gen_random_uuid(), NULL, 30, NULL, NULL
			)
		`)
		require.ErrorContains(t, err, "session_family_usage_mins")
		_, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT before_null_insert`)
		require.NoError(t, err)

		// Omitting it entirely falls back to the empty map.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO template_usage_stats (
				start_time, end_time, template_id, user_id, median_latency_ms,
				usage_mins, app_usage_mins
			) VALUES (
				date_trunc('hour', statement_timestamp()),
				date_trunc('hour', statement_timestamp()) + interval '30 minutes',
				gen_random_uuid(), gen_random_uuid(), NULL, 30, NULL
			)
		`)
		require.NoError(t, err)

		var familyMins []byte
		var appMins []byte
		err = tx.QueryRowContext(ctx, `
			SELECT session_family_usage_mins, session_app_usage_mins
			FROM template_usage_stats
		`).Scan(&familyMins, &appMins)
		require.NoError(t, err)
		require.JSONEq(t, `{}`, string(familyMins))
		require.Nil(t, appMins)
	})
}

// TestMigration000591ChainFrom589 walks the whole window this change spans,
// 589 up to 591 and back down to 589, with data present at every step. The
// isolated 591 tests start at 590, so they never see 590 converting raw
// session counts the rollup has not consumed, which is the state an upgrade
// actually finds.
//
//nolint:tparallel,paralleltest // Subtests share one database with transaction-local fixtures.
func TestMigration000591ChainFrom589(t *testing.T) {
	t.Parallel()

	const priorMigrationVersion = 589

	sqlDB := testSQLDB(t)
	next, err := migrations.Stepper(sqlDB)
	require.NoError(t, err)
	for {
		version, more, err := next()
		require.NoError(t, err)
		if !more {
			t.Fatalf("migration %d not found", priorMigrationVersion)
		}
		if version == priorMigrationVersion {
			break
		}
	}

	ctx := testutil.Context(t, testutil.WaitSuperLong)
	up590, err := os.ReadFile("000590_workspace_agent_session_counts.up.sql")
	require.NoError(t, err)
	down590, err := os.ReadFile("000590_workspace_agent_session_counts.down.sql")
	require.NoError(t, err)
	up591, err := os.ReadFile("000591_template_usage_stats_session_usage.up.sql")
	require.NoError(t, err)
	down591, err := os.ReadFile("000591_template_usage_stats_session_usage.down.sql")
	require.NoError(t, err)

	// backlogHours spans more than a day, and two backlogged rows sit inside
	// that span, so 590's backlog warning branch runs. That is the slow path an
	// upgrade of a stalled deployment takes.
	const backlogHours = 48

	// chainStep names a migration file so a failure says which step broke.
	type chainStep struct {
		name string
		sql  []byte
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	// One rolled-up half hour, so 590 has a watermark to measure the backlog
	// against, and so 591 has a row whose fixed family minutes must convert.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO template_usage_stats (
			start_time, end_time, template_id, user_id, median_latency_ms,
			usage_mins, ssh_mins, sftp_mins, reconnecting_pty_mins,
			vscode_mins, jetbrains_mins, app_usage_mins
		) VALUES (
			date_trunc('hour', statement_timestamp()) - $1::bigint * interval '1 hour',
			date_trunc('hour', statement_timestamp()) - $1::bigint * interval '1 hour' + interval '30 minutes',
			'22222222-2222-2222-2222-222222222222'::uuid,
			'11111111-1111-1111-1111-111111111111'::uuid,
			NULL, 30, 3, 2, 0, 4, 0, NULL
		)
	`, backlogHours)
	require.NoError(t, err)

	// Raw agent stats the rollup has not consumed: two inside the backlog,
	// spanning more than a day so 590 measures a wide backlog and takes its
	// warning path, and one older than the window 590 converts, which is the
	// watermark less the day DeleteOldWorkspaceAgentStats retains. 590 leaves
	// that one as an empty map because template_usage_stats accounts for it.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workspace_agent_stats (
			id, created_at, user_id, agent_id, workspace_id, template_id,
			connection_count, session_count_vscode, session_count_ssh
		) VALUES (
			gen_random_uuid(), statement_timestamp() - interval '5 minutes',
			'11111111-1111-1111-1111-111111111111'::uuid, gen_random_uuid(),
			gen_random_uuid(), '22222222-2222-2222-2222-222222222222'::uuid, 1, 2, 1
		), (
			gen_random_uuid(), statement_timestamp() - ($1::bigint - 1) * interval '1 hour',
			'11111111-1111-1111-1111-111111111111'::uuid, gen_random_uuid(),
			gen_random_uuid(), '22222222-2222-2222-2222-222222222222'::uuid, 1, 4, 2
		), (
			gen_random_uuid(), statement_timestamp() - $1::bigint * interval '1 hour' - interval '30 hours',
			'11111111-1111-1111-1111-111111111111'::uuid, gen_random_uuid(),
			gen_random_uuid(), '22222222-2222-2222-2222-222222222222'::uuid, 1, 5, 5
		)
	`, backlogHours)
	require.NoError(t, err)

	for _, step := range []chainStep{{"590 up", up590}, {"591 up", up591}} {
		_, err = tx.ExecContext(ctx, string(step.sql))
		require.NoError(t, err, "%s", step.name)
	}

	// 590 converted both backlogged rows under the canonical family names, and
	// zeroed the row the rollup had already consumed.
	rows, err := tx.QueryContext(ctx, `
		SELECT session_counts FROM workspace_agent_stats ORDER BY created_at
	`)
	require.NoError(t, err)
	var gotCounts []string
	for rows.Next() {
		var counts []byte
		require.NoError(t, rows.Scan(&counts))
		gotCounts = append(gotCounts, string(counts))
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Len(t, gotCounts, 3)
	require.JSONEq(t, `{}`, gotCounts[0], "older than the converted window")
	require.JSONEq(t, `{"vscode": 4, "ssh": 2}`, gotCounts[1], "backlogged, over a day old")
	require.JSONEq(t, `{"vscode": 2, "ssh": 1}`, gotCounts[2], "backlogged, recent")

	// 591 converted the fixed family minutes and left per-app minutes unknown.
	var familyMins []byte
	var appMins []byte
	err = tx.QueryRowContext(ctx, `
		SELECT session_family_usage_mins, session_app_usage_mins
		FROM template_usage_stats
	`).Scan(&familyMins, &appMins)
	require.NoError(t, err)
	require.JSONEq(t, `{"ssh": 3, "sftp": 2, "vscode": 4}`, string(familyMins))
	require.Nil(t, appMins)

	// Back down: 591 restores the fixed columns, then 590 restores the fixed
	// session counts, landing on the 589 schema.
	for _, step := range []chainStep{{"591 down", down591}, {"590 down", down590}} {
		_, err = tx.ExecContext(ctx, string(step.sql))
		require.NoError(t, err, "%s", step.name)
	}

	var ssh, sftp, reconnectingPTY, vscode, jetbrains int64
	err = tx.QueryRowContext(ctx, `
		SELECT ssh_mins, sftp_mins, reconnecting_pty_mins, vscode_mins, jetbrains_mins
		FROM template_usage_stats
	`).Scan(&ssh, &sftp, &reconnectingPTY, &vscode, &jetbrains)
	require.NoError(t, err)
	require.EqualValues(t, 3, ssh)
	require.EqualValues(t, 2, sftp)
	require.EqualValues(t, 0, reconnectingPTY)
	require.EqualValues(t, 4, vscode)
	require.EqualValues(t, 0, jetbrains)

	var backloggedVSCode, backloggedSSH int64
	err = tx.QueryRowContext(ctx, `
		SELECT session_count_vscode, session_count_ssh
		FROM workspace_agent_stats
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(&backloggedVSCode, &backloggedSSH)
	require.NoError(t, err)
	require.EqualValues(t, 2, backloggedVSCode)
	require.EqualValues(t, 1, backloggedSSH)

	// The columns both migrations added are gone, so the 589 schema is back.
	var mapColumns int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE (table_name = 'template_usage_stats'
				AND column_name IN ('session_app_usage_mins', 'session_family_usage_mins'))
			OR (table_name = 'workspace_agent_stats' AND column_name = 'session_counts')
	`).Scan(&mapColumns)
	require.NoError(t, err)
	require.Zero(t, mapColumns)
}
