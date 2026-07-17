import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const schemaText = readFileSync(resolve(process.cwd(), 'db/runtime-schema.sql'), 'utf8')
const queueMigrationText = readFileSync(resolve(process.cwd(), 'db/V11__ai_session_queue_items.sql'), 'utf8')
const orchestrationMigrationText = readFileSync(resolve(process.cwd(), 'db/V12__ai_orchestration_tasks.sql'), 'utf8')
const mariadbStoreText = readFileSync(resolve(process.cwd(), 'src/store/mariadbRuntimeStore.ts'), 'utf8')

describe('runtime database schema', () => {
  it('declares generic AI runtime tables owned by easydo-ai-runtime', () => {
    for (const table of [
      'ai_agent_profiles',
      'ai_agent_profile_versions',
      'ai_agent_profile_resource_refs',
      'ai_agent_resources',
      'ai_agent_resource_versions',
      'ai_agent_profile_validation_results',
      'workspace_ai_settings',
      'ai_runtime_workspaces',
      'ai_runtime_workspace_audits',
      'ai_sessions',
      'ai_session_entries',
      'ai_runtime_runs',
      'ai_agent_actions',
      'ai_action_decisions',
      'ai_session_permission_grants',
      'ai_action_executions',
      'ai_action_events',
      'ai_agent_runtime_event_sequences',
      'ai_agent_runtime_events',
      'ai_runtime_artifacts',
      'ai_session_queue_items',
      'ai_child_run_links',
      'ai_orchestration_plans',
      'ai_orchestration_context_packs',
      'ai_orchestration_tasks'
    ]) {
      expect(schemaText).toContain(`CREATE TABLE IF NOT EXISTS \`${table}\``)
    }
  })

  it('defines durable per-session queue identity, ordering, and claim fields', () => {
    expect(schemaText).toContain('CREATE TABLE IF NOT EXISTS `ai_session_queue_items`')
    expect(schemaText).toContain('`client_item_id` varchar(64) NOT NULL')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_session_queue_client_item` (`workspace_id`,`session_id`,`client_item_id`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_session_queue_item_seq` (`workspace_id`,`session_id`,`item_seq`)')
    expect(schemaText).toContain('KEY `idx_ai_session_queue_pending_order` (`workspace_id`,`session_id`,`status`,`position`,`item_seq`)')
    expect(schemaText).toContain('`claim_epoch` bigint unsigned NOT NULL DEFAULT 0')
    expect(schemaText).toContain('`expires_at` datetime(3) DEFAULT NULL')
    expect(schemaText).toContain('`consumed_runtime_run_id` varchar(64) DEFAULT NULL')
    expect(queueMigrationText).toContain('CREATE TABLE IF NOT EXISTS `ai_session_queue_items`')
    expect(queueMigrationText).toContain('UNIQUE KEY `uk_ai_session_queue_client_item`')
    expect(queueMigrationText).toContain('`consumed_runtime_run_id` varchar(64) DEFAULT NULL')
  })

  it('uses bounded readable idempotency and event replay columns', () => {
    expect(schemaText).toContain('`idempotency_key` varchar(191) NOT NULL')
    expect(schemaText).toContain('`event_id` varchar(128) NOT NULL')
    expect(schemaText).toContain('`action_id` varchar(128) NOT NULL')
    expect(schemaText).toContain('`artifact_id` varchar(128) NOT NULL')
    expect(schemaText).toContain('`child_run_link_id` varchar(128) NOT NULL')
    expect(schemaText).toContain('`decision_id` varchar(160) NOT NULL')
    expect(schemaText).toContain('`grant_id` varchar(160) NOT NULL')
    expect(schemaText).toContain('`execution_id` varchar(160) NOT NULL')
    expect(schemaText).toContain('`display_json` longtext NOT NULL')
    expect(schemaText).toContain('`active_slot` varchar(16) DEFAULT NULL')
    expect(schemaText).toContain('`owner_instance_id` varchar(191) DEFAULT NULL')
    expect(schemaText).toContain('`owner_epoch` bigint unsigned NOT NULL DEFAULT 0')
    expect(schemaText).toContain('`owner_lease_expires_at` datetime(3) DEFAULT NULL')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_runtime_runs_active_root` (`workspace_id`,`session_id`,`active_slot`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_agent_actions_action_id` (`workspace_id`,`action_id`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_runtime_artifacts_artifact_id` (`workspace_id`,`artifact_id`)')
    expect(schemaText).toContain('KEY `idx_ai_session_permission_grants_match` (`workspace_id`,`session_id`,`permission_key`,`tool_name`,`resource_type`,`resource_id`,`status`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_child_run_links_child_run` (`workspace_id`,`child_runtime_run_id`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_action_events_run_seq` (`workspace_id`,`runtime_run_id`,`event_seq`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_agent_runtime_events_session_seq` (`session_id`,`event_seq`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_agent_runtime_events_event_id` (`event_id`)')
    expect(schemaText).toContain('KEY `idx_ai_agent_runtime_events_run_seq` (`runtime_run_id`,`event_seq`)')
    expect(schemaText).toContain('KEY `idx_ai_runtime_runs_owner_lease` (`status`,`owner_lease_expires_at`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_runtime_workspaces_owner_key` (`workspace_id`,`owner_user_id`,`workspace_key`)')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_runtime_workspaces_runtime_id` (`workspace_runtime_id`)')
    expect(schemaText).toContain('KEY `idx_ai_runtime_workspaces_lease` (`status`,`lease_expires_at`)')
    expect(schemaText).toContain('KEY `idx_ai_runtime_workspace_audits_workspace` (`workspace_id`,`workspace_runtime_id`,`created_at`)')
  })

  it('persists chatbox profile version keys and run profile snapshots', () => {
    expect(schemaText).toContain('`agent_profile_version_key` varchar(64) NOT NULL DEFAULT')
    expect(schemaText).toContain('`session_key` varchar(512) DEFAULT NULL')
    expect(schemaText).toContain('`first_session_timestamp` datetime(3) DEFAULT NULL')
    expect(schemaText).toContain('`source` varchar(64) DEFAULT NULL')
    expect(schemaText).toContain('`model_override_json` longtext NOT NULL')
    expect(schemaText).toContain('`profile_snapshot_json` longtext NOT NULL')
    expect(schemaText.match(/`agent_workspace_runtime_id` varchar\(128\) NOT NULL/g)).toHaveLength(2)
    expect(schemaText.match(/`agent_workspace_snapshot_hash` varchar\(96\) NOT NULL/g)).toHaveLength(2)
  })

  it('stores runtime context tags instead of scene matching columns', () => {
    expect(schemaText).toContain('`context_tags_json` longtext NOT NULL')
    expect(schemaText).not.toContain('`supported_scene_types_json`')
    expect(schemaText).not.toContain('`scene_type`')
    expect(schemaText).not.toContain('`scene_code`')
  })

  it('keeps profile names unique inside a workspace', () => {
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_agent_profiles_workspace_name` (`workspace_id`,`name`)')
  })

  it('keeps Agent resource revision identity separate from source version labels', () => {
    expect(schemaText).toContain('CREATE TABLE IF NOT EXISTS `ai_agent_resource_versions`')
    expect(schemaText).toContain('`revision` bigint unsigned NOT NULL')
    expect(schemaText).toContain('`source_version` varchar(128) NOT NULL')
    expect(schemaText).toContain('`snapshot_digest` varchar(96) NOT NULL')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_agent_resource_versions_revision` (`workspace_id`,`resource_id`,`revision`)')
    expect(schemaText).toContain("`resource_id_kind` enum('database_id','resource_key') NOT NULL")
  })

  it('does not upsert runtime runs through unrelated active-slot unique keys', () => {
    const saveRunBody = mariadbStoreText.match(/async saveRun\(run: AIRuntimeRun\) \{[\s\S]*?\n {2}\}/)?.[0] || ''

    expect(saveRunBody).toContain('INSERT INTO ai_runtime_runs')
    expect(saveRunBody).toContain('UPDATE ai_runtime_runs')
    expect(saveRunBody).not.toContain('ON DUPLICATE KEY UPDATE')
  })

  it('does not persist user-facing model adapter fields on agent profiles', () => {
    expect(schemaText).not.toContain('model_adapter')
    expect(schemaText).not.toContain('model_adapter_json')
  })

  it('does not reintroduce page-specific or runtime-profile compatibility tables', () => {
    for (const forbidden of [
      'page_ai_assistant_sessions',
      'page_ai_assistant_messages',
      'page_ai_tool_confirmations',
      'ai_tool_confirmations',
      'workspace_ai_assistant_settings',
      'ai_runtime_profiles',
      'runtime_profile_id',
      'ai_agent_scene_bindings',
      'scene_binding'
    ]) {
      expect(schemaText).not.toContain(forbidden)
    }
  })

  it('defines durable multi-agent orchestration plan/task/context pack tables', () => {
    expect(schemaText).toContain('CREATE TABLE IF NOT EXISTS `ai_orchestration_plans`')
    expect(schemaText).toContain('CREATE TABLE IF NOT EXISTS `ai_orchestration_context_packs`')
    expect(schemaText).toContain('CREATE TABLE IF NOT EXISTS `ai_orchestration_tasks`')
    expect(schemaText).toContain('UNIQUE KEY `uk_ai_orchestration_parent_run` (`workspace_id`,`parent_runtime_run_id`)')
    expect(schemaText).toContain('`claim_epoch` bigint unsigned NOT NULL DEFAULT 0')
    expect(schemaText).toContain('`context_pack_digest` varchar(96) NOT NULL')
    expect(schemaText).toContain('`dependency_ids_json` longtext NOT NULL')
    expect(orchestrationMigrationText).toContain('CREATE TABLE IF NOT EXISTS `ai_orchestration_tasks`')
    expect(orchestrationMigrationText).toContain('UNIQUE KEY `uk_ai_orchestration_task_seq`')
  })

})
