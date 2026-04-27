<template>
  <div class="pipeline-design-container">
    <div
      class="library-toggle-anchor"
      :class="{ collapsed: leftPanelCollapsed }"
    >
      <el-tooltip :content="leftPanelCollapsed ? '展开组件库' : '折叠组件库'" placement="right">
        <el-button
          circle
          class="library-toggle-btn"
          :aria-label="leftPanelCollapsed ? '展开组件库' : '折叠组件库'"
          :title="leftPanelCollapsed ? '展开组件库' : '折叠组件库'"
          @click="leftPanelCollapsed = !leftPanelCollapsed"
        >
          <el-icon><component :is="leftPanelCollapsed ? 'Expand' : 'Fold'" /></el-icon>
        </el-button>
      </el-tooltip>
    </div>

    <!-- 左侧组件库面板 -->
    <div class="components-panel" :class="{ collapsed: leftPanelCollapsed }" :style="{ width: leftPanelCollapsed ? '0' : '260px' }">
      <div class="panel-header">
        <span>组件库</span>
      </div>
      <div class="components-content" v-show="!leftPanelCollapsed">
        <div 
          v-for="category in componentCategories" 
          :key="category.name" 
          class="component-category"
        >
          <div class="category-header" @click="toggleCategory(category.name)">
            <el-icon>
              <component :is="expandedCategories.includes(category.name) ? 'ArrowDown' : 'ArrowRight'" />
            </el-icon>
            <span>{{ category.label }}</span>
            <span class="count">({{ getCategoryCount(category.name) }})</span>
          </div>
          <div v-show="expandedCategories.includes(category.name)" class="category-items">
            <div
              v-for="comp in category.components"
              :key="comp.type"
              class="component-item"
              draggable="true"
              @dragstart="handleDragStart($event, comp)"
              @click="addNodeFromLibrary(comp)"
            >
              <div class="component-icon" :style="{ background: comp.color }">
                <el-icon><component :is="comp.icon" /></el-icon>
              </div>
              <div class="component-info">
                <span class="component-name">{{ comp.name }}</span>
                <span class="component-desc">{{ comp.description }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

      <!-- 画布区域 -->
    <div class="canvas-area" ref="canvasArea">
      <!-- 画布工具栏 -->
      <div class="canvas-toolbar">
        <div class="toolbar-left">
          <el-tooltip content="撤销" placement="bottom">
            <el-button @click="undo" :disabled="!canUndo">
              <el-icon><Top /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip content="重做" placement="bottom">
            <el-button @click="redo" :disabled="!canRedo">
              <el-icon><Bottom /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
        <div class="toolbar-right">
          <el-tooltip content="网格对齐" placement="bottom">
            <el-button :type="gridSnap ? 'primary' : ''" @click="gridSnap = !gridSnap">
              <el-icon><Grid /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip content="清空画布" placement="bottom">
            <el-button @click="clearCanvas">
              <el-icon><Delete /></el-icon>
            </el-button>
          </el-tooltip>
          <el-button type="primary" @click="savePipeline">
            <el-icon><Check /></el-icon>
            保存
          </el-button>
        </div>
      </div>

      <!-- DAG 画布 -->
      <div
        class="canvas-wrapper"
        ref="canvasWrapper"
        @mousedown="handleCanvasMouseDown"
        @mousemove="handleCanvasMouseMove"
        @mouseup="handleCanvasMouseUp"
                @dragover="handleDragOver"
        @drop="handleDrop"
      >
        <!-- 网格背景 -->
        <div 
          class="grid-background"
          :style="{
            backgroundPosition: `${canvasOffset.x}px ${canvasOffset.y}px`,
            backgroundSize: `20px`
          }"
        ></div>

        <!-- 连接线层 -->
        <div 
          class="connections-layer-wrapper"
          :style="{
            width: `${canvasWidth}px`,
            height: `${canvasHeight}px`,
            transform: `translate(${canvasOffset.x}px, ${canvasOffset.y}px)`,
            transformOrigin: '0 0'
          }"
        >
          <svg
            class="connections-layer"
            :width="canvasWidth"
            :height="canvasHeight"
            :viewBox="`0 0 ${canvasWidth} ${canvasHeight}`"
          >
            <defs>
              <!-- 常规连接箭头：细线空心，偏工程化精致感 -->
              <marker id="arrowhead" markerWidth="14" markerHeight="14" refX="11.5" refY="7" orient="auto" markerUnits="userSpaceOnUse">
                <path d="M 2 2 L 11 7 L 2 12" fill="none" stroke="var(--pipeline-connection-color)" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
              </marker>
              <!-- 选中连接箭头 -->
              <marker id="arrowhead-selected" markerWidth="14" markerHeight="14" refX="11.5" refY="7" orient="auto" markerUnits="userSpaceOnUse">
                <path d="M 2 2 L 11 7 L 2 12" fill="none" stroke="var(--pipeline-connection-selected)" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" />
              </marker>
              <!-- 虚线连接标记 -->
              <marker id="arrowhead-dashed" markerWidth="14" markerHeight="14" refX="11.5" refY="7" orient="auto" markerUnits="userSpaceOnUse">
                <path d="M 2 2 L 11 7 L 2 12" fill="none" stroke="var(--text-muted)" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
              </marker>
            </defs>
            <!-- 已完成的连接线 -->
            <g
              v-for="conn in connections"
              :key="conn.id"
              class="connection-item"
              :class="{ selected: selectedConnection?.id === conn.id }"
            >
              <path
                :d="getConnectionPath(conn)"
                class="connection-line-glow"
                fill="none"
              />
              <path
                :d="getConnectionPath(conn)"
                class="connection-line"
                fill="none"
                :marker-end="selectedConnection?.id === conn.id ? 'url(#arrowhead-selected)' : 'url(#arrowhead)'"
                @click="selectConnection(conn)"
                @dblclick="deleteConnection(conn)"
              />
            </g>
            <!-- 正在拖拽的连接线 -->
            <path
              v-if="connectingLine"
              :d="connectingLine.path"
              class="connecting-line-temp"
              stroke="var(--text-muted)"
              stroke-width="2"
              stroke-dasharray="5,5"
              fill="none"
            />
          </svg>
        </div>

        <!-- 节点层 -->
        <div 
          class="nodes-layer"
          :style="{
            transform: `translate(${canvasOffset.x}px, ${canvasOffset.y}px)`
          }"
        >
          <div
            v-for="node in nodes"
            :key="node.id"
            class="pipeline-node"
            :class="{ 
              selected: selectedNode?.id === node.id
            }"
            :style="{
              left: `${node.x}px`,
              top: `${node.y}px`,
              width: `${node.width}px`
            }"
            @mousedown.stop="handleNodeMouseDown($event, node)"
            @click="selectNode(node)"
            @dblclick="editNode(node)"
          >
            <!-- 输入端口 -->
            <div
              v-if="(node.inputs?.length || 0) > 0"
              class="node-port input-port"
              :class="{ connected: node.inputs?.some(input => input.connected) }"
              title="输入"
              @mousedown.stop="startConnection(node, node.inputs[0], 'input')"
              @mouseup.stop="finishConnection(node, node.inputs[0], 'input')"
            />

            <!-- 节点内容 -->
            <div class="node-content">
              <div class="node-header">
                <div class="node-main">
                  <div v-if="executionOrder.get(node.id)" class="node-order-badge">
                    #{{ executionOrder.get(node.id) }}
                  </div>
                  <div class="node-icon" :style="{ background: getNodeColor(node.type) }">
                    <el-icon><component :is="getNodeIcon(node.type)" /></el-icon>
                  </div>
                  <div class="node-info">
                    <span class="node-name">{{ node.name }}</span>
                  </div>
                </div>
                <el-icon class="delete-btn" @click.stop="deleteNode(node)"><Close /></el-icon>
              </div>
              <div class="node-meta">
                <span class="meta-chip type-chip">{{ getNodeTypeLabel(node.type) }}</span>
                <span class="meta-chip io-chip">
                  IN {{ getNodeInCount(node.id) }} / OUT {{ getNodeOutCount(node.id) }}
                </span>
              </div>
            </div>

            <!-- 输出端口 -->
            <div
              v-if="(node.outputs?.length || 0) > 0"
              class="node-port output-port"
              :class="{ connected: node.outputs?.some(output => output.connected) }"
              title="输出"
              @mousedown.stop="startConnection(node, node.outputs[0], 'output')"
              @mouseup.stop="finishConnection(node, node.outputs[0], 'output')"
            />
          </div>
        </div>
      </div>

      <!-- 空状态提示 -->
      <div v-if="nodes.length === 0" class="empty-state">
        <el-empty description="从左侧组件库拖拽组件到此处开始设计">
          <template #description>
            <p>点击组件或将其拖拽到画布上</p>
            <p>使用鼠标滚轮缩放，按住空格或中键拖动画布</p>
          </template>
        </el-empty>
      </div>
    </div>

    <!-- 右侧配置面板 -->
    <div 
      class="config-panel" 
      :style="{ 
        width: rightPanelCollapsed ? '0' : (selectedNode ? '320px' : '0'),
        opacity: selectedNode ? 1 : 0
      }"
    >
      <div class="panel-header" v-if="selectedNode">
        <span>组件配置</span>
        <el-icon class="close-btn" @click="selectedNode = null">
          <Close />
        </el-icon>
      </div>
      <div class="config-content" v-if="selectedNode">
        <!-- 基础信息 -->
        <div class="config-section">
          <div class="section-title">基础信息</div>
          <el-form label-position="top" size="small">
            <el-form-item label="节点名称">
              <el-input v-model="selectedNode.name" @change="updateNode(selectedNode)" />
            </el-form-item>
            <el-form-item label="节点类型">
              <el-input :value="getNodeTypeLabel(selectedNode.type)" disabled />
            </el-form-item>
            <el-form-item label="描述">
              <el-input 
                v-model="selectedNode.description" 
                type="textarea" 
                :rows="2"
                @change="updateNode(selectedNode)"
              />
            </el-form-item>
          </el-form>
        </div>

        <!-- 参数配置 -->
        <div class="config-section">
          <div class="section-title">参数配置</div>
          <el-form label-position="top" size="small">
            <template v-for="param in getNodeParams(selectedNode.type)" :key="param.key">
              <el-form-item 
                :label="param.label" 
                v-if="isParamVisible(param, selectedNode)"
              >
                <el-input 
                  v-if="param.type === 'text'"
                  :model-value="getNodeParamValue(selectedNode, param.key)"
                  :placeholder="param.placeholder"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                />
                <el-input 
                  v-else-if="param.type === 'textarea'"
                  :model-value="getNodeParamValue(selectedNode, param.key)"
                  type="textarea"
                  :rows="4"
                  :placeholder="param.placeholder"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                />
                <el-select 
                  v-else-if="param.type === 'select'"
                  :model-value="getNodeParamValue(selectedNode, param.key)"
                  :placeholder="param.placeholder"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                >
                  <el-option 
                    v-for="opt in param.options" 
                    :key="opt.value" 
                    :label="opt.label" 
                    :value="opt.value"
                  />
                </el-select>
                <el-switch 
                  v-else-if="param.type === 'boolean'"
                  :model-value="Boolean(getNodeParamValue(selectedNode, param.key))"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                />
                <el-input-number 
                  v-else-if="param.type === 'number'"
                  :model-value="Number(getNodeParamValue(selectedNode, param.key) || 0)"
                  :min="param.min"
                  :max="param.max"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                />
                <el-checkbox-group
                  v-else-if="param.type === 'checkbox_group'"
                  :model-value="Array.isArray(getNodeParamValue(selectedNode, param.key)) ? getNodeParamValue(selectedNode, param.key) : []"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                >
                  <el-checkbox
                    v-for="opt in param.options"
                    :key="opt.value"
                    :label="opt.value"
                  >
                    {{ opt.label }}
                  </el-checkbox>
                </el-checkbox-group>
                <el-select
                  v-else-if="param.type === 'resource_selector'"
                  :model-value="getNodeParamValue(selectedNode, param.key)"
                  filterable
                  clearable
                  :placeholder="param.placeholder || '选择资源'"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @change="updateNode(selectedNode)"
                >
                  <el-option
                    v-for="resource in getResourceOptions(param.resource_type)"
                    :key="resource.id"
                    :label="formatResourceOptionLabel(resource)"
                    :value="resource.id"
                  />
                </el-select>
                <CredentialSelector
                  v-else-if="param.type === 'credential_selector'"
                  :model-value="getNodeParamValue(selectedNode, param.key)"
                  :credential-type="param.credential_type"
                  :credential-category="param.credential_category"
                  @update:model-value="setNodeParamValue(selectedNode, param.key, $event, param.label)"
                  @invalid-selection="handleInvalidCredentialSelection(param.label, $event)"
                  @change="updateNode(selectedNode)"
                />
                <div class="param-flex-row">
                  <el-checkbox
                    :model-value="isNodeParamFlexible(selectedNode, param.key)"
                    @update:model-value="setNodeParamFlexible(selectedNode, param.key, $event, param.label)"
                    @change="updateNode(selectedNode)"
                  >
                    手动运行可覆盖
                  </el-checkbox>
                </div>
              </el-form-item>
            </template>
          </el-form>
        </div>

        <!-- 可用输出变量 -->
        <div class="config-section" v-if="getTaskOutputFields(selectedNode.type).length > 0">
          <el-collapse>
            <el-collapse-item name="outputs">
              <template #title>
                <div class="outputs-header">
                  <span>可用输出变量</span>
                  <el-icon class="outputs-icon"><QuestionFilled /></el-icon>
                </div>
              </template>
              <div class="outputs-content">
                <div class="outputs-tip">
                  可使用 <code>${outputs.&lt;前置节点ID&gt;.&lt;field&gt;}</code> 引用前置任务输出
                </div>
                <div class="outputs-tip outputs-node-type">
                  当前节点: {{ selectedNode.id }}
                </div>
                <div class="outputs-list">
                  <div
                    v-for="field in getTaskOutputFields(selectedNode.type)"
                    :key="field.key"
                    class="output-item"
                    @click="copyToClipboard('${outputs.' + selectedNode.id + '.' + field.key + '}')"
                  >
                    <code class="output-key">${outputs.{{ selectedNode.id }}.{{ field.key }}}</code>
                     <span class="output-desc">{{ field.label || field.desc || '-' }}</span>
                  </div>
                </div>
                <div class="outputs-hint">
                  <el-icon><DocumentCopy /></el-icon>
                  点击变量即可复制进行粘贴
                </div>
              </div>
            </el-collapse-item>
          </el-collapse>
        </div>

        <div class="config-section" v-if="getNodeCredentialSlots(selectedNode.type).length > 0">
          <div class="section-title">凭据绑定</div>
          <el-form label-position="top" size="small">
            <el-form-item
              v-for="slot in getNodeCredentialSlots(selectedNode.type)"
              :key="slot.slot_key"
              :label="`${slot.label || slot.slot_key}${slot.required ? '（必填）' : ''}`"
            >
              <CredentialSelector
                :model-value="getNodeParamValue(selectedNode, `credentials.${slot.slot_key}.credential_id`)"
                :credential-types="slot.allowed_types || []"
                :credential-categories="slot.allowed_categories || []"
                @update:model-value="setNodeParamValue(selectedNode, `credentials.${slot.slot_key}.credential_id`, $event, slot.label || slot.slot_key)"
                @invalid-selection="handleInvalidCredentialSelection(slot.label || slot.slot_key, $event)"
                @change="updateNode(selectedNode)"
              />
            </el-form-item>
          </el-form>
          <div class="config-tip">
                    修改凭据绑定可能影响当前流水线后续运行，以及所有引用该凭据的任务认证行为。
          </div>
        </div>

        <!-- 前置条件 -->
        <div class="config-section">
          <div class="section-title">前置条件</div>
          <div class="condition-list">
            <div 
              v-for="(condition, idx) in selectedNode.conditions" 
              :key="idx"
              class="condition-item"
            >
              <el-select v-model="condition.operator" size="small">
                <el-option label="全部满足" value="AND" />
                <el-option label="任一满足" value="OR" />
              </el-select>
              <el-select v-model="condition.field" size="small">
                <el-option label="上一节点状态" value="prev_status" />
                <el-option label="变量值" value="variable" />
              </el-select>
              <el-input v-model="condition.value" size="small" placeholder="值" />
              <el-icon class="remove-btn" @click="removeCondition(idx)"><Close /></el-icon>
            </div>
            <el-button type="primary" link size="small" @click="addCondition">
              <el-icon><Plus /></el-icon>
              添加条件
            </el-button>
          </div>
        </div>

        <!-- 前置任务 -->
        <div class="config-section">
          <div class="section-title">前置任务</div>
          <div class="predecessor-list">
            <div 
              v-for="(predId, idx) in selectedNode.predecessors" 
              :key="idx"
              class="predecessor-item"
            >
                <el-select 
                  v-model="selectedNode.predecessors[idx]" 
                  size="small" 
                  placeholder="选择前置任务" 
                  style="flex: 1;" 
                  :disabled="!availablePredecessorNodes.length && !predId"
                  @change="(val) => handlePredecessorSelectChange(val, idx)"
                >
                <!-- 显示已选中的节点名称作为第一个选项 -->
                <el-option
                  v-if="predId"
                  :key="predId"
                  :label="getPredecessorName(predId)"
                  :value="predId"
                />
                <!-- 可选的前置任务列表 -->
                <el-option
                  v-for="node in availablePredecessorNodes"
                  :key="node.id"
                  :label="node.name"
                  :value="node.id"
                  :disabled="node.id === selectedNode.id"
                />
              </el-select>
              <el-icon class="remove-btn" @click="removePredecessor(idx)"><Close /></el-icon>
            </div>
            <el-button type="primary" link size="small" @click="addPredecessor">
              <el-icon><Plus /></el-icon>
              添加前置任务
            </el-button>
          </div>
          <div class="config-tip">
            添加前置任务将自动创建连接线，支持通过拖拽端口或在此处配置两种方式创建连线
          </div>
        </div>

        <!-- 失败忽略配置 -->
        <div class="config-section">
          <div class="section-title">失败处理</div>
          <el-form label-position="top" size="small">
            <el-form-item>
              <el-checkbox v-model="selectedNode.ignore_failure" @change="updateNode(selectedNode)">
                <div class="checkbox-label">
                  <span class="checkbox-title">忽略当前节点失败</span>
                  <span class="checkbox-desc">当前节点执行失败时，任务状态仍显示失败，但不会阻断后续节点继续执行</span>
                </div>
              </el-checkbox>
            </el-form-item>
          </el-form>
        </div>

        <!-- 操作按钮 -->
        <div class="config-actions">
          <el-button type="primary" @click="saveNodeConfig">保存配置</el-button>
          <el-button type="danger" @click="deleteNodeFromConfig(selectedNode)">删除节点</el-button>
          <el-button @click="selectedNode = null">取消</el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import CredentialSelector from './components/CredentialSelector.vue'
