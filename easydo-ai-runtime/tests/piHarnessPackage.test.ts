import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const packageJson = JSON.parse(readFileSync(resolve(process.cwd(), 'package.json'), 'utf8'))
const dockerfile = readFileSync(resolve(process.cwd(), 'Dockerfile'), 'utf8')
const workspaceDockerfile = readFileSync(resolve(process.cwd(), 'Dockerfile.workspace'), 'utf8')

describe('Pi harness runtime package contract', () => {
  it('declares Pi harness dependencies explicitly', () => {
    expect(packageJson.dependencies['@earendil-works/pi-agent-core']).toBeDefined()
    expect(packageJson.dependencies['@earendil-works/pi-ai']).toBeDefined()
  })

  it('uses Node 22 because Pi requires node >=22.19', () => {
    expect(packageJson.engines.node).toBe('>=22.19.0')
    expect(dockerfile).toContain('FROM node:22-alpine AS builder')
    expect(dockerfile).toContain('FROM node:22-alpine AS runner')
  })

  it('ships Docker control and a non-root workspace tool image', () => {
    expect(dockerfile).toContain('docker-cli')
    expect(workspaceDockerfile).toContain('FROM node:22-alpine')
    expect(workspaceDockerfile).toContain('USER 1000:1000')
    expect(workspaceDockerfile).toContain('WORKDIR /workspace')
    for (const tool of ['bash', 'git', 'ripgrep', 'curl', 'openssh-client']) {
      expect(workspaceDockerfile).toContain(tool)
    }
  })
})
