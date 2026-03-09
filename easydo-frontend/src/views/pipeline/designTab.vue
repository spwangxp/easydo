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
                v-if="!param.show_if || selectedNode.params[param.show_if.key] === param.show_if.value"
              >
                <el-input 
                  v-if="param.type === 'text'"
                  v-model="selectedNode.params[param.key]"
                  :placeholder="param.placeholder"
                  @change="updateNode(selectedNode)"
                />
                <el-input 
                  v-else-if="param.type === 'textarea'"
                  v-model="selectedNode.params[param.key]"
                  type="textarea"
                  :rows="4"
                  :placeholder="param.placeholder"
                  @change="updateNode(selectedNode)"
                />
                <el-select 
                  v-else-if="param.type === 'select'"
                  v-model="selectedNode.params[param.key]"
                  :placeholder="param.placeholder"
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
                  v-model="selectedNode.params[param.key]"
                  @change="updateNode(selectedNode)"
                />
                <el-input-number 
                  v-else-if="param.type === 'number'"
                  v-model="selectedNode.params[param.key]"
                  :min="param.min"
                  :max="param.max"
                  @change="updateNode(selectedNode)"
                />
                <CredentialSelector
                  v-else-if="param.type === 'credential_selector'"
                  v-model="selectedNode.params[param.key]"
                  :credential-type="param.credential_type"
                  :credential-category="param.credential_category"
                  @change="updateNode(selectedNode)"
                />
              </el-form-item>
            </template>
          </el-form>
        </div>

        <div class="config-section" v-if="getNodeCredentialSlots(selectedNode.type).length > 0">
          <div class="section-title">凭据绑定</div>
          <el-form label-position="top" size="small">
            <el-form-item
              v-for="slot in getNodeCredentialSlots(selectedNode.type)"
              :key="slot.slot"
              :label="`${slot.label || slot.slot}${slot.required ? '（必填）' : ''}`"
            >
              <CredentialSelector
                v-model="selectedNode.params[`credentials.${slot.slot}.credential_id`]"
                :credential-types="slot.allowed_types || []"
                :credential-categories="slot.allowed_categories || []"
                @change="updateNode(selectedNode)"
              />
            </el-form-item>
          </el-form>
          <div class="config-tip">
            修改凭据绑定可能影响当前流水线后续运行，以及所有引用该密钥的任务认证行为。
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
                  <span class="checkbox-title">忽略失败</span>
                  <span class="checkbox-desc">当前置任务执行失败时，仍然继续执行当前任务</span>
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
  Check
} from '@element-plus/icons-vue'
import { updatePipeline, getPipelineDetail, getPipelineTaskTypes } from '@/api/pipeline'
import { buildConnectionPath } from './connectionGeometry'

const route = useRoute()
const pipelineId = computed(() => parseInt(route.params.id))
const taskTypeDefinitions = ref({})
const loadedCredentialBindingSnapshot = ref([])

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

