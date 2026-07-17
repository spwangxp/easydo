import { describe, expect, it } from 'vitest'
import { ok, type ExecutionEnv } from '@earendil-works/pi-agent-core'
import { createWorkspaceTools, filterWorkspaceToolsByPolicy } from './piWorkspaceTools.js'
import { PiToolApprovalRequiredError } from './piResources.js'

describe('createWorkspaceTools', () => {
  it('exposes the complete sandbox coding tool set', () => {
    const tools = createWorkspaceTools({ cwd: '/workspace' } as ExecutionEnv)
    expect(tools.map((tool) => tool.name)).toEqual([
      'bash',
      'read_file',
      'write_file',
      'edit_file',
      'list_directory',
      'file_info'
    ])
  })

  it('exposes only explicitly read-classified tools in read_only mode', () => {
    const tools = createWorkspaceTools({ cwd: '/workspace' } as ExecutionEnv, 'read_only')

    expect(tools.map((tool) => tool.name)).toEqual(['read_file', 'list_directory', 'file_info'])
    expect(tools.map((tool) => tool.operation_type)).toEqual(['read', 'read', 'read'])
  })

  it('classifies the complete write-mode tool catalog with explicit operation metadata', () => {
    const tools = createWorkspaceTools({ cwd: '/workspace' } as ExecutionEnv, 'write')

    expect(tools.map((tool) => [tool.name, tool.operation_type])).toEqual([
      ['bash', 'execute'],
      ['read_file', 'read'],
      ['write_file', 'write'],
      ['edit_file', 'write'],
      ['list_directory', 'read'],
      ['file_info', 'read']
    ])
  })

  it('executes shell and file writes through the supplied workspace environment', async () => {
    const calls: Array<{ method: string, args: unknown[] }> = []
    const env = {
      cwd: '/workspace',
      async exec(command: string, options: unknown) {
        calls.push({ method: 'exec', args: [command, options] })
        return ok({ stdout: 'ok\n', stderr: '', exitCode: 0 })
      },
      async writeFile(path: string, content: string) {
        calls.push({ method: 'writeFile', args: [path, content] })
        return ok(undefined)
      }
    } as unknown as ExecutionEnv
    const tools = createWorkspaceTools(env, 'write', {
      toolPolicy: {
        tools: {
          bash: 'allow',
          write_file: 'allow'
        }
      }
    })

    await tools.find((tool) => tool.name === 'bash')!.execute('call-1', { command: 'pwd', cwd: 'src' } as never)
    await tools.find((tool) => tool.name === 'write_file')!.execute('call-2', { path: 'demo.txt', content: 'hello' } as never)

    expect(calls).toEqual([
      { method: 'exec', args: ['pwd', { cwd: 'src', timeout: undefined }] },
      { method: 'writeFile', args: ['demo.txt', 'hello'] }
    ])
  })

  it('filters workspace tools by tool_policy.workspace_tools when configured', () => {
    const tools = createWorkspaceTools({ cwd: '/workspace' } as ExecutionEnv, 'write')
    expect(filterWorkspaceToolsByPolicy(tools, {}).map((tool) => tool.name)).toEqual(tools.map((tool) => tool.name))
    expect(filterWorkspaceToolsByPolicy(tools, { workspace_tools: ['read_file', 'write_file', 'bash'] }).map((tool) => tool.name)).toEqual([
      'bash',
      'read_file',
      'write_file'
    ])
  })

  it('filters out workspace tools with explicit deny decisions', () => {
    const tools = createWorkspaceTools({ cwd: '/workspace' } as ExecutionEnv, 'write')
    expect(filterWorkspaceToolsByPolicy(tools, {
      workspace_tools: ['bash', 'read_file', 'list_directory'],
      workspace_tool_decisions: {
        bash: 'deny',
        read_file: 'deny',
        list_directory: 'allow'
      }
    }).map((tool) => tool.name)).toEqual(['list_directory'])
  })

  it('denies workspace tool execution when tool_policy marks the tool as deny', async () => {
    const events: Array<{ type: string, decision?: string }> = []
    let executed = false
    const env = {
      cwd: '/workspace',
      async exec() {
        executed = true
        return ok({ stdout: 'should not run', stderr: '', exitCode: 0 })
      }
    } as unknown as ExecutionEnv
    const tools = createWorkspaceTools(env, 'write', {
      toolPolicy: {
        tools: { bash: 'deny' },
        workspace_tool_decisions: { bash: 'deny' }
      },
      recordEvent: async (event) => {
        events.push(event as never)
      }
    })

    await expect(tools.find((tool) => tool.name === 'bash')!.execute('call-deny-1', { command: 'pwd' } as never))
      .rejects.toThrow(/deny/i)

    expect(executed).toBe(false)
    expect(events.map((event) => event.type)).toEqual(['action.permission_evaluated', 'permission.resolved'])
    expect(events[0]).toMatchObject({ decision: 'deny', tool_name: 'bash' })
  })

  it('requires approval for workspace tools with request decision', async () => {
    const events: Array<{ type: string }> = []
    let executed = false
    const env = {
      cwd: '/workspace',
      async readTextFile() {
        executed = true
        return ok('secret')
      }
    } as unknown as ExecutionEnv
    const tools = createWorkspaceTools(env, 'write', {
      toolPolicy: {
        tools: { read_file: 'request' },
        workspace_tool_decisions: { read_file: 'request' }
      },
      recordEvent: async (event) => {
        events.push(event as never)
      }
    })

    await expect(tools.find((tool) => tool.name === 'read_file')!.execute('call-ask-1', { path: 'daemon.json' } as never))
      .rejects.toBeInstanceOf(PiToolApprovalRequiredError)

    expect(executed).toBe(false)
    expect(events.map((event) => event.type)).toEqual(['action.permission_evaluated', 'permission.asked'])
  })
})
