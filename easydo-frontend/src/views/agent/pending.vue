<template>
  <div class="pending-container">
    <div class="pending-header">
      <h1 class="page-title">待接纳执行器</h1>
      <div class="page-subtitle">当前工作空间：{{ userStore.currentWorkspace?.name || '平台级' }}</div>
    </div>

    <div class="pending-table" v-loading="loading">
      <el-table :data="pendingList" style="width: 100%" row-key="id">
        <el-table-column prop="name" label="名称" min-width="200">
          <template #default="{ row }">
            <div class="agent-name-cell">
              <div class="agent-icon">
                <el-icon><Monitor /></el-icon>
              </div>
              <div class="agent-info">
                <span class="agent-name-text">{{ row.name }}</span>
                <span class="agent-host">{{ row.host }}:{{ row.port }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="标签" min-width="150" align="center">
          <template #default="{ row }">
            <div class="agent-tags" v-if="row.labels">
              <el-tag
                v-for="(label, idx) in parseLabels(row.labels)"
                :key="idx"
                size="small"
                type="info"
                class="agent-tag"
              >
                {{ label }}
              </el-tag>
            </div>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="系统" width="120" align="center">
          <template #default="{ row }">
            <span>{{ row.os }} {{ row.arch }}</span>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="180" align="center">
          <template #default="{ row }">
            <div class="scope-cell">
              <el-tag :type="row.scope_type === 'platform' ? 'primary' : 'info'" size="small">
                {{ row.scope_type === 'platform' ? '平台型' : '工作空间私有' }}
              </el-tag>
              <span v-if="row.scope_type !== 'platform'" class="scope-text">{{ agentWorkspaceName(row) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="资源" width="150" align="center">
          <template #default="{ row }">
            <span class="resource-info">
              <el-icon><Cpu /></el-icon>
              {{ row.cpu_cores }}C
            </span>
            <span class="resource-info">
              <el-icon><Grid /></el-icon>
              {{ formatMemory(row.memory_total) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="注册时间" width="180" align="center">
          <template #default="{ row }">
            <span>{{ formatDateTime(row.created_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" align="center">
          <template #default="{ row }">
            <el-button v-if="canApproveAgent(row)" type="primary" size="small" @click="handleApprove(row)">接纳</el-button>
            <el-button v-if="canRejectAgent(row)" type="danger" size="small" @click="handleReject(row)">拒绝</el-button>
            <span v-if="!canApproveAgent(row) && !canRejectAgent(row)" class="text-muted">仅可查看</span>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && pendingList.length === 0" description="暂无待接纳的执行器" />
    </div>

    <!-- 接纳确认弹窗 -->
    <el-dialog
      v-model="approveDialogVisible"
      title="接纳执行器"
      width="500px"
      :close-on-click-modal="false"
    >
      <div class="approve-info" v-if="currentAgent">
        <el-descriptions :column="1" border>
          <el-descriptions-item label="名称">{{ currentAgent.name }}</el-descriptions-item>
          <el-descriptions-item label="主机">{{ currentAgent.host }}:{{ currentAgent.port }}</el-descriptions-item>
          <el-descriptions-item label="类型">
            <el-tag :type="currentAgent.scope_type === 'platform' ? 'primary' : 'info'" size="small">
              {{ currentAgent.scope_type === 'platform' ? '平台型' : '工作空间私有' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="归属范围">{{ currentAgent.scope_type === 'platform' ? '平台级' : agentWorkspaceName(currentAgent) }}</el-descriptions-item>
          <el-descriptions-item label="操作系统">{{ currentAgent.os }} {{ currentAgent.arch }}</el-descriptions-item>
          <el-descriptions-item label="CPU">{{ currentAgent.cpu_cores }} 核</el-descriptions-item>
          <el-descriptions-item label="内存">{{ formatMemory(currentAgent.memory_total) }}</el-descriptions-item>
        </el-descriptions>
      </div>
      <el-form :model="approveForm" label-width="80px" style="margin-top: 20px">
        <el-form-item label="备注">
          <el-input
            v-model="approveForm.remark"
            type="textarea"
            placeholder="可选填写备注信息"
            :rows="3"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="approveDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="approveLoading" @click="handleApproveConfirm">
          确认接纳
        </el-button>
      </template>
    </el-dialog>

    <!-- 拒绝确认弹窗 -->
    <el-dialog
      v-model="rejectDialogVisible"
      title="拒绝执行器"
      width="500px"
      :close-on-click-modal="false"
    >
      <div class="reject-warning">
        <el-icon color="var(--danger-color)" size="24"><Warning /></el-icon>
        <p>确定要拒绝执行器 <strong>{{ currentAgent?.name }}</strong> 的注册申请吗？</p>
      </div>
      <el-form :model="rejectForm" label-width="80px" style="margin-top: 20px">
        <el-form-item label="拒绝原因" required>
          <el-input
            v-model="rejectForm.remark"
            type="textarea"
            placeholder="请输入拒绝原因"
            :rows="3"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rejectDialogVisible = false">取消</el-button>
        <el-button
          type="danger"
          :loading="rejectLoading"
          :disabled="!rejectForm.remark"
          @click="handleRejectConfirm"
        >
          确认拒绝
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useUserStore } from '@/stores/user'
import { Monitor, Cpu, Grid, Warning } from '@element-plus/icons-vue'
import { getPendingAgents, approveAgent, rejectAgent } from '@/api/agent'

const userStore = useUserStore()
const loading = ref(false)
const pendingList = ref([])

const approveDialogVisible = ref(false)
const rejectDialogVisible = ref(false)
const approveLoading = ref(false)
const rejectLoading = ref(false)
const currentAgent = ref(null)

const approveForm = reactive({
  remark: ''
})

const rejectForm = reactive({
  remark: ''
})

const isPlatformAdmin = computed(() => String(userStore.userInfo?.role || '').toLowerCase() === 'admin')
const isWorkspaceMaintainerLike = computed(() => ['owner', 'maintainer'].includes(String(userStore.currentWorkspace?.role || '').toLowerCase()))
const isPlatformScoped = (agent) => (agent?.scope_type || 'platform') === 'platform'
const workspaceEntryById = (workspaceId) => userStore.workspaces?.find(item => Number(item.id) === Number(workspaceId)) || null
const agentWorkspaceName = (agent) => {
  if (isPlatformScoped(agent)) {
    return '平台级'
  }
  const workspaceID = Number(agent?.workspace_id || 0)
  const workspace = workspaceEntryById(workspaceID)
  if (workspace?.name) {
    return `${workspace.name} (#${workspace.id})`
  }
  if (workspaceID > 0) {
    return `工作空间 #${workspaceID}`
  }
  return '未指定工作空间'
}
const canManageAgent = (agent) => {
  if (!agent) {
    return false
  }
  if (isPlatformScoped(agent)) {
    return isPlatformAdmin.value
  }
  if (isPlatformAdmin.value) {
    return true
  }
  return isWorkspaceMaintainerLike.value && Number(agent.workspace_id) === Number(userStore.currentWorkspaceId)
}
const canApproveAgent = (agent) => canManageAgent(agent)
const canRejectAgent = (agent) => canManageAgent(agent)
const routeRefresh = computed(() => userStore.currentWorkspaceId)

const parseLabels = (labelsStr) => {
  if (!labelsStr) return []
  try {
    const labels = JSON.parse(labelsStr)
    return Array.isArray(labels) ? labels : []
  } catch {
    return labelsStr.split(',').map(l => l.trim()).filter(l => l)
  }
}

const formatMemory = (bytes) => {
  if (!bytes) return '-'
  const gb = bytes / (1024 * 1024 * 1024)
  if (gb >= 1) return gb.toFixed(1) + 'GB'
  const mb = bytes / (1024 * 1024)
  return mb.toFixed(0) + 'MB'
}

const formatDateTime = (timestamp) => {
  if (!timestamp) return '-'
  const date = new Date(timestamp * 1000)
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day} ${hour}:${minute}`
}

const fetchPendingAgents = async () => {
  loading.value = true
  try {
    const res = await getPendingAgents({ page: 1, page_size: 100 })
    if (res.code === 200) {
      pendingList.value = res.data.list || []
    }
  } catch (error) {
    console.error('获取待接纳列表失败:', error)
    ElMessage.error('获取待接纳列表失败')
  } finally {
    loading.value = false
  }
}

const handleApprove = (agent) => {
  currentAgent.value = agent
  approveForm.remark = ''
  approveDialogVisible.value = true
}

const handleApproveConfirm = async () => {
  approveLoading.value = true
  try {
    const res = await approveAgent(currentAgent.value.id, { remark: approveForm.remark })
    if (res.code === 200) {
      ElMessage.success('接纳成功')
      approveDialogVisible.value = false
      fetchPendingAgents()
    } else {
      ElMessage.error(res.message || '接纳失败')
    }
  } catch (error) {
    console.error('接纳失败:', error)
    ElMessage.error('接纳失败')
  } finally {
    approveLoading.value = false
  }
}

const handleReject = (agent) => {
  currentAgent.value = agent
  rejectForm.remark = ''
  rejectDialogVisible.value = true
}

const handleRejectConfirm = async () => {
  if (!rejectForm.remark) {
    ElMessage.warning('请输入拒绝原因')
    return
  }
  
  rejectLoading.value = true
  try {
    const res = await rejectAgent(currentAgent.value.id, { remark: rejectForm.remark })
    if (res.code === 200) {
      ElMessage.success('已拒绝该执行器的注册申请')
      rejectDialogVisible.value = false
      fetchPendingAgents()
    } else {
      ElMessage.error(res.message || '操作失败')
    }
  } catch (error) {
    console.error('拒绝失败:', error)
    ElMessage.error('操作失败')
  } finally {
    rejectLoading.value = false
  }
}

onMounted(() => {
  fetchPendingAgents()
})

watch(routeRefresh, () => {
  fetchPendingAgents()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.pending-container {
  animation: float-up 0.45s ease both;

  .pending-header {
    margin-bottom: 18px;

    .page-title {
      font-family: $font-family-display;
      font-size: 32px;
      font-weight: 760;
      letter-spacing: -0.03em;
      color: var(--text-primary);
    }

    .page-subtitle {
      margin-top: 8px;
      color: var(--text-secondary);
      font-size: 13px;
    }
  }

  .pending-table {
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    border-radius: $radius-xl;
    padding: 16px;
    box-shadow: var(--shadow-md);
    backdrop-filter: $blur-md;
    -webkit-backdrop-filter: $blur-md;

    :deep(.el-table) {
      background: transparent;

      th.el-table__cell {
        background: var(--bg-secondary);
        color: var(--text-secondary);
        font-weight: 600;
        height: 46px;
      }

      td.el-table__cell {
        height: 52px;
        color: var(--text-primary);
      }

      .el-table__row:hover > td.el-table__cell {
        background: var(--primary-lighter);
      }
    }
  }
}

.agent-name-cell {
  display: flex;
  align-items: center;
  gap: 12px;
}

.agent-icon {
  width: 40px;
  height: 40px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  color: #fff;
  font-size: 18px;
  background: linear-gradient(135deg, var(--primary-color), var(--primary-hover));
  box-shadow: var(--shadow-sm);
}

.agent-info {
  display: flex;
  flex-direction: column;
}

.agent-name-text {
  font-size: 14px;
  color: var(--text-primary);
  font-weight: 600;
}

.agent-host {
  font-size: 12px;
  color: var(--text-muted);
  font-family: $font-family-mono;
}

.agent-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;

  .agent-tag {
    margin: 0;
  }
}

.scope-cell {
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: center;
}

.scope-text {
  font-size: 12px;
  color: var(--text-muted);
}

.resource-info {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-right: 10px;
  color: var(--text-secondary);

  .el-icon {
    font-size: 14px;
    color: var(--primary-color);
  }
}

.approve-info {
  margin-bottom: 10px;
}

.reject-warning {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 16px 0;

  p {
    margin: 12px 0 4px;
    color: var(--text-primary);
    font-size: 14px;
  }
}

.text-muted {
  color: var(--text-muted);
}
</style>