import { getResourceList } from '@/api/resource'
import { getVisibleCredentialSlots } from './credentialSlots'
import {
  ArrowDown,
  ArrowRight,
  Fold,
  Expand,
  Top,
  Bottom,
  Grid,
  Delete,
  Close,
  Plus,
  Check,
  QuestionFilled,
  DocumentCopy
} from '@element-plus/icons-vue'
import { updatePipeline, getPipelineDetail, getPipelineTaskTypes } from '@/api/pipeline'
import { buildConnectionPath } from './connectionGeometry'

const route = useRoute()
const emit = defineEmits(['saved'])
const pipelineId = computed(() => parseInt(route.params.id))
const taskTypeDefinitions = ref({})
const loadedCredentialBindingSnapshot = ref([])
const loadedDefinition = ref({
  triggers: [],
  metadata: {
    version: '2.0'
  }
})
const availableResources = ref([])

// 画布状态
const canvasArea = ref(null)
const canvasWrapper = ref(null)
const canvasOffset = reactive({ x: 0, y: 0 })
const canvasWidth = ref(5000)
const canvasHeight = ref(5000)
const gridSnap = ref(true)
const isPanning = ref(false)
const panStart = reactive({ x: 0, y: 0 })

// 面板状态
const leftPanelCollapsed = ref(false)
const rightPanelCollapsed = ref(false)
const expandedCategories = ref(['source', 'build'])

// 选中状态
const selectedNode = ref(null)
const selectedConnection = ref(null)

// 连接状态
const connectingLine = ref(null)
const connectionStart = ref(null)

// 历史记录（撤销/重做）
const history = ref([])
const historyIndex = ref(-1)

// 节点和连接
const nodes = ref([])
const connections = ref([])

// 组件类别（由后端 task_definition 驱动）
const categoryMetaMap = {
  source: { label: '代码源', icon: 'Connection', color: '#67C23A' },
  build: { label: '构建', icon: 'Box', color: '#E6A23C' },
  test: { label: '测试', icon: 'Document', color: '#409EFF' },
  artifact: { label: '制品', icon: 'UploadFilled', color: '#36B8A6' },
  deploy: { label: '部署', icon: 'Promotion', color: '#F56C6C' },
  notify: { label: '通知', icon: 'Bell', color: 'var(--text-muted)' },
  utility: { label: '工具', icon: 'Tools', color: '#8c33fe' },
  custom: { label: '自定义', icon: 'Setting', color: '#909399' },
  default: { label: '其它', icon: 'Box', color: '#909399' }
}

const getCategoryMeta = (category) => {
  return categoryMetaMap[String(category || '').toLowerCase()] || categoryMetaMap.default
}

const getTaskDefinitionByType = (type) => {
  const normalized = normalizeTaskType(type)
  return taskTypeDefinitions.value[normalized] || null
}

const getNodePorts = (taskType, direction) => {
  const def = getTaskDefinitionByType(taskType)
  if (!def) {
    if (direction === 'input') return [{ label: '输入', key: 'input' }]
    return [{ label: '输出', key: 'output' }]
  }

  const category = String(def.category || '').toLowerCase()
  if (direction === 'input') {
    if (category === 'source') return []
    return [{ label: '输入', key: 'input' }]
  }

  if (Array.isArray(def.outputs_schema) && def.outputs_schema.length > 0) {
    return [{ label: '输出', key: 'output' }]
  }
  return []
}

