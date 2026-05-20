ALTER TABLE `ai_sessions`
  DROP INDEX `idx_ai_sessions_scenario`,
  CHANGE COLUMN `scenario` `task_type` varchar(64) NOT NULL,
  ADD INDEX `idx_ai_sessions_task_type` (`task_type`);
