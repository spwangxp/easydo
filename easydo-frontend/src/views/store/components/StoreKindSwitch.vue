<template>
  <div class="store-switch" role="tablist" aria-label="商店切换">
    <button
      type="button"
      class="store-switch__option"
      :class="{ active: modelValue === 'app' }"
      @click="$emit('update:modelValue', 'app')"
    >
      应用商店
    </button>
    <button
      type="button"
      class="store-switch__option"
      :class="{ active: modelValue === 'ai' }"
      @click="$emit('update:modelValue', 'ai')"
    >
      AI 模型
    </button>
    <button
      type="button"
      class="store-switch__option"
      :class="{ active: modelValue === 'ai-agent' }"
      @click="$emit('update:modelValue', 'ai-agent')"
    >
      AI Agent
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
.store-switch {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 38px;
  padding: 3px;
  border: 1px solid var(--border-color-light);
  border-radius: 12px;
  background: var(--bg-card);
  box-shadow: 0 8px 18px rgba(15, 23, 42, 0.06);
}

.store-switch__option {
  border: 0;
  min-height: 30px;
  padding: 5px 12px;
  border-radius: 9px;
  background: transparent;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 600;
  line-height: 1;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.18s ease, color 0.18s ease, box-shadow 0.18s ease;
}

.store-switch__option:hover {
  color: var(--primary-color);
}

.store-switch__option.active {
  background: var(--primary-color);
  color: #fff;
  box-shadow: 0 8px 18px rgba(37, 99, 235, 0.2);
}
</style>