// 组件类别
const componentCategories = [
  {
    name: 'source',
    label: '代码源',
    components: [
      { type: 'git_clone', name: 'Git 检出', description: 'Git 代码仓库检出', icon: 'Connection', color: '#67C23A', inputs: [], outputs: [{ label: '代码', key: 'code' }] }
    ]
  },
  {
    name: 'build',
    label: '构建',
    components: [
      { type: 'npm', name: 'NPM 构建', description: 'NPM 包构建', icon: 'Box', color: '#E6A23C', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '产物', key: 'artifact' }] },
      { type: 'maven', name: 'Maven 构建', description: 'Maven 项目构建', icon: 'Box', color: '#E6A23C', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '产物', key: 'artifact' }] },
      { type: 'gradle', name: 'Gradle 构建', description: 'Gradle 项目构建', icon: 'Box', color: '#E6A23C', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '产物', key: 'artifact' }] },
      { type: 'docker', name: 'Docker 构建', description: 'Docker 镜像构建', icon: 'Box', color: '#E6A23C', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '镜像', key: 'image' }] },
      { type: 'artifact_publish', name: '制品归档', description: '归档构建产物', icon: 'UploadFilled', color: '#E6A23C', inputs: [{ label: '产物', key: 'artifact' }], outputs: [{ label: '结果', key: 'result' }] }
    ]
  },
  {
    name: 'test',
    label: '测试',
    components: [
      { type: 'unit', name: '单元测试', description: '执行单元测试', icon: 'Document', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '报告', key: 'report' }] },
      { type: 'integration', name: '集成测试', description: '执行集成测试', icon: 'Document', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '报告', key: 'report' }] },
      { type: 'e2e', name: 'E2E 测试', description: '端到端测试', icon: 'Document', color: '#409EFF', inputs: [{ label: '应用', key: 'app' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'coverage', name: '代码覆盖率', description: '代码覆盖率分析', icon: 'DataAnalysis', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '报告', key: 'report' }] },
      { type: 'lint', name: '代码检查', description: '静态检查/格式检查', icon: 'Warning', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '结果', key: 'result' }] }
    ]
  },
  {
    name: 'deploy',
    label: '部署',
    components: [
      { type: 'ssh', name: 'SSH 部署', description: '通过 SSH 部署', icon: 'Promotion', color: '#F56C6C', inputs: [{ label: '产物', key: 'artifact' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'kubernetes', name: 'K8s 部署', description: 'Kubernetes 部署', icon: 'Promotion', color: '#F56C6C', inputs: [{ label: '镜像', key: 'image' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'docker-run', name: 'Docker 运行', description: 'Docker 容器运行', icon: 'Promotion', color: '#F56C6C', inputs: [{ label: '镜像', key: 'image' }], outputs: [{ label: '结果', key: 'result' }] }
    ]
  },
  {
    name: 'notify',
    label: '通知',
    components: [
      { type: 'email', name: '邮件通知', description: '邮件通知', icon: 'Message', color: 'var(--text-muted)', inputs: [{ label: '消息', key: 'message' }], outputs: [] },
      { type: 'webhook', name: 'Webhook', description: 'Webhook 回调', icon: 'Link', color: 'var(--text-muted)', inputs: [{ label: '数据', key: 'data' }], outputs: [] },
      { type: 'in_app', name: '站内信', description: '发送站内通知消息', icon: 'Bell', color: 'var(--text-muted)', inputs: [{ label: '消息', key: 'message' }], outputs: [] }
    ]
  },
  {
    name: 'utils',
    label: '工具',
    components: [
      { type: 'shell', name: 'Shell 脚本', description: '执行 Shell 脚本', icon: 'Terminal', color: '#8c33fe', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '输出', key: 'output' }] },
      { type: 'sleep', name: '等待', description: '暂停等待', icon: 'Clock', color: '#8c33fe', inputs: [], outputs: [] }
    ]
  }
]

// 获取类别组件数量
const getCategoryCount = (categoryName) => {
  const category = componentCategories.find(c => c.name === categoryName)
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
  for (const cat of componentCategories) {
    const comp = cat.components.find(c => c.type === type)
    if (comp) return comp.name
  }
  return type
}

// 获取节点图标
const getNodeIcon = (type) => {
  for (const cat of componentCategories) {
    const comp = cat.components.find(c => c.type === type)
    if (comp) return comp.icon
  }
  return 'Box'
}

// 获取节点颜色
const getNodeColor = (type) => {
  for (const cat of componentCategories) {
    const comp = cat.components.find(c => c.type === type)
    if (comp) return comp.color
  }
  return '#409EFF'
}

const taskTypeAliasMap = {
  github: 'git_clone',
  gitee: 'git_clone',
  agent: 'shell',
  custom: 'shell',
  script: 'shell',
  dingtalk: 'webhook',
  wechat: 'webhook'
}

const normalizeTaskType = (type) => {
  if (!type) return ''
  const normalized = String(type).trim().toLowerCase()
  return taskTypeAliasMap[normalized] || normalized
}

const getNodeCredentialSlots = (type) => {
  const normalized = normalizeTaskType(type)
  const def = taskTypeDefinitions.value[normalized]
  if (!def || !Array.isArray(def.credential_slots)) {
    return []
  }
  return def.credential_slots
}

// 获取节点参数定义
const getNodeParams = (type) => {
  const paramDefs = {
    git_clone: [
      { key: 'repository.url', label: '仓库地址', type: 'text', placeholder: 'git@github.com:company/app.git 或 https://github.com/company/app.git' },
      { key: 'repository.branch', label: '分支', type: 'text', placeholder: 'main', value: 'main' },
      { key: 'repository.target_dir', label: '目标目录', type: 'text', placeholder: './app', value: './app' },
      { key: 'repository.commit_id', label: '指定提交（可选）', type: 'text', placeholder: '留空则检出最新提交' },
      { key: 'repository.depth', label: '克隆深度', type: 'number', min: 1, value: 10 },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 60, value: 300 }
    ],
    npm: [
      { key: 'command', label: '构建命令', type: 'text', placeholder: 'npm run build' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    maven: [
      { key: 'command', label: '构建命令', type: 'text', placeholder: 'mvn -B clean package' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    gradle: [
      { key: 'command', label: '构建命令', type: 'text', placeholder: './gradlew build' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    docker: [
      { key: 'image_name', label: '镜像名称', type: 'text', placeholder: 'myapp' },
      { key: 'image_tag', label: '镜像标签', type: 'text', placeholder: 'latest' },
      { key: 'dockerfile', label: 'Dockerfile 路径', type: 'text', placeholder: './Dockerfile', value: './Dockerfile' },
      { key: 'context', label: '构建上下文', type: 'text', placeholder: '.', value: '.' },
      { key: 'push', label: '构建后推送', type: 'boolean', value: true },
      { key: 'registry', label: '仓库地址', type: 'text', placeholder: 'registry.example.com' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 60, value: 600 }
    ],
    artifact_publish: [
      { key: 'artifact_path', label: '产物路径', type: 'text', placeholder: './dist' },
      { key: 'target_dir', label: '归档目标目录', type: 'text', placeholder: './artifacts' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 60, value: 300 }
    ],
    unit: [
      { key: 'command', label: '测试命令', type: 'text', placeholder: 'npm run test:unit' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    integration: [
      { key: 'command', label: '测试命令', type: 'text', placeholder: 'npm run test:integration' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    e2e: [
      { key: 'command', label: '测试命令', type: 'text', placeholder: 'npm run test:e2e' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    coverage: [
      { key: 'command', label: '测试命令', type: 'text', placeholder: 'npm run test:coverage' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    lint: [
      { key: 'command', label: '检查命令', type: 'text', placeholder: 'npm run lint' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    ssh: [
      { key: 'host', label: '主机地址', type: 'text', placeholder: '192.168.1.1' },
      { key: 'port', label: '端口', type: 'number', placeholder: '22' },
      { key: 'user', label: '用户名', type: 'text', placeholder: 'root' },
      { key: 'script', label: '远端脚本', type: 'textarea', placeholder: 'cd /app && ./deploy.sh' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 600 }
    ],
    kubernetes: [
      { key: 'command', label: '部署命令', type: 'textarea', placeholder: 'kubectl apply -f deploy.yaml' },
      { key: 'manifest', label: 'Manifest 文件(可选)', type: 'text', placeholder: './deploy.yaml' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 600 }
    ],
    'docker-run': [
      { key: 'registry', label: '镜像仓库(可选)', type: 'text', placeholder: 'registry.example.com' },
      { key: 'image_name', label: '镜像名称', type: 'text', placeholder: 'myapp' },
      { key: 'image_tag', label: '镜像标签', type: 'text', placeholder: 'latest' },
      { key: 'container_name', label: '容器名称(可选)', type: 'text', placeholder: 'myapp-web' },
      { key: 'run_args', label: '运行参数', type: 'text', placeholder: '-p 8080:8080 -e ENV=prod' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 600 }
    ],
    shell: [
      { key: 'script', label: '脚本内容', type: 'textarea', placeholder: 'echo "hello"' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'env', label: '环境变量', type: 'textarea', placeholder: 'JSON格式，如 {"KEY": "value"}' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    sleep: [
      { key: 'seconds', label: '等待秒数', type: 'number', min: 1, max: 3600, placeholder: '60' }
    ],
    email: [
      { key: 'to', label: '收件人', type: 'textarea', placeholder: '用逗号分隔多个邮箱，如: dev@example.com, test@example.com' },
      { key: 'cc', label: '抄送', type: 'textarea', placeholder: '用逗号分隔多个邮箱' },
      { key: 'subject', label: '邮件主题', type: 'text', placeholder: '构建完成通知' },
      { key: 'body', label: '邮件正文', type: 'textarea', placeholder: '支持 HTML 格式' },
      { key: 'smtp_host', label: 'SMTP 主机', type: 'text', placeholder: 'smtp.example.com' },
      { key: 'smtp_port', label: 'SMTP 端口', type: 'number', min: 1, max: 65535, value: 25 },
      { key: 'smtp_username', label: 'SMTP 用户名', type: 'text', placeholder: 'noreply@example.com' },
      { key: 'smtp_password', label: 'SMTP 密码', type: 'text', placeholder: '应用密码或授权码' },
      { key: 'from', label: '发件人', type: 'text', placeholder: 'noreply@example.com' },
      { key: 'body_type', label: '正文类型', type: 'select', options: [
        { label: '纯文本', value: 'text' },
        { label: 'HTML', value: 'html' }
      ], value: 'text' }
    ],
    webhook: [
      { key: 'url', label: 'Webhook 地址', type: 'text', placeholder: 'https://example.com/webhook' },
      { key: 'method', label: '请求方法', type: 'select', options: [
        { label: 'POST', value: 'POST' },
        { label: 'PUT', value: 'PUT' },
        { label: 'PATCH', value: 'PATCH' }
      ], value: 'POST' },
      { key: 'headers_json', label: '请求头(JSON)', type: 'textarea', placeholder: '{"Authorization":"Bearer xxx"}' },
      { key: 'body', label: '请求体', type: 'textarea', placeholder: '{"status":"success"}' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 300, value: 10 }
    ],
    in_app: [
      { key: 'title', label: '消息标题', type: 'text', placeholder: '流水线通知' },
      { key: 'content', label: '消息内容', type: 'textarea', placeholder: '部署已完成' },
      { key: 'message_type', label: '消息类型', type: 'select', options: [
        { label: '系统', value: 'system' },
        { label: '告警', value: 'alert' },
        { label: '警告', value: 'warning' }
      ], value: 'system' },
      { key: 'priority', label: '优先级', type: 'number', min: 0, max: 2, value: 0 },
      { key: 'metadata_json', label: '附加元数据(JSON)', type: 'textarea', placeholder: '{"scope":"pipeline"}' }
    ]
  }
  return paramDefs[type] || []
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
      params: {},
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
    params: {},
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
      to: node.id,
      ignore_failure: false
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
        to: node.id,
        ignore_failure: existing?.ignore_failure || false
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

  // 单节点无边是允许的（简单任务）
  // 多节点必须包含依赖边
  if (nodes.value.length > 1 && (!connections.value || connections.value.length === 0)) {
    errors.push('多节点流水线必须包含依赖边')
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

// 计算节点的执行顺序（基于拓扑排序）
const getExecutionOrder = () => {
  const orderMap = new Map()
  const orderList = []

  // 构建邻接表和入度表
  const adj = new Map()
  const inDegree = new Map()

  // 初始化
  nodes.value.forEach(node => {
    adj.set(node.id, [])
    inDegree.set(node.id, 0)
    orderMap.set(node.id, null)
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

  // 找出所有入度为0的节点（入口节点）
  const queue = []
  inDegree.forEach((degree, nodeId) => {
    if (degree === 0) {
      queue.push(nodeId)
    }
  })

  // 拓扑排序
  let order = 1
  while (queue.length > 0) {
    const current = queue.shift()
    orderMap.set(current, order++)
    orderList.push(current)

    const neighbors = adj.get(current) || []
    neighbors.forEach(neighbor => {
      inDegree.set(neighbor, inDegree.get(neighbor) - 1)
      if (inDegree.get(neighbor) === 0) {
        queue.push(neighbor)
      }
    })
  }

  return orderMap
}

const buildCredentialBindingSnapshot = (nodeList) => {
  const list = Array.isArray(nodeList) ? nodeList : []
  const result = []

  list.forEach(node => {
    const params = node?.params || {}
    Object.entries(params).forEach(([key, value]) => {
      const match = /^credentials\.([^.]+)\.credential_id$/.exec(key)
      if (!match) return

      const slot = match[1]
      const credentialID = Number(value || 0)
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

// 保存流水线
const savePipeline = async () => {
  // 验证 DAG
  const validationErrors = validateDAG()
  if (validationErrors.length > 0) {
    ElMessage.error(`保存失败：\n${validationErrors.join('\n')}`)
    return
  }
  
  try {
    // 准备保存数据 - 转换为后端需要的格式
    const nodeList = nodes.value.map(node => {
      // 将 params 中的嵌套配置转换为平铺的 config
      const config = {}
      
      // 处理嵌套配置（如 repository.url -> config.repository.url）
      for (const [key, value] of Object.entries(node.params || {})) {
        if (key.includes('.')) {
          // 嵌套键，如 'repository.url'
          const keys = key.split('.')
          let current = config
          for (let i = 0; i < keys.length - 1; i++) {
            if (!current[keys[i]]) {
              current[keys[i]] = {}
            }
            current = current[keys[i]]
          }
          current[keys[keys.length - 1]] = value
        } else {
          config[key] = value
        }
      }
      
      // 对于特定类型，确保有正确的默认配置
      if (node.type === 'shell' && !config.script && node.params?.script) {
        config.script = node.params.script
      }
      if (node.type === 'docker' && !config.image_name && node.params?.tag) {
        config.image_name = node.params.tag?.replace(/:.*$/, '') || 'myapp'
        config.image_tag = node.params.tag?.replace(/^.*:/, '') || 'latest'
      }
      if (node.type === 'git_clone') {
        if (!config.repository) {
          config.repository = {}
        }
        if (!config.repository.branch) config.repository.branch = 'main'
        if (!config.repository.target_dir) config.repository.target_dir = './app'
      }
      
      return {
        id: node.id,
        type: node.type,
        name: node.name,
        // 保存节点位置
        x: typeof node.x === 'number' ? node.x : 100,
        y: typeof node.y === 'number' ? node.y : 100,
        config: config,
        timeout: node.params?.timeout || 3600,
        ignore_failure: node.ignore_failure || false
      }
    })
    
    // 转换连接为边
    const edges = connections.value.map(conn => ({
      from: conn.from,
      to: conn.to,
      ignore_failure: conn.ignore_failure || false
    }))
    
    const pipelineData = {
      version: '2.0',
      nodes: nodeList,
      edges: edges
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
        '这可能影响该流水线后续运行认证，以及相关密钥的影响范围统计。',
        ...previewLines
      ].join('\n')

      await ElMessageBox.confirm(warningText, '流水线变更影响提醒', {
        type: 'warning',
        confirmButtonText: '继续保存',
        cancelButtonText: '取消'
      })
    }
    
    console.log('保存流水线配置:', JSON.stringify(pipelineData, null, 2))
    
    // 调用保存接口
    const response = await updatePipeline(pipelineId.value, {
      config: JSON.stringify(pipelineData)
    })
    
    if (response.code === 200) {
      ElMessage.success('流水线保存成功')
      loadedCredentialBindingSnapshot.value = nextSnapshot
      saveHistory()
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
    if (response.code === 200 && response.data && response.data.config) {
      try {
        const config = JSON.parse(response.data.config)
        
        // 检查是否是新格式（version 2.0 + edges）
        if (config.version === '2.0' && config.edges) {
          // 新格式：转换为前端节点格式
          nodes.value = (config.nodes || []).map(node => {
            // 将嵌套的 config 展平为 params
            const params = {}

            const flatten = (obj, prefix = '') => {
              for (const [key, value] of Object.entries(obj)) {
                const newKey = prefix ? `${prefix}.${key}` : key
                if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
                  flatten(value, newKey)
                } else {
                  params[newKey] = value
                }
              }
            }
            flatten(node.config || {})

            // 保留节点位置，如果保存时没有位置则使用默认值
            return {
              id: node.id,
              type: node.type,
              name: node.name,
              description: '',
              // 优先使用保存的位置，否则使用计算位置
              x: typeof node.x === 'number' ? node.x : 100 + (parseInt(node.id.replace(/[^0-9]/g, '') || '0') % 20) * 50,
              y: typeof node.y === 'number' ? node.y : 100 + (parseInt(node.id.replace(/[^0-9]/g, '') || '0') % 20) * 50,
              width: 200,
              inputs: getDefaultInputs(node.type),
              outputs: getDefaultOutputs(node.type),
              params: params,
              conditions: [],
              predecessors: [],
              status: 'pending'
            }
          })
          
          // 从 edges 重建 connections
          connections.value = (config.edges || []).map((edge, idx) => ({
            id: `conn_${Date.now()}_${idx}`,
            from: edge.from,
            to: edge.to,
            ignore_failure: edge.ignore_failure || false
          }))
          
          // 从 edges 重建每个节点的 predecessors 数组
          // 这样配置面板中的"前置任务"才能正确显示
          config.edges?.forEach(edge => {
            const targetNode = nodes.value.find(n => n.id === edge.to)
            if (targetNode) {
              if (!targetNode.predecessors) {
                targetNode.predecessors = []
              }
              if (!targetNode.predecessors.includes(edge.from)) {
                targetNode.predecessors.push(edge.from)
              }
            }
          })
          
          // 以 predecessors 为准，重建连接并刷新端口状态
          syncConnectionsWithPredecessors()
          console.log('新格式流水线配置加载成功', { nodes: nodes.value.length, connections: connections.value.length })
        } else {
          // 旧格式兼容
          if (config.nodes && config.nodes.length > 0) {
            nodes.value = config.nodes
          }
          if (config.connections && config.connections.length > 0) {
            connections.value = config.connections
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

// 获取默认输入端口
const getDefaultInputs = (type) => {
  const inputsMap = {
    git_clone: [],
    npm: [{ label: '代码', key: 'code' }],
    maven: [{ label: '代码', key: 'code' }],
    gradle: [{ label: '代码', key: 'code' }],
    shell: [{ label: '输入', key: 'input' }],
    docker: [{ label: '代码', key: 'code' }],
    artifact_publish: [{ label: '产物', key: 'artifact' }],
    unit: [{ label: '代码', key: 'code' }],
    integration: [{ label: '代码', key: 'code' }],
    e2e: [{ label: '应用', key: 'app' }],
    coverage: [{ label: '代码', key: 'code' }],
    lint: [{ label: '代码', key: 'code' }],
    ssh: [{ label: '产物', key: 'artifact' }],
    kubernetes: [{ label: '镜像', key: 'image' }],
    'docker-run': [{ label: '镜像', key: 'image' }],
    sleep: [],
    email: [{ label: '消息', key: 'message' }],
    webhook: [{ label: '数据', key: 'data' }],
    in_app: [{ label: '消息', key: 'message' }],
    default: [{ label: '输入', key: 'input' }]
  }
  return inputsMap[type] || inputsMap.default
}

// 获取默认输出端口
const getDefaultOutputs = (type) => {
  const outputsMap = {
    git_clone: [{ label: '代码', key: 'code' }],
    npm: [{ label: '产物', key: 'artifact' }],
    maven: [{ label: '产物', key: 'artifact' }],
    gradle: [{ label: '产物', key: 'artifact' }],
    shell: [{ label: '输出', key: 'output' }],
    docker: [{ label: '镜像', key: 'image' }],
    artifact_publish: [{ label: '结果', key: 'result' }],
    unit: [{ label: '报告', key: 'report' }],
    integration: [{ label: '报告', key: 'report' }],
    e2e: [{ label: '结果', key: 'result' }],
    coverage: [{ label: '报告', key: 'report' }],
    lint: [{ label: '结果', key: 'result' }],
    ssh: [{ label: '结果', key: 'result' }],
    kubernetes: [{ label: '结果', key: 'result' }],
    'docker-run': [{ label: '结果', key: 'result' }],
    sleep: [],
    email: [],
    webhook: [],
    in_app: [],
    default: [{ label: '输出', key: 'output' }]
  }
  return outputsMap[type] || outputsMap.default
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
