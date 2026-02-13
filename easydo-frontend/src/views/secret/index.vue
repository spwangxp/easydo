<template>
  <div class="secret-container">
    <div class="page-header">
      <h1 class="page-title">密钥管理</h1>
      <div class="header-actions">
        <el-button @click="$router.push('/secrets/statistics')">
          <el-icon><TrendCharts /></el-icon>
          统计
        </el-button>
        <el-button type="primary" @click="handleCreate">
          <el-icon><Plus /></el-icon>
          新增密钥
        </el-button>
      </div>
    </div>

    <div class="search-bar">
      <el-input
        v-model="searchQuery"
        placeholder="搜索密钥名称"
        clearable
        style="width: 240px"
        @keyup.enter="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      
      <el-select v-model="filterType" placeholder="密钥类型" clearable style="width: 150px; margin-left: 12px;">
        <el-option label="SSH密钥" value="ssh" />
        <el-option label="访问令牌" value="token" />
        <el-option label="镜像仓库" value="registry" />
        <el-option label="API密钥" value="api_key" />
        <el-option label="Kubernetes" value="kubernetes" />
      </el-select>

      <el-button type="primary" style="margin-left: 12px;" @click="handleSearch">
        搜索
      </el-button>
    </div>

    <div v-if="selectedSecrets.length > 0" class="batch-actions" style="margin-bottom: 16px; padding: 12px; background-color: #f5f7fa; border-radius: 4px;">
      <span style="margin-right: 16px;">已选择 {{ selectedSecrets.length }} 项</span>
      <el-button type="danger" size="small" @click="handleBatchDelete">
        批量删除
      </el-button>
    </div>

    <el-table 
      :data="secretList" 
      v-loading="loading" 
      style="width: 100%"
      @selection-change="handleSelectionChange"
    >
      <el-table-column type="selection" width="55" />
      <el-table-column prop="name" label="密钥名称" min-width="180">
        <template #default="{ row }">
          <div class="secret-name">
            <el-icon class="secret-icon">
              <Key v-if="row.type === 'ssh'" />
              <Connection v-else-if="row.type === 'token'" />
              <Box v-else-if="row.type === 'registry'" />
              <Lock v-else-if="row.type === 'api_key'" />
              <Monitor v-else />
            </el-icon>
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
          <el-tag :type="getStatusTagType(row.status)" size="small">
            {{ getStatusLabel(row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      
      <el-table-column prop="used_count" label="使用次数" width="100" />
      
      <el-table-column prop="updated_at" label="更新时间" width="180">
        <template #default="{ row }">
          {{ formatTime(row.updated_at) }}
        </template>
      </el-table-column>
      
      <el-table-column label="操作" width="280" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link size="small" @click="handleView(row)">
            查看
          </el-button>
          <el-button type="success" link size="small" @click="handleVerify(row)">
            验证
          </el-button>
          <el-button type="warning" link size="small" @click="handleRotate(row)">
            轮换
          </el-button>
          <el-button type="primary" link size="small" @click="handleEdit(row)">
            编辑
          </el-button>
          <el-button type="danger" link size="small" @click="handleDelete(row)">
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @size-change="handleSizeChange"
        @current-change="handlePageChange"
      />
    </div>

    <!-- 新增/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
      destroy-on-close
    >
      <SecretForm
        v-if="dialogVisible"
        :type="formType"
        :data="currentSecret"
        @submit="handleFormSubmit"
        @cancel="dialogVisible = false"
      />
    </el-dialog>

    <!-- 查看详情对话框 -->
    <el-dialog
      v-model="detailVisible"
      title="密钥详情"
      width="600px"
    >
      <SecretDetail
        v-if="detailVisible"
        :data="currentSecret"
        @edit="handleEditFromDetail"
        @delete="handleDeleteFromDetail"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search, Key, Connection, Box, Lock, Monitor, TrendCharts } from '@element-plus/icons-vue'
import { getSecretList, deleteSecret, verifySecret, rotateSecret, batchDeleteSecrets } from '@/api/secret'
import SecretForm from './components/SecretForm.vue'
import SecretDetail from './components/SecretDetail.vue'

const loading = ref(false)
const secretList = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const searchQuery = ref('')
const filterType = ref('')

const dialogVisible = ref(false)
const detailVisible = ref(false)
const dialogTitle = ref('新增密钥')
const formType = ref('create')
const currentSecret = ref(null)
const selectedSecrets = ref([])

const typeMap = {
  ssh: { label: 'SSH密钥', tagType: 'success' },
  token: { label: '访问令牌', tagType: 'primary' },
  registry: { label: '镜像仓库', tagType: 'warning' },
  api_key: { label: 'API密钥', tagType: 'info' },
  kubernetes: { label: 'Kubernetes', tagType: 'danger' }
}

const categoryMap = {
  github: 'GitHub',
  gitlab: 'GitLab',
  gitee: 'Gitee',
  docker: 'Docker',
  dingtalk: '钉钉',
  wechat: '企业微信',
  kubernetes: 'Kubernetes',
  custom: '自定义'
}

const statusMap = {
  active: { label: '启用', tagType: 'success' },
  inactive: { label: '禁用', tagType: 'info' },
  expired: { label: '过期', tagType: 'warning' },
  revoked: { label: '撤销', tagType: 'danger' }
}

const getTypeLabel = (type) => typeMap[type]?.label || type
const getTypeTagType = (type) => typeMap[type]?.tagType || ''
const getCategoryLabel = (category) => categoryMap[category] || category
const getStatusLabel = (status) => statusMap[status]?.label || status
const getStatusTagType = (status) => statusMap[status]?.tagType || ''

const formatTime = (time) => {
  if (!time) return '-'
  return new Date(time).toLocaleString()
}

const loadData = async () => {
  loading.value = true
  try {
    const params = {
      page: page.value,
      page_size: pageSize.value,
      search: searchQuery.value,
      type: filterType.value
    }
    const res = await getSecretList(params)
    if (res.code === 200) {
      secretList.value = res.data.list
      total.value = res.data.total
    }
  } catch (error) {
    ElMessage.error('加载密钥列表失败')
  } finally {
    loading.value = false
  }
}

const handleSearch = () => {
  page.value = 1
  loadData()
}

const handleSizeChange = (val) => {
  pageSize.value = val
  loadData()
}

const handlePageChange = (val) => {
  page.value = val
  loadData()
}

const handleCreate = () => {
  formType.value = 'create'
  dialogTitle.value = '新增密钥'
  currentSecret.value = null
  dialogVisible.value = true
}

const handleEdit = (row) => {
  formType.value = 'edit'
  dialogTitle.value = '编辑密钥'
  currentSecret.value = { ...row }
  dialogVisible.value = true
}

const handleView = (row) => {
  currentSecret.value = { ...row }
  detailVisible.value = true
}

const handleDelete = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除密钥 "${row.name}" 吗？此操作不可恢复。`,
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
    
    const res = await deleteSecret(row.id)
    if (res.code === 200) {
      ElMessage.success('删除成功')
      loadData()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

const handleFormSubmit = async (formData) => {
  dialogVisible.value = false
  ElMessage.success(formType.value === 'create' ? '创建成功' : '更新成功')
  loadData()
}

const handleEditFromDetail = () => {
  detailVisible.value = false
  handleEdit(currentSecret.value)
}

const handleDeleteFromDetail = () => {
  detailVisible.value = false
  handleDelete(currentSecret.value)
}

const handleVerify = async (row) => {
  try {
    const res = await verifySecret(row.id)
    if (res.code === 200) {
      if (res.valid) {
        ElMessage.success(`验证成功: ${res.message}`)
      } else {
        ElMessage.warning(`验证失败: ${res.message}`)
      }
    }
  } catch (error) {
    ElMessage.error('验证请求失败')
  }
}

const handleRotate = async (row) => {
  try {
    await ElMessageBox.confirm(
      `确定要轮换密钥 "${row.name}" 吗？此操作将生成新的密钥值，旧值将被保存到历史记录中。`,
      '确认轮换',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )
    
    const res = await rotateSecret(row.id, {
      regenerate: row.type === 'ssh'
    })
    if (res.code === 200) {
      ElMessage.success(`轮换成功 (版本: ${res.data.old_version} → ${res.data.new_version})`)
      loadData()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('轮换失败')
    }
  }
}

const handleSelectionChange = (selection) => {
  selectedSecrets.value = selection
}

const handleBatchDelete = async () => {
  if (selectedSecrets.value.length === 0) {
    ElMessage.warning('请先选择要删除的密钥')
    return
  }

  try {
    await ElMessageBox.confirm(
      `确定要删除选中的 ${selectedSecrets.value.length} 个密钥吗？此操作不可恢复。`,
      '确认批量删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    const ids = selectedSecrets.value.map(s => s.id)
    const res = await batchDeleteSecrets(ids)
    if (res.code === 200) {
      ElMessage.success(`批量删除成功，共删除 ${res.data.deleted_count} 个密钥`)
      selectedSecrets.value = []
      loadData()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('批量删除失败')
    }
  }
}

onMounted(() => {
  loadData()
})
</script>

<style lang="scss" scoped>
.secret-container {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;

  .page-title {
    font-size: 24px;
    font-weight: 600;
    color: #303133;
    margin: 0;
  }
}

.search-bar {
  margin-bottom: 20px;
  display: flex;
  align-items: center;
}

.secret-name {
  display: flex;
  align-items: center;
  gap: 8px;

  .secret-icon {
    color: #409EFF;
    font-size: 16px;
  }
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
