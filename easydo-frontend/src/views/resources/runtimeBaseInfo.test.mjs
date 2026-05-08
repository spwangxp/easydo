import assert from 'node:assert/strict'

import { normalizeRuntimeBaseInfo } from './runtimeBaseInfo.js'

const canonicalVmPayload = {
  schemaVersion: 3,
  resourceId: 'vm-resource-1',
  status: 'ready',
  source: 'agent_self',
  collectedAt: '2026-05-06T10:00:00Z',
  labels: { environment: 'testing' },
  entities: [
    {
      id: 'resource:1:entity:host',
      kind: 'host',
      name: 'vm-host-1',
      fields: [
        { name: 'hostname', value: 'vm-host-1' },
        { name: 'primaryIpv4', value: '10.0.0.8' },
        { name: 'osName', value: 'Ubuntu' },
        { name: 'osVersion', value: '24.04' },
        { name: 'kernelVersion', value: '6.8.0' },
        { name: 'arch', value: 'x86_64' }
      ]
    }
  ],
  resourceTypes: [
    { id: 'cpu', name: 'CPU' },
    { id: 'memory', name: 'Memory' },
    { id: 'storage', name: 'Storage' },
    { id: 'gpu', name: 'GPU' }
  ],
  resourceInstances: [
    {
      id: 'cpu-pool',
      resourceTypeId: 'cpu',
      entityId: 'resource:1:entity:host',
      identity: [{ name: 'pool', value: 'cpu' }],
      spec: [{ name: 'model', value: 'AMD EPYC' }],
      capacity: [
        { name: 'logicalCores', allocatable: 64 },
        { name: 'logicalCoresUsed', used: 12 }
      ]
    },
    {
      id: 'memory-pool',
      resourceTypeId: 'memory',
      entityId: 'resource:1:entity:host',
      identity: [{ name: 'pool', value: 'memory' }],
      capacity: [
        { name: 'memoryBytes', allocatable: 274877906944 },
        { name: 'memoryBytesUsed', used: 68719476736 }
      ]
    },
    {
      id: 'storage-pool',
      resourceTypeId: 'storage',
      entityId: 'resource:1:entity:host',
      identity: [{ name: 'pool', value: 'storage' }],
      capacity: [
        { name: 'diskBytes', capacity: 1099511627776 },
        { name: 'rootDiskBytes', capacity: 536870912000 }
      ]
    },
    {
      id: 'gpu-0',
      resourceTypeId: 'gpu',
      entityId: 'resource:1:entity:host',
      identity: [
        { name: 'index', value: 0 },
        { name: 'uuid', value: 'GPU-0-UUID' },
        { name: 'busId', value: '0000:01:00.0' }
      ],
      spec: [
        { name: 'vendor', value: 'NVIDIA' },
        { name: 'model', value: 'A100' }
      ],
      capacity: [
        { name: 'memoryBytes', capacity: 85899345920 },
        { name: 'memoryBytesAvailable', available: 64424509440 }
      ],
      metrics: [
        { name: 'memoryBytesUsed', value: 21474836480 },
        { name: 'utilizationGpuPercent', value: 55 },
        { name: 'temperatureGpuCelsius', value: 67 }
      ]
    },
    {
      id: 'gpu-1',
      resourceTypeId: 'gpu',
      entityId: 'resource:1:entity:host',
      identity: [
        { name: 'index', value: 1 },
        { name: 'uuid', value: 'GPU-1-UUID' },
        { name: 'busId', value: '0000:02:00.0' }
      ],
      spec: [
        { name: 'vendor', value: 'NVIDIA' },
        { name: 'model', value: 'A100' }
      ],
      capacity: [{ name: 'memoryBytes', capacity: 85899345920 }],
      metrics: [{ name: 'utilizationGpuPercent', value: 10 }, { name: 'temperatureGpuCelsius', value: 49 }]
    }
  ],
  services: [
    {
      id: 'service-a',
      name: 'trainer-a',
      entityId: 'resource:1:entity:host',
      resourceInstanceIds: ['gpu-0'],
      fields: [
        { name: 'pid', value: 1201 },
        { name: 'runtime', value: 'docker' },
        { name: 'containerId', value: 'ctr-a' },
        { name: 'containerName', value: 'trainer-a' }
      ]
    },
    {
      id: 'service-b',
      name: 'trainer-b',
      entityId: 'resource:1:entity:host',
      resourceInstanceIds: ['gpu-0', 'gpu-1'],
      fields: [
        { name: 'pid', value: 1301 },
        { name: 'runtime', value: 'docker' },
        { name: 'containerId', value: 'ctr-b' },
        { name: 'containerName', value: 'trainer-b' }
      ]
    },
    {
      id: 'service-idle',
      name: 'idle-service',
      entityId: 'resource:1:entity:host',
      resourceInstanceIds: [],
      fields: [{ name: 'pid', value: 1401 }]
    }
  ],
  allocations: [
    {
      id: 'alloc-a',
      claims: [
        {
          serviceId: 'service-a',
          resourceInstanceId: 'gpu-0',
          dimensions: [
            { name: 'pid', value: 1201 },
            { name: 'memoryUsedBytes', value: 21474836480 }
          ]
        }
      ]
    },
    {
      id: 'alloc-b',
      claims: [
        {
          serviceId: 'service-b',
          resourceInstanceId: 'gpu-0',
          dimensions: [
            { name: 'pid', value: 1301 },
            { name: 'observedPid', value: 2301 },
            { name: 'memoryUsedBytes', value: 10737418240 }
          ]
        },
        {
          serviceId: 'service-b',
          resourceInstanceId: 'gpu-1',
          dimensions: [
            { name: 'pid', value: 1301 },
            { name: 'memoryUsedBytes', value: 8589934592 }
          ]
        }
      ]
    }
  ],
  vm: { should: 'be ignored' },
  machine: { should: 'be ignored' },
  gpuColumns: [{ id: 'legacy-col' }],
  matrixRows: [{ id: 'legacy-row' }]
}