const componentCategories = computed(() => {
  const grouped = new Map()

  Object.values(taskTypeDefinitions.value).forEach((def) => {
    const categoryKey = String(def.category || 'custom').toLowerCase()
    if (!grouped.has(categoryKey)) {
      const meta = getCategoryMeta(categoryKey)
      grouped.set(categoryKey, {
        name: categoryKey,
        label: meta.label,
        components: []
      })
    }

    const meta = getCategoryMeta(categoryKey)
    grouped.get(categoryKey).components.push({
      type: def.type,
      name: def.name || def.task_key || def.type,
      description: def.description || '',
      icon: meta.icon,
      color: meta.color,
      inputs: getNodePorts(def.type, 'input'),
      outputs: getNodePorts(def.type, 'output')
    })
  })

  return Array.from(grouped.values()).map(category => ({
    ...category,
    components: category.components.sort((a, b) => String(a.name || '').localeCompare(String(b.name || '')))
  }))
})

// 获取类别组件数量
const getCategoryCount = (categoryName) => {
  const category = componentCategories.value.find(c => c.name === categoryName)
  return category ? category.components.length : 0
}

// 切换类别展开/收起
const toggleCategory = (categoryName) => {
  const idx = expandedCategories.value.indexOf(categoryName)
  if (idx > -1) {
    expandedCategories.value.splice(idx, 1)
  } else {
    expandedCategories.value.push(categoryName)
  }
}

// 获取节点类型标签
const getNodeTypeLabel = (type) => {
  for (const cat of componentCategories.value) {
    const comp = cat.components.find(c => c.type === type)
    if (comp) return comp.name
  }
  return type
}

// 获取节点图标
const getNodeIcon = (type) => {
  for (const cat of componentCategories.value) {
    const comp = cat.components.find(c => c.type === type)
    if (comp) return comp.icon
  }
  return 'Box'
}

// 获取节点颜色
const getNodeColor = (type) => {
  for (const cat of componentCategories.value) {
    const comp = cat.components.find(c => c.type === type)
    if (comp) return comp.color
  }
  return '#409EFF'
}

const normalizeTaskType = (type) => {
  if (!type) return ''
  return String(type).trim().toLowerCase()
}

const getNodeCredentialSlots = (type) => {
  const def = getTaskDefinitionByType(type)
  if (!def || !Array.isArray(def.credential_slots)) {
    return []
  }
  return getVisibleCredentialSlots(normalizeTaskType(type), def.credential_slots)
}

const mapFieldTypeToFormType = (field = {}) => {
  const fieldType = String(field.type || '').toLowerCase()
  const ui = String(field.ui_component || '').toLowerCase()

  if (fieldType === 'boolean') return 'boolean'
  if (fieldType === 'number') return 'number'
  if (fieldType === 'text') return 'textarea'
  if (fieldType === 'select') return 'select'
  if (fieldType === 'multiselect') return 'checkbox_group'
  if (fieldType === 'resource') return 'resource_selector'
  if (fieldType === 'credential') return 'credential_selector'
  if (fieldType === 'json' || fieldType === 'object' || fieldType === 'array') return 'textarea'

  if (ui === 'textarea') return 'textarea'
  if (ui === 'switch') return 'boolean'
  if (ui === 'select') return 'select'

  return 'text'
}

const getFieldDefaultValue = (field = {}) => {
  if (field.default !== undefined) return field.default
  const fieldType = String(field.type || '').toLowerCase()
  if (fieldType === 'boolean') return false
  if (fieldType === 'number') return 0
  if (fieldType === 'multiselect') return []
  return ''
}

const normalizeFieldOptions = (options) => {
  if (!Array.isArray(options)) return []
  return options.map((item) => {
    if (item && typeof item === 'object') {
      const value = item.value ?? item.key ?? item.id ?? item.label
      return {
        label: String(item.label ?? value ?? ''),
        value
      }
    }
    return {
      label: String(item ?? ''),
      value: item
    }
  })
}

const getNodeParams = (type) => {
  const def = getTaskDefinitionByType(type)
  if (!def || !Array.isArray(def.fields_schema)) return []

  return def.fields_schema.map((field) => {
    const formType = mapFieldTypeToFormType(field)
    return {
      key: field.key,
      label: field.label || field.key,
      type: formType,
      field_type: field.type,
      required: Boolean(field.required),
      readonly: Boolean(field.readonly),
      secret: Boolean(field.secret),
      placeholder: field.ui_placeholder || field.placeholder || '',
      description: field.description || '',
      options: formType === 'select' || formType === 'checkbox_group' ? normalizeFieldOptions(field.options) : [],
      defaultValue: getFieldDefaultValue(field),
      resource_type: field.resource_type || '',
      credential_type: field.credential_type || '',
      credential_category: field.credential_category || ''
    }
  })
}

const buildDefaultNodeParams = (type) => {
  return getNodeParams(type).map((param) => ({
    key: param.key,
    label: param.label,
    value: Array.isArray(param.defaultValue) ? [...param.defaultValue] : param.defaultValue,
    is_flexible: false
  }))
}

const normalizeNodeParams = (type, params = []) => {
  const paramDefs = getNodeParams(type)
  const byKey = new Map()

  if (Array.isArray(params)) {
    params.forEach((item) => {
      if (!item || !item.key) return
      byKey.set(item.key, {
        key: item.key,
        label: item.label || item.key,
        value: item.value,
        is_flexible: Boolean(item.is_flexible)
      })
    })
  } else if (params && typeof params === 'object') {
    Object.entries(params).forEach(([key, value]) => {
      byKey.set(key, {
        key,
        label: key,
        value,
        is_flexible: false
      })
    })
  }

  const merged = paramDefs.map((paramDef) => {
    const existing = byKey.get(paramDef.key)
    return {
      key: paramDef.key,
      label: existing?.label || paramDef.label,
      value: existing?.value !== undefined ? existing.value : paramDef.defaultValue,
      is_flexible: Boolean(existing?.is_flexible)
    }
  })

  byKey.forEach((value, key) => {
    if (paramDefs.some(item => item.key === key)) return
    merged.push({
      key,
      label: value.label || key,
      value: value.value,
      is_flexible: Boolean(value.is_flexible)
    })
  })

  return merged
}

const getNodeParamEntry = (node, key) => {
  if (!node || !Array.isArray(node.params)) return null
  return node.params.find(item => item && item.key === key) || null
}

const getNodeParamValue = (node, key) => {
  const entry = getNodeParamEntry(node, key)
  return entry ? entry.value : undefined
}

const setNodeParamValue = (node, key, value, fallbackLabel = '') => {
  if (!node) return
  if (!Array.isArray(node.params)) {
    node.params = []
  }

  const index = node.params.findIndex(item => item && item.key === key)
  if (index >= 0) {
    node.params[index].value = value
    if (!node.params[index].label && fallbackLabel) {
      node.params[index].label = fallbackLabel
    }
    return
  }

  node.params.push({
    key,
    label: fallbackLabel || key,
    value,
    is_flexible: false
  })
}

const isNodeParamFlexible = (node, key) => {
  const entry = getNodeParamEntry(node, key)
  return Boolean(entry?.is_flexible)
}

const setNodeParamFlexible = (node, key, flexible, fallbackLabel = '') => {
  if (!node) return
  if (!Array.isArray(node.params)) {
    node.params = []
  }
  const index = node.params.findIndex(item => item && item.key === key)
  if (index >= 0) {
    node.params[index].is_flexible = Boolean(flexible)
    if (!node.params[index].label && fallbackLabel) {
      node.params[index].label = fallbackLabel
    }
    return
  }

  node.params.push({
    key,
    label: fallbackLabel || key,
    value: '',
    is_flexible: Boolean(flexible)
  })
}

const isParamVisible = (param, node) => {
  if (!param?.show_if) return true
  return getNodeParamValue(node, param.show_if.key) === param.show_if.value
}

const loadResources = async () => {
  try {
    const response = await getResourceList({ type: 'vm' })
    if (response.code === 200 && Array.isArray(response.data)) {
      availableResources.value = response.data
    }
  } catch (error) {
    console.error('加载资源列表失败:', error)
  }
}

const getResourceOptions = (resourceType) => {
  if (!resourceType) return availableResources.value
  return availableResources.value.filter(item => item.type === resourceType)
}

const formatResourceOptionLabel = (resource) => {
  if (!resource) return ''
  return `${resource.name} (${resource.endpoint || resource.type || 'resource'})`
}

// 获取连接路径 - 使用节点两侧锚点，确保终点箭头在节点外侧清晰可见
const getConnectionPath = (conn) => buildConnectionPath({
  conn,
  nodes: nodes.value,
  connections: connections.value
})

// 缩放控制
const resetCanvasView = () => {
  canvasOffset.x = 100
  canvasOffset.y = 100
}

// 拖拽组件开始
const handleDragStart = (event, component) => {
  // 计算鼠标在组件中的偏移位置
  const rect = event.target.getBoundingClientRect()
  const offsetX = event.clientX - rect.left
  const offsetY = event.clientY - rect.top
  
  // 设置多种数据格式以提高兼容性
  const componentData = JSON.stringify({
    type: component.type,
    name: component.name,
    description: component.description,
    icon: component.icon,
    color: component.color,
    inputs: component.inputs,
    outputs: component.outputs,
    dragOffset: { x: offsetX, y: offsetY }
  })
  
  event.dataTransfer.setData('application/json', componentData)
  event.dataTransfer.setData('component', componentData)
  event.dataTransfer.effectAllowed = 'copy'
  
  // 设置拖拽图像（使用透明占位符，避免影响视觉效果）
  const emptyImg = new Image()
  emptyImg.src = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'
  event.dataTransfer.setDragImage(emptyImg, 0, 0)
}

// 处理拖拽经过画布
const handleDragOver = (event) => {
  event.preventDefault()
  event.dataTransfer.dropEffect = 'copy'
  event.dataTransfer.effectAllowed = 'copy'
}

