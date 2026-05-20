ALTER TABLE `pipeline_triggers`
  ADD COLUMN `webhook_runtime_input_mappings` longtext DEFAULT NULL AFTER `last_event_types`,
  ADD COLUMN `webhook_config_status` varchar(32) NOT NULL DEFAULT 'valid' AFTER `webhook_runtime_input_mappings`,
  ADD COLUMN `webhook_config_invalid_reason` text DEFAULT NULL AFTER `webhook_config_status`;