const normalizedVm = normalizeRuntimeBaseInfo(canonicalVmPayload)

assert.equal(normalizedVm.schemaVersion, 3)
assert.equal(normalizedVm.resourceId, 'vm-resource-1')
assert.equal(normalizedVm.status, 'ready')
assert.equal(normalizedVm.source, 'agent_self')
assert.equal(normalizedVm.collectedAt, '2026-05-06T10:00:00Z')
assert.deepEqual(normalizedVm.labels, { environment: 'testing' })
assert.equal(normalizedVm.summary.gpuCount, 2)
assert.equal(normalizedVm.summary.hostCount, 1)
assert.equal(normalizedVm.summary.nodeCount, 0)
assert.equal(normalizedVm.resourceInstancesById['gpu-0'].entityId, 'resource:1:entity:host')
assert.equal(normalizedVm.resourceInstancesById['gpu-0'].specMap.model, 'A100')
assert.equal(normalizedVm.resourceInstancesById['gpu-0'].metricsMap.temperatureGpuCelsius.value, 67)
assert.equal(normalizedVm.servicesById['service-a'].fieldsMap.runtime, 'docker')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[0].caption, 'docker')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[0].label, 'trainer-a')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].caption, 'docker')
assert.equal(normalizedVm.allocationsById['alloc-a'].claims.length, 1)
assert.equal(normalizedVm.detail.hostSummary.hostname, 'vm-host-1')
assert.equal(normalizedVm.detail.hostSummary.primaryIpv4, '10.0.0.8')
assert.equal(normalizedVm.detail.hostSummary.cpuLogicalCores, 64)
assert.equal(normalizedVm.detail.hostSummary.memoryBytes, 274877906944)
assert.equal(normalizedVm.detail.hostSummary.diskBytes, 1099511627776)
assert.equal(normalizedVm.detail.hostSummary.gpuCount, 2)
assert.deepEqual(normalizedVm.detail.hostSummary.gpuModels, ['A100'])
assert.deepEqual(normalizedVm.matrix.rows.map(row => row.id), ['service-a', 'service-b'])
assert.deepEqual(normalizedVm.matrix.columns.map(column => column.id), ['gpu-0', 'gpu-1'])
assert.equal(normalizedVm.matrix.cells['service-a::gpu-0'].claimCount, 1)
assert.equal(normalizedVm.matrix.cells['service-a::gpu-0'].summary.pid, 1201)
assert.equal(normalizedVm.matrix.cells['service-a::gpu-0'].summary.memoryUsedBytes, 21474836480)
assert.equal(normalizedVm.matrix.cells['service-b::gpu-0'].claimCount, 1)
assert.equal(normalizedVm.matrix.cells['service-b::gpu-1'].claimCount, 1)
assert.equal(normalizedVm.matrix.cells['service-a::gpu-1'].claimCount, 0)
assert.equal(normalizedVm.matrix.hasData, true)
assert.equal(normalizedVm.gpuView.rowCount, 1)
assert.equal(normalizedVm.gpuView.gpuCount, 2)
assert.equal(normalizedVm.gpuView.activeServiceCount, 2)
assert.equal(normalizedVm.gpuView.hasData, true)
assert.deepEqual(normalizedVm.gpuView.rows.map(row => row.id), ['resource:1:entity:host'])
assert.equal(normalizedVm.gpuView.rows[0].gpuCells.length, 2)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].resourceInstance.id, 'gpu-0')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments.length, 2)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[0].label, 'trainer-a')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[0].summary.memoryUsedBytes, 21474836480)
assert.deepEqual(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].serviceHover.pids, [1301])
assert.deepEqual(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].serviceHover.observedPids, [2301])
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].serviceHover.descendantProcessCount, 1)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].serviceHover.descendantProcesses[0].memoryUsedBytes, 10737418240)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].serviceHover.runtimeType, 'docker')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].serviceHover.containerName, 'trainer-b')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].segments[1].gpuHover.temperatureGpuCelsius, 67)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].occupancy.serviceSummaries[0].runtimeType, 'docker')
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].occupancy.serviceSummaries[1].memoryUsedBytes, 10737418240)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].resourceInstance.metricsMap.temperatureGpuCelsius.value, 67)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[0].resourceInstance.metricsMap.memoryBytesUsed.value, 21474836480)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[1].segments.length, 1)
assert.equal(normalizedVm.gpuView.rows[0].gpuCells[1].segments[0].label, 'trainer-b')
assert.equal('vm' in normalizedVm, false)
assert.equal('k8s' in normalizedVm, false)
assert.equal('machine' in normalizedVm, false)
assert.equal('gpuColumns' in normalizedVm, false)
assert.equal('matrixRows' in normalizedVm, false)

