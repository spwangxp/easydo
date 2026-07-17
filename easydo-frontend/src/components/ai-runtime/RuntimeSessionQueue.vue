<template>
  <div v-loading="loading" class="runtime-session-queue">
    <div class="runtime-session-queue__title">队列</div>
    <div
      v-for="item in items"
      :key="item.queue_item_id"
      class="runtime-session-queue__item"
    >
      <div class="runtime-session-queue__meta">
        <el-tag size="small" effect="plain">{{ runtimeQueueModeLabel(item.mode) }}</el-tag>
        <el-tag size="small" :type="runtimeQueueStatusType(item.status)" effect="plain">
          {{ runtimeQueueStatusLabel(item.status) }}
        </el-tag>
      </div>
      <div class="runtime-session-queue__content">{{ item.content }}</div>
      <el-button
        v-if="item.status === 'pending'"
        link
        type="danger"
        size="small"
        @click="emit('cancel', item.queue_item_id)"
      >
        取消
      </el-button>
    </div>
  </div>
</template>

<script setup>
import {
  runtimeQueueModeLabel,
  runtimeQueueStatusLabel,
  runtimeQueueStatusType
} from './runtimeSessionQueue.js'

defineProps({
  items: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['cancel'])
</script>

<style scoped>
.runtime-session-queue {
  padding: 10px 12px;
  border: 1px solid var(--border-color-light);
  border-radius: 10px;
  background: var(--surface-muted);
  display: grid;
  gap: 8px;
}

.runtime-session-queue__title {
  font-size: 12px;
  color: var(--text-tertiary);
}

.runtime-session-queue__item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 4px 8px;
  align-items: start;
}

.runtime-session-queue__meta {
  grid-column: 1 / -1;
  display: flex;
  gap: 6px;
}

.runtime-session-queue__content {
  font-size: 13px;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
