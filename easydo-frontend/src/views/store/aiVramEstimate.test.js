import test from 'node:test'
import assert from 'node:assert/strict'

import {
  parseModelParameterCount,
  resolveModelPrecisionBytes,
  resolveKvPrecisionBytes,
  normalizeGpuDevices,
  buildDeployVramEstimate,
  buildDeployVramEstimateViewModel
} from './aiVramEstimate.js'

test('parseModelParameterCount supports direct and mixture billion forms', () => {
  assert.equal(parseModelParameterCount('7B'), 7e9)
  assert.equal(parseModelParameterCount('8x7B'), 56e9)
})

test('resolveModelPrecisionBytes prefers quantization over dtype', () => {
  assert.equal(resolveModelPrecisionBytes({
    parameterValues: { quantization: 'AWQ', dtype: 'fp16' },
    model: {}
  }), 0.5)
})

test('resolveModelPrecisionBytes uses load_format before dtype and falls back to model metadata', () => {
  assert.equal(resolveModelPrecisionBytes({
    parameterValues: { load_format: 'fp8', dtype: 'fp16' },
    model: {}
  }), 1)

  assert.equal(resolveModelPrecisionBytes({
    parameterValues: { dtype: 'fp16' },
    model: { quantization: 'gptq' }
  }), 2)

  assert.equal(resolveModelPrecisionBytes({
    parameterValues: {},
    model: { quantization: 'gptq' }
  }), 0.5)
})

test('resolveKvPrecisionBytes resolves kv-specific aliases before dtype and ignores model quantization', () => {
  assert.equal(resolveKvPrecisionBytes({
    parameterValues: { kv_cache_dtype: 'fp8', dtype: 'fp32' },
    model: {}
  }), 1)

  assert.equal(resolveKvPrecisionBytes({
    parameterValues: { dtype: 'fp32' },
    model: { quantization: 'gptq' }
  }), 4)
})

test('normalizeGpuDevices preserves deviceKey and formats labels', () => {
  const devices = normalizeGpuDevices([
    { index: 0, name: 'RTX 4090', memoryBytes: 24 * 1024 ** 3, deviceKey: 'gpu-0' }
  ])

  assert.equal(devices[0].deviceKey, 'gpu-0')
  assert.match(devices[0].label, /RTX 4090/)
})

test('buildDeployVramEstimate returns insufficient status with memory breakdown', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '14B', architecture: 'llama' },
    parameterValues: { max_model_len: 32768, max_num_seqs: 4, gpu_memory_utilization: 0.9 },
    gpuDevices: [
      { index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: 'gpu-0' }
    ],
    selectedGpuDeviceKeys: ['gpu-0']
  })

  assert.equal(estimate.status, 'insufficient')
  assert.ok(estimate.totalBytes > 0)
  assert.ok(estimate.weightsBytes > 0)
  assert.ok(estimate.kvCacheBytes > 0)
  assert.ok(estimate.runtimeReserveBytes > 0)
})

test('buildDeployVramEstimate returns sufficient status when gpu capacity is adequate', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '7B', architecture: 'qwen' },
    parameterValues: { max_model_len: 4096, max_num_seqs: 1, gpu_memory_utilization: 0.9 },
    gpuDevices: [
      { index: 0, memoryBytes: 80 * 1024 ** 3, deviceKey: 'gpu-0' }
    ],
    selectedGpuDeviceKeys: ['gpu-0']
  })

  assert.equal(estimate.status, 'sufficient')
  assert.ok(estimate.usageRatio < estimate.gpuUtilization)
})

 test('buildDeployVramEstimate returns missing-data when gpu memory is unavailable', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '7B', architecture: 'qwen' },
    parameterValues: { max_model_len: 4096, max_num_seqs: 1 },
    gpuDevices: [],
    selectedGpuDeviceKeys: []
  })

  assert.equal(estimate.status, 'missing-data')
  assert.match(estimate.message, /缺少可用 GPU 数据|无法估算/)
})

test('buildDeployVramEstimate resolves gpu utilization and cpu offload aliases', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '7B', architecture: 'qwen' },
    parameterValues: {
      gpuMemoryUtilization: 85,
      cpu_offload: 8,
      context_length: 8192,
      batch_size: 2
    },
    gpuDevices: [{ index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: 'gpu-0' }],
    selectedGpuDeviceKeys: ['gpu-0']
  })

  assert.equal(estimate.gpuUtilization, 0.85)
  assert.ok(estimate.cpuOffloadBytes > 0)
})

test('buildDeployVramEstimate resolves context and sequence aliases', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '7B', architecture: 'qwen' },
    parameterValues: {
      max_seq_len: 16384,
      max_batch_size: 3,
      gpu_memory_utilization: 0.9
    },
    gpuDevices: [{ index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: 'gpu-0' }],
    selectedGpuDeviceKeys: ['gpu-0']
  })

  assert.equal(estimate.contextLength, 16384)
  assert.equal(estimate.maxSequences, 3)
})

test('buildDeployVramEstimate prefers selected gpus then falls back to resource gpus', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '7B', architecture: 'qwen' },
    parameterValues: { tensor_parallel_size: 2 },
    gpuDevices: [
      { index: 0, memoryBytes: 24 * 1024 ** 3, deviceKey: 'gpu-0' },
      { index: 1, memoryBytes: 24 * 1024 ** 3, deviceKey: 'gpu-1' }
    ],
    selectedGpuDeviceKeys: []
  })

  assert.equal(estimate.runtimeGpuCount, 2)
  assert.equal(estimate.selectedGpuMemoryBytes, 48 * 1024 ** 3)
})

