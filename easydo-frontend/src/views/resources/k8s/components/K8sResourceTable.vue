<template>
  <section class="card-shell" v-loading="loading">
    <div class="section-header">
      <div>
        <h2 class="section-title">{{ namespace ? `${namespace} · 资源浏览` : '资源浏览' }}</h2>
        <p class="section-description">浏览指定命名空间中的核心工作负载与配置资源；执行类入口仅保留安全动作。</p>
      </div>
      <el-button :loading="querying" @click="$emit('refresh')">刷新资源</el-button>
    </div>

    <div class="toolbar">
      <el-select
        :model-value="selectedKinds"
        multiple
        collapse-tags
        collapse-tags-tooltip
        placeholder="选择资源类型"
        class="kind-select"
        @update:model-value="$emit('update:selectedKinds', $event)"
      >
        <el-option v-for="option in K8S_KIND_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
      </el-select>
      <el-input
        :model-value="keyword"
        placeholder="搜索资源名称 / 类型"
        clearable
        class="keyword-input"
        @update:model-value="$emit('update:keyword', $event)"
      />
    </div>

    <el-table v-if="items.length > 0" :data="items" row-key="uid" style="width: 100%">
      <el-table-column label="类型" width="140">
        <template #default="{ row }">
          <el-tag :type="getKindTagType(row.kind)">{{ row.kind }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="名称" min-width="220">
        <template #default="{ row }">
          <div class="resource-name-cell">
            <span class="resource-name">{{ row.name }}</span>
            <span class="resource-meta">{{ row.namespace || '-' }} · {{ formatRelativeAge(row.createdAt) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="状态" min-width="160">
        <template #default="{ row }">
          <span class="resource-status">{{ row.statusText }}</span>
        </template>
      </el-table-column>
      <el-table-column label="摘要" min-width="280">
        <template #default="{ row }">
          <span class="resource-summary">{{ row.summaryText }}</span>
        </template>
      </el-table-column>
      <el-table-column v-if="canOperate" label="操作" width="140" fixed="right">
        <template #default="{ row }">
          <el-button
            link
            type="warning"
            :disabled="row.actionOptions.length === 0"
            @click="$emit('request-action', row)"
          >
            安全操作
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-empty
      v-else
      :description="namespace ? '当前筛选条件下暂无资源' : '请先选择命名空间后再浏览资源'"
      :image-size="76"
    />
  </section>
</template>

<script setup>
import { formatRelativeAge, getKindTagType, K8S_KIND_OPTIONS } from '../utils'

defineProps({
  namespace: {
    type: String,
    default: ''
  },
  items: {
    type: Array,
    default: () => []
  },
  selectedKinds: {
    type: Array,
    default: () => []
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
  },
  canOperate: {
    type: Boolean,
    default: false
  }
})

defineEmits(['refresh', 'request-action', 'update:selectedKinds', 'update:keyword'])
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

.toolbar {
  display: flex;
  gap: $space-3;
  flex-wrap: wrap;
  margin-bottom: $space-4;
}

.kind-select {
  width: 320px;
}

.keyword-input {
  width: 260px;
}

.resource-name-cell {
  display: flex;
  flex-direction: column;
  gap: $space-1;
}

.resource-name {
  font-weight: 700;
  color: var(--text-primary);
}

.resource-meta,
.resource-summary {
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.6;
}

.resource-status {
  color: var(--text-primary);
  font-weight: 600;
}

@media (max-width: 768px) {
  .section-header {
    flex-direction: column;
  }

  .kind-select,
  .keyword-input {
    width: 100%;
  }
}
</style>
