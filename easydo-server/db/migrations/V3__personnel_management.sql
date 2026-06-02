-- Personnel management schema additions.

UPDATE `workspaces` SET `kind` = 'admin' WHERE `name` = 'AdminWorkspace' AND `created_by` IN (SELECT `id` FROM `users` WHERE `role` = 'admin');

ALTER TABLE `users` ADD COLUMN `must_change_password` tinyint(1) NOT NULL DEFAULT 0;
ALTER TABLE `users` ADD COLUMN `password_changed_at` bigint NOT NULL DEFAULT 0;
ALTER TABLE `users` ADD COLUMN `disabled_at` bigint NOT NULL DEFAULT 0;
ALTER TABLE `users` ADD COLUMN `disabled_by` bigint unsigned DEFAULT NULL;
ALTER TABLE `users` ADD KEY `idx_users_disabled_by` (`disabled_by`);
ALTER TABLE `users` ADD CONSTRAINT `fk_users_disabled_by` FOREIGN KEY (`disabled_by`) REFERENCES `users` (`id`);

UPDATE `users` SET `email` = CONCAT('user-', `id`, '@local.invalid') WHERE `email` IS NULL OR TRIM(`email`) = '';
ALTER TABLE `users` MODIFY COLUMN `email` varchar(128) NOT NULL;
CREATE UNIQUE INDEX `idx_users_email` ON `users` (`email`);

UPDATE `users` SET `phone` = '' WHERE `phone` IS NULL;
ALTER TABLE `users` MODIFY COLUMN `phone` varchar(20) NOT NULL DEFAULT '';
ALTER TABLE `users` ADD COLUMN `phone_unique` varchar(20) GENERATED ALWAYS AS (NULLIF(`phone`, '')) VIRTUAL;
CREATE UNIQUE INDEX `idx_users_phone` ON `users` (`phone_unique`);

CREATE TABLE IF NOT EXISTS `audit_logs` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `workspace_id` bigint unsigned DEFAULT NULL,
  `actor_user_id` bigint unsigned NOT NULL,
  `actor_role` varchar(32) DEFAULT NULL,
  `actor_workspace_id` bigint unsigned DEFAULT NULL,
  `action` varchar(96) NOT NULL,
  `target_type` varchar(64) NOT NULL,
  `target_id` bigint unsigned NOT NULL,
  `before_json` longtext DEFAULT NULL,
  `after_json` longtext DEFAULT NULL,
  `metadata_json` longtext DEFAULT NULL,
  `ip` varchar(64) DEFAULT NULL,
  `user_agent` varchar(512) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_audit_logs_workspace_id` (`workspace_id`),
  KEY `idx_audit_logs_actor_user_id` (`actor_user_id`),
  KEY `idx_audit_logs_actor_workspace_id` (`actor_workspace_id`),
  KEY `idx_audit_logs_action` (`action`),
  KEY `idx_audit_logs_target_type` (`target_type`),
  KEY `idx_audit_logs_target_id` (`target_id`),
  CONSTRAINT `fk_audit_logs_workspace` FOREIGN KEY (`workspace_id`) REFERENCES `workspaces` (`id`),
  CONSTRAINT `fk_audit_logs_actor_user` FOREIGN KEY (`actor_user_id`) REFERENCES `users` (`id`),
  CONSTRAINT `fk_audit_logs_actor_workspace` FOREIGN KEY (`actor_workspace_id`) REFERENCES `workspaces` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `notification_sender_configs` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `scope` varchar(32) NOT NULL,
  `workspace_id` bigint unsigned NOT NULL DEFAULT 0,
  `enabled` tinyint(1) DEFAULT 0,
  `from_name` varchar(128) DEFAULT NULL,
  `from_address` varchar(255) DEFAULT NULL,
  `smtp_host` varchar(255) DEFAULT NULL,
  `smtp_port` bigint DEFAULT NULL,
  `smtp_username` varchar(255) DEFAULT NULL,
  `smtp_password_encrypted` longtext DEFAULT NULL,
  `smtp_tls_mode` varchar(32) DEFAULT 'plain',
  `updated_by` bigint unsigned DEFAULT NULL,
  `last_tested_at` bigint DEFAULT NULL,
  `last_test_status` varchar(32) DEFAULT NULL,
  `last_test_error` text DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_notification_sender_scope_workspace` (`scope`,`workspace_id`),
  KEY `idx_notification_sender_configs_workspace_id` (`workspace_id`),
  KEY `idx_notification_sender_configs_updated_by` (`updated_by`),
  CONSTRAINT `fk_notification_sender_configs_updated_by` FOREIGN KEY (`updated_by`) REFERENCES `users` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