// 处理放置到画布
const handleDrop = (event) => {
  event.preventDefault()
  
  // 调试日志
  console.log('Drop event triggered', event.dataTransfer.types)
  
  try {
    // 尝试获取组件数据
    let componentData = event.dataTransfer.getData('application/json')
    if (!componentData) {
      componentData = event.dataTransfer.getData('component')
    }
    
    if (!componentData) {
      console.warn('No component data found in drop event')
      ElMessage.warning('无法添加组件，请重试')
      return
    }

    const component = JSON.parse(componentData)
    console.log('Parsed component:', component)
    
    const canvasRect = canvasWrapper.value?.getBoundingClientRect()
    if (!canvasRect) {
      console.error('Canvas wrapper not found')
      ElMessage.error('画布初始化失败，请刷新页面重试')
      return
    }

    // 计算节点在画布中的位置
    // 节点的位置(node.x, node.y)是相对于nodes-layer未变换坐标系的
    // 需要将鼠标位置从canvas-wrapper坐标系转换到nodes-layer坐标系
    // 转换公式: nodePosition = mouseInWrapper - canvasOffset
    const mouseInWrapperX = event.clientX - canvasRect.left
    const mouseInWrapperY = event.clientY - canvasRect.top
    
    // 考虑拖拽偏移量（鼠标点击位置相对于组件中心的偏移）
    let finalX = mouseInWrapperX - canvasOffset.x - (component.dragOffset?.x || 0)
    let finalY = mouseInWrapperY - canvasOffset.y - (component.dragOffset?.y || 0)
    
    // 网格对齐
    if (gridSnap.value) {
      finalX = Math.round(finalX / 20) * 20
      finalY = Math.round(finalY / 20) * 20
    }

    // 创建新节点
    const newNode = {
      id: `node_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      type: component.type,
      name: component.name || getNodeTypeLabel(component.type),
      description: component.description || '',
      x: gridSnap.value ? Math.round(finalX / 20) * 20 : finalX,
      y: gridSnap.value ? Math.round(finalY / 20) * 20 : finalY,
      width: 200,
      inputs: (component.inputs || []).map(i => ({ ...i, connected: false })),
      outputs: (component.outputs || []).map(o => ({ ...o, connected: false })),
      params: buildDefaultNodeParams(component.type),
      conditions: [],
      predecessors: [],
      status: 'pending'
    }

    nodes.value.push(newNode)
    saveHistory()
    selectNode(newNode)

    ElMessage.success('添加节点成功')
  } catch (error) {
    console.error('Drop error:', error)
    ElMessage.error('添加节点失败，请重试')
  }
}

// 从组件库添加节点
const addNodeFromLibrary = (component) => {
  const newNode = {
    id: `node_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
    type: component.type,
    name: component.name,
    description: '',
    x: 100 + Math.random() * 200,
    y: 100 + Math.random() * 200,
    width: 200,
    inputs: component.inputs.map(i => ({ ...i, connected: false })),
    outputs: component.outputs.map(o => ({ ...o, connected: false })),
      params: buildDefaultNodeParams(component.type),
    conditions: [],
    predecessors: [],
    status: 'pending'
  }
  nodes.value.push(newNode)
  saveHistory()
  selectNode(newNode)
}

// 选择节点
const selectNode = (node) => {
  selectedNode.value = node
  selectedConnection.value = null
}

// 编辑节点
const editNode = (node) => {
  selectedNode.value = node
}

// 删除节点
const deleteNode = async (node) => {
  try {
    await ElMessageBox.confirm('确定要删除此节点吗？', '确认删除', { type: 'warning' })

    // 删除相关的连接
    connections.value = connections.value.filter(
      c => c.from !== node.id && c.to !== node.id
    )

    // 从其他节点的前置任务列表中移除当前节点
    nodes.value.forEach(n => {
      if (n.predecessors && n.predecessors.includes(node.id)) {
        n.predecessors = n.predecessors.filter(predId => predId !== node.id)
      }
    })

    // 删除节点
    nodes.value = nodes.value.filter(n => n.id !== node.id)

    if (selectedNode.value?.id === node.id) {
      selectedNode.value = null
    }

    syncConnectionsWithPredecessors()
    saveHistory()
    ElMessage.success('删除成功')
  } catch {
    // 用户取消
  }
}

// 从配置面板删除节点
const deleteNodeFromConfig = (node) => {
  if (node) {
    deleteNode(node)
  }
}

// 更新节点
const updateNode = (node) => {
  const idx = nodes.value.findIndex(n => n.id === node.id)
  if (idx > -1) {
    nodes.value[idx] = { ...nodes.value[idx], ...node }
    saveHistory()
  }
}

// 选择连接
const selectConnection = (conn) => {
  selectedConnection.value = conn
  selectedNode.value = null
}

// 删除连接
const deleteConnection = async (conn) => {
  try {
    await ElMessageBox.confirm('确定要删除此连接吗？', '确认删除', { type: 'warning' })
    connections.value = connections.value.filter(c => c.id !== conn.id)
    
    // 更新端口连接状态
    const fromNode = nodes.value.find(n => n.id === conn.from)
    const toNode = nodes.value.find(n => n.id === conn.to)
    if (fromNode) {
      fromNode.outputs.forEach(o => o.connected = connections.value.some(c => c.from === fromNode.id))
    }
    if (toNode) {
      toNode.inputs.forEach(i => i.connected = connections.value.some(c => c.to === toNode.id))
      
      // 从目标节点的前置任务数组中移除源节点ID
      if (toNode.predecessors) {
        const predIndex = toNode.predecessors.indexOf(conn.from)
        if (predIndex > -1) {
          toNode.predecessors.splice(predIndex, 1)
        }
      }
    }

    syncConnectionsWithPredecessors()
    saveHistory()
    ElMessage.success('删除成功')
  } catch {
    // 用户取消
  }
}

// 开始连接
const startConnection = (node, port, type) => {
  connectionStart.value = { node, port, type }
  selectedNode.value = null
}

// 完成连接
const finishConnection = (node, port, type) => {
  if (!connectionStart.value) return
  
  // 不能连接自己
  if (connectionStart.value.node.id === node.id) {
    connectionStart.value = null
    connectingLine.value = null
    return
  }
  
  // 输入只能连接输出，输出只能连接输入
  if (connectionStart.value.type === type) {
    connectionStart.value = null
    connectingLine.value = null
    return
  }
  
  // 检查是否已存在连接
  const exists = connections.value.some(c => 
    c.from === connectionStart.value.node.id && c.to === node.id
  )
  
    if (!exists) {
    connections.value.push({
      id: `conn_${Date.now()}`,
      from: connectionStart.value.node.id,
      to: node.id
    })
    
    // 更新端口连接状态
    connectionStart.value.node.outputs.forEach(o => {
      o.connected = connections.value.some(c => c.from === connectionStart.value.node.id)
    })
    node.inputs.forEach(i => {
      i.connected = connections.value.some(c => c.to === node.id)
    })
    
    // 更新目标节点的前置任务数组，保持与连接同步
    if (!node.predecessors) {
      node.predecessors = []
    }
    if (!node.predecessors.includes(connectionStart.value.node.id)) {
      node.predecessors.push(connectionStart.value.node.id)
    }
    
    syncConnectionsWithPredecessors()
    saveHistory()
  }
  
  connectionStart.value = null
  connectingLine.value = null
}

// 节点拖拽
const nodeDragging = ref(null)
const nodeDragOffset = reactive({ x: 0, y: 0 }) // 鼠标点击位置相对于节点左上角的偏移
const nodeDragStartPos = reactive({ x: 0, y: 0 }) // 节点在nodes-layer中的起始位置

const handleNodeMouseDown = (event, node) => {
  nodeDragging.value = node
  // 记录节点在nodes-layer坐标系中的起始位置
  nodeDragStartPos.x = node.x
  nodeDragStartPos.y = node.y
  
  // 计算鼠标点击位置相对于节点左上角的偏移（屏幕坐标）
  const rect = event.target.closest('.pipeline-node').getBoundingClientRect()
  nodeDragOffset.x = event.clientX - rect.left
  nodeDragOffset.y = event.clientY - rect.top
  selectNode(node)
}

// 画布鼠标事件
const handleCanvasMouseDown = (event) => {
  if (event.button === 0 && !event.target.closest('.pipeline-node') && !event.target.closest('.connection-line')) {
    selectedNode.value = null
    selectedConnection.value = null
    isPanning.value = true
    panStart.x = event.clientX - canvasOffset.x
    panStart.y = event.clientY - canvasOffset.y
  } else if (event.button === 1 || (event.button === 0 && event.altKey)) {
    isPanning.value = true
    panStart.x = event.clientX - canvasOffset.x
    panStart.y = event.clientY - canvasOffset.y
  }
}

const handleCanvasMouseMove = (event) => {
  // 拖拽节点
  if (nodeDragging.value) {
    const canvasRect = canvasWrapper.value.getBoundingClientRect()
    
    // 计算鼠标在canvas-wrapper中的位置
    const mouseInWrapperX = event.clientX - canvasRect.left
    const mouseInWrapperY = event.clientY - canvasRect.top
    
    // 计算节点左上角在nodes-layer坐标系中的位置
    // 节点屏幕位置 = mouseInWrapper - 节点相对于wrapper的偏移
    // 节点相对于wrapper的偏移 = mouseInWrapper - nodeDragOffset
    // 节点在nodes-layer中的位置 = mouseInWrapper - nodeDragOffset - canvasOffset
    let newX = mouseInWrapperX - nodeDragOffset.x - canvasOffset.x
    let newY = mouseInWrapperY - nodeDragOffset.y - canvasOffset.y
    
    if (gridSnap.value) {
      newX = Math.round(newX / 20) * 20
      newY = Math.round(newY / 20) * 20
    }
    
    nodeDragging.value.x = Math.max(0, newX)
    nodeDragging.value.y = Math.max(0, newY)
  }
  
  // 拖动画布
  if (isPanning.value) {
    canvasOffset.x = event.clientX - panStart.x
    canvasOffset.y = event.clientY - panStart.y
  }
}

const handleCanvasMouseUp = () => {
  if (nodeDragging.value) {
    saveHistory()
  }
  nodeDragging.value = null
  isPanning.value = false
}
// 键盘事件
const handleKeyDown = (event) => {
  // 检查焦点是否在输入框中
  const isInputFocused = event.target.tagName === 'INPUT' || 
                         event.target.tagName === 'TEXTAREA' || 
                         event.target.contentEditable === 'true' ||
                         event.target.closest('.el-input') !== null
  
  // 如果焦点在输入框中，不处理删除键
  if (isInputFocused) {
    return
  }
  
  if (event.key === 'Delete' || event.key === 'Backspace') {
    if (selectedNode.value) {
      deleteNode(selectedNode.value)
    } else if (selectedConnection.value) {
      deleteConnection(selectedConnection.value)
    }
  }
  if (event.ctrlKey || event.metaKey) {
    if (event.key === 'z') {
      if (event.shiftKey) {
        redo()
      } else {
        undo()
      }
    }
    if (event.key === 'y') {
      redo()
    }
  }
}

const handleKeyUp = (event) => {
  // No special handling needed in unified mode
}

// 历史记录
const saveHistory = () => {
  history.value = history.value.slice(0, historyIndex.value + 1)
  history.value.push({
    nodes: JSON.parse(JSON.stringify(nodes.value)),
    connections: JSON.parse(JSON.stringify(connections.value))
  })
  historyIndex.value = history.value.length - 1
}

const undo = () => {
  if (historyIndex.value > 0) {
    historyIndex.value--
    const state = history.value[historyIndex.value]
    nodes.value = JSON.parse(JSON.stringify(state.nodes))
    connections.value = JSON.parse(JSON.stringify(state.connections))
    syncConnectionsWithPredecessors()
  }
}

const redo = () => {
  if (historyIndex.value < history.value.length - 1) {
    historyIndex.value++
    const state = history.value[historyIndex.value]
    nodes.value = JSON.parse(JSON.stringify(state.nodes))
    connections.value = JSON.parse(JSON.stringify(state.connections))
    syncConnectionsWithPredecessors()
  }
}

const canUndo = computed(() => historyIndex.value > 0)
const canRedo = computed(() => historyIndex.value < history.value.length - 1)

const nodeDependencyStats = computed(() => {
  const statsMap = new Map()

  nodes.value.forEach(node => {
    statsMap.set(node.id, { inSet: new Set(), outSet: new Set() })
  })

  connections.value.forEach(conn => {
    if (!conn?.from || !conn?.to || conn.from === conn.to) return

    const from = statsMap.get(conn.from)
    const to = statsMap.get(conn.to)
    if (!from || !to) return

    from.outSet.add(conn.to)
    to.inSet.add(conn.from)
  })

  statsMap.forEach((value, nodeID) => {
    statsMap.set(nodeID, { in: value.inSet.size, out: value.outSet.size })
  })

  return statsMap
})

const getNodeInCount = (nodeId) => {
  return nodeDependencyStats.value.get(nodeId)?.in || 0
}

const getNodeOutCount = (nodeId) => {
  return nodeDependencyStats.value.get(nodeId)?.out || 0
}

// 计算执行顺序
const executionOrder = computed(() => getExecutionOrder())

// 条件管理
const addCondition = () => {
  if (!selectedNode.value) return
  if (!selectedNode.value.conditions) {
    selectedNode.value.conditions = []
  }
  selectedNode.value.conditions.push({
    operator: 'AND',
    field: 'prev_status',
    value: ''
  })
}

const removeCondition = (idx) => {
  if (selectedNode.value?.conditions) {
    selectedNode.value.conditions.splice(idx, 1)
  }
}

// 可选的前置任务节点列表（排除自己和已被选择的）
const availablePredecessorNodes = computed(() => {
  if (!selectedNode.value) return []
  return nodes.value.filter(node => {
    // 排除自己
    if (node.id === selectedNode.value.id) return false
    // 排除已选择的前置任务（避免重复连接）
    if (selectedNode.value.predecessors?.includes(node.id)) return false
    return true
  })
})

// 获取已选前置节点的名称
const getPredecessorName = (predId) => {
  const node = nodes.value.find(n => n.id === predId)
  return node ? node.name : predId
}

// 获取任务类型的输出字段定义
const getTaskOutputFields = (taskType) => {
  const def = getTaskDefinitionByType(taskType)
  if (!def || !Array.isArray(def.outputs_schema)) return []
  return def.outputs_schema.map(field => ({
    key: field.key,
    label: field.label || field.key,
    desc: field.description || ''
  }))
}

// 获取当前节点的第一个前置节点的 ID
const getFirstPredecessorNodeId = (node) => {
  if (!node || !node.predecessors || node.predecessors.length === 0) {
    return null
  }
  return node.predecessors[0]
}

// 复制文本到剪贴板
const copyToClipboard = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch (err) {
    // 降级方案
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
    ElMessage.success('已复制到剪贴板')
  }
}