test('buildDeployVramEstimate uses tensor_parallel_size as effective gpu count fallback for reserve math', () => {
  const estimate = buildDeployVramEstimate({
    model: { parameterSize: '7B', architecture: 'qwen' },
    parameterValues: { tensor_parallel_size: 4 },
    gpuDevices: [],
    selectedGpuDeviceKeys: []
  })

  assert.equal(estimate.runtimeGpuCount, 4)
})

test('buildDeployVramEstimateViewModel maps idle to instructional state', () => {
  const viewModel = buildDeployVramEstimateViewModel({ resourceState: 'idle' })

  assert.equal(viewModel.displayStatus, 'idle')
  assert.match(viewModel.message, /选择目标资源|先选择资源/)
})

test('buildDeployVramEstimateViewModel keeps model-side estimate visible before selecting resource', () => {
  const viewModel = buildDeployVramEstimateViewModel({
    resourceState: 'idle',
    estimate: {
      status: 'missing-data',
      parameterCount: 14e9,
      contextLength: 32768,
      maxSequences: 4,
      tensorParallelSize: 2,
      runtimeGpuCount: 2,
      weightsBytes: 28 * 1024 ** 3,
      kvCacheBytes: 12 * 1024 ** 3,
      runtimeReserveBytes: 6 * 1024 ** 3,
      cpuOffloadBytes: 4 * 1024 ** 3,
      totalBytes: 42 * 1024 ** 3,
      selectedGpuMemoryBytes: 0,
      gpuUtilization: 0.9,
      usageRatio: null,
      message: '缺少可用 GPU 数据，无法估算显存。'
    }
  })

  assert.equal(viewModel.displayStatus, 'idle')
  assert.ok(viewModel.summary.some((item) => /总显存需求/.test(item.label) && /42/.test(item.value)))
  assert.ok(viewModel.composition.some((item) => /模型权重/.test(item.label) && /28/.test(item.value)))
  assert.ok(viewModel.selection.some((item) => /参数量/.test(item.label) && /14/.test(item.value)))
  assert.match(viewModel.message, /目标资源|GPU/)
})

test('buildDeployVramEstimateViewModel maps loading to collecting', () => {
  const viewModel = buildDeployVramEstimateViewModel({ resourceState: 'loading' })

  assert.equal(viewModel.displayStatus, 'collecting')
  assert.match(viewModel.message, /采集|收集/)
})

test('buildDeployVramEstimateViewModel maps error to failed with retry', () => {
  const viewModel = buildDeployVramEstimateViewModel({
    resourceState: 'error',
    resourceError: 'collector failed'
  })

  assert.equal(viewModel.displayStatus, 'failed')
  assert.equal(viewModel.showRetry, true)
})

test('buildDeployVramEstimateViewModel maps unsupported to missing-data', () => {
  const viewModel = buildDeployVramEstimateViewModel({ resourceState: 'unsupported' })

  assert.equal(viewModel.displayStatus, 'missing-data')
})

test('buildDeployVramEstimateViewModel returns summary and breakdown for ready state', () => {
  const viewModel = buildDeployVramEstimateViewModel({
    resourceState: 'ready',
    estimate: {
      status: 'warning',
      weightsBytes: 10 * 1024 ** 3,
      kvCacheBytes: 20 * 1024 ** 3,
      runtimeReserveBytes: 30 * 1024 ** 3,
      cpuOffloadBytes: 5 * 1024 ** 3,
      totalBytes: 55 * 1024 ** 3,
      selectedGpuMemoryBytes: 80 * 1024 ** 3,
      gpuUtilization: 0.9,
      usageRatio: 55 / 80,
      message: 'warning'
    }
  })

  assert.equal(viewModel.displayStatus, 'warning')
  assert.equal(viewModel.breakdown.length, 4)
  assert.ok(viewModel.summary.some((item) => /总显存需求/.test(item.label)))
  assert.ok(viewModel.summary.some((item) => /GPU 容量/.test(item.label)))
  assert.ok(viewModel.summary.some((item) => /阈值|占比/.test(item.label)))
})

test('buildDeployVramEstimateViewModel exposes compact composition details and current selection summary', () => {
  const viewModel = buildDeployVramEstimateViewModel({
    resourceState: 'ready',
    estimate: {
      status: 'warning',
      parameterCount: 14e9,
      contextLength: 32768,
      maxSequences: 4,
      tensorParallelSize: 2,
      runtimeGpuCount: 2,
      weightsBytes: 28 * 1024 ** 3,
      kvCacheBytes: 12 * 1024 ** 3,
      runtimeReserveBytes: 6 * 1024 ** 3,
      cpuOffloadBytes: 4 * 1024 ** 3,
      totalBytes: 42 * 1024 ** 3,
      selectedGpuMemoryBytes: 48 * 1024 ** 3,
      gpuUtilization: 0.9,
      usageRatio: 42 / 48,
      message: 'warning'
    }
  })

  assert.ok(viewModel.composition.some((item) => /模型权重/.test(item.label)))
  assert.ok(viewModel.composition.some((item) => /KV Cache/.test(item.label)))
  assert.ok(viewModel.composition.some((item) => /运行时开销/.test(item.label)))
  assert.ok(viewModel.composition.some((item) => /CPU 卸载/.test(item.label)))
  assert.ok(viewModel.selection.some((item) => /参数量/.test(item.label)))
  assert.ok(viewModel.selection.some((item) => /上下文/.test(item.label)))
  assert.ok(viewModel.selection.some((item) => /并发|批次/.test(item.label)))
  assert.ok(viewModel.selection.some((item) => /并行|GPU/.test(item.label)))
})
