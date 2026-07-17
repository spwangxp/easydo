import { z } from 'zod'

const jsonValueSchema: z.ZodType<unknown> = z.lazy(() =>
  z.union([
    z.string(),
    z.number(),
    z.boolean(),
    z.null(),
    z.array(jsonValueSchema),
    z.record(jsonValueSchema)
  ])
)

export const contextTagsSchema = z.array(z.string().min(1)).default([])

export const runtimeActorSchema = z.object({
  user_id: z.number().int().positive(),
  username: z.string().min(1),
  system_role: z.string().min(1),
  workspace_id: z.number().int().positive(),
  workspace_role: z.string().min(1),
  auth_session_id: z.string().min(1)
})

export const runtimeAuthSchema = z.object({
  delegated_user_token: z.string().optional(),
  user_token: z.string().optional(),
  server_internal_token: z.string().min(1)
})

export const runtimeEnvelopeSchema = z.object({
  request_id: z.string().min(1),
  idempotency_key: z.string().min(1),
  actor: runtimeActorSchema,
  auth: runtimeAuthSchema,
  payload: z.record(jsonValueSchema).default({})
})

export const contextRefSchema = z.object({
  kind: z.string().min(1),
  route_path: z.string().optional(),
  route_name: z.string().optional(),
  object_type: z.string().optional(),
  object_id: z.union([z.string(), z.number()]).optional(),
  selected_tab: z.string().optional()
})

export const runtimeEventSchema = z.object({
  run_id: z.string().min(1),
  session_id: z.number().int().positive(),
  entry_id: z.number().int().positive().optional(),
  type: z.string().min(1),
  seq: z.number().int().nonnegative(),
  payload: z.record(jsonValueSchema).default({}),
  timestamp: z.number().int().positive()
})

export const agentResourceRefSchema = z.object({
  resource_type: z.string().min(1),
  resource_id: z.union([z.string(), z.number()]),
  resource_version_id: z.number().int().positive().optional(),
  resource_version: z.string().optional(),
  snapshot_digest: z.string().regex(/^sha256:[a-f0-9]{64}$/).optional(),
  config: z.record(jsonValueSchema).default({}),
  required: z.boolean().default(true)
})

export const agentResourceSnapshotSchema = z.object({
  id: z.number().int().positive(),
  workspace_id: z.number().int().positive(),
  resource_kind: z.enum(['skill', 'mcp_server']),
  resource_key: z.string().min(1),
  resource_id: z.string().min(1),
  name: z.string().min(1),
  description: z.string(),
  version: z.string().min(1),
  status: z.enum(['draft', 'active', 'disabled', 'archived']),
  spec: z.record(jsonValueSchema),
  endpoint: z.record(jsonValueSchema),
  secret_ref: z.record(jsonValueSchema),
  tags: z.array(z.string()),
  created_by: z.number().int().positive()
})

export const agentResourceVersionSchema = z.object({
  resource_version_id: z.number().int().positive(),
  resource_id: z.number().int().positive(),
  workspace_id: z.number().int().positive(),
  revision: z.number().int().positive(),
  source_version: z.string().min(1),
  snapshot_digest: z.string().regex(/^sha256:[a-f0-9]{64}$/),
  snapshot: agentResourceSnapshotSchema,
  created_by: z.number().int().positive(),
  created_at: z.string().min(1)
})

export const agentProfileDraftSchema = z.object({
  workspace_id: z.number().int().positive(),
  name: z.string().min(1),
  description: z.string().default(''),
  profile_kind: z.string().min(1).default('generic'),
  context_tags: contextTagsSchema,
  provider: z.record(jsonValueSchema).default({}),
  model: z.record(jsonValueSchema).default({}),
  binding: z.record(jsonValueSchema).default({}),
  model_settings: z.record(jsonValueSchema).default({}),
  fallback_policy: z.record(jsonValueSchema).default({}),
  system_prompt: z.string().default(''),
  user_prompt_template: z.string().default(''),
  skills: z.array(agentResourceRefSchema).default([]),
  subagents: z.array(agentResourceRefSchema).default([]),
  input_schema: z.record(jsonValueSchema).default({}),
  output_schema: z.record(jsonValueSchema).default({}),
  context_contract: z.record(jsonValueSchema).default({}),
  tool_policy: z.record(jsonValueSchema).default({}),
  memory_policy: z.record(jsonValueSchema).default({}),
  confirmation_policy: z.record(jsonValueSchema).default({}),
  mcp_servers: z.array(agentResourceRefSchema).default([]),
  response_mode: z.enum(['text', 'json', 'schema', 'mixed']).default('text'),
  status: z.enum(['draft', 'active', 'disabled', 'archived']).default('draft')
})

export const agentProfileVersionSchema = agentProfileDraftSchema.extend({
  profile_id: z.number().int().positive(),
  profile_version_id: z.number().int().positive(),
  snapshot_hash: z.string().min(1),
  version: z.number().int().positive(),
  status: z.enum(['published', 'deprecated', 'archived']).default('published')
})

