<template>
  <div class="secret-statistics">
    <div class="page-header">
      <h1 class="page-title">密钥统计</h1>
      <el-button @click="$router.back()">
        <el-icon><ArrowLeft /></el-icon>
        返回
      </el-button>
    </div>

    <el-row :gutter="20" class="stat-cards">
      <el-col :span="8">
        <el-card class="stat-card">
          <template #header>
            <div class="card-header">
              <el-icon><Key /></el-icon>
              <span>密钥总数</span>
            </div>
          </template>
          <div class="stat-value">{{ statistics.total_secrets }}</div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stat-card">
          <template #header>
            <div class="card-header">
              <el-icon><TrendCharts /></el-icon>
              <span>使用次数</span>
            </div>
          </template>
          <div class="stat-value">{{ statistics.total_usages }}</div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card class="stat-card">
          <template #header>
            <div class="card-header">
              <el-icon><CircleCheck /></el-icon>
              <span>活跃密钥</span>
            </div>
          </template>
          <div class="stat-value">{{ activeCount }}</div>
        </el-card>
      </el-col>
    </el-row>

    <el-row :gutter="20" class="chart-section">
      <el-col :span="12">
        <el-card>
          <template #header>密钥类型分布</template>
          <el-table :data="statistics.by_type" stripe style="width: 100%">
            <el-table-column prop="type" label="类型">
              <template #default="{ row }">
                {{ getTypeLabel(row.type) }}
              </template>
            </el-table-column>
            <el-table-column prop="count" label="数量" width="100" />
          </el-table>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card>
          <template #header>密钥状态分布</template>
          <el-table :data="statistics.by_status" stripe style="width: 100%">
            <el-table-column prop="status" label="状态">
              <template #default="{ row }">
                <el-tag :type="getStatusTagType(row.status)" size="small">
                  {{ getStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="count" label="数量" width="100" />
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <el-row class="chart-section">
      <el-col :span="24">
        <el-card>
          <template #header>近7天使用趋势</template>
          <el-table :data="statistics.usage_by_day" stripe style="width: 100%">
            <el-table-column prop="date" label="日期" />
            <el-table-column prop="count" label="使用次数" />
          </el-table>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Key, TrendCharts, CircleCheck } from '@element-plus/icons-vue'
import { getStatistics } from '@/api/secret'

const statistics = ref({
  total_secrets: 0,
  total_usages: 0,
  by_type: [],
  by_status: [],
  usage_by_day: []
})

const activeCount = computed(() => {
  const active = statistics.value.by_status?.find(s => s.status === 'active')
  return active ? active.count : 0
})

const typeMap = {
  ssh: 'SSH密钥',
  token: '访问令牌',
  registry: '镜像仓库',
  api_key: 'API密钥',
  kubernetes: 'Kubernetes'
}

const statusMap = {
  active: { label: '启用', tagType: 'success' },
  inactive: { label: '禁用', tagType: 'info' },
  expired: { label: '过期', tagType: 'warning' },
  revoked: { label: '撤销', tagType: 'danger' }
}

const getTypeLabel = (type) => typeMap[type] || type
const getStatusLabel = (status) => statusMap[status]?.label || status
const getStatusTagType = (status) => statusMap[status]?.tagType || ''

const loadStatistics = async () => {
  try {
    const res = await getStatistics()
    if (res.code === 200) {
      statistics.value = res.data
    }
  } catch (error) {
    ElMessage.error('加载统计数据失败')
  }
}

onMounted(() => {
  loadStatistics()
})
</script>

<style scoped>
.secret-statistics {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.page-title {
  margin: 0;
  font-size: 24px;
  font-weight: 600;
}

.stat-cards {
  margin-bottom: 20px;
}

.stat-card {
  text-align: center;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.stat-value {
  font-size: 36px;
  font-weight: bold;
  color: #409EFF;
  padding: 20px 0;
}

.chart-section {
  margin-bottom: 20px;
}
</style>
