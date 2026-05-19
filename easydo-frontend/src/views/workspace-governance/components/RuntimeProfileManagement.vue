<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">运行策略</h2>
        <div class="section-subtitle">按 AI Agent 维度管理运行策略，并保留删除确认。</div>
      </div>
      <div class="header-actions">
        <el-select v-model="activeAgentId" class="agent-select" placeholder="选择 AI Agent" clearable>
          <el-option v-for="agent in agents" :key="agent.id" :label="agent.name" :value="agent.id" />
        </el-select>
        <el-button v-if="canManageAI" type="primary" :disabled="!activeAgentId" @click="openProfileDialog()">新增运行策略</el-button>
      </div>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>
    <div v-else-if="!agents.length" class="empty-hint">当前工作区还没有 AI Agent，请先在上一个标签页创建。</div>
    <div v-else-if="!activeAgentId" class="empty-hint">请选择一个 AI Agent 查看运行策略。</div>

    <el-table v-else :data="runtimeProfiles" style="width: 100%">
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column label="模型" width="180">
        <template #default="{ row }">{{ modelName(row.model_id) }}</template>
      </el-table-column>
      <el-table-column prop="fallback_enabled" label="允许降级" width="120">
        <template #default="{ row }">
          <el-tag :type="row.fallback_enabled ? 'success' : 'info'" size="small">{{ row.fallback_enabled ? '是' : '否' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column prop="binding_priority_json" label="模型绑定顺序" min-width="260" show-overflow-tooltip />
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
const activeAgentId = ref(undefined)
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
    activeAgentId.value = undefined
    return
  }
  try {
    const [agentsRes, modelsRes] = await Promise.all([
      getWorkspaceAIAgents(),
      getWorkspaceAIModelCatalog()
    ])
    agents.value = Array.isArray(agentsRes?.data) ? agentsRes.data : []
    models.value = Array.isArray(modelsRes?.data) ? modelsRes.data : []
    if (!agents.value.some(agent => Number(agent.id) === Number(activeAgentId.value))) {
      activeAgentId.value = agents.value[0]?.id
    }
    if (!activeAgentId.value) {
      runtimeProfiles.value = []
      return
    }
    await loadRuntimeProfiles(activeAgentId.value)
  } catch (error) {
    ElMessage.error('加载运行策略数据失败')
  }
}

const loadRuntimeProfiles = async (agentId) => {
  if (!agentId) {
    runtimeProfiles.value = []
    return
  }
  try {
    const res = await getWorkspaceAIRuntimeProfiles(agentId)
    runtimeProfiles.value = Array.isArray(res?.data) ? res.data : []
  } catch (error) {
    ElMessage.error('加载运行策略失败')
  }
}

const modelName = (modelId) => {
  const model = models.value.find(item => Number(item.id) === Number(modelId))
  return model?.display_name || model?.name || modelId || '-'
}

const openProfileDialog = (row = null) => {
  resetProfileForm()
  if (row) {
    profileForm.id = row.id
    profileForm.name = row.name || ''
    profileForm.model_id = row.model_id || undefined
    profileForm.binding_priority_json = row.binding_priority_json || '[]'
    profileForm.runtime_settings_json = row.runtime_settings_json || '{}'
    profileForm.fallback_enabled = Boolean(row.fallback_enabled)
    profileForm.status = row.status || 'draft'
  }
  profileDialogVisible.value = true
}

const submitProfile = async () => {
  if (!activeAgentId.value) {
    return
  }
  profileSaving.value = true
  try {
    const payload = {
      name: profileForm.name,
      model_id: profileForm.model_id,
      binding_priority_json: JSON.parse(profileForm.binding_priority_json || '[]'),
      runtime_settings_json: JSON.parse(profileForm.runtime_settings_json || '{}'),
      fallback_enabled: profileForm.fallback_enabled,
      status: profileForm.status
    }
    if (profileForm.id) {
      await updateWorkspaceAIRuntimeProfile(activeAgentId.value, profileForm.id, payload)
    } else {
      await createWorkspaceAIRuntimeProfile(activeAgentId.value, payload)
    }
    profileDialogVisible.value = false
    await loadRuntimeProfiles(activeAgentId.value)
    ElMessage.success('运行策略已保存')
  } catch (error) {
    ElMessage.error('保存运行策略失败')
  } finally {
    profileSaving.value = false
  }
}

const removeProfile = async (row) => {
  if (!activeAgentId.value) {
    return
  }
  try {
    await ElMessageBox.confirm(`确认删除运行策略 ${row.name} 吗？`, '删除运行策略', { type: 'warning' })
    await deleteWorkspaceAIRuntimeProfile(activeAgentId.value, row.id)
    await loadRuntimeProfiles(activeAgentId.value)
    ElMessage.success('运行策略已删除')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除运行策略失败')
    }
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadBaseData()
}, { immediate: true })

watch(activeAgentId, async (value, previousValue) => {
  if (value === previousValue) {
    return
  }
  await loadRuntimeProfiles(value)
})
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

.header-actions {
  display: flex;
  gap: 12px;
  align-items: center;
}

.agent-select {
  width: 240px;
}

.empty-hint {
  padding: 24px;
  background: var(--bg-card);
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-align: center;
}
</style>
