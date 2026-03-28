import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildStatisticsDateParams,
  formatLocalCalendarDate,
  getDefaultStatisticsDateRange
} from './dateRange.js'

test('formatLocalCalendarDate uses local calendar fields instead of UTC serialization', () => {
  const date = new Date(2026, 2, 27, 0, 30, 0)
  const utcSensitiveValue = date.toISOString().split('T')[0]

  assert.equal(formatLocalCalendarDate(date), '2026-03-27')
  assert.notEqual(formatLocalCalendarDate(date), utcSensitiveValue)
})

test('buildStatisticsDateParams returns shared start_date and end_date for a complete range', () => {
  const params = buildStatisticsDateParams([
    new Date(2026, 2, 20, 10, 30, 0),
    new Date(2026, 2, 26, 22, 15, 0)
  ])

  assert.deepEqual(params, {
    start_date: '2026-03-20',
    end_date: '2026-03-26'
  })
})

test('buildStatisticsDateParams returns an empty object for incomplete or invalid ranges', () => {
  assert.deepEqual(buildStatisticsDateParams([]), {})
  assert.deepEqual(buildStatisticsDateParams([new Date(2026, 2, 20, 10, 30, 0)]), {})
  assert.deepEqual(buildStatisticsDateParams([new Date('invalid'), new Date(2026, 2, 26, 22, 15, 0)]), {})
  assert.deepEqual(
    buildStatisticsDateParams([
      new Date(2026, 2, 26, 22, 15, 0),
      new Date(2026, 2, 20, 10, 30, 0)
    ]),
    {}
  )
})

test('getDefaultStatisticsDateRange returns a true inclusive 7-day window ending today', () => {
  const now = new Date(2026, 2, 27, 15, 45, 30)
  const [start, end] = getDefaultStatisticsDateRange(now)

  assert.equal(formatLocalCalendarDate(start), '2026-03-21')
  assert.equal(formatLocalCalendarDate(end), '2026-03-27')
  assert.equal(start.getHours(), 0)
  assert.equal(start.getMinutes(), 0)
  assert.equal(start.getSeconds(), 0)
  assert.equal(start.getMilliseconds(), 0)
  assert.equal(end.getHours(), 0)
  assert.equal(end.getMinutes(), 0)
  assert.equal(end.getSeconds(), 0)
  assert.equal(end.getMilliseconds(), 0)
})
