import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildConnectionPath,
  getConnectionAnchors,
  getSideConnectionOffset
} from '../src/views/pipeline/connectionGeometry.js'

const sourceNode = { id: 'source', x: 100, y: 120, width: 220, height: 120 }
const middleNode = { id: 'middle', x: 480, y: 140, width: 220, height: 120 }
const targetNode = { id: 'target', x: 860, y: 160, width: 220, height: 120 }

test('target anchor stays outside target node so arrowhead remains visible', () => {
  const conn = { id: 'c1', from: sourceNode.id, to: targetNode.id }
  const anchors = getConnectionAnchors({
    conn,
    nodes: [sourceNode, targetNode],
    connections: [conn]
  })

  assert.ok(anchors.from.x > sourceNode.x + sourceNode.width)
  assert.ok(anchors.to.x < targetNode.x)
  assert.equal(anchors.from.y, sourceNode.y + sourceNode.height / 2)
  assert.equal(anchors.to.y, targetNode.y + targetNode.height / 2)
})

test('multiple outgoing connections are distributed vertically on the source side', () => {
  const first = { id: 'c1', from: sourceNode.id, to: middleNode.id }
  const second = { id: 'c2', from: sourceNode.id, to: targetNode.id }
  const connections = [first, second]

  const firstOffset = getSideConnectionOffset({
    conn: first,
    node: sourceNode,
    connections,
    isTarget: false,
    nodes: [sourceNode, middleNode, targetNode]
  })

  const secondOffset = getSideConnectionOffset({
    conn: second,
    node: sourceNode,
    connections,
    isTarget: false,
    nodes: [sourceNode, middleNode, targetNode]
  })

  assert.notEqual(firstOffset.y, secondOffset.y)
  assert.ok(firstOffset.y > 0)
  assert.ok(secondOffset.y < sourceNode.height)
})

test('connection path ends at the outside target anchor instead of the node body', () => {
  const conn = { id: 'c1', from: sourceNode.id, to: targetNode.id }
  const path = buildConnectionPath({
    conn,
    nodes: [sourceNode, targetNode],
    connections: [conn]
  })

  assert.match(path, /^M\s+\d+(?:\.\d+)?\s+\d+(?:\.\d+)?\s+C\s+/)
  assert.match(path, /842 220$/)
  assert.doesNotMatch(path, /860 220$/)
})
