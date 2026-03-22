<template>
  <section class="card-shell namespace-panel" v-loading="loading">
    <div class="section-header">
      <div>
        <h2 class="section-title">命名空间</h2>
        <p class="section-description">先定位命名空间，再继续浏览当前集群中的业务资源。</p>
      </div>
      <el-button :loading="querying" @click="$emit('refresh')">刷新</el-button>
    </div>

    <el-input
      :model-value="keyword"
      placeholder="搜索命名空间"
      clearable
      class="namespace-search"
      @update:model-value="$emit('update:keyword', $event)"
    />

    <div v-if="namespaces.length > 0" class="namespace-list">
      <button
        v-for="item in namespaces"
        :key="item.uid"
        class="namespace-item"
        :class="{ active: item.name === selectedNamespace }"
        type="button"
        @click="$emit('select', item.name)"
      >
        <div class="namespace-main">
          <span class="namespace-name">{{ item.name }}</span>
          <span class="namespace-meta">{{ item.phase }} · {{ formatRelativeAge(item.createdAt) }}</span>
        </div>
        <el-tag size="small" :type="item.name === 'default' ? 'success' : 'info'">{{ item.name === 'default' ? '默认' : '命名空间' }}</el-tag>
      </button>
    </div>

    <el-empty v-else :description="querying ? '正在加载命名空间…' : '当前集群暂无可浏览的命名空间'" :image-size="72" />
  </section>
</template>

<script setup>
import { formatRelativeAge } from '../utils'

defineProps({
  namespaces: {
    type: Array,
    default: () => []
  },
  selectedNamespace: {
    type: String,
    default: ''
  },
  keyword: {
    type: String,
    default: ''
  },
  loading: {
    type: Boolean,
    default: false
  },
  querying: {
    type: Boolean,
    default: false
  }
})

defineEmits(['refresh', 'select', 'update:keyword'])
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

.namespace-panel {
  display: flex;
  flex-direction: column;
  gap: $space-4;
  min-height: 100%;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: $space-3;
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

.namespace-search {
  width: 100%;
}

.namespace-list {
  display: flex;
  flex-direction: column;
  gap: $space-3;
  min-height: 0;
  overflow-y: auto;
}

.namespace-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $space-3;
  padding: $space-3 $space-4;
  border-radius: $radius-lg;
  border: 1px solid var(--border-color-light);
  background: var(--bg-elevated);
  cursor: pointer;
  transition: transform $transition-fast, border-color $transition-fast, box-shadow $transition-fast, background $transition-fast;

  &:hover {
    transform: translateY(-1px);
    border-color: var(--border-color-hover);
    box-shadow: var(--shadow-sm);
  }

  &.active {
    border-color: rgba($primary-color, 0.34);
    background: rgba($primary-color, 0.08);
    box-shadow: 0 0 0 1px rgba($primary-color, 0.14), var(--shadow-sm);
  }
}

.namespace-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: $space-1;
  text-align: left;
}

.namespace-name {
  font-weight: 700;
  color: var(--text-primary);
}

.namespace-meta {
  font-size: 12px;
  color: var(--text-muted);
}

@media (max-width: 768px) {
  .section-header {
    flex-direction: column;
  }
}
</style>
