import { createHash } from 'node:crypto'
import { agentProfileSnapshotHash, createAgentResourceVersion } from '../domain/runtime.js'
import type {
  AgentProfileDraft,
  AgentProfileSnapshot,
  AgentProfileVersion,
  AgentResource,
  AgentResourceKind,
  AgentResourceRef,
  AgentResourceVersion,
  ProfileValidationIssue,
  ProfileValidationResult,
  ResourceHealthResult,
  RuntimeActor
} from '../domain/runtime.js'
import type { RuntimeStore } from '../store/memoryRuntimeStore.js'
import {
  assertKnownContextTags,
  assertProfileContextTags,
  normalizeContextTags,
  PAGE_ASSISTANT_CONTEXT_TAG,
  PAGE_ASSISTANT_PROFILE_NAME
} from './contextTags.js'
export { RuntimeDomainError } from './runtimeErrors.js'
import { RuntimeDomainError } from './runtimeErrors.js'
import { createMcpClient, resolveMcpClientConfigSecrets } from './mcpStreamableHttpClient.js'
import { scanSkillRepository as scanSkillRepositoryFiles } from './skillScanner.js'

function now() {
  return new Date().toISOString()
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? { ...(value as Record<string, unknown>) } : {}
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const item = String(value || '').trim()
    if (item) return item
  }
  return ''
}

function hasRecordFields(value: unknown) {
  return Object.keys(asRecord(value)).length > 0
}

function asResourceKind(value: unknown): AgentResourceKind {
  switch (value) {
    case 'skill':
    case 'mcp_server':
      return value
    default:
      throw new RuntimeDomainError('agent_resource_kind_invalid', 'Agent resource kind is invalid')
  }
}

function asTags(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item).trim()).filter(Boolean)
}

function asResourceRefs(value: unknown): AgentResourceRef[] {
  if (!Array.isArray(value)) return []
  return value
    .filter((item) => item && typeof item === 'object')
    .map((item) => {
      const raw = item as Record<string, unknown>
      return {
        resource_type: String(raw.resource_type || ''),
        resource_id: raw.resource_id as string | number,
        resource_version_id: Number.isInteger(Number(raw.resource_version_id)) && Number(raw.resource_version_id) > 0
          ? Number(raw.resource_version_id)
          : undefined,
        resource_version: typeof raw.resource_version === 'string' ? raw.resource_version : undefined,
        snapshot_digest: typeof raw.snapshot_digest === 'string' ? raw.snapshot_digest : undefined,
        config: asRecord(raw.config),
        required: typeof raw.required === 'boolean' ? raw.required : true
      }
    })
    .filter((item) => item.resource_type !== '' && item.resource_id !== undefined && item.resource_id !== null)
}

function responseMode(value: unknown): AgentProfileDraft['response_mode'] {
  switch (value) {
    case 'json':
    case 'schema':
    case 'mixed':
      return value
    default:
      return 'text'
  }
}

function profileStatus(value: unknown): AgentProfileDraft['status'] {
  switch (value) {
    case 'active':
    case 'disabled':
    case 'archived':
      return value
    default:
      return 'draft'
  }
}

function resourceStatus(value: unknown): AgentResource['status'] {
  switch (value) {
    case 'active':
    case 'disabled':
    case 'archived':
      return value
    default:
      return 'draft'
  }
}

function isMaskedSecretRef(value: unknown) {
  const record = asRecord(value)
  return record.masked === true || record.configured === true
}

function assertCredentialReferenceOnly(profile: AgentProfileDraft) {
  const credentialRef = asRecord(profile.provider_credential_ref)
  const secretRef = asRecord(credentialRef.secret_ref)
  const resolvedValue = [
    credentialRef.api_key,
    credentialRef.token,
    credentialRef.bearer_token,
    credentialRef.password,
    credentialRef.client_secret,
    secretRef.api_key,
    secretRef.token,
    secretRef.bearer_token,
    secretRef.password,
    secretRef.client_secret
  ].find((value) => typeof value === 'string' && value.trim())
  if (resolvedValue) {
    throw new RuntimeDomainError(
      'provider_credential_reference_required',
      'Provider credentials must use a credential reference',
      400
    )
  }
}

function sanitizeSecretRef(secretRef: Record<string, unknown>) {
  if (Object.keys(secretRef).length === 0) return {}
  return {
    configured: true,
    masked: true
  }
}

function sanitizeResource(resource: AgentResource): AgentResource {
  return {
    ...resource,
    secret_ref: sanitizeSecretRef(resource.secret_ref)
  }
}

function normalizedResourceID(value: unknown) {
  return String(value ?? '').trim().toLowerCase()
}

function builtinEasyDoSnapshotDigest() {
  return `sha256:${createHash('sha256').update('easydo-mcp:builtin-v1').digest('hex')}`
}

function refMatchesResource(ref: AgentResourceRef, resource: AgentResource) {
  if (typeof ref.resource_id === 'number') return resource.id === ref.resource_id
  const expected = normalizedResourceID(ref.resource_id)
  return [resource.resource_id, resource.resource_key]
    .some((value) => normalizedResourceID(value) === expected)
}

function resourceToolPermissions(resource: { spec?: Record<string, unknown> }) {
  return asRecord(asRecord(resource.spec).tool_permissions)
}

function refWithDefaultToolPermissions(ref: AgentResourceRef, resource: { spec?: Record<string, unknown> }): AgentResourceRef {
  if (hasRecordFields(asRecord(ref.config).tool_permissions)) return ref
  const toolPermissions = resourceToolPermissions(resource)
  if (Object.keys(toolPermissions).length === 0) return ref
  return {
    ...ref,
    config: {
      ...asRecord(ref.config),
      tool_permissions: toolPermissions
    }
  }
}

function buildSnapshot(profile: AgentProfileDraft): AgentProfileSnapshot {
  const { created_at: _createdAt, updated_at: _updatedAt, ...snapshot } = profile
  return snapshot
}

function defaultPageAssistantProfile(id: number, actor: RuntimeActor): AgentProfileDraft {
  const timestamp = now()
  return {
    id,
    workspace_id: actor.workspace_id,
    name: PAGE_ASSISTANT_PROFILE_NAME,
    description: 'Fixed page assistant profile. Configure provider, model, prompt, skills, MCP servers, and publish before users call it.',
    profile_kind: 'generic',
    context_tags: [PAGE_ASSISTANT_CONTEXT_TAG],
    provider: {},
    binding: {},
    model: {},
    provider_credential_ref: {},
    inference: {},
    prompt: {
      system: 'Answer using the current EasyDo page context.'
    },
    skills: [],
    subagents: [],
    mcp_servers: [],
    context_contract: {
      requires: [PAGE_ASSISTANT_CONTEXT_TAG],
      context_ref_kind: 'current-page'
    },
    input_schema: {},
    output_schema: {},
    memory_policy: {},
    tool_policy: {},
    confirmation_policy: {},
    response_mode: 'mixed',
    status: 'draft',
    created_by: actor.user_id,
    created_at: timestamp,
    updated_at: timestamp
  }
}

