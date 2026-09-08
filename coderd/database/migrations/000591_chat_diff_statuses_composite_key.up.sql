-- One row per (chat, origin, branch) so a chat can track more than
-- one pull request. Existing rows already obey the new key: both
-- columns are NOT NULL with a default of '', and the old key allowed
-- only one row per chat.
-- Old coderd instances must stop before this migration runs: their
-- ON CONFLICT (chat_id) writes fail without the old key.
ALTER TABLE chat_diff_statuses
    DROP CONSTRAINT chat_diff_statuses_pkey;

ALTER TABLE chat_diff_statuses
    ADD PRIMARY KEY (chat_id, git_remote_origin, git_branch);
