<template>
  <div class="pipeline-design-container">
    <!-- 左侧组件库面板 -->
    <div class="components-panel" :style="{ width: leftPanelCollapsed ? '0' : '260px' }">
      <div class="panel-header">
        <el-icon><component :is="leftPanelCollapsed ? 'Expand' : 'Fold'" /></el-icon>
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
        <div class="toolbar-center">
          <el-button-group>
            <el-tooltip content="缩小" placement="bottom">
              <el-button @click="zoomOut">
                <el-icon><ZoomOut /></el-icon>
              </el-button>
            </el-tooltip>
            <el-button class="zoom-text">{{ Math.round(canvasScale * 100) }}%</el-button>
            <el-tooltip content="放大" placement="bottom">
              <el-button @click="zoomIn">
                <el-icon><ZoomIn /></el-icon>
              </el-button>
            </el-tooltip>
            <el-tooltip content="适应屏幕" placement="bottom">
              <el-button @click="fitToScreen">
                <el-icon><FullScreen /></el-icon>
              </el-button>
            </el-tooltip>
          </el-button-group>
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
        @wheel="handleWheel"
        @dragover="handleDragOver"
        @drop="handleDrop"
      >
        <!-- 网格背景 -->
        <div 
          class="grid-background"
          :style="{
            backgroundPosition: `${canvasOffset.x}px ${canvasOffset.y}px`,
            backgroundSize: `${20 * canvasScale}px`
          }"
        ></div>

        <!-- 连接线层 -->
        <div 
          class="connections-layer-wrapper"
          :style="{
            transform: `translate(${canvasOffset.x}px, ${canvasOffset.y}px) scale(${canvasScale})`,
            transformOrigin: '0 0'
          }"
        >
          <svg class="connections-layer" :viewBox="`0 0 ${canvasWidth} ${canvasHeight}`">
            <defs>
              <!-- 箭头标记 - 红色连接线 -->
              <marker id="arrowhead" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
                <polygon points="0 0, 10 3.5, 0 7" fill="#FF0000" />
              </marker>
              <!-- 虚线连接标记 -->
              <marker id="arrowhead-dashed" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
                <polygon points="0 0, 10 3.5, 0 7" fill="#909399" />
              </marker>
            </defs>
            <!-- 已完成的连接线 -->
            <path
              v-for="conn in connections"
              :key="conn.id"
              :d="getConnectionPath(conn)"
              class="connection-line"
              :class="{ selected: selectedConnection?.id === conn.id }"
              stroke="#FF0000"
              stroke-width="3"
              fill="none"
              marker-end="url(#arrowhead)"
              @click="selectConnection(conn)"
              @dblclick="deleteConnection(conn)"
            />
            <!-- 正在拖拽的连接线 -->
            <path
              v-if="connectingLine"
              :d="connectingLine.path"
              class="connecting-line-temp"
              stroke="#909399"
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
            transform: `translate(${canvasOffset.x}px, ${canvasOffset.y}px) scale(${canvasScale})`
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
              v-for="(input, idx) in node.inputs" 
              :key="`input-${idx}`"
              class="node-port input-port"
              :class="{ connected: input.connected }"
              @mousedown.stop="startConnection(node, input, 'input')"
              @mouseup.stop="finishConnection(node, input, 'input')"
            >
              <div class="port-dot"></div>
              <span class="port-label">{{ input.label }}</span>
            </div>

            <!-- 节点内容 -->
            <div class="node-content">
              <div class="node-header">
                <!-- 执行顺序标识 -->
                <div v-if="executionOrder.get(node.id)" class="node-order-badge">
                  {{ executionOrder.get(node.id) }}
                </div>
                <div class="node-icon" :style="{ background: getNodeColor(node.type) }">
                  <el-icon><component :is="getNodeIcon(node.type)" /></el-icon>
                </div>
                <div class="node-info">
                  <span class="node-name">{{ node.name }}</span>
                  <span class="node-type">{{ getNodeTypeLabel(node.type) }}</span>
                </div>
                <div class="node-actions">
                  <el-icon class="delete-btn" @click.stop="deleteNode(node)"><Close /></el-icon>
                </div>
              </div>
              <div class="node-status" v-if="node.status">
                <el-tag :type="getStatusType(node.status)" size="small">
                  {{ getStatusText(node.status) }}
                </el-tag>
              </div>
            </div>

            <!-- 输出端口 -->
            <div 
              v-for="(output, idx) in node.outputs" 
              :key="`output-${idx}`"
              class="node-port output-port"
              :class="{ connected: output.connected }"
              @mousedown.stop="startConnection(node, output, 'output')"
              @mouseup.stop="finishConnection(node, output, 'output')"
            >
              <span class="port-label">{{ output.label }}</span>
              <div class="port-dot"></div>
            </div>
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
  ZoomIn,
  ZoomOut,
  FullScreen,
  Grid,
  Delete,
  Close,
  Plus,
  Check
} from '@element-plus/icons-vue'
import { updatePipeline, getPipelineDetail } from '@/api/pipeline'

