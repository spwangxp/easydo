const DEFAULT_NODE_WIDTH = 220
const DEFAULT_NODE_HEIGHT = 90
const MIN_NODE_HEIGHT = 90
const SIDE_ANCHOR_GAP = 0
const MIN_VERTICAL_SPACING = 22
const MIN_HORIZONTAL_SPACING = 22
const BORDER_MIDPOINT_OFFSET = 0.5

const ANCHOR_VECTORS = {
  left: { x: -1, y: 0 },
  right: { x: 1, y: 0 },
  top: { x: 0, y: -1 },
  bottom: { x: 0, y: 1 }
}

const getNodeWidth = (node) => Math.max(node?.width || DEFAULT_NODE_WIDTH, DEFAULT_NODE_WIDTH)
const getNodeHeight = (node) => Math.max(node?.height || DEFAULT_NODE_HEIGHT, MIN_NODE_HEIGHT)

const getAnchorBasePoint = (node, side) => {
  const width = getNodeWidth(node)
  const height = getNodeHeight(node)

  switch (side) {
    case 'left':
      return { x: node.x + BORDER_MIDPOINT_OFFSET - SIDE_ANCHOR_GAP, y: node.y + height / 2 }
    case 'right':
      return { x: node.x + width - BORDER_MIDPOINT_OFFSET + SIDE_ANCHOR_GAP, y: node.y + height / 2 }
    case 'top':
      return { x: node.x + width / 2, y: node.y + BORDER_MIDPOINT_OFFSET - SIDE_ANCHOR_GAP }
    case 'bottom':
      return { x: node.x + width / 2, y: node.y + height - BORDER_MIDPOINT_OFFSET + SIDE_ANCHOR_GAP }
    default:
      return { x: node.x + width / 2, y: node.y + height / 2 }
  }
}

const getPointDistance = (from, to) => Math.hypot(to.x - from.x, to.y - from.y)

const getFacingScore = (fromPoint, fromSide, toPoint, toSide) => {
  const sourceVector = ANCHOR_VECTORS[fromSide]
  const targetVector = ANCHOR_VECTORS[toSide]
  const forward = {
    x: toPoint.x - fromPoint.x,
    y: toPoint.y - fromPoint.y
  }
  const backward = {
    x: fromPoint.x - toPoint.x,
    y: fromPoint.y - toPoint.y
  }

  const sourceFacing = forward.x * sourceVector.x + forward.y * sourceVector.y
  const targetFacing = backward.x * targetVector.x + backward.y * targetVector.y

  let penalty = 0
  if (sourceFacing <= 0) {
    penalty += 100000 + Math.abs(sourceFacing) * 100
  }
  if (targetFacing <= 0) {
    penalty += 100000 + Math.abs(targetFacing) * 100
  }
  if (fromSide === toSide) {
    penalty += 5000
  }

  return penalty
}

const selectAnchorSides = ({ fromNode, toNode }) => {
  const sides = ['left', 'right', 'top', 'bottom']
  let best = { fromSide: 'right', toSide: 'left', score: Number.POSITIVE_INFINITY }

  sides.forEach((fromSide) => {
    sides.forEach((toSide) => {
      const fromPoint = getAnchorBasePoint(fromNode, fromSide)
      const toPoint = getAnchorBasePoint(toNode, toSide)
      const score = getPointDistance(fromPoint, toPoint) + getFacingScore(fromPoint, fromSide, toPoint, toSide)

      if (score < best.score) {
        best = { fromSide, toSide, score }
      }
    })
  })

  return best
}

const sortConnectionsByOtherNodeAxis = ({ items, isTarget, nodes, side }) => {
  const primaryAxis = side === 'left' || side === 'right' ? 'y' : 'x'
  const secondaryAxis = primaryAxis === 'y' ? 'x' : 'y'

  return [...items].sort((leftConn, rightConn) => {
    const leftNode = nodes.find(node => node.id === (isTarget ? leftConn.from : leftConn.to))
    const rightNode = nodes.find(node => node.id === (isTarget ? rightConn.from : rightConn.to))

    const leftPrimary = leftNode?.[primaryAxis] || 0
    const rightPrimary = rightNode?.[primaryAxis] || 0
    if (leftPrimary !== rightPrimary) {
      return leftPrimary - rightPrimary
    }

    const leftSecondary = leftNode?.[secondaryAxis] || 0
    const rightSecondary = rightNode?.[secondaryAxis] || 0
    return leftSecondary - rightSecondary
  })
}

const getAnchorSpreadOffset = ({ node, side }) => {
  if (!node) {
    return 0
  }

  if (side === 'left' || side === 'right') {
    return 0
  }

  return 0
}

const getAnchorPoint = ({ conn, node, connections, isTarget, nodes, side }) => {
  const basePoint = getAnchorBasePoint(node, side)
  const spreadOffset = getAnchorSpreadOffset({ conn, node, connections, isTarget, nodes, side })

  if (side === 'left' || side === 'right') {
    return {
      x: basePoint.x,
      y: basePoint.y + spreadOffset,
      side
    }
  }

  return {
    x: basePoint.x + spreadOffset,
    y: basePoint.y,
    side
  }
}

export const getConnectionAnchors = ({ conn, nodes, connections }) => {
  const fromNode = nodes.find(node => node.id === conn.from)
  const toNode = nodes.find(node => node.id === conn.to)

  if (!fromNode || !toNode) {
    return null
  }

  const { fromSide, toSide } = selectAnchorSides({ fromNode, toNode })

  return {
    from: getAnchorPoint({ conn, node: fromNode, connections, isTarget: false, nodes, side: fromSide }),
    to: getAnchorPoint({ conn, node: toNode, connections, isTarget: true, nodes, side: toSide })
  }
}

const getControlPoint = (anchor, distance) => {
  const vector = ANCHOR_VECTORS[anchor.side] || { x: 0, y: 0 }
  return {
    x: anchor.x + vector.x * distance,
    y: anchor.y + vector.y * distance
  }
}

export const buildConnectionPath = ({ conn, nodes, connections }) => {
  const anchors = getConnectionAnchors({ conn, nodes, connections })
  if (!anchors) return ''

  const { from, to } = anchors
  const distance = getPointDistance(from, to)
  const controlDistance = Math.max(48, Math.min(distance * 0.45, 180))
  const c1 = getControlPoint(from, controlDistance)
  const c2 = getControlPoint(to, controlDistance)

  return `M ${from.x} ${from.y} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${to.x} ${to.y}`
}