// 连接键（用于去重和复用连接ID）
const getConnectionKey = (from, to) => `${from}__${to}`

// 从连接线反推 predecessors（兼容旧格式数据）
const rebuildPredecessorsFromConnections = () => {
  const predecessorMap = new Map()
  const nodeIdSet = new Set(nodes.value.map(node => node.id))

  nodes.value.forEach(node => {
    predecessorMap.set(node.id, [])
  })

  connections.value.forEach(conn => {
    if (!nodeIdSet.has(conn.from) || !nodeIdSet.has(conn.to) || conn.from === conn.to) return
    const targetPreds = predecessorMap.get(conn.to) || []
    if (!targetPreds.includes(conn.from)) {
      targetPreds.push(conn.from)
    }
    predecessorMap.set(conn.to, targetPreds)
  })

  nodes.value.forEach(node => {
    node.predecessors = predecessorMap.get(node.id) || []
  })
}

// 以 predecessors 为唯一来源，同步 DAG 连接线
const syncConnectionsWithPredecessors = () => {
  const nodeIdSet = new Set(nodes.value.map(node => node.id))
  const existingConnectionMap = new Map()

  connections.value.forEach(conn => {
    existingConnectionMap.set(getConnectionKey(conn.from, conn.to), conn)
  })

  const nextConnections = []
  const seenKeys = new Set()

  nodes.value.forEach(node => {
    const currentPredecessors = Array.isArray(node.predecessors) ? node.predecessors : []
    const editablePredecessors = []
    const normalizedPredecessors = []

    currentPredecessors.forEach(predId => {
      // 保留空占位，避免“添加前置任务”后下拉项被同步逻辑立即清掉
      if (!predId) {
        editablePredecessors.push('')
        return
      }
      if (predId === node.id || !nodeIdSet.has(predId)) return
      if (normalizedPredecessors.includes(predId)) return
      normalizedPredecessors.push(predId)
      editablePredecessors.push(predId)

      const key = getConnectionKey(predId, node.id)
      if (seenKeys.has(key)) return
      seenKeys.add(key)

      const existing = existingConnectionMap.get(key)
      nextConnections.push({
        id: existing?.id || `conn_${Date.now()}_${Math.random().toString(36).slice(2, 11)}`,
        from: predId,
        to: node.id
      })
    })

    // 清洗无效/重复 predecessor，避免配置和连线不一致
    if (JSON.stringify(currentPredecessors) !== JSON.stringify(editablePredecessors)) {
      node.predecessors = editablePredecessors
    }
  })

  connections.value = nextConnections
  updateAllPortsConnectionStatus()
}

// 添加前置任务
const addPredecessor = () => {
  if (!selectedNode.value) return
  if (!selectedNode.value.predecessors) {
    selectedNode.value.predecessors = []
  }
  selectedNode.value.predecessors.push('')
}

// 移除前置任务
const removePredecessor = (idx) => {
  if (!selectedNode.value?.predecessors) return

  selectedNode.value.predecessors.splice(idx, 1)
  syncConnectionsWithPredecessors()
  saveHistory()
}

// 处理前置任务选择变化 - 当用户通过下拉框选择前置任务时调用
const handlePredecessorSelectChange = (newValue, idx) => {
  if (!selectedNode.value || !selectedNode.value.predecessors) return

  selectedNode.value.predecessors.splice(idx, 1, newValue || '')
  syncConnectionsWithPredecessors()
  saveHistory()
}

// 监听节点依赖配置变化，实时同步为有向边
watch(
  () => nodes.value.map(node => ({
    id: node.id,
    predecessors: Array.isArray(node.predecessors) ? [...node.predecessors] : []
  })),
  () => {
    syncConnectionsWithPredecessors()
  },
  { deep: true }
)

// 清空画布
const clearCanvas = async () => {
  try {
    await ElMessageBox.confirm('确定要清空画布吗？', '确认清空', { type: 'warning' })
    nodes.value = []
    connections.value = []
    selectedNode.value = null
    saveHistory()
    ElMessage.success('画布已清空')
  } catch {
    // 用户取消
  }
}

// 验证 DAG 有效性
const validateDAG = () => {
  syncConnectionsWithPredecessors()
  const errors = []
  
  // 检查是否有节点
  if (!nodes.value || nodes.value.length === 0) {
    errors.push('画布为空，没有可保存的节点')
    return errors
  }

  if (nodes.value.length > 1 && (!connections.value || connections.value.length === 0)) {
    errors.push('流水线配置无效：多节点流水线必须包含依赖边')
    return errors
  }

  // 检查节点ID唯一性
  const nodeIds = nodes.value.map(n => n.id)
  const uniqueIds = new Set(nodeIds)
  if (uniqueIds.size !== nodeIds.length) {
    errors.push('存在重复的节点ID')
  }
  
  // 构建邻接表和入度表
  const adj = new Map()
  const inDegree = new Map()
  
  // 初始化
  nodes.value.forEach(node => {
    adj.set(node.id, [])
    inDegree.set(node.id, 0)
  })
  
  // 检查连接并构建图
  if (connections.value) {
    connections.value.forEach((conn, idx) => {
      // 检查连接是否有效
      if (!nodeIds.includes(conn.from)) {
        errors.push(`连接 #${idx + 1} 的源节点不存在: ${conn.from}`)
      }
      if (!nodeIds.includes(conn.to)) {
        errors.push(`连接 #${idx + 1} 的目标节点不存在: ${conn.to}`)
      }
      
      // 检查自循环
      if (conn.from === conn.to) {
        errors.push(`连接 #${idx + 1} 形成自循环（源节点和目标节点相同）`)
      }
      
      if (adj.has(conn.from) && adj.has(conn.to) && conn.from !== conn.to) {
        adj.get(conn.from).push(conn.to)
        inDegree.set(conn.to, inDegree.get(conn.to) + 1)
      }
    })
  }
  
  const nodesInEdges = new Set()
  if (connections.value) {
    connections.value.forEach(conn => {
      nodesInEdges.add(conn.from)
      nodesInEdges.add(conn.to)
    })
  }

  if (nodes.value.length > 1 || nodesInEdges.size > 0) {
    const isolatedNodes = nodes.value.filter(node => !nodesInEdges.has(node.id))
    if (isolatedNodes.length > 0) {
      errors.push(`存在孤立节点（未连接到依赖图）: ${isolatedNodes.map(n => n.name || n.id).join(', ')}`)
    }
  }

  const entryNodes = []
  inDegree.forEach((degree, nodeId) => {
    if (degree === 0 && nodesInEdges.has(nodeId)) {
      entryNodes.push(nodeId)
    }
  })

  if (entryNodes.length === 0 && nodesInEdges.size > 0) {
    errors.push('没有起始任务（所有任务都有前置依赖）')
  }

  const exitNodes = []
  nodes.value.forEach(node => {
    const neighbors = adj.get(node.id) || []
    if (neighbors.length === 0 && nodesInEdges.has(node.id)) {
      exitNodes.push(node.id)
    }
  })

  if (exitNodes.length === 0 && nodesInEdges.size > 0) {
    errors.push('没有结束任务（所有任务都有后置依赖）')
  }

  const tempInDegree = new Map(inDegree)
  const queue = [...entryNodes]
  let visitedCount = 0

  while (queue.length > 0) {
    const current = queue.shift()
    visitedCount++

    const neighbors = adj.get(current) || []
    neighbors.forEach(neighbor => {
      tempInDegree.set(neighbor, tempInDegree.get(neighbor) - 1)
      if (tempInDegree.get(neighbor) === 0) {
        queue.push(neighbor)
      }
    })
  }

  if (visitedCount !== nodesInEdges.size && nodesInEdges.size > 0) {
    errors.push('流水线存在循环依赖，必须是有效的有向无环图(DAG)')

    const cycle = findCycle(nodes.value, connections.value || [])
    if (cycle.length > 0) {
      errors.push(`检测到的循环路径: ${cycle.join(' → ')}`)
    }
  }

  return errors
}

