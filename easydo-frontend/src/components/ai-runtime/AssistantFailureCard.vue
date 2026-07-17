<template>
  <section class="assistant-failure-card" :class="`assistant-failure-card--${details.status}`" role="alert">
    <div class="assistant-failure-card__header">
      <strong>{{ details.statusLabel }}</strong>
      <div v-if="details.code || details.category" class="assistant-failure-card__labels">
        <code v-if="details.code">{{ details.code }}</code>
        <span v-if="details.category">{{ details.category }}</span>
      </div>
    </div>
    <p class="assistant-failure-card__reason">{{ details.reason }}</p>
    <div v-if="details.requestId || details.runtimeRunId || details.occurredAt" class="assistant-failure-card__meta">
      <span v-if="details.requestId">request: {{ details.requestId }}</span>
      <span v-if="details.runtimeRunId">run: {{ details.runtimeRunId }}</span>
      <span v-if="details.occurredAt">{{ details.occurredAt }}</span>
    </div>
    <div v-if="details.runtimeRunId || canContinue || canRetry" class="assistant-failure-card__actions">
      <el-button
        v-if="details.runtimeRunId"
        size="small"
        text
        :icon="Refresh"
        :loading="refreshing"
        @click="emit('refresh')"
      >
        刷新轨迹
      </el-button>
      <el-button
        v-if="canRetry"
        size="small"
        type="primary"
        plain
        :icon="Position"
        :loading="continuing"
        @click="emit('retry')"
      >
        重试
      </el-button>
      <el-button
        v-else-if="canContinue"
        size="small"
        type="primary"
        plain
        :icon="Position"
        :loading="continuing"
        @click="emit('continue')"
      >
        继续
      </el-button>
    </div>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { Position, Refresh } from '@element-plus/icons-vue'
import { assistantFailureDetails, entryRuntimeRunId } from './assistantFailure'

const props = defineProps({
  entry: { type: Object, required: true },
  refreshing: { type: Boolean, default: false },
  canContinue: { type: Boolean, default: false },
  canRetry: { type: Boolean, default: false },
  continuing: { type: Boolean, default: false }
})
const emit = defineEmits(['refresh', 'continue', 'retry'])
const details = computed(() => {
  const base = assistantFailureDetails(props.entry)
  return {
    ...base,
    runtimeRunId: entryRuntimeRunId(props.entry) || base.runtimeRunId
  }
})
</script>

<style scoped>
.assistant-failure-card {
  display: grid;
  gap: 10px;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--danger-color) 32%, var(--border-color-light));
  border-radius: var(--border-radius-base);
  background: var(--status-danger-soft);
  color: var(--text-primary);
}

.assistant-failure-card--cancelled {
  border-color: var(--border-color-medium);
  background: var(--surface-subtle);
}

.assistant-failure-card__header,
.assistant-failure-card__actions,
.assistant-failure-card__labels,
.assistant-failure-card__meta {
  display: flex;
  align-items: center;
}

.assistant-failure-card__header {
  justify-content: space-between;
  gap: 12px;
  color: var(--danger-color);
}

.assistant-failure-card--cancelled .assistant-failure-card__header {
  color: var(--text-secondary);
}

.assistant-failure-card__labels {
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
  color: var(--text-secondary);
  font-size: 12px;
}

.assistant-failure-card__labels code,
.assistant-failure-card__labels span {
  padding: 2px 6px;
  border: 1px solid var(--border-color-light);
  border-radius: var(--border-radius-small);
  background: var(--surface-overlay);
}

.assistant-failure-card__reason {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.6;
}

.assistant-failure-card__meta {
  flex-wrap: wrap;
  gap: 8px 12px;
  color: var(--text-secondary);
  font-size: 12px;
  font-family: var(--font-family-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
}

.assistant-failure-card__actions {
  justify-content: flex-end;
  gap: 8px;
}
</style>