const canonicalK8sPayload = {
  schemaVersion: 3,
  resourceId: 'k8s-resource-1',
  status: 'ready',
  source: 'k8s_api',
  collectedAt: '2026-05-06T11:00:00Z',
  labels: { cluster: 'qa' },
  entities: [
    {
      id: 'node-1',
      kind: 'node',
      name: 'node-a',
      fields: [
        { name: 'roles', value: ['gpu', 'worker'] },
        { name: 'arch', value: 'amd64' },
        { name: 'osImage', value: 'Ubuntu 24.04' },
        { name: 'kubeletVersion', value: 'v1.31.0' },
        { name: 'serverVersion', value: 'v1.31.0' }
      ]
    },
    {
      id: 'node-2',
      kind: 'node',
      name: 'node-b',
      fields: [
        { name: 'roles', value: ['gpu'] },
        { name: 'arch', value: 'amd64' },
        { name: 'osImage', value: 'Ubuntu 24.04' },
        { name: 'kubeletVersion', value: 'v1.31.0' },
        { name: 'serverVersion', value: 'v1.31.0' }
      ]
    }
  ],
  resourceTypes: [
    { id: 'cpu', name: 'CPU' },
    { id: 'memory', name: 'Memory' },
    { id: 'gpu', name: 'GPU' }
  ],
  resourceInstances: [
    {
      id: 'node-1-cpu',
      resourceTypeId: 'cpu',
      entityId: 'node-1',
      identity: [{ name: 'pool', value: 'cpu' }],
      capacity: [
        { name: 'cpuMilli', allocatable: 64000 },
        { name: 'cpuMilliUsed', used: 12000 }
      ]
    },
    {
      id: 'node-1-memory',
      resourceTypeId: 'memory',
      entityId: 'node-1',
      identity: [{ name: 'pool', value: 'memory' }],
      capacity: [
        { name: 'memoryBytes', allocatable: 274877906944 },
        { name: 'memoryBytesUsed', used: 68719476736 }
      ]
    },
    {
      id: 'node-1-gpu-0',
      resourceTypeId: 'gpu',
      entityId: 'node-1',
      identity: [
        { name: 'uuid', value: 'GPU-node1-0' },
        { name: 'busId', value: '0000:11:00.0' },
        { name: 'name', value: 'node-a GPU0' },
        { name: 'index', value: '0' }
      ],
      spec: [
        { name: 'vendor', value: 'NVIDIA' },
        { name: 'model', value: 'L40S' }
      ],
      capacity: [{ name: 'memoryBytes', capacity: 51539607552 }],
      metrics: [{ name: 'memoryBytesUsed', value: 21474836480 }]
    },
    {
      id: 'node-2-cpu',
      resourceTypeId: 'cpu',
      entityId: 'node-2',
      identity: [{ name: 'pool', value: 'cpu' }],
      capacity: [
        { name: 'cpuMilli', allocatable: 64000 },
        { name: 'cpuMilliUsed', used: 8000 }
      ]
    },
    {
      id: 'node-2-memory',
      resourceTypeId: 'memory',
      entityId: 'node-2',
      identity: [{ name: 'pool', value: 'memory' }],
      capacity: [
        { name: 'memoryBytes', allocatable: 274877906944 },
        { name: 'memoryBytesUsed', used: 34359738368 }
      ]
    },
    {
      id: 'node-2-gpu-0',
      resourceTypeId: 'gpu',
      entityId: 'node-2',
      identity: [
        { name: 'uuid', value: 'GPU-node2-0' },
        { name: 'busId', value: '0000:21:00.0' },
        { name: 'name', value: 'node-b GPU0' },
        { name: 'index', value: '0' }
      ],
      spec: [
        { name: 'vendor', value: 'NVIDIA' },
        { name: 'model', value: 'L40S' }
      ],
      capacity: [{ name: 'memoryBytes', capacity: 51539607552 }]
    }
  ],
  services: [
    {
      id: 'svc-infer-a',
      name: 'inference-a',
      entityId: 'node-1',
      resourceInstanceIds: ['node-1-gpu-0'],
      fields: [
        { name: 'uid', value: 'pod-a' },
        { name: 'namespace', value: 'default' },
        { name: 'nodeName', value: 'node-a' },
        { name: 'phase', value: 'Running' },
        { name: 'ownerKind', value: 'StatefulSet' },
        { name: 'ownerName', value: 'infer-b' },
        { name: 'ownerKind', value: 'Deployment' },
        { name: 'ownerName', value: 'infer-a' }
      ]
    },
    {
      id: 'svc-infer-b',
      name: 'inference-b',
      entityId: 'node-2',
      resourceInstanceIds: ['node-1-gpu-0', 'node-2-gpu-0'],
      fields: [
        { name: 'uid', value: 'pod-b' },
        { name: 'namespace', value: 'default' },
        { name: 'nodeName', value: 'node-b' },
        { name: 'phase', value: 'Running' },
        { name: 'ownerKind', value: 'StatefulSet' },
        { name: 'ownerName', value: 'infer-b' }
      ]
    }
  ],
  allocations: [
    {
      id: 'alloc-k8s-1',
      claims: [{
        serviceId: 'svc-infer-a',
        resourceInstanceId: 'node-1-gpu-0',
        dimensions: [{ name: 'podUid', value: 'pod-a' }]
      }]
    },
    {
      id: 'alloc-k8s-2',
      claims: [
        {
          serviceId: 'svc-infer-b',
          resourceInstanceId: 'node-1-gpu-0',
          dimensions: [{ name: 'podUid', value: 'pod-b' }]
        },
        {
          serviceId: 'svc-infer-b',
          resourceInstanceId: 'node-2-gpu-0',
          dimensions: [{ name: 'podUid', value: 'pod-b' }]
        }
      ]
    }
  ],
  k8s: { should: 'be ignored' },
  gpuColumns: [{ id: 'legacy-col-k8s' }],
  matrixRows: [{ id: 'legacy-row-k8s' }]
}