const route = useRoute()
const pipelineId = computed(() => parseInt(route.params.id))

// 画布状态
const canvasArea = ref(null)
const canvasWrapper = ref(null)
const canvasScale = ref(1)
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
      { type: 'git_clone', name: 'Git 检出', description: 'Git 代码仓库检出', icon: 'Connection', color: '#67C23A', inputs: [], outputs: [{ label: '代码', key: 'code' }] },
      { type: 'github', name: 'GitHub', description: 'GitHub 代码仓库', icon: 'Connection', color: '#67C23A', inputs: [], outputs: [{ label: '代码', key: 'code' }] },
      { type: 'gitee', name: 'Gitee', description: 'Gitee 代码仓库', icon: 'Connection', color: '#67C23A', inputs: [], outputs: [{ label: '代码', key: 'code' }] }
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
      { type: 'custom', name: '自定义脚本', description: '自定义构建脚本', icon: 'Edit', color: '#E6A23C', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '输出', key: 'output' }] }
    ]
  },
  {
    name: 'test',
    label: '测试',
    components: [
      { type: 'unit', name: '单元测试', description: '执行单元测试', icon: 'Document', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '报告', key: 'report' }] },
      { type: 'integration', name: '集成测试', description: '执行集成测试', icon: 'Document', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '报告', key: 'report' }] },
      { type: 'e2e', name: 'E2E 测试', description: '端到端测试', icon: 'Document', color: '#409EFF', inputs: [{ label: '应用', key: 'app' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'coverage', name: '代码覆盖率', description: '代码覆盖率分析', icon: 'DataAnalysis', color: '#409EFF', inputs: [{ label: '代码', key: 'code' }], outputs: [{ label: '报告', key: 'report' }] }
    ]
  },
  {
    name: 'deploy',
    label: '部署',
    components: [
      { type: 'ssh', name: 'SSH 部署', description: '通过 SSH 部署', icon: 'Promotion', color: '#F56C6C', inputs: [{ label: '产物', key: 'artifact' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'kubernetes', name: 'K8s 部署', description: 'Kubernetes 部署', icon: 'Promotion', color: '#F56C6C', inputs: [{ label: '镜像', key: 'image' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'docker-run', name: 'Docker 运行', description: 'Docker 容器运行', icon: 'Promotion', color: '#F56C6C', inputs: [{ label: '镜像', key: 'image' }], outputs: [{ label: '结果', key: 'result' }] },
      { type: 'script', name: '自定义脚本', description: '自定义部署脚本', icon: 'Edit', color: '#F56C6C', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '输出', key: 'output' }] }
    ]
  },
  {
    name: 'notify',
    label: '通知',
    components: [
      { type: 'dingtalk', name: '钉钉通知', description: '钉钉机器人通知', icon: 'Bell', color: '#909399', inputs: [{ label: '消息', key: 'message' }], outputs: [] },
      { type: 'wechat', name: '企业微信', description: '企业微信通知', icon: 'Bell', color: '#909399', inputs: [{ label: '消息', key: 'message' }], outputs: [] },
      { type: 'email', name: '邮件通知', description: '邮件通知', icon: 'Message', color: '#909399', inputs: [{ label: '消息', key: 'message' }], outputs: [] },
      { type: 'webhook', name: 'Webhook', description: 'Webhook 回调', icon: 'Link', color: '#909399', inputs: [{ label: '数据', key: 'data' }], outputs: [] }
    ]
  },
  {
    name: 'utils',
    label: '工具',
    components: [
      { type: 'shell', name: 'Shell 脚本', description: '执行 Shell 脚本', icon: 'Terminal', color: '#8c33fe', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '输出', key: 'output' }] },
      { type: 'agent', name: 'Agent 执行', description: 'Agent 远程执行任务', icon: 'Monitor', color: '#00d4aa', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '输出', key: 'output' }] },
      { type: 'condition', name: '条件判断', description: '条件分支处理', icon: 'Share', color: '#8c33fe', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '是', key: 'yes' }, { label: '否', key: 'no' }] },
      { type: 'parallel', name: '并行执行', description: '并行执行多个任务', icon: 'Share', color: '#8c33fe', inputs: [{ label: '输入', key: 'input' }], outputs: [{ label: '输出', key: 'output' }] },
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

