import test from 'node:test'
import assert from 'node:assert/strict'

import {
  createGpuInfoCacheEntry,
  createGpuRefreshToken,
  normalizeResourceGpuInfo,
  shouldRefreshResourceGpuInfo,
  beginGpuInfoRefresh,
  applyGpuInfoTerminalState,
  markGpuInfoTimeout,
  invalidateGpuInfoRefresh,
  resolveGpuInfoTerminalState
} from './aiResourceGpuInfo.js'

test('normalizeResourceGpuInfo extracts gpu devices from baseInfo.machine.gpu.devices and marks ready', () => {
  const collectedAt = 1710000000
  const resource = {
    baseInfo: {
      machine: {
        gpu: {
          devices: [
            {
              index: 0,
              name: 'RTX 4090',
              memoryBytes: 24 * 1024 ** 3,
              uuid: 'gpu-0'
            }
          ]
        }
      }
    },
    baseInfoStatus: 'succeeded',
    baseInfoCollectedAt: collectedAt
  }

  const result = normalizeResourceGpuInfo(resource)

  assert.equal(result.status, 'ready')
  assert.equal(result.gpuDevices.length, 1)
  assert.equal(result.gpuDevices[0].memoryBytes, 24 * 1024 ** 3)
  assert.equal(result.baseInfoStatus, 'succeeded')
  assert.equal(result.baseInfoCollectedAt, collectedAt)
  assert.deepEqual(result.baseInfoSnapshot, resource.baseInfo)
  assert.notEqual(result.baseInfoSnapshot, resource.baseInfo)
})

test('normalizeResourceGpuInfo does not treat count-only gpu data as ready', () => {
  const resource = {
    baseInfo: {
      machine: {
        gpu: {
          count: 4
        }
      }
    },
    baseInfoStatus: 'succeeded'
  }

  const result = normalizeResourceGpuInfo(resource)

  assert.equal(result.status, 'unsupported')
  assert.deepEqual(result.gpuDevices, [])
})

test('applyGpuInfoTerminalState ignores stale terminal updates by refreshToken', () => {
  const cacheEntry = {
    status: 'loading',
    requestedAt: 1710000000,
    refreshToken: 'latest',
    error: '',
    gpuDevices: [],
    baseInfoStatus: 'pending',
    baseInfoCollectedAt: 0,
    baseInfoSnapshot: {}
  }

  const result = applyGpuInfoTerminalState(cacheEntry, {
    status: 'error',
    refreshToken: 'stale',
    error: 'collector failed'
  })

  assert.equal(result.status, 'loading')
  assert.equal(result.refreshToken, 'latest')
  assert.equal(result.error, '')
})

test('applyGpuInfoTerminalState rejects terminal updates without refreshToken when current entry already has one', () => {
  const cacheEntry = createGpuInfoCacheEntry({
    status: 'loading',
    refreshToken: 'latest',
    baseInfoStatus: 'pending'
  })

  const result = applyGpuInfoTerminalState(cacheEntry, {
    status: 'error',
    error: 'collector failed'
  })

  assert.equal(result.status, 'loading')
  assert.equal(result.refreshToken, 'latest')
  assert.equal(result.error, '')
})

 test('shouldRefreshResourceGpuInfo refreshes idle, error, unsupported, and unknown states only', () => {
  assert.equal(shouldRefreshResourceGpuInfo(), true)
  assert.equal(shouldRefreshResourceGpuInfo({ status: 'idle' }), true)
  assert.equal(shouldRefreshResourceGpuInfo({ status: 'error' }), true)
  assert.equal(shouldRefreshResourceGpuInfo({ status: 'unsupported' }), true)
  assert.equal(shouldRefreshResourceGpuInfo({ status: 'weird-status' }), true)
  assert.equal(shouldRefreshResourceGpuInfo({ status: 'loading' }), false)
  assert.equal(shouldRefreshResourceGpuInfo({ status: 'ready' }), false)
})

test('normalizeResourceGpuInfo maps failed base info status to error and keeps last error message', () => {
  const resource = {
    baseInfo: {},
    baseInfoStatus: 'failed',
    baseInfoLastError: 'base info collection failed'
  }

  const result = normalizeResourceGpuInfo(resource)

  assert.equal(result.status, 'error')
  assert.equal(result.error, 'base info collection failed')
  assert.equal(result.baseInfoStatus, 'failed')
})

test('beginGpuInfoRefresh does not start a new refresh while the same resource is already loading', () => {
  const currentEntry = createGpuInfoCacheEntry({
    status: 'loading',
    refreshToken: 'token-1',
    requestedAt: 1710000000,
    baseInfoStatus: 'running'
  })

  const result = beginGpuInfoRefresh(currentEntry, createGpuRefreshToken(), 1710000100)

  assert.equal(result.started, false)
  assert.equal(result.entry.status, 'loading')
  assert.equal(result.entry.refreshToken, 'token-1')
  assert.equal(result.entry.requestedAt, 1710000000)
})

