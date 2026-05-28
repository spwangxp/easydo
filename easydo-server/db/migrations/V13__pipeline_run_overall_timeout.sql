ALTER TABLE `pipeline_runs`
  ADD COLUMN `timeout_seconds` bigint NOT NULL DEFAULT 0 AFTER `duration`,
  ADD COLUMN `timeout_deadline` bigint NOT NULL DEFAULT 0 AFTER `timeout_seconds`,
  ADD INDEX `idx_pipeline_runs_timeout_deadline` (`status`,`timeout_deadline`);