// 获取节点参数定义
const getNodeParams = (type) => {
  const paramDefs = {
    git_clone: [
      { key: 'repository.url', label: '仓库地址', type: 'text', placeholder: 'git@github.com:company/app.git 或 https://github.com/company/app.git' },
      { key: 'repository.branch', label: '分支', type: 'text', placeholder: 'main', value: 'main' },
      { key: 'repository.target_dir', label: '目标目录', type: 'text', placeholder: './app', value: './app' },
      { key: 'repository.commit_id', label: '指定提交（可选）', type: 'text', placeholder: '留空则检出最新提交' },
      { key: 'repository.shallow_clone', label: '浅克隆', type: 'boolean', value: true },
      { key: 'repository.depth', label: '克隆深度', type: 'number', min: 1, value: 10 },
      { key: 'repository.submodule', label: '克隆子模块', type: 'boolean', value: false },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 60, value: 300 }
    ],
    git: [
      { key: 'url', label: '仓库地址', type: 'text', placeholder: 'https://github.com/xxx/xxx.git' },
      { key: 'branch', label: '分支', type: 'text', placeholder: 'main' },
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '无需认证', value: 'none' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择凭据', type: 'credential_selector', credential_type: 'TOKEN', show_if: { key: 'auth_mode', value: 'credential' } }
    ],
    github: [
      { key: 'url', label: '仓库地址', type: 'text', placeholder: 'https://github.com/xxx/xxx.git' },
      { key: 'branch', label: '分支', type: 'text', placeholder: 'main' },
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '无需认证', value: 'none' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择 GitHub 凭据', type: 'credential_selector', credential_category: 'github', show_if: { key: 'auth_mode', value: 'credential' } }
    ],
    gitee: [
      { key: 'url', label: '仓库地址', type: 'text', placeholder: 'https://gitee.com/xxx/xxx.git' },
      { key: 'branch', label: '分支', type: 'text', placeholder: 'main' },
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '无需认证', value: 'none' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择 Gitee 凭据', type: 'credential_selector', credential_category: 'gitee', show_if: { key: 'auth_mode', value: 'credential' } }
    ],
    npm: [
      { key: 'command', label: '构建命令', type: 'text', placeholder: 'npm run build' },
      { key: 'workingDir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'cache', label: '启用缓存', type: 'boolean' }
    ],
    docker: [
      { key: 'image_name', label: '镜像名称', type: 'text', placeholder: 'myapp' },
      { key: 'image_tag', label: '镜像标签', type: 'text', placeholder: 'latest' },
      { key: 'dockerfile', label: 'Dockerfile 路径', type: 'text', placeholder: './Dockerfile', value: './Dockerfile' },
      { key: 'context', label: '构建上下文', type: 'text', placeholder: '.', value: '.' },
      { key: 'build_args', label: '构建参数', type: 'textarea', placeholder: 'JSON格式，如 {"KEY": "value"}' },
      { key: 'cache_from', label: '缓存镜像', type: 'text', placeholder: 'myapp:latest' },
      { key: 'cache_to', label: '缓存输出', type: 'text', placeholder: 'myapp:cache' },
      { key: 'push', label: '构建后推送', type: 'boolean', value: true },
      { key: 'registry', label: '仓库地址', type: 'text', placeholder: 'registry.example.com' },
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '无需认证', value: 'none' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择 Docker 凭据', type: 'credential_selector', credential_category: 'docker', show_if: { key: 'auth_mode', value: 'credential' } },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 60, value: 600 }
    ],
    unit: [
      { key: 'command', label: '测试命令', type: 'text', placeholder: 'npm run test:unit' },
      { key: 'coverage', label: '生成覆盖率报告', type: 'boolean' }
    ],
    ssh: [
      { key: 'host', label: '主机地址', type: 'text', placeholder: '192.168.1.1' },
      { key: 'port', label: '端口', type: 'number', placeholder: '22' },
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '密码认证', value: 'password' }, { label: '使用 SSH 凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择 SSH 凭据', type: 'credential_selector', credential_type: 'SSH_KEY', show_if: { key: 'auth_mode', value: 'credential' } },
      { key: 'script', label: '部署脚本', type: 'textarea', placeholder: '部署命令' }
    ],
    kubernetes: [
      { key: 'cluster', label: '集群名称', type: 'text', placeholder: 'default' },
      { key: 'namespace', label: '命名空间', type: 'text', placeholder: 'default' },
      { key: 'deployment', label: 'Deployment', type: 'text' },
      { key: 'image', label: '镜像', type: 'text' },
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '默认配置', value: 'default' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择 K8s 凭据', type: 'credential_selector', credential_category: 'kubernetes', show_if: { key: 'auth_mode', value: 'credential' } }
    ],
    dingtalk: [
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '直接配置', value: 'direct' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择钉钉凭据', type: 'credential_selector', credential_category: 'dingtalk', show_if: { key: 'auth_mode', value: 'credential' } },
      { key: 'webhook', label: 'Webhook 地址', type: 'text', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=xxx', show_if: { key: 'auth_mode', value: 'direct' } },
      { key: 'atMobiles', label: '@手机号', type: 'text', placeholder: '138xxxx' },
      { key: 'atAll', label: '@全体成员', type: 'boolean' }
    ],
    wechat: [
      { key: 'auth_mode', label: '认证模式', type: 'select', options: [{ label: '直接配置', value: 'direct' }, { label: '使用凭据', value: 'credential' }] },
      { key: 'credential_id', label: '选择企业微信凭据', type: 'credential_selector', credential_category: 'wechat', show_if: { key: 'auth_mode', value: 'credential' } }
    ],
    shell: [
      { key: 'script', label: '脚本内容', type: 'textarea', placeholder: 'echo "hello"' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'env', label: '环境变量', type: 'textarea', placeholder: 'JSON格式，如 {"KEY": "value"}' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    agent: [
      { key: 'agentId', label: '选择 Agent', type: 'select', placeholder: '选择执行 agent' },
      { key: 'taskType', label: '任务类型', type: 'select', options: [
        { label: 'Shell 脚本', value: 'shell' },
        { label: 'Python 脚本', value: 'python' },
        { label: 'Node.js 脚本', value: 'node' },
        { label: 'Docker 命令', value: 'docker' }
      ]},
      { key: 'script', label: '执行脚本', type: 'textarea', placeholder: '要执行的脚本内容' },
      { key: 'workingDir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'envVars', label: '环境变量', type: 'text', placeholder: 'JSON 格式，如 {"KEY": "value"}' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400 }
    ],
    custom: [
      { key: 'script', label: '脚本内容', type: 'textarea', placeholder: 'echo "hello"' },
      { key: 'working_dir', label: '工作目录', type: 'text', placeholder: './' },
      { key: 'env', label: '环境变量', type: 'textarea', placeholder: 'JSON格式，如 {"KEY": "value"}' },
      { key: 'timeout', label: '超时时间(秒)', type: 'number', min: 1, max: 86400, value: 3600 }
    ],
    condition: [
      { key: 'expression', label: '条件表达式', type: 'text', placeholder: '${VAR} == "value"' }
    ],
    sleep: [
      { key: 'seconds', label: '等待秒数', type: 'number', min: 1, max: 3600, placeholder: '60' }
    ],
    email: [
      { key: 'to', label: '收件人', type: 'textarea', placeholder: '用逗号分隔多个邮箱，如: dev@example.com, test@example.com' },
      { key: 'cc', label: '抄送', type: 'textarea', placeholder: '用逗号分隔多个邮箱' },
      { key: 'subject', label: '邮件主题', type: 'text', placeholder: '构建完成通知' },
      { key: 'body', label: '邮件正文', type: 'textarea', placeholder: '支持 HTML 格式' },
      { key: 'body_type', label: '正文类型', type: 'select', options: [
        { label: '纯文本', value: 'text' },
        { label: 'HTML', value: 'html' }
      ], value: 'text' }
    ]
  }
  return paramDefs[type] || []
}

// 辅助函数：获取连接点的水平偏移位置
// 当多个连接指向同一个节点时，将连接点均匀分布在节点边缘
const getConnectionOffset = (conn, nodeId, isTarget) => {
  const node = nodes.value.find(n => n.id === nodeId)
  if (!node) return { x: 100, index: 0, total: 1 }

  // 获取所有连接到该节点的连接（入站或出站）
  const connectionsList = isTarget
    ? connections.value.filter(c => c.to === nodeId)
    : connections.value.filter(c => c.from === nodeId)

  // 按另一节点的X位置排序，确保从左到右的顺序一致
  connectionsList.sort((a, b) => {
    const otherNodeA = nodes.value.find(n => n.id === (isTarget ? a.from : a.to))
    const otherNodeB = nodes.value.find(n => n.id === (isTarget ? b.from : b.to))
    return (otherNodeA?.x || 0) - (otherNodeB?.x || 0)
  })

  const index = connectionsList.findIndex(c => c.id === conn.id)
  const total = connectionsList.length
  const sectionWidth = node.width / (total + 1)

  return {
    x: sectionWidth * (index + 1),  // 在节点坐标系中的位置
    index: index,                    // 在所有连接中的索引（从0开始）
    total: total                     // 连接总数
  }
}

// 获取连接路径 - 优化后的贝塞尔曲线，支持多连接的水平偏移
// 连接线从源节点的右侧输出端口连接到目标节点的左侧输入端口
const getConnectionPath = (conn) => {
  const fromNode = nodes.value.find(n => n.id === conn.from)
  const toNode = nodes.value.find(n => n.id === conn.to)
  if (!fromNode || !toNode) return ''

  const nodeHeight = 100 // 节点高度估算
  const portY = 60 // 端口在节点内的 Y 位置（与 CSS 中的 top: 60px 对应）
  const portOffsetX = 10 // 端口向外延伸的距离

  // 获取源节点（出站连接）的偏移信息
  const fromOffset = getConnectionOffset(conn, fromNode.id, false)
  // 输出端口在节点右侧
  const fromX = fromNode.x + fromOffset.x + portOffsetX
  const fromY = fromNode.y + portY

  // 获取目标节点（入站连接）的偏移信息
  const toOffset = getConnectionOffset(conn, toNode.id, true)
  // 输入端口在节点左侧
  const toX = toNode.x + toOffset.x - portOffsetX
  const toY = toNode.y + portY

  // 计算水平距离
  const dx = toX - fromX
  const absDx = Math.abs(dx)

  // 根据节点相对位置选择不同的曲线类型
  let path = ''

  if (absDx < 50) {
    // 节点非常接近，使用简单的曲线
    const controlOffset = 30
    path = `M ${fromX} ${fromY} C ${fromX + controlOffset} ${fromY}, ${toX - controlOffset} ${toY}, ${toX} ${toY}`
  } else if (absDx < 300) {
    // 水平距离适中，使用标准贝塞尔曲线
    const controlOffset = absDx / 2
    path = `M ${fromX} ${fromY} C ${fromX + controlOffset} ${fromY}, ${toX - controlOffset} ${toY}, ${toX} ${toY}`
  } else {
    // 水平距离较大，使用更平滑的曲线
    const controlOffset = Math.min(absDx / 2, 150)
    path = `M ${fromX} ${fromY} C ${fromX + controlOffset} ${fromY}, ${toX - controlOffset} ${toY}, ${toX} ${toY}`
  }

  return path
}

// 缩放控制
const zoomIn = () => {
  canvasScale.value = Math.min(canvasScale.value + 0.1, 2)
}

const zoomOut = () => {
  canvasScale.value = Math.max(canvasScale.value - 0.1, 0.3)
}

const fitToScreen = () => {
  canvasScale.value = 1
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
    // 转换公式: nodePosition = (mouseInWrapper - canvasOffset) / scale
    const mouseInWrapperX = event.clientX - canvasRect.left
    const mouseInWrapperY = event.clientY - canvasRect.top
    
    // 考虑拖拽偏移量（鼠标点击位置相对于组件中心的偏移）
    let finalX = (mouseInWrapperX - canvasOffset.x - (component.dragOffset?.x || 0)) / canvasScale.value
    let finalY = (mouseInWrapperY - canvasOffset.y - (component.dragOffset?.y || 0)) / canvasScale.value
    
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
let mouseMoveHandler = null
let mouseUpHandler = null

const handleCanvasMouseDown = (event) => {
  if (event.button === 1 || (event.button === 0 && event.altKey)) {
    // 中键或 Alt+左键 拖动画布
    isPanning.value = true
    panStart.x = event.clientX - canvasOffset.x
    panStart.y = event.clientY - canvasOffset.y
  } else if (event.button === 0 && !event.target.closest('.pipeline-node')) {
    // 点击空白处取消选择
    selectedNode.value = null
    selectedConnection.value = null
  }
}

const handleCanvasMouseMove = (event) => {
  // 拖拽节点
  if (nodeDragging.value) {
    const scale = canvasScale.value
    const canvasRect = canvasWrapper.value.getBoundingClientRect()
    
    // 计算鼠标在canvas-wrapper中的位置
    const mouseInWrapperX = event.clientX - canvasRect.left
    const mouseInWrapperY = event.clientY - canvasRect.top
    
    // 计算节点左上角在nodes-layer坐标系中的位置
    // 节点屏幕位置 = mouseInWrapper - 节点相对于wrapper的偏移
    // 节点相对于wrapper的偏移 = mouseInWrapper - nodeDragOffset
    // 节点在nodes-layer中的位置 = (mouseInWrapper - nodeDragOffset - canvasOffset) / scale
    let newX = (mouseInWrapperX - nodeDragOffset.x - canvasOffset.x) / scale
    let newY = (mouseInWrapperY - nodeDragOffset.y - canvasOffset.y) / scale
    
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

// 鼠标滚轮缩放
const handleWheel = (event) => {
  event.preventDefault()
  const delta = event.deltaY > 0 ? -0.1 : 0.1
  canvasScale.value = Math.max(0.3, Math.min(2, canvasScale.value + delta))
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
  }
}

const redo = () => {
  if (historyIndex.value < history.value.length - 1) {
    historyIndex.value++
    const state = history.value[historyIndex.value]
    nodes.value = JSON.parse(JSON.stringify(state.nodes))
    connections.value = JSON.parse(JSON.stringify(state.connections))
  }
}

const canUndo = computed(() => historyIndex.value > 0)
const canRedo = computed(() => historyIndex.value < history.value.length - 1)

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

// 添加前置任务并创建连线
const addPredecessor = () => {
  if (!selectedNode.value) return
  if (!selectedNode.value.predecessors) {
    selectedNode.value.predecessors = []
  }
  selectedNode.value.predecessors.push('')
}

// 移除前置任务并删除连线
const removePredecessor = (idx) => {
  if (!selectedNode.value?.predecessors) return
  
  const removedId = selectedNode.value.predecessors[idx]
  
  // 移除对应的连接
  if (removedId) {
    const connIndex = connections.value.findIndex(
      c => c.from === removedId && c.to === selectedNode.value.id
    )
    if (connIndex > -1) {
      connections.value.splice(connIndex, 1)
    }
    
    // 更新源节点输出端口的连接状态
    const fromNode = nodes.value.find(n => n.id === removedId)
    if (fromNode) {
      fromNode.outputs.forEach(o => {
        o.connected = connections.value.some(c => c.from === fromNode.id)
      })
    }
    
    // 更新目标节点输入端口的连接状态
    selectedNode.value.inputs.forEach(i => {
      i.connected = connections.value.some(c => c.to === selectedNode.value.id)
    })
  }
  
  selectedNode.value.predecessors.splice(idx, 1)
  saveHistory()
}

// 创建从前置任务到当前节点的连线
const createConnectionFromPredecessor = (predId) => {
  console.log('createConnectionFromPredecessor called with predId:', predId)
  if (!predId || !selectedNode.value) {
    console.log('Early return: predId or selectedNode.value is null')
    return
  }
  
  // 检查是否已存在连接
  const exists = connections.value.find(
    c => c.from === predId && c.to === selectedNode.value.id
  )
  console.log('Connection exists:', exists)
  
  if (!exists) {
    const newConn = {
      id: `conn_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
      from: predId,
      to: selectedNode.value.id
    }
    console.log('Creating new connection:', newConn)
    connections.value.push(newConn)
    console.log('connections.value after push:', connections.value)
    
    // 更新源节点输出端口的连接状态
    const fromNode = nodes.value.find(n => n.id === predId)
    if (fromNode) {
      console.log('Found fromNode:', fromNode.name)
      fromNode.outputs.forEach(o => {
        o.connected = connections.value.some(c => c.from === fromNode.id)
      })
    }
    
    // 更新目标节点输入端口的连接状态
    selectedNode.value.inputs.forEach(i => {
      i.connected = connections.value.some(c => c.to === selectedNode.value.id)
    })
  }
}

// 监听前置任务变化，自动创建/删除连接
// 监听predecessors数组的深度变化
watch(
  () => selectedNode.value?.predecessors,
  (newPredecessors, oldPredecessors) => {
    if (!selectedNode.value || !newPredecessors) return
    
    // 过滤掉空字符串，只处理有效的节点ID
    const validNewPreds = newPredecessors.filter(id => id && id !== '')
    const validOldPreds = (oldPredecessors || []).filter(id => id && id !== '')
    
    // 获取旧的前置任务集合（只包含有效ID）
    const oldSet = new Set(validOldPreds)
    const newSet = new Set(validNewPreds)
    
    // 找出新增的前置任务，为每个新添加的前置任务创建连接
    validNewPreds.forEach(predId => {
      if (!oldSet.has(predId)) {
        console.log('Creating connection for new predecessor:', predId)
        createConnectionFromPredecessor(predId)
      }
    })
    
    // 找出移除的前置任务，删除对应的连接
    validOldPreds.forEach(predId => {
      if (!newSet.has(predId)) {
        console.log('Removing connection for predecessor:', predId)
        removeConnectionForPredecessor(predId)
      }
    })
  },
  { deep: true }
)

// 移除指定前置任务的连接
const removeConnectionForPredecessor = (predId) => {
  console.log('removeConnectionForPredecessor called with predId:', predId)
  if (!predId || !selectedNode.value) {
    console.log('Early return: predId or selectedNode is null')
    return
  }
  
  // 移除对应的连接
  const connIndex = connections.value.findIndex(
    c => c.from === predId && c.to === selectedNode.value.id
  )
  console.log('Connection index to remove:', connIndex)
  if (connIndex > -1) {
    connections.value.splice(connIndex, 1)
    console.log('Connection removed, remaining:', connections.value.length)
  }
  
  // 更新源节点输出端口的连接状态
  const fromNode = nodes.value.find(n => n.id === predId)
  if (fromNode) {
    fromNode.outputs.forEach(o => {
      o.connected = connections.value.some(c => c.from === fromNode.id)
    })
  }
  
  // 更新目标节点输入端口的连接状态
  selectedNode.value.inputs.forEach(i => {
    i.connected = connections.value.some(c => c.to === selectedNode.value.id)
  })
}

// 处理前置任务选择变化 - 当用户通过下拉框选择前置任务时调用
// Vue 3中直接通过索引修改数组(arr[idx] = value)不会触发响应式更新
// 必须使用splice方法来确保watch能捕获变化
const handlePredecessorSelectChange = (newValue, idx) => {
  if (!selectedNode.value || !selectedNode.value.predecessors) return
  
  if (newValue) {
    console.log('Predecessor selected:', newValue, 'at index:', idx, 'node:', selectedNode.value.name)
    
    // 使用splice替换数组元素以触发Vue响应式更新
    selectedNode.value.predecessors.splice(idx, 1, newValue)
    
    // 手动创建连接（因为splice可能不会触发深度watch的立即响应）
    createConnectionFromPredecessor(newValue)
  }
}

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
    
    console.log('保存流水线配置:', JSON.stringify(pipelineData, null, 2))
    
    // 调用保存接口
    const response = await updatePipeline(pipelineId.value, {
      config: JSON.stringify(pipelineData)
    })
    
    if (response.code === 200) {
      ElMessage.success('流水线保存成功')
      saveHistory()
    } else {
      ElMessage.error(response.message || '保存失败')
    }
  } catch (error) {
    console.error('保存流水线失败:', error)
    ElMessage.error('保存失败，请稍后重试')
  }
}

// 保存配置
const saveNodeConfig = () => {
  ElMessage.success('配置已保存')
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
          
          // 更新端口连接状态
          updateAllPortsConnectionStatus()
          console.log('新格式流水线配置加载成功', { nodes: nodes.value.length, connections: connections.value.length })
        } else {
          // 旧格式兼容
          if (config.nodes && config.nodes.length > 0) {
            nodes.value = config.nodes
          }
          if (config.connections && config.connections.length > 0) {
            connections.value = config.connections
          } else {
            rebuildConnectionsFromPredecessors()
          }
          updateAllPortsConnectionStatus()
          console.log('旧格式流水线配置加载成功', { nodes: nodes.value.length, connections: connections.value.length })
        }
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
    shell: [{ label: '输入', key: 'input' }],
    docker: [{ label: '代码', key: 'code' }],
    email: [{ label: '消息', key: 'message' }],
    default: [{ label: '输入', key: 'input' }]
  }
  return inputsMap[type] || inputsMap.default
}

// 获取默认输出端口
const getDefaultOutputs = (type) => {
  const outputsMap = {
    git_clone: [{ label: '代码', key: 'code' }],
    shell: [{ label: '输出', key: 'output' }],
    docker: [{ label: '镜像', key: 'image' }],
    email: [],
    default: [{ label: '输出', key: 'output' }]
  }
  return outputsMap[type] || outputsMap.default
}

// 根据predecessors重建connections数组
const rebuildConnectionsFromPredecessors = () => {
  console.log('rebuildConnectionsFromPredecessors called')
  const newConnections = []
  
  nodes.value.forEach(node => {
    if (node.predecessors && Array.isArray(node.predecessors)) {
      node.predecessors.forEach(predId => {
        if (predId) {
          // 检查是否已存在该连接
          const exists = newConnections.some(c => c.from === predId && c.to === node.id)
          if (!exists) {
            newConnections.push({
              id: `conn_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
              from: predId,
              to: node.id
            })
            console.log(`创建连接: ${predId} -> ${node.id}`)
          }
        }
      })
    }
  })
  
  connections.value = newConnections
  console.log('重建连接线完成:', connections.value.length, '条连接')
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
  // 加载流水线配置
  await loadPipeline()
  
  // 初始化历史记录
  saveHistory()
  
  // 绑定键盘事件
  window.addEventListener('keydown', handleKeyDown)
  window.addEventListener('keyup', handleKeyUp)
  
  // 初始缩放
  fitToScreen()
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyDown)
  window.removeEventListener('keyup', handleKeyUp)
})

