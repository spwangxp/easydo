export function getVisibleCredentialSlots(taskType, slots) {
  if (!Array.isArray(slots)) {
    return []
  }
  if (String(taskType || '').trim().toLowerCase() !== 'docker-run') {
    return slots
  }
  return slots.filter(slot => slot?.slot !== 'ssh_auth')
}