// 查找循环路径
const findCycle = (nodes, connections) => {
  const adj = new Map()
  nodes.forEach(node => adj.set(node.id, []))
  connections.forEach(conn => {
    if (adj.has(conn.from) && adj.has(conn.to)) {
      adj.get(conn.from).push(conn.to)
    }
  })
  
  const visited = new Set()
  const recursionStack = new Set()
  
  const dfs = (nodeId, path) => {
    visited.add(nodeId)
    recursionStack.add(nodeId)
    path.push(nodeId)
    
    const neighbors = adj.get(nodeId) || []
    for (const neighbor of neighbors) {
      if (!visited.has(neighbor)) {
        const result = dfs(neighbor, [...path])
        if (result) return result
      } else if (recursionStack.has(neighbor)) {
        // 找到循环
        const cycleStart = path.indexOf(neighbor)
        return path.slice(cycleStart)
      }
    }
    
    recursionStack.delete(nodeId)
    return null
  }
  
  for (const node of nodes) {
    if (!visited.has(node.id)) {
      const cycle = dfs(node.id, [])
      if (cycle) return cycle
    }
  }
  
  return []
}

// 计算节点的执行顺序（基于分层拓扑排序 Kahn 算法）
// 每轮取当前所有入度为0的节点作为当前层，同层按节点数组顺序排列
const getExecutionOrder = () => {
  // 1. 构建邻接表和入度表
  const adj = new Map()
  const inDegree = new Map()
  
  // 初始化
  nodes.value.forEach(node => {
    adj.set(node.id, [])
    inDegree.set(node.id, 0)
  })
  
  // 构建图
  if (connections.value) {
    connections.value.forEach(conn => {
      if (adj.has(conn.from) && adj.has(conn.to)) {
        adj.get(conn.from).push(conn.to)
        inDegree.set(conn.to, inDegree.get(conn.to) + 1)
      }
    })
  }
  
  // 2. 分层遍历：每轮取所有入度为0的节点作为当前层
  const layers = []
  let currentLayer = []
  
  // 初始：收集所有入度为0的节点
  inDegree.forEach((degree, nodeId) => {
    if (degree === 0) {
      currentLayer.push(nodeId)
    }
  })
  
  while (currentLayer.length > 0) {
    layers.push([...currentLayer])  // 复制当前层
    
    // 收集下一层节点
    const nextLayer = []
    currentLayer.forEach(nodeId => {
      const neighbors = adj.get(nodeId) || []
      neighbors.forEach(neighborId => {
        const newDegree = inDegree.get(neighborId) - 1
        inDegree.set(neighborId, newDegree)
        if (newDegree === 0) {
          nextLayer.push(neighborId)
        }
      })
    })
    
    currentLayer = nextLayer
  }
  
  // 3. 展平为顺序列表
  const orderMap = new Map()
  let order = 1
  layers.forEach(layer => {
    layer.forEach(nodeId => {
      orderMap.set(nodeId, order++)
    })
  })
  
  return orderMap
}

const buildCredentialBindingSnapshot = (nodeList) => {
  const list = Array.isArray(nodeList) ? nodeList : []
  const result = []

  list.forEach(node => {
    const params = Array.isArray(node?.params) ? node.params : []
    params.forEach((entry) => {
      if (!entry?.key) return
      const match = /^credentials\.([^.]+)\.credential_id$/.exec(entry.key)
      if (!match) return

      const slot = match[1]
      const credentialID = Number(entry.value || 0)
      if (!Number.isFinite(credentialID) || credentialID <= 0) return

      result.push({
        key: `${node.id}::${slot}`,
        node_id: node.id,
        node_name: node.name || node.id,
        task_type: normalizeTaskType(node.type),
        slot,
        credential_id: credentialID
      })
    })
  })

  result.sort((a, b) => a.key.localeCompare(b.key))
  return result
}

const diffCredentialBindingSnapshots = (beforeList, afterList) => {
  const beforeMap = new Map((beforeList || []).map(item => [item.key, item]))
  const afterMap = new Map((afterList || []).map(item => [item.key, item]))

  const added = []
  const removed = []
  const changed = []

  afterMap.forEach((afterItem, key) => {
    const beforeItem = beforeMap.get(key)
    if (!beforeItem) {
      added.push(afterItem)
      return
    }
    if (beforeItem.credential_id !== afterItem.credential_id) {
      changed.push({ before: beforeItem, after: afterItem })
    }
  })

  beforeMap.forEach((beforeItem, key) => {
    if (!afterMap.has(key)) {
      removed.push(beforeItem)
    }
  })

  return {
    added,
    removed,
    changed,
    total: added.length + removed.length + changed.length
  }
}

const buildBindingsFromNodeParams = (node, normalizedParams) => {
  const credentialBindings = {}
  const resourceBindings = {}

  normalizedParams.forEach((param) => {
    if (!param?.key) return

    const credentialMatch = /^credentials\.([^.]+)\.credential_id$/.exec(param.key)
    if (credentialMatch) {
      const slotKey = credentialMatch[1]
      const credentialID = Number(param.value || 0)
      if (Number.isFinite(credentialID) && credentialID > 0) {
        credentialBindings[slotKey] = credentialID
      }
      return
    }

    const paramDef = getNodeParams(node.type).find(item => item.key === param.key)
    if (paramDef?.type === 'resource_selector') {
      const value = Number(param.value || 0)
      if (Number.isFinite(value) && value > 0) {
        resourceBindings[param.key] = value
      }
    }
  })

  return {
    credential_bindings: credentialBindings,
    resource_bindings: resourceBindings
  }
}

const mergeCredentialBindingsIntoParams = (params = [], credentialBindings = {}) => {
  const merged = Array.isArray(params) ? [...params] : []
  const byKey = new Map(merged.map((item, idx) => [item?.key, idx]))

  Object.entries(credentialBindings || {}).forEach(([slot, credentialID]) => {
    const key = `credentials.${slot}.credential_id`
    const idx = byKey.get(key)
    if (idx !== undefined) {
      merged[idx] = {
        ...merged[idx],
        value: credentialID
      }
      return
    }
    merged.push({
      key,
      label: slot,
      value: credentialID,
      is_flexible: false
    })
  })

  return merged
}

const buildDefinitionNodes = () => nodes.value.map((node) => {
  const normalizedParams = normalizeNodeParams(node.type, node.params)
  const bindings = buildBindingsFromNodeParams(node, normalizedParams)

  return {
    node_id: node.id,
    node_name: node.name || node.id,
    task_key: normalizeTaskType(node.type),
    task_version: 1,
    ignore_failure: Boolean(node.ignore_failure),
    params: normalizedParams,
    credential_bindings: bindings.credential_bindings,
    resource_bindings: bindings.resource_bindings,
    metadata: {
      x: typeof node.x === 'number' ? node.x : 100,
      y: typeof node.y === 'number' ? node.y : 100,
      description: node.description || ''
    }
  }
})

const buildDefinitionEdges = () => connections.value.map((conn) => ({
  from_node_id: conn.from,
  to_node_id: conn.to,
  condition: null
}))

// 保存流水线
const savePipeline = async () => {
  // 验证 DAG
  const validationErrors = validateDAG()
  if (validationErrors.length > 0) {
    ElMessage.error(`保存失败：\n${validationErrors.join('\n')}`)
    return
  }
  
  try {
    // ========== 步骤 1: 重新生成 node_id ==========
    // 使用分层拓扑排序计算执行顺序
    const executionOrder = getExecutionOrder()  // Map<nodeId, orderNumber>
    
    // 构建 oldId → newId 映射
    const idMapping = new Map()
    executionOrder.forEach((order, oldId) => {
      idMapping.set(oldId, `node_${order}`)
    })
    
    // 更新节点的 id
    nodes.value.forEach(node => {
      const newId = idMapping.get(node.id)
      if (newId) {
        node.id = newId
      }
    })
    
    // 更新连接的 from/to
    connections.value.forEach(conn => {
      if (idMapping.has(conn.from)) conn.from = idMapping.get(conn.from)
      if (idMapping.has(conn.to)) conn.to = idMapping.get(conn.to)
    })
    
    // 更新 predecessors
    nodes.value.forEach(node => {
      if (node.predecessors) {
        node.predecessors = node.predecessors.map(predId => 
          idMapping.has(predId) ? idMapping.get(predId) : predId
        )
      }
    })
    
    const nodeList = buildDefinitionNodes()
    const edges = buildDefinitionEdges()

    const definitionData = {
      nodes: nodeList,
      edges,
      triggers: Array.isArray(loadedDefinition.value?.triggers) ? loadedDefinition.value.triggers : [],
      metadata: {
        ...(loadedDefinition.value?.metadata || {}),
        version: '2.0'
      }
    }

    const nextSnapshot = buildCredentialBindingSnapshot(nodes.value)
    const credentialDiff = diffCredentialBindingSnapshots(loadedCredentialBindingSnapshot.value, nextSnapshot)
    if (credentialDiff.total > 0) {
      const previewLines = []
      credentialDiff.added.slice(0, 4).forEach(item => {
        previewLines.push(`新增绑定：${item.node_name}/${item.slot} -> 凭据 #${item.credential_id}`)
      })
      credentialDiff.changed.slice(0, 4).forEach(item => {
        previewLines.push(`变更绑定：${item.after.node_name}/${item.after.slot} #${item.before.credential_id} -> #${item.after.credential_id}`)
      })
      credentialDiff.removed.slice(0, 4).forEach(item => {
        previewLines.push(`移除绑定：${item.node_name}/${item.slot} (原凭据 #${item.credential_id})`)
      })

      const warningText = [
        `检测到 ${credentialDiff.total} 处凭据绑定变更。`,
      '这可能影响该流水线后续运行认证，以及相关凭据的影响范围统计。',
        ...previewLines
      ].join('\n')

      await ElMessageBox.confirm(warningText, '流水线变更影响提醒', {
        type: 'warning',
        confirmButtonText: '继续保存',
        cancelButtonText: '取消'
      })
    }
    
    console.log('保存流水线配置:', JSON.stringify(definitionData, null, 2))
    
    // 调用保存接口
    const response = await updatePipeline(pipelineId.value, {
      definition_json: JSON.stringify(definitionData)
    })
    
    if (response.code === 200) {
      ElMessage.success('流水线保存成功')
      loadedCredentialBindingSnapshot.value = nextSnapshot
      loadedDefinition.value = definitionData
      saveHistory()
      emit('saved')
    } else {
      ElMessage.error(response.message || '保存失败')
    }
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    console.error('保存流水线失败:', error)
    ElMessage.error('保存失败，请稍后重试')
  }
}

// 保存配置
const saveNodeConfig = () => {
  ElMessage.success('配置已保存')
}

const handleInvalidCredentialSelection = (slotLabel, credential) => {
  const label = slotLabel || '凭据槽位'
  const credentialName = credential?.name || `#${credential?.id || ''}`
  ElMessage.warning(`${label} 已清除不符合当前任务类型限制的凭据：${credentialName}`)
}

const loadTaskTypeDefinitions = async () => {
  try {
    const response = await getPipelineTaskTypes()
    if (response.code !== 200 || !Array.isArray(response.data)) {
      return
    }
    const map = {}
    response.data.forEach(item => {
      if (!item?.type) return
      map[String(item.type).trim().toLowerCase()] = item
    })
    taskTypeDefinitions.value = map
  } catch (error) {
    console.error('加载任务类型定义失败:', error)
  }
}

