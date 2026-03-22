import assert from 'node:assert/strict'

import { parseKubectlListOutput } from './utils.js'

const noisyStdout = `[easydo][step] 执行 Kubernetes 任务
[easydo][info] auth_mode=kubeconfig
[easydo][cmd] kubectl get namespaces -o json
{
  "items": [
    {
      "kind": "Namespace",
      "metadata": {
        "name": "default"
      }
    },
    {
      "kind": "Namespace",
      "metadata": {
        "name": "qa-k8s"
      }
    }
  ]
}`

assert.deepEqual(
  parseKubectlListOutput(noisyStdout).map(item => item?.metadata?.name),
  ['default', 'qa-k8s']
)

console.log('k8s utils tests passed')
