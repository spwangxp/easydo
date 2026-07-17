import { ACTIVE_RUNTIME_RUN_STATUSES, agentProfileSnapshotHash, agentResourceSnapshotDigest, assertAgentResourceVersionMatchesResource, assertAgentResourceVersionSnapshotSafe, normalizeRuntimeRunActiveSlot, sameSessionQueueItemSet, sessionQueueItemBusinessID } from '../domain/runtime.js';
import { RuntimeDomainError } from '../services/runtimeErrors.js';
function sessionQueueLifecycleEvent(draft, item) {
    const marker = draft.type === 'session.queue.claimed' ? item.claim_epoch : draft.marker;
    const eventBase = {
        session_id: draft.session_id,
        event_id: [draft.session_id, draft.type, item.queue_item_id, marker]
            .map((part) => encodeURIComponent(String(part).trim() || 'event'))
            .join(':'),
        seq: 0,
        timestamp: item.updated_at,
        queue_item: structuredClone(item)
    };
    switch (draft.type) {
        case 'session.queue.added':
            return { ...eventBase, type: 'session.queue.added' };
        case 'session.queue.claimed':
            return { ...eventBase, type: 'session.queue.claimed' };
        case 'session.queue.cancelled':
            return { ...eventBase, type: 'session.queue.cancelled' };
        case 'session.queue.expired':
            return { ...eventBase, type: 'session.queue.expired' };
        case 'session.queue.failed':
            return { ...eventBase, type: 'session.queue.failed' };
        case 'session.steer.applied':
            return { ...eventBase, type: 'session.steer.applied', runtime_run_id: draft.runtime_run_id };
    }
}
export class MemoryRuntimeStore {
    profileSeq = 1;
    profileVersionSeq = 1;
    agentResourceSeq = 1;
    agentResourceVersionSeq = 1;
    sessionSeq = 1;
    entrySeq = 1;
    runSeq = 1;
    actionSeq = 1;
    agentWorkspaceSeq = 1;
    profiles = new Map();
    profileVersions = new Map();
    agentResources = new Map();
    agentResourceVersions = new Map();
    sessions = new Map();
    currentSessions = new Map();
    entries = new Map();
    sessionQueueItems = new Map();
    sessionQueueSeqs = new Map();
    runs = new Map();
    actions = new Map();
    actionBusinessIndex = new Map();
    runEventSeqs = new Map();
    runArtifactSeqs = new Map();
    actionChildSeqs = new Map();
    actionEvents = new Map();
    runtimeArtifacts = new Map();
    childRunLinks = new Map();
    orchestrationPlans = new Map();
    orchestrationContextPacks = new Map();
    orchestrationTasks = new Map();
    actionDecisionSeqs = new Map();
    sessionPermissionGrantSeqs = new Map();
    actionExecutionAttempts = new Map();
    actionDecisions = new Map();
    sessionPermissionGrants = new Map();
    actionExecutions = new Map();
    agentWorkspaces = new Map();
    agentWorkspaceAudits = new Map();
    async nextProfileId() {
        return this.profileSeq++;
    }
    async nextProfileVersionId() {
        return this.profileVersionSeq++;
    }
    async nextAgentResourceId() {
        return this.agentResourceSeq++;
    }
    async nextAgentResourceVersionId() {
        return this.agentResourceVersionSeq++;
    }
    async nextSessionId() {
        return this.sessionSeq++;
    }
    async nextEntryId() {
        return this.entrySeq++;
    }
    async nextRunId() {
        return this.runSeq++;
    }
    async nextActionId() {
        return this.actionSeq++;
    }
    async nextAgentWorkspaceId() {
        return this.agentWorkspaceSeq++;
    }
    async nextRunEventSeq(runtimeRunID) {
        const nextSeq = (this.runEventSeqs.get(runtimeRunID) || 0) + 1;
        this.runEventSeqs.set(runtimeRunID, nextSeq);
        return nextSeq;
    }
    async nextRunArtifactSeq(runtimeRunID) {
        const nextSeq = (this.runArtifactSeqs.get(runtimeRunID) || 0) + 1;
        this.runArtifactSeqs.set(runtimeRunID, nextSeq);
        return nextSeq;
    }
    async nextActionChildSeq(actionID) {
        const nextSeq = (this.actionChildSeqs.get(actionID) || 0) + 1;
        this.actionChildSeqs.set(actionID, nextSeq);
        return nextSeq;
    }
    async nextActionDecisionSeq(actionID) {
        const nextSeq = (this.actionDecisionSeqs.get(actionID) || 0) + 1;
        this.actionDecisionSeqs.set(actionID, nextSeq);
        return nextSeq;
    }
    async nextSessionPermissionGrantSeq(sessionID) {
        const nextSeq = (this.sessionPermissionGrantSeqs.get(sessionID) || 0) + 1;
        this.sessionPermissionGrantSeqs.set(sessionID, nextSeq);
        return nextSeq;
    }
    async nextActionExecutionAttempt(actionID) {
        const nextAttempt = (this.actionExecutionAttempts.get(actionID) || 0) + 1;
        this.actionExecutionAttempts.set(actionID, nextAttempt);
        return nextAttempt;
    }
    async enqueueSessionQueueItem(input, activeItemLimit) {
        const existing = [...this.sessionQueueItems.values()].find((item) => item.workspace_id === input.workspace_id &&
            item.session_id === input.session_id &&
            item.client_item_id === input.client_item_id);
        if (existing)
            return { outcome: 'existing', item: structuredClone(existing) };
        const activeItems = [...this.sessionQueueItems.values()].filter((item) => item.workspace_id === input.workspace_id &&
            item.session_id === input.session_id &&
            ['pending', 'claimed'].includes(item.status));
        if (activeItems.length >= activeItemLimit)
            return { outcome: 'capacity_exceeded' };
        const sequenceKey = `${input.workspace_id}:${input.session_id}`;
        const itemSeq = (this.sessionQueueSeqs.get(sequenceKey) || 0) + 1;
        this.sessionQueueSeqs.set(sequenceKey, itemSeq);
        const position = activeItems.reduce((maximum, item) => Math.max(maximum, item.position), 0) + 1;
        const item = {
            queue_item_id: sessionQueueItemBusinessID(input.workspace_id, input.session_id, itemSeq),
            workspace_id: input.workspace_id,
            session_id: input.session_id,
            item_seq: itemSeq,
            position,
            mode: input.mode,
            content: input.content,
            attachments: structuredClone(input.attachments),
            context_ref: structuredClone(input.context_ref),
            target_run_id: input.target_run_id,
            expires_at: input.expires_at,
            status: 'pending',
            client_item_id: input.client_item_id,
            claim_epoch: 0,
            created_by: input.created_by,
            created_at: input.created_at,
            updated_at: input.created_at
        };
        this.sessionQueueItems.set(`${item.workspace_id}:${item.queue_item_id}`, structuredClone(item));
        return input.lifecycle_event
            ? {
                outcome: 'created',
                item: structuredClone(item),
                event: sessionQueueLifecycleEvent(input.lifecycle_event, item),
                event_committed: false
            }
            : { outcome: 'created', item: structuredClone(item) };
    }
    async listSessionQueueItems(workspaceID, sessionID) {
        return [...this.sessionQueueItems.values()]
            .filter((item) => item.workspace_id === workspaceID && item.session_id === sessionID)
            .sort(sessionQueueItemOrder)
            .map((item) => structuredClone(item));
    }
    async cancelSessionQueueItem(input) {
        const key = `${input.workspace_id}:${input.queue_item_id}`;
        const item = this.sessionQueueItems.get(key);
        if (!item || item.session_id !== input.session_id)
            return { outcome: 'not_found' };
        if (item.status === 'cancelled')
            return { outcome: 'already_cancelled', item: structuredClone(item) };
        if (item.status !== 'pending')
            return { outcome: 'not_cancellable' };
        const cancelled = { ...item, status: 'cancelled', updated_at: input.updated_at };
        this.sessionQueueItems.set(key, cancelled);
        return input.lifecycle_event
            ? {
                outcome: 'cancelled',
                item: structuredClone(cancelled),
                event: sessionQueueLifecycleEvent(input.lifecycle_event, cancelled),
                event_committed: false
            }
            : { outcome: 'cancelled', item: structuredClone(cancelled) };
    }
    async reorderSessionQueueItems(workspaceID, sessionID, queueItemIDs, updatedAt) {
        const pending = [...this.sessionQueueItems.values()]
            .filter((item) => item.workspace_id === workspaceID && item.session_id === sessionID && item.status === 'pending');
        if (!sameSessionQueueItemSet(pending.map((item) => item.queue_item_id), queueItemIDs))
            return { outcome: 'conflict' };
        const byID = new Map(pending.map((item) => [item.queue_item_id, item]));
        const reordered = queueItemIDs.map((queueItemID, index) => ({
            ...byID.get(queueItemID),
            position: index + 1,
            updated_at: updatedAt
        }));
        for (const item of reordered)
            this.sessionQueueItems.set(`${workspaceID}:${item.queue_item_id}`, item);
        return { outcome: 'reordered', items: reordered.map((item) => structuredClone(item)) };
    }
    async claimNextSessionQueueItem(input) {
        const claimComparisonTime = Date.parse(input.updated_at);
        const candidates = [...this.sessionQueueItems.values()]
            .filter((item) => item.workspace_id === input.workspace_id &&
            item.session_id === input.session_id &&
            (item.status === 'pending' ||
                (item.status === 'claimed' &&
                    Boolean(item.claim_expires_at) &&
                    Date.parse(item.claim_expires_at || '') <= claimComparisonTime)) &&
            (!input.queue_item_id || item.queue_item_id === input.queue_item_id))
            .sort(sessionQueueItemOrder);
        const next = candidates[0];
        if (!next)
            return { outcome: 'empty' };
        const claimed = {
            ...next,
            status: 'claimed',
            claimed_by: input.claimed_by,
            claim_epoch: Number(next.claim_epoch || 0) + 1,
            claim_expires_at: input.claim_expires_at,
            updated_at: input.updated_at
        };
        this.sessionQueueItems.set(`${claimed.workspace_id}:${claimed.queue_item_id}`, claimed);
        return input.lifecycle_event
            ? {
                outcome: 'claimed',
                item: structuredClone(claimed),
                event: sessionQueueLifecycleEvent(input.lifecycle_event, claimed),
                event_committed: false
            }
            : { outcome: 'claimed', item: structuredClone(claimed) };
    }
    async updateSessionQueueItem(item) {
        const key = `${item.workspace_id}:${item.queue_item_id}`;
        const existing = this.sessionQueueItems.get(key);
        if (!existing || existing.session_id !== item.session_id) {
            throw new RuntimeDomainError('queue_item_not_found', 'Session queue item not found', 404);
        }
        const stored = structuredClone(item);
        this.sessionQueueItems.set(key, stored);
        return structuredClone(stored);
    }
    async applySessionQueueSteer(input) {
        const item = this.sessionQueueItems.get(`${input.workspace_id}:${input.queue_item_id}`);
        const run = this.runs.get(input.runtime_run_id);
        if (!item || item.session_id !== input.session_id)
            return { outcome: 'stale_claim' };
        if (!run || run.workspace_id !== input.workspace_id || run.session_id !== input.session_id || !isActiveRunStatus(run.status)) {
            return { outcome: 'target_not_active' };
        }
        if (item.status === 'applied')
            return { outcome: 'already_applied', item: structuredClone(item), run: structuredClone(run) };
        if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
            return { outcome: 'stale_claim' };
        }
        const updatedRun = { ...run, result: structuredClone(input.run_result), updated_at: input.updated_at };
        const applied = { ...item, status: 'applied', updated_at: input.updated_at };
        this.runs.set(updatedRun.runtime_run_id, updatedRun);
        this.sessionQueueItems.set(`${applied.workspace_id}:${applied.queue_item_id}`, applied);
        return input.lifecycle_event
            ? {
                outcome: 'applied',
                item: structuredClone(applied),
                run: structuredClone(updatedRun),
                event: sessionQueueLifecycleEvent(input.lifecycle_event, applied),
                event_committed: false
            }
            : { outcome: 'applied', item: structuredClone(applied), run: structuredClone(updatedRun) };
    }
    async transitionSessionQueueItem(input) {
        const key = `${input.item.workspace_id}:${input.item.queue_item_id}`;
        const current = this.sessionQueueItems.get(key);
        if (!current ||
            current.session_id !== input.item.session_id ||
            current.status !== 'claimed' ||
            current.claimed_by !== input.claimed_by ||
            current.claim_epoch !== input.claim_epoch)
            return { outcome: 'stale_claim' };
        const stored = structuredClone(input.item);
        this.sessionQueueItems.set(key, stored);
        return input.lifecycle_event
            ? {
                outcome: 'transitioned',
                item: structuredClone(stored),
                event: sessionQueueLifecycleEvent(input.lifecycle_event, stored),
                event_committed: false
            }
            : { outcome: 'transitioned', item: structuredClone(stored) };
    }
    async consumeSessionQueueItem(input) {
        const item = this.sessionQueueItems.get(`${input.workspace_id}:${input.queue_item_id}`);
        const run = this.runs.get(input.consumed_runtime_run_id);
        if (!item || item.session_id !== input.session_id || !run || run.workspace_id !== input.workspace_id || run.session_id !== input.session_id) {
            return { outcome: 'stale_claim' };
        }
        if (item.status === 'consumed')
            return { outcome: 'already_consumed', item: structuredClone(item), run: structuredClone(run) };
        if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
            return { outcome: 'stale_claim' };
        }
        const activeConflict = [...this.runs.values()].some((candidate) => candidate.workspace_id === input.workspace_id &&
            candidate.session_id === input.session_id &&
            candidate.runtime_run_id !== input.consumed_runtime_run_id &&
            isActiveRunStatus(candidate.status));
        if (activeConflict)
            return { outcome: 'active_run_conflict' };
        const consumed = {
            ...item,
            status: 'consumed',
            consumed_runtime_run_id: input.consumed_runtime_run_id,
            updated_at: input.updated_at
        };
        this.sessionQueueItems.set(`${consumed.workspace_id}:${consumed.queue_item_id}`, consumed);
        return { outcome: 'consumed', item: structuredClone(consumed), run: structuredClone(run) };
    }
    async consumeSessionQueueItemWithRun(input) {
        const itemKey = `${input.workspace_id}:${input.queue_item_id}`;
        const item = this.sessionQueueItems.get(itemKey);
        if (!item || item.session_id !== input.session_id)
            return { outcome: 'stale_claim' };
        if (item.status === 'consumed') {
            const existingRun = item.consumed_runtime_run_id ? this.runs.get(item.consumed_runtime_run_id) : undefined;
            return existingRun
                ? { outcome: 'already_consumed', item: structuredClone(item), run: structuredClone(existingRun) }
                : { outcome: 'stale_claim' };
        }
        if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
            return { outcome: 'stale_claim' };
        }
        const activeConflict = [...this.runs.values()].some((candidate) => candidate.workspace_id === input.workspace_id &&
            candidate.session_id === input.session_id &&
            candidate.runtime_run_id !== input.run.runtime_run_id &&
            isActiveRunStatus(candidate.status));
        if (activeConflict)
            return { outcome: 'active_run_conflict' };
        const consumed = {
            ...item,
            status: 'consumed',
            consumed_runtime_run_id: input.run.runtime_run_id,
            updated_at: input.follow_up_started_event.timestamp
        };
        const event = {
            ...input.follow_up_started_event,
            runtime_run_id: input.run.runtime_run_id,
            consumed_runtime_run_id: input.run.runtime_run_id,
            queue_item: consumed
        };
        const run = normalizeRuntimeRunActiveSlot(structuredClone(input.run));
        this.runs.set(run.runtime_run_id, run);
        this.entries.set(input.user_entry.id, structuredClone(input.user_entry));
        this.entries.set(input.assistant_entry.id, structuredClone(input.assistant_entry));
        this.sessions.set(input.updated_session_progress.id, structuredClone(input.updated_session_progress));
        this.sessionQueueItems.set(itemKey, consumed);
        return {
            outcome: 'consumed',
            item: structuredClone(consumed),
            run: structuredClone(run),
            user_entry: structuredClone(input.user_entry),
            assistant_entry: structuredClone(input.assistant_entry),
            follow_up_started_event: structuredClone(event),
            event_committed: false
        };
    }
    async saveProfile(profile) {
        const nameConflict = [...this.profiles.values()].find((candidate) => candidate.workspace_id === profile.workspace_id && candidate.name === profile.name && candidate.id !== profile.id);
        if (nameConflict)
            throw new RuntimeDomainError('agent_profile_name_exists', 'Agent profile name already exists', 409);
        const stored = structuredClone(profile);
        this.profiles.set(stored.id, stored);
        return structuredClone(stored);
    }
    async getProfile(workspaceID, profileID) {
        const profile = this.profiles.get(profileID);
        return profile && profile.workspace_id === workspaceID ? profile : undefined;
    }
    async listProfiles(workspaceID) {
        return [...this.profiles.values()]
            .filter((profile) => profile.workspace_id === workspaceID)
            .sort((left, right) => right.id - left.id);
    }
    async deleteProfileGuarded(workspaceID, profileID) {
        const profile = await this.getProfile(workspaceID, profileID);
        if (!profile)
            return 'not_found';
        const hasPublishedVersion = [...this.profileVersions.values()].some((version) => version.workspace_id === workspaceID && version.profile_id === profileID);
        const hasParentRef = [...this.profiles.values()].some((parent) => parent.workspace_id === workspaceID && parent.id !== profileID &&
            parent.subagents.some((ref) => ref.resource_type === 'subagent_profile' && Number(ref.resource_id) === profileID));
        const hasSession = [...this.sessions.values()].some((session) => session.workspace_id === workspaceID && session.agent_profile_id === profileID);
        if (hasPublishedVersion || hasParentRef || hasSession)
            return 'in_use';
        this.profiles.delete(profileID);
        return 'deleted';
    }
    async saveAgentResource(resource) {
        const existing = [...this.agentResources.values()].find((item) => item.workspace_id === resource.workspace_id &&
            item.resource_kind === resource.resource_kind &&
            item.resource_key === resource.resource_key &&
            item.id !== resource.id);
        if (existing) {
            this.agentResources.delete(existing.id);
        }
        this.agentResources.set(resource.id, resource);
        return resource;
    }
    assertAgentResourceIdentityAvailable(resource) {
        const conflict = [...this.agentResources.values()].find((item) => item.workspace_id === resource.workspace_id && item.resource_kind === resource.resource_kind &&
            item.resource_key === resource.resource_key && item.id !== resource.id);
        if (conflict)
            throw new RuntimeDomainError('agent_resource_key_exists', 'Agent resource key already exists', 409);
    }
    saveAgentResourceRevision(resource, version, create) {
        assertAgentResourceVersionMatchesResource(resource, version);
        this.assertAgentResourceIdentityAvailable(resource);
        const existingHead = this.agentResources.get(resource.id);
        if (create ? existingHead !== undefined : existingHead === undefined || existingHead.workspace_id !== resource.workspace_id) {
            throw new RuntimeDomainError(create ? 'agent_resource_key_exists' : 'agent_resource_not_found', create ? 'Agent resource already exists' : 'Agent resource not found', create ? 409 : 404);
        }
        const duplicateID = this.agentResourceVersions.has(version.resource_version_id);
        const duplicateRevision = [...this.agentResourceVersions.values()].some((item) => item.workspace_id === version.workspace_id &&
            item.resource_id === version.resource_id &&
            item.revision === version.revision);
        if (duplicateID || duplicateRevision) {
            throw new Error(`Agent resource revision ${version.resource_id}:${version.revision} already exists`);
        }
        const storedResource = structuredClone(resource);
        const storedVersion = structuredClone(version);
        this.agentResources.set(storedResource.id, storedResource);
        this.agentResourceVersions.set(storedVersion.resource_version_id, storedVersion);
        return {
            resource: structuredClone(storedResource),
            version: structuredClone(storedVersion)
        };
    }
    async createAgentResourceWithVersion(resource, version) {
        return this.saveAgentResourceRevision(resource, version, true);
    }
    async updateAgentResourceWithVersion(resource, version) {
        return this.saveAgentResourceRevision(resource, version, false);
    }
    async getAgentResource(workspaceID, resourceID) {
        const resource = this.agentResources.get(resourceID);
        return resource && resource.workspace_id === workspaceID ? resource : undefined;
    }
    async listAgentResources(workspaceID) {
        return [...this.agentResources.values()]
            .filter((resource) => resource.workspace_id === workspaceID)
            .sort((left, right) => right.updated_at.localeCompare(left.updated_at) || right.id - left.id);
    }
    async deleteAgentResource(workspaceID, resourceID) {
        const resource = await this.getAgentResource(workspaceID, resourceID);
        if (!resource)
            return false;
        return this.agentResources.delete(resourceID);
    }
    async deleteAgentResourceGuarded(workspaceID, resourceID) {
        const resource = await this.getAgentResource(workspaceID, resourceID);
        if (!resource)
            return 'not_found';
        const versionIDs = new Set([...this.agentResourceVersions.values()]
            .filter((version) => version.workspace_id === workspaceID && version.resource_id === resourceID)
            .map((version) => version.resource_version_id));
        const section = resource.resource_kind === 'skill' ? 'skills' : 'mcp_servers';
        const hasLiveRef = [...this.profiles.values()].some((profile) => profile.workspace_id === workspaceID && profile[section].some((ref) => versionIDs.has(Number(ref.resource_version_id || 0))));
        const hasActiveExecution = [...this.runs.values()].some((run) => {
            if (run.workspace_id !== workspaceID || !ACTIVE_RUNTIME_RUN_STATUSES.has(run.status))
                return false;
            const frozen = run.profile_snapshot?.frozen_resources?.[section] || [];
            return frozen.some((item) => item.id === resourceID);
        });
        if (hasLiveRef || hasActiveExecution)
            return 'in_use';
        this.agentResources.delete(resourceID);
        for (const [versionID, version] of this.agentResourceVersions.entries()) {
            if (version.workspace_id === workspaceID && version.resource_id === resourceID) {
                this.agentResourceVersions.delete(versionID);
            }
        }
        return 'deleted';
    }
    async appendAgentResourceVersion(version) {
        assertAgentResourceVersionSnapshotSafe(version);
        const duplicateID = this.agentResourceVersions.has(version.resource_version_id);
        const duplicateRevision = [...this.agentResourceVersions.values()].some((item) => item.workspace_id === version.workspace_id &&
            item.resource_id === version.resource_id &&
            item.revision === version.revision);
        if (duplicateID || duplicateRevision) {
            throw new Error(`Agent resource revision ${version.resource_id}:${version.revision} already exists`);
        }
        const stored = structuredClone(version);
        this.agentResourceVersions.set(stored.resource_version_id, stored);
        return structuredClone(stored);
    }
    async getAgentResourceVersion(workspaceID, resourceVersionID) {
        const version = this.agentResourceVersions.get(resourceVersionID);
        return version && version.workspace_id === workspaceID ? this.verifiedAgentResourceVersion(version) : undefined;
    }
    async listAgentResourceVersions(workspaceID, resourceID) {
        return [...this.agentResourceVersions.values()]
            .filter((version) => version.workspace_id === workspaceID && version.resource_id === resourceID)
            .sort((left, right) => right.revision - left.revision || right.resource_version_id - left.resource_version_id)
            .map((version) => this.verifiedAgentResourceVersion(version));
    }
    async resolveProviderCredentialRef(_workspaceID, _providerID, _credentialID) {
        return undefined;
    }
    async saveProfileVersion(version) {
        const duplicate = [...this.profileVersions.values()].find((candidate) => candidate.profile_version_id === version.profile_version_id ||
            (candidate.profile_id === version.profile_id && candidate.version === version.version) ||
            (candidate.workspace_id === version.workspace_id && candidate.snapshot_hash === version.snapshot_hash));
        if (duplicate)
            throw new RuntimeDomainError('agent_profile_version_conflict', 'Agent profile version already exists', 409);
        const stored = structuredClone(version);
        this.profileVersions.set(stored.profile_version_id, stored);
        return structuredClone(stored);
    }
    async publishProfileVersion(profile, version, expectedUpdatedAt) {
        const current = await this.getProfile(profile.workspace_id, profile.id);
        if (!current)
            throw new RuntimeDomainError('agent_profile_not_found', 'Agent profile not found', 404);
        if (current.updated_at !== expectedUpdatedAt) {
            throw new RuntimeDomainError('agent_profile_publish_conflict', 'Agent profile changed during publish', 409);
        }
        const existing = [...this.profileVersions.values()].find((candidate) => candidate.workspace_id === version.workspace_id && candidate.snapshot_hash === version.snapshot_hash);
        if (existing) {
            if (existing.profile_id !== profile.id) {
                throw new RuntimeDomainError('agent_profile_version_conflict', 'Agent profile snapshot hash already exists', 409);
            }
            this.profiles.set(profile.id, structuredClone(profile));
            return structuredClone(existing);
        }
        const saved = await this.saveProfileVersion(version);
        this.profiles.set(profile.id, structuredClone(profile));
        return saved;
    }
    async listProfileVersions(workspaceID, profileID) {
        return [...this.profileVersions.values()]
            .filter((version) => version.workspace_id === workspaceID && version.profile_id === profileID)
            .sort((left, right) => right.version - left.version)
            .map((version) => this.verifiedProfileVersion(version));
    }
    async getProfileVersion(workspaceID, profileVersionID) {
        const version = this.profileVersions.get(profileVersionID);
        return version && version.workspace_id === workspaceID ? this.verifiedProfileVersion(version) : undefined;
    }
    verifiedAgentResourceVersion(version) {
        if (agentResourceSnapshotDigest(version.snapshot) !== version.snapshot_digest) {
            throw new RuntimeDomainError('agent_resource_version_integrity_failed', 'Agent resource version snapshot integrity verification failed', 500);
        }
        return structuredClone(version);
    }
    verifiedProfileVersion(version) {
        if (agentProfileSnapshotHash(version.snapshot) !== version.snapshot_hash) {
            throw new RuntimeDomainError('agent_profile_version_integrity_failed', 'Agent profile version snapshot integrity verification failed', 500);
        }
        return structuredClone(version);
    }
    async saveSession(session) {
        this.sessions.set(session.id, session);
        return session;
    }
    async getSession(workspaceID, sessionID) {
        const session = this.sessions.get(sessionID);
        return session && session.workspace_id === workspaceID ? session : undefined;
    }
    async listSessions(workspaceID, filter = {}) {
        return [...this.sessions.values()]
            .filter((session) => session.workspace_id === workspaceID)
            .filter((session) => !filter.context_tag || session.context_tags.includes(filter.context_tag))
            .filter((session) => !filter.context_tags?.length || filter.context_tags.every((tag) => session.context_tags.includes(tag)))
            .filter((session) => !filter.business_type || session.business_type === filter.business_type)
            .filter((session) => !filter.business_id || session.business_id === filter.business_id)
            .filter((session) => !filter.status || session.status === filter.status)
            .sort((left, right) => right.updated_at.localeCompare(left.updated_at) || right.id - left.id);
    }
    async findCurrentSession(key) {
        const sessionID = this.currentSessions.get(key);
        if (!sessionID)
            return undefined;
        return this.sessions.get(sessionID);
    }
    async rememberCurrentSession(key, sessionID) {
        this.currentSessions.set(key, sessionID);
    }
    async saveEntry(entry) {
        this.entries.set(entry.id, entry);
        return entry;
    }
    async listEntries(workspaceID, sessionID) {
        return [...this.entries.values()]
            .filter((entry) => entry.workspace_id === workspaceID && entry.session_id === sessionID)
            .sort((left, right) => left.seq - right.seq);
    }
    async saveRun(run) {
        const normalized = normalizeRuntimeRunActiveSlot(run);
        if (ACTIVE_RUNTIME_RUN_STATUSES.has(normalized.status)) {
            const existingActiveRun = [...this.runs.values()].find((candidate) => candidate.workspace_id === normalized.workspace_id &&
                candidate.session_id === normalized.session_id &&
                candidate.runtime_run_id !== normalized.runtime_run_id &&
                ACTIVE_RUNTIME_RUN_STATUSES.has(candidate.status));
            if (existingActiveRun) {
                throw new RuntimeDomainError('active_run_conflict', `Session already has active runtime run ${existingActiveRun.runtime_run_id} in status ${existingActiveRun.status}`, 409, {
                    active_run: {
                        runtime_run_id: existingActiveRun.runtime_run_id,
                        status: existingActiveRun.status,
                        next_steps: existingActiveRun.status === 'awaiting_decision' ? ['approve_once', 'approve_session', 'reject', 'cancel'] : ['wait', 'cancel']
                    }
                });
            }
        }
        this.runs.set(normalized.runtime_run_id, normalized);
        return normalized;
    }
    async getRun(workspaceID, runtimeRunID) {
        const run = this.runs.get(runtimeRunID);
        return run && run.workspace_id === workspaceID ? run : undefined;
    }
    async findActiveRun(workspaceID, sessionID) {
        const activeStatuses = new Set(['queued', 'running', 'awaiting_decision', 'awaiting_input']);
        return [...this.runs.values()]
            .filter((run) => run.workspace_id === workspaceID && run.session_id === sessionID && activeStatuses.has(run.status))
            .sort((left, right) => right.created_at.localeCompare(left.created_at) || right.id - left.id)[0];
    }
    async claimRunLease(workspaceID, runtimeRunID, instanceID, leaseExpiresAt, observedAt = new Date().toISOString()) {
        const run = await this.getRun(workspaceID, runtimeRunID);
        if (!run || !ACTIVE_RUNTIME_RUN_STATUSES.has(run.status))
            return undefined;
        const leaseIsLive = Boolean(run.owner_instance_id && run.owner_lease_expires_at && run.owner_lease_expires_at > observedAt);
        if (leaseIsLive && run.owner_instance_id !== instanceID)
            return undefined;
        const ownerChanged = run.owner_instance_id !== instanceID;
        const claimed = normalizeRuntimeRunActiveSlot({
            ...run,
            owner_instance_id: instanceID,
            owner_epoch: ownerChanged ? Number(run.owner_epoch || 0) + 1 : Math.max(1, Number(run.owner_epoch || 0)),
            owner_lease_expires_at: leaseExpiresAt,
            updated_at: observedAt
        });
        this.runs.set(runtimeRunID, claimed);
        return claimed;
    }
    async renewRunLease(workspaceID, runtimeRunID, instanceID, ownerEpoch, leaseExpiresAt) {
        const run = await this.getRun(workspaceID, runtimeRunID);
        if (!run || !ACTIVE_RUNTIME_RUN_STATUSES.has(run.status))
            return undefined;
        if (run.owner_instance_id !== instanceID || Number(run.owner_epoch || 0) !== ownerEpoch)
            return undefined;
        const renewed = { ...run, owner_lease_expires_at: leaseExpiresAt, updated_at: new Date().toISOString() };
        this.runs.set(runtimeRunID, renewed);
        return renewed;
    }
    async releaseRunLease(workspaceID, runtimeRunID, instanceID, ownerEpoch) {
        const run = await this.getRun(workspaceID, runtimeRunID);
        if (!run || run.owner_instance_id !== instanceID || Number(run.owner_epoch || 0) !== ownerEpoch)
            return false;
        this.runs.set(runtimeRunID, {
            ...run,
            owner_instance_id: undefined,
            owner_lease_expires_at: undefined,
            updated_at: new Date().toISOString()
        });
        return true;
    }
    async interruptExpiredRunLeases(observedAt) {
        const interrupted = [];
        for (const run of this.runs.values()) {
            if (!ACTIVE_RUNTIME_RUN_STATUSES.has(run.status) || run.status === 'awaiting_decision')
                continue;
            if (!run.owner_instance_id || !run.owner_lease_expires_at || run.owner_lease_expires_at > observedAt)
                continue;
            const next = normalizeRuntimeRunActiveSlot({
                ...run,
                status: 'interrupted',
                owner_instance_id: undefined,
                owner_lease_expires_at: undefined,
                error_code: 'runtime_owner_lease_expired',
                error_msg: `Runtime owner ${run.owner_instance_id} lease expired`,
                finished_at: observedAt,
                updated_at: observedAt
            });
            this.runs.set(run.runtime_run_id, next);
            interrupted.push(next);
        }
        return interrupted;
    }
    async getRuntimeOperationsSummary(observedAt) {
        const runs = [...this.runs.values()];
        const fiveMinutesAgo = new Date(Date.parse(observedAt) - 5 * 60_000).toISOString();
        const oneHourAgo = new Date(Date.parse(observedAt) - 60 * 60_000).toISOString();
        const terminalStatuses = ['completed', 'failed', 'cancelled', 'timeout', 'interrupted'];
        const terminalCounts = (threshold) => Object.fromEntries(terminalStatuses.map((status) => [
            status,
            runs.filter((run) => {
                const terminalAt = firstString(run.finished_at, run.updated_at);
                return run.status === status && terminalAt >= threshold && terminalAt <= observedAt;
            }).length
        ]));
        const failures = new Map();
        for (const run of runs) {
            if (!['failed', 'timeout', 'interrupted'].includes(run.status))
                continue;
            const terminalAt = firstString(run.finished_at, run.updated_at);
            if (terminalAt < oneHourAgo || terminalAt > observedAt)
                continue;
            const error = asRecord(run.result.error);
            const category = firstString(error.category, run.status === 'timeout' ? 'provider_timeout' : 'internal');
            const code = firstString(error.code, run.error_code, 'runtime_error');
            const key = `${category}:${code}`;
            const existing = failures.get(key);
            if (existing)
                existing.count += 1;
            else
                failures.set(key, { category, code, count: 1 });
        }
        const activeRuns = runs.filter((run) => ACTIVE_RUNTIME_RUN_STATUSES.has(run.status));
        return {
            observed_at: observedAt,
            runs: {
                active: activeRuns.length,
                awaiting_approval: activeRuns.filter((run) => run.status === 'awaiting_decision').length,
                awaiting_input: activeRuns.filter((run) => run.status === 'awaiting_input').length,
                stale: activeRuns.filter((run) => ['queued', 'running'].includes(run.status)
                    && Boolean(run.owner_lease_expires_at && run.owner_lease_expires_at <= observedAt)).length,
                orphaned: activeRuns.filter((run) => ['queued', 'running'].includes(run.status)
                    && (!run.owner_instance_id || !run.owner_lease_expires_at)).length
            },
            terminal_5m: terminalCounts(fiveMinutesAgo),
            terminal_1h: terminalCounts(oneHourAgo),
            failures_1h: [...failures.values()].sort((left, right) => left.category.localeCompare(right.category) || left.code.localeCompare(right.code))
        };
    }
    async saveAction(action) {
        this.actions.set(action.id, action);
        this.actionBusinessIndex.set(`${action.workspace_id}:${action.action_id}`, action.id);
        return action;
    }
    async createToolAction(action) {
        const existing = this.findActionByIdempotency(action.workspace_id, action.idempotency_key)
            || this.findActionByBusinessID(action.workspace_id, action.action_id);
        if (existing)
            return { outcome: 'existing', action: cloneAction(existing) };
        const stored = cloneAction(action);
        this.actions.set(stored.id, stored);
        this.actionBusinessIndex.set(`${stored.workspace_id}:${stored.action_id}`, stored.id);
        return { outcome: 'created', action: cloneAction(stored) };
    }
    async decideAwaitingAction(input) {
        const action = this.actions.get(input.action_id);
        if (!action || action.workspace_id !== input.workspace_id)
            return { outcome: 'not_found' };
        if (action.status !== 'awaiting_decision')
            return { outcome: 'already_decided', action: cloneAction(action) };
        const decided = cloneAction({
            ...action,
            status: statusForDecision(input.decision.decision),
            decided_by: input.decision.actor_user_id,
            decided_at: input.decided_at,
            updated_at: input.decided_at
        });
        this.actionDecisions.set(input.decision.decision_id, structuredClone(input.decision));
        if (input.session_grant) {
            this.sessionPermissionGrants.set(`${input.session_grant.workspace_id}:${input.session_grant.grant_id}`, structuredClone(input.session_grant));
        }
        this.actions.set(decided.id, decided);
        return { outcome: 'decided', action: cloneAction(decided) };
    }
    async claimApprovedActionExecution(input) {
        const action = this.actions.get(input.action_id);
        if (!action || action.workspace_id !== input.workspace_id)
            return { outcome: 'not_approved' };
        if (action.input_digest !== input.input_digest)
            return { outcome: 'input_mismatch' };
        if (action.status === 'executing' || action.status === 'executed' || action.status === 'failed') {
            return { outcome: 'already_claimed', action: cloneAction(action) };
        }
        if (action.status !== 'approved')
            return { outcome: 'not_approved' };
        const attempt = (this.actionExecutionAttempts.get(action.id) || 0) + 1;
        this.actionExecutionAttempts.set(action.id, attempt);
        const execution = {
            execution_id: actionExecutionBusinessID(action, attempt),
            workspace_id: action.workspace_id,
            session_id: action.session_id,
            runtime_run_id: action.runtime_run_id,
            action_id: action.id,
            attempt,
            executor_type: executorTypeForActionKind(action.action_kind),
            executor_ref_json: { action_kind: action.action_kind, capability_id: action.capability_id },
            idempotency_key: `execution:${action.action_id}:attempt${seq36(attempt)}`,
            status: 'running',
            result_json: {},
            started_at: input.claimed_at,
            created_at: input.claimed_at,
            updated_at: input.claimed_at
        };
        const claimed = cloneAction({ ...action, status: 'executing', executed_at: input.claimed_at, updated_at: input.claimed_at });
        this.actions.set(claimed.id, claimed);
        this.actionExecutions.set(`${execution.workspace_id}:${execution.action_id}:${execution.attempt}`, execution);
        return { outcome: 'claimed', action: cloneAction(claimed), execution: structuredClone(execution) };
    }
    findActionByIdempotency(workspaceID, idempotencyKey) {
        return [...this.actions.values()].find((action) => action.workspace_id === workspaceID && action.idempotency_key === idempotencyKey);
    }
    findActionByBusinessID(workspaceID, actionBusinessID) {
        const actionID = this.actionBusinessIndex.get(`${workspaceID}:${actionBusinessID}`);
        return actionID ? this.actions.get(actionID) : undefined;
    }
    async getAction(workspaceID, actionID) {
        const action = this.actions.get(actionID);
        return action && action.workspace_id === workspaceID ? action : undefined;
    }
    async getActionByBusinessID(workspaceID, actionBusinessID) {
        const actionID = this.actionBusinessIndex.get(`${workspaceID}:${actionBusinessID}`);
        if (!actionID)
            return undefined;
        return this.getAction(workspaceID, actionID);
    }
    async appendActionEvent(event) {
        if (this.actionEvents.has(event.event_id)) {
            return this.actionEvents.get(event.event_id);
        }
        this.actionEvents.set(event.event_id, event);
        return event;
    }
    async listActionEvents(workspaceID, runtimeRunID, afterEventID = '', limit = 0) {
        const events = [...this.actionEvents.values()]
            .filter((event) => event.workspace_id === workspaceID && event.runtime_run_id === runtimeRunID)
            .sort((left, right) => left.event_seq - right.event_seq);
        let filtered = events;
        if (afterEventID) {
            const after = events.find((event) => event.event_id === afterEventID);
            filtered = after ? events.filter((event) => event.event_seq > after.event_seq) : events;
        }
        const pageLimit = Number.isFinite(Number(limit)) && Number(limit) > 0 ? Math.floor(Number(limit)) : 0;
        return pageLimit > 0 ? filtered.slice(0, pageLimit) : filtered;
    }
    async saveRuntimeArtifact(artifact) {
        this.runtimeArtifacts.set(`${artifact.workspace_id}:${artifact.artifact_id}`, artifact);
        return artifact;
    }
    async getRuntimeArtifact(workspaceID, artifactID) {
        return this.runtimeArtifacts.get(`${workspaceID}:${artifactID}`);
    }
    async listActionArtifacts(workspaceID, actionID) {
        return [...this.runtimeArtifacts.values()]
            .filter((artifact) => artifact.workspace_id === workspaceID && artifact.action_id === actionID)
            .sort((left, right) => left.artifact_seq - right.artifact_seq);
    }
    async saveChildRunLink(link) {
        this.childRunLinks.set(`${link.workspace_id}:${link.child_run_link_id}`, link);
        return link;
    }
    async listChildRunLinks(workspaceID, parentRuntimeRunID) {
        return [...this.childRunLinks.values()]
            .filter((link) => link.workspace_id === workspaceID && link.parent_runtime_run_id === parentRuntimeRunID)
            .sort((left, right) => left.child_seq - right.child_seq);
    }
    async getParentRunLink(workspaceID, childRuntimeRunID) {
        return [...this.childRunLinks.values()]
            .find((link) => link.workspace_id === workspaceID && link.child_runtime_run_id === childRuntimeRunID);
    }
    async saveOrchestrationPlan(plan) {
        this.orchestrationPlans.set(plan.orchestration_id, { ...plan });
        return this.orchestrationPlans.get(plan.orchestration_id);
    }
    async getOrchestrationPlan(workspaceID, orchestrationID) {
        const plan = this.orchestrationPlans.get(orchestrationID);
        if (!plan)
            return undefined;
        if (workspaceID > 0 && plan.workspace_id !== workspaceID)
            return undefined;
        return plan;
    }
    async getOrchestrationPlanByParentRun(workspaceID, parentRuntimeRunID) {
        return [...this.orchestrationPlans.values()]
            .find((plan) => plan.workspace_id === workspaceID && plan.parent_runtime_run_id === parentRuntimeRunID);
    }
    async claimOrchestrationPlan(input) {
        const plan = await this.getOrchestrationPlan(input.workspace_id, input.orchestration_id);
        if (!plan)
            return undefined;
        if (plan.status === 'completed' || plan.status === 'failed' || plan.status === 'cancelled')
            return plan;
        const claimed = {
            ...plan,
            claim_owner: input.owner,
            claim_epoch: Number(plan.claim_epoch || 0) + 1,
            claim_expires_at: input.claim_expires_at,
            updated_at: new Date().toISOString()
        };
        return this.saveOrchestrationPlan(claimed);
    }
    async saveOrchestrationContextPack(pack) {
        this.orchestrationContextPacks.set(pack.context_pack_id, { ...pack });
        return this.orchestrationContextPacks.get(pack.context_pack_id);
    }
    async getOrchestrationContextPack(workspaceID, contextPackID) {
        const pack = this.orchestrationContextPacks.get(contextPackID);
        if (!pack)
            return undefined;
        if (workspaceID > 0 && pack.workspace_id !== workspaceID)
            return undefined;
        return pack;
    }
    async saveOrchestrationTask(task) {
        this.orchestrationTasks.set(task.task_id, { ...task, dependency_ids: [...task.dependency_ids], artifact_refs: [...task.artifact_refs] });
        return this.orchestrationTasks.get(task.task_id);
    }
    async listOrchestrationTasks(workspaceID, orchestrationID) {
        return [...this.orchestrationTasks.values()]
            .filter((task) => task.workspace_id === workspaceID && task.orchestration_id === orchestrationID)
            .sort((left, right) => left.task_seq - right.task_seq);
    }
    async getOrchestrationTask(workspaceID, taskID) {
        const task = this.orchestrationTasks.get(taskID);
        if (!task)
            return undefined;
        if (workspaceID > 0 && task.workspace_id !== workspaceID)
            return undefined;
        return task;
    }
    async claimOrchestrationTask(input) {
        const task = await this.getOrchestrationTask(input.workspace_id, input.task_id);
        if (!task)
            return undefined;
        if (!['pending', 'queued'].includes(task.status))
            return undefined;
        const claimed = {
            ...task,
            status: 'claimed',
            claim_owner: input.owner,
            claim_epoch: Number(task.claim_epoch || 0) + 1,
            claim_expires_at: input.claim_expires_at,
            updated_at: new Date().toISOString()
        };
        return this.saveOrchestrationTask(claimed);
    }
    async saveActionDecision(decision) {
        this.actionDecisions.set(decision.decision_id, decision);
        return decision;
    }
    async listActionDecisions(workspaceID, actionID) {
        return [...this.actionDecisions.values()]
            .filter((decision) => decision.workspace_id === workspaceID && decision.action_id === actionID)
            .sort((left, right) => left.decision_seq - right.decision_seq);
    }
    async saveSessionPermissionGrant(grant) {
        this.sessionPermissionGrants.set(`${grant.workspace_id}:${grant.grant_id}`, grant);
        return grant;
    }
    async listSessionPermissionGrants(workspaceID, sessionID) {
        return [...this.sessionPermissionGrants.values()]
            .filter((grant) => grant.workspace_id === workspaceID && grant.session_id === sessionID)
            .sort((left, right) => left.grant_seq - right.grant_seq);
    }
    async saveActionExecution(execution) {
        this.actionExecutions.set(`${execution.workspace_id}:${execution.action_id}:${execution.attempt}`, execution);
        return execution;
    }
    async getActionExecution(workspaceID, actionID, attempt) {
        return this.actionExecutions.get(`${workspaceID}:${actionID}:${attempt}`);
    }
    async listActionExecutions(workspaceID, actionID) {
        return [...this.actionExecutions.values()]
            .filter((execution) => execution.workspace_id === workspaceID && execution.action_id === actionID)
            .sort((left, right) => left.attempt - right.attempt);
    }
    async saveAgentWorkspace(workspace) {
        this.agentWorkspaces.set(`${workspace.workspace_id}:${workspace.workspace_runtime_id}`, workspace);
        return workspace;
    }
    async createOrGetDefaultAgentWorkspace(workspace) {
        const existing = [...this.agentWorkspaces.values()].find((item) => item.workspace_id === workspace.workspace_id &&
            item.owner_user_id === workspace.owner_user_id &&
            item.workspace_key === workspace.workspace_key);
        if (existing)
            return { outcome: 'existing', workspace: structuredClone(existing) };
        const stored = structuredClone(workspace);
        this.agentWorkspaces.set(`${stored.workspace_id}:${stored.workspace_runtime_id}`, stored);
        return { outcome: 'created', workspace: structuredClone(stored) };
    }
    async claimAgentWorkspaceProvision(input) {
        const key = `${input.workspace_id}:${input.workspace_runtime_id}`;
        const workspace = this.agentWorkspaces.get(key);
        if (!workspace || workspace.owner_user_id !== input.owner_user_id)
            return undefined;
        const active = workspace.provision_owner_instance_id && workspace.provision_expires_at && workspace.provision_expires_at > input.observed_at;
        if (active)
            return undefined;
        if (!['provisioning', 'failed', 'recycled'].includes(workspace.status))
            return undefined;
        const claimed = {
            ...workspace,
            status: 'provisioning',
            provision_owner_instance_id: input.instance_id,
            provision_epoch: Number(workspace.provision_epoch || 0) + 1,
            provision_expires_at: input.lease_expires_at,
            state_version: Number(workspace.state_version || 0) + 1,
            error_code: undefined,
            error_msg: undefined,
            updated_at: input.observed_at
        };
        this.agentWorkspaces.set(key, claimed);
        return structuredClone(claimed);
    }
    async completeAgentWorkspaceProvision(input) {
        const key = `${input.workspace_id}:${input.workspace_runtime_id}`;
        const workspace = this.agentWorkspaces.get(key);
        if (!workspace || workspace.status !== 'provisioning')
            return undefined;
        if (workspace.provision_owner_instance_id !== input.instance_id || Number(workspace.provision_epoch || 0) !== input.provision_epoch)
            return undefined;
        const ready = {
            ...workspace,
            sandbox_id: input.sandbox_id,
            status: 'ready',
            provision_owner_instance_id: undefined,
            provision_expires_at: undefined,
            state_version: Number(workspace.state_version || 0) + 1,
            error_code: undefined,
            error_msg: undefined,
            updated_at: input.updated_at
        };
        this.agentWorkspaces.set(key, ready);
        return structuredClone(ready);
    }
    async failAgentWorkspaceProvision(input) {
        const key = `${input.workspace_id}:${input.workspace_runtime_id}`;
        const workspace = this.agentWorkspaces.get(key);
        if (!workspace || workspace.status !== 'provisioning')
            return undefined;
        if (workspace.provision_owner_instance_id !== input.instance_id || Number(workspace.provision_epoch || 0) !== input.provision_epoch)
            return undefined;
        const failed = {
            ...workspace,
            status: 'failed',
            provision_owner_instance_id: undefined,
            provision_expires_at: undefined,
            state_version: Number(workspace.state_version || 0) + 1,
            error_code: input.error_code,
            error_msg: input.error_msg,
            updated_at: input.updated_at
        };
        this.agentWorkspaces.set(key, failed);
        return structuredClone(failed);
    }
    async getAgentWorkspace(workspaceID, workspaceRuntimeID) {
        return this.agentWorkspaces.get(`${workspaceID}:${workspaceRuntimeID}`);
    }
    async listAgentWorkspaces(workspaceID, ownerUserID) {
        return [...this.agentWorkspaces.values()]
            .filter((workspace) => workspace.workspace_id === workspaceID)
            .filter((workspace) => ownerUserID === undefined || workspace.owner_user_id === ownerUserID)
            .sort((left, right) => right.updated_at.localeCompare(left.updated_at) || right.id - left.id);
    }
    async beginAgentWorkspaceRecycle(workspaceID, workspaceRuntimeID, ownerUserID, operationID, leaseExpiresAt, observedAt = new Date().toISOString()) {
        const key = `${workspaceID}:${workspaceRuntimeID}`;
        const workspace = this.agentWorkspaces.get(key);
        if (!workspace || workspace.owner_user_id !== ownerUserID)
            return undefined;
        if (!['ready', 'paused', 'recycled', 'failed'].includes(workspace.status))
            return undefined;
        const leaseIsLive = Boolean(workspace.lease_owner_instance_id &&
            workspace.lease_expires_at &&
            workspace.lease_expires_at > observedAt);
        if (leaseIsLive)
            return undefined;
        const claimed = {
            ...workspace,
            status: 'recycling',
            lease_owner_instance_id: operationID,
            lease_epoch: Number(workspace.lease_epoch || 0) + 1,
            lease_expires_at: leaseExpiresAt,
            paused_at: undefined,
            error_code: undefined,
            error_msg: undefined,
            updated_at: observedAt
        };
        this.agentWorkspaces.set(key, claimed);
        return claimed;
    }
    async claimAgentWorkspaceLease(workspaceID, workspaceRuntimeID, instanceID, leaseExpiresAt, observedAt = new Date().toISOString()) {
        const workspace = await this.getAgentWorkspace(workspaceID, workspaceRuntimeID);
        if (!workspace || workspace.status !== 'ready')
            return undefined;
        const leaseIsLive = Boolean(workspace.lease_owner_instance_id &&
            workspace.lease_expires_at &&
            workspace.lease_expires_at > observedAt);
        if (leaseIsLive && workspace.lease_owner_instance_id !== instanceID)
            return undefined;
        const keepsLiveLease = leaseIsLive && workspace.lease_owner_instance_id === instanceID;
        const claimed = {
            ...workspace,
            lease_owner_instance_id: instanceID,
            lease_epoch: keepsLiveLease ? Math.max(1, Number(workspace.lease_epoch || 0)) : Number(workspace.lease_epoch || 0) + 1,
            lease_expires_at: leaseExpiresAt,
            last_connected_at: observedAt,
            updated_at: observedAt
        };
        this.agentWorkspaces.set(`${workspaceID}:${workspaceRuntimeID}`, claimed);
        return claimed;
    }
    async renewAgentWorkspaceLease(workspaceID, workspaceRuntimeID, instanceID, leaseEpoch, leaseExpiresAt) {
        const workspace = await this.getAgentWorkspace(workspaceID, workspaceRuntimeID);
        if (!workspace || workspace.status !== 'ready')
            return undefined;
        if (workspace.lease_owner_instance_id !== instanceID || Number(workspace.lease_epoch || 0) !== leaseEpoch)
            return undefined;
        const renewed = { ...workspace, lease_expires_at: leaseExpiresAt, updated_at: new Date().toISOString() };
        this.agentWorkspaces.set(`${workspaceID}:${workspaceRuntimeID}`, renewed);
        return renewed;
    }
    async releaseAgentWorkspaceLease(workspaceID, workspaceRuntimeID, instanceID, leaseEpoch) {
        const workspace = await this.getAgentWorkspace(workspaceID, workspaceRuntimeID);
        if (!workspace || workspace.lease_owner_instance_id !== instanceID || Number(workspace.lease_epoch || 0) !== leaseEpoch)
            return false;
        this.agentWorkspaces.set(`${workspaceID}:${workspaceRuntimeID}`, {
            ...workspace,
            lease_owner_instance_id: undefined,
            lease_expires_at: undefined,
            updated_at: new Date().toISOString()
        });
        return true;
    }
    async appendAgentWorkspaceAudit(audit) {
        this.agentWorkspaceAudits.set(`${audit.workspace_id}:${audit.audit_id}`, audit);
        return audit;
    }
    async listAgentWorkspaceAudits(workspaceID, workspaceRuntimeID) {
        return [...this.agentWorkspaceAudits.values()]
            .filter((audit) => audit.workspace_id === workspaceID && audit.workspace_runtime_id === workspaceRuntimeID)
            .sort((left, right) => left.created_at.localeCompare(right.created_at) || left.audit_id.localeCompare(right.audit_id));
    }
}
export function createMemoryRuntimeStore() {
    return new MemoryRuntimeStore();
}
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
function firstString(...values) {
    for (const value of values) {
        if (typeof value === 'string' && value.trim())
            return value.trim();
    }
    return '';
}
function sessionQueueItemOrder(left, right) {
    const leftTerminal = ['cancelled', 'applied', 'failed'].includes(left.status);
    const rightTerminal = ['cancelled', 'applied', 'failed'].includes(right.status);
    if (leftTerminal !== rightTerminal)
        return leftTerminal ? 1 : -1;
    return left.position - right.position || left.item_seq - right.item_seq;
}
function cloneAction(action) {
    return structuredClone(action);
}
function statusForDecision(decision) {
    switch (decision) {
        case 'approve_once':
        case 'approve_session':
            return 'approved';
        case 'reject':
        case 'steer':
            return 'rejected';
        case 'expired':
            return 'expired';
    }
}
function executorTypeForActionKind(actionKind) {
    switch (actionKind) {
        case 'pipeline.trigger':
            return 'pipeline';
        case 'subagent.spawn':
            return 'subagent';
        default:
            return 'mcp';
    }
}
function isActiveRunStatus(status) {
    return status === 'queued' || status === 'running' || status === 'awaiting_decision' || status === 'awaiting_input';
}
function actionExecutionBusinessID(action, attempt) {
    return `${action.action_id.replace(/^a_/, 'x_')}_${seq36(attempt)}`;
}
function seq36(value) {
    return Math.max(0, Math.floor(value)).toString(36).padStart(6, '0');
}
