CREATE TABLE `notification_preferences_v7` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `user_id` bigint unsigned NOT NULL,
  `workspace_id` bigint unsigned DEFAULT NULL,
  `resource_type` varchar(64) DEFAULT NULL,
  `resource_id` bigint unsigned DEFAULT NULL,
  `family` varchar(64) NOT NULL,
  `event_type` varchar(64) NOT NULL,
  `channel` varchar(32) NOT NULL,
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  `rule_key` varchar(191) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_notification_preferences_rule_key` (`rule_key`),
  KEY `idx_notification_preferences_user_id` (`user_id`),
  KEY `idx_notification_preferences_workspace_id` (`workspace_id`),
  KEY `idx_notification_preferences_resource_type` (`resource_type`),
  KEY `idx_notification_preferences_resource_id` (`resource_id`),
  KEY `idx_notification_preferences_family` (`family`),
  KEY `idx_notification_preferences_event_type` (`event_type`),
  KEY `idx_notification_preferences_channel` (`channel`),
  CONSTRAINT `fk_notification_preferences_v7_user` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`),
  CONSTRAINT `fk_notification_preferences_v7_workspace` FOREIGN KEY (`workspace_id`) REFERENCES `workspaces` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT INTO `notification_preferences_v7` (`created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, `event_type`, `channel`, `enabled`, `rule_key`)
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'workspace.invitation.created', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':workspace.invitation.created:', `channel`)
FROM `notification_preferences` WHERE `family` = 'workspace.invitation'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'workspace.invitation.accepted', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':workspace.invitation.accepted:', `channel`)
FROM `notification_preferences` WHERE `family` = 'workspace.invitation'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'workspace.member.role_updated', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':workspace.member.role_updated:', `channel`)
FROM `notification_preferences` WHERE `family` = 'workspace.member'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'workspace.member.removed', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':workspace.member.removed:', `channel`)
FROM `notification_preferences` WHERE `family` = 'workspace.member'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'agent.approved', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':agent.approved:', `channel`)
FROM `notification_preferences` WHERE `family` = 'agent.lifecycle'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'agent.rejected', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':agent.rejected:', `channel`)
FROM `notification_preferences` WHERE `family` = 'agent.lifecycle'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'agent.removed', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':agent.removed:', `channel`)
FROM `notification_preferences` WHERE `family` = 'agent.lifecycle'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'agent.offline', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':agent.offline:', `channel`)
FROM `notification_preferences` WHERE `family` = 'agent.lifecycle'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'pipeline.run.succeeded', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':pipeline.run.succeeded:', `channel`)
FROM `notification_preferences` WHERE `family` = 'pipeline.run'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'pipeline.run.failed', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':pipeline.run.failed:', `channel`)
FROM `notification_preferences` WHERE `family` = 'pipeline.run'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'pipeline.run.cancelled', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':pipeline.run.cancelled:', `channel`)
FROM `notification_preferences` WHERE `family` = 'pipeline.run'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'deployment.request.created', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':deployment.request.created:', `channel`)
FROM `notification_preferences` WHERE `family` = 'deployment.request'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'deployment.request.queued', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':deployment.request.queued:', `channel`)
FROM `notification_preferences` WHERE `family` = 'deployment.request'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'deployment.request.running', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':deployment.request.running:', `channel`)
FROM `notification_preferences` WHERE `family` = 'deployment.request'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'deployment.request.succeeded', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':deployment.request.succeeded:', `channel`)
FROM `notification_preferences` WHERE `family` = 'deployment.request'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'deployment.request.failed', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':deployment.request.failed:', `channel`)
FROM `notification_preferences` WHERE `family` = 'deployment.request'
UNION ALL
SELECT `created_at`, `updated_at`, `user_id`, `workspace_id`, `resource_type`, `resource_id`, `family`, 'deployment.request.cancelled', `channel`, `enabled`,
  CONCAT(`user_id`, ':', COALESCE(CAST(`workspace_id` AS CHAR), '*'), ':', COALESCE(NULLIF(`resource_type`, ''), '*'), ':', COALESCE(CAST(`resource_id` AS CHAR), '*'), ':deployment.request.cancelled:', `channel`)
FROM `notification_preferences` WHERE `family` = 'deployment.request';

DROP TABLE `notification_preferences`;
RENAME TABLE `notification_preferences_v7` TO `notification_preferences`;
