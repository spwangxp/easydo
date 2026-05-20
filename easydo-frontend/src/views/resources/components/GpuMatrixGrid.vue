<template>
  <div class="gpu-grid">
    <div v-for="row in rows" :key="row.id" class="gpu-row">
      <div class="node-column">
        <GpuHoverPopover :title="row.displayName" :entity="row.entity">
          <div class="node-card">
            <strong>{{ row.displayName }}</strong>
            <span>{{ row.gpuCount }} GPU / {{ row.activeServiceCount }} 载体</span>
          </div>
        </GpuHoverPopover>
      </div>
      <div class="gpu-grid-scroll">
        <div class="gpu-cells" :style="gpuCellGridStyle(row)">
          <GpuHoverPopover
            v-for="cell in row.gpuCells"
            :key="cell.id"
            :title="cell.resourceInstance.displayName"
            :entity="row.entity"
            :resource-instance="cell.gpuHover"
            :occupancy="cell.occupancy"
          >
            <div class="gpu-cell">
              <div class="gpu-metrics">
                <span class="gpu-title">GPU {{ cell.resourceInstance.identityMap?.index ?? '-' }}</span>
                <span class="gpu-metric">{{ formatTemperature(cell.resourceInstance.metricsMap?.temperatureGpuCelsius?.value) }}</span>
                <span class="gpu-metric">{{ formatBytes(gpuUsedMemory(cell.resourceInstance, cell.occupancy)) }}</span>
                <span class="gpu-metric">/ {{ formatBytes(totalMemory(cell.resourceInstance)) }}</span>
              </div>
              <div class="gpu-segments" :class="{ 'gpu-segments--empty': !cell.segments.length }">
                <span v-if="!cell.segments.length" class="gpu-empty">空闲</span>
              </div>
            </div>
          </GpuHoverPopover>
          <template v-for="(lane, laneIndex) in gpuSegmentLanes(row)" :key="`${row.id}-lane-${laneIndex}`">
            <div
              v-for="segment in lane"
              :key="segment.id"
              class="gpu-segment-run"
              :style="gpuSegmentRunStyle(segment, laneIndex)"
            >
              <GpuHoverPopover
                :title="segment.label"
                :entity="row.entity"
                :resource-instance="segment.gpuHover"
                :service="segment.serviceHover"
                :segment="segment"
              >
                <span class="gpu-segment" :style="segmentStyle(segment)">
                  <span class="gpu-segment-main">
                    <strong>{{ segment.label }}</strong>
                    <span v-if="segment.caption" class="gpu-segment-caption">{{ segment.caption }}</span>
                  </span>
                  <span class="gpu-segment-memory">
                    <span v-if="segment.span > 1" class="gpu-segment-breakdown">{{ formatGpuMemoryBreakdown(segment) }}</span>
                    <span>{{ formatBytes(segment.summary?.memoryUsedBytes) }}</span>
                  </span>
                </span>
              </GpuHoverPopover>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

import GpuHoverPopover from './GpuHoverPopover.vue'
import { buildGpuSegmentLanes } from './gpuSegmentRuns.js'

const props = defineProps({
  rows: {
    type: Array,
    default: () => []
  }
})

const colorPalette = [
  '#409eff',
  '#67c23a',
  '#e6a23c',
  '#f56c6c',
  '#909399',
  '#8e44ad',
  '#16a085',
  '#d35400'
]

const segmentLanesByRowId = computed(() => {
  return new Map(props.rows.map(row => [row.id, buildGpuSegmentLanes(row.gpuCells || [])]))
})

const hashString = (value) => {
  const text = String(value || '')
  let hash = 0
  for (let index = 0; index < text.length; index += 1) {
    hash = ((hash << 5) - hash) + text.charCodeAt(index)
    hash |= 0
  }
  return Math.abs(hash)
}

const segmentStyle = (segment) => {
  const color = colorPalette[hashString(segment?.colorKey) % colorPalette.length]
  return {
    '--segment-color': color,
    borderColor: color,
    backgroundColor: `${color}1A`
  }
}