// 加载流水线配置
const loadPipeline = async () => {
  try {
    const response = await getPipelineDetail(pipelineId.value)
    if (response.code === 200 && response.data) {
      try {
        const configRaw = response.data.definition_json || response.data.config
        if (!configRaw) return
        const config = typeof configRaw === 'string' ? JSON.parse(configRaw) : configRaw
        const normalizedConfig = Array.isArray(config?.nodes)
          ? config
          : {
              version: config?.version || config?.metadata?.version || '2.0',
              nodes: config?.definition_json?.nodes || [],
              edges: config?.definition_json?.edges || config?.edges || [],
              triggers: config?.definition_json?.triggers || config?.triggers || [],
              metadata: config?.metadata || {}
            }
        loadedDefinition.value = {
          triggers: Array.isArray(normalizedConfig.triggers) ? normalizedConfig.triggers : [],
          metadata: normalizedConfig.metadata || { version: normalizedConfig.version || '2.0' }
        }
        
        // 检查是否是新格式（version 2.0 + edges）
        if (normalizedConfig.version === '2.0' && Array.isArray(normalizedConfig.edges)) {
          // 新格式：转换为前端节点格式
          nodes.value = (normalizedConfig.nodes || []).map(node => {
            const nodeID = node.node_id || node.id
            const taskType = normalizeTaskType(node.task_key || node.type)
            const normalizedParams = normalizeNodeParams(taskType, node.params || node.config || {})
            const paramsWithCredentialBindings = mergeCredentialBindingsIntoParams(normalizedParams, node.credential_bindings)
            return {
              id: nodeID,
              type: taskType,
              name: node.node_name || node.name || nodeID,
              description: node.metadata?.description || node.description || '',
              x: typeof node.metadata?.x === 'number' ? node.metadata.x : (typeof node.x === 'number' ? node.x : 100 + (parseInt(String(nodeID || '').replace(/[^0-9]/g, '') || '0') % 20) * 50),
              y: typeof node.metadata?.y === 'number' ? node.metadata.y : (typeof node.y === 'number' ? node.y : 100 + (parseInt(String(nodeID || '').replace(/[^0-9]/g, '') || '0') % 20) * 50),
              width: 200,
              inputs: getNodePorts(taskType, 'input').map(i => ({ ...i, connected: false })),
              outputs: getNodePorts(taskType, 'output').map(o => ({ ...o, connected: false })),
              params: paramsWithCredentialBindings,
              conditions: [],
              predecessors: [],
              status: 'pending',
              ignore_failure: Boolean(node.metadata?.ignore_failure || node.ignore_failure)
            }
          })
          
          // 从 edges 重建 connections
          connections.value = (normalizedConfig.edges || []).map((edge, idx) => ({
            id: `conn_${Date.now()}_${idx}`,
            from: edge.from_node_id || edge.from,
            to: edge.to_node_id || edge.to
          }))
          
          // 从 edges 重建每个节点的 predecessors 数组
          // 这样配置面板中的"前置任务"才能正确显示
          normalizedConfig.edges?.forEach(edge => {
            const fromNodeID = edge.from_node_id || edge.from
            const toNodeID = edge.to_node_id || edge.to
            const targetNode = nodes.value.find(n => n.id === toNodeID)
            if (targetNode) {
              if (!targetNode.predecessors) {
                targetNode.predecessors = []
              }
              if (fromNodeID && !targetNode.predecessors.includes(fromNodeID)) {
                targetNode.predecessors.push(fromNodeID)
              }
            }
          })
          
          // 以 predecessors 为准，重建连接并刷新端口状态
          syncConnectionsWithPredecessors()
          console.log('新格式流水线配置加载成功', { nodes: nodes.value.length, connections: connections.value.length })
        } else {
          // 旧格式兼容
          if (normalizedConfig.nodes && normalizedConfig.nodes.length > 0) {
            nodes.value = normalizedConfig.nodes.map((node) => {
              const taskType = normalizeTaskType(node.task_key || node.type)
              const normalizedParams = normalizeNodeParams(taskType, node.params || node.config || {})
              const paramsWithCredentialBindings = mergeCredentialBindingsIntoParams(normalizedParams, node.credential_bindings)
              return {
                ...node,
                id: node.id,
                type: taskType,
                name: node.name || taskType,
                description: node.description || '',
                x: typeof node.x === 'number' ? node.x : 100,
                y: typeof node.y === 'number' ? node.y : 100,
                width: node.width || 200,
                inputs: getNodePorts(taskType, 'input').map(i => ({ ...i, connected: false })),
                outputs: getNodePorts(taskType, 'output').map(o => ({ ...o, connected: false })),
                params: paramsWithCredentialBindings,
                conditions: Array.isArray(node.conditions) ? node.conditions : [],
                predecessors: Array.isArray(node.predecessors) ? node.predecessors : [],
                status: node.status || 'pending'
              }
            })
          }
          if (normalizedConfig.connections && normalizedConfig.connections.length > 0) {
            connections.value = normalizedConfig.connections
            rebuildPredecessorsFromConnections()
          } else {
            rebuildConnectionsFromPredecessors()
          }
          syncConnectionsWithPredecessors()
          console.log('旧格式流水线配置加载成功', { nodes: nodes.value.length, connections: connections.value.length })
        }
        loadedCredentialBindingSnapshot.value = buildCredentialBindingSnapshot(nodes.value)
      } catch (parseError) {
        console.error('解析流水线配置失败:', parseError)
      }
    }
  } catch (error) {
    console.error('加载流水线配置失败:', error)
  }
}

// 根据predecessors重建connections数组
const rebuildConnectionsFromPredecessors = () => {
  syncConnectionsWithPredecessors()
}

// 更新所有节点的端口连接状态
const updateAllPortsConnectionStatus = () => {
  // 重置所有端口的connected状态
  nodes.value.forEach(node => {
    if (node.inputs) {
      node.inputs.forEach(input => {
        input.connected = connections.value.some(c => c.to === node.id)
      })
    }
    if (node.outputs) {
      node.outputs.forEach(output => {
        output.connected = connections.value.some(c => c.from === node.id)
      })
    }
  })
}

// 初始化
onMounted(async () => {
  await loadTaskTypeDefinitions()
  await loadResources()

  // 加载流水线配置
  await loadPipeline()
  
  // 初始化历史记录
  saveHistory()
  
  // 绑定键盘事件
  window.addEventListener('keydown', handleKeyDown)
  window.addEventListener('keyup', handleKeyUp)
  
  // 初始视图位置
  resetCanvasView()
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyDown)
  window.removeEventListener('keyup', handleKeyUp)
})
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.pipeline-design-container {
  position: relative;
  display: flex;
  height: calc(100vh - 120px);
  background: var(--bg-primary);
  overflow: hidden;
}

.library-toggle-anchor {
  position: absolute;
  top: 18px;
  left: 244px;
  z-index: 30;
  transition: left 0.3s ease;

  &.collapsed {
    left: 12px;
  }

  :deep(.library-toggle-btn) {
    width: 32px;
    height: 32px;
    padding: 0;
    border: 1px solid var(--border-color-light);
    background: color-mix(in srgb, var(--bg-card) 82%, transparent);
    color: var(--text-secondary);
    box-shadow: 0 14px 30px rgba(14, 35, 68, 0.14);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);

    &:hover {
      color: var(--primary-color);
      border-color: color-mix(in srgb, var(--primary-color) 34%, var(--border-color-light));
      background: color-mix(in srgb, var(--bg-elevated) 88%, transparent);
      box-shadow: 0 16px 34px rgba(14, 35, 68, 0.2);
    }
  }
}

/* 左侧组件库面板 */
.components-panel {
  width: 260px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-color);
  transition: width 0.3s ease, border-color 0.3s ease, box-shadow 0.3s ease;
  overflow: hidden;
  flex-shrink: 0;
  min-width: 0;
  box-shadow: 4px 0 24px rgba(0, 0, 0, 0.04);

  &.collapsed {
    border-right-color: transparent;
    box-shadow: none;
  }

  .panel-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 16px;
    color: var(--text-primary);
    font-size: 14px;
    font-weight: 600;
    border-bottom: 1px solid var(--border-color);
    cursor: default;
    background: var(--bg-card);
    box-shadow: var(--shadow-sm);

    &:hover {
      background: var(--bg-secondary);
    }
  }

  .components-content {
    height: calc(100% - 52px);
    overflow-y: auto;
    padding: 12px;
  }

  .component-category {
    margin-bottom: 8px;

    .category-header {
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 10px 12px;
      color: var(--text-secondary);
      font-size: 13px;
      cursor: pointer;
      transition: all 0.2s;
      border-radius: $radius-md;

      &:hover {
        background: var(--bg-secondary);
        color: var(--text-primary);
      }

      .count {
        color: var(--text-muted);
        font-size: 12px;
        margin-left: auto;
      }
    }

    .category-items {
      padding: 4px;
    }

    .component-item {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 12px;
      margin-bottom: 8px;
      background: var(--bg-card);
      border-radius: $radius-md;
      cursor: pointer;
      transition: all $transition-base;
      border: 1px solid var(--border-color-light);
      box-shadow: var(--shadow-sm);

      &:hover {
        background: var(--bg-elevated);
        border-color: var(--primary-color);
        transform: translateX(4px);
        box-shadow: var(--shadow-md);
      }

      &:active {
        box-shadow: var(--shadow-inset);
      }

      .component-icon {
        width: 36px;
        height: 36px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: $radius-md;
        color: white;
        font-size: 18px;
        flex-shrink: 0;
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
      }

      .component-info {
        flex: 1;
        min-width: 0;

        .component-name {
          display: block;
          color: var(--text-primary);
          font-size: 13px;
          font-weight: 600;
        }

        .component-desc {
          display: block;
          color: var(--text-muted);
          font-size: 11px;
          margin-top: 2px;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }
      }
    }
  }
}

/* 画布区域 */
.canvas-area {
  flex: 1;
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.canvas-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px;
  background: var(--glass-bg);
  backdrop-filter: $blur-md;
  -webkit-backdrop-filter: $blur-md;
  border-bottom: 1px solid var(--glass-border);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
  z-index: 10;

  .toolbar-left,
  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  :deep(.el-button) {
    border-radius: $radius-md;
    box-shadow: var(--shadow-sm);
    border: 1px solid var(--border-color-light);
    background: var(--bg-card);
    color: var(--text-secondary);

    &:hover {
      background: var(--bg-elevated);
      color: var(--primary-color);
      box-shadow: var(--shadow-md);
    }

    &:active {
      box-shadow: var(--shadow-inset);
    }

    &.el-button--primary {
      background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
      color: white;
      border: none;
    }
  }
}

