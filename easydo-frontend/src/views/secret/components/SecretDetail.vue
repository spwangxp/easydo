<template>
  <div class="secret-detail">
    <el-descriptions :column="2" border>
      <el-descriptions-item label="密钥名称">{{ data.name }}</el-descriptions-item>
      <el-descriptions-item label="密钥ID">{{ data.id }}</el-descriptions-item>
      
      <el-descriptions-item label="类型">
        <el-tag :type="getTypeTagType(data.type)" size="small">
          {{ getTypeLabel(data.type) }}
        </el-tag>
      </el-descriptions-item>
      
      <el-descriptions-item label="分类">
        {{ getCategoryLabel(data.category) }}
      </el-descriptions-item>
      
      <el-descriptions-item label="状态">
        <el-tag :type="getStatusTagType(data.status)" size="small">
          {{ getStatusLabel(data.status) }}
        </el-tag>
      </el-descriptions-item>
      
      <el-descriptions-item label="使用次数">{{ data.used_count || 0 }}</el-descriptions-item>
      
      <el-descriptions-item label="创建时间">{{ formatTime(data.created_at) }}</el-descriptions-item>
      <el-descriptions-item label="更新时间">{{ formatTime(data.updated_at) }}</el-descriptions-item>
      
      <el-descriptions-item label="最后使用时间" v-if="data.last_used_at">
        {{ formatTime(data.last_used_at) }}
      </el-descriptions-item>
      
      <el-descriptions-item label="版本">{{ data.version || 1 }}</el-descriptions-item>
      
      <el-descriptions-item label="描述" :span="2">
        {{ data.description || '无' }}
      </el-descriptions-item>
    </el-descriptions>

    <div class="actions" style="margin-top: 24px; display: flex; justify-content: flex-end; gap: 12px;">
      <el-button @click="handleClose">关闭</el-button>
      <el-button type="primary" @click="handleEdit">编辑</el-button>
      <el-button type="danger" @click="handleDelete">删除</el-button>
    </div>
  </div>
</template>

<script setup>
const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const emit = defineEmits(['edit', 'delete', 'close'])

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

const handleClose = () => {
  emit('close')
}

const handleEdit = () => {
  emit('edit')
}

const handleDelete = () => {
  emit('delete')
}
</script>

<style lang="scss" scoped>
.secret-detail {
  padding: 0;
}
</style>
