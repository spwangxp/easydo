<template>
  <section class="page-header">
    <div class="page-header-main">
      <div class="page-header-title">
        <slot name="title" />
      </div>
      <div v-if="$slots.subtitle" class="page-header-subtitle">
        <slot name="subtitle" />
      </div>
    </div>
    <div v-if="$slots.actions" class="page-header-actions">
      <slot name="actions" />
    </div>
  </section>
</template>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 0 0 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.page-header-main {
  min-width: 0;
}

.page-header-title :deep(h1),
.page-header-title :deep(h2),
.page-header-title :deep(.store-kind-option.active) {
  margin: 0;
  font-family: "Outfit", "Plus Jakarta Sans", "PingFang SC", "Microsoft YaHei", sans-serif;
  color: var(--text-primary);
}

.page-header-title :deep(h1),
.page-header-title :deep(h2) {
  font-size: 28px;
  font-weight: 760;
  letter-spacing: -0.03em;
  line-height: 1.15;
}

.page-header-subtitle {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.page-header-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

@media (max-width: 720px) {
  .page-header {
    align-items: flex-start;
  }
}
</style>
