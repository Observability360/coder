package coderd

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/codersdk"
)

func TestDecodeSessionFamilyMap(t *testing.T) {
	t.Parallel()

	t.Run("UsageSeconds", func(t *testing.T) {
		t.Parallel()

		got, err := decodeSessionFamilyMap[int64](json.RawMessage(`{"vscode": 60, "ssh": 120}`))
		require.NoError(t, err)
		require.Equal(t, map[codersdk.AppFamilyName]int64{
			codersdk.AppFamilyVSCode: 60,
			codersdk.AppFamilySSH:    120,
		}, got)
	})

	t.Run("TemplateIDs", func(t *testing.T) {
		t.Parallel()

		templateID := uuid.New()
		got, err := decodeSessionFamilyMap[[]uuid.UUID](json.RawMessage(`{"vscode": ["` + templateID.String() + `"]}`))
		require.NoError(t, err)
		require.Equal(t, map[codersdk.AppFamilyName][]uuid.UUID{
			codersdk.AppFamilyVSCode: {templateID},
		}, got)
	})

	t.Run("AbsentAndNull", func(t *testing.T) {
		t.Parallel()

		for name, raw := range map[string]json.RawMessage{
			"Nil":    nil,
			"Empty":  json.RawMessage(``),
			"Null":   json.RawMessage(`null`),
			"Object": json.RawMessage(`{}`),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := decodeSessionFamilyMap[int64](raw)
				require.NoError(t, err)
				require.NotNil(t, got, "callers index the map, it must never be nil")
				require.Empty(t, got)
			})
		}
	})

	t.Run("Malformed", func(t *testing.T) {
		t.Parallel()

		for name, raw := range map[string]json.RawMessage{
			"NotJSON":     json.RawMessage(`{`),
			"NotAnObject": json.RawMessage(`[1, 2]`),
			"WrongValue":  json.RawMessage(`{"vscode": "sixty"}`),
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				// A malformed payload must not decode to zero usage, or an
				// encoding bug would look like an idle deployment.
				_, err := decodeSessionFamilyMap[int64](raw)
				require.Error(t, err)
			})
		}
	})
}