const normalizedK8s = normalizeRuntimeBaseInfo(canonicalK8sPayload)

assert.equal(normalizedK8s.summary.nodeCount, 2)
assert.equal(normalizedK8s.summary.gpuCount, 2)
assert.equal(normalizedK8s.detail.clusterSummary.clusterVersion, 'v1.31.0')
assert.equal(normalizedK8s.detail.clusterSummary.nodeCount, 2)
assert.equal(normalizedK8s.detail.clusterSummary.cpuAllocatableMilli, 128000)
assert.equal(normalizedK8s.detail.clusterSummary.memoryAllocatableBytes, 549755813888)
assert.equal(normalizedK8s.detail.clusterSummary.gpuAllocatable, 2)
assert.deepEqual(normalizedK8s.detail.nodeSummaries.map(item => item.name), ['node-a', 'node-b'])
assert.deepEqual(normalizedK8s.matrix.rows.map(row => row.id), ['svc-infer-a', 'svc-infer-b'])
assert.deepEqual(normalizedK8s.matrix.columns.map(column => column.id), ['node-1-gpu-0', 'node-2-gpu-0'])
assert.equal(normalizedK8s.matrix.cells['svc-infer-b::node-1-gpu-0'].claimCount, 1)
assert.equal(normalizedK8s.matrix.cells['svc-infer-b::node-1-gpu-0'].summary.podUid, 'pod-b')
assert.equal(normalizedK8s.matrix.cells['svc-infer-a::node-2-gpu-0'].claimCount, 0)
assert.equal(normalizedK8s.gpuView.rowCount, 2)
assert.deepEqual(normalizedK8s.gpuView.rows.map(row => row.id), ['node-1', 'node-2'])
assert.equal(normalizedK8s.gpuView.rows[0].gpuCells.length, 1)
assert.equal(normalizedK8s.gpuView.rows[0].gpuCells[0].segments.length, 2)
assert.equal(normalizedK8s.gpuView.rows[0].gpuCells[0].segments[0].summary.podUid, 'pod-a')
assert.equal(normalizedK8s.gpuView.rows[0].gpuCells[0].segments[0].caption, 'pod')
assert.equal(normalizedK8s.gpuView.rows[1].gpuCells[0].segments[0].caption, 'pod')
assert.equal(normalizedK8s.gpuView.rows[1].gpuCells[0].segments.length, 1)
assert.equal('k8s' in normalizedK8s, false)
assert.equal('gpuColumns' in normalizedK8s, false)
assert.equal('matrixRows' in normalizedK8s, false)

