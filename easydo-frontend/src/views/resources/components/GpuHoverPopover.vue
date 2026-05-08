<template>
  <el-popover placement="top" :width="popoverWidth" trigger="hover" popper-class="gpu-hover-popper">
    <template #reference>
      <slot />
    </template>

    <div class="hover-card">
      <div class="hover-title">{{ title || '-' }}</div>

      <div v-if="entity" class="hover-block">
        <div class="hover-label">节点</div>
        <div class="kv-list">
          <div class="kv-item"><span>名称</span><strong>{{ entity.name || '-' }}</strong></div>
          <div v-if="entity.fieldsMap?.hostname" class="kv-item"><span>主机名</span><strong>{{ formatValue(entity.fieldsMap.hostname) }}</strong></div>
          <div v-if="entity.fieldsMap?.primaryIpv4" class="kv-item"><span>IP</span><strong>{{ formatValue(entity.fieldsMap.primaryIpv4) }}</strong></div>
        </div>
      </div>

      <div v-if="gpuInfo" class="hover-block">
        <div class="hover-label">GPU</div>
        <div class="kv-list">
          <div class="kv-item"><span>名称</span><strong>{{ gpuInfo.displayName || '-' }}</strong></div>
          <div class="kv-item"><span>型号</span><strong>{{ gpuInfo.model || '-' }}</strong></div>
          <div class="kv-item"><span>厂商</span><strong>{{ gpuInfo.vendor || '-' }}</strong></div>
          <div class="kv-item"><span>索引</span><strong>{{ formatValue(gpuInfo.index) }}</strong></div>
          <div class="kv-item"><span>UUID</span><strong>{{ formatValue(gpuInfo.uuid) }}</strong></div>
          <div class="kv-item"><span>Bus ID</span><strong>{{ formatValue(gpuInfo.busId) }}</strong></div>
          <div class="kv-item"><span>显存</span><strong>{{ formatBytes(gpuInfo.memoryBytes) }}</strong></div>
          <div class="kv-item"><span>已用显存</span><strong>{{ formatBytes(gpuInfo.memoryUsedBytes) }}</strong></div>
          <div class="kv-item"><span>温度</span><strong>{{ formatTemperature(gpuInfo.temperatureGpuCelsius) }}</strong></div>
          <div class="kv-item"><span>利用率</span><strong>{{ formatPercent(gpuInfo.utilizationGpuPercent) }}</strong></div>
        </div>
      </div>

      <div v-if="serviceHover" class="hover-block">
        <div class="hover-label">服务</div>
        <div class="kv-list">
          <div class="kv-item"><span>服务名</span><strong>{{ serviceHover.displayName || '-' }}</strong></div>
          <div class="kv-item"><span>ID</span><strong>{{ serviceHover.id || '-' }}</strong></div>
          <div class="kv-item"><span>name</span><strong>{{ serviceHover.name || '-' }}</strong></div>
          <div class="kv-item"><span>部署方式</span><strong>{{ serviceHover.runtimeType || '-' }}</strong></div>
          <div class="kv-item"><span>PID</span><strong>{{ formatPidList(serviceHover.pids) }}</strong></div>
          <div class="kv-item"><span>观测 PID</span><strong>{{ formatPidList(serviceHover.observedPids) }}</strong></div>
          <div class="kv-item"><span>显存占用</span><strong>{{ formatBytes(serviceHover.memoryUsedBytes) }}</strong></div>
          <div v-if="serviceHover.containerName" class="kv-item"><span>容器名</span><strong>{{ serviceHover.containerName }}</strong></div>
          <div v-if="serviceHover.containerId" class="kv-item"><span>容器 ID</span><strong>{{ serviceHover.containerId }}</strong></div>
          <div v-if="serviceHover.uid" class="kv-item"><span>UID</span><strong>{{ serviceHover.uid }}</strong></div>
          <div v-if="serviceHover.namespace" class="kv-item"><span>命名空间</span><strong>{{ serviceHover.namespace }}</strong></div>
          <div v-if="serviceHover.nodeName" class="kv-item"><span>节点</span><strong>{{ serviceHover.nodeName }}</strong></div>
          <div v-if="serviceHover.phase" class="kv-item"><span>状态</span><strong>{{ serviceHover.phase }}</strong></div>
          <div v-if="serviceHover.ownerDisplayName" class="kv-item"><span>归属</span><strong>{{ serviceHover.ownerDisplayName }}</strong></div>
        </div>
      </div>

      <div v-if="segment && !serviceHover" class="hover-block">
        <div class="hover-label">载体</div>
        <div class="kv-list">
          <div class="kv-item"><span>名称</span><strong>{{ segment.label || '-' }}</strong></div>
          <div class="kv-item"><span>部署方式</span><strong>{{ segment.caption || '-' }}</strong></div>
          <div class="kv-item"><span>显存占用</span><strong>{{ formatBytes(segment.summary?.memoryUsedBytes) }}</strong></div>
        </div>
      </div>

      <div v-if="occupancy && !serviceHover" class="hover-block">
        <div class="hover-label">占用概览</div>
        <div class="kv-list">
          <div class="kv-item"><span>载体数</span><strong>{{ occupancy.serviceCount || 0 }}</strong></div>
          <div class="kv-item"><span>显存占用</span><strong>{{ formatBytes(occupancy.memoryUsedBytes) }}</strong></div>
        </div>
        <div v-if="occupancy.serviceSummaries?.length" class="summary-list">
          <div v-for="item in occupancy.serviceSummaries" :key="item.id" class="summary-item">
            <strong>{{ item.displayName || item.name || item.id }}</strong>
            <span>{{ item.runtimeType || '-' }}</span>
            <span>PID {{ formatPidList(item.pids) }}</span>
            <span>{{ formatBytes(item.memoryUsedBytes) }}</span>
          </div>
        </div>
      </div>

      <div v-if="serviceHover?.descendantProcesses?.length" class="hover-block">
        <div class="hover-label">子孙进程显存</div>
        <div class="summary-list">
          <div v-for="process in serviceHover.descendantProcesses" :key="process.id" class="summary-item">
            <strong>PID {{ formatValue(process.observedPid) }}</strong>
            <span>归属 PID {{ formatValue(process.pid) }}</span>
            <span>{{ formatBytes(process.memoryUsedBytes) }}</span>
          </div>
        </div>
      </div>
    </div>
  </el-popover>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  title: {
    type: String,
    default: ''
  },
  entity: {
    type: Object,
    default: null
  },
  resourceInstance: {
    type: Object,
    default: null
  },
  service: {
    type: Object,
    default: null
  },
  segment: {
    type: Object,
    default: null
  },
  occupancy: {
    type: Object,
    default: null
  }
})

