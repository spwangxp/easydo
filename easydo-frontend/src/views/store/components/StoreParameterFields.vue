<template>
  <div>
    <div v-if="basicFields.length > 0" class="parameter-grid">
      <div
        v-for="field in basicFields"
        :key="fieldKey(field)"
        class="parameter-field"
        :class="{ full: isFullWidthField(field) }"
      >
        <el-form-item :required="field.required">
          <template #label>
            <span class="parameter-label">
              <span>{{ fieldLabel(field) }}</span>
              <span v-if="fieldDescription(field)" class="parameter-recommendation">{{ fieldDescription(field) }}</span>
              <el-tooltip v-if="fieldExtraTip(field)" :content="fieldExtraTip(field)" placement="top" effect="dark">
                <el-icon class="parameter-help-icon"><QuestionFilled /></el-icon>
              </el-tooltip>
              <el-tag v-if="field.mutable === false" size="small" effect="plain">只读</el-tag>
            </span>
          </template>
          <el-input
            v-if="fieldType(field) === 'text' || fieldType(field) === 'password'"
            v-model="modelValue[fieldKey(field)]"
            :type="fieldType(field) === 'password' ? 'password' : 'text'"
            :show-password="fieldType(field) === 'password'"
            :placeholder="fieldPlaceholder(field)"
            :disabled="field.mutable === false"
          />
          <el-input
            v-else-if="fieldType(field) === 'textarea' || fieldType(field) === 'json'"
            v-model="modelValue[fieldKey(field)]"
            type="textarea"
            :rows="fieldRows(field)"
            :placeholder="fieldPlaceholder(field)"
            :disabled="field.mutable === false"
          />
          <el-input-number
            v-else-if="fieldType(field) === 'number'"
            v-model="modelValue[fieldKey(field)]"
            :min="field.min"
            :max="field.max"
            :step="fieldStep(field)"
            class="parameter-number"
            :disabled="field.mutable === false"
          />
          <el-switch
            v-else-if="fieldType(field) === 'switch'"
            v-model="modelValue[fieldKey(field)]"
            :disabled="field.mutable === false"
          />
          <el-select
            v-else-if="fieldType(field) === 'select'"
            v-model="modelValue[fieldKey(field)]"
            :placeholder="fieldPlaceholder(field) || '请选择'"
            style="width: 100%"
            :disabled="field.mutable === false"
          >
            <el-option
              v-for="option in fieldOptions(field)"
              :key="`${fieldKey(field)}-${option.value}`"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
          <el-input
            v-else
            v-model="modelValue[fieldKey(field)]"
            :placeholder="fieldPlaceholder(field)"
            :disabled="field.mutable === false"
          />
        </el-form-item>
      </div>
    </div>

    <el-collapse v-if="advancedFields.length > 0" v-model="advancedPanels" class="advanced-parameter-collapse">
      <el-collapse-item :title="advancedTitle" name="advanced">
        <div class="parameter-grid advanced-grid">
          <div
            v-for="field in advancedFields"
            :key="fieldKey(field)"
            class="parameter-field"
            :class="{ full: isFullWidthField(field) }"
          >
            <el-form-item :required="field.required">
              <template #label>
                <span class="parameter-label">
                  <span>{{ fieldLabel(field) }}</span>
                  <span v-if="fieldDescription(field)" class="parameter-recommendation">{{ fieldDescription(field) }}</span>
                  <el-tooltip v-if="fieldExtraTip(field)" :content="fieldExtraTip(field)" placement="top" effect="dark">
                    <el-icon class="parameter-help-icon"><QuestionFilled /></el-icon>
                  </el-tooltip>
                  <el-tag v-if="field.mutable === false" size="small" effect="plain">只读</el-tag>
                </span>
              </template>
              <el-input
                v-if="fieldType(field) === 'text' || fieldType(field) === 'password'"
                v-model="modelValue[fieldKey(field)]"
                :type="fieldType(field) === 'password' ? 'password' : 'text'"
                :show-password="fieldType(field) === 'password'"
                :placeholder="fieldPlaceholder(field)"
                :disabled="field.mutable === false"
              />
              <el-input
                v-else-if="fieldType(field) === 'textarea' || fieldType(field) === 'json'"
                v-model="modelValue[fieldKey(field)]"
                type="textarea"
                :rows="fieldRows(field)"
                :placeholder="fieldPlaceholder(field)"
                :disabled="field.mutable === false"
              />
              <el-input-number
                v-else-if="fieldType(field) === 'number'"
                v-model="modelValue[fieldKey(field)]"
                :min="field.min"
                :max="field.max"
                :step="fieldStep(field)"
                class="parameter-number"
                :disabled="field.mutable === false"
              />
              <el-switch
                v-else-if="fieldType(field) === 'switch'"
                v-model="modelValue[fieldKey(field)]"
                :disabled="field.mutable === false"
              />
              <el-select
                v-else-if="fieldType(field) === 'select'"
                v-model="modelValue[fieldKey(field)]"
                :placeholder="fieldPlaceholder(field) || '请选择'"
                style="width: 100%"
                :disabled="field.mutable === false"
              >
                <el-option
                  v-for="option in fieldOptions(field)"
                  :key="`${fieldKey(field)}-${option.value}`"
                  :label="option.label"
                  :value="option.value"
                />
              </el-select>
              <el-input
                v-else
                v-model="modelValue[fieldKey(field)]"
                :placeholder="fieldPlaceholder(field)"
                :disabled="field.mutable === false"
              />
            </el-form-item>
          </div>
        </div>
      </el-collapse-item>
    </el-collapse>

    <el-empty
      v-else-if="showEmpty && basicFields.length === 0 && advancedFields.length === 0"
      :description="emptyDescription"
      :image-size="76"
    />
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { QuestionFilled } from '@element-plus/icons-vue'