test('beginGpuInfoRefresh retry creates a new refreshToken', () => {
  const currentEntry = createGpuInfoCacheEntry({
    status: 'error',
    refreshToken: 'token-1',
    error: 'previous failure'
  })
  const nextToken = createGpuRefreshToken()

  const result = beginGpuInfoRefresh(currentEntry, nextToken, 1710000200)

  assert.equal(result.started, true)
  assert.equal(result.entry.status, 'loading')
  assert.equal(result.entry.refreshToken, nextToken)
  assert.notEqual(result.entry.refreshToken, 'token-1')
  assert.equal(result.entry.error, '')
})

test('stale token cannot overwrite latest terminal state', () => {
  const loadingEntry = beginGpuInfoRefresh(createGpuInfoCacheEntry(), 'latest-token', 1710000300).entry
  const latestTerminal = applyGpuInfoTerminalState(loadingEntry, {
    status: 'ready',
    refreshToken: 'latest-token',
    gpuDevices: [{ index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: '0' }],
    baseInfoStatus: 'succeeded'
  })

  const staleResult = applyGpuInfoTerminalState(latestTerminal, {
    status: 'error',
    refreshToken: 'stale-token',
    error: 'late stale failure'
  })

  assert.equal(staleResult.status, 'ready')
  assert.equal(staleResult.refreshToken, 'latest-token')
  assert.equal(staleResult.error, '')
})

test('markGpuInfoTimeout turns matching loading token into error', () => {
  const loadingEntry = beginGpuInfoRefresh(createGpuInfoCacheEntry(), 'timeout-token', 1710000400).entry

  const timeoutEntry = markGpuInfoTimeout(loadingEntry, 'timeout-token', 'gpu info refresh timed out')

  assert.equal(timeoutEntry.status, 'error')
  assert.equal(timeoutEntry.refreshToken, 'timeout-token')
  assert.equal(timeoutEntry.error, 'gpu info refresh timed out')
})

test('resolveGpuInfoTerminalState updates only the targeted resource cache entry', () => {
  const resourceOneEntry = beginGpuInfoRefresh(createGpuInfoCacheEntry(), 'token-1', 1710000500).entry
  const resourceTwoEntry = createGpuInfoCacheEntry({ status: 'ready', gpuDevices: [{ index: 1, memoryBytes: 16 * 1024 ** 3, deviceKey: '1' }] })
  const cache = {
    '1': resourceOneEntry,
    '2': resourceTwoEntry
  }
  const resource = {
    id: 1,
    baseInfoStatus: 'succeeded',
    baseInfo: {
      machine: {
        gpu: {
          devices: [{ index: 0, memoryBytes: 24 * 1024 ** 3, uuid: 'gpu-0' }]
        }
      }
    }
  }

  const nextCache = resolveGpuInfoTerminalState(cache, 1, 'token-1', resource)

  assert.equal(nextCache['1'].status, 'ready')
  assert.equal(nextCache['2'], resourceTwoEntry)
  assert.equal(nextCache['2'].status, 'ready')
})

test('invalidateGpuInfoRefresh resets loading entry to idle so it can refresh again after resource switch or dialog close', () => {
  const loadingEntry = beginGpuInfoRefresh(createGpuInfoCacheEntry(), 'token-before-close', 1710000600).entry
  const invalidatedEntry = invalidateGpuInfoRefresh(loadingEntry)

  const staleResult = applyGpuInfoTerminalState(invalidatedEntry, {
    status: 'ready',
    refreshToken: 'token-before-close',
    gpuDevices: [{ index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: '0' }],
    baseInfoStatus: 'succeeded'
  })

  assert.equal(invalidatedEntry.status, 'idle')
  assert.equal(invalidatedEntry.refreshToken, '')
  assert.equal(invalidatedEntry.requestedAt, 0)
  assert.equal(shouldRefreshResourceGpuInfo(invalidatedEntry), true)
  assert.equal(staleResult.status, 'idle')
  assert.equal(staleResult.refreshToken, '')
})

test('invalidateGpuInfoRefresh keeps non-loading entry status while clearing token only', () => {
  const readyEntry = createGpuInfoCacheEntry({
    status: 'ready',
    refreshToken: 'token-ready',
    requestedAt: 1710000700,
    gpuDevices: [{ index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: '0' }]
  })

  const invalidatedEntry = invalidateGpuInfoRefresh(readyEntry)

  assert.equal(invalidatedEntry.status, 'ready')
  assert.equal(invalidatedEntry.refreshToken, '')
  assert.equal(invalidatedEntry.requestedAt, 1710000700)
  assert.equal(shouldRefreshResourceGpuInfo(invalidatedEntry), false)
})
