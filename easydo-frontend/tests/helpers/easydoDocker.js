import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

export async function composeServiceContainers(service) {
  const args = [
    'ps',
    '--filter', `label=com.docker.compose.service=${service}`,
    '--format', '{{.Names}}'
  ]
  const { stdout } = await execFileAsync('docker', args)
  return stdout.trim().split('\n').map((line) => line.trim()).filter(Boolean).sort()
}

export async function composeServiceContainer(service) {
  const containers = await composeServiceContainers(service)
  if (containers.length === 0) throw new Error(`Compose service container not found: ${service}`)
  return containers[0]
}

export async function containerHostname(container) {
  const { stdout } = await execFileAsync('docker', ['inspect', '--format', '{{.Config.Hostname}}', container])
  return stdout.trim()
}

export async function containerHealth(container) {
  const { stdout } = await execFileAsync('docker', ['inspect', '--format', '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}', container])
  return stdout.trim()
}

export async function waitForContainerReady(container, timeoutMs = 30000) {
  const started = Date.now()
  let lastStatus = ''
  while (Date.now() - started < timeoutMs) {
    lastStatus = await containerHealth(container).catch((error) => error instanceof Error ? error.message : String(error))
    if (lastStatus === 'healthy' || lastStatus === 'running') return true
    await sleep(500)
  }
  throw new Error(`Container ${container} did not become ready within ${timeoutMs}ms; last status: ${lastStatus}`)
}

export async function restartContainer(container, timeoutMs = 30000) {
  await execFileAsync('docker', ['restart', container])
  await waitForContainerReady(container, timeoutMs)
}

export async function stopContainer(container) {
  await execFileAsync('docker', ['stop', container])
}

export async function startContainer(container, timeoutMs = 30000) {
  await execFileAsync('docker', ['start', container])
  await waitForContainerReady(container, timeoutMs)
}

export async function queryMariaDB(sql) {
  const dbContainer = await composeServiceContainer('db')
  const { stdout } = await execFileAsync('docker', [
    'exec', dbContainer, 'mariadb', '-uroot', '-ppassword', 'easydo', '-N', '-e', sql
  ])
  return stdout.trim()
}

export async function runtimeOwnerID(runtimeRunID) {
  return queryMariaDB(`SELECT owner_instance_id FROM ai_runtime_runs WHERE runtime_run_id=${sqlString(runtimeRunID)}`)
}

export async function runtimeRunResult(runtimeRunID) {
  const raw = await queryMariaDB(`SELECT result_json FROM ai_runtime_runs WHERE runtime_run_id=${sqlString(runtimeRunID)}`)
  return JSON.parse(raw || '{}')
}

export async function runtimeContainerForInstance(instanceID) {
  const containers = await composeServiceContainers('ai-runtime')
  for (const container of containers) {
    if (await containerHostname(container) === instanceID) return container
  }
  throw new Error(`Runtime owner container not found for ${instanceID}`)
}

export async function runtimeContainerForRun(runtimeRunID) {
  return runtimeContainerForInstance(await runtimeOwnerID(runtimeRunID))
}

function sqlString(value) {
  return `'${String(value).replace(/\\/g, '\\\\').replace(/'/g, "''")}'`
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
