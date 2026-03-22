<template>
  <section class="sidebar-block">
    <header class="sidebar-block__header">
      <div>
        <h2>{{ title }}</h2>
        <p v-if="description">{{ description }}</p>
      </div>
      <el-tag v-if="badge !== undefined && badge !== null" size="small" :type="badgeType">{{ badge }}</el-tag>
    </header>

    <div class="sidebar-block__body">
      <slot />
    </div>
  </section>
</template>

<script setup>
defineProps({
  title: {
    type: String,
    required: true
  },
  description: {
    type: String,
    default: ''
  },
  badge: {
    type: [String, Number],
    default: null
  },
  badgeType: {
    type: String,
    default: 'info'
  }
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.sidebar-block {
  display: flex;
  flex-direction: column;
  min-height: 0;
  padding: $space-4;
  border-radius: $radius-xl;
  background: var(--bg-card);
  border: 1px solid var(--border-color-light);
  box-shadow: var(--shadow-md);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
}

.sidebar-block__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: $space-3;
  margin-bottom: $space-4;

  h2 {
    margin: 0;
    font-family: $font-family-display;
    font-size: 18px;
    line-height: 1.2;
    font-weight: 700;
    color: var(--text-primary);
  }

  p {
    margin: $space-1 0 0;
    color: var(--text-muted);
    font-size: 12px;
    line-height: 1.6;
  }
}

.sidebar-block__body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
</style>
