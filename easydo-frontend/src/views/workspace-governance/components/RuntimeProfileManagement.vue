<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">运行策略</h2>
        <div class="section-subtitle">管理工作区级 Runtime Profile，并查看当前被哪些 AI Agent 引用。</div>
      </div>
      <el-button v-if="canManageAI" type="primary" :disabled="!models.length" @click="openProfileDialog()">新增运行策略</el-button>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>
    <div v-else-if="!models.length" class="empty-hint">当前工作区还没有可用模型，请先在平台治理中准备模型与 Binding。</div>

    <el-table v-else :data="runtimeProfiles" style="width: 100%">
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column label="模型" width="200">
        <template #default="{ row }">{{ modelName(runtimeProfileModelId(row)) }}</template>
      </el-table-column>
      <el-table-column prop="fallback_enabled" label="允许降级" width="120">
        <template #default="{ row }">
          <el-tag :type="row.fallback_enabled ? 'success' : 'info'" size="small">{{ row.fallback_enabled ? '是' : '否' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="引用 Agent" min-width="220" show-overflow-tooltip>
        <template #default="{ row }">{{ referencedAgentNames(row) }}</template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column label="模型绑定顺序" min-width="260" show-overflow-tooltip>
        <template #default="{ row }">{{ bindingPriorityText(row.binding_priority_json) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button v-if="canManageAI" link type="primary" @click="openProfileDialog(row)">编辑</el-button>
          <el-button v-if="canManageAI" link type="danger" @click="removeProfile(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="profileDialogVisible" :title="profileForm.id ? '编辑运行策略' : '新增运行策略'" width="780px">
      <el-form :model="profileForm" label-width="140px">
        <el-form-item label="名称">
          <el-input v-model="profileForm.name" placeholder="例如 MR Review Default" />
        </el-form-item>
        <el-form-item label="模型">
          <el-select v-model="profileForm.model_id" style="width: 100%" clearable>
            <el-option v-for="model in models" :key="model.id" :label="model.display_name || model.name" :value="model.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="模型绑定顺序(JSON)">
          <el-input v-model="profileForm.binding_priority_json" type="textarea" :rows="6" placeholder='[{"binding_id":1,"priority":1,"enabled":true,"fallback_on_error":true}]' />
        </el-form-item>
        <el-form-item label="运行参数(JSON)">
          <el-input v-model="profileForm.runtime_settings_json" type="textarea" :rows="4" placeholder='{"output_language":"zh-CN"}' />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="profileForm.status" style="width: 100%">
            <el-option label="Draft" value="draft" />
            <el-option label="Active" value="active" />
            <el-option label="Disabled" value="disabled" />
          </el-select>
        </el-form-item>
        <el-form-item label="允许降级">
          <el-switch v-model="profileForm.fallback_enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="profileDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="profileSaving" @click="submitProfile">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import {
  createWorkspaceAIRuntimeProfile,
  deleteWorkspaceAIRuntimeProfile,
  getWorkspaceAIAgents,
  getWorkspaceAIModelCatalog,
  getWorkspaceAIRuntimeProfiles,
  updateWorkspaceAIRuntimeProfile
} from '@/api/agent'

const userStore = useUserStore()
const agents = ref([])
const models = ref([])
const runtimeProfiles = ref([])
const profileDialogVisible = ref(false)
const profileSaving = ref(false)
const canManageAI = computed(() => userStore.canAccessWorkspaceGovernance)
const profileForm = reactive({
  id: 0,
  name: '',
  model_id: undefined,
  binding_priority_json: '[]',
  runtime_settings_json: '{}',
  fallback_enabled: true,
  status: 'draft'
})

const resetProfileForm = () => {
  profileForm.id = 0
  profileForm.name = ''
  profileForm.model_id = undefined
  profileForm.binding_priority_json = '[]'
  profileForm.runtime_settings_json = '{}'
  profileForm.fallback_enabled = true
  profileForm.status = 'draft'
}

const loadBaseData = async () => {
  if (!userStore.currentWorkspaceId) {
    agents.value = []
    models.value = []
    runtimeProfiles.value = []
    return
  }
  try {
    const [agentsRes, modelsRes, runtimeProfilesRes] = await Promise.all([
      getWorkspaceAIAgents(),
      getWorkspaceAIModelCatalog(),
      getWorkspaceAIRuntimeProfiles()
    ])
    agents.value = extractList(agentsRes?.data)
    models.value = extractList(modelsRes?.data)
    runtimeProfiles.value = extractList(runtimeProfilesRes?.data)
  } catch (error) {
    ElMessage.error('加载运行策略数据失败')
  }
}

const modelName = (modelId) => {
  const model = models.value.find(item => Number(item.id) === Number(modelId))
  return model?.display_name || model?.name || modelId || '-'
}

const referencedAgentNames = (row) => {
  const matched = agents.value
    .filter(agent => Number(runtimeProfileRelationId(agent) || 0) === Number(row?.id || 0))
    .map(agent => agent.name)
  return matched.length > 0 ? matched.join('、') : '-'
}

const bindingPriorityText = (value) => {
  const parsed = parseSerializedJson(value)
  if (parsed == null) {
    return '[]'
  }
  return typeof parsed === 'string' ? parsed : JSON.stringify(parsed)
}

const openProfileDialog = (row = null) => {
  resetProfileForm()
  if (row) {
    profileForm.id = row.id
    profileForm.name = row.name || ''
    profileForm.model_id = normalizeNullableId(runtimeProfileModelId(row))
    profileForm.binding_priority_json = formatJsonText(row.binding_priority_json, [])
    profileForm.runtime_settings_json = formatJsonText(row.runtime_settings_json, {})
    profileForm.fallback_enabled = Boolean(row.fallback_enabled)
    profileForm.status = row.status || 'draft'
  }
  profileDialogVisible.value = true
}

const submitProfile = async () => {
  const bindingPriority = parseJsonField(profileForm.binding_priority_json, [], '模型绑定顺序(JSON)')
  const runtimeSettings = parseJsonField(profileForm.runtime_settings_json, {}, '运行参数(JSON)')
  if (bindingPriority === INVALID_JSON || runtimeSettings === INVALID_JSON) {
    return
  }
  if (!Array.isArray(bindingPriority)) {
    ElMessage.warning('模型绑定顺序(JSON) 必须是数组')
    return
  }
  if (!isPlainObject(runtimeSettings)) {
    ElMessage.warning('运行参数(JSON) 必须是对象')
    return
  }

  profileSaving.value = true
  try {
    const payload = {
      name: profileForm.name,
      model_id: profileForm.model_id,
      binding_priority_json: bindingPriority,
      runtime_settings_json: runtimeSettings,
      fallback_enabled: profileForm.fallback_enabled,
      status: profileForm.status
    }
    if (profileForm.id) {
      await updateWorkspaceAIRuntimeProfile(profileForm.id, payload)
    } else {
      await createWorkspaceAIRuntimeProfile(payload)
    }
    profileDialogVisible.value = false
    await loadBaseData()
    ElMessage.success('运行策略已保存')
  } catch (error) {
    ElMessage.error('保存运行策略失败')
  } finally {
    profileSaving.value = false
  }
}

const removeProfile = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除运行策略 ${row.name} 吗？`, '删除运行策略', { type: 'warning' })
    await deleteWorkspaceAIRuntimeProfile(row.id)
    await loadBaseData()
    ElMessage.success('运行策略已删除')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除运行策略失败')
    }
  }
}

function formatJsonText(value, fallback) {
  const parsed = parseSerializedJson(value)
  return JSON.stringify(parsed ?? fallback, null, 2)
}

function extractList(payload) {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.agents)) return payload.agents
  if (Array.isArray(payload?.models)) return payload.models
  if (Array.isArray(payload?.runtimeProfiles)) return payload.runtimeProfiles
  if (Array.isArray(payload?.runtime_profiles)) return payload.runtime_profiles
  return []
}

function runtimeProfileRelationId(record) {
  return record?.runtime_profile_id ?? record?.runtimeProfileID ?? record?.runtime_profile?.id ?? null
}

function runtimeProfileModelId(record) {
  return record?.model_id ?? record?.modelID ?? record?.model?.id ?? null
}

function normalizeNullableId(value) {
  const normalized = Number(value)
  return Number.isFinite(normalized) && normalized > 0 ? normalized : undefined
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

function isPlainObject(value) {
  return value != null && typeof value === 'object' && !Array.isArray(value)
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
  await loadBaseData()
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