function profileFromPayload(id: number, actor: RuntimeActor, payload: Record<string, unknown>): AgentProfileDraft {
  const timestamp = now()
  return {
    id,
    workspace_id: actor.workspace_id,
    name: String(payload.name || '').trim(),
    description: String(payload.description || ''),
    profile_kind: String(payload.profile_kind || 'generic').trim(),
    context_tags: normalizeContextTags(payload.context_tags),
    provider: asRecord(payload.provider),
    binding: asRecord(payload.binding),
    model: asRecord(payload.model),
    provider_credential_ref: asRecord(payload.provider_credential_ref),
    inference: asRecord(payload.inference),
    prompt: asRecord(payload.prompt),
    skills: asResourceRefs(payload.skills),
    subagents: asResourceRefs(payload.subagents),
    mcp_servers: asResourceRefs(payload.mcp_servers),
    context_contract: asRecord(payload.context_contract),
    input_schema: asRecord(payload.input_schema),
    output_schema: asRecord(payload.output_schema),
    tool_policy: asRecord(payload.tool_policy),
    memory_policy: asRecord(payload.memory_policy),
    confirmation_policy: asRecord(payload.confirmation_policy),
    response_mode: responseMode(payload.response_mode),
    status: profileStatus(payload.status),
    created_by: actor.user_id,
    created_at: timestamp,
    updated_at: timestamp
  }
}

function resourceFromPayload(id: number, actor: RuntimeActor, payload: Record<string, unknown>): AgentResource {
  const timestamp = now()
  const resourceKey = String(payload.resource_key || payload.resource_id || '').trim()
  return {
    id,
    workspace_id: actor.workspace_id,
    resource_kind: asResourceKind(payload.resource_kind),
    resource_key: resourceKey,
    resource_id: resourceKey,
    name: String(payload.name || resourceKey).trim(),
    description: String(payload.description || ''),
    version: String(payload.version || 'latest').trim(),
    status: resourceStatus(payload.status),
    spec: asRecord(payload.spec),
    endpoint: asRecord(payload.endpoint),
    secret_ref: asRecord(payload.secret_ref),
    tags: asTags(payload.tags),
    created_by: actor.user_id,
    created_at: timestamp,
    updated_at: timestamp
  }
}

function mergeProfile(profile: AgentProfileDraft, payload: Record<string, unknown>): AgentProfileDraft {
  return {
    ...profile,
    name: payload.name === undefined ? profile.name : String(payload.name || '').trim(),
    description: payload.description === undefined ? profile.description : String(payload.description || ''),
    profile_kind: payload.profile_kind === undefined ? profile.profile_kind : String(payload.profile_kind || 'generic').trim(),
    context_tags: payload.context_tags === undefined ? profile.context_tags : normalizeContextTags(payload.context_tags),
    provider: payload.provider === undefined ? profile.provider : asRecord(payload.provider),
    binding: payload.binding === undefined ? profile.binding : asRecord(payload.binding),
    model: payload.model === undefined ? profile.model : asRecord(payload.model),
    provider_credential_ref: payload.provider_credential_ref === undefined ? profile.provider_credential_ref : asRecord(payload.provider_credential_ref),
    inference: payload.inference === undefined ? profile.inference : asRecord(payload.inference),
    prompt: payload.prompt === undefined ? profile.prompt : asRecord(payload.prompt),
    skills: payload.skills === undefined ? profile.skills : asResourceRefs(payload.skills),
    subagents: payload.subagents === undefined ? profile.subagents : asResourceRefs(payload.subagents),
    mcp_servers: payload.mcp_servers === undefined ? profile.mcp_servers : asResourceRefs(payload.mcp_servers),
    context_contract: payload.context_contract === undefined ? profile.context_contract : asRecord(payload.context_contract),
    input_schema: payload.input_schema === undefined ? profile.input_schema : asRecord(payload.input_schema),
    output_schema: payload.output_schema === undefined ? profile.output_schema : asRecord(payload.output_schema),
    tool_policy: payload.tool_policy === undefined ? profile.tool_policy : asRecord(payload.tool_policy),
    memory_policy: payload.memory_policy === undefined ? profile.memory_policy : asRecord(payload.memory_policy),
    confirmation_policy: payload.confirmation_policy === undefined ? profile.confirmation_policy : asRecord(payload.confirmation_policy),
    response_mode: payload.response_mode === undefined ? profile.response_mode : responseMode(payload.response_mode),
    status: payload.status === undefined ? profile.status : profileStatus(payload.status),
    updated_at: now()
  }
}

function mergeResource(resource: AgentResource, payload: Record<string, unknown>): AgentResource {
  const resourceKey = payload.resource_key === undefined && payload.resource_id === undefined
    ? resource.resource_key
    : String(payload.resource_key || payload.resource_id || '').trim()
  const nextSecretRef = payload.secret_ref === undefined || isMaskedSecretRef(payload.secret_ref)
    ? resource.secret_ref
    : asRecord(payload.secret_ref)
  return {
    ...resource,
    resource_kind: payload.resource_kind === undefined ? resource.resource_kind : asResourceKind(payload.resource_kind),
    resource_key: resourceKey,
    resource_id: resourceKey,
    name: payload.name === undefined ? resource.name : String(payload.name || resourceKey).trim(),
    description: payload.description === undefined ? resource.description : String(payload.description || ''),
    version: payload.version === undefined ? resource.version : String(payload.version || 'latest').trim(),
    status: payload.status === undefined ? resource.status : resourceStatus(payload.status),
    spec: payload.spec === undefined ? resource.spec : asRecord(payload.spec),
    endpoint: payload.endpoint === undefined ? resource.endpoint : asRecord(payload.endpoint),
    secret_ref: nextSecretRef,
    tags: payload.tags === undefined ? resource.tags : asTags(payload.tags),
    updated_at: now()
  }
}

function scanSkillSucceededSpec(resource: AgentResource, discoveredSkills: Record<string, unknown>[]) {
  return {
    ...resource.spec,
    discovered_skills: discoveredSkills,
    last_scan: {
      status: 'success',
      scanned_at: now(),
      discovered_count: discoveredSkills.length
    }
  }
}

function scanMcpSucceededSpec(resource: AgentResource, discoveredTools: Record<string, unknown>[]) {
  return {
    ...resource.spec,
    discovered_tools: discoveredTools,
    last_scan: {
      status: 'success',
      capability: 'mcp',
      scanned_at: now(),
      discovered_count: discoveredTools.length
    }
  }
}

function mcpServerConfigs(resource: AgentResource) {
  const specServers = asRecord(asRecord(resource.spec).mcpServers)
  const configs = Object.entries(specServers)
    .map(([name, config]) => ({ name, config: asRecord(config) }))
    .filter(({ config }) => config.disabled !== true && String(config.disabled || '').toLowerCase() !== 'true')
  if (configs.length > 0) return configs
  const endpoint = asRecord(resource.endpoint)
  if (firstString(endpoint.url, endpoint.command)) {
    return [{ name: resource.resource_key, config: endpoint }]
  }
  return []
}

