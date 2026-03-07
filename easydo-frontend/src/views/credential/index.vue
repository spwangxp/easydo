<template>
  <div class="credential-management">
    <div class="page-header">
      <h2>凭据管理</h2>
      <el-button type="primary" @click="showCreateDialog">
        <el-icon><Plus /></el-icon>
        新建凭据
      </el-button>
    </div>

    <div class="filter-bar">
      <el-input
        v-model="searchQuery"
        placeholder="搜索凭据名称"
        clearable
        style="width: 200px"
        @clear="handleSearch"
        @keyup.enter="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      
      <el-select v-model="filterType" placeholder="凭据类型" clearable style="width: 150px" @change="handleSearch">
        <el-option
          v-for="type in credentialTypes"
          :key="type.value"
          :label="type.label"
          :value="type.value"
        />
      </el-select>

      <el-select v-model="filterCategory" placeholder="分类" clearable style="width: 150px" @change="handleSearch">
        <el-option
          v-for="cat in credentialCategories"
          :key="cat.value"
          :label="cat.label"
          :value="cat.value"
        />
      </el-select>

      <el-select v-model="filterStatus" placeholder="状态" clearable style="width: 120px" @change="handleSearch">
        <el-option label="活跃" value="active" />
        <el-option label="已禁用" value="inactive" />
        <el-option label="已过期" value="expired" />
      </el-select>
    </div>

    <el-table :data="credentials" v-loading="loading" style="width: 100%">
      <el-table-column prop="name" label="名称" min-width="200">
        <template #default="{ row }">
          <div class="credential-name">
            <el-icon :size="20" :color="getTypeColor(row.type)">
              <component :is="getTypeIcon(row.type)" />
            </el-icon>
            <span>{{ row.name }}</span>
          </div>
        </template>
      </el-table-column>
      
      <el-table-column prop="type" label="类型" width="120">
        <template #default="{ row }">
          <el-tag :type="getTypeTagType(row.type)">{{ getTypeLabel(row.type) }}</el-tag>
        </template>
      </el-table-column>
      
      <el-table-column prop="category" label="分类" width="120">
        <template #default="{ row }">
          {{ getCategoryLabel(row.category) }}
        </template>
      </el-table-column>
      
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="getStatusType(row.status)">{{ getStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      
      <el-table-column prop="last_used_at" label="最后使用" width="180">
        <template #default="{ row }">
          {{ row.last_used_at ? formatTime(row.last_used_at) : '从未使用' }}
        </template>
      </el-table-column>
      
      <el-table-column prop="used_count" label="使用次数" width="100" />
      
      <el-table-column label="操作" width="200" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link @click="handleEdit(row)">编辑</el-button>
          <el-button type="primary" link @click="handleVerify(row)">验证</el-button>
          <el-button type="danger" link @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination">
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @size-change="handleSizeChange"
        @current-change="handlePageChange"
      />
    </div>

    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? '编辑凭据' : '新建凭据'"
      width="600px"
    >
      <CredentialForm
        v-if="dialogVisible"
        :initial-data="currentCredential"
        :types="credentialTypes"
        :categories="credentialCategories"
        @submit="handleFormSubmit"
        @cancel="dialogVisible = false"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Search } from '@element-plus/icons-vue'
import { getCredentialList, deleteCredential, verifyCredential, getCredentialTypes, getCredentialCategories } from '@/api/credential'
import CredentialForm from './components/CredentialForm.vue'

const loading = ref(false)
const credentials = ref([])
const total = ref(0)
const page = ref(1)
const size = ref(10)
const searchQuery = ref('')
const filterType = ref('')
const filterCategory = ref('')
const filterStatus = ref('')
const credentialTypes = ref([])
const credentialCategories = ref([])
const dialogVisible = ref(false)
const isEdit = ref(false)
const currentCredential = ref(null)

const typeIconMap = {
  PASSWORD: 'Lock',
  SSH_KEY: 'Key',
  TOKEN: 'Ticket',
  OAUTH2: 'Connection',
  CERTIFICATE: 'Document'
}

const typeColorMap = {
  PASSWORD: '#E6A23C',
  SSH_KEY: '#67C23A',
  TOKEN: '#409EFF',
  OAUTH2: 'var(--text-muted)',
  CERTIFICATE: '#F56C6C'
}

const typeTagMap = {
  PASSWORD: 'warning',
  SSH_KEY: 'success',
  TOKEN: 'primary',
  OAUTH2: 'info',
  CERTIFICATE: 'danger'
}

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

onMounted(() => {
  loadCredentials()
  loadTypes()
  loadCategories()
})

async function loadCredentials() {
  loading.value = true
  try {
    const params = {
      page: page.value,
      size: size.value,
      keyword: searchQuery.value,
      type: filterType.value,
      category: filterCategory.value,
      status: filterStatus.value
    }
    const res = await getCredentialList(params)
    if (res.code === 200) {
      credentials.value = res.data.list
      total.value = res.data.total
    }
  } catch (error) {
    ElMessage.error('加载凭据列表失败')
  } finally {
    loading.value = false
  }
}

async function loadTypes() {
  try {
    const res = await getCredentialTypes()
    if (res.code === 200) {
      credentialTypes.value = res.data
    }
  } catch (error) {
    console.error('加载凭据类型失败', error)
  }
}

async function loadCategories() {
  try {
    const res = await getCredentialCategories()
    if (res.code === 200) {
      credentialCategories.value = res.data
    }
  } catch (error) {
    console.error('加载凭据分类失败', error)
  }
}

function handleSearch() {
  page.value = 1
  loadCredentials()
}

function handleSizeChange(val) {
  size.value = val
  loadCredentials()
}

function handlePageChange(val) {
  page.value = val
  loadCredentials()
}

function showCreateDialog() {
  isEdit.value = false
  currentCredential.value = null
  dialogVisible.value = true
}

function handleEdit(row) {
  isEdit.value = true
  currentCredential.value = row
  dialogVisible.value = true
}

async function handleVerify(row) {
  try {
    const res = await verifyCredential(row.id)
    if (res.code === 200) {
      if (res.data.valid) {
        ElMessage.success('凭据验证通过')
      } else {
        ElMessage.error('凭据验证失败: ' + res.data.message)
      }
    }
  } catch (error) {
    ElMessage.error('验证凭据失败')
  }
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm('确定要删除此凭据吗？', '确认删除', {
      type: 'warning'
    })
    const res = await deleteCredential(row.id)
    if (res.code === 200) {
      ElMessage.success('删除成功')
      loadCredentials()
    }
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

async function handleFormSubmit(formData) {
  dialogVisible.value = false
  loadCredentials()
}

function getTypeIcon(type) {
  return typeIconMap[type] || 'Document'
}

function getTypeColor(type) {
  return typeColorMap[type] || '#409EFF'
}

function getTypeTagType(type) {
  return typeTagMap[type] || 'info'
}

function getTypeLabel(type) {
  const found = credentialTypes.value.find(t => t.value === type)
  return found ? found.label : type
}

function getCategoryLabel(category) {
  const found = credentialCategories.value.find(c => c.value === category)
  return found ? found.label : category
}

function getStatusType(status) {
  return statusTagMap[status] || 'info'
}

function getStatusLabel(status) {
  return statusLabelMap[status] || status
}

function formatTime(timestamp) {
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  return date.toLocaleString()
}
</script>

<style scoped>
.credential-management {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.filter-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
}

.credential-name {
  display: flex;
  align-items: center;
  gap: 8px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
