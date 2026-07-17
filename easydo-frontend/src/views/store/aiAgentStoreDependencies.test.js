import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildPinnedResourceRefs,
  createPinnedResourceRefRow,
  isPinnedResourceRef,
  mergeResourceToolPermissionsIntoRefRow,
  profileDependencyHealth,
  profileResourceHealthIssues,
  toolPermissionConfigFromRows,
  toolPermissionRowsForResource,
  statusTransitionOperation
} from './aiAgentStoreDependencies.js'

test('status transitions map disable and archive to backend dependency operations', () => {
  assert.equal(statusTransitionOperation('active', 'disabled'), 'disable')
  assert.equal(statusTransitionOperation('disabled', 'disabled'), '')
  assert.equal(statusTransitionOperation('active', 'archived'), 'archive')
  assert.equal(statusTransitionOperation('archived', 'active'), '')
})

test('profile resource refs must include canonical version pins before save', () => {
  assert.throws(() => buildPinnedResourceRefs([
    createPinnedResourceRefRow({
      resource_type: 'skill',
      resource_id: 42,
      resource_version: '1.0.0',
      snapshot_digest: 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
      configJSON: '{}'
    }, 'skill')
  ], {
    label: 'Skills',
    fallbackResourceType: 'skill',
    parseConfig: JSON.parse
  }), /必须选择固定版本/)
})

test('profile resource refs preserve exact id version and digest pins', () => {
  const refs = buildPinnedResourceRefs([
    createPinnedResourceRefRow({
      resource_type: 'mcp_server',
      resource_id: 7,
      resource_version_id: 81,
      resource_version: '2026.07.13',
      snapshot_digest: 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
      required: true,
      configJSON: '{"policy":"ask"}'
    }, 'mcp_server')
  ], {
    label: 'MCP Servers',
    fallbackResourceType: 'mcp_server',
    parseConfig: JSON.parse
  })

  assert.deepEqual(refs, [{
    resource_type: 'mcp_server',
    resource_id: 7,
    resource_version_id: 81,
    resource_version: '2026.07.13',
    snapshot_digest: 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    config: { policy: 'ask' },
    required: true
  }])
  assert.equal(isPinnedResourceRef(refs[0]), true)
})

test('profile resource refs preserve per-tool permission config', () => {
  const rows = toolPermissionRowsForResource({
    spec: {
      discovered_tools: [
        { name: 'safe_read', operation_type: 'read' },
        { name: 'danger_write', operation_type: 'write' }
      ]
    }
  }, {
    tool_permissions: {
      tools: {
        safe_read: { decision: 'allow', operation_type: 'read' },
        danger_write: { decision: 'deny', operation_type: 'write' }
      }
    }
  })
  const refRow = createPinnedResourceRefRow({
    resource_type: 'mcp_server',
    resource_id: 7,
    resource_version_id: 81,
    resource_version: '2026.07.13',
    snapshot_digest: 'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    config: toolPermissionConfigFromRows(rows)
  }, 'mcp_server')

  const refs = buildPinnedResourceRefs([refRow], {
    label: 'MCP Servers',
    fallbackResourceType: 'mcp_server',
    parseConfig: JSON.parse
  })

  assert.deepEqual(refs[0].config.tool_permissions.tools, {
    safe_read: { decision: 'allow', operation_type: 'read' },
    danger_write: { decision: 'deny', operation_type: 'write' }
  })
})

test('profile resource ref rows default-copy resource tool permissions only when the ref is unset', () => {
  const resource = {
    spec: {
      tool_permissions: {
        tools: {
          safe_read: { decision: 'allow', operation_type: 'read' }
        }
      }
    }
  }
  const emptyRef = createPinnedResourceRefRow({ resource_type: 'mcp_server', resource_id: 7 }, 'mcp_server')
  mergeResourceToolPermissionsIntoRefRow(emptyRef, resource)
  assert.deepEqual(JSON.parse(emptyRef.configJSON).tool_permissions.tools.safe_read, {
    decision: 'allow',
    operation_type: 'read'
  })

  const overrideRef = createPinnedResourceRefRow({
    resource_type: 'mcp_server',
    resource_id: 7,
    config: { tool_permissions: { tools: { safe_read: 'deny' } } }
  }, 'mcp_server')
  mergeResourceToolPermissionsIntoRefRow(overrideRef, resource)
  assert.equal(JSON.parse(overrideRef.configJSON).tool_permissions.tools.safe_read, 'deny')
})

