<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">AI Agent</h2>
        <div class="section-subtitle">管理当前工作区下的 AI Agent，并绑定工作区级 Runtime Profile 与运行能力配置。</div>
      </div>
      <el-button v-if="canManageAI" type="primary" @click="openAgentDialog()">新建 AI Agent</el-button>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

    <el-table v-else :data="agents" style="width: 100%">
      <el-table-column prop="name" label="AI Agent" min-width="180" />
      <el-table-column label="Runtime Profile" min-width="220">
        <template #default="{ row }">{{ runtimeProfileName(row) }}</template>
      </el-table-column>
      <el-table-column label="能力配置" min-width="220">
        <template #default="{ row }">{{ capabilitySummary(row) }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <el-button v-if="canManageAI" link type="primary" @click="openAgentDialog(row)">编辑</el-button>
          <el-button v-if="canManageAI" link type="danger" @click="removeAgent(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="agentDialogVisible" :title="agentForm.id ? '编辑 AI Agent' : '新建 AI Agent'" width="820px">
      <el-form :model="agentForm" label-width="140px">
        <el-form-item label="名称">
          <el-input v-model="agentForm.name" />
        </el-form-item>
        <el-form-item label="Runtime Profile">
          <el-select v-model="agentForm.runtime_profile_id" clearable style="width: 100%">
            <el-option
              v-for="profile in runtimeProfiles"
              :key="profile.id"
              :label="runtimeProfileOptionLabel(profile)"
              :value="profile.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="agentForm.status" style="width: 100%">
            <el-option label="Draft" value="draft" />
            <el-option label="Active" value="active" />
            <el-option label="Archived" value="archived" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="agentForm.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="System Prompt">
          <el-input v-model="agentForm.system_prompt" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="User Prompt Template">
          <el-input v-model="agentForm.user_prompt_template" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="Input Schema JSON">
          <el-input v-model="agentForm.input_schema_json" type="textarea" :rows="4" placeholder='{"type":"object","properties":{"input":{"type":"string"}}}' />
        </el-form-item>
        <el-form-item label="Output Schema JSON">
          <el-input v-model="agentForm.output_schema_json" type="textarea" :rows="4" placeholder='{"type":"object","properties":{"summary":{"type":"string"}}}' />
        </el-form-item>
        <el-form-item label="Tool Policy JSON">
          <el-input v-model="agentForm.tool_policy_json" type="textarea" :rows="4" placeholder='{"mode":"strict"}' />
        </el-form-item>
        <el-form-item label="Tools JSON">
          <el-input v-model="agentForm.tools_json" type="textarea" :rows="4" placeholder='[{"name":"read_page_context"}]' />
        </el-form-item>
        <el-form-item label="Skills JSON">
          <el-input v-model="agentForm.skills_json" type="textarea" :rows="4" placeholder='[{"name":"summarize"}]' />
        </el-form-item>
        <el-form-item label="Memory JSON">
          <el-input v-model="agentForm.memory_json" type="textarea" :rows="4" placeholder='{"mode":"short_term"}' />
        </el-form-item>
        <el-form-item label="MCP Servers JSON">
          <el-input v-model="agentForm.mcp_servers_json" type="textarea" :rows="4" placeholder='[{"name":"grafana"}]' />
        </el-form-item>
        <el-form-item label="Sub Agents JSON">
          <el-input v-model="agentForm.sub_agents_json" type="textarea" :rows="4" placeholder='[{"agent_id":2,"name":"log-agent"}]' />
        </el-form-item>
        <el-form-item label="Metadata JSON">
          <el-input v-model="agentForm.metadata_json" type="textarea" :rows="4" placeholder='{"owner":"workspace-admin"}' />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="agentDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="agentSaving" @click="submitAgent">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import {
  createWorkspaceAIAgent,
  deleteWorkspaceAIAgent,
  getWorkspaceAIAgents,
  getWorkspaceAIRuntimeProfiles,
  updateWorkspaceAIAgent
} from '@/api/agent'

