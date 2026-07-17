export const EASYDO_WORKSPACE_TOOLS = [
  {
    name: 'bash',
    operation_type: 'execute',
    risk: 'high',
    description: '在隔离 Agent 工作区内执行 Bash 命令（构建、测试、Git、脚本等）。'
  },
  {
    name: 'read_file',
    operation_type: 'read',
    risk: 'low',
    description: '读取工作区内的 UTF-8 文本文件。'
  },
  {
    name: 'write_file',
    operation_type: 'write',
    risk: 'high',
    description: '创建或覆盖工作区内的 UTF-8 文本文件。'
  },
  {
    name: 'edit_file',
    operation_type: 'write',
    risk: 'high',
    description: '在工作区文件中精确替换一段文本（仅允许唯一匹配）。'
  },
  {
    name: 'list_directory',
    operation_type: 'read',
    risk: 'low',
    description: '列出工作区目录下的直接子项。'
  },
  {
    name: 'file_info',
    operation_type: 'read',
    risk: 'low',
    description: '查看工作区路径的元数据（文件/目录/软链）。'
  }
]

export const EASYDO_WORKSPACE_TOOL_NAMES = EASYDO_WORKSPACE_TOOLS.map((tool) => tool.name)

export function normalizeWorkspaceTools(value) {
  if (!Array.isArray(value)) return [...EASYDO_WORKSPACE_TOOL_NAMES]
  const allowed = new Set(EASYDO_WORKSPACE_TOOL_NAMES)
  const selected = value
    .map((item) => String(item || '').trim())
    .filter((name) => allowed.has(name))
  return selected.length > 0 ? [...new Set(selected)] : [...EASYDO_WORKSPACE_TOOL_NAMES]
}

export const WORKSPACE_TOOL_PERMISSION_DECISIONS = ['allow', 'request', 'deny']

/**
 * Extract workspace tool decisions from a tool_policy object.
 * The runtime's evaluateToolPermission reads tool-specific decisions
 * from the policy's `tools` map, so we store workspace tool permissions there.
 */
export function extractWorkspaceToolDecisions(toolPolicy = {}) {
  const tools = asRecord(toolPolicy.tools)
  const decisions = {}
  for (const toolName of EASYDO_WORKSPACE_TOOL_NAMES) {
    const decision = tools[toolName]
    if (WORKSPACE_TOOL_PERMISSION_DECISIONS.includes(decision)) {
      decisions[toolName] = decision
    }
  }
  return decisions
}

export function mergeToolPolicyWithWorkspaceTools(toolPolicy = {}, workspaceTools = [], workspaceToolDecisions = {}) {
  const policy = toolPolicy && typeof toolPolicy === 'object' && !Array.isArray(toolPolicy)
    ? { ...toolPolicy }
    : {}
  policy.workspace_tools = normalizeWorkspaceTools(workspaceTools)
  // Merge workspace tool decisions into policy.tools — the key the
  // runtime's toolPermissionRules() reads for tool-specific permissions.
  // Preserve any existing tools entries from advanced JSON (e.g. wildcards).
  const tools = { ...asRecord(policy.tools) }
  if (workspaceToolDecisions && typeof workspaceToolDecisions === 'object') {
    for (const [toolName, decision] of Object.entries(workspaceToolDecisions)) {
      if (WORKSPACE_TOOL_PERMISSION_DECISIONS.includes(decision)) {
        tools[toolName] = decision
      }
    }
  }
  // Clean up: remove workspace tools where decision was removed (unset → 'request')
  for (const toolName of EASYDO_WORKSPACE_TOOL_NAMES) {
    if (!workspaceToolDecisions[toolName] && tools[toolName] === 'request') {
      delete tools[toolName]
    }
  }
  if (Object.keys(tools).length > 0) {
    policy.tools = tools
  } else {
    delete policy.tools
  }
  return policy
}

function asRecord(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

export function workspaceToolsFromToolPolicy(toolPolicy = {}) {
  const policy = toolPolicy && typeof toolPolicy === 'object' && !Array.isArray(toolPolicy)
    ? toolPolicy
    : {}
  return normalizeWorkspaceTools(policy.workspace_tools)
}

export function subagentModeFromConfig(config = {}) {
  const raw = config && typeof config === 'object' ? config.mode : ''
  return String(raw || '').toLowerCase().includes('write') ? 'write' : 'read_only'
}

export function mergeSubagentConfigWithMode(config = {}, mode = 'write') {
  const next = config && typeof config === 'object' && !Array.isArray(config) ? { ...config } : {}
  next.mode = mode === 'read_only' ? 'read_only' : 'write'
  return next
}