const modelValue = defineModel({ type: Object, required: true })

const props = defineProps({
  basicFields: { type: Array, default: () => [] },
  advancedFields: { type: Array, default: () => [] },
  showEmpty: { type: Boolean, default: false },
  emptyDescription: { type: String, default: '' },
  advancedTitle: { type: String, default: '高级配置' },
  defaultOpenAdvanced: { type: Boolean, default: false }
})

const advancedPanels = ref(props.defaultOpenAdvanced ? ['advanced'] : [])

watch(() => props.defaultOpenAdvanced, (value) => {
  advancedPanels.value = value ? ['advanced'] : []
})

function fieldKey(field = {}) {
  return field.key || field.name || ''
}

function fieldLabel(field = {}) {
  return field.label || fieldKey(field)
}

function fieldDescription(field = {}) {
  return field.description || ''
}

function fieldExtraTip(field = {}) {
  return field.extraTip || field.extra_tip || ''
}

function fieldType(field = {}) {
  return field.type || 'text'
}

function fieldPlaceholder(field = {}) {
  return field.placeholder || ''
}

function fieldRows(field = {}) {
  return field.rows || (fieldType(field) === 'json' ? 6 : 4)
}

function fieldStep(field = {}) {
  return field.step ?? 1
}

function fieldOptions(field = {}) {
  return Array.isArray(field.options) ? field.options : (Array.isArray(field.option_values) ? field.option_values : [])
}

function isFullWidthField(field = {}) {
  return Boolean(field.fullWidth || field.full_width || fieldType(field) === 'textarea' || fieldType(field) === 'json')
}
</script>

<style scoped>
.parameter-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.advanced-parameter-collapse {
  margin-top: 16px;
}

.parameter-field.full {
  grid-column: 1 / -1;
}

.parameter-label {
  display: inline-flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  line-height: 1.35;
}

.parameter-help-icon {
  color: var(--text-muted);
  cursor: help;
}

.parameter-recommendation {
  color: var(--text-muted);
}

.parameter-number {
  width: 100%;
}

:deep(.parameter-number .el-input__wrapper) {
  width: 100%;
}

:deep(.advanced-parameter-collapse .el-collapse-item__header) {
  min-height: 40px;
  font-size: 13px;
}

@media (max-width: 960px) {
  .parameter-grid,
  .advanced-grid {
    grid-template-columns: 1fr;
  }
}
</style>