// 状态相关
const getStatusType = (status) => {
  const types = {
    success: 'success',
    running: 'warning',
    failed: 'danger',
    pending: 'info'
  }
  return types[status] || 'info'
}

const getStatusText = (status) => {
  const texts = {
    success: '成功',
    running: '运行中',
    failed: '失败',
    pending: '等待'
  }
  return texts[status] || status
}
</script>

<style lang="scss" scoped>
@import '@/assets/styles/variables.scss';

.pipeline-design-container {
  display: flex;
  height: calc(100vh - 120px);
  background: var(--bg-primary);
  overflow: hidden;
}

/* 左侧组件库面板 */
.components-panel {
  width: 260px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--border-color);
  transition: width 0.3s ease;
  overflow: hidden;
  flex-shrink: 0;
  box-shadow: 4px 0 24px rgba(0, 0, 0, 0.04);

  .panel-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 16px;
    color: var(--text-primary);
    font-size: 14px;
    font-weight: 600;
    border-bottom: 1px solid var(--border-color);
    cursor: pointer;
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
  .toolbar-center,
  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .zoom-text {
    min-width: 60px;
    text-align: center;
    color: var(--text-secondary);
    font-weight: 500;
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
  background: var(--bg-primary);

  &:active {
    cursor: grabbing;
  }

  .grid-background {
    position: absolute;
    top: -5000px;
    left: -5000px;
    width: 10000px;
    height: 10000px;
    background-color: var(--bg-primary);
    background-image:
      radial-gradient(circle, var(--border-color-medium) 1px, transparent 1px);
    background-size: 20px 20px;
    pointer-events: none;
  }

  .connections-layer-wrapper {
    position: absolute;
    top: 0;
    left: 0;
    width: 0;
    height: 0;
    z-index: 10;
    pointer-events: none;

    .connections-layer {
      position: absolute;
      top: 0;
      left: 0;
      overflow: visible;

      .connection-line {
        cursor: pointer;
        transition: stroke-width 0.2s;
        opacity: 0.9;

        &:hover {
          stroke-width: 4;
        }

        &.selected {
          stroke: var(--danger-color);
          stroke-width: 4;
        }
      }

      .connecting-line-temp {
        pointer-events: none;
        opacity: 0.7;
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
  min-width: 200px;
  background: var(--bg-card);
  border: 1px solid var(--border-color);
  border-radius: $radius-lg;
  cursor: move;
  transition: box-shadow $transition-base, border-color $transition-base, transform $transition-base;
  overflow: visible;
  box-shadow: var(--shadow-md);

  &:hover {
    border-color: var(--primary-color);
    box-shadow: var(--shadow-lg);
    transform: translateY(-2px);
  }

  &.selected {
    border-color: var(--primary-color);
    box-shadow:
      0 0 0 2px var(--primary-light),
      var(--shadow-lg);
  }

  &.connecting {
    cursor: crosshair;
  }

  .node-port {
    position: absolute;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 24px;
    padding: 0 8px;
    cursor: pointer;
    transition: all 0.2s;

    .port-dot {
      width: 12px;
      height: 12px;
      border-radius: 50%;
      background: var(--primary-color);
      border: 2px solid var(--bg-primary);
      transition: all 0.2s;
      box-shadow: 0 0 0 2px var(--border-color);
    }

    .port-label {
      font-size: 11px;
      color: var(--text-secondary);
      white-space: nowrap;
      font-weight: 500;
    }

    &:hover {
      .port-dot {
        transform: scale(1.3);
        box-shadow: 0 0 12px var(--primary-color);
      }
    }

    &.input-port {
      left: -12px;
      top: 60px;
      transform: translateX(-100%);

      .port-dot {
        background: var(--primary-color);
      }
    }

    &.output-port {
      right: -12px;
      top: 60px;

      .port-dot {
        background: var(--success-color);
      }

      &:hover .port-dot {
        background: var(--success-color);
        box-shadow: 0 0 12px var(--success-color);
      }
    }

    &.connected .port-dot {
      background: var(--success-color);
      box-shadow: 0 0 8px var(--success-color);
    }
  }

  .node-content {
    padding: 16px;

    .node-header {
      display: flex;
      align-items: center;
      gap: 12px;

      .node-order-badge {
        width: 28px;
        height: 28px;
        display: flex;
        align-items: center;
        justify-content: center;
        background: linear-gradient(135deg, var(--primary-color) 0%, var(--primary-hover) 100%);
        color: white;
        font-size: 12px;
        font-weight: 700;
        border-radius: 50%;
        flex-shrink: 0;
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
      }

      .node-icon {
        width: 40px;
        height: 40px;
        display: flex;
        align-items: center;
        justify-content: center;
        border-radius: $radius-md;
        color: white;
        font-size: 20px;
        flex-shrink: 0;
        box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
      }

      .node-info {
        flex: 1;
        min-width: 0;

        .node-name {
          display: block;
          color: var(--text-primary);
          font-size: 14px;
          font-weight: 600;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }

        .node-type {
          display: block;
          color: var(--text-muted);
          font-size: 12px;
          margin-top: 2px;
        }
      }

      .node-actions {
        opacity: 0;
        transition: opacity 0.2s;

        .delete-btn {
          color: var(--danger-color);
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
    }

    .node-status {
      margin-top: 12px;

      :deep(.el-tag) {
        border-radius: $radius-full;
        padding: 4px 12px;
        font-weight: 500;
        border: none;
      }
    }
  }

  &:hover .node-actions {
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
