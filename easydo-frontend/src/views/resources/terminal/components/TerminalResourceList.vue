<template>
  <TerminalSidebarBlock title="VM 资源列表" description="点击 VM 资源即可在当前页面中新增会话。" :badge="resources.length" badge-type="success">
    <div v-if="resources.length > 0" class="resource-list">
      <button
        v-for="resource in resources"
        :key="resource.id"
        :data-testid="`terminal-resource-${resource.id}`"
        class="resource-item"
        :class="{ active: resource.id === activeResourceId }"
        type="button"
        :disabled="creatingResourceId === resource.id"
        @click="$emit('open', resource.id)"
      >
        <div class="resource-main">
          <span class="resource-name">{{ resource.name }}</span>
          <span class="resource-endpoint">{{ resource.endpoint || '-' }}</span>
        </div>

        <el-tag size="small" :type="statusTagType(resource.status)">{{ resource.status || '-' }}</el-tag>
      </button>
    </div>

    <el-empty v-else description="当前工作空间暂无 VM 资源" :image-size="72" />
  </TerminalSidebarBlock>
</template>

<script setup>
import TerminalSidebarBlock from './TerminalSidebarBlock.vue'

defineProps({
  resources: {
    type: Array,
    default: () => []
  },
  activeResourceId: {
    type: Number,
    default: 0
  },
  creatingResourceId: {
    type: Number,
    default: 0
  }
})

defineEmits(['open'])

const statusTagType = (status) => ({
  online: 'success',
  offline: 'info',
  error: 'danger',
  archived: 'warning'
}[status] || 'info')
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.resource-list {
  display: flex;
  flex-direction: column;
  gap: $space-3;
  height: 100%;
  overflow-y: auto;
  padding-right: $space-1;
}

.resource-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $space-3;
  width: 100%;
  padding: $space-3;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);
  cursor: pointer;
  transition: border-color $transition-fast, transform $transition-fast, box-shadow $transition-fast, background $transition-fast;

  &:hover:not(:disabled) {
    transform: translateY(-1px);
    border-color: var(--border-color-hover);
    box-shadow: var(--shadow-sm);
  }

  &:disabled {
    cursor: wait;
    opacity: 0.7;
  }

  &.active {
    border-color: rgba($success-color, 0.3);
    background: var(--success-light);
  }
}

.resource-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: $space-1;
  text-align: left;
}

.resource-name {
  color: var(--text-primary);
  font-weight: 700;
  line-height: 1.4;
}

.resource-endpoint {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}
</style>
