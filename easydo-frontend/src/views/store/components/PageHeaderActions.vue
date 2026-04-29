<template>
  <div class="page-header-actions-group">
    <slot />
  </div>
</template>

<style scoped>
.page-header-actions-group {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.page-header-actions-group :deep(.el-button) {
  height: 42px;
  padding: 0 18px;
}

.page-header-actions-group :deep(.el-button--text) {
  padding: 0;
  height: auto;
  color: var(--primary-color);
}
</style>