const userStore = useUserStore()
const agents = ref([])
const runtimeProfiles = ref([])
const agentDialogVisible = ref(false)
const agentSaving = ref(false)
const canManageAI = computed(() => userStore.canAccessWorkspaceGovernance)
const runtimeProfileById = computed(() => new Map(runtimeProfiles.value.map((profile) => [String(profile.id), profile])))
const agentForm = reactive({
  id: 0,
  name: '',
  description: '',
  runtime_profile_id: undefined,
  system_prompt: '',
  user_prompt_template: '',
  input_schema_json: '{}',
  output_schema_json: '{}',
  tool_policy_json: '{}',
  tools_json: '[]',
  skills_json: '[]',
  memory_json: '{}',
  mcp_servers_json: '[]',
  sub_agents_json: '[]',
  metadata_json: '{}',
  status: 'draft'
})

const resetAgentForm = () => {
  agentForm.id = 0
  agentForm.name = ''
  agentForm.description = ''
  agentForm.runtime_profile_id = undefined
  agentForm.system_prompt = ''
  agentForm.user_prompt_template = ''
  agentForm.input_schema_json = '{}'
  agentForm.output_schema_json = '{}'
  agentForm.tool_policy_json = '{}'
  agentForm.tools_json = '[]'
  agentForm.skills_json = '[]'
  agentForm.memory_json = '{}'
  agentForm.mcp_servers_json = '[]'
  agentForm.sub_agents_json = '[]'
  agentForm.metadata_json = '{}'
  agentForm.status = 'draft'
}

const loadAgentData = async () => {
  if (!userStore.currentWorkspaceId) {
    agents.value = []
    runtimeProfiles.value = []
    return
  }
  try {
    const [agentsRes, runtimeProfilesRes] = await Promise.all([
      getWorkspaceAIAgents(),
      getWorkspaceAIRuntimeProfiles()
    ])
    agents.value = extractList(agentsRes?.data)
    runtimeProfiles.value = extractList(runtimeProfilesRes?.data)
  } catch (error) {
    ElMessage.error('加载 AI Agent 失败')
  }
}

const runtimeProfileOptionLabel = (profile) => {
  const modelName = profile?.model?.display_name || profile?.model?.name
  return modelName ? `${profile.name} · ${modelName}` : profile?.name || '-'
}

const runtimeProfileName = (row) => {
  const profileId = runtimeProfileRelationId(row) ?? ''
  const profile = runtimeProfileById.value.get(String(profileId))
  if (profile) {
    return runtimeProfileOptionLabel(profile)
  }
  if (row?.runtime_profile?.name) {
    return runtimeProfileOptionLabel(row.runtime_profile)
  }
  return '-'
}

const capabilitySummary = (row) => {
  const tools = normalizeArrayLength(row?.tools_json)
  const skills = normalizeArrayLength(row?.skills_json)
  const mcpServers = normalizeArrayLength(row?.mcp_servers_json)
  const subAgents = normalizeArrayLength(row?.sub_agents_json)
  return `Tools ${tools} / Skills ${skills} / MCP ${mcpServers} / Sub ${subAgents}`
}

const openAgentDialog = (row = null) => {
  resetAgentForm()
  if (row) {
    agentForm.id = row.id
    agentForm.name = row.name || ''
    agentForm.description = row.description || ''
    agentForm.runtime_profile_id = normalizeNullableId(runtimeProfileRelationId(row))
    agentForm.system_prompt = row.system_prompt || ''
    agentForm.user_prompt_template = row.user_prompt_template || ''
    agentForm.input_schema_json = formatJsonText(row.input_schema_json, {})
    agentForm.output_schema_json = formatJsonText(row.output_schema_json, {})
    agentForm.tool_policy_json = formatJsonText(row.tool_policy_json, {})
    agentForm.tools_json = formatJsonText(row.tools_json, [])
    agentForm.skills_json = formatJsonText(row.skills_json, [])
    agentForm.memory_json = formatJsonText(row.memory_json, {})
    agentForm.mcp_servers_json = formatJsonText(row.mcp_servers_json, [])
    agentForm.sub_agents_json = formatJsonText(row.sub_agents_json, [])
    agentForm.metadata_json = formatJsonText(row.metadata_json, {})
    agentForm.status = row.status || 'draft'
  }
  agentDialogVisible.value = true
}

