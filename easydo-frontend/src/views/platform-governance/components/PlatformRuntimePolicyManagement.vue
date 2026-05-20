<template>
  <div class="governance-card">
    <div class="section-header-row">
      <div>
        <h2 class="section-title">默认运行策略</h2>
        <div class="section-subtitle">当前版本用 Provider Model Binding 表达平台默认运行策略。</div>
      </div>
      <div class="header-actions">
        <el-select v-model="activeProviderId" class="provider-select" placeholder="选择供应商" clearable>
          <el-option v-for="provider in providers" :key="provider.id" :label="provider.name" :value="provider.id" />
        </el-select>
        <el-button type="primary" :disabled="!activeProviderId" @click="openBindingDialog()">新增策略</el-button>
      </div>
    </div>

    <div v-if="!providers.length" class="empty-hint">请先在“模型与供应商”中创建供应商。</div>
    <div v-else-if="!activeProviderId" class="empty-hint">请选择一个供应商查看默认运行策略。</div>

    <el-table v-else :data="bindings" style="width: 100%">
      <el-table-column label="模型" min-width="180">
        <template #default="{ row }">{{ modelName(row.model_id) }}</template>
      </el-table-column>
      <el-table-column prop="provider_model_key" label="Provider Model Key" min-width="220" show-overflow-tooltip />
      <el-table-column prop="status" label="状态" width="120" />
      <el-table-column label="操作" width="180" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openBindingDialog(row)">编辑</el-button>
          <el-button link type="danger" @click="removeBinding(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="bindingForm.id ? '编辑默认运行策略' : '新增默认运行策略'" width="640px">
      <el-form :model="bindingForm" label-width="150px">
        <el-form-item label="模型">
          <el-select v-model="bindingForm.model_id" style="width: 100%" clearable>
            <el-option v-for="model in models" :key="model.id" :label="model.display_name || model.name" :value="model.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="Provider Model Key">
          <el-input v-model="bindingForm.provider_model_key" />
        </el-form-item>
        <el-form-item label="Settings JSON">
          <el-input v-model="bindingForm.settings_json" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="Metadata JSON">
          <el-input v-model="bindingForm.metadata_json" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="bindingForm.status" style="width: 100%">
            <el-option label="active" value="active" />
            <el-option label="disabled" value="disabled" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitBinding">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createAIModelBinding,
  deleteAIModelBinding,
  getAIModelBindings,
  getAIModelCatalog,
  getAIProviders,
  updateAIModelBinding
} from '@/api/store'

const providers = ref([])
const models = ref([])
const bindings = ref([])
const activeProviderId = ref(undefined)
const dialogVisible = ref(false)
const saving = ref(false)
const bindingForm = reactive({
  id: 0,
  model_id: undefined,
  provider_model_key: '',
  settings_json: '{}',
  metadata_json: '{}',
  status: 'active'
})

const extractList = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.models)) return payload.models
  return []
}

const modelName = (modelId) => {
  const model = models.value.find(item => Number(item.id) === Number(modelId))
  return model?.display_name || model?.name || modelId || '-'
}

const parseJsonField = (label, value, fallback) => {
  if (!value) return fallback
  try {
    return JSON.parse(value)
  } catch (error) {
    throw new Error(`${label} JSON 格式错误`)
  }
}

const loadBaseData = async () => {
  try {
    const [providersRes, modelsRes] = await Promise.all([
      getAIProviders(),
      getAIModelCatalog()
    ])
    providers.value = extractList(providersRes?.data)
    models.value = extractList(modelsRes?.data)
    if (!providers.value.some(item => Number(item.id) === Number(activeProviderId.value))) {
      activeProviderId.value = providers.value[0]?.id
    }
    if (!activeProviderId.value) {
      bindings.value = []
      return
    }
    await loadBindings(activeProviderId.value)
  } catch (error) {
    ElMessage.error('加载默认运行策略失败')
  }
}

const loadBindings = async (providerId) => {
  if (!providerId) {
    bindings.value = []
    return
  }
  try {
    const res = await getAIModelBindings(providerId)
    bindings.value = extractList(res?.data)
  } catch (error) {
    ElMessage.error('加载默认运行策略失败')
  }
}

const openBindingDialog = (row = null) => {
  bindingForm.id = row?.id || 0
  bindingForm.model_id = row?.model_id || undefined
  bindingForm.provider_model_key = row?.provider_model_key || ''
  bindingForm.settings_json = row?.settings_json || '{}'
  bindingForm.metadata_json = row?.metadata_json || '{}'
  bindingForm.status = row?.status || 'active'
  dialogVisible.value = true
}

const submitBinding = async () => {
  if (!activeProviderId.value || !bindingForm.model_id) {
    ElMessage.warning('请选择模型')
    return
  }
  saving.value = true
  try {
    const payload = {
      model_id: bindingForm.model_id,
      provider_model_key: bindingForm.provider_model_key,
      settings_json: parseJsonField('Settings', bindingForm.settings_json, {}),
      metadata_json: parseJsonField('Metadata', bindingForm.metadata_json, {}),
      status: bindingForm.status
    }
    const res = bindingForm.id
      ? await updateAIModelBinding(activeProviderId.value, bindingForm.id, payload)
      : await createAIModelBinding(activeProviderId.value, payload)
    if (res.code === 200) {
      dialogVisible.value = false
      await loadBindings(activeProviderId.value)
      ElMessage.success(bindingForm.id ? '默认运行策略已更新' : '默认运行策略已创建')
      return
    }
    ElMessage.error(res.message || '保存默认运行策略失败')
  } catch (error) {
    ElMessage.error(error?.message || error?.response?.data?.message || '保存默认运行策略失败')
  } finally {
    saving.value = false
  }
}

const removeBinding = async (row) => {
  if (!activeProviderId.value) {
    return
  }
  try {
    await ElMessageBox.confirm(`确认删除默认运行策略 ${modelName(row.model_id)} 吗？`, '确认操作', { type: 'warning' })
    const res = await deleteAIModelBinding(activeProviderId.value, row.id)
    if (res.code === 200) {
      await loadBindings(activeProviderId.value)
      ElMessage.success('默认运行策略已删除')
      return
    }
    ElMessage.error(res.message || '删除默认运行策略失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '删除默认运行策略失败')
    }
  }
}

watch(activeProviderId, async (value, previousValue) => {
  if (value === previousValue) {
    return
  }
  await loadBindings(value)
})

onMounted(() => {
  loadBaseData()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.governance-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.section-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.section-subtitle {
  margin-top: 6px;
  color: var(--text-secondary);
  font-size: 14px;
}

.header-actions {
  display: flex;
  gap: 12px;
  align-items: center;
}

.provider-select {
  width: 260px;
}

.empty-hint {
  padding: 24px;
  background: var(--bg-card);
  border-radius: $radius-lg;
  color: var(--text-secondary);
  text-align: center;
}
</style>
