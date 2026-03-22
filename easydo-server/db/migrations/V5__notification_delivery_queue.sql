ALTER TABLE `notification_deliveries`
  ADD COLUMN `attempt_count` bigint NOT NULL DEFAULT '0' AFTER `error_message`,
  ADD COLUMN `last_attempt_at` bigint DEFAULT NULL AFTER `attempt_count`,
  ADD COLUMN `next_retry_at` bigint DEFAULT NULL AFTER `last_attempt_at`,
  ADD KEY `idx_notification_deliveries_next_retry_at` (`next_retry_at`);
