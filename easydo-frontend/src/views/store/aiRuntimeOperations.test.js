import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  createRuntimeOperationsLoader,
  formatDurationMetric,
  normalizeRuntimeOperationsSummary,
  terminalRows
} from './aiRuntimeOperations.js'

const currentDir = dirname(fileURLToPath(import.meta.url))

test('normalizes missing operations fields without inventing health data', () => {
  assert.deepEqual(normalizeRuntimeOperationsSummary({
    observed_at: '2026-07-12T01:00:00.000Z',
    instance_id: 'runtime-a',
    runs: { active: 2, stale: 1 }
  }), {
    observed_at: '2026-07-12T01:00:00.000Z',
    replica: { instance_id: 'runtime-a', status: 'unknown' },
    runs: { active: 2, awaiting_approval: 0, awaiting_input: 0, stale: 1, orphaned: 0 },
    terminal_5m: { completed: 0, failed: 0, cancelled: 0, timeout: 0, interrupted: 0 },
    terminal_1h: { completed: 0, failed: 0, cancelled: 0, timeout: 0, interrupted: 0 },
    latency_5m: {},
    latency_1h: {},
    failures_1h: []
  })
})

test('formats latency summaries and terminal windows for compact tables', () => {
  assert.equal(formatDurationMetric({ count: 4, p95_ms: 1250 }), '1.25 s')
  assert.equal(formatDurationMetric({ count: 0, p95_ms: 0 }), '-')
  assert.equal(formatDurationMetric(undefined), '-')
  assert.deepEqual(terminalRows({ completed: 3, failed: 1, timeout: 2 }), [
    { key: 'completed', label: '完成', count: 3 },
    { key: 'failed', label: '失败', count: 1 },
    { key: 'cancelled', label: '取消', count: 0 },
    { key: 'timeout', label: '超时', count: 2 },
    { key: 'interrupted', label: '中断', count: 0 }
  ])
})

test('operations loader ignores stale responses and invalidates in-flight work', async () => {
  const pending = []
  const loader = createRuntimeOperationsLoader(() => new Promise((resolve) => pending.push(resolve)))
  const first = loader.load()
  const second = loader.load()
  pending[1]({ data: { observed_at: '2026-07-12T01:00:02.000Z' } })
  assert.equal((await second).summary.observed_at, '2026-07-12T01:00:02.000Z')
  pending[0]({ data: { observed_at: '2026-07-12T01:00:01.000Z' } })
  assert.deepEqual(await first, { applied: false })

  const third = loader.load()
  loader.invalidate()
  pending[2]({ data: { observed_at: '2026-07-12T01:00:03.000Z' } })
  assert.deepEqual(await third, { applied: false })
})

test('operations component owns conservative refresh lifecycle and quiet status tables', async () => {
  const source = await readFile(join(currentDir, 'ai-runtime-operations.vue'), 'utf8')

  assert.match(source, /getAgentRuntimeOperationsSummary/)
  assert.match(source, /const REFRESH_INTERVAL_MS = 60_000/)
  assert.match(source, /watch\(\(\) => props\.active/)
  assert.match(source, /onBeforeUnmount/)
  assert.match(source, /运行状态/)
  assert.match(source, /5 分钟/)
  assert.match(source, /1 小时/)
  assert.match(source, /失败分类/)
  assert.match(source, /实例状态/)
  assert.doesNotMatch(source, /pipeline|easydo-agent/i)
})
