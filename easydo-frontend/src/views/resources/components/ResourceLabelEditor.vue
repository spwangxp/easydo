<template>
  <div class="resource-label-editor">
    <div v-for="(row, index) in rows" :key="row.id" class="label-row">
      <el-input
        v-model="row.key"
        :disabled="disabled"
        placeholder="标签键"
        class="label-input"
        @input="handleRowsChange"
      />
      <el-input
        v-model="row.value"
        :disabled="disabled"
        placeholder="标签值"
        class="label-input"
        @input="handleRowsChange"
      />
      <el-button
        :disabled="disabled"
        text
        type="danger"
        class="delete-button"
        @click="removeRow(index)"
      >
        删除
      </el-button>
    </div>

    <el-button :disabled="disabled" class="add-button" @click="addRow">
      添加标签
    </el-button>

    <div v-if="errorMessage" class="error-message">{{ errorMessage }}</div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import {
  labelObjectToRows,
  validateResourceLabelRows
} from '../resourceLabels'

const props = defineProps({
  modelValue: {
    type: Object,
    default: () => ({})
  },
  disabled: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue', 'valid-change'])

let rowIdSeed = 0
const createLabelRow = (key = '', value = '') => ({
  id: rowIdSeed += 1,
  key,
  value
})
const ensureRows = (sourceRows = []) => sourceRows.length > 0 ? sourceRows.map(row => createLabelRow(row.key || '', row.value || '')) : [createLabelRow()]
const serializeLabels = (labels = {}) => JSON.stringify(labels)

const rows = ref(ensureRows(labelObjectToRows(props.modelValue)))
const validationState = ref(validateResourceLabelRows(rows.value))
const lastModelSignature = ref(serializeLabels(validationState.value.ok ? validationState.value.labels : props.modelValue))

const errorMessage = computed(() => validationState.value.errors[0]?.message || '')

const validate = () => {
  const result = validateResourceLabelRows(rows.value)
  validationState.value = result
  emit('valid-change', result.ok)
  return result
}

const syncRowsFromModel = (modelValue) => {
  rows.value = ensureRows(labelObjectToRows(modelValue))
  validationState.value = validateResourceLabelRows(rows.value)
}

watch(
  () => props.modelValue,
  value => {
    const signature = serializeLabels(value)
    if (signature === lastModelSignature.value) return
    lastModelSignature.value = signature
    syncRowsFromModel(value)
  },
  { deep: true }
)

const handleRowsChange = () => {
  const result = validate()
  if (result.ok) {
    lastModelSignature.value = serializeLabels(result.labels)
    emit('update:modelValue', result.labels)
  }
}

const addRow = () => {
  rows.value.push(createLabelRow())
  handleRowsChange()
}

const removeRow = (index) => {
  rows.value.splice(index, 1)
  if (rows.value.length === 0) {
    rows.value.push(createLabelRow())
  }
  handleRowsChange()
}

defineExpose({ validate })
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.resource-label-editor {
  display: flex;
  flex-direction: column;
  gap: $space-3;
}

.label-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: $space-3;
  align-items: center;
}

.label-input {
  width: 100%;
}

.delete-button {
  justify-self: flex-end;
}

.add-button {
  align-self: flex-start;
}

.error-message {
  color: var(--el-color-danger);
  font-size: 12px;
  line-height: 1.5;
}

@media (max-width: 768px) {
  .label-row {
    grid-template-columns: 1fr;
  }

  .delete-button,
  .add-button {
    justify-self: flex-start;
  }
}
</style>