const malformedReferencePayload = {
  schemaVersion: 3,
  resourceId: 'malformed-check',
  entities: [{ id: 'node-bad', kind: 'node', name: 'node-bad' }],
  resourceTypes: [{ id: 'gpu', name: 'GPU' }],
  resourceInstances: [
    { id: 'node-bad-gpu-0', entityId: 'node-bad', resourceTypeId: 'gpu', identity: [{ name: 'index', value: 0 }] },
    { id: 'broken-instance', entityId: 'node-bad', resourceTypeId: 'missing-type', identity: [{ name: 'name', value: 'broken' }] }
  ],
  services: [{ id: 'svc-bad', name: 'job-bad', entityId: 'node-bad', fields: [{ name: 'uid', value: 'bad-pod' }] }],
  allocations: [
    { id: 'alloc-good', claims: [{ serviceId: 'svc-bad', resourceInstanceId: 'node-bad-gpu-0', dimensions: [{ name: 'podUid', value: 'bad-pod' }] }] },
    { id: 'alloc-missing-instance', claims: [{ serviceId: 'svc-bad', resourceInstanceId: 'missing-instance', dimensions: [{ name: 'podUid', value: 'bad-pod-2' }] }] },
    { id: 'alloc-missing-service', claims: [{ serviceId: 'missing-service', resourceInstanceId: 'node-bad-gpu-0', dimensions: [{ name: 'podUid', value: 'bad-pod-3' }] }] }
  ]
}

const malformed = normalizeRuntimeBaseInfo(malformedReferencePayload)
assert.equal(malformed.resourceInstancesById['broken-instance'].resourceTypeId, 'missing-type')
assert.deepEqual(malformed.matrix.rows.map(row => row.id), ['svc-bad'])
assert.deepEqual(malformed.matrix.columns.map(column => column.id), ['node-bad-gpu-0'])
assert.equal(malformed.matrix.cells['svc-bad::node-bad-gpu-0'].claimCount, 1)
assert.equal(malformed.matrix.cells['svc-bad::node-bad-gpu-0'].summary.podUid, 'bad-pod')
assert.equal(malformed.gpuView.rowCount, 1)
assert.equal(malformed.gpuView.rows[0].gpuCells.length, 1)
assert.equal(malformed.gpuView.rows[0].gpuCells[0].segments.length, 1)
assert.equal(Boolean(malformed.matrix.cells['svc-bad::missing-instance']), false)
assert.equal(Boolean(malformed.matrix.cells['missing-service::node-bad-gpu-0']), false)

console.log('runtime base info tests passed')