function normalizeMcpDiscoveredTools(values: unknown[], serverKey = '') {
  const tools: Record<string, unknown>[] = []
  const seen = new Set<string>()
  for (const value of values) {
    const record = asRecord(value)
    const name = firstString(record.name, record.tool_name, typeof value === 'string' ? value : '')
    const mcpServerKey = firstString(record.mcp_server_key, record.mcp_server, record.server_key, serverKey)
    const dedupeKey = `${mcpServerKey}:${name}`
    if (!name || seen.has(dedupeKey)) continue
    seen.add(dedupeKey)
    const inputSchema = asRecord(record.input_schema ?? record.inputSchema ?? record.parameters ?? record.schema)
    const tool: Record<string, unknown> = {
      name,
      description: firstString(record.description)
    }
    if (Object.keys(inputSchema).length > 0) tool.input_schema = inputSchema
    const operationType = firstString(record.operation_type, record.operationType, record.operation)
    const targetType = firstString(record.target_type, record.targetType)
    if (operationType) tool.operation_type = operationType
    if (targetType) tool.target_type = targetType
    if (record.requires_confirmation === true || record.requiresConfirmation === true) tool.requires_confirmation = true
    if (mcpServerKey) tool.mcp_server_key = mcpServerKey
    tool.capability = firstString(record.capability, 'tools')
    tools.push(tool)
  }
  return tools
}

async function scanMcpTools(resource: AgentResource) {
  const configs = mcpServerConfigs(resource)
  if (configs.length === 0) {
    throw new RuntimeDomainError('mcp_server_endpoint_required', 'MCP server endpoint is required for scanning', 400)
  }
  const tools: Record<string, unknown>[] = []
  const errors: string[] = []
  for (const entry of configs) {
    const config = resolveMcpClientConfigSecrets(entry.config, resource.secret_ref)
    const serverKey = firstString(entry.name, resource.resource_key)
    const client = createMcpClient({
      ...config,
      type: firstString(config.type, resource.endpoint.type, config.command ? 'stdio' : 'streamable_http'),
      timeout_ms: Number(config.timeout_ms || config.timeoutMs || resource.endpoint.timeout_ms || resource.endpoint.timeout || 30_000)
    })
    try {
      const result = await client.request(
        'tools/list',
        {},
        `mcp_tools_${resource.workspace_id}_${resource.id}_${serverKey}`,
        { mcp_server_id: serverKey }
      )
      tools.push(...normalizeMcpDiscoveredTools(Array.isArray(result.tools) ? result.tools : [], serverKey))
    } catch (error) {
      errors.push(`${serverKey}: ${error instanceof Error ? error.message : String(error)}`)
    } finally {
      await client.close()
    }
  }
  if (tools.length === 0 && errors.length > 0) {
    throw new RuntimeDomainError('mcp_server_scan_failed', errors.join('; '), 400)
  }
  return tools
}

export async function probeMcpServerConfig(configInput: Record<string, unknown>) {
  const config = asRecord(configInput)
  const type = firstString(config.type, config.transport, config.command ? 'stdio' : 'streamable_http')
  if (!type) {
    throw new RuntimeDomainError('mcp_server_endpoint_required', 'MCP server type is required', 400)
  }
  if (['streamable_http', 'sse'].includes(type) && !firstString(config.url, config.sse_url, config.sseUrl)) {
    throw new RuntimeDomainError('mcp_server_endpoint_required', `${type} MCP server URL is required`, 400)
  }
  if ((type === 'stdio' || type === 'command') && !firstString(config.command)) {
    throw new RuntimeDomainError('mcp_server_endpoint_required', 'stdio MCP command is required', 400)
  }
  const serverKey = firstString(config.server_key, config.name, config.mcp_server_key, 'probe')
  const resolvedConfig = resolveMcpClientConfigSecrets(config, asRecord(config.secret_ref))
  const client = createMcpClient({
    ...resolvedConfig,
    type,
    timeout_ms: Number(resolvedConfig.timeout_ms || resolvedConfig.timeoutMs || 30_000)
  })
  try {
    const result = await client.request('tools/list', {}, `mcp_probe_${serverKey}_${Date.now()}`, {
      mcp_server_id: serverKey
    })
    const discoveredTools = normalizeMcpDiscoveredTools(Array.isArray(result.tools) ? result.tools : [], serverKey)
    return {
      ok: true,
      transport: type,
      server_key: serverKey,
      discovered_tools: discoveredTools,
      tool_count: discoveredTools.length
    }
  } catch (error) {
    if (error instanceof RuntimeDomainError) throw error
    throw new RuntimeDomainError(
      'mcp_server_probe_failed',
      error instanceof Error ? error.message : 'MCP server probe failed',
      400
    )
  } finally {
    await client.close()
  }
}

function assertValidResource(resource: AgentResource) {
  if (!resource.resource_key) {
    throw new RuntimeDomainError('agent_resource_key_required', 'Agent resource key is required')
  }
  if (!resource.name) {
    throw new RuntimeDomainError('agent_resource_name_required', 'Agent resource name is required')
  }
}

function isResourceRevisionConflict(error: unknown) {
  if (error instanceof RuntimeDomainError && error.code === 'agent_resource_revision_conflict') return true
  const message = error instanceof Error ? error.message : String(error || '')
  return /duplicate|already exists/i.test(message)
}

async function assertUniqueResourceKey(store: RuntimeStore, resource: AgentResource) {
  const resources = await store.listAgentResources(resource.workspace_id)
  const existing = resources.find(
    (item) =>
      item.id !== resource.id &&
      item.resource_kind === resource.resource_kind &&
      item.resource_key === resource.resource_key
  )
  if (existing) {
    throw new RuntimeDomainError('agent_resource_key_exists', 'Agent resource key already exists', 409)
  }
}

function groupResources(resources: AgentResource[], profiles: AgentProfileDraft[]) {
  const grouped = {
    mcp_servers: [] as AgentResource[],
    skills: [] as AgentResource[],
    subagent_profiles: profiles.map((profile) => ({
      id: profile.id,
      resource_id: profile.id,
      resource_key: String(profile.id),
      name: profile.name,
      description: profile.description,
      profile_kind: profile.profile_kind,
      context_tags: profile.context_tags,
      status: profile.status,
      updated_at: profile.updated_at
    }))
  }
  for (const resource of resources) {
    const safeResource = sanitizeResource(resource)
    if (resource.resource_kind === 'skill') grouped.skills.push(safeResource)
    if (resource.resource_kind === 'mcp_server') grouped.mcp_servers.push(safeResource)
  }
  return grouped
}

export class AgentProfileService {
  private readonly pageAssistantEnsureJobs = new Map<number, Promise<void>>()

  constructor(private readonly store: RuntimeStore) {}

