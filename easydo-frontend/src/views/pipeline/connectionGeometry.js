const DEFAULT_NODE_WIDTH = 220
const DEFAULT_NODE_HEIGHT = 120
const MIN_NODE_HEIGHT = 90
const SIDE_ANCHOR_GAP = 18
const MIN_VERTICAL_SPACING = 22
const DEFAULT_VERTICAL_RATIO = 0.5

const getNodeWidth = (node) => node?.width || DEFAULT_NODE_WIDTH
const getNodeHeight = (node) => Math.max(node?.height || DEFAULT_NODE_HEIGHT, MIN_NODE_HEIGHT)

const sortConnectionsByOtherNodeY = ({ items, isTarget, nodes }) => {
  return [...items].sort((leftConn, rightConn) => {
    const leftNode = nodes.find(node => node.id === (isTarget ? leftConn.from : leftConn.to))
    const rightNode = nodes.find(node => node.id === (isTarget ? rightConn.from : rightConn.to))

    const leftY = leftNode?.y || 0
    const rightY = rightNode?.y || 0

    if (leftY !== rightY) {
      return leftY - rightY
    }

    const leftX = leftNode?.x || 0
    const rightX = rightNode?.x || 0
    return leftX - rightX
  })
}

export const getSideConnectionOffset = ({ conn, node, connections, isTarget, nodes }) => {
  if (!node) {
    return {
      x: isTarget ? -SIDE_ANCHOR_GAP : SIDE_ANCHOR_GAP,
      y: DEFAULT_NODE_HEIGHT * DEFAULT_VERTICAL_RATIO,
      index: 0,
      total: 1
    }
  }

  const relatedConnections = sortConnectionsByOtherNodeY({
    items: connections.filter(item => (isTarget ? item.to === node.id : item.from === node.id)),
    isTarget,
    nodes
  })

  const index = Math.max(relatedConnections.findIndex(item => item.id === conn.id), 0)
  const total = Math.max(relatedConnections.length, 1)
  const nodeHeight = getNodeHeight(node)
  const availableHeight = Math.max(nodeHeight - MIN_VERTICAL_SPACING * 2, MIN_VERTICAL_SPACING)
  const spacing = total === 1 ? 0 : Math.max(availableHeight / (total - 1), MIN_VERTICAL_SPACING)
  const occupiedHeight = spacing * Math.max(total - 1, 0)
  const startY = (nodeHeight - occupiedHeight) / 2

  return {
    x: isTarget ? -SIDE_ANCHOR_GAP : getNodeWidth(node) + SIDE_ANCHOR_GAP,
    y: startY + spacing * index,
    index,
    total
  }
}

export const getConnectionAnchors = ({ conn, nodes, connections }) => {
  const fromNode = nodes.find(node => node.id === conn.from)
  const toNode = nodes.find(node => node.id === conn.to)

  if (!fromNode || !toNode) {
    return null
  }

  const fromOffset = getSideConnectionOffset({
    conn,
    node: fromNode,
    connections,
    isTarget: false,
    nodes
  })

  const toOffset = getSideConnectionOffset({
    conn,
    node: toNode,
    connections,
    isTarget: true,
    nodes
  })

  return {
    from: {
      x: fromNode.x + fromOffset.x,
      y: fromNode.y + fromOffset.y
    },
    to: {
      x: toNode.x + toOffset.x,
      y: toNode.y + toOffset.y
    }
  }
}

export const buildConnectionPath = ({ conn, nodes, connections }) => {
  const anchors = getConnectionAnchors({ conn, nodes, connections })
  if (!anchors) return ''

  const { from, to } = anchors
  const dx = to.x - from.x
  const absDx = Math.abs(dx)
  const dy = to.y - from.y
  const absDy = Math.abs(dy)

  const controlOffsetX = Math.max(48, Math.min(absDx * 0.45 || 0, 180))
  const controlOffsetY = absDx < 120 ? Math.min(absDy * 0.15, 24) : 0

  const c1x = from.x + controlOffsetX
  const c1y = from.y + (dy > 0 ? controlOffsetY : -controlOffsetY)
  const c2x = to.x - controlOffsetX
  const c2y = to.y - (dy > 0 ? controlOffsetY : -controlOffsetY)

  return `M ${from.x} ${from.y} C ${c1x} ${c1y}, ${c2x} ${c2y}, ${to.x} ${to.y}`
}
