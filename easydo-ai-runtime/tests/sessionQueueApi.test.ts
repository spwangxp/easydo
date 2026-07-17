import { describe, expect, it } from 'vitest'
import { createApp } from '../src/server.js'
import { createMemoryRuntimeStore } from '../src/store/memoryRuntimeStore.js'

let requestSeq = 0

function request(method: string, payload: Record<string, unknown>) {
  requestSeq += 1
  return {
    method,
    headers: {
      'content-type': 'application/json',
      'x-internal-token': 'secret'
    },
    body: JSON.stringify({
      request_id: `queue-request-${requestSeq}`,
      idempotency_key: `queue-envelope-${requestSeq}`,
      actor: {
        user_id: 12,
        username: 'demo',
        system_role: 'user',
        workspace_id: 3,
        workspace_role: 'owner',
        auth_session_id: 'auth-session-queue'
      },
      auth: {
        server_internal_token: 'secret',
        delegated_user_token: 'delegated-user-token'
      },
      payload
    })
  }
}

describe('session queue internal API', () => {
  it('enqueues, lists, reorders, and cancels durable queue items', async () => {
    const app = createApp({ internalToken: 'secret', autoDrainSessionQueue: false })
    const profileResponse = await app.request('/v1/agent-profiles', request('POST', {
      name: 'Queue API Agent',
      provider: { provider_id: 'test' },
      model: { provider_model_key: 'test/model' }
    }))
    const profile = (await profileResponse.json()).data
    const sessionResponse = await app.request('/v1/sessions/current', request('POST', {
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: `${profile.id}:draft`,
      title: 'Queue API',
      profile_selection: {
        agent_profile_id: profile.id,
        agent_profile_version_id: 'draft',
        first_session_timestamp: '2026-07-14T00:00:00.000Z'
      }
    }))
    const session = (await sessionResponse.json()).data

    const firstResponse = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
      mode: 'follow_up',
      content: 'First',
      client_item_id: 'api-queue-first'
    }))
    const secondResponse = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
      mode: 'stop_and_run',
      content: 'Second',
      client_item_id: 'api-queue-second'
    }))
    expect(firstResponse.status).toBe(200)
    expect(secondResponse.status).toBe(200)
    const first = (await firstResponse.json()).data.item
    const second = (await secondResponse.json()).data.item

    const steerRejected = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
      mode: 'steer',
      content: 'needs active run',
      client_item_id: 'api-queue-steer'
    }))
    expect(steerRejected.status).toBe(409)

    const reorderResponse = await app.request(`/v1/sessions/${session.id}/queue/reorder`, request('POST', {
      queue_item_ids: [second.queue_item_id, first.queue_item_id]
    }))
    expect(reorderResponse.status).toBe(200)
    expect((await reorderResponse.json()).data.items.map((item: { content: string }) => item.content)).toEqual(['Second', 'First'])

    const cancelResponse = await app.request(
      `/v1/sessions/${session.id}/queue/items/${first.queue_item_id}/cancel`,
      request('POST', {})
    )
    expect(cancelResponse.status).toBe(200)
    expect((await cancelResponse.json()).data.item.status).toBe('cancelled')

    const listResponse = await app.request(`/v1/sessions/${session.id}/queue/query`, request('POST', {}))
    expect(listResponse.status).toBe(200)
    const listed = (await listResponse.json()).data
    expect(listed.pending_count).toBe(1)
    expect(listed.items).toHaveLength(2)

    const processResponse = await app.request(`/v1/sessions/${session.id}/queue/process`, request('POST', {}))
    expect(processResponse.status).toBe(404)
  })

  it('returns the P1-11 public queue error codes', async () => {
    const store = createMemoryRuntimeStore()
    const app = createApp({ internalToken: 'secret', autoDrainSessionQueue: false, store })
    const profileResponse = await app.request('/v1/agent-profiles', request('POST', {
      name: 'Queue Error Agent',
      provider: { provider_id: 'test' },
      model: { provider_model_key: 'test/model' }
    }))
    const profile = (await profileResponse.json()).data
    const sessionResponse = await app.request('/v1/sessions/current', request('POST', {
      session_kind: 'chat',
      business_type: 'agent_profile',
      business_id: `${profile.id}:draft`,
      title: 'Queue Errors',
      profile_selection: {
        agent_profile_id: profile.id,
        agent_profile_version_id: 'draft',
        first_session_timestamp: '2026-07-14T00:00:00.000Z'
      }
    }))
    const session = (await sessionResponse.json()).data

    const invalidMode = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
      mode: 'unsupported', content: 'invalid', client_item_id: 'api-invalid-mode'
    }))
    expect(invalidMode.status).toBe(400)
    expect((await invalidMode.json()).code).toBe('queue_mode_invalid')

    const missing = await app.request(`/v1/sessions/${session.id}/queue/items/missing/cancel`, request('POST', {}))
    expect(missing.status).toBe(404)
    expect((await missing.json()).code).toBe('queue_item_not_found')

    const queuedResponse = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
      mode: 'follow_up', content: 'claim before cancel', client_item_id: 'api-claimed-item'
    }))
    const queued = (await queuedResponse.json()).data.item
    await store.claimNextSessionQueueItem({
      workspace_id: 3,
      session_id: session.id,
      claimed_by: 'api-test-runtime',
      claim_expires_at: '2099-01-01T00:00:00.000Z',
      updated_at: '2026-07-14T00:00:00.000Z',
      queue_item_id: queued.queue_item_id
    })
    const notCancellable = await app.request(`/v1/sessions/${session.id}/queue/items/${queued.queue_item_id}/cancel`, request('POST', {}))
    expect(notCancellable.status).toBe(409)
    expect((await notCancellable.json()).code).toBe('queue_item_not_cancellable')

    for (let index = 1; index <= 49; index += 1) {
      const response = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
        mode: 'follow_up', content: `Queue ${index}`, client_item_id: `api-capacity-${index}`
      }))
      expect(response.status).toBe(200)
    }
    const full = await app.request(`/v1/sessions/${session.id}/queue/items`, request('POST', {
      mode: 'follow_up', content: 'over capacity', client_item_id: 'api-capacity-full'
    }))
    expect(full.status).toBe(409)
    expect((await full.json()).code).toBe('session_queue_full')
  })
})
