-- Durable labels for resource runtime metadata

CREATE TABLE `resource_runtime_labels` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `workspace_id` bigint unsigned NOT NULL,
  `resource_id` bigint unsigned NOT NULL,
  `target_type` varchar(64) NOT NULL,
  `target_key` varchar(255) NOT NULL,
  `labels_json` longtext NOT NULL,
  `created_by` bigint unsigned NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_resource_runtime_labels_target` (`workspace_id`,`resource_id`,`target_type`,`target_key`),
  KEY `idx_resource_runtime_labels_workspace_target` (`workspace_id`,`target_type`,`target_key`),
  KEY `idx_resource_runtime_labels_resource_target` (`resource_id`,`target_type`,`target_key`),
  KEY `idx_resource_runtime_labels_created_by` (`created_by`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
