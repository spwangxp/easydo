<template>
  <div class="matrix-shell">
    <table class="matrix-table">
      <thead>
        <tr>
          <th class="service-col">服务</th>
          <th v-for="column in columns" :key="column.id" class="gpu-col">
            <GpuHoverPopover :title="column.displayName" :resource-instance="column" :claims="column.activeClaims">
              <div class="gpu-head">
                <strong>{{ getShortGpuTitle(column) }}</strong>
                <span>{{ column.entity?.name || '-' }}</span>
              </div>
            </GpuHoverPopover>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.id">
          <td class="service-col">
            <GpuHoverPopover :title="row.displayName" :service="row" :claims="row.activeClaims">
              <div class="service-head">
                <strong>{{ row.displayName }}</strong>
                <span>{{ getServiceMeta(row) }}</span>
              </div>
            </GpuHoverPopover>
          </td>
          <td v-for="column in columns" :key="`${row.id}-${column.id}`" class="matrix-cell">
            <GpuHoverPopover
              :title="`${row.displayName} · ${column.displayName}`"
              :service="row"
              :resource-instance="column"
              :claims="getCell(row.id, column.id).claims"
            >
              <div class="cell-chip" :class="{ active: getCell(row.id, column.id).claimCount > 0 }">
                <strong>{{ getCell(row.id, column.id).claimCount || '-' }}</strong>
                <span>{{ getCellMeta(getCell(row.id, column.id)) }}</span>
              </div>
            </GpuHoverPopover>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup>
import GpuHoverPopover from './GpuHoverPopover.vue'

const props = defineProps({
  rows: {
    type: Array,
    default: () => []
  },
  columns: {
    type: Array,
    default: () => []
  },
  cells: {
    type: Object,
    default: () => ({})
  }
})

const getCell = (serviceId, resourceInstanceId) => props.cells[`${serviceId}::${resourceInstanceId}`] || { claims: [], claimCount: 0, summary: {} }
const getShortGpuTitle = (column) => column.specMap?.model || column.identityMap?.name || `GPU ${column.identityMap?.index ?? '-'}`
const getServiceMeta = (row) => row.fields?.map(item => `${item.name}=${Array.isArray(item.value) ? item.value.join(',') : item.value}`).join(' · ') || '-'
const getCellMeta = (cell) => {
  if (cell.summary?.podUid) return `pod=${cell.summary.podUid}`
  if (cell.summary?.pid) return `pid=${cell.summary.pid}`
  return cell.claimCount ? 'allocated' : 'idle'
}
</script>

<style lang="scss" scoped>
.matrix-shell {
  overflow-x: auto;
}

.matrix-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  table-layout: fixed;
  font-size: 12px;

  th,
  td {
    padding: 6px;
    border: 1px solid var(--border-color-lighter);
    vertical-align: middle;
    background: var(--bg-card);
  }

  th {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--bg-elevated);
  }
}

.service-col {
  min-width: 220px;
  width: 220px;
}

.gpu-col {
  min-width: 140px;
  width: 140px;
}

.gpu-head,
.service-head,
.cell-chip {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.gpu-head strong,
.service-head strong,
.cell-chip strong {
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.25;
}

.gpu-head span,
.service-head span,
.cell-chip span {
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.25;
  word-break: break-word;
}

.matrix-cell {
  padding: 4px;
}

.cell-chip {
  min-height: 42px;
  justify-content: center;
  padding: 4px 6px;
  border-radius: 6px;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);

  &.active {
    border-color: var(--el-color-primary-light-5);
    background: rgba(64, 158, 255, 0.08);
  }
}
</style>