test('profile dependency health reports invalid refs without canonical pins', () => {
  const profile = {
    id: 9,
    name: 'Broken Agent',
    skills: [{ resource_type: 'skill', resource_id: 42, resource_version: 'v1' }],
    mcp_servers: [],
    subagents: []
  }

  const issues = profileResourceHealthIssues(profile, {
    resources: { skills: [{ id: 42, name: 'Review Skill', status: 'active' }] },
    profiles: []
  })

  assert.equal(issues[0]?.severity, 'invalid')
  assert.match(issues[0]?.message || '', /缺少固定版本/)
  assert.deepEqual(profileDependencyHealth(profile, { issues }), {
    label: '配置失效',
    type: 'danger'
  })
})

test('profile dependency health reports update candidates from loaded version cache', () => {
  const digestV1 = `sha256:${'a'.repeat(64)}`
  const digestV2 = `sha256:${'b'.repeat(64)}`
  const profile = {
    id: 10,
    name: 'Updatable Agent',
    skills: [{
      resource_type: 'skill',
      resource_id: 42,
      resource_version_id: 1,
      resource_version: 'v1',
      snapshot_digest: digestV1
    }],
    mcp_servers: [],
    subagents: []
  }

  const issues = profileResourceHealthIssues(profile, {
    resources: { skills: [{ id: 42, name: 'Review Skill', status: 'active' }] },
    profiles: [],
    versionCache: {
      'skills:42': {
        versions: [
          { resource_version_id: 2, source_version: 'v2', revision: 2, snapshot_digest: digestV2 },
          { resource_version_id: 1, source_version: 'v1', revision: 1, snapshot_digest: digestV1 }
        ]
      }
    }
  })

  assert.equal(issues[0]?.severity, 'update_available')
  assert.match(issues[0]?.message || '', /可更新/)
  assert.deepEqual(profileDependencyHealth(profile, { issues }), {
    label: '可更新',
    type: 'warning'
  })
})

test('profile dependency health rejects a pinned version missing from an authoritative empty cache', () => {
  const profile = {
    id: 11,
    name: 'Missing Version Agent',
    skills: [{
      resource_type: 'skill',
      resource_id: 42,
      resource_version_id: 9,
      resource_version: 'v9',
      snapshot_digest: `sha256:${'c'.repeat(64)}`
    }],
    mcp_servers: [],
    subagents: []
  }

  const issues = profileResourceHealthIssues(profile, {
    resources: { skills: [{ id: 42, name: 'Review Skill', status: 'active' }] },
    profiles: [],
    versionCache: {
      'skills:42': { loaded: true, versions: [] }
    }
  })

  assert.equal(issues[0]?.severity, 'invalid')
  assert.match(issues[0]?.message || '', /版本不可用/)
})

test('profile dependency health rejects save when version verification failed', () => {
  const profile = {
    id: 12,
    name: 'Unverified Version Agent',
    skills: [],
    mcp_servers: [{
      resource_type: 'mcp_server',
      resource_id: 7,
      resource_version_id: 81,
      resource_version: '2026.07.13',
      snapshot_digest: `sha256:${'d'.repeat(64)}`
    }],
    subagents: []
  }

  const issues = profileResourceHealthIssues(profile, {
    resources: { mcp_servers: [{ id: 7, name: 'Write MCP', status: 'active' }] },
    profiles: [],
    versionCache: {
      'mcp_servers:7': { loaded: false, error: '版本服务不可用', versions: [] }
    }
  })

  assert.equal(issues[0]?.severity, 'invalid')
  assert.match(issues[0]?.message || '', /版本校验失败/)
})