func TestConvertTemplateInsightsApps(t *testing.T) {
	t.Parallel()

	vscodeTemplateID := uuid.New()
	sshTemplateID := uuid.New()
	sftpTemplateID := uuid.New()

	t.Run("Builtin", func(t *testing.T) {
		t.Parallel()

		usage := database.GetTemplateInsightsRow{
			SessionFamilyUsageSeconds: json.RawMessage(`{
				"vscode": 60,
				"jetbrains": 120,
				"reconnecting_pty": 180,
				"ssh": 240,
				"sftp": 300,
				"unknown": 360
			}`),
			SessionFamilyTemplateIds: json.RawMessage(`{
				"vscode": ["` + vscodeTemplateID.String() + `"],
				"ssh": ["` + sshTemplateID.String() + `"],
				"sftp": ["` + sftpTemplateID.String() + `"],
				"unknown": ["` + uuid.NewString() + `"]
			}`),
		}

		apps, err := convertTemplateInsightsApps(usage, nil)
		require.NoError(t, err)
		require.Equal(t, []codersdk.TemplateAppUsage{
			{
				TemplateIDs: []uuid.UUID{vscodeTemplateID},
				Type:        codersdk.TemplateAppsTypeBuiltin,
				DisplayName: codersdk.TemplateBuiltinAppDisplayNameVSCode,
				Slug:        "vscode",
				Icon:        "/icon/code.svg",
				Seconds:     60,
			},
			{
				TemplateIDs: []uuid.UUID{},
				Type:        codersdk.TemplateAppsTypeBuiltin,
				DisplayName: codersdk.TemplateBuiltinAppDisplayNameJetBrains,
				Slug:        "jetbrains",
				Icon:        "/icon/intellij.svg",
				Seconds:     120,
			},
			{
				TemplateIDs: []uuid.UUID{},
				Type:        codersdk.TemplateAppsTypeBuiltin,
				DisplayName: codersdk.TemplateBuiltinAppDisplayNameWebTerminal,
				Slug:        "reconnecting-pty",
				Icon:        "/icon/terminal.svg",
				Seconds:     180,
			},
			{
				TemplateIDs: []uuid.UUID{sshTemplateID},
				Type:        codersdk.TemplateAppsTypeBuiltin,
				DisplayName: codersdk.TemplateBuiltinAppDisplayNameSSH,
				Slug:        "ssh",
				Icon:        "/icon/terminal.svg",
				Seconds:     240,
			},
			{
				// The rollup no longer produces SFTP usage, but rows migrated
				// from the old sftp_mins column still report it.
				TemplateIDs: []uuid.UUID{sftpTemplateID},
				Type:        codersdk.TemplateAppsTypeBuiltin,
				DisplayName: codersdk.TemplateBuiltinAppDisplayNameSFTP,
				Slug:        "sftp",
				Icon:        "/icon/terminal.svg",
				Seconds:     300,
			},
		}, apps)
	})

	t.Run("NoUsage", func(t *testing.T) {
		t.Parallel()

		apps, err := convertTemplateInsightsApps(database.GetTemplateInsightsRow{
			SessionFamilyUsageSeconds: json.RawMessage(`{}`),
			SessionFamilyTemplateIds:  json.RawMessage(`{}`),
		}, nil)
		require.NoError(t, err)
		require.Len(t, apps, 5)
		for _, app := range apps {
			require.Zero(t, app.Seconds, "%s", app.Slug)
			// The API has always returned an empty list here, never null.
			require.NotNil(t, app.TemplateIDs, "%s", app.Slug)
			require.Empty(t, app.TemplateIDs, "%s", app.Slug)
			require.Equal(t, codersdk.TemplateAppsTypeBuiltin, app.Type, "%s", app.Slug)
		}
	})

	t.Run("CustomApps", func(t *testing.T) {
		t.Parallel()

		appTemplateID := uuid.New()
		apps, err := convertTemplateInsightsApps(database.GetTemplateInsightsRow{}, []database.GetTemplateAppInsightsRow{
			{
				TemplateIDs:  []uuid.UUID{appTemplateID},
				DisplayName:  "Zed",
				Slug:         "zed",
				Icon:         "/icon/zed.svg",
				UsageSeconds: 60,
				TimesUsed:    2,
			},
			{
				TemplateIDs:  []uuid.UUID{appTemplateID},
				DisplayName:  "App",
				Slug:         "app",
				Icon:         "/icon/app.svg",
				UsageSeconds: 30,
				TimesUsed:    1,
			},
		})
		require.NoError(t, err)
		require.Len(t, apps, 7)
		// Custom apps follow the builtins, sorted by slug.
		require.Equal(t, []codersdk.TemplateAppUsage{
			{
				TemplateIDs: []uuid.UUID{appTemplateID},
				Type:        codersdk.TemplateAppsTypeApp,
				DisplayName: "App",
				Slug:        "app",
				Icon:        "/icon/app.svg",
				Seconds:     30,
				TimesUsed:   1,
			},
			{
				TemplateIDs: []uuid.UUID{appTemplateID},
				Type:        codersdk.TemplateAppsTypeApp,
				DisplayName: "Zed",
				Slug:        "zed",
				Icon:        "/icon/zed.svg",
				Seconds:     60,
				TimesUsed:   2,
			},
		}, apps[5:])
	})

	t.Run("Malformed", func(t *testing.T) {
		t.Parallel()

		for name, usage := range map[string]database.GetTemplateInsightsRow{
			"UsageSeconds": {
				SessionFamilyUsageSeconds: json.RawMessage(`{"vscode": {}}`),
				SessionFamilyTemplateIds:  json.RawMessage(`{}`),
			},
			"TemplateIDs": {
				SessionFamilyUsageSeconds: json.RawMessage(`{}`),
				SessionFamilyTemplateIds:  json.RawMessage(`{"vscode": "not-a-uuid"}`),
			},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				apps, err := convertTemplateInsightsApps(usage, nil)
				require.Error(t, err)
				require.Nil(t, apps)
			})
		}
	})
}