export const aiSessionSchema = z.object({
  id: z.number().int().positive(),
  workspace_id: z.number().int().positive(),
  context_tags: contextTagsSchema,
  session_kind: z.enum(['chat', 'task_run', 'workflow', 'test_run']),
  user_id: z.number().int().positive().optional(),
  auth_session_id: z.string().optional(),
  business_type: z.string().optional(),
  business_id: z.union([z.string(), z.number()]).optional(),
  status: z.enum(['active', 'archived', 'deleted']),
  agent_profile_id: z.number().int().positive().optional(),
  agent_profile_version_id: z.number().int().positive().optional(),
  agent_profile_snapshot_hash: z.string().optional(),
  agent_workspace_runtime_id: z.string().min(1),
  agent_workspace_snapshot_hash: z.string().min(1)
})

export const aiSessionEntrySchema = z.object({
  id: z.number().int().positive(),
  session_id: z.number().int().positive(),
  workspace_id: z.number().int().positive(),
  seq: z.number().int().nonnegative(),
  entry_type: z.string().min(1),
  role: z.string().min(1),
  status: z.enum(['queued', 'running', 'streaming', 'completed', 'failed', 'cancelled']),
  content: z.string().default(''),
  content_blocks: z.array(z.record(jsonValueSchema)).default([]),
  runtime_run_id: z.string().optional(),
  event_seq: z.number().int().nonnegative().optional()
})

export const aiRuntimeRunSchema = z.object({
  id: z.number().int().positive(),
  session_id: z.number().int().positive(),
  workspace_id: z.number().int().positive(),
  context_tags: contextTagsSchema,
  runtime_run_id: z.string().min(1),
  status: z.enum(['queued', 'running', 'awaiting_decision', 'awaiting_input', 'completed', 'failed', 'cancelled', 'timeout', 'interrupted']),
  agent_profile_id: z.number().int().positive().optional(),
  agent_profile_version_id: z.number().int().positive().optional(),
  agent_profile_snapshot_hash: z.string().optional(),
  agent_workspace_runtime_id: z.string().min(1),
  agent_workspace_snapshot_hash: z.string().min(1)
})

export const aiAgentActionSchema = z.object({
  id: z.number().int().positive(),
  action_id: z.string().min(1),
  internal_id: z.number().int().positive().optional(),
  workspace_id: z.number().int().positive(),
  context_tags: contextTagsSchema,
  session_id: z.number().int().positive(),
  entry_id: z.number().int().positive().optional(),
  runtime_run_id: z.string().min(1),
  action_kind: z.string().min(1),
  idempotency_key: z.string().min(1),
  source: z.enum(['model', 'policy', 'runtime', 'user']),
  capability_id: z.string().min(1),
  input_json: z.record(jsonValueSchema).default({}),
  input_digest: z.string().regex(/^sha256:[0-9a-f]{64}$/),
  target_json: z.record(jsonValueSchema).default({}),
  policy_json: z.record(jsonValueSchema).default({}),
  display_json: z.record(jsonValueSchema).default({}),
  status: z.enum(['awaiting_decision', 'approved', 'rejected', 'executing', 'executed', 'failed', 'expired', 'cancelled']),
  requested_by: z.number().int().positive(),
  decided_by: z.number().int().positive().optional(),
  decided_at: z.string().optional(),
  executed_at: z.string().optional(),
  result_json: z.record(jsonValueSchema).default({}),
  error_msg: z.string().optional(),
  created_at: z.string().min(1),
  updated_at: z.string().min(1)
}).strict()

export const approvalRequestSchema = z.object({
  approval_id: z.string().min(1),
  run_id: z.string().min(1),
  action_id: z.string().min(1),
  tool_name: z.string().min(1),
  permission_key: z.string().min(1),
  risk_level: z.enum(['read', 'write', 'network', 'destructive', 'admin']),
  reason: z.string().default(''),
  input_preview: z.record(jsonValueSchema).default({}),
  affected_resources: z.array(z.record(jsonValueSchema)).default([]),
  options: z.array(z.enum(['approve_once', 'approve_session', 'reject'])).min(1),
  steer_supported: z.boolean().default(true),
  created_at: z.string().min(1),
  expires_at: z.string().min(1)
}).strict()

export type RuntimeActor = z.infer<typeof runtimeActorSchema>
export type RuntimeEnvelope = z.infer<typeof runtimeEnvelopeSchema>
export type RuntimeEvent = z.infer<typeof runtimeEventSchema>
export type AgentProfileDraft = z.infer<typeof agentProfileDraftSchema>
export type AgentProfileVersion = z.infer<typeof agentProfileVersionSchema>
export type AgentResourceVersion = z.infer<typeof agentResourceVersionSchema>
export type ApprovalRequest = z.infer<typeof approvalRequestSchema>