const submitAgent = async () => {
  const payload = buildAgentPayload()
  if (!payload) {
    return
  }

  agentSaving.value = true
  try {
    if (agentForm.id) {
      await updateWorkspaceAIAgent(agentForm.id, payload)
    } else {
      await createWorkspaceAIAgent(payload)
    }
    agentDialogVisible.value = false
    await loadAgentData()
    ElMessage.success('AI Agent 已保存')
  } catch (error) {
    ElMessage.error('保存 AI Agent 失败')
  } finally {
    agentSaving.value = false
  }
}

const buildAgentPayload = () => {
  const inputSchema = parseJsonField(agentForm.input_schema_json, {}, 'Input Schema JSON')
  const outputSchema = parseJsonField(agentForm.output_schema_json, {}, 'Output Schema JSON')
  const toolPolicy = parseJsonField(agentForm.tool_policy_json, {}, 'Tool Policy JSON')
  const tools = parseJsonField(agentForm.tools_json, [], 'Tools JSON')
  const skills = parseJsonField(agentForm.skills_json, [], 'Skills JSON')
  const memory = parseJsonField(agentForm.memory_json, {}, 'Memory JSON')
  const mcpServers = parseJsonField(agentForm.mcp_servers_json, [], 'MCP Servers JSON')
  const subAgents = parseJsonField(agentForm.sub_agents_json, [], 'Sub Agents JSON')
  const metadata = parseJsonField(agentForm.metadata_json, {}, 'Metadata JSON')

  if ([inputSchema, outputSchema, toolPolicy, tools, skills, memory, mcpServers, subAgents, metadata].some((item) => item === INVALID_JSON)) {
    return null
  }

  return {
    name: agentForm.name,
    description: agentForm.description,
    runtime_profile_id: normalizeNullableId(agentForm.runtime_profile_id) ?? null,
    system_prompt: agentForm.system_prompt,
    user_prompt_template: agentForm.user_prompt_template,
    input_schema_json: inputSchema,
    output_schema_json: outputSchema,
    tool_policy_json: toolPolicy,
    tools_json: tools,
    skills_json: skills,
    memory_json: memory,
    mcp_servers_json: mcpServers,
    sub_agents_json: subAgents,
    metadata_json: metadata,
    status: agentForm.status
  }
}

const removeAgent = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除 AI Agent ${row.name} 吗？`, '删除 AI Agent', { type: 'warning' })
    await deleteWorkspaceAIAgent(row.id)
    await loadAgentData()
    ElMessage.success('AI Agent 已删除')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除 AI Agent 失败')
    }
  }
}

function extractList(payload) {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.agents)) return payload.agents
  if (Array.isArray(payload?.runtimeProfiles)) return payload.runtimeProfiles
  if (Array.isArray(payload?.runtime_profiles)) return payload.runtime_profiles
  return []
}

function runtimeProfileRelationId(record) {
  return record?.runtime_profile_id ?? record?.runtimeProfileID ?? record?.runtime_profile?.id ?? null
}

function normalizeNullableId(value) {
  const normalized = Number(value)
  return Number.isFinite(normalized) && normalized > 0 ? normalized : undefined
}

function normalizeArrayLength(value) {
  const parsed = parseSerializedJson(value)
  return Array.isArray(parsed) ? parsed.length : 0
}

function formatJsonText(value, fallback) {
  const parsed = parseSerializedJson(value)
  return JSON.stringify(parsed ?? fallback, null, 2)
}

function parseSerializedJson(value) {
  if (value == null || value === '') {
    return null
  }
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch {
      return null
    }
  }
  return value
}

const INVALID_JSON = Symbol('invalid-json')

function parseJsonField(value, fallback, label) {
  const raw = String(value || '').trim()
  if (!raw) {
    return fallback
  }
  try {
    return JSON.parse(raw)
  } catch {
    ElMessage.warning(`${label} 不是有效的 JSON`)
    return INVALID_JSON
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadAgentData()
}, { immediate: true })
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.governance-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.section-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.section-subtitle {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 14px;
}

.empty-hint {
  padding: 24px;
  background: var(--bg-card);
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-align: center;
}
</style>