function asRecord(value) {
  return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
}

function firstString(...values) {
  for (const value of values) {
    if (value !== undefined && value !== null && String(value).trim()) {
      return String(value).trim()
    }
  }
  return ''
}

function compactJoin(values, separator = ' · ') {
  return values.map((item) => String(item || '').trim()).filter(Boolean).join(separator)
}

function prettyJSON(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return ''
  if (Object.keys(value).length === 0) return ''
  return JSON.stringify(value, null, 2)
}

export function actionTitle(action = {}) {
  const display = asRecord(action.display_json)
  return firstString(
    display.title,
    display.name,
    action.capability_id,
    action.action_kind,
    '动作决策'
  )
}

export function actionSummary(action = {}) {
  const display = asRecord(action.display_json)
  const policy = asRecord(action.policy_json)
  return firstString(
    display.summary,
    policy.risk_summary,
    '需要决策后继续执行'
  )
}

export function actionMeta(action = {}) {
  const target = asRecord(action.target_json)
  const policy = asRecord(action.policy_json)
  const operation = firstString(policy.operation_type, target.operation_type)
  const targetName = compactJoin([
    firstString(target.target_type, target.type),
    firstString(target.target_id, target.id)
  ], ' ')
  return firstString(
    compactJoin([operation, targetName]),
    action.action_kind,
    action.capability_id
  )
}

export function actionInputPreview(action = {}) {
  const display = asRecord(action.display_json)
  const input = asRecord(action.input_json)
  const displayPreview = display.input_preview
  if (typeof displayPreview === 'string' && displayPreview.trim()) return displayPreview.trim()
  if (displayPreview && typeof displayPreview === 'object') return JSON.stringify(displayPreview, null, 2)
  return prettyJSON(asRecord(input.arguments))
}