  private async resolveResourceVersion(
    actor: RuntimeActor,
    ref: AgentResourceRef,
    resource: AgentResource,
    kind: AgentResourceKind
  ) {
    const requestedVersionID = Number(ref.resource_version_id || 0)
    const requestedSourceVersion = String(ref.resource_version || '').trim()
    const versions = await this.store.listAgentResourceVersions(actor.workspace_id, resource.id)
    let version: AgentResourceVersion | undefined
    if (requestedVersionID > 0) {
      version = await this.store.getAgentResourceVersion(actor.workspace_id, requestedVersionID)
      if (version && requestedSourceVersion) {
        const sourceMatches = versions.filter((candidate) => candidate.source_version === requestedSourceVersion)
        if (sourceMatches.length > 1) {
          throw new RuntimeDomainError(
            'agent_profile_resource_version_ambiguous',
            `${kind} resource ${ref.resource_id} source version is ambiguous`,
            409
          )
        }
        if (sourceMatches[0]?.resource_version_id !== version.resource_version_id) {
          throw new RuntimeDomainError(
            'agent_profile_resource_version_selector_conflict',
            `${kind} resource ${ref.resource_id} version selectors conflict`,
            409
          )
        }
      }
    } else if (requestedSourceVersion) {
      const matches = versions.filter((candidate) => candidate.source_version === requestedSourceVersion)
      if (matches.length > 1) {
        throw new RuntimeDomainError(
          'agent_profile_resource_version_ambiguous',
          `${kind} resource ${ref.resource_id} source version is ambiguous`,
          409
        )
      }
      version = matches[0]
    } else {
      version = versions[0]
    }
    if (!version || version.resource_id !== resource.id || version.snapshot.resource_kind !== kind) {
      throw new RuntimeDomainError(
        'agent_profile_resource_version_not_found',
        `${kind} resource ${ref.resource_id} version was not found`,
        404
      )
    }
    if (ref.snapshot_digest && ref.snapshot_digest !== version.snapshot_digest) {
      throw new RuntimeDomainError(
        'agent_profile_resource_digest_mismatch',
        `${kind} resource ${ref.resource_id} digest does not match its version`,
        409
      )
    }
    return version
  }

  private async resolvePublishedSubagentVersion(actor: RuntimeActor, ref: AgentResourceRef, profileID: number) {
    const versions = (await this.store.listProfileVersions(actor.workspace_id, profileID))
      .filter((version) => version.status === 'published')
      .sort((left, right) => right.version - left.version || right.profile_version_id - left.profile_version_id)
    const requestedVersionID = Number(ref.resource_version_id || 0)
    const requestedLogicalVersion = String(ref.resource_version || '').trim()
    const version = requestedVersionID > 0
      ? versions.find((candidate) => candidate.profile_version_id === requestedVersionID)
      : requestedLogicalVersion
        ? versions.find((candidate) => String(candidate.version) === requestedLogicalVersion)
        : versions[0]
    if (version && requestedVersionID > 0 && requestedLogicalVersion && String(version.version) !== requestedLogicalVersion) {
      throw new RuntimeDomainError(
        'agent_profile_subagent_version_selector_conflict',
        `Published Subagent ${ref.resource_id} version selectors conflict`,
        409
      )
    }
    if (!version) {
      throw new RuntimeDomainError(
        'agent_profile_subagent_version_not_found',
        `Published Subagent ${ref.resource_id} version was not found`,
        404
      )
    }
    if (ref.snapshot_digest && ref.snapshot_digest !== version.snapshot_hash) {
      throw new RuntimeDomainError(
        'agent_profile_subagent_digest_mismatch',
        `Published Subagent ${ref.resource_id} digest does not match its version`,
        409
      )
    }
    return version
  }

  async listProfiles(actor: RuntimeActor) {
    await this.ensurePageAssistantProfile(actor)
    return this.store.listProfiles(actor.workspace_id)
  }

  async getProfile(actor: RuntimeActor, profileID: number) {
    const profile = await this.store.getProfile(actor.workspace_id, profileID)
    if (!profile) {
      throw new RuntimeDomainError('agent_profile_not_found', 'Agent profile not found', 404)
    }
    return profile
  }

  async createProfile(actor: RuntimeActor, payload: Record<string, unknown>) {
    const profile = await this.pinDraftResourceRefs(actor, profileFromPayload(await this.store.nextProfileId(), actor, payload))
    if (!profile.name) {
      throw new RuntimeDomainError('agent_profile_name_required', 'Agent profile name is required')
    }
    assertCredentialReferenceOnly(profile)
    assertProfileContextTags(profile.name, profile.context_tags)
    await this.assertProfileNameUnique(actor.workspace_id, profile)
    return this.store.saveProfile(profile)
  }

  async updateProfile(actor: RuntimeActor, profileID: number, payload: Record<string, unknown>) {
    const profile = await this.getProfile(actor, profileID)
    const updated = await this.pinDraftResourceRefs(actor, mergeProfile(profile, payload))
    if (profile.name === PAGE_ASSISTANT_PROFILE_NAME && updated.name !== PAGE_ASSISTANT_PROFILE_NAME) {
      throw new RuntimeDomainError('reserved_profile_name_immutable', 'page-ai-assistant profile name cannot be changed')
    }
    assertProfileContextTags(updated.name, updated.context_tags)
    assertCredentialReferenceOnly(updated)
    await this.assertProfileNameUnique(actor.workspace_id, updated)
    return this.store.saveProfile(updated)
  }

  async deleteProfile(actor: RuntimeActor, profileID: number) {
    const profile = await this.getProfile(actor, profileID)
    if (profile.name === PAGE_ASSISTANT_PROFILE_NAME) {
      throw new RuntimeDomainError('reserved_profile_delete_forbidden', 'page-ai-assistant profile cannot be deleted')
    }
    const dependencies = await this.getProfileDependencies(actor, profileID)
    const blockers = dependencies.dependencies.filter((dependency) =>
      ['blocking_live_ref', 'published_version', 'session_reference', 'active_execution'].includes(String(dependency.dependency_class))
    )
    if (blockers.length > 0) {
      throw new RuntimeDomainError('agent_profile_in_use', 'Agent profile is still referenced', 409, {
        dependencies: dependencies.dependencies,
        operations: dependencies.operations
      })
    }
    const deleted = await this.store.deleteProfileGuarded(actor.workspace_id, profileID)
    if (deleted === 'in_use') {
      const refreshed = await this.getProfileDependencies(actor, profileID)
      throw new RuntimeDomainError('agent_profile_in_use', 'Agent profile is still referenced', 409, {
        dependencies: refreshed.dependencies,
        operations: refreshed.operations
      })
    }
    if (deleted === 'not_found') {
      throw new RuntimeDomainError('agent_profile_not_found', 'Agent profile not found', 404)
    }
    return { deleted: true }
  }

  async listResources(actor: RuntimeActor) {
    const [resources, profiles] = await Promise.all([
      this.store.listAgentResources(actor.workspace_id),
      this.store.listProfiles(actor.workspace_id)
    ])
    return groupResources(resources, profiles)
  }

  async listResourceVersions(actor: RuntimeActor, resourceID: number) {
    await this.getResource(actor, resourceID)
    return this.store.listAgentResourceVersions(actor.workspace_id, resourceID)
  }

  async createResource(actor: RuntimeActor, payload: Record<string, unknown>) {
    const resource = resourceFromPayload(await this.store.nextAgentResourceId(), actor, payload)
    assertValidResource(resource)
    await assertUniqueResourceKey(this.store, resource)
    return sanitizeResource(await this.saveResourceRevision(actor, resource, 'create'))
  }

