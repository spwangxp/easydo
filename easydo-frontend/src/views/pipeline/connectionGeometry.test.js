import test from 'node:test'
import assert from 'node:assert/strict'
import { getConnectionAnchors, buildConnectionPath } from './connectionGeometry.js'

const createNodes = (fromNode, toNode) => [
  {
    id: 'from',
    x: 100,
    y: 100,
    width: 220,
    height: 90,
    ...fromNode
  },
  {
    id: 'to',
    x: 420,
    y: 120,
    width: 220,
    height: 90,
    ...toNode
  }
]

test('getConnectionAnchors uses visible border midpoints for mostly horizontal nodes', () => {
  const conn = { id: 'c1', from: 'from', to: 'to' }
  const nodes = createNodes({ width: 200 }, { width: 200 })
  const anchors = getConnectionAnchors({ conn, nodes, connections: [conn] })

  assert.deepEqual(anchors, {
    from: { x: 319.5, y: 145, side: 'right' },
    to: { x: 420.5, y: 165, side: 'left' }
  })
})

test('getConnectionAnchors keeps outgoing endpoints on the source side midpoint even with multiple connections', () => {
  const conn1 = { id: 'c1', from: 'from', to: 'to' }
  const conn2 = { id: 'c2', from: 'from', to: 'to2' }
  const nodes = [
    {
      id: 'from',
      x: 100,
      y: 100,
      width: 200,
      height: 90
    },
    {
      id: 'to',
      x: 420,
      y: 120,
      width: 200,
      height: 90
    },
    {
      id: 'to2',
      x: 420,
      y: 260,
      width: 200,
      height: 90
    }
  ]

  const anchors1 = getConnectionAnchors({ conn: conn1, nodes, connections: [conn1, conn2] })
  const anchors2 = getConnectionAnchors({ conn: conn2, nodes, connections: [conn1, conn2] })

  assert.deepEqual(anchors1.from, { x: 319.5, y: 145, side: 'right' })
  assert.deepEqual(anchors2.from, { x: 319.5, y: 145, side: 'right' })
})

test('getConnectionAnchors uses top and bottom anchors for mostly vertical nodes', () => {
  const conn = { id: 'c1', from: 'from', to: 'to' }
  const nodes = createNodes({}, { x: 110, y: 320 })
  const anchors = getConnectionAnchors({ conn, nodes, connections: [conn] })

  assert.deepEqual(anchors, {
    from: { x: 210, y: 189.5, side: 'bottom' },
    to: { x: 220, y: 320.5, side: 'top' }
  })
})

test('buildConnectionPath keeps smooth bezier curve while using top and bottom anchors for stacked nodes', () => {
  const conn = { id: 'c1', from: 'from', to: 'to' }
  const nodes = createNodes({}, { x: 110, y: 320 })
  const path = buildConnectionPath({ conn, nodes, connections: [conn] })

  assert.match(path, /^M 210 189\.5 C 210 /)
  assert.match(path, /, 220 \d+(?:\.\d+)?, 220 320\.5$/)
  assert.doesNotMatch(path, / Q /)
})

test('buildConnectionPath keeps smooth bezier curve while using mixed-side anchors for close diagonal nodes', () => {
  const conn = { id: 'c1', from: 'from', to: 'to' }
  const nodes = createNodes({}, { x: 280, y: 210 })
  const path = buildConnectionPath({ conn, nodes, connections: [conn] })

  assert.match(path, /^M 319\.5 145 C /)
  assert.match(path, /390 210\.5$/)
  assert.doesNotMatch(path, / Q /)
})
