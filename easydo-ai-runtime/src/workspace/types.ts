export type AIRuntimeWorkspaceStatus = 'provisioning' | 'ready' | 'paused' | 'recycling' | 'recycled' | 'failed'

export interface AIRuntimeWorkspaceRepository {
  url?: string
  ref?: string
  commit?: string
  subdirectory?: string
}

export interface AIRuntimeWorkspaceNetworkPolicy {
  mode: 'none' | 'allowlist' | 'unrestricted'
  allow_hosts: string[]
}

export interface AIRuntimeWorkspaceQuota {
  cpu_cores: number
  memory_mb: number
  disk_mb: number
  pids: number
}

export interface AIRuntimeWorkspace {
  id: number
  workspace_runtime_id: string
  workspace_id: number
  owner_user_id: number
  workspace_key: string
  provider: 'docker'
  provider_endpoint?: string
  sandbox_id?: string
  status: AIRuntimeWorkspaceStatus
  image: string
  root_path: string
  repository: AIRuntimeWorkspaceRepository
  network_policy: AIRuntimeWorkspaceNetworkPolicy
  secret_refs: Array<Record<string, unknown>>
  quota: AIRuntimeWorkspaceQuota
  snapshot_hash: string
  lease_owner_instance_id?: string
  lease_epoch: number
  lease_expires_at?: string
  provision_owner_instance_id?: string
  provision_epoch: number
  provision_expires_at?: string
  state_version: number
  last_connected_at?: string
  paused_at?: string
  recycled_at?: string
  error_code?: string
  error_msg?: string
  created_at: string
  updated_at: string
}

export interface AIRuntimeWorkspaceAudit {
  audit_id: string
  workspace_runtime_id: string
  workspace_id: number
  owner_user_id: number
  actor_user_id: number
  operation: 'created' | 'connected' | 'paused' | 'resumed' | 'recycled' | 'lease_claimed' | 'lease_released' | 'failed'
  outcome: 'succeeded' | 'failed'
  details: Record<string, unknown>
  created_at: string
}
