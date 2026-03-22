<template>
  <TerminalSidebarBlock title="打开会话" description="当前页面内已创建的终端会话。" :badge="sessions.length">
    <div v-if="sessions.length > 0" class="session-list">
      <div
        v-for="session in sessions"
        :key="session.sessionId"
        :data-testid="`terminal-session-${session.sessionId}`"
        class="session-item"
        :class="{ active: session.sessionId === activeSessionId }"
        role="button"
        tabindex="0"
        @click="$emit('select', session.sessionId)"
        @keyup.enter="$emit('select', session.sessionId)"
      >
        <div class="session-main">
          <span class="session-name">{{ session.label }}</span>
          <span class="session-endpoint">{{ session.endpoint }}</span>
        </div>

        <div class="session-side">
          <span class="session-id">{{ session.shortSessionId }}</span>
          <button
            class="close-button"
            type="button"
            :disabled="closingSessionId === session.sessionId"
            @click.stop="$emit('close', session.sessionId)"
          >
            关闭
          </button>
        </div>
      </div>
    </div>

    <el-empty v-else description="暂无打开会话" :image-size="72" />
  </TerminalSidebarBlock>
</template>

<script setup>
import TerminalSidebarBlock from './TerminalSidebarBlock.vue'

defineProps({
  sessions: {
    type: Array,
    default: () => []
  },
  activeSessionId: {
    type: String,
    default: ''
  },
  closingSessionId: {
    type: String,
    default: ''
  }
})

defineEmits(['select', 'close'])
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.session-list {
  display: flex;
  flex-direction: column;
  gap: $space-3;
  height: 100%;
  overflow-y: auto;
  padding-right: $space-1;
}

.session-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $space-3;
  padding: $space-3;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);
  cursor: pointer;
  transition: border-color $transition-fast, transform $transition-fast, box-shadow $transition-fast, background $transition-fast;

  &:hover {
    transform: translateY(-1px);
    border-color: var(--border-color-hover);
    box-shadow: var(--shadow-sm);
  }

  &.active {
    border-color: var(--border-color-hover);
    background: var(--primary-lighter);
    box-shadow: 0 0 0 1px rgba($primary-color, 0.12), var(--shadow-sm);
  }
}

.session-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: $space-1;
}

.session-name {
  color: var(--text-primary);
  font-weight: 700;
  line-height: 1.4;
}

.session-endpoint,
.session-id {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.session-side {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: $space-2;
  flex-shrink: 0;
}

.close-button {
  border: none;
  background: transparent;
  color: var(--danger-color);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;

  &:disabled {
    cursor: not-allowed;
    color: var(--text-muted);
  }
}
</style>
