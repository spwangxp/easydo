ALTER TABLE `ai_agents`
  ADD COLUMN `runtime_profile_id` bigint unsigned DEFAULT NULL AFTER `scope_type`;

UPDATE `ai_agents` AS `agent`
LEFT JOIN (
  SELECT `agent_id`, MAX(`id`) AS `runtime_profile_id`
  FROM `ai_runtime_profiles`
  GROUP BY `agent_id`
) AS `runtime_profile_map` ON `runtime_profile_map`.`agent_id` = `agent`.`id`
SET `agent`.`runtime_profile_id` = `runtime_profile_map`.`runtime_profile_id`
WHERE `agent`.`runtime_profile_id` IS NULL;

ALTER TABLE `ai_runtime_profiles`
  DROP FOREIGN KEY `fk_ai_runtime_profiles_agent`,
  DROP INDEX `idx_ai_runtime_profiles_agent_id`,
  DROP COLUMN `agent_id`;

ALTER TABLE `ai_agents`
  DROP INDEX `idx_ai_agents_scenario`,
  DROP COLUMN `scenario`,
  ADD COLUMN `tools_json` longtext AFTER `tool_policy_json`,
  ADD COLUMN `skills_json` longtext AFTER `tools_json`,
  ADD COLUMN `memory_json` longtext AFTER `skills_json`,
  ADD COLUMN `mcp_servers_json` longtext AFTER `memory_json`,
  ADD COLUMN `sub_agents_json` longtext AFTER `mcp_servers_json`,
  ADD COLUMN `metadata_json` longtext AFTER `sub_agents_json`,
  ADD KEY `idx_ai_agents_runtime_profile_id` (`runtime_profile_id`),
  ADD CONSTRAINT `fk_ai_agents_runtime_profile` FOREIGN KEY (`runtime_profile_id`) REFERENCES `ai_runtime_profiles` (`id`);