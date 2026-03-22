<template>
  <el-dialog :model-value="modelValue" title="K8s 安全操作" width="560px" destroy-on-close @close="closeDialog">
    <div v-if="resourceItem" class="dialog-body">
      <el-alert type="warning" :closable="false" show-icon>
        所有执行动作都会落库到资源审计，建议在填写原因时明确变更背景与影响范围。
      </el-alert>

      <el-descriptions :column="2" border>
        <el-descriptions-item label="命名空间">{{ namespace || '-' }}</el-descriptions-item>
        <el-descriptions-item label="资源类型">{{ resourceItem.kind }}</el-descriptions-item>
        <el-descriptions-item label="资源名称" :span="2">{{ resourceItem.name }}</el-descriptions-item>
        <el-descriptions-item label="当前状态" :span="2">{{ resourceItem.statusText }} · {{ resourceItem.summaryText }}</el-descriptions-item>
      </el-descriptions>

      <el-form label-position="top" class="action-form">
        <el-form-item label="动作" required>
          <el-select v-model="form.action" style="width: 100%" placeholder="选择动作">
            <el-option v-for="option in actionOptions" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
        </el-form-item>

        <el-form-item v-if="requiresReplicas" label="目标副本数" required>
          <el-input-number v-model="form.replicas" :min="0" :step="1" style="width: 100%" />
        </el-form-item>

        <el-form-item label="执行原因" required>
          <el-input v-model="form.reason" type="textarea" :rows="4" maxlength="300" show-word-limit placeholder="例如：发布后实例未滚动，需手动重启 Deployment" />
        </el-form-item>
      </el-form>
    </div>

    <template #footer>
      <el-button @click="closeDialog">取消</el-button>
      <el-button type="primary" :loading="submitting" :disabled="!form.action" @click="handleSubmit">提交操作</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { resolveDefaultReplicas } from '../utils'

const props = defineProps({
  modelValue: {
    type: Boolean,
    default: false
  },
  namespace: {
    type: String,
    default: ''
  },
  resourceItem: {
    type: Object,
    default: null
  },
  submitting: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:modelValue', 'submit'])

const form = reactive({
  action: '',
  reason: '',
  replicas: 1
})

const actionOptions = computed(() => props.resourceItem?.actionOptions || [])
const currentAction = computed(() => actionOptions.value.find(item => item.value === form.action) || null)
const requiresReplicas = computed(() => currentAction.value?.needsReplicas === true)

const resetForm = () => {
  form.action = actionOptions.value[0]?.value || ''
  form.reason = ''
  form.replicas = resolveDefaultReplicas(props.resourceItem)
}

const closeDialog = () => {
  emit('update:modelValue', false)
}

const handleSubmit = () => {
  if (!form.action) {
    ElMessage.warning('当前资源没有可执行的安全操作')
    return
  }
  if (!form.reason.trim()) {
    ElMessage.warning('请填写执行原因')
    return
  }

  emit('submit', {
    namespace: props.namespace,
    target_kind: props.resourceItem?.kind,
    target_name: props.resourceItem?.name,
    action: form.action,
    reason: form.reason.trim(),
    replicas: requiresReplicas.value ? Number(form.replicas) : undefined
  })
}

watch(
  () => [props.modelValue, props.resourceItem?.uid].join(':'),
  () => {
    if (!props.modelValue) return
    resetForm()
  },
  { immediate: true }
)
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.dialog-body {
  display: flex;
  flex-direction: column;
  gap: $space-4;
}

.action-form {
  margin-top: $space-2;
}
</style>