const gpuInfo = computed(() => props.segment?.gpuHover || props.resourceInstance)
const serviceHover = computed(() => props.segment?.serviceHover || props.service)
const popoverWidth = 'min(520px, calc(100vw - 32px))'

const formatValue = (value) => {
  if (Array.isArray(value)) return value.join(', ') || '-'
  return value == null || value === '' ? '-' : String(value)
}

const formatPidList = (values) => {
  if (!Array.isArray(values) || !values.length) return '-'
  return values.map(item => formatValue(item)).join(', ')
}

const formatPercent = (value) => {
  if (value == null || value === '') return '-'
  const number = Number(value)
  return Number.isFinite(number) ? `${number}%` : '-'
}

const formatTemperature = (value) => {
  if (value == null || value === '') return '-'
  const number = Number(value)
  return Number.isFinite(number) ? `${number}°C` : '-'
}

const formatBytes = (value) => {
  if (value == null || value === '') return '-'
  const size = Number(value)
  if (!Number.isFinite(size) || size < 0) return '-'
  if (size === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = size
  let unitIndex = 0
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024
    unitIndex += 1
  }
  return `${current >= 10 || unitIndex === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[unitIndex]}`
}
</script>

<style lang="scss" scoped>
.hover-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: calc(100vw - 32px);
  font-size: 12px;
  line-height: 1.45;
}

.hover-title {
  font-weight: 700;
  color: var(--text-primary);
}

.hover-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.hover-label {
  font-size: 11px;
  font-weight: 700;
  color: var(--text-secondary);
  text-transform: uppercase;
}

.kv-list {
  display: grid;
  grid-template-columns: 1fr;
  gap: 4px;
}

.kv-item {
  display: grid;
  grid-template-columns: minmax(72px, auto) minmax(0, 1fr);
  gap: 8px;
  align-items: start;

  span {
    color: var(--text-secondary);
    white-space: nowrap;
  }

  strong {
    color: var(--text-primary);
    text-align: right;
    word-break: break-all;
    min-width: 0;
  }
}

.summary-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.summary-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 2px;
  padding-top: 6px;
  border-top: 1px solid var(--border-color-lighter);
  align-items: start;

  &:first-child {
    padding-top: 0;
    border-top: 0;
  }

  strong {
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  span {
    color: var(--text-secondary);
    word-break: break-all;
  }
}
</style>
