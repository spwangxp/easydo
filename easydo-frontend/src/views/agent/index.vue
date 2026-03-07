<template>
  <div class="agent-container">
    <div class="agent-header">
      <h1 class="page-title">执行器管理</h1>
    </div>

    <div class="agent-filters">
      <div class="filter-tabs">
        <div
          class="tab-item"
          :class="{ active: activeTab === 'all' }"
          @click="activeTab = 'all'; fetchAgents()"
        >
          <span>全部</span>
          <span class="tab-count">{{ total }}</span>
        </div>
        <div
          class="tab-item"
          :class="{ active: activeTab === 'pending' }"
          @click="activeTab = 'pending'; fetchAgents()"
        >
          <span>待接纳</span>
          <span class="tab-count">{{ pendingCount }}</span>
        </div>
        <div
          class="tab-item"
          :class="{ active: activeTab === 'online' }"
          @click="activeTab = 'online'; fetchAgents()"
        >
          <span>在线</span>
          <span class="tab-count">{{ onlineCount }}</span>
        </div>
        <div
          class="tab-item"
          :class="{ active: activeTab === 'offline' }"
          @click="activeTab = 'offline'; fetchAgents()"
        >
          <span>离线</span>
          <span class="tab-count">{{ offlineCount }}</span>
        </div>
      </div>

      <div class="filter-search">
        <el-input
          v-model="searchKeyword"
          placeholder="搜索名称"
          prefix-icon="Search"
          clearable
          style="width: 240px"
          @input="handleSearch"
        />
      </div>
    </div>

    <div class="agent-table" v-loading="loading">
      <el-table :data="agentList" style="width: 100%" row-key="id">
        <el-table-column prop="name" label="名称" min-width="200">
          <template #default="{ row }">
            <div class="agent-name-cell">
              <div class="agent-icon" :style="{ background: getStatusColor(row.status) }">
                <el-icon><Monitor /></el-icon>
              </div>
              <div class="agent-info">
                <span class="agent-name-text">{{ row.name }}</span>
                <span class="agent-host">{{ row.host }}:{{ row.port }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)" size="small">
              {{ getStatusText(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="审批状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="getRegistrationStatusType(row.registration_status)" size="small">
              {{ getRegistrationStatusText(row.registration_status) }}
            </el-tag>
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
        <el-table-column label="最后心跳" width="120" align="center">
          <template #default="{ row }">
            <span>{{ formatDate(row.last_heart_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" align="center">
          <template #default="{ row }">
            <el-dropdown trigger="click" @command="(cmd) => handleCommand(cmd, row)">
              <el-icon class="action-icon more-icon"><MoreFilled /></el-icon>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="detail">详情</el-dropdown-item>
                  <el-dropdown-item command="edit">编辑</el-dropdown-item>
                  <!-- 待接纳状态显示接纳/拒绝选项 -->
                  <template v-if="row.registration_status === 'pending'">
                    <el-dropdown-item 
                      :command="'approve'" 
                      :disabled="row.status !== 'online'"
                      divided
                    >
                      <el-tooltip :content="row.status !== 'online' ? 'Agent离线，无法批准。请等待Agent上线后再试。' : ''" placement="top" :disabled="row.status === 'online'">
                        <span>
                          <el-icon><CircleCheck /></el-icon>
                          接纳
                        </span>
                      </el-tooltip>
                    </el-dropdown-item>
                    <el-dropdown-item command="reject">
                      <el-icon><CircleClose /></el-icon>
                      拒绝
                    </el-dropdown-item>
                  </template>
                  <!-- 已接纳状态显示移除选项 -->
                  <template v-else-if="row.registration_status === 'approved'">
                    <el-dropdown-item command="remove" divided>
                      <el-icon><CircleClose /></el-icon>
                      移除
                    </el-dropdown-item>
                  </template>
                  <el-dropdown-item command="heartbeats" divided>心跳记录</el-dropdown-item>
                  <el-dropdown-item command="delete" divided>删除</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
        </el-table-column>
      </el-table>

      <el-empty v-if="!loading && agentList.length === 0" description="暂无执行器" />
    </div>

    <!-- 创建/编辑执行器弹窗 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑执行器' : '添加执行器'"
      width="560px"
      :close-on-click-modal="false"
    >
      <el-form
        ref="formRef"
        :model="formData"
        :rules="formRules"
        label-width="100px"
      >
        <el-form-item label="名称" prop="name">
          <el-input v-model="formData.name" placeholder="请输入执行器名称" maxlength="64" show-word-limit />
        </el-form-item>
        <el-form-item label="主机地址" prop="host">
          <el-input v-model="formData.host" placeholder="请输入主机地址" />
        </el-form-item>
        <el-form-item label="端口" prop="port">
          <el-input-number v-model="formData.port" :min="1" :max="65535" placeholder="端口号" />
        </el-form-item>
        <el-form-item label="标签" prop="labels">
          <el-input v-model="formData.labels" placeholder="JSON数组格式，如 [&#34;linux&#34;, &#34;docker&#34;]" />
          <div class="form-tip">多个标签用逗号分隔，支持 JSON 数组格式</div>
        </el-form-item>
        <el-form-item label="备注" prop="tags">
          <el-input
            v-model="formData.tags"
            type="textarea"
            placeholder="JSON对象格式，如 {&#34;env&#34;: &#34;prod&#34;}"
            :rows="2"
          />
        </el-form-item>
        <el-form-item label="状态" prop="status">
          <el-select v-model="formData.status" placeholder="选择状态">
            <el-option label="在线" value="online" />
            <el-option label="离线" value="offline" />
            <el-option label="忙碌" value="busy" />
            <el-option label="错误" value="error" />
          </el-select>
        </el-form-item>
        <el-form-item label="心跳周期" prop="heartbeat_interval">
          <el-input-number v-model="formData.heartbeat_interval" :min="5" :max="300" placeholder="秒" />
          <span class="form-tip">单位：秒，建议 10-60 秒</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">
          确定
        </el-button>
      </template>
    </el-dialog>

    <!-- 详情弹窗 -->
    <el-dialog
      v-model="detailDialogVisible"
      title="执行器详情"
      width="640px"
    >
      <div class="agent-detail" v-if="currentAgent">
        <div class="detail-section">
          <div class="section-title">基本信息</div>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="ID">{{ currentAgent.id }}</el-descriptions-item>
            <el-descriptions-item label="名称">{{ currentAgent.name }}</el-descriptions-item>
            <el-descriptions-item label="主机">{{ currentAgent.host }}:{{ currentAgent.port }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="getStatusType(currentAgent.status)" size="small">
                {{ getStatusText(currentAgent.status) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="标签" :span="2">
              <div class="agent-tags" v-if="currentAgent.labels">
                <el-tag
                  v-for="(label, idx) in parseLabels(currentAgent.labels)"
                  :key="idx"
                  size="small"
                  type="info"
                  class="agent-tag"
                >
                  {{ label }}
                </el-tag>
              </div>
              <span v-else>-</span>
            </el-descriptions-item>
            <el-descriptions-item label="备注" :span="2">
              {{ currentAgent.tags || '-' }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
        <div class="detail-section">
          <div class="section-title">系统信息</div>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="操作系统">{{ currentAgent.os }} {{ currentAgent.arch }}</el-descriptions-item>
            <el-descriptions-item label="主机名">{{ currentAgent.hostname || '-' }}</el-descriptions-item>
            <el-descriptions-item label="IP地址">{{ currentAgent.ip_address || '-' }}</el-descriptions-item>
            <el-descriptions-item label="CPU">{{ currentAgent.cpu_cores }} 核</el-descriptions-item>
            <el-descriptions-item label="内存">{{ formatMemory(currentAgent.memory_total) }}</el-descriptions-item>
            <el-descriptions-item label="磁盘">{{ formatMemory(currentAgent.disk_total) }}</el-descriptions-item>
          </el-descriptions>
        </div>
        <div class="detail-section">
          <div class="section-title">运行信息</div>
          <el-descriptions :column="2" border>
            <el-descriptions-item label="版本">{{ currentAgent.version || '-' }}</el-descriptions-item>
            <el-descriptions-item label="最后心跳">{{ formatDateTime(currentAgent.last_heart_at) }}</el-descriptions-item>
          </el-descriptions>
        </div>
        <div class="detail-section" v-if="currentAgent.registration_status === 'approved'">
          <div class="section-title">Token信息</div>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="Token">
              <el-input
                v-model="currentAgent.token"
                readonly
                size="small"
                style="width: 400px"
              >
                <template #append>
                  <el-button @click="copyToken(currentAgent.token)">复制</el-button>
                </template>
              </el-input>
            </el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag v-if="currentAgent.token" type="success" size="small">已配置</el-tag>
              <el-tag v-else type="warning" size="small">未配置</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="操作">
              <el-button type="warning" size="small" @click="handleRefreshToken">
                <el-icon><Refresh /></el-icon>
                刷新Token
              </el-button>
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </div>
      <template #footer>
        <el-button @click="detailDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>

    <!-- 新Token弹窗 -->
    <el-dialog
      v-model="refreshTokenDialogVisible"
      title="新Token"
      width="480px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="false"
    >
      <div class="new-token-content">
        <el-alert
          title="Token已刷新成功"
          type="success"
          :closable="false"
          show-icon
          style="margin-bottom: 16px"
        />
        <p class="token-tip">请立即复制新Token并配置到执行器，刷新页面后将无法查看此Token。</p>
        <el-input
          v-model="newToken"
          readonly
          size="large"
        >
          <template #append>
            <el-button @click="copyToken(newToken)">复制</el-button>
          </template>
        </el-input>
      </div>
      <template #footer>
        <el-button type="primary" @click="refreshTokenDialogVisible = false">我已复制</el-button>
      </template>
    </el-dialog>

    <!-- 删除确认对话框 -->
    <el-dialog
      v-model="deleteDialogVisible"
      title="确认删除"
      width="400px"
      :close-on-click-modal="false"
    >
      <div class="delete-warning">
        <el-icon color="var(--warning-color)" size="24"><Warning /></el-icon>
        <p>确定要删除执行器 <strong>{{ deleteForm.name }}</strong> 吗？</p>
        <p class="delete-tip">此操作不可恢复，请输入执行器名称以确认。</p>
      </div>
      <el-form :model="deleteForm" label-width="0">
        <el-form-item>
          <el-input
            v-model="deleteForm.confirmName"
            :placeholder="`请输入 '${deleteForm.name}' 以确认`"
          />
        </el-form-item>
      </el-form>
      
      <template #footer>
        <el-button @click="deleteDialogVisible = false">取消</el-button>
        <el-button
          type="danger"
          :loading="deleteLoading"
          :disabled="deleteForm.confirmName !== deleteForm.name"
          @click="handleDeleteConfirm"
        >
          删除
        </el-button>
      </template>
    </el-dialog>

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
          <el-descriptions-item label="操作系统">{{ currentAgent.os }} {{ currentAgent.arch }}</el-descriptions-item>
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

    <!-- 移除确认弹窗 -->
    <el-dialog
      v-model="removeDialogVisible"
      title="移除执行器"
      width="400px"
      :close-on-click-modal="false"
    >
      <div class="remove-warning">
        <el-icon color="var(--warning-color)" size="24"><Warning /></el-icon>
        <p>确定要移除执行器 <strong>{{ currentAgent?.name }}</strong> 吗？</p>
        <p class="remove-tip">移除后该执行器将被注销，需要重新注册并审批才能使用。</p>
      </div>
      <template #footer>
        <el-button @click="removeDialogVisible = false">取消</el-button>
        <el-button
          type="warning"
          :loading="removeLoading"
          @click="handleRemoveConfirm"
        >
          确认移除
        </el-button>
      </template>
    </el-dialog>

    <!-- 心跳记录弹窗 -->
    <el-dialog
      v-model="heartbeatDialogVisible"
      title="心跳记录"
      width="800px"
    >
      <div class="heartbeat-chart" v-loading="heartbeatLoading">
        <el-table :data="heartbeatList" height="400" style="width: 100%">
          <el-table-column prop="timestamp" label="时间" width="180">
            <template #default="{ row }">
              {{ formatDateTime(row.timestamp) }}
            </template>
          </el-table-column>
          <el-table-column label="CPU" width="100" align="center">
            <template #default="{ row }">
              <span :class="row.cpu_usage > 80 ? 'text-danger' : ''">
                {{ row.cpu_usage?.toFixed(1) }}%
              </span>
            </template>
          </el-table-column>
          <el-table-column label="内存" width="100" align="center">
            <template #default="{ row }">
              <span :class="row.memory_usage > 80 ? 'text-danger' : ''">
                {{ row.memory_usage?.toFixed(1) }}%
              </span>
            </template>
          </el-table-column>
          <el-table-column label="磁盘" width="100" align="center">
            <template #default="{ row }">
              {{ row.disk_usage?.toFixed(1) }}%
            </template>
          </el-table-column>
          <el-table-column label="负载" width="120" align="center">
            <template #default="{ row }">
              {{ row.load_avg || '-' }}
            </template>
          </el-table-column>
          <el-table-column label="运行任务" width="100" align="center">
            <template #default="{ row }">
              {{ row.tasks_running || 0 }}
            </template>
          </el-table-column>
        </el-table>
      </div>
      <template #footer>
        <el-button type="primary" :loading="heartbeatLoading" @click="refreshHeartbeats">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Search,
  MoreFilled,
  Warning,
  Monitor,
  Cpu,
  Grid,
  Refresh,
  CircleCheck,
  CircleClose
} from '@element-plus/icons-vue'
import { getAgentList, getAgentDetail, updateAgent, deleteAgent, getAgentHeartbeats, refreshAgentToken, approveAgent, rejectAgent, removeAgent } from '@/api/agent'

const activeTab = ref('all')
const searchKeyword = ref('')
const total = ref(0)
const pendingCount = ref(0)
const onlineCount = ref(0)
const offlineCount = ref(0)
const loading = ref(false)
const agentList = ref([])

const dialogVisible = ref(false)
const isEdit = ref(false)
const submitting = ref(false)
const currentAgentId = ref(null)
const formRef = ref(null)

const detailDialogVisible = ref(false)
const currentAgent = ref(null)
const newToken = ref('')
const refreshTokenDialogVisible = ref(false)

const deleteDialogVisible = ref(false)
const deleteLoading = ref(false)
const deleteForm = reactive({
  id: null,
  name: '',
  confirmName: ''
})

const approveDialogVisible = ref(false)
const approveLoading = ref(false)
const approveForm = reactive({
  remark: ''
})

const rejectDialogVisible = ref(false)
const rejectLoading = ref(false)
const rejectForm = reactive({
  remark: ''
})

const removeDialogVisible = ref(false)
const removeLoading = ref(false)

const heartbeatDialogVisible = ref(false)
const heartbeatLoading = ref(false)
const heartbeatList = ref([])

const formData = reactive({
  name: '',
  host: '',
  port: 8080,
  labels: '',
  tags: '',
  status: 'online',
  heartbeat_interval: 10
})

const formRules = {
  name: [
    { required: true, message: '请输入执行器名称', trigger: 'blur' },
    { min: 2, max: 64, message: '名称长度为2-64个字符', trigger: 'blur' }
  ],
  host: [
    { required: true, message: '请输入主机地址', trigger: 'blur' }
  ],
  port: [
    { required: true, message: '请输入端口', trigger: 'blur' }
  ]
}

let searchTimer = null
let pollingTimer = null

// Polling interval for agent status (milliseconds)
const POLLING_INTERVAL = 5000

const getStatusColor = (status) => {
  const colors = {
    online: '#67C23A',
    offline: 'var(--text-muted)',
    busy: '#E6A23C',
    error: '#F56C6C'
  }
  return colors[status] || 'var(--text-muted)'
}

const getStatusType = (status) => {
  const types = {
    online: 'success',
    offline: 'info',
    busy: 'warning',
    error: 'danger'
  }
  return types[status] || 'info'
}

const getStatusText = (status) => {
  const texts = {
    online: '在线',
    offline: '离线',
    busy: '忙碌',
    error: '错误'
  }
  return texts[status] || '未知'
}

const getRegistrationStatusType = (status) => {
  const types = {
    pending: 'warning',
    approved: 'success',
    rejected: 'danger'
  }
  return types[status] || 'info'
}

const getRegistrationStatusText = (status) => {
  const texts = {
    pending: '待接纳',
    approved: '已接纳',
    rejected: '已拒绝'
  }
  return texts[status] || '未知'
}

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

const formatDate = (timestamp) => {
  if (!timestamp) return '-'
  // 支持 Unix 时间戳（秒）或 ISO 8601 字符串
  let date
  if (typeof timestamp === 'number') {
    date = new Date(timestamp * 1000)
  } else {
    date = new Date(timestamp)
  }
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const formatDateTime = (timestamp) => {
  if (!timestamp || timestamp === 0) return '-'
  // Support Unix timestamp (seconds) or ISO 8601 string
  let date
  if (typeof timestamp === 'number') {
    // Ensure timestamp is positive
    if (timestamp < 0) return '-'
    date = new Date(timestamp * 1000)
  } else {
    date = new Date(timestamp)
  }
  // Check if date is valid
  if (isNaN(date.getTime())) return '-'
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  const second = String(date.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day} ${hour}:${minute}:${second}`
}

const fetchAgents = async () => {
  loading.value = true
  try {
    const params = {
      page: 1,
      page_size: 100
    }

    if (searchKeyword.value) {
      params.keyword = searchKeyword.value
    }

    if (activeTab.value === 'online') {
      params.status = 'online'
    } else if (activeTab.value === 'offline') {
      params.status = 'offline'
    } else if (activeTab.value === 'pending') {
      params.registration_status = 'pending'
    }

    const res = await getAgentList(params)
    if (res.code === 200) {
      agentList.value = res.data.list || []
      total.value = res.data.total || 0
      
      onlineCount.value = agentList.value.filter(a => a.status === 'online').length
      offlineCount.value = agentList.value.filter(a => a.status === 'offline').length
      pendingCount.value = agentList.value.filter(a => a.registration_status === 'pending').length
    }
  } catch (error) {
    console.error('获取执行器列表失败:', error)
    ElMessage.error('获取执行器列表失败')
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    fetchAgents()
  }, 300)
}

const handleEdit = (agent) => {
  isEdit.value = true
  currentAgentId.value = agent.id
  formData.name = agent.name
  formData.host = agent.host
  formData.port = agent.port
  formData.labels = agent.labels || ''
  formData.tags = agent.tags || ''
  formData.status = agent.status
  formData.heartbeat_interval = agent.heartbeat_interval || 10
  dialogVisible.value = true
}

const handleDetail = async (agent) => {
  try {
    const res = await getAgentDetail(agent.id)
    if (res.code === 200) {
      currentAgent.value = res.data
      detailDialogVisible.value = true
    } else {
      ElMessage.error(res.message || '获取详情失败')
    }
  } catch (error) {
    console.error('获取执行器详情失败:', error)
    ElMessage.error('获取详情失败')
  }
}

const handleDelete = (agent) => {
  deleteForm.id = agent.id
  deleteForm.name = agent.name
  deleteForm.confirmName = ''
  deleteDialogVisible.value = true
}

const handleApprove = (agent) => {
  // Check if agent is online before approval
  if (agent.status !== 'online') {
    ElMessage.warning('Agent离线，无法批准。请等待Agent上线后再试。')
    return
  }
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
      fetchAgents()
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
      fetchAgents()
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

const handleRemove = (agent) => {
  currentAgent.value = agent
  removeDialogVisible.value = true
}

const handleRemoveConfirm = async () => {
  removeLoading.value = true
  try {
    const res = await removeAgent(currentAgent.value.id)
    if (res.code === 200) {
      ElMessage.success('移除成功')
      removeDialogVisible.value = false
      fetchAgents()
    } else {
      ElMessage.error(res.message || '移除失败')
    }
  } catch (error) {
    console.error('移除失败:', error)
    ElMessage.error('移除失败')
  } finally {
    removeLoading.value = false
  }
}

const handleDeleteConfirm = async () => {
  if (deleteForm.confirmName !== deleteForm.name) {
    ElMessage.error('名称不匹配')
    return
  }
  
  deleteLoading.value = true
  try {
    const res = await deleteAgent(deleteForm.id)
    if (res.code === 200) {
      ElMessage.success('删除成功')
      deleteDialogVisible.value = false
      fetchAgents()
    } else {
      ElMessage.error(res.message || '删除失败')
    }
  } catch (error) {
    console.error('删除执行器失败:', error)
    ElMessage.error('删除失败')
  } finally {
    deleteLoading.value = false
  }
}

const handleHeartbeats = async (agent) => {
  currentAgent.value = agent
  heartbeatDialogVisible.value = true
  heartbeatLoading.value = true
  heartbeatList.value = []
  
  try {
    const res = await getAgentHeartbeats(agent.id, { page: 1, page_size: 100 })
    if (res.code === 200) {
      heartbeatList.value = res.data.list || []
    }
  } catch (error) {
    console.error('获取心跳记录失败:', error)
    ElMessage.error('获取心跳记录失败')
  } finally {
    heartbeatLoading.value = false
  }
}

const refreshHeartbeats = async () => {
  if (!currentAgent.value) return
  
  heartbeatLoading.value = true
  heartbeatList.value = []
  
  try {
    const res = await getAgentHeartbeats(currentAgent.value.id, { page: 1, page_size: 100 })
    if (res.code === 200) {
      heartbeatList.value = res.data.list || []
    }
  } catch (error) {
    console.error('刷新心跳记录失败:', error)
    ElMessage.error('刷新心跳记录失败')
  } finally {
    heartbeatLoading.value = false
  }
}

const handleCommand = (command, agent) => {
  switch (command) {
    case 'detail':
      handleDetail(agent)
      break
    case 'edit':
      handleEdit(agent)
      break
    case 'heartbeats':
      handleHeartbeats(agent)
      break
    case 'approve':
      handleApprove(agent)
      break
    case 'reject':
      handleReject(agent)
      break
    case 'remove':
      handleRemove(agent)
      break
    case 'delete':
      handleDelete(agent)
      break
  }
}

const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    submitting.value = true

    const data = {
      name: formData.name,
      labels: formData.labels,
      tags: formData.tags,
      status: formData.status,
      heartbeat_interval: formData.heartbeat_interval
    }

    if (isEdit.value) {
      const res = await updateAgent(currentAgentId.value, data)
      if (res.code === 200) {
        ElMessage.success('更新成功')
        dialogVisible.value = false
        fetchAgents()
      } else {
        ElMessage.error(res.message || '更新失败')
      }
    } else {
      ElMessage.info('请通过执行器配置文件注册新执行器')
      dialogVisible.value = false
    }
  } catch (error) {
    console.error('提交失败:', error)
  } finally {
    submitting.value = false
  }
}

const copyToken = (token) => {
  if (!token) {
    ElMessage.warning('暂无Token')
    return
  }
  navigator.clipboard.writeText(token).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败，请手动复制')
  })
}

const handleRefreshToken = async () => {
  if (!currentAgent.value) return

  try {
    await ElMessageBox.confirm(
      '刷新Token将使当前Token立即失效，执行器需要重新配置才能继续工作。确定要刷新Token吗？',
      '刷新Token确认',
      {
        confirmButtonText: '确定刷新',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const res = await refreshAgentToken(currentAgent.value.id)
    if (res.code === 200 && res.data) {
      newToken.value = res.data.token
      refreshTokenDialogVisible.value = true
      detailDialogVisible.value = false
    } else {
      ElMessage.error(res.message || '刷新Token失败')
    }
  } catch (error) {
    if (error !== 'cancel') {
      console.error('刷新Token失败:', error)
      ElMessage.error('刷新Token失败')
    }
  }
}

onMounted(() => {
  fetchAgents()
  // Start polling for agent status updates
  pollingTimer = setInterval(() => {
    fetchAgents()
  }, POLLING_INTERVAL)
})

onUnmounted(() => {
  // Clean up polling timer
  if (pollingTimer) {
    clearInterval(pollingTimer)
    pollingTimer = null
  }
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.agent-container {
  .agent-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 28px;

    .page-title {
      font-family: $font-family-display;
      font-size: 28px;
      font-weight: 700;
      color: var(--text-primary);
      letter-spacing: -0.02em;
    }
  }

  .agent-filters {
    display: flex;
    align-items: center;
    gap: 20px;
    margin-bottom: 24px;
    padding: 14px 20px;
    background: var(--bg-card);
    border-radius: $radius-xl;
    box-shadow: $shadow-sm;

    .filter-tabs {
      display: flex;
      align-items: center;
      gap: 6px;

      .tab-item {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 8px 16px;
        color: var(--text-secondary);
        cursor: pointer;
        border-radius: $radius-full;
        transition: all $transition-base;
        font-weight: 500;
        font-size: 14px;

        &:hover {
          background: var(--primary-lighter);
          color: var(--primary-color);
        }

        &.active {
          color: var(--primary-color);
          background: var(--primary-light);
          box-shadow: inset 0 0 0 1px var(--border-color-hover);
        }

        .tab-count {
          font-size: 12px;
          color: var(--text-muted);
          background: var(--bg-secondary);
          padding: 2px 8px;
          border-radius: $radius-full;
        }
      }
    }

    .filter-search {
      flex: 1;
      
      :deep(.el-input__wrapper) {
        background: var(--bg-secondary);
        border-radius: $radius-md;
        box-shadow: $shadow-inset;
        border: 1px solid var(--border-color-light);
        
        &:hover, &.is-focus {
          border-color: var(--border-color-hover);
        }
      }
    }
  }

  .agent-table {
    background: var(--bg-card);
    border-radius: $radius-xl;
    padding: 20px;
    box-shadow: $shadow-md;

    :deep(.el-table) {
      background: transparent;

      th.el-table__cell {
        background: var(--bg-secondary);
        color: var(--text-secondary);
        font-weight: 600;
        font-size: 13px;
        height: 50px;
        border-bottom: 1px solid var(--border-color);
      }

      td.el-table__cell {
        height: 56px;
        color: var(--text-primary);
        border-bottom: 1px solid var(--border-color-light);
      }
      
      .el-table__row:hover > td.el-table__cell {
        background: var(--primary-lighter);
      }
      
      .el-tag {
        border-radius: $radius-full;
        padding: 4px 12px;
        font-weight: 500;
        border: none;
        
        &.el-tag--success {
          background: var(--success-light);
          color: var(--success-color);
        }
        
        &.el-tag--info {
          background: var(--info-light);
          color: var(--info-color);
        }
        
        &.el-tag--warning {
          background: var(--warning-light);
          color: var(--warning-color);
        }
        
        &.el-tag--danger {
          background: var(--danger-light);
          color: var(--danger-color);
        }
      }
    }

    .agent-name-cell {
      display: flex;
      align-items: center;
      gap: 12px;

      .agent-icon {
        width: 44px;
        height: 44px;
        display: flex;
        align-items: center;
        justify-content: center;
        color: white;
        font-size: 20px;
        border-radius: $radius-md;
        flex-shrink: 0;
        box-shadow: 0 2px 8px rgba(0,0,0,0.15);
      }

      .agent-info {
        display: flex;
        flex-direction: column;

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
      }
    }

    .agent-tags {
      display: flex;
      flex-wrap: wrap;
      gap: 4px;

      .agent-tag {
        margin: 0;
      }
    }

    .resource-info {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      margin-right: 12px;
      color: var(--text-secondary);
      font-size: 13px;

      .el-icon {
        font-size: 14px;
        color: var(--primary-color);
      }
    }

    .action-cell {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;

      .action-icon {
        width: 32px;
        height: 32px;
        display: flex;
        align-items: center;
        justify-content: center;
        font-size: 16px;
        color: var(--text-secondary);
        cursor: pointer;
        border-radius: $radius-md;
        transition: all $transition-fast;

        &:hover {
          color: var(--primary-color);
          background: var(--primary-lighter);
        }

        &.more-icon {
          font-size: 18px;
        }
      }
    }
    
    .el-empty {
      padding: 60px 0;
    }
  }
}

.agent-detail {
  .detail-section {
    margin-bottom: 24px;

    &:last-child {
      margin-bottom: 0;
    }

    .section-title {
      font-family: $font-family-display;
      font-size: 16px;
      font-weight: 600;
      color: var(--text-primary);
      margin-bottom: 16px;
      padding-left: 12px;
      border-left: 3px solid var(--primary-color);
    }
  }
}

.delete-warning {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 0;
  
  p {
    margin: 16px 0 8px;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 500;
  }
  
  .delete-tip {
    color: var(--text-muted);
    font-size: 13px;
  }
}

.new-token-content {
  padding: 8px 0;

  .token-tip {
    color: var(--warning-color);
    font-size: 13px;
    margin: 0 0 16px;
    line-height: 1.5;
  }
}

.approve-info {
  margin-bottom: 10px;
}

.reject-warning {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 0;

  p {
    margin: 16px 0 8px;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 500;
  }
}

.remove-warning {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 20px 0;

  p {
    margin: 16px 0 8px;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 500;
  }

  .remove-tip {
    color: var(--text-muted);
    font-size: 13px;
  }
}

.form-tip {
  font-size: 12px;
  color: var(--text-muted);
  margin-top: 4px;
}

.text-muted {
  color: var(--text-muted);
}

.text-danger {
  color: var(--danger-color);
}

:deep(.el-dialog__body) {
  padding-bottom: 10px;
}
</style>
