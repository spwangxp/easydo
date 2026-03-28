const MS_PER_DAY = 24 * 60 * 60 * 1000

function normalizeToLocalMidnight(value) {
  const date = value instanceof Date ? new Date(value) : new Date(value)
  if (Number.isNaN(date.getTime())) {
    return null
  }

  date.setHours(0, 0, 0, 0)
  return date
}

export function formatLocalCalendarDate(value) {
  const date = normalizeToLocalMidnight(value)
  if (!date) {
    return ''
  }

  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')

  return `${year}-${month}-${day}`
}

export function buildStatisticsDateParams(dateRange) {
  if (!Array.isArray(dateRange) || dateRange.length < 2) {
    return {}
  }

  const start = normalizeToLocalMidnight(dateRange[0])
  const end = normalizeToLocalMidnight(dateRange[1])

  if (!start || !end) {
    return {}
  }

  if (end.getTime() < start.getTime()) {
    return {}
  }

  return {
    start_date: formatLocalCalendarDate(start),
    end_date: formatLocalCalendarDate(end)
  }
}

export function getDefaultStatisticsDateRange(value = new Date()) {
  const end = normalizeToLocalMidnight(value)
  if (!end) {
    return []
  }

  const start = new Date(end)
  start.setTime(start.getTime() - 6 * MS_PER_DAY)

  return [start, end]
}