const gpuCellGridStyle = (row) => ({
  gridTemplateColumns: `repeat(${Math.max(row?.gpuCells?.length || 0, 1)}, minmax(136px, 1fr))`
})

const gpuSegmentLanes = (row) => segmentLanesByRowId.value.get(row.id) || []

const gpuSegmentRunStyle = (segment, laneIndex) => ({
  ...segmentStyle(segment),
  gridColumn: `${segment.startColumn} / span ${segment.span}`,
  gridRow: `${laneIndex + 2}`
})

const formatGpuMemoryBreakdown = (segment) => {
  const usages = segment?.gpuMemoryUsages || []
  if (!usages.length) return ''
  return `(${usages.map(item => `${item.gpuLabel}:${formatBytes(item.memoryUsedBytes)}`).join('  ')})`
}

const totalMemory = (resourceInstance) => Number(resourceInstance?.capacityMap?.memoryBytes?.capacity || resourceInstance?.capacityMap?.memoryBytes?.allocatable || resourceInstance?.capacityMap?.memoryBytes?.value || 0)

const gpuUsedMemory = (resourceInstance, occupancy) => Number(resourceInstance?.metricsMap?.memoryBytesUsed?.value || resourceInstance?.metricsMap?.memoryBytesUsed?.used || occupancy?.memoryUsedBytes || 0)

const formatTemperature = (value) => {
  const number = Number(value)
  return Number.isFinite(number) ? `${number}°C` : '-'
}

const formatBytes = (value) => {
  const size = Number(value || 0)
  if (!size) return '-'
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
.gpu-grid {
  --gpu-summary-row-height: 62px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.gpu-row {
  display: grid;
  grid-template-columns: 128px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
}

.node-column {
  min-width: 0;
  display: flex;
  align-items: flex-start;
}

.node-card {
  width: 100%;
  height: var(--gpu-summary-row-height);
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 2px;
  padding: 8px 10px;
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  background: var(--bg-card);
  overflow: hidden;

  strong {
    color: var(--text-primary);
    font-size: 12px;
    line-height: 1.25;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  span {
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.25;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.gpu-grid-scroll {
  min-width: 0;
  overflow-x: auto;
  padding-bottom: 2px;
}

.gpu-cells {
  display: grid;
  gap: 6px;
  min-width: max-content;
}

.gpu-cell {
  height: var(--gpu-summary-row-height);
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-height: var(--gpu-summary-row-height);
  padding: 6px;
  border: 1px solid var(--border-color-light);
  border-radius: 8px;
  background: var(--bg-card);
}

.gpu-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 6px;
  align-items: center;
  font-size: 11px;
  line-height: 1.25;
}

.gpu-title {
  font-weight: 700;
  color: var(--text-primary);
}

.gpu-metric {
  color: var(--text-secondary);
}

.gpu-segments {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.gpu-segments--empty {
  min-height: 28px;
  justify-content: center;
}

.gpu-segment {
  display: flex;
  justify-content: space-between;
  gap: 6px;
  align-items: center;
  padding: 3px 5px;
  border: 1px solid var(--segment-color);
  border-radius: 6px;
  font-size: 11px;
  line-height: 1.25;
  min-width: 0;

  strong {
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

}

.gpu-segment-run {
  min-width: 0;

  .gpu-segment {
    height: 100%;
  }
}

.gpu-segment-main {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  min-width: 0;
  overflow: hidden;
  white-space: normal;
  flex-shrink: 1;

  strong {
    white-space: normal;
  }
}

.gpu-segment-caption {
  color: var(--text-secondary);
  font-size: 10px;
  line-height: 1.2;
  padding: 1px 4px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.42);
  flex-shrink: 0;
}

.gpu-segment-memory {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  flex-shrink: 0;

  span {
    color: var(--text-secondary);
    white-space: nowrap;
  }
}

.gpu-segment-breakdown {
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
}

.gpu-empty {
  color: var(--text-placeholder);
  font-size: 11px;
}

@media (max-width: 768px) {
  .gpu-row {
    grid-template-columns: 1fr;
  }

  .node-card {
    height: auto;
    min-height: var(--gpu-summary-row-height);
  }
}
</style>
