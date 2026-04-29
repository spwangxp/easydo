<template>
  <div class="store-header-actions">
    <slot />
  </div>
</template>

<style scoped>
.store-header-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
</style>
