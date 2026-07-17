export function runtimeQueueModeLabel(mode) {
  if (mode === 'steer') return '注入'
  if (mode === 'stop_and_run') return '停止重发'
  return '跟进'
}

export function runtimeQueueStatusLabel(status) {
  if (status === 'applied') return '已应用'
  if (status === 'consumed') return '已消费'
  if (status === 'claimed') return '处理中'
  if (status === 'cancelled') return '已取消'
  if (status === 'expired') return '已过期'
  if (status === 'failed') return '失败'
  return '排队中'
}

export function runtimeQueueStatusType(status) {
  if (status === 'applied' || status === 'consumed') return 'success'
  if (status === 'failed' || status === 'expired') return 'danger'
  if (status === 'claimed') return 'warning'
  return 'info'
}
