-- The fixed columns are NOT NULL with no default, so add them with a default
-- to fill existing rows, then drop the default to restore the original schema.
ALTER TABLE template_usage_stats
	ADD COLUMN ssh_mins smallint DEFAULT 0 NOT NULL,
	ADD COLUMN sftp_mins smallint DEFAULT 0 NOT NULL,
	ADD COLUMN reconnecting_pty_mins smallint DEFAULT 0 NOT NULL,
	ADD COLUMN vscode_mins smallint DEFAULT 0 NOT NULL,
	ADD COLUMN jetbrains_mins smallint DEFAULT 0 NOT NULL;

ALTER TABLE template_usage_stats
	ALTER COLUMN ssh_mins DROP DEFAULT,
	ALTER COLUMN sftp_mins DROP DEFAULT,
	ALTER COLUMN reconnecting_pty_mins DROP DEFAULT,
	ALTER COLUMN vscode_mins DROP DEFAULT,
	ALTER COLUMN jetbrains_mins DROP DEFAULT;

COMMENT ON COLUMN template_usage_stats.ssh_mins IS 'Total minutes the user has been using SSH.';

COMMENT ON COLUMN template_usage_stats.sftp_mins IS 'Total minutes the user has been using SFTP.';

COMMENT ON COLUMN template_usage_stats.reconnecting_pty_mins IS 'Total minutes the user has been using the reconnecting PTY.';

COMMENT ON COLUMN template_usage_stats.vscode_mins IS 'Total minutes the user has been using VSCode.';

COMMENT ON COLUMN template_usage_stats.jetbrains_mins IS 'Total minutes the user has been using JetBrains.';

-- Restore the five families the fixed columns have room for. Usage attributed
-- to any other family is discarded, and so is all per-app session usage: the
-- fixed columns have nowhere to put either.
UPDATE template_usage_stats
SET
	ssh_mins = COALESCE((session_family_usage_mins ->> 'ssh')::smallint, 0),
	sftp_mins = COALESCE((session_family_usage_mins ->> 'sftp')::smallint, 0),
	reconnecting_pty_mins = COALESCE((session_family_usage_mins ->> 'reconnecting_pty')::smallint, 0),
	vscode_mins = COALESCE((session_family_usage_mins ->> 'vscode')::smallint, 0),
	jetbrains_mins = COALESCE((session_family_usage_mins ->> 'jetbrains')::smallint, 0)
WHERE session_family_usage_mins <> '{}'::jsonb;

ALTER TABLE template_usage_stats
	DROP COLUMN session_app_usage_mins,
	DROP COLUMN session_family_usage_mins;
