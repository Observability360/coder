package insights

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/codersdk"
)

func TestConvertTemplateInsights(t *testing.T) {
	t.Parallel()

	t.Run("Decodes", func(t *testing.T) {
		t.Parallel()

		templateID := uuid.New()
		rows, err := convertTemplateInsights([]database.GetTemplateInsightsByTemplateRow{
			{
				TemplateID:  templateID,
				ActiveUsers: 3,
				SessionFamilyUsageSeconds: json.RawMessage(`{
					"vscode": 60,
					"jetbrains": 120,
					"reconnecting_pty": 180,
					"ssh": 240,
					"unknown": 300
				}`),
			},
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, templateID, rows[0].templateID)
		require.EqualValues(t, 3, rows[0].activeUsers)
		require.EqualValues(t, 60, rows[0].usageSeconds(codersdk.AppFamilyVSCode))
		require.EqualValues(t, 120, rows[0].usageSeconds(codersdk.AppFamilyJetBrains))
		require.EqualValues(t, 180, rows[0].usageSeconds(codersdk.AppFamilyReconnectingPTY))
		require.EqualValues(t, 240, rows[0].usageSeconds(codersdk.AppFamilySSH))
	})

	t.Run("MissingFamily", func(t *testing.T) {
		t.Parallel()

		rows, err := convertTemplateInsights([]database.GetTemplateInsightsByTemplateRow{
			{
				TemplateID:                uuid.New(),
				SessionFamilyUsageSeconds: json.RawMessage(`{"ssh": 60}`),
			},
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		// A family the query did not report had no usage, and the collector
		// still emits its metric.
		require.EqualValues(t, 0, rows[0].usageSeconds(codersdk.AppFamilyVSCode))
		require.EqualValues(t, 60, rows[0].usageSeconds(codersdk.AppFamilySSH))
	})

	t.Run("NoRows", func(t *testing.T) {
		t.Parallel()

		rows, err := convertTemplateInsights(nil)
		require.NoError(t, err)
		require.Empty(t, rows)
	})

	t.Run("Malformed", func(t *testing.T) {
		t.Parallel()

		for name, raw := range map[string]json.RawMessage{
			"NotJSON":     json.RawMessage(`{`),
			"NotAnObject": json.RawMessage(`[1, 2]`),
			"WrongValue":  json.RawMessage(`{"ssh": "sixty"}`),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				// Reporting zero usage would look like an idle template, so
				// the collector must fail the tick instead.
				rows, err := convertTemplateInsights([]database.GetTemplateInsightsByTemplateRow{
					{TemplateID: uuid.New(), SessionFamilyUsageSeconds: raw},
				})
				require.Error(t, err)
				require.Nil(t, rows)
			})
		}
	})

	t.Run("AbsentPayload", func(t *testing.T) {
		t.Parallel()

		rows, err := convertTemplateInsights([]database.GetTemplateInsightsByTemplateRow{
			{TemplateID: uuid.New()},
		})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.NotNil(t, rows[0].usageSecondsByFamily)
		require.EqualValues(t, 0, rows[0].usageSeconds(codersdk.AppFamilySSH))
	})
}

func TestUniqueTemplateIDs(t *testing.T) {
	t.Parallel()

	templateID := uuid.New()
	otherTemplateID := uuid.New()

	ids := uniqueTemplateIDs(
		[]templateInsightsRow{{templateID: templateID}},
		[]database.GetTemplateAppInsightsByTemplateRow{{TemplateID: templateID}},
		[]parameterRow{{templateID: otherTemplateID}},
	)
	require.ElementsMatch(t, []uuid.UUID{templateID, otherTemplateID}, ids)
}
