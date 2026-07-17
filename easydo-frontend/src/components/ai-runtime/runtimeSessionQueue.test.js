import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'

import {
  runtimeQueueModeLabel,
  runtimeQueueStatusLabel,
  runtimeQueueStatusType
} from './runtimeSessionQueue.js'

test('runtime queue presentation maps modes and statuses consistently', () => {
  assert.equal(runtimeQueueModeLabel('follow_up'), '跟进')
  assert.equal(runtimeQueueModeLabel('steer'), '注入')
  assert.equal(runtimeQueueModeLabel('stop_and_run'), '停止重发')
  assert.equal(runtimeQueueStatusLabel('pending'), '排队中')
  assert.equal(runtimeQueueStatusLabel('claimed'), '处理中')
  assert.equal(runtimeQueueStatusLabel('applied'), '已应用')
  assert.equal(runtimeQueueStatusLabel('consumed'), '已消费')
  assert.equal(runtimeQueueStatusLabel('cancelled'), '已取消')
  assert.equal(runtimeQueueStatusLabel('expired'), '已过期')
  assert.equal(runtimeQueueStatusLabel('failed'), '失败')
  assert.equal(runtimeQueueStatusType('claimed'), 'warning')
  assert.equal(runtimeQueueStatusType('applied'), 'success')
  assert.equal(runtimeQueueStatusType('failed'), 'danger')
  assert.equal(runtimeQueueStatusType('cancelled'), 'info')
})

test('runtime queue component renders compact items and only cancels pending rows', async () => {
  const source = await readFile(new URL('./RuntimeSessionQueue.vue', import.meta.url), 'utf8')

  assert.match(source, /v-for="item in items"/)
  assert.match(source, /item\.status === 'pending'/)
  assert.match(source, /emit\('cancel', item\.queue_item_id\)/)
  assert.match(source, /runtimeQueueModeLabel/)
  assert.match(source, /runtimeQueueStatusLabel/)
  assert.match(source, /runtime-session-queue__content/)
})
