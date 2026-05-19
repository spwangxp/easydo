<template>
  <div class="governance-card">
    <div class="stack-block">
      <div class="section-header-row">
        <div>
          <h2 class="section-title">模型目录</h2>
          <div class="section-subtitle">导入平台模型目录，并为供应商绑定模型做准备。</div>
        </div>
        <el-button type="primary" @click="importDialogVisible = true">导入模型</el-button>
      </div>

      <el-table :data="models" style="width: 100%">
        <el-table-column prop="display_name" label="显示名" min-width="180">
          <template #default="{ row }">{{ row.display_name || row.name }}</template>
        </el-table-column>
        <el-table-column prop="name" label="模型标识" min-width="180" />
        <el-table-column prop="source" label="来源" width="140" />
        <el-table-column prop="parameter_size" label="参数规模" width="140" />
      </el-table>
    </div>

    <div class="stack-block">
      <div class="section-header-row">
        <div>
          <h2 class="section-title">供应商</h2>
          <div class="section-subtitle">管理平台级 AI Provider，绑定关系在“默认运行策略”中维护。</div>
        </div>
        <el-button type="primary" @click="openProviderDialog()">新建供应商</el-button>
      </div>

      <el-table :data="providers" style="width: 100%">
        <el-table-column prop="name" label="名称" min-width="180" />
        <el-table-column prop="provider_type" label="类型" width="160" />
        <el-table-column prop="base_url" label="Endpoint" min-width="220" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="120" />
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openProviderDialog(row)">编辑</el-button>
            <el-button link type="danger" @click="removeProvider(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <el-dialog v-model="importDialogVisible" title="导入模型" width="560px">
      <el-form :model="importForm" label-width="130px">
        <el-form-item label="来源">
          <el-select v-model="importForm.source" style="width: 100%">
            <el-option label="huggingface" value="huggingface" />
            <el-option label="modelscope" value="modelscope" />
          </el-select>
        </el-form-item>
        <el-form-item label="Source Model ID">
          <el-input v-model="importForm.source_model_id" />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="importForm.name" />
        </el-form-item>
        <el-form-item label="显示名">
          <el-input v-model="importForm.display_name" />
        </el-form-item>
        <el-form-item label="参数规模">
          <el-input v-model="importForm.parameter_size" />
        </el-form-item>
        <el-form-item label="摘要">
          <el-input v-model="importForm.summary" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="importDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="importing" @click="submitImport">导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="providerDialogVisible" :title="providerForm.id ? '编辑供应商' : '新建供应商'" width="560px">
      <el-form :model="providerForm" label-width="120px">
        <el-form-item label="名称">
          <el-input v-model="providerForm.name" />
        </el-form-item>
        <el-form-item label="Provider Type">
          <el-input v-model="providerForm.provider_type" />
        </el-form-item>
        <el-form-item label="Endpoint">
          <el-input v-model="providerForm.base_url" />
        </el-form-item>
        <el-form-item label="Credential ID">
          <el-input-number v-model="providerForm.credential_id" :min="1" style="width: 100%" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="providerForm.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="providerForm.status" style="width: 100%">
            <el-option label="active" value="active" />
            <el-option label="disabled" value="disabled" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="providerDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="providerSaving" @click="submitProvider">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createAIProvider,
  deleteAIProvider,
  getAIModelCatalog,
  getAIProviders,
  importAIModel,
  updateAIProvider
} from '@/api/store'

const models = ref([])
const providers = ref([])
const importDialogVisible = ref(false)
const providerDialogVisible = ref(false)
const importing = ref(false)
const providerSaving = ref(false)
const importForm = reactive({
  source: 'huggingface',
  source_model_id: '',
  name: '',
  display_name: '',
  parameter_size: '',
  summary: ''
})
const providerForm = reactive({
  id: 0,
  name: '',
  provider_type: '',
  base_url: '',
  credential_id: undefined,
  description: '',
  status: 'active'
})

const extractList = (payload) => {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.list)) return payload.list
  if (Array.isArray(payload?.items)) return payload.items
  if (Array.isArray(payload?.models)) return payload.models
  return []
}

const loadData = async () => {
  try {
    const [modelsRes, providersRes] = await Promise.all([
      getAIModelCatalog(),
      getAIProviders()
    ])
    models.value = extractList(modelsRes?.data)
    providers.value = extractList(providersRes?.data)
  } catch (error) {
    ElMessage.error('加载模型与供应商失败')
  }
}

const resetImportForm = () => {
  importForm.source = 'huggingface'
  importForm.source_model_id = ''
  importForm.name = ''
  importForm.display_name = ''
  importForm.parameter_size = ''
  importForm.summary = ''
}

const openProviderDialog = (row = null) => {
  providerForm.id = row?.id || 0
  providerForm.name = row?.name || ''
  providerForm.provider_type = row?.provider_type || ''
  providerForm.base_url = row?.base_url || ''
  providerForm.credential_id = row?.credential_id ?? undefined
  providerForm.description = row?.description || ''
  providerForm.status = row?.status || 'active'
  providerDialogVisible.value = true
}

const submitImport = async () => {
  if (!importForm.source_model_id) {
    ElMessage.warning('请输入 Source Model ID')
    return
  }
  importing.value = true
  try {
    const res = await importAIModel({ ...importForm })
    if (res.code === 200) {
      importDialogVisible.value = false
      resetImportForm()
      await loadData()
      ElMessage.success('模型已导入')
      return
    }
    ElMessage.error(res.message || '导入模型失败')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '导入模型失败')
  } finally {
    importing.value = false
  }
}

const submitProvider = async () => {
  if (!providerForm.name || !providerForm.provider_type) {
    ElMessage.warning('请填写供应商名称和 Provider Type')
    return
  }
  providerSaving.value = true
  try {
    const payload = {
      name: providerForm.name,
      provider_type: providerForm.provider_type,
      base_url: providerForm.base_url,
      description: providerForm.description,
      status: providerForm.status
    }
    if (providerForm.credential_id) {
      payload.credential_id = providerForm.credential_id
    }
    const res = providerForm.id
      ? await updateAIProvider(providerForm.id, payload)
      : await createAIProvider(payload)
    if (res.code === 200) {
      providerDialogVisible.value = false
      await loadData()
      ElMessage.success(providerForm.id ? '供应商已更新' : '供应商已创建')
      return
    }
    ElMessage.error(res.message || '保存供应商失败')
  } catch (error) {
    ElMessage.error(error?.response?.data?.message || '保存供应商失败')
  } finally {
    providerSaving.value = false
  }
}

const removeProvider = async (row) => {
  try {
    await ElMessageBox.confirm(`确认删除供应商 ${row.name} 吗？`, '确认操作', { type: 'warning' })
    const res = await deleteAIProvider(row.id)
    if (res.code === 200) {
      await loadData()
      ElMessage.success('供应商已删除')
      return
    }
    ElMessage.error(res.message || '删除供应商失败')
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      ElMessage.error(error?.response?.data?.message || '删除供应商失败')
    }
  }
}

onMounted(() => {
  loadData()
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.governance-card {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.stack-block {
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
</style>
