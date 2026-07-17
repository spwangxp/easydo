import type { AIRuntimeWorkspace } from './types.js'

export interface AgentWorkspaceProvider {
  provision(workspace: AIRuntimeWorkspace): Promise<{ sandbox_id: string }>
  connect(workspace: AIRuntimeWorkspace): Promise<void>
  pause(workspace: AIRuntimeWorkspace): Promise<void>
  resume(workspace: AIRuntimeWorkspace): Promise<void>
  recycle(workspace: AIRuntimeWorkspace): Promise<void>
}
