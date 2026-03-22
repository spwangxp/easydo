<template>
  <section class="card-shell" v-loading="loading">
    <div class="section-header">
      <div>
        <h2 class="section-title">操作审计</h2>
        <p class="section-description">展示当前资源范围内的 K8s 操作记录，默认跟随已选择的命名空间收敛结果。</p>
      </div>
      <el-button @click="$emit('refresh')">刷新审计</el-button>
    </div>

    <el-table v-if="audits.length > 0" :data="audits" style="width: 100%">
      <el-table-column label="动作" width="140">
        <template #default="{ row }">
          <el-tag :type="getActionType(row.action)">{{ getActionLabel(row.action) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="目标资源" min-width="220">
        <template #default="{ row }">
          <div class="target-cell">
            <span class="target-main">{{ row.target_kind || '-' }} / {{ row.target_name || '-' }}</span>
            <span class="target-meta">{{ row.namespace || '-' }} · Task #{{ row.task_id || '-' }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="reason" label="执行原因" min-width="220" />
      <el-table-column label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="getAuditStatusType(row.status)">{{ getAuditStatusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="结果摘要" min-width="260">
        <template #default="{ row }">
          <span class="result-summary">{{ row.error_message || row.result_summary || '-' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="180">
        <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
      </el-table-column>
    </el-table>

    <el-empty v-else description="当前筛选范围内暂无操作审计记录" :image-size="76" />
  </section>
</template>

<script setup>
import { formatDateTime, getActionLabel, getActionType, getAuditStatusLabel, getAuditStatusType } from '../utils'

defineProps({
  audits: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  }
})

defineEmits(['refresh'])
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.card-shell {
  padding: $space-5;
  border-radius: $radius-xl;
  border: 1px solid var(--border-color-light);
  background: var(--bg-card);
  box-shadow: var(--shadow-md);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: $space-4;
  margin-bottom: $space-4;
}

.section-title {
  margin: 0;
  font-size: 20px;
  color: var(--text-primary);
}

.section-description {
  margin: $space-2 0 0;
  color: var(--text-secondary);
  line-height: 1.7;
}

.target-cell {
  display: flex;
  flex-direction: column;
  gap: $space-1;
}

.target-main {
  font-weight: 700;
  color: var(--text-primary);
}

.target-meta,
.result-summary {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.6;
}

@media (max-width: 768px) {
  .section-header {
    flex-direction: column;
  }
}
</style>
