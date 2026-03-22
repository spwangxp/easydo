<template>
  <section class="card-shell" v-loading="loading">
    <div class="section-header">
      <div>
        <h2 class="section-title">集群概览</h2>
        <p class="section-description">优先展示资源管理里已采集到的集群基础信息，避免把资源浏览做成另一套入口。</p>
      </div>
      <el-tag :type="statusTagType" size="large">{{ statusLabel }}</el-tag>
    </div>

    <el-alert v-if="overview?.baseInfoStatus === 'failed'" type="warning" :closable="false" show-icon class="status-alert">
      {{ overview?.baseInfoLastError || '最近一次基础信息采集失败，请返回资源管理刷新基础信息后重试。' }}
    </el-alert>
    <el-alert v-else-if="overview?.baseInfoStatus === 'pending'" type="info" :closable="false" show-icon class="status-alert">
      执行器仍在采集基础信息，集群概览可能不完整。
    </el-alert>

    <div class="overview-grid">
      <div class="overview-card">
        <span class="overview-label">Kubernetes 版本</span>
        <strong class="overview-value">{{ overview?.clusterVersion || '-' }}</strong>
      </div>
      <div class="overview-card accent-primary">
        <span class="overview-label">节点数</span>
        <strong class="overview-value">{{ overview?.nodeCount || 0 }}</strong>
      </div>
      <div class="overview-card accent-success">
        <span class="overview-label">可分配 CPU</span>
        <strong class="overview-value compact">{{ formatCPUMilli(overview?.cpuAllocatableMilli) }}</strong>
      </div>
      <div class="overview-card accent-warning">
        <span class="overview-label">可分配内存</span>
        <strong class="overview-value compact">{{ formatBytes(overview?.memoryAllocatableBytes) }}</strong>
      </div>
    </div>

    <el-descriptions :column="2" border class="overview-meta">
      <el-descriptions-item label="资源名称">{{ overview?.name || '-' }}</el-descriptions-item>
      <el-descriptions-item label="接入地址">{{ overview?.endpoint || '-' }}</el-descriptions-item>
      <el-descriptions-item label="环境">{{ getEnvironmentLabel(overview?.environment) }}</el-descriptions-item>
      <el-descriptions-item label="可分配 GPU">{{ overview?.gpuAllocatable || 0 }}</el-descriptions-item>
      <el-descriptions-item label="基础信息来源">{{ overview?.baseInfoSource || '-' }}</el-descriptions-item>
      <el-descriptions-item label="最近采集时间">{{ formatDateTime(overview?.baseInfoCollectedAt) }}</el-descriptions-item>
    </el-descriptions>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import {
  formatBytes,
  formatCPUMilli,
  formatDateTime,
  getClusterStatusType,
  getEnvironmentLabel
} from '../utils'

const props = defineProps({
  overview: {
    type: Object,
    default: null
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const statusLabel = computed(() => ({ online: '在线', offline: '离线', error: '异常', archived: '归档' }[props.overview?.status] || props.overview?.status || '-'))
const statusTagType = computed(() => getClusterStatusType(props.overview?.status))
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

.status-alert {
  margin-bottom: $space-4;
}

.overview-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: $space-3;
  margin-bottom: $space-4;
}

.overview-card {
  display: flex;
  flex-direction: column;
  gap: $space-2;
  padding: $space-4;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);
  box-shadow: var(--shadow-sm);

  &.accent-primary {
    background: linear-gradient(140deg, rgba($primary-color, 0.14), rgba($primary-color, 0.04));
  }

  &.accent-success {
    background: linear-gradient(140deg, rgba($success-color, 0.14), rgba($success-color, 0.04));
  }

  &.accent-warning {
    background: linear-gradient(140deg, rgba($warning-color, 0.14), rgba($warning-color, 0.04));
  }
}

.overview-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary);
}

.overview-value {
  font-family: $font-family-display;
  font-size: 28px;
  line-height: 1;
  color: var(--text-primary);

  &.compact {
    font-size: 22px;
  }
}

.overview-meta {
  :deep(.el-descriptions__body) {
    background: transparent;
  }
}

@media (max-width: 992px) {
  .overview-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 768px) {
  .section-header,
  .overview-grid {
    grid-template-columns: 1fr;
    flex-direction: column;
  }
}
</style>
