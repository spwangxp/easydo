<template>
  <div class="secrets-management">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-left">
        <h2>
          <el-icon class="header-icon"><Key /></el-icon>
          密钥管理
        </h2>
        <p class="header-desc">统一管理所有身份验证凭据和密钥</p>
        <p class="header-desc">当前工作空间：{{ userStore.currentWorkspace?.name || '-' }}</p>
      </div>
      <div class="header-actions">
        <el-button v-if="canWriteCredentials" type="primary" @click="showCreateDialog">
          <el-icon><Plus /></el-icon>
          新建密钥
        </el-button>
        <el-button @click="showImportDialog">
          <el-icon><Upload /></el-icon>
          导入
        </el-button>
        <el-button @click="exportSecrets">
          <el-icon><Download /></el-icon>
          导出
        </el-button>
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-cards">
      <el-card class="stat-card" shadow="hover">
        <div class="stat-content">
          <div class="stat-icon total">
            <el-icon><Key /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-value">{{ stats.total }}</div>
            <div class="stat-label">密钥总数</div>
          </div>
        </div>
      </el-card>
      
      <el-card class="stat-card" shadow="hover">
        <div class="stat-content">
          <div class="stat-icon active">
            <el-icon><CircleCheck /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-value">{{ stats.active }}</div>
            <div class="stat-label">活跃</div>
          </div>
        </div>
      </el-card>
      
      <el-card class="stat-card" shadow="hover">
        <div class="stat-content">
          <div class="stat-icon expiring">
            <el-icon><Clock /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-value">{{ stats.expiring }}</div>
            <div class="stat-label">即将过期</div>
          </div>
        </div>
      </el-card>
      
      <el-card class="stat-card" shadow="hover">
        <div class="stat-content">
          <div class="stat-icon expired">
            <el-icon><Warning /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-value">{{ stats.expired }}</div>
            <div class="stat-label">已过期</div>
          </div>
        </div>
      </el-card>
    </div>

    <!-- 筛选栏 -->
    <div class="filter-section">
      <div class="filter-left">
        <el-input
          v-model="searchQuery"
          placeholder="搜索密钥名称..."
          clearable
          class="search-input"
          @clear="handleSearch"
          @keyup.enter="handleSearch"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
        
        <el-select v-model="filterType" placeholder="类型" clearable class="filter-select">
          <el-option
            v-for="type in credentialTypes"
            :key="type.value"
            :label="type.label"
            :value="type.value"
          />
        </el-select>
        
        <el-select v-model="filterCategory" placeholder="分类" clearable class="filter-select">
          <el-option
            v-for="cat in credentialCategories"
            :key="cat.value"
            :label="cat.label"
            :value="cat.value"
          />
        </el-select>
        
        <el-select v-model="filterStatus" placeholder="状态" clearable class="filter-select">
          <el-option label="活跃" value="active" />
          <el-option label="即将过期" value="expiring" />
          <el-option label="已过期" value="expired" />
          <el-option label="已禁用" value="inactive" />
        </el-select>
      </div>
      
      <div class="filter-right">
        <el-button-group>
          <el-button :type="viewMode === 'card' ? 'primary' : ''" @click="viewMode = 'card'">
            <el-icon><Grid /></el-icon>
          </el-button>
          <el-button :type="viewMode === 'list' ? 'primary' : ''" @click="viewMode = 'list'">
            <el-icon><List /></el-icon>
          </el-button>
        </el-button-group>
      </div>
    </div>

    <!-- 批量操作栏 -->
    <div v-if="selectedIds.length > 0" class="batch-actions">
      <span class="batch-info">已选择 {{ selectedIds.length }} 项</span>
      <el-button type="primary" link size="small" @click="batchVerify">
        <el-icon><CircleCheck /></el-icon>
        批量验证
      </el-button>
      <el-button type="warning" link size="small" @click="batchExport">
        <el-icon><Download /></el-icon>
        批量导出
      </el-button>
      <el-button type="danger" link size="small" @click="batchDelete">
        <el-icon><Delete /></el-icon>
        批量删除
      </el-button>
      <el-button link size="small" @click="selectedIds = []">取消</el-button>
    </div>

    <!-- 卡片视图 -->
    <div v-if="viewMode === 'card'" v-loading="loading" class="card-grid">
      <el-card
        v-for="secret in secrets"
        :key="secret.id"
        class="secret-card"
        shadow="hover"
        :body-style="{ padding: '0px' }"
      >
        <div class="card-header">
          <div class="card-type-icon" :style="{ backgroundColor: getTypeColor(secret.type) }">
            <el-icon><component :is="getTypeIcon(secret.type)" /></el-icon>
          </div>
          <div class="card-info">
            <div class="card-name">{{ secret.name }}</div>
            <div class="card-type">{{ getTypeLabel(secret.type) }}</div>
          </div>
          <el-dropdown trigger="click" @command="handleCardCommand($event, secret)">
            <el-button link size="small" class="card-more">
              <el-icon><MoreFilled /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                 <el-dropdown-item v-if="canWriteCredentials" command="edit">
                  <el-icon><Edit /></el-icon>
                  编辑
                </el-dropdown-item>
                <el-dropdown-item command="verify">
                  <el-icon><CircleCheck /></el-icon>
                  验证
                </el-dropdown-item>
                 <el-dropdown-item v-if="canWriteCredentials" command="rotate">
                  <el-icon><Refresh /></el-icon>
                  轮换
                </el-dropdown-item>
                <el-dropdown-item command="usage">
                  <el-icon><DataLine /></el-icon>
                  使用统计
                </el-dropdown-item>
                <el-dropdown-item command="duplicate">
                  <el-icon><CopyDocument /></el-icon>
                  复制
                </el-dropdown-item>
                 <el-dropdown-item v-if="canWriteCredentials" command="delete" divided>
                  <el-icon><Delete /></el-icon>
                  删除
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
        
        <div class="card-body">
          <div class="card-category">
            <el-tag size="small">{{ getCategoryLabel(secret.category) }}</el-tag>
          </div>
          
          <div class="card-meta">
            <div class="meta-item">
              <el-icon><Clock /></el-icon>
              <span>{{ formatTime(secret.last_used_at) || '从未使用' }}</span>
            </div>
            <div class="meta-item">
              <el-icon><View /></el-icon>
              <span>使用 {{ secret.used_count }} 次</span>
            </div>
          </div>
          
          <div class="card-status">
            <el-tag :type="getStatusType(secret.status)" size="small">
              {{ getStatusLabel(secret.status) }}
            </el-tag>
            <el-tag v-if="isExpiringSoon(secret)" type="warning" size="small">
              即将过期
            </el-tag>
          </div>
        </div>
        
        <div class="card-footer">
          <el-checkbox
            :model-value="selectedIds.includes(secret.id)"
            @change="toggleSelect(secret.id)"
          >
            选中
          </el-checkbox>
          <span class="card-time">{{ formatDate(secret.updated_at) }}</span>
        </div>
      </el-card>
    </div>

    <!-- 列表视图 -->
    <el-table
      v-else
      v-loading="loading"
      :data="secrets"
      class="secret-table"
      @selection-change="handleSelectionChange"
    >
      <el-table-column type="selection" width="50" />
      
      <el-table-column prop="name" label="名称" min-width="200">
        <template #default="{ row }">
          <div class="table-name-cell">
            <div class="type-icon" :style="{ backgroundColor: getTypeColor(row.type) }">
              <el-icon><component :is="getTypeIcon(row.type)" /></el-icon>
            </div>
            <span>{{ row.name }}</span>
          </div>
        </template>
      </el-table-column>
      
      <el-table-column prop="type" label="类型" width="120">
        <template #default="{ row }">
          <el-tag :type="getTypeTagType(row.type)" size="small">
            {{ getTypeLabel(row.type) }}
          </el-tag>
        </template>
      </el-table-column>
      
      <el-table-column prop="category" label="分类" width="120">
        <template #default="{ row }">
          {{ getCategoryLabel(row.category) }}
        </template>
      </el-table-column>
      
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="getStatusType(row.status)" size="small">
            {{ getStatusLabel(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      
      <el-table-column prop="used_count" label="使用次数" width="100" align="center" />
      
      <el-table-column prop="last_used_at" label="最后使用" width="180">
        <template #default="{ row }">
          {{ formatTime(row.last_used_at) || '从未使用' }}
        </template>
      </el-table-column>
      
      <el-table-column prop="updated_at" label="更新时间" width="180">
        <template #default="{ row }">
          {{ formatDate(row.updated_at) }}
        </template>
      </el-table-column>
      
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
           <el-button v-if="canWriteCredentials" type="primary" link size="small" @click="handleEdit(row)">
            <el-icon><Edit /></el-icon>
            编辑
          </el-button>
          <el-button type="success" link size="small" @click="handleVerify(row)">
            <el-icon><CircleCheck /></el-icon>
            验证
          </el-button>
           <el-button v-if="canWriteCredentials" type="danger" link size="small" @click="handleDelete(row)">
            <el-icon><Delete /></el-icon>
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 分页 -->
    <div class="pagination-wrapper">
      <el-pagination
        v-model:current-page="pagination.page"
        v-model:page-size="pagination.size"
        :total="pagination.total"
        :page-sizes="[12, 24, 48, 96]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadSecrets"
        @current-change="loadSecrets"
      />
    </div>

    <!-- 创建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑密钥' : '新建密钥'"
      width="640px"
      destroy-on-close
      :append-to-body="false"
    >

      <SecretForm
        v-if="dialogVisible"
        :initial-data="currentSecret"
        :types="credentialTypes"
        :categories="credentialCategories"
        @submit="handleFormSubmit"
        @cancel="dialogVisible = false"
      />
    </el-dialog>

    <!-- 详情抽屉 -->
    <el-drawer
      v-model="detailDrawerVisible"
      :title="currentSecret?.name"
      direction="rtl"
      size="480px"
    >
      <template v-if="currentSecret">
        <div class="detail-section">
          <h4>基本信息</h4>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="名称">{{ currentSecret.name }}</el-descriptions-item>
            <el-descriptions-item label="类型">
              <el-tag :type="getTypeTagType(currentSecret.type)" size="small">
                {{ getTypeLabel(currentSecret.type) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="分类">
              {{ getCategoryLabel(currentSecret.category) }}
            </el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag :type="getStatusType(currentSecret.status)" size="small">
                {{ getStatusLabel(currentSecret.status) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="描述">
              {{ currentSecret.description || '暂无描述' }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
        
        <div class="detail-section">
          <h4>使用统计</h4>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="使用次数">{{ currentSecret.used_count }}</el-descriptions-item>
            <el-descriptions-item label="最后使用">
              {{ formatTime(currentSecret.last_used_at) || '从未使用' }}
            </el-descriptions-item>
            <el-descriptions-item label="版本">v{{ currentSecret.version }}</el-descriptions-item>
          </el-descriptions>
        </div>
        
        <div class="detail-section">
          <h4>时间信息</h4>
          <el-descriptions :column="1" border>
            <el-descriptions-item label="创建时间">
              {{ formatDateTime(currentSecret.created_at) }}
            </el-descriptions-item>
            <el-descriptions-item label="更新时间">
              {{ formatDateTime(currentSecret.updated_at) }}
            </el-descriptions-item>
            <el-descriptions-item label="过期时间">
              {{ currentSecret.expires_at ? formatDateTime(currentSecret.expires_at * 1000) : '永不过期' }}
            </el-descriptions-item>
          </el-descriptions>
        </div>
      </template>
    </el-drawer>

    <!-- 使用统计对话框 -->
    <el-dialog
      v-model="usageDialogVisible"
      title="使用统计"
      width="560px"
    >
      <div v-if="usageData" class="usage-stats">
        <el-row :gutter="20">
          <el-col :span="8">
            <div class="usage-stat-item">
              <div class="usage-value">{{ usageData.used_count }}</div>
              <div class="usage-label">总使用次数</div>
            </div>
          </el-col>
          <el-col :span="8">
            <div class="usage-stat-item success">
              <div class="usage-value">{{ usageData.success_count }}</div>
              <div class="usage-label">成功次数</div>
            </div>
          </el-col>
          <el-col :span="8">
            <div class="usage-stat-item danger">
              <div class="usage-value">{{ usageData.failed_count }}</div>
              <div class="usage-label">失败次数</div>
            </div>
          </el-col>
        </el-row>

        <div class="usage-chart">
          <el-progress
            type="circle"
            :percentage="Math.round(usageData.success_rate)"
            :width="160"
          >
            <template #default="{ percentage }">
              <div class="progress-text">
                <div class="progress-value">{{ percentage }}%</div>
                <div class="progress-label">成功率</div>
              </div>
            </template>
          </el-progress>
        </div>

        <div class="usage-last">
          <el-icon><Clock /></el-icon>
          最后使用：{{ formatDateTime(usageData.last_used_at * 1000) }}
        </div>
      </div>
    </el-dialog>

    <!-- 导入对话框 -->
    <el-dialog
      v-model="importDialogVisible"
      title="导入密钥"
      width="480px"
    >
      <el-upload
        class="import-upload"
        drag
        accept=".json"
        :auto-upload="false"
        :on-change="handleImportFileChange"
        :limit="1"
      >
        <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
        <div class="el-upload__text">
          拖拽 JSON 文件到此处，或 <em>点击上传</em>
        </div>
        <template #tip>
          <div class="el-upload__tip">
            支持 JSON 格式的密钥文件
          </div>
        </template>
      </el-upload>

      <div v-if="importPreview.length > 0" class="import-preview">
        <div class="preview-header">
          <span>预览 ({{ importPreview.length }} 个密钥)</span>
          <el-button type="primary" link size="small" @click="importAll">
            全部导入
          </el-button>
        </div>
        <el-table :data="importPreview" max-height="200" size="small">
          <el-table-column prop="name" label="名称" />
          <el-table-column prop="type" label="类型" width="100" />
          <el-table-column prop="category" label="分类" width="100" />
        </el-table>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useUserStore } from '@/stores/user'
import {
  Key, Plus, Upload, Download, Search, Grid, List,
  MoreFilled, Edit, CircleCheck, Refresh, DataLine,
  CopyDocument, Delete, Clock, Warning, View, UploadFilled
} from '@element-plus/icons-vue'
import { getCredentialList, deleteCredential, verifyCredential, getCredentialTypes, getCredentialCategories, getCredentialUsage, rotateCredential, createCredential, batchVerifyCredentials, batchDeleteCredentials, exportCredentials, getCredentialSecretData, getCredentialImpact, batchCredentialImpact } from '@/api/credential'
import SecretForm from './components/SecretForm.vue'

const userStore = useUserStore()

// 状态
const loading = ref(false)
const secrets = ref([])
const selectedIds = ref([])
const credentialTypes = ref([])
const credentialCategories = ref([])
const searchQuery = ref('')
const filterType = ref('')
const filterCategory = ref('')
const filterStatus = ref('')
const viewMode = ref('card')
const dialogVisible = ref(false)
const isEdit = ref(false)
const currentSecret = ref(null)
const formKey = ref(0)
const detailDrawerVisible = ref(false)
const usageDialogVisible = ref(false)
const usageData = ref(null)
const importDialogVisible = ref(false)
const importPreview = ref([])
const importFile = ref(null)
const canWriteCredentials = computed(() => userStore.hasPermission('credential.write'))

// 分页
const pagination = reactive({
  page: 1,
  size: 12,
  total: 0
})

// 统计数据
const stats = computed(() => {
  return {
    total: pagination.total,
    active: secrets.value.filter(s => s.status === 'active').length,
    expiring: secrets.value.filter(s => s.expires_at && isExpiringSoon(s)).length,
    expired: secrets.value.filter(s => s.status === 'expired').length
  }
})

// 加载凭据列表
async function loadSecrets() {
  loading.value = true
  try {
    const params = {
      page: pagination.page,
      size: pagination.size,
      keyword: searchQuery.value,
      type: filterType.value,
      category: filterCategory.value,
      status: filterStatus.value === 'expiring' ? '' : filterStatus.value
    }
    
    const res = await getCredentialList(params)
    if (res.code === 200) {
      secrets.value = res.data.list
      pagination.total = res.data.total
    }
  } catch (error) {
    ElMessage.error('加载密钥列表失败')
  } finally {
    loading.value = false
  }
}

// 加载类型和分类
async function loadTypesAndCategories() {
  try {
    const [typesRes, catsRes] = await Promise.all([
      getCredentialTypes(),
      getCredentialCategories()
    ])
    if (typesRes.code === 200) {
      credentialTypes.value = typesRes.data.map(t => ({
        value: t.value || t.name,
        label: t.label || t.name,
        name: t.name,
        icon: t.icon,
        description: t.description
      }))
    }
    if (catsRes.code === 200) {
      credentialCategories.value = catsRes.data.map(c => ({
        value: c.value || c.name,
        label: c.label || c.name || c.value,
        name: c.name,
        icon: c.icon
      }))
    }
  } catch (error) {
    console.error('加载类型分类失败', error)
  }
}

// 搜索
function handleSearch() {
  pagination.page = 1
  loadSecrets()
}

// 选中切换
function toggleSelect(id) {
  const index = selectedIds.value.indexOf(id)
  if (index > -1) {
    selectedIds.value.splice(index, 1)
  } else {
    selectedIds.value.push(id)
  }
}

// 批量选择
function handleSelectionChange(val) {
  selectedIds.value = val.map(item => item.id)
}

// 卡片操作
function handleCardCommand(command, secret) {
  switch (command) {
    case 'edit':
      handleEdit(secret)
      break
    case 'verify':
      handleVerify(secret)
      break
    case 'rotate':
      handleRotate(secret)
      break
    case 'usage':
      handleShowUsage(secret)
      break
    case 'duplicate':
      handleDuplicate(secret)
      break
    case 'delete':
      handleDelete(secret)
      break
  }
}

function formatImpactHint(impact) {
  if (!impact) return ''
  const refs = Number(impact.reference_count || 0)
  if (refs <= 0) return ''
  const pipelines = Number(impact.pipeline_count || 0)
  return `当前密钥被 ${pipelines} 条流水线、${refs} 个任务节点引用，变更可能影响相关流水线后续运行。`
}

// 编辑
async function handleEdit(secret) {
  isEdit.value = true
  try {
    const secretRes = await getCredentialSecretData(secret.id)
    currentSecret.value = {
      ...secret,
      secret_data: secretRes?.data?.secret_data || {}
    }
  } catch (error) {
    currentSecret.value = {
      ...secret,
      secret_data: {}
    }
    ElMessage.warning('未获取到敏感字段回填数据，可手动补充后保存')
  }
  formKey.value++
  dialogVisible.value = true
}

// 验证
async function handleVerify(secret) {
  try {
    const res = await verifyCredential(secret.id)
    if (res.code === 200) {
      if (res.data.valid) {
        ElMessage.success('密钥验证通过')
      } else {
        ElMessage.warning('密钥验证失败: ' + res.data.message)
      }
    }
  } catch (error) {
    ElMessage.error('验证失败')
  }
}

// 轮换
async function handleRotate(secret) {
  try {
    let impactHint = ''
    try {
      const impactRes = await getCredentialImpact(secret.id)
      impactHint = formatImpactHint(impactRes?.data)
    } catch (error) {
      console.warn('获取密钥影响范围失败', error)
    }

    const rotateMessage = impactHint
      ? `确定要轮换密钥 "${secret.name}" 吗？\n${impactHint}\n轮换后旧凭据将失效。`
      : `确定要轮换密钥 "${secret.name}" 吗？轮换后旧凭据将失效。`

    await ElMessageBox.confirm(
      rotateMessage,
      '确认轮换',
      {
        type: 'warning',
        confirmButtonText: '确认轮换',
        cancelButtonText: '取消'
      }
    )

    const { value: secretDataText } = await ElMessageBox.prompt(
      '请输入新的 secret_data（JSON 格式）',
      '填写轮换内容',
      {
        inputType: 'textarea',
        inputValue: '{}',
        inputPlaceholder: '{"token":"new-token"}',
        confirmButtonText: '提交轮换',
        cancelButtonText: '取消'
      }
    )

    let secretData = {}
    try {
      secretData = JSON.parse(secretDataText || '{}')
    } catch (e) {
      ElMessage.error('secret_data 必须是合法 JSON')
      return
    }
    if (!secretData || typeof secretData !== 'object' || Object.keys(secretData).length === 0) {
      ElMessage.error('secret_data 不能为空')
      return
    }

    const res = await rotateCredential(secret.id, {
      secret_data: secretData,
      reason: '用户手动触发轮换'
    })

    if (res.code === 200) {
      ElMessage.success('密钥轮换成功')
      loadSecrets()
    } else {
      ElMessage.error(res.message || '轮换失败')
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('轮换失败')
    }
  }
}

// 显示使用统计
async function handleShowUsage(secret) {
  try {
    const res = await getCredentialUsage(secret.id)
    if (res.code === 200) {
      usageData.value = res.data
      usageDialogVisible.value = true
    }
  } catch (error) {
    ElMessage.error('获取使用统计失败')
  }
}

// 复制
function handleDuplicate(secret) {
  isEdit.value = false
  currentSecret.value = { ...secret, id: undefined, name: secret.name + ' (副本)' }
  formKey.value++
  dialogVisible.value = true
}

// 删除
async function handleDelete(secret) {
  try {
    let impactHint = ''
    try {
      const impactRes = await getCredentialImpact(secret.id)
      impactHint = formatImpactHint(impactRes?.data)
    } catch (error) {
      console.warn('获取密钥影响范围失败', error)
    }

    const deleteMessage = impactHint
      ? `确定要删除此密钥吗？\n${impactHint}\n此操作不可恢复。`
      : '确定要删除此密钥吗？此操作不可恢复。'

    await ElMessageBox.confirm(deleteMessage, '确认删除', {
      type: 'warning'
    })
    const res = await deleteCredential(secret.id)
    if (res.code === 200) {
      ElMessage.success('删除成功')
      loadSecrets()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

// 批量验证
async function batchVerify() {
  try {
    const res = await batchVerifyCredentials(selectedIds.value)
    if (res.code === 200) {
      ElMessage.success(`验证完成，成功 ${res.data.success}，失败 ${res.data.failed}`)
      loadSecrets()
      selectedIds.value = []
    }
  } catch (error) {
    ElMessage.error('批量验证失败')
  }
}

// 批量删除
async function batchDelete() {
  try {
    let impactHint = ''
    try {
      const impactRes = await batchCredentialImpact(selectedIds.value)
      const data = impactRes?.data
      if (data && Number(data.total_references || 0) > 0) {
        impactHint = `其中 ${data.impacted_credentials} 个密钥被 ${data.total_references} 个流水线任务引用，删除后可能导致相关流水线任务认证失败。`
      }
    } catch (error) {
      console.warn('获取批量密钥影响范围失败', error)
    }

    const batchMessage = impactHint
      ? `确定要删除选中的 ${selectedIds.value.length} 个密钥吗？\n${impactHint}`
      : `确定要删除选中的 ${selectedIds.value.length} 个密钥吗？`

    await ElMessageBox.confirm(batchMessage, '批量删除', {
      type: 'warning'
    })
    const res = await batchDeleteCredentials(selectedIds.value)
    if (res.code === 200) {
      ElMessage.success(`成功删除 ${res.data.deleted} 个密钥`)
      loadSecrets()
      selectedIds.value = []
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('批量删除失败')
    }
  }
}

// 批量导出
async function batchExport() {
  if (selectedIds.value.length === 0) {
    ElMessage.warning('请先选择要导出的密钥')
    return
  }

  try {
    ElMessage.info('正在导出...')

    // 过滤选中的密钥
    const selectedSecrets = secrets.value.filter(s => selectedIds.value.includes(s.id))
    const exportData = selectedSecrets.map(s => ({
      name: s.name,
      type: s.type,
      category: s.category,
      description: s.description,
      scope: s.scope,
      created_at: s.created_at,
      updated_at: s.updated_at
    }))

    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `secrets-batch-${new Date().toISOString().slice(0, 10)}.json`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    ElMessage.success(`成功导出 ${selectedIds.value.length} 个密钥`)
    selectedIds.value = []
  } catch (error) {
    ElMessage.error('批量导出失败')
  }
}

// 导出
async function exportSecrets() {
  try {
    ElMessage.info('正在导出...')
    const res = await exportCredentials({
      type: filterType.value || undefined,
      category: filterCategory.value || undefined
    })

    // 处理 Blob 响应
    const blob = new Blob([res], { type: 'application/json' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `secrets-export-${new Date().toISOString().slice(0, 10)}.json`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    window.URL.revokeObjectURL(url)

    ElMessage.success('导出成功')
  } catch (error) {
    ElMessage.error('导出失败')
  }
}

// 显示创建对话框
function showCreateDialog() {
  isEdit.value = false
  currentSecret.value = null
  formKey.value++
  dialogVisible.value = true
}

// 显示导入对话框
function showImportDialog() {
  importPreview.value = []
  importFile.value = null
  importDialogVisible.value = true
}

// 处理导入文件变化
function handleImportFileChange(file) {
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      const data = JSON.parse(e.target.result)
      importPreview.value = Array.isArray(data) ? data : [data]
      importFile.value = file
    } catch (error) {
      ElMessage.error('解析 JSON 文件失败')
    }
  }
  reader.readAsText(file.raw)
}

// 导入所有预览的密钥
async function importAll() {
  if (importPreview.value.length === 0) {
    ElMessage.warning('没有可导入的密钥')
    return
  }

  let successCount = 0
  let failCount = 0

  for (const item of importPreview.value) {
    try {
      await createCredential({
        name: item.name,
        type: item.type,
        category: item.category,
        description: item.description,
        scope: item.scope,
        secret_data: item.secret_data || {}
      })
      successCount++
    } catch (error) {
      failCount++
      console.error(`导入 ${item.name} 失败`, error)
    }
  }

  ElMessage.success(`导入完成，成功 ${successCount}，失败 ${failCount}`)
  importDialogVisible.value = false
  loadSecrets()
}

// 表单提交
async function handleFormSubmit(formData) {
  dialogVisible.value = false
  loadSecrets()
}

// 判断是否即将过期
function isExpiringSoon(secret) {
  if (!secret.expires_at) return false
  const warningDays = 7 * 24 * 60 * 60
  const now = Math.floor(Date.now() / 1000)
  return secret.expires_at <= now + warningDays && secret.expires_at > now
}

// 类型图标
const typeIconMap = {
  PASSWORD: 'Lock',
  SSH_KEY: 'Key',
  TOKEN: 'Ticket',
  OAUTH2: 'Connection',
  CERTIFICATE: 'Document',
  PASSKEY: 'Shield',
  MFA: 'Lock',
  IAM_ROLE: 'User'
}

// 类型标签
const typeLabelMap = {
  PASSWORD: '密码',
  SSH_KEY: 'SSH',
  TOKEN: 'Token',
  OAUTH2: 'OAuth2',
  CERTIFICATE: '证书',
  PASSKEY: 'Passkey',
  MFA: 'MFA',
  IAM_ROLE: 'IAM'
}

// 状态标签
const statusTagMap = {
  active: 'success',
  inactive: 'info',
  expired: 'warning',
  revoked: 'danger'
}

const statusLabelMap = {
  active: '活跃',
  inactive: '已禁用',
  expired: '已过期',
  revoked: '已撤销'
}

const typeTagMap = {
  PASSWORD: 'warning',
  SSH_KEY: 'success',
  TOKEN: 'primary',
  OAUTH2: 'info',
  CERTIFICATE: 'danger',
  PASSKEY: '',
  MFA: '',
  IAM_ROLE: ''
}

function getTypeIcon(type) {
  return typeIconMap[type] || 'Document'
}

function getTypeLabel(type) {
  return typeLabelMap[type] || type
}

function getCategoryLabel(category) {
  const found = credentialCategories.value.find(c => c.value === category)
  return found ? found.label : category || '未分类'
}

function getStatusType(status) {
  return statusTagMap[status] || 'info'
}

function getStatusLabel(status) {
  return statusLabelMap[status] || status
}

function getTypeTagType(type) {
  return typeTagMap[type] || 'info'
}

function getTypeColor(type) {
  const colors = {
    PASSWORD: '#E6A23C',
    SSH_KEY: '#67C23A',
    TOKEN: '#409EFF',
    OAUTH2: 'var(--text-muted)',
    CERTIFICATE: '#F56C6C',
    PASSKEY: '#00D4AA',
    MFA: '#FF6B6B',
    IAM_ROLE: '#7C3AED'
  }
  return colors[type] || '#409EFF'
}

function formatTime(timestamp) {
  if (!timestamp) return null
  const date = new Date(timestamp * 1000)
  const now = new Date()
  const diff = now - date
  
  if (diff < 60000) return '刚刚'
  if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`
  if (diff < 604800000) return `${Math.floor(diff / 86400000)} 天前`
  
  return date.toLocaleDateString()
}

function formatDate(timestamp) {
  if (!timestamp) return '-'
  // 处理 Unix 时间戳（秒）
  const ts = typeof timestamp === 'number' ? timestamp * 1000 : timestamp
  return new Date(ts).toLocaleDateString()
}

function formatDateTime(timestamp) {
  if (!timestamp) return '-'
  // 处理 Unix 时间戳（秒）
  const ts = typeof timestamp === 'number' ? timestamp * 1000 : timestamp
  return new Date(ts).toLocaleString()
}

// 监听筛选变化
watch([searchQuery, filterType, filterCategory, filterStatus], () => {
  pagination.page = 1
  loadSecrets()
})

// 初始化
onMounted(() => {
  loadSecrets()
  loadTypesAndCategories()
})
</script>

<style scoped>
.secrets-management {
  padding: 24px;
  background: var(--bg-secondary);
  min-height: 100%;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
}

.header-left h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 8px 0;
  font-size: 24px;
  font-weight: 600;
  color: var(--text-primary);
}

.header-icon {
  color: var(--primary-color);
}

.header-desc {
  margin: 0;
  color: var(--text-muted);
  font-size: 14px;
}

.header-actions {
  display: flex;
  gap: 12px;
}

.stats-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 24px;
}

.stat-card {
  border: none;
}

.stat-card :deep(.el-card__body) {
  padding: 20px;
}

.stat-content {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  color: white;
}

.stat-icon.total { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); }
.stat-icon.active { background: linear-gradient(135deg, #11998e 0%, #38ef7d 100%); }
.stat-icon.expiring { background: linear-gradient(135deg, #f2994a 0%, #f2c94c 100%); }
.stat-icon.expired { background: linear-gradient(135deg, #eb3349 0%, #f45c43 100%); }

.stat-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-primary);
}

.stat-label {
  font-size: 14px;
  color: var(--text-muted);
  margin-top: 4px;
}

.filter-section {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
  padding: 16px;
  background: var(--bg-card);
  border-radius: 8px;
}

.filter-left {
  display: flex;
  gap: 12px;
}

.search-input {
  width: 240px;
}

.filter-select {
  width: 140px;
}

.batch-actions {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  background: var(--primary-lighter);
  border-radius: 8px;
  margin-bottom: 16px;
}

.batch-info {
  color: var(--primary-color);
  font-weight: 500;
}

.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
}

.secret-card {
  border: none;
  transition: transform 0.2s, box-shadow 0.2s;
}

.secret-card:hover {
  transform: translateY(-2px);
}

.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  border-bottom: 1px solid #f0f0f0;
}

.card-type-icon {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 20px;
}

.card-info {
  flex: 1;
  min-width: 0;
}

.card-name {
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.card-type {
  font-size: 12px;
  color: var(--text-muted);
}

.card-more {
  padding: 4px;
}

.card-body {
  padding: 16px;
}

.card-category {
  margin-bottom: 12px;
}

.card-meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 12px;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--text-muted);
}

.card-status {
  display: flex;
  gap: 8px;
}

.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: var(--bg-tertiary);
  border-top: 1px solid #f0f0f0;
}

.card-time {
  font-size: 12px;
  color: var(--text-muted);
}

.secret-table {
  background: var(--bg-card);
  border-radius: 8px;
}

.table-name-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.type-icon {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 16px;
}

.pagination-wrapper {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
}

.detail-section {
  margin-bottom: 24px;
}

.detail-section h4 {
  margin: 0 0 12px 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}

.usage-stats {
  padding: 20px 0;
}

.usage-stat-item {
  text-align: center;
  padding: 20px;
  background: var(--bg-secondary);
  border-radius: 8px;
}

.usage-stat-item.success {
  background: var(--success-light);
}

.usage-stat-item.danger {
  background: var(--danger-light);
}

.usage-value {
  font-size: 32px;
  font-weight: 700;
  color: var(--text-primary);
}

.usage-label {
  font-size: 14px;
  color: var(--text-muted);
  margin-top: 8px;
}

.usage-chart {
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 40px 0;
}

.progress-text {
  text-align: center;
}

.progress-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-primary);
}

.progress-label {
  font-size: 14px;
  color: var(--text-muted);
}

.usage-last {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 14px;
}

.import-upload {
  width: 100%;
}

.import-upload :deep(.el-upload) {
  width: 100%;
}

.import-upload :deep(.el-upload-dragger) {
  width: 100%;
}

.import-preview {
  margin-top: 20px;
  border-top: 1px solid #f0f0f0;
  padding-top: 16px;
}

.preview-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  font-weight: 500;
  color: var(--text-primary);
}
</style>