  async updateResource(actor: RuntimeActor, resourceID: number, payload: Record<string, unknown>) {
    const resource = await this.store.getAgentResource(actor.workspace_id, resourceID)
    if (!resource) {
      throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404)
    }
    const updated = mergeResource(resource, payload)
    await this.assertResourceStatusMutationAllowed(actor, resource, updated)
    assertValidResource(updated)
    await assertUniqueResourceKey(this.store, updated)
    return sanitizeResource(await this.saveResourceRevision(actor, updated, 'update'))
  }

  private async assertResourceStatusMutationAllowed(actor: RuntimeActor, previous: AgentResource, next: AgentResource) {
    if (previous.status === next.status || !['disabled', 'archived'].includes(next.status)) return
    const dependencies = await this.getResourceDependencies(actor, previous.id)
    const operation = next.status === 'disabled' ? dependencies.operations.disable : dependencies.operations.archive
    if (operation?.allowed === true) return
    throw new RuntimeDomainError('agent_resource_in_use', `Agent resource cannot be ${next.status} while it is referenced`, 409, {
      dependencies: dependencies.dependencies,
      operations: dependencies.operations
    })
  }

  async deleteResource(actor: RuntimeActor, resourceID: number) {
    await this.getResource(actor, resourceID)
    const dependencies = await this.getResourceDependencies(actor, resourceID)
    const blockers = dependencies.dependencies.filter((dependency) =>
      ['blocking_live_ref', 'active_execution'].includes(String(dependency.dependency_class))
    )
    if (blockers.length > 0) {
      throw new RuntimeDomainError('agent_resource_in_use', 'Agent resource is still referenced', 409, {
        dependencies: dependencies.dependencies,
        operations: dependencies.operations
      })
    }
    const deleted = await this.store.deleteAgentResourceGuarded(actor.workspace_id, resourceID)
    if (deleted === 'in_use') {
      const refreshed = await this.getResourceDependencies(actor, resourceID)
      throw new RuntimeDomainError('agent_resource_in_use', 'Agent resource is still referenced', 409, {
        dependencies: refreshed.dependencies,
        operations: refreshed.operations
      })
    }
    if (deleted === 'not_found') {
      throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404)
    }
    return { deleted: true }
  }

  private async getResource(actor: RuntimeActor, resourceID: number) {
    const resource = await this.store.getAgentResource(actor.workspace_id, resourceID)
    if (!resource) throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404)
    return resource
  }

  async getResourceDependencies(actor: RuntimeActor, resourceID: number) {
    const resource = await this.getResource(actor, resourceID)
    const profiles = await this.store.listProfiles(actor.workspace_id)
    const dependencies: Array<Record<string, unknown>> = []
    const section = resource.resource_kind === 'skill' ? 'skills' : 'mcp_servers'
    for (const profile of profiles) {
      const refs = profile[section]
      if (refs.some((ref) => refMatchesResource(ref, resource))) {
        dependencies.push({
          dependency_class: 'blocking_live_ref',
          dependency_type: 'profile_draft',
          profile_id: profile.id,
          profile_name: profile.name,
          profile_section: section
        })
      }
      for (const version of await this.store.listProfileVersions(actor.workspace_id, profile.id)) {
        const frozen = version.snapshot.frozen_resources?.[section] || []
        if (frozen.some((item) => item.id === resource.id)) {
          dependencies.push({
            dependency_class: 'historical_snapshot',
            dependency_type: 'profile_version',
            profile_id: profile.id,
            profile_name: profile.name,
            profile_version_id: version.profile_version_id,
            profile_version: version.version
          })
        }
      }
    }
    const sessions = await this.store.listSessions(actor.workspace_id)
    for (const session of sessions) {
      const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)
      const frozen = activeRun?.profile_snapshot?.frozen_resources?.[section] || []
      if (frozen.some((item) => item.id === resource.id)) {
        dependencies.push({
          dependency_class: 'active_execution',
          dependency_type: 'runtime_run',
          session_id: session.id,
          runtime_run_id: activeRun?.runtime_run_id
        })
      }
    }
    return {
      resource_id: resource.id,
      dependencies,
      operations: {
        delete: { allowed: !dependencies.some((item) => ['blocking_live_ref', 'active_execution'].includes(String(item.dependency_class))) },
        disable: { allowed: !dependencies.some((item) => ['blocking_live_ref', 'active_execution'].includes(String(item.dependency_class))) },
        archive: { allowed: !dependencies.some((item) => ['blocking_live_ref', 'active_execution'].includes(String(item.dependency_class))) }
      }
    }
  }

  async getProfileDependencies(actor: RuntimeActor, profileID: number) {
    const profile = await this.getProfile(actor, profileID)
    const profiles = await this.store.listProfiles(actor.workspace_id)
    const dependencies: Array<Record<string, unknown>> = []
    for (const parent of profiles) {
      if (parent.id !== profileID && parent.subagents.some((ref) => Number(ref.resource_id) === profileID)) {
        dependencies.push({
          dependency_class: 'blocking_live_ref',
          dependency_type: 'parent_profile_draft',
          profile_id: parent.id,
          profile_name: parent.name
        })
      }
      for (const version of await this.store.listProfileVersions(actor.workspace_id, parent.id)) {
        if (parent.id === profileID) {
          dependencies.push({
            dependency_class: 'published_version',
            dependency_type: 'profile_version',
            profile_id: profileID,
            profile_name: profile.name,
            profile_version_id: version.profile_version_id,
            profile_version: version.version
          })
        } else if (version.snapshot.frozen_resources?.subagents.some((item) => item.snapshot.id === profileID)) {
          dependencies.push({
            dependency_class: 'historical_snapshot',
            dependency_type: 'parent_profile_version',
            profile_id: parent.id,
            profile_name: parent.name,
            profile_version_id: version.profile_version_id
          })
        }
      }
    }
    for (const session of await this.store.listSessions(actor.workspace_id)) {
      if (session.agent_profile_id !== profileID) continue
      dependencies.push({
        dependency_class: 'session_reference',
        dependency_type: 'session',
        profile_id: profileID,
        session_id: session.id,
        status: session.status
      })
      const activeRun = await this.store.findActiveRun(actor.workspace_id, session.id)
      if (activeRun) {
        dependencies.push({
          dependency_class: 'active_execution',
          dependency_type: 'runtime_run',
          profile_id: profileID,
          session_id: session.id,
          runtime_run_id: activeRun.runtime_run_id
        })
      }
    }
    return {
      profile_id: profileID,
      dependencies,
      operations: {
        delete: {
          allowed: !dependencies.some((item) =>
            ['blocking_live_ref', 'published_version', 'session_reference', 'active_execution'].includes(String(item.dependency_class))
          )
        },
        disable: {
          allowed: !dependencies.some((item) =>
            ['blocking_live_ref', 'session_reference', 'active_execution'].includes(String(item.dependency_class))
          )
        },
        archive: { allowed: true }
      }
    }
  }

  async scanResource(actor: RuntimeActor, resourceID: number) {
    const resource = await this.store.getAgentResource(actor.workspace_id, resourceID)
    if (!resource) {
      throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404)
    }
    if (resource.resource_kind === 'mcp_server') {
      try {
        const discoveredTools = await scanMcpTools(resource)
        const updated = await this.saveResourceRevision(actor, {
          ...resource,
          spec: scanMcpSucceededSpec(resource, discoveredTools),
          updated_at: now()
        }, 'update')
        return {
          resource: sanitizeResource(updated),
          discovered_tools: discoveredTools
        }
      } catch (error) {
        if (error instanceof RuntimeDomainError) throw error
        throw new RuntimeDomainError(
          'mcp_server_scan_failed',
          error instanceof Error ? error.message : 'MCP server scan failed',
          400
        )
      }
    }
    try {
      const discoveredSkills = await scanSkillRepositoryFiles(resource)
      const updated = await this.saveResourceRevision(actor, {
        ...resource,
        spec: scanSkillSucceededSpec(resource, discoveredSkills as unknown as Record<string, unknown>[]),
        updated_at: now()
      }, 'update')
      return {
        resource: sanitizeResource(updated),
        discovered_skills: discoveredSkills
      }
    } catch (error) {
      throw new RuntimeDomainError(
        'skill_repository_scan_failed',
        error instanceof Error ? error.message : 'Skill repository scan failed',
        400
      )
    }
  }

  private async saveResourceRevision(actor: RuntimeActor, resource: AgentResource, operation: 'create' | 'update') {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const versions = await this.store.listAgentResourceVersions(actor.workspace_id, resource.id)
      const revision = (versions[0]?.revision || 0) + 1
      const version = createAgentResourceVersion({
        resourceVersionID: await this.store.nextAgentResourceVersionId(),
        revision,
        resource,
        createdBy: actor.user_id,
        createdAt: now()
      })
      try {
        const saved = operation === 'create'
          ? await this.store.createAgentResourceWithVersion(resource, version)
          : await this.store.updateAgentResourceWithVersion(resource, version)
        return saved.resource
      } catch (error) {
        if (!isResourceRevisionConflict(error) || attempt === 2) throw error
      }
    }
    throw new RuntimeDomainError('agent_resource_revision_conflict', 'Agent resource revision could not be allocated', 409)
  }

  private async pinDraftResourceRefs(actor: RuntimeActor, profile: AgentProfileDraft) {
    const resources = await this.store.listAgentResources(actor.workspace_id)
    const pin = async (ref: AgentResourceRef, kind: AgentResourceKind): Promise<AgentResourceRef> => {
      if (kind === 'mcp_server' && normalizedResourceID(ref.resource_id) === 'easydo') {
        return {
          ...ref,
          resource_version: 'builtin-v1',
          snapshot_digest: builtinEasyDoSnapshotDigest()
        }
      }
      const resource = resources.find((candidate) => candidate.resource_kind === kind && refMatchesResource(ref, candidate))
      if (!resource) return ref
      const version = await this.resolveResourceVersion(actor, ref, resource, kind)
      return {
        ...refWithDefaultToolPermissions(ref, version.snapshot),
        resource_version_id: version.resource_version_id,
        resource_version: version.source_version,
        snapshot_digest: version.snapshot_digest
      }
    }
    const pinSubagent = async (ref: AgentResourceRef): Promise<AgentResourceRef> => {
      if (ref.resource_type !== 'subagent_profile') return ref
      const profileID = Number(ref.resource_id)
      if (!Number.isInteger(profileID) || profileID <= 0) {
        throw new RuntimeDomainError('agent_profile_subagent_version_not_found', `Published Subagent ${ref.resource_id} version was not found`, 404)
      }
      let version: AgentProfileVersion
      try {
        version = await this.resolvePublishedSubagentVersion(actor, ref, profileID)
      } catch (error) {
        const hasExplicitVersion = Number(ref.resource_version_id || 0) > 0 || String(ref.resource_version || '').trim().length > 0
        if (!hasExplicitVersion && error instanceof RuntimeDomainError && error.code === 'agent_profile_subagent_version_not_found') {
          return ref
        }
        throw error
      }
      return {
        ...ref,
        resource_id: profileID,
        resource_version_id: version.profile_version_id,
        resource_version: String(version.version),
        snapshot_digest: version.snapshot_hash
      }
    }
    return {
      ...profile,
      skills: await Promise.all(profile.skills.map((ref) => pin(ref, 'skill'))),
      mcp_servers: await Promise.all(profile.mcp_servers.map((ref) => pin(ref, 'mcp_server'))),
      subagents: await Promise.all(profile.subagents.map(pinSubagent))
    }
  }

  async publishProfile(actor: RuntimeActor, profileID: number, payload: Record<string, unknown>) {
    for (let attempt = 0; attempt < 3; attempt += 1) {
      const validation = await this.validateProfile(actor, profileID)
      if (validation.status === 'failed') {
        throw new RuntimeDomainError('agent_profile_validation_failed', 'Agent profile validation failed')
      }
      const profile = await this.getProfile(actor, profileID)
      const previous = await this.store.listProfileVersions(actor.workspace_id, profileID)
      const frozenResources = await this.freezeProfileResources(actor, profile)
      const snapshot: AgentProfileSnapshot = {
        ...buildSnapshot({ ...profile, status: 'active' }),
        skills: frozenResources.skillRefs,
        mcp_servers: frozenResources.mcpRefs,
        subagents: frozenResources.subagents.map(({ ref }) => ref),
        frozen_resources: {
          skills: frozenResources.skills.map(({ resource }) => resource),
          mcp_servers: frozenResources.mcpServers.map(({ resource }) => resource),
          subagents: frozenResources.subagents.map(({ ref, version }) => ({
            ref,
            profile_version_id: version.profile_version_id,
            snapshot_hash: version.snapshot_hash,
            snapshot: version.snapshot
          }))
        }
      }
      const version: AgentProfileVersion = {
        profile_version_id: await this.store.nextProfileVersionId(),
        profile_id: profile.id,
        workspace_id: actor.workspace_id,
        version: (previous[0]?.version || 0) + 1,
        snapshot,
        snapshot_hash: agentProfileSnapshotHash(snapshot),
        status: 'published',
        published_by: actor.user_id,
        published_at: now(),
        change_summary: String(payload.change_summary || '')
      }
      try {
        return await this.store.publishProfileVersion(
          { ...profile, status: 'active', updated_at: now() },
          version,
          profile.updated_at
        )
      } catch (error) {
        if (!(error instanceof RuntimeDomainError) || error.code !== 'agent_profile_publish_conflict' || attempt === 2) throw error
      }
    }
    throw new RuntimeDomainError('agent_profile_publish_conflict', 'Agent profile changed during publish', 409)
  }

  async compileDraftProfileSnapshot(actor: RuntimeActor, profileID: number) {
    const profile = await this.pinDraftResourceRefs(actor, await this.getProfile(actor, profileID))
    const frozenResources = await this.freezeProfileResources(actor, profile)
    const snapshot: AgentProfileSnapshot = {
      ...buildSnapshot(profile),
      skills: frozenResources.skillRefs,
      mcp_servers: frozenResources.mcpRefs,
      subagents: frozenResources.subagents.map(({ ref }) => ref),
      frozen_resources: {
        skills: frozenResources.skills.map(({ resource }) => resource),
        mcp_servers: frozenResources.mcpServers.map(({ resource }) => resource),
        subagents: frozenResources.subagents.map(({ ref, version }) => ({
          ref,
          profile_version_id: version.profile_version_id,
          snapshot_hash: version.snapshot_hash,
          snapshot: version.snapshot
        }))
      }
    }
    return {
      snapshot,
      snapshot_hash: agentProfileSnapshotHash(snapshot)
    }
  }

  private async freezeProfileResources(actor: RuntimeActor, profile: AgentProfileDraft) {
    const resources = await this.store.listAgentResources(actor.workspace_id)
    type FrozenResourceResolution = { ref: AgentResourceRef, resource?: AgentResource }
    const freezeResources = async (refs: AgentResourceRef[], kind: AgentResourceKind): Promise<FrozenResourceResolution[]> => Promise.all(refs.map(async (ref) => {
      if (kind === 'mcp_server' && normalizedResourceID(ref.resource_id) === 'easydo') {
        return { ref: { ...ref, resource_version: 'builtin-v1', snapshot_digest: builtinEasyDoSnapshotDigest() } }
      }
      const resource = resources.find((candidate) => candidate.resource_kind === kind && refMatchesResource(ref, candidate))
      if (!resource) {
        throw new RuntimeDomainError('agent_profile_resource_not_found', `${kind} resource ${ref.resource_id} was not found`)
      }
      if (resource.status !== 'active') {
        throw new RuntimeDomainError('agent_profile_resource_not_active', `${kind} resource ${ref.resource_id} is not active`)
      }
      const version = await this.resolveResourceVersion(actor, ref, resource, kind)
      return {
        ref: {
          ...refWithDefaultToolPermissions(ref, version.snapshot),
          resource_version_id: version.resource_version_id,
          resource_version: version.source_version,
          snapshot_digest: version.snapshot_digest
        },
        resource: {
          ...version.snapshot,
          resource_version_id: version.resource_version_id,
          snapshot_digest: version.snapshot_digest,
          created_at: version.created_at,
          updated_at: version.created_at
        }
      }
    }))
    const frozenSkills = await freezeResources(profile.skills, 'skill')
    const frozenMcpServers = await freezeResources(profile.mcp_servers, 'mcp_server')
    const skills = frozenSkills.flatMap((item) => item.resource ? [{ ref: item.ref, resource: item.resource }] : [])
    const mcpServers = frozenMcpServers.flatMap((item) => item.resource ? [{ ref: item.ref, resource: item.resource }] : [])
    const subagents = [] as Array<{ ref: AgentResourceRef, version: AgentProfileVersion }>
    for (const ref of profile.subagents) {
      if (ref.resource_type !== 'subagent_profile') {
        throw new RuntimeDomainError('agent_profile_subagent_type_invalid', `Subagent reference ${ref.resource_id} is invalid`)
      }
      const profileID = Number(ref.resource_id)
      const version = await this.resolvePublishedSubagentVersion(actor, ref, profileID)
      subagents.push({
        ref: {
          ...ref,
          resource_version_id: version.profile_version_id,
          resource_version: String(version.version),
          snapshot_digest: version.snapshot_hash
        },
        version
      })
    }
    return {
      skills,
      skillRefs: frozenSkills.map(({ ref }) => ref),
      mcpServers,
      mcpRefs: frozenMcpServers.map(({ ref }) => ref),
      subagents
    } satisfies {
      skills: Array<{ ref: AgentResourceRef, resource: AgentResource }>
      skillRefs: AgentResourceRef[]
      mcpServers: Array<{ ref: AgentResourceRef, resource: AgentResource }>
      mcpRefs: AgentResourceRef[]
      subagents: Array<{ ref: AgentResourceRef, version: AgentProfileVersion }>
    }
  }

  async listVersions(actor: RuntimeActor, profileID: number) {
    await this.getProfile(actor, profileID)
    return this.store.listProfileVersions(actor.workspace_id, profileID)
  }

  async validateProfile(actor: RuntimeActor, profileID: number): Promise<ProfileValidationResult> {
    const profile = await this.getProfile(actor, profileID)
    const errors: ProfileValidationIssue[] = []
    if (!profile.name.trim()) {
      errors.push({ code: 'name_required', message: 'Agent profile name is required', path: 'name' })
    }
    try {
      assertKnownContextTags(profile.context_tags)
      assertProfileContextTags(profile.name, profile.context_tags)
    } catch (error) {
      if (error instanceof RuntimeDomainError) {
        errors.push({ code: error.code, message: error.message, path: 'context_tags' })
      } else {
        throw error
      }
    }
    if (Object.keys(profile.provider).length === 0) {
      errors.push({ code: 'provider_required', message: 'Provider selection is required', path: 'provider' })
    }
    if (Object.keys(profile.model).length === 0) {
      errors.push({ code: 'model_required', message: 'Model selection is required', path: 'model' })
    }
    if (await this.hasSubagentCycle(actor.workspace_id, profile.id)) {
      errors.push({ code: 'subagent_cycle', message: 'Subagent profile references contain a cycle', path: 'subagents' })
    }
    const warnings: ProfileValidationIssue[] = []
    const resource_health: ResourceHealthResult[] = []
    const resources = await this.store.listAgentResources(actor.workspace_id)
    const resolvePinnedResource = async (ref: AgentResourceRef, kind: AgentResourceKind) => {
      const resource = resources.find((candidate) => candidate.resource_kind === kind && refMatchesResource(ref, candidate))
      if (!resource) return { resource: undefined, version: undefined }
      try {
        return { resource, version: await this.resolveResourceVersion(actor, ref, resource, kind) }
      } catch (error) {
        if (error instanceof RuntimeDomainError) return { resource, version: undefined, resolutionError: error }
        throw error
      }
    }
    const recordFailure = (ref: AgentResourceRef, code: string, message: string, path: string) => {
      resource_health.push({
        resource_type: ref.resource_type,
        resource_id: ref.resource_id,
        status: 'failed',
        message
      })
      const issue = { code, message, path }
      if (ref.required === false) warnings.push(issue)
      else errors.push(issue)
    }
    for (const [index, ref] of profile.skills.entries()) {
      const path = `skills.${index}`
      const { resource, version, resolutionError } = await resolvePinnedResource(ref, 'skill')
      if (!resource) {
        recordFailure(ref, 'agent_profile_resource_not_found', `Skill ${ref.resource_id} was not found`, path)
        continue
      }
      if (resource.status !== 'active') {
        recordFailure(ref, 'agent_profile_resource_not_active', `Skill ${ref.resource_id} is not active`, path)
        continue
      }
      if (!version) {
        recordFailure(
          ref,
          resolutionError?.code || 'agent_profile_resource_version_not_found',
          resolutionError?.message || `Skill ${ref.resource_id} version was not found`,
          path
        )
        continue
      }
      const spec = asRecord(version.snapshot.spec)
      const hasContent = ['instructions', 'markdown', 'content', 'prompt'].some((key) => String(spec[key] || '').trim())
      const discoveredSkills = Array.isArray(spec.discovered_skills) ? spec.discovered_skills : []
      if (!hasContent && discoveredSkills.length === 0) {
        recordFailure(ref, 'agent_profile_skill_content_missing', `Skill ${ref.resource_id} has no readable content`, path)
        continue
      }
      resource_health.push({
        resource_type: ref.resource_type,
        resource_id: ref.resource_id,
        status: 'healthy',
        message: `Resolved active Skill revision ${version.revision} (${version.source_version})`
      })
    }
    for (const [index, ref] of profile.mcp_servers.entries()) {
      const path = `mcp_servers.${index}`
      if (normalizedResourceID(ref.resource_id) === 'easydo') {
        resource_health.push({
          resource_type: ref.resource_type,
          resource_id: ref.resource_id,
          status: 'healthy',
          message: 'Built-in EasyDo MCP transport is configured by the Runtime'
        })
        continue
      }
      const { resource, version, resolutionError } = await resolvePinnedResource(ref, 'mcp_server')
      if (!resource) {
        recordFailure(ref, 'agent_profile_resource_not_found', `MCP server ${ref.resource_id} was not found`, path)
        continue
      }
      if (resource.status !== 'active') {
        recordFailure(ref, 'agent_profile_resource_not_active', `MCP server ${ref.resource_id} is not active`, path)
        continue
      }
      if (!version) {
        recordFailure(
          ref,
          resolutionError?.code || 'agent_profile_resource_version_not_found',
          resolutionError?.message || `MCP server ${ref.resource_id} version was not found`,
          path
        )
        continue
      }
      const spec = asRecord(version.snapshot.spec)
      const discoveredTools = Array.isArray(spec.discovered_tools) ? spec.discovered_tools : []
      if (discoveredTools.length === 0) {
        const message = `MCP server ${ref.resource_id} has no discovered tools; connection health is unverified`
        resource_health.push({ resource_type: ref.resource_type, resource_id: ref.resource_id, status: 'warning', message })
        warnings.push({ code: 'agent_profile_mcp_tools_unverified', message, path })
        continue
      }
      resource_health.push({
        resource_type: ref.resource_type,
        resource_id: ref.resource_id,
        status: 'healthy',
        message: `Resolved active MCP server revision ${version.revision} (${version.source_version}) with ${discoveredTools.length} discovered tools`
      })
    }
    for (const [index, ref] of profile.subagents.entries()) {
      const path = `subagents.${index}`
      if (ref.resource_type !== 'subagent_profile') {
        recordFailure(ref, 'agent_profile_subagent_type_invalid', `Subagent reference ${ref.resource_id} is invalid`, path)
        continue
      }
      const profileID = Number(ref.resource_id)
      let version: AgentProfileVersion
      try {
        version = await this.resolvePublishedSubagentVersion(actor, ref, profileID)
      } catch (error) {
        if (!(error instanceof RuntimeDomainError)) throw error
        recordFailure(ref, error.code, error.message, path)
        continue
      }
      resource_health.push({
        resource_type: ref.resource_type,
        resource_id: ref.resource_id,
        status: 'healthy',
        message: `Resolved published Subagent version ${version.version}`
      })
    }
    return {
      status: errors.length > 0 ? 'failed' : 'passed',
      errors,
      warnings,
      resource_health
    }
  }

  async getVersion(actor: RuntimeActor, profileVersionID: number) {
    const version = await this.store.getProfileVersion(actor.workspace_id, profileVersionID)
    if (!version) {
      throw new RuntimeDomainError('agent_profile_version_not_found', 'Agent profile version not found', 404)
    }
    return version
  }

  async getPublishedVersionByName(actor: RuntimeActor, profileName: string) {
    const profiles = await this.store.listProfiles(actor.workspace_id)
    const profile = profiles.find((item) => item.name === profileName && item.status === 'active')
    if (!profile) {
      throw new RuntimeDomainError('agent_profile_name_not_found', 'No active agent profile uses this reserved name', 404)
    }
    const versions = (await this.store.listProfileVersions(actor.workspace_id, profile.id))
      .flat()
      .filter((version) => version.status === 'published')
      .sort((left, right) =>
        right.published_at.localeCompare(left.published_at) ||
        right.version - left.version ||
        right.profile_version_id - left.profile_version_id
      )
    const version = versions[0]
    if (!version) {
      throw new RuntimeDomainError('agent_profile_version_not_found', 'No published agent profile version is available', 404)
    }
    return version
  }

  async getLatestPublishedVersion(actor: RuntimeActor, profileID: number) {
    await this.getProfile(actor, profileID)
    const versions = (await this.store.listProfileVersions(actor.workspace_id, profileID))
      .filter((version) => version.status === 'published')
      .sort((left, right) =>
        right.published_at.localeCompare(left.published_at) ||
        right.version - left.version ||
        right.profile_version_id - left.profile_version_id
      )
    const version = versions[0]
    if (!version) {
      throw new RuntimeDomainError('agent_profile_version_not_found', 'No published agent profile version is available', 404)
    }
    return version
  }

  assertVersionCallable(version: AgentProfileVersion) {
    if (version.status !== 'published') {
      throw new RuntimeDomainError('agent_profile_version_not_published', 'Agent profile version is not published')
    }
    assertProfileContextTags(version.snapshot.name, version.snapshot.context_tags)
    if (version.snapshot.status === 'disabled' || version.snapshot.status === 'archived') {
      throw new RuntimeDomainError('agent_profile_not_callable', 'Agent profile cannot be called from chatbox')
    }
  }

  private async ensurePageAssistantProfile(actor: RuntimeActor) {
    const existingJob = this.pageAssistantEnsureJobs.get(actor.workspace_id)
    if (existingJob) {
      await existingJob
      return
    }
    const job = this.materializePageAssistantProfile(actor)
    this.pageAssistantEnsureJobs.set(actor.workspace_id, job)
    try {
      await job
    } finally {
      this.pageAssistantEnsureJobs.delete(actor.workspace_id)
    }
  }

  private async materializePageAssistantProfile(actor: RuntimeActor) {
    const profiles = await this.store.listProfiles(actor.workspace_id)
    if (profiles.some((item) => item.name === PAGE_ASSISTANT_PROFILE_NAME)) return
    const profile = defaultPageAssistantProfile(await this.store.nextProfileId(), actor)
    assertProfileContextTags(profile.name, profile.context_tags)
    await this.store.saveProfile(profile)
  }

  private async assertProfileNameUnique(workspaceID: number, profile: AgentProfileDraft) {
    const profiles = await this.store.listProfiles(workspaceID)
    const existing = profiles.find((item) => item.id !== profile.id && item.name === profile.name)
    if (existing) {
      if (profile.name === PAGE_ASSISTANT_PROFILE_NAME) {
        throw new RuntimeDomainError('reserved_profile_name_exists', 'page-ai-assistant profile already exists in this workspace', 409)
      }
      throw new RuntimeDomainError('agent_profile_name_exists', 'Agent profile name already exists in this workspace', 409)
    }
  }

  private async hasSubagentCycle(workspaceID: number, profileID: number) {
    const visited = new Set<number>()
    const stack = new Set<number>()
    const visit = async (currentID: number): Promise<boolean> => {
      if (stack.has(currentID)) return true
      if (visited.has(currentID)) return false
      visited.add(currentID)
      stack.add(currentID)
      const profile = await this.store.getProfile(workspaceID, currentID)
      if (profile) {
        for (const ref of profile.subagents) {
          if (ref.resource_type !== 'subagent_profile') continue
          const nextID = Number(ref.resource_id)
          if (Number.isFinite(nextID) && nextID > 0 && (await visit(nextID))) {
            return true
          }
        }
      }
      stack.delete(currentID)
      return false
    }
    return visit(profileID)
  }
}
