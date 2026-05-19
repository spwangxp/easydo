<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">AI Agent</h2>
        <div class="section-subtitle">管理当前工作区下的 AI Agent 定义。</div>
      </div>
      <el-button v-if="canManageAI" type="primary" @click="openAgentDialog()">新建 AI Agent</el-button>
    </div>

    <div v-if="!userStore.currentWorkspaceId" class="empty-hint">请先在顶部切换到一个工作空间</div>

    <el-table v-else :data="agents" style="width: 100%">
      <el-table-column prop="name" label="AI Agent" min-width="180" />
      <el-table-column prop="scenario" label="场景" width="180" />
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column label="运行策略数" width="140">
        <template #default="{ row }">
          {{ Array.isArray(row.runtime_profiles) ? row.runtime_profiles.length : 0 }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <el-button v-if="canManageAI" link type="primary" @click="openAgentDialog(row)">编辑</el-button>
          <el-button v-if="canManageAI" link type="danger" @click="removeAgent(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="agentDialogVisible" :title="agentForm.id ? '编辑 AI Agent' : '新建 AI Agent'" width="560px">
      <el-form :model="agentForm" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="agentForm.name" />
        </el-form-item>
        <el-form-item label="场景">
          <el-select v-model="agentForm.scenario" style="width: 100%">
            <el-option label="MR 质量检测" value="mr_quality_check" />
            <el-option label="需求缺陷助手" value="requirement_defect_assistant" />
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
  updateWorkspaceAIAgent
} from '@/api/agent'

const userStore = useUserStore()
const agents = ref([])
const agentDialogVisible = ref(false)
const agentSaving = ref(false)
const canManageAI = computed(() => userStore.canAccessWorkspaceGovernance)
const agentForm = reactive({
  id: 0,
  name: '',
  description: '',
  scenario: 'mr_quality_check',
  status: 'draft'
})

const resetAgentForm = () => {
  agentForm.id = 0
  agentForm.name = ''
  agentForm.description = ''
  agentForm.scenario = 'mr_quality_check'
  agentForm.status = 'draft'
}

const loadAgents = async () => {
  if (!userStore.currentWorkspaceId) {
    agents.value = []
    return
  }
  try {
    const res = await getWorkspaceAIAgents()
    agents.value = Array.isArray(res?.data) ? res.data : []
  } catch (error) {
    ElMessage.error('加载 AI Agent 失败')
  }
}

const openAgentDialog = (row = null) => {
  resetAgentForm()
  if (row) {
    agentForm.id = row.id
    agentForm.name = row.name || ''
    agentForm.description = row.description || ''
    agentForm.scenario = row.scenario || 'mr_quality_check'
    agentForm.status = row.status || 'draft'
  }
  agentDialogVisible.value = true
}

const submitAgent = async () => {
  agentSaving.value = true
  try {
    const payload = {
      name: agentForm.name,
      description: agentForm.description,
      scenario: agentForm.scenario,
      status: agentForm.status
    }
    if (agentForm.id) {
      await updateWorkspaceAIAgent(agentForm.id, payload)
    } else {
      await createWorkspaceAIAgent(payload)
    }
    agentDialogVisible.value = false
    await loadAgents()
    ElMessage.success('AI Agent 已保存')
  } catch (error) {
    ElMessage.error('保存 AI Agent 失败')
  } finally {
    agentSaving.value = false
  }
}

const removeAgent = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除 AI Agent ${row.name} 吗？`, '删除 AI Agent', { type: 'warning' })
    await deleteWorkspaceAIAgent(row.id)
    await loadAgents()
    ElMessage.success('AI Agent 已删除')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error('删除 AI Agent 失败')
    }
  }
}

watch(() => userStore.currentWorkspaceId, async () => {
  await loadAgents()
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
