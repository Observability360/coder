ALTER TABLE template_usage_stats
	ADD COLUMN session_app_usage_mins jsonb,
	ADD COLUMN session_family_usage_mins jsonb DEFAULT '{}'::jsonb NOT NULL;

COMMENT ON COLUMN template_usage_stats.session_app_usage_mins IS 'Total minutes the user has been using each app, keyed by the app name the agent reported. Agents that report only the fixed session counts, and history converted by migration 000590, report family names in this position, so a key can be a family aggregate rather than a distinct app. Null means the row was rolled up before the column existed.';

COMMENT ON COLUMN template_usage_stats.session_family_usage_mins IS 'Total minutes the user has been using each app family, keyed by family name. Empty means no session usage was recorded.';

-- Carry every family the fixed columns recorded into the map, sftp included:
-- the rollup has never written it, but a row that has a value must not lose
-- it. Zero minutes are dropped so a row lists only the families it saw, which
-- is what the rollup writes from now on. Per-app usage stays null for these
-- rows because the fixed columns only ever recorded the family.
--
-- Null is not a clean boundary in time: the rollup re-upserts its rewind
-- window, so a bucket near this migration can be rewritten with a per-app map
-- computed from converted rows, whose keys are family names. The column alone
-- cannot separate those keys from genuine app names.
UPDATE template_usage_stats
SET session_family_usage_mins = jsonb_strip_nulls(jsonb_build_object(
	'ssh', CASE WHEN ssh_mins > 0 THEN ssh_mins END,
	'sftp', CASE WHEN sftp_mins > 0 THEN sftp_mins END,
	'reconnecting_pty', CASE WHEN reconnecting_pty_mins > 0 THEN reconnecting_pty_mins END,
	'vscode', CASE WHEN vscode_mins > 0 THEN vscode_mins END,
	'jetbrains', CASE WHEN jetbrains_mins > 0 THEN jetbrains_mins END
))
WHERE
	ssh_mins > 0
	OR sftp_mins > 0
	OR reconnecting_pty_mins > 0
	OR vscode_mins > 0
	OR jetbrains_mins > 0;

ALTER TABLE template_usage_stats
	DROP COLUMN ssh_mins,
	DROP COLUMN sftp_mins,
	DROP COLUMN reconnecting_pty_mins,
	DROP COLUMN vscode_mins,
	DROP COLUMN jetbrains_mins;
