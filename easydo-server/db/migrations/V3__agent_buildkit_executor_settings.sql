ALTER TABLE `agents`
  ADD COLUMN `task_concurrency` bigint NOT NULL DEFAULT 5 AFTER `max_concurrent_pipelines`,
  ADD COLUMN `dockerhub_mirrors` longtext DEFAULT NULL AFTER `task_concurrency`,
  ADD COLUMN `dockerhub_mirrors_configured` tinyint(1) NOT NULL DEFAULT 0 AFTER `dockerhub_mirrors`;

CREATE TABLE `system_settings` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `key` varchar(128) NOT NULL,
  `value` longtext,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_system_settings_key` (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
