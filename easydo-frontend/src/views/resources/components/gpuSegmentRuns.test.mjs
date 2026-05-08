import assert from 'node:assert/strict'

import { buildGpuSegmentLanes } from './gpuSegmentRuns.js'

const segment = (serviceId, memoryUsedBytes) => ({
  id: `segment-${serviceId}-${memoryUsedBytes}`,
  colorKey: serviceId,
  label: serviceId,
  service: { id: serviceId },
  serviceHover: {
    id: serviceId,
    memoryUsedBytes,
    pids: [memoryUsedBytes],
    observedPids: []
  },
  summary: { memoryUsedBytes },
  claims: [{ id: `claim-${serviceId}-${memoryUsedBytes}` }],
  allocationIds: [`allocation-${serviceId}`]
})

const cells = [
  {
    id: 'gpu-0',
    resourceInstance: { id: 'gpu-0', identityMap: { index: 3 } },
    segments: [segment('service-a', 10), segment('service-b', 20)]
  },
  {
    id: 'gpu-1',
    resourceInstance: { id: 'gpu-1', identityMap: { index: 4 } },
    segments: [segment('service-b', 30)]
  },
  {
    id: 'gpu-2',
    resourceInstance: { id: 'gpu-2' },
    segments: []
  },
  {
    id: 'gpu-3',
    resourceInstance: { id: 'gpu-3', identityMap: { index: 5 } },
    segments: [segment('service-b', 40)]
  }
]

const lanes = buildGpuSegmentLanes(cells)
const runs = lanes.flatMap(lane => lane)

assert.equal(runs.length, 3)

const serviceBFirstRun = runs.find(run => run.serviceId === 'service-b' && run.startColumn === 1)
assert.equal(serviceBFirstRun.span, 2)
assert.deepEqual(serviceBFirstRun.cellIds, ['gpu-0', 'gpu-1'])
assert.equal(serviceBFirstRun.summary.memoryUsedBytes, 50)
assert.equal(serviceBFirstRun.serviceHover.memoryUsedBytes, 50)
assert.deepEqual(serviceBFirstRun.serviceHover.pids, [20, 30])
assert.deepEqual(serviceBFirstRun.gpuMemoryUsages, [
  { gpuIndex: 3, gpuLabel: '3', memoryUsedBytes: 20 },
  { gpuIndex: 4, gpuLabel: '4', memoryUsedBytes: 30 }
])

const serviceAFirstRun = runs.find(run => run.serviceId === 'service-a')
assert.equal(serviceAFirstRun.startColumn, 1)
assert.equal(serviceAFirstRun.span, 1)

const serviceBSecondRun = runs.find(run => run.serviceId === 'service-b' && run.startColumn === 4)
assert.equal(serviceBSecondRun.span, 1)
assert.deepEqual(serviceBSecondRun.cellIds, ['gpu-3'])

console.log('gpu segment run tests passed')
