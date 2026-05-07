<template>
  <el-popover placement="top" :width="360" trigger="hover" popper-class="gpu-hover-popper">
    <template #reference>
      <slot />
    </template>

    <div class="hover-card">
      <div class="hover-title">{{ title || '-' }}</div>

      <div v-if="resourceInstance" class="hover-block">
        <div class="hover-label">GPU</div>
        <div class="kv-list">
          <div class="kv-item"><span>名称</span><strong>{{ resourceInstance.displayName }}</strong></div>
          <div class="kv-item"><span>节点</span><strong>{{ resourceInstance.entity?.name || '-' }}</strong></div>
          <div class="kv-item"><span>型号</span><strong>{{ resourceInstance.specMap?.model || '-' }}</strong></div>
          <div class="kv-item"><span>厂商</span><strong>{{ resourceInstance.specMap?.vendor || '-' }}</strong></div>
          <div class="kv-item"><span>索引</span><strong>{{ formatValue(resourceInstance.identityMap?.index) }}</strong></div>
          <div class="kv-item"><span>UUID</span><strong>{{ formatValue(resourceInstance.identityMap?.uuid) }}</strong></div>
          <div class="kv-item"><span>显存</span><strong>{{ formatBytes(getMeasureValue(resourceInstance.capacityMap?.memoryBytes, ['capacity', 'allocatable', 'total', 'value'])) }}</strong></div>
          <div class="kv-item"><span>已用显存</span><strong>{{ formatBytes(getMeasureValue(resourceInstance.metricsMap?.memoryBytesUsed, ['value', 'used'])) }}</strong></div>
          <div class="kv-item"><span>利用率</span><strong>{{ formatPercent(getMeasureValue(resourceInstance.metricsMap?.utilizationGpuPercent, ['value'])) }}</strong></div>
        </div>
      </div>

      <div v-if="service" class="hover-block">
        <div class="hover-label">服务</div>
        <div class="kv-list">
          <div class="kv-item"><span>名称</span><strong>{{ service.displayName }}</strong></div>
          <div class="kv-item"><span>实体</span><strong>{{ service.entity?.name || '-' }}</strong></div>
          <div class="kv-item" v-for="field in service.fields || []" :key="`service-${field.name}`"><span>{{ field.name }}</span><strong>{{ formatValue(field.value) }}</strong></div>
        </div>
      </div>

      <div v-if="claims?.length" class="hover-block">
        <div class="hover-label">Claims</div>
        <div class="claim-list">
          <div v-for="claim in claims" :key="claim.id" class="claim-item">
            <span class="claim-id">{{ claim.allocationId }}</span>
            <span class="claim-meta">{{ formatClaim(claim) }}</span>
          </div>
        </div>
      </div>
    </div>
  </el-popover>
</template>

<script setup>
defineProps({
  title: {
    type: String,
    default: ''
  },
  resourceInstance: {
    type: Object,
    default: null
  },
  service: {
    type: Object,
    default: null
  },
  claims: {
    type: Array,
    default: () => []
  }
})

const getMeasureValue = (measure, keys) => {
  if (!measure) return null
  for (const key of keys) {
    const value = Number(measure[key])
    if (Number.isFinite(value)) return value
  }
  return null
}

const formatValue = (value) => {
  if (Array.isArray(value)) return value.join(', ') || '-'
  return value == null || value === '' ? '-' : String(value)
}

const formatPercent = (value) => {
  if (value == null || value === '') return '-'
  const number = Number(value)
  return Number.isFinite(number) ? `${number}%` : '-'
}

const formatBytes = (value) => {
  if (value == null || value === '') return '-'
  const size = Number(value)
  if (!Number.isFinite(size)) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = size
  let unitIndex = 0
  while (current >= 1024 && unitIndex < units.length - 1) {
    current /= 1024
    unitIndex += 1
  }
  return `${current >= 10 || unitIndex === 0 ? current.toFixed(0) : current.toFixed(1)} ${units[unitIndex]}`
}

const formatClaim = (claim) => {
  const parts = (claim?.dimensions || []).map(item => `${item.name}=${formatValue(item.value)}`)
  return parts.join(' · ') || '-'
}
</script>

<style lang="scss" scoped>
.hover-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
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
  display: flex;
  justify-content: space-between;
  gap: 12px;

  span {
    color: var(--text-secondary);
    white-space: nowrap;
  }

  strong {
    color: var(--text-primary);
    text-align: right;
    word-break: break-all;
  }
}

.claim-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.claim-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding-top: 6px;
  border-top: 1px solid var(--border-color-lighter);

  &:first-child {
    padding-top: 0;
    border-top: 0;
  }
}

.claim-id {
  font-weight: 600;
  color: var(--text-primary);
}

.claim-meta {
  color: var(--text-secondary);
  word-break: break-word;
}
</style>