.canvas-wrapper {
  flex: 1;
  position: relative;
  overflow: hidden;
  cursor: grab;
  background: var(--pipeline-canvas-bg);

  &:active {
    cursor: grabbing;
  }

  .grid-background {
    position: absolute;
    top: -5000px;
    left: -5000px;
    width: 10000px;
    height: 10000px;
    background-color: var(--surface-base);
    background-image:
      radial-gradient(circle, var(--pipeline-grid-dot) 1px, transparent 1px);
    background-size: 20px 20px;
    pointer-events: none;
  }

  .connections-layer-wrapper {
    position: absolute;
    top: 0;
    left: 0;
    z-index: 10;
    pointer-events: auto;

    .connections-layer {
      position: absolute;
      top: 0;
      left: 0;
      width: 100%;
      height: 100%;
      overflow: visible;

      pointer-events: none;

      .connection-item {
        .connection-line-glow {
          stroke: var(--pipeline-connection-glow);
          stroke-width: 5.5;
          stroke-linecap: round;
          stroke-linejoin: round;
          opacity: 0.22;
          transition: opacity $transition-fast, stroke $transition-fast, stroke-width $transition-fast;
        }

        .connection-line {
          cursor: pointer;
          pointer-events: stroke;
          stroke: var(--pipeline-connection-color);
          stroke-width: 2.35;
          stroke-linecap: round;
          stroke-linejoin: round;
          opacity: 0.9;
          transition: stroke $transition-fast, stroke-width $transition-fast, opacity $transition-fast;
        }

        &:hover {
          .connection-line-glow {
            opacity: 0.38;
            stroke-width: 6.2;
          }

          .connection-line {
            opacity: 0.98;
            stroke-width: 2.7;
          }
        }

        &.selected {
          .connection-line-glow {
            stroke: var(--pipeline-node-selected-ring);
            opacity: 0.52;
            stroke-width: 6.8;
          }

          .connection-line {
            stroke: var(--pipeline-connection-selected);
            stroke-width: 2.9;
            opacity: 1;
          }
        }
      }

      .connecting-line-temp {
        pointer-events: none;
        opacity: 0.62;
        stroke: var(--pipeline-connection-color);
        stroke-width: 1.8;
      }
    }
  }

  .nodes-layer {
    position: absolute;
    top: 0;
    left: 0;
    transform-origin: 0 0;
    z-index: 20;
  }
}

.pipeline-node {
  position: absolute;
  min-width: 220px;
  background: var(--pipeline-node-bg);
  border: 1px solid var(--pipeline-node-border);
  border-radius: $radius-lg;
  cursor: move;
  transition: box-shadow $transition-base, border-color $transition-base, transform $transition-base;
  overflow: visible;
  box-shadow: var(--pipeline-node-shadow);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  isolation: isolate;

  &:hover {
    border-color: var(--pipeline-connection-selected);
    box-shadow: var(--pipeline-node-hover-shadow);
    transform: translateY(-1px);
  }

  &.selected {
    border-color: var(--pipeline-connection-selected);
    box-shadow:
      0 0 0 2px var(--pipeline-node-selected-ring),
      var(--pipeline-node-hover-shadow);
  }

  &.connecting {
    cursor: crosshair;
  }

  .node-port {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 14px;
    cursor: crosshair;
    background: transparent;
    z-index: 2;

    &.input-port {
      left: -7px;
      transform: none;
    }

    &.output-port {
      right: -7px;
      transform: none;
    }
  }

  .node-content {
    position: relative;
    z-index: 1;
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 10px;

    .node-header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 8px;

      .node-main {
        display: flex;
        align-items: center;
        gap: 8px;
        flex: 1;
        min-width: 0;
      }

      .node-order-badge {
        min-width: 24px;
        height: 24px;
        padding: 0 6px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: var(--pipeline-node-track);
        color: var(--primary-color);
        font-size: 11px;
        font-weight: 700;
        border-radius: $radius-full;
        flex-shrink: 0;
      }

      .node-icon {
        width: 32px;
        height: 32px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: $radius-md;
        color: white;
        font-size: 16px;
        flex-shrink: 0;
        box-shadow: 0 2px 6px rgba(0, 0, 0, 0.12);
      }

      .node-info {
        flex: 1;
        min-width: 0;

        .node-name {
          display: block;
          color: var(--text-primary);
          font-size: 13px;
          font-weight: 600;
          line-height: 1.2;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }
      }

      .delete-btn {
        opacity: 0;
        transition: opacity 0.2s;
        color: var(--text-muted);
        cursor: pointer;
        font-size: 16px;
        padding: 4px;
        border-radius: $radius-sm;
        transition: all $transition-fast;

        &:hover {
          color: var(--danger-color);
          background: var(--danger-light);
        }
      }
    }

    .node-meta {
      display: flex;
      align-items: center;
      gap: 6px;
      flex-wrap: wrap;

      .meta-chip {
        display: inline-flex;
        align-items: center;
        height: 22px;
        padding: 0 8px;
        border-radius: $radius-full;
        font-size: 11px;
        font-weight: 500;
        line-height: 1;
        color: var(--text-secondary);
        background: var(--bg-secondary);
        border: 1px solid var(--border-color-light);
      }

      .type-chip {
        color: var(--text-secondary);
      }

      .io-chip {
        color: var(--text-muted);
      }
    }
  }

  &:hover .delete-btn {
    opacity: 1;
  }
}

.empty-state {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  text-align: center;
  color: var(--text-muted);

  :deep(.el-empty__description) {
    color: var(--text-secondary);
  }
}

/* 右侧配置面板 */
.config-panel {
  width: 320px;
  background: var(--bg-sidebar);
  border-left: 1px solid var(--border-color);
  transition: width 0.3s ease, opacity 0.3s ease;
  overflow: hidden;
  flex-shrink: 0;
  box-shadow: -4px 0 24px rgba(0, 0, 0, 0.04);

  .panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 20px;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 600;
    border-bottom: 1px solid var(--border-color);
    background: var(--bg-card);
    box-shadow: var(--shadow-sm);

    .close-btn {
      cursor: pointer;
      color: var(--text-muted);
      padding: 4px;
      border-radius: $radius-sm;
      transition: all $transition-fast;

      &:hover {
        color: var(--text-primary);
        background: var(--bg-secondary);
      }
    }
  }

  .config-content {
    height: calc(100% - 60px);
    overflow-y: auto;
    padding: 20px;
  }

  .config-section {
    margin-bottom: 24px;
    padding: 16px;
    background: var(--bg-card);
    border-radius: $radius-lg;
    box-shadow: var(--shadow-sm);
    border: 1px solid var(--border-color-light);

    .section-title {
      color: var(--text-secondary);
      font-size: 13px;
      font-weight: 600;
      margin-bottom: 16px;
      padding-bottom: 12px;
      border-bottom: 1px solid var(--border-color);
      text-transform: uppercase;
      letter-spacing: 0.5px;
    }
  }

  :deep(.el-form-item__label) {
    color: var(--text-secondary);
    font-weight: 500;
  }

  :deep(.el-input__wrapper) {
    background: var(--bg-secondary);
    border-radius: $radius-md;
    box-shadow: var(--shadow-inset);
    border: 1px solid var(--border-color-light);

    &:hover, &.is-focus {
      border-color: var(--primary-color);
      box-shadow: var(--shadow-inset), 0 0 0 3px var(--primary-light);
    }
  }

  :deep(.el-textarea__inner) {
    background: var(--bg-secondary);
    border-radius: $radius-md;
    box-shadow: var(--shadow-inset);
    border: 1px solid var(--border-color-light);
    color: var(--text-primary);

    &:hover, &:focus {
      border-color: var(--primary-color);
      box-shadow: var(--shadow-inset), 0 0 0 3px var(--primary-light);
    }
  }

  .param-flex-row {
    margin-top: 8px;
    display: flex;
    align-items: center;
  }

  .condition-list {
    .condition-item {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 10px;

      .el-select {
        width: 100px;
      }

      .el-input {
        width: 100px;
      }

      .remove-btn {
        color: var(--danger-color);
        cursor: pointer;
        padding: 4px;
        border-radius: $radius-sm;
        transition: all $transition-fast;

        &:hover {
          background: var(--danger-light);
        }
      }
    }
  }

  .predecessor-list {
    .predecessor-item {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-bottom: 10px;

      .remove-btn {
        color: var(--danger-color);
        cursor: pointer;
        padding: 4px;
        border-radius: $radius-sm;
        transition: all $transition-fast;

        &:hover {
          background: var(--danger-light);
        }
      }
    }
  }

  .config-tip {
    margin-top: 12px;
    font-size: 12px;
    color: var(--text-muted);
    line-height: 1.5;
    padding: 12px;
    background: var(--bg-secondary);
    border-radius: $radius-md;
    border-left: 3px solid var(--info-color);
  }

  .outputs-header {
    display: flex;
    align-items: center;
    gap: 6px;

    .outputs-icon {
      font-size: 14px;
      color: var(--text-muted);
    }
  }

  .outputs-content {
    padding: 8px 0;

    .outputs-tip {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 8px;
      line-height: 1.5;

      code {
        color: var(--primary-color);
        font-family: 'Consolas', 'Monaco', monospace;
        background: var(--bg-secondary);
        padding: 2px 4px;
        border-radius: 2px;
      }

      &.outputs-node-type {
        font-weight: 500;
        color: var(--text-secondary);
      }
    }

    .outputs-list {
      background: var(--bg-secondary);
      border-radius: $radius-md;
      overflow: hidden;

      .output-item {
        display: flex;
        align-items: center;
        padding: 8px 12px;
        cursor: pointer;
        transition: background 0.2s;

        &:not(:last-child) {
          border-bottom: 1px solid var(--border-color);
        }

        &:hover {
          background: var(--bg-card);
        }

        .output-key {
          flex: 0 0 auto;
          font-family: 'Consolas', 'Monaco', monospace;
          font-size: 12px;
          color: var(--primary-color);
          margin-right: 12px;
        }

        .output-desc {
          font-size: 12px;
          color: var(--text-muted);
        }
      }
    }

    .outputs-hint {
      display: flex;
      align-items: center;
      gap: 4px;
      margin-top: 8px;
      font-size: 12px;
      color: var(--text-muted);

      .el-icon {
        font-size: 12px;
      }
    }
  }

  .checkbox-label {
    display: flex;
    flex-direction: column;

    .checkbox-title {
      color: var(--text-primary);
      font-size: 14px;
      font-weight: 500;
    }

    .checkbox-desc {
      color: var(--text-muted);
      font-size: 12px;
      margin-top: 4px;
    }
  }

  .config-actions {
    display: flex;
    gap: 12px;
    margin-top: 24px;
    padding-top: 20px;
    border-top: 1px solid var(--border-color);

    .el-button {
      flex: 1;
      border-radius: $radius-md;
      font-weight: 500;

      &.el-button--primary {
        background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
        border: none;
        box-shadow: var(--shadow-md);

        &:hover {
          box-shadow: var(--shadow-lg);
          transform: translateY(-1px);
        }
      }

      &.el-button--danger {
        background: var(--danger-light);
        color: var(--danger-color);
        border: none;

        &:hover {
          background: var(--danger-color);
          color: white;
        }
      }
    }
  }
}

/* 滚动条样式 */
::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

::-webkit-scrollbar-track {
  background: transparent;
}

::-webkit-scrollbar-thumb {
  background: var(--text-muted);
  border-radius: 3px;

  &:hover {
    background: var(--text-tertiary);
  }
}
</style>
