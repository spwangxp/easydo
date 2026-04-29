<template>
  <div class="store-kind-switch" role="tablist" aria-label="商店切换">
    <button
      type="button"
      class="store-kind-option"
      :class="{ active: modelValue === 'app' }"
      @click="$emit('update:modelValue', 'app')"
    >
      应用商店
    </button>
    <span class="store-kind-divider" aria-hidden="true"></span>
    <button
      type="button"
      class="store-kind-option"
      :class="{ active: modelValue === 'ai' }"
      @click="$emit('update:modelValue', 'ai')"
    >
      AI 商店
    </button>
  </div>
</template>

<script setup>
defineProps({
  modelValue: {
    type: String,
    required: true
  }
})

defineEmits(['update:modelValue'])
</script>

<style scoped>
.store-kind-switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.store-kind-option {
  border: 0;
  padding: 0;
  background: transparent;
  color: var(--text-secondary);
  font-size: 15px;
  font-weight: 400;
  line-height: 1.15;
  cursor: pointer;
  transition: color 0.2s ease, font-size 0.2s ease, font-weight 0.2s ease, opacity 0.2s ease;
}

.store-kind-option.active {
  color: var(--text-primary);
  font-size: 28px;
  font-weight: 760;
  opacity: 1;
}

.store-kind-option:not(.active) {
  opacity: 0.6;
}

.store-kind-divider {
  width: 1px;
  height: 16px;
  background: var(--border-color-light);
  opacity: 0.6;
}
</style>
