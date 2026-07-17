import { createDecipheriv } from 'node:crypto';
import { createPool } from 'mysql2/promise';
import { activeSlotForRunStatus, agentProfileSnapshotHash, agentResourceSnapshotDigest, assertAgentResourceVersionMatchesResource, assertAgentResourceVersionSnapshotSafe, normalizeRuntimeRunActiveSlot, sameSessionQueueItemSet, sessionQueueItemBusinessID, stableJSONStringify } from '../domain/runtime.js';
import { RuntimeDomainError } from '../services/runtimeErrors.js';
function json(value) {
    return stableJSONStringify(value ?? {});
}
function parseJSON(value, fallback) {
    if (!value)
        return fallback;
    try {
        return JSON.parse(value);
    }
    catch {
        return fallback;
    }
}
function toDBDate(value) {
    const date = value ? new Date(value) : new Date();
    return date.toISOString().slice(0, 23).replace('T', ' ');
}
function fromDBDate(value) {
    if (!value)
        return undefined;
    return new Date(value.replace(' ', 'T') + 'Z').toISOString();
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
function actionExecutionBusinessID(action, attempt) {
    return `${action.action_id.replace(/^a_/, 'x_')}_${seq36(attempt)}`;
}
function seq36(value) {
    return Math.max(0, Math.floor(value)).toString(36).padStart(6, '0');
}
function activeSlotDBValue(status) {
    return activeSlotForRunStatus(status) ?? null;
}
function isDuplicateActiveRunError(error) {
    const record = error;
    return (record.code === 'ER_DUP_ENTRY' || record.errno === 1062) &&
        String(record.sqlMessage || '').includes('uk_ai_runtime_runs_active_root');
}
function resourceIDForDB(value) {
    return String(value);
}
function resourceIDKindForDB(value) {
    return typeof value === 'number' ? 'database_id' : 'resource_key';
}
function resourceIDFromDB(value, kind) {
    return kind === 'database_id' ? Number(value) : value;
}
function decryptCredentialPayload(encryptedPayload, keyMaterial) {
    const key = Buffer.from(keyMaterial, 'base64');
    if (key.length !== 32) {
        throw new Error(`invalid master key length: ${key.length}`);
    }
    const payload = Buffer.from(encryptedPayload, 'base64');
    if (payload.length < 29) {
        throw new Error('invalid credential payload length');
    }
    const iv = payload.subarray(0, 12);
    const encrypted = payload.subarray(12, payload.length - 16);
    const authTag = payload.subarray(payload.length - 16);
    const decipher = createDecipheriv('aes-256-gcm', key, iv);
    decipher.setAuthTag(authTag);
    const plaintext = Buffer.concat([decipher.update(encrypted), decipher.final()]).toString('utf8');
    return parseJSON(plaintext, {});
}
function providerSecretRef(payload) {
    const secretRef = {};
    for (const key of ['api_key', 'token', 'access_token', 'key', 'password', 'bearer_token']) {
        const value = payload[key];
        if (typeof value === 'string' && value.trim()) {
            secretRef[key] = value.trim();
        }
    }
    const tokenType = payload.token_type || payload.auth_scheme;
    if (typeof tokenType === 'string' && tokenType.trim()) {
        secretRef.token_type = tokenType.trim();
    }
    const authHeader = payload.auth_header || payload.header_name || payload.api_key_header;
    if (typeof authHeader === 'string' && authHeader.trim()) {
        secretRef.auth_header = authHeader.trim();
    }
    return secretRef;
}
function isForeignKeyConstraintError(error) {
    const record = error;
    return record?.code === 'ER_ROW_IS_REFERENCED_2' || record?.code === 'ER_NO_REFERENCED_ROW_2' ||
        record?.errno === 1451 || record?.errno === 1452;
}
function isDuplicateKeyError(error) {
    const record = error;
    return record?.code === 'ER_DUP_ENTRY' || record?.errno === 1062;
}
const agentResourceInsertSQL = `INSERT INTO ai_agent_resources (
  id, workspace_id, resource_kind, resource_key, name, description, version, status,
  spec_json, endpoint_json, secret_ref_json, tags_json, created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`;
const agentResourceUpdateSQL = `UPDATE ai_agent_resources SET
  resource_kind = ?, resource_key = ?, name = ?, description = ?, version = ?, status = ?,
  spec_json = ?, endpoint_json = ?, secret_ref_json = ?, tags_json = ?, updated_at = ?
WHERE workspace_id = ? AND id = ?`;
const agentResourceVersionInsertSQL = `INSERT INTO ai_agent_resource_versions (
  id, resource_id, workspace_id, revision, source_version, snapshot_digest,
  snapshot_json, created_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`;
function agentResourceWriteParams(resource) {
    return [
        resource.id,
        resource.workspace_id,
        resource.resource_kind,
        resource.resource_key,
        resource.name,
        resource.description,
        resource.version,
        resource.status,
        json(resource.spec),
        json(resource.endpoint),
        json(resource.secret_ref),
        json(resource.tags),
        resource.created_by,
        toDBDate(resource.created_at),
        toDBDate(resource.updated_at)
    ];
}
function agentResourceUpdateParams(resource) {
    return [
        resource.resource_kind,
        resource.resource_key,
        resource.name,
        resource.description,
        resource.version,
        resource.status,
        json(resource.spec),
        json(resource.endpoint),
        json(resource.secret_ref),
        json(resource.tags),
        toDBDate(resource.updated_at),
        resource.workspace_id,
        resource.id
    ];
}
function agentResourceVersionWriteParams(version) {
    return [
        version.resource_version_id,
        version.resource_id,
        version.workspace_id,
        version.revision,
        version.source_version,
        version.snapshot_digest,
        json(version.snapshot),
        version.created_by,
        toDBDate(version.created_at)
    ];
}
export class MariadbRuntimeStore {
    pool;
    constructor(pool) {
        this.pool = pool;
    }
    async ping() {
        await this.pool.query('SELECT 1');
    }
    static create(options) {
        const uri = options.uri?.trim();
        const pool = uri
            ? createPool({ uri, waitForConnections: true, connectionLimit: 10, dateStrings: true })
            : createPool({
                host: options.host,
                port: options.port || 3306,
                user: options.user,
                password: options.password,
                database: options.database,
                waitForConnections: true,
                connectionLimit: 10,
                dateStrings: true
            });
        return new MariadbRuntimeStore(pool);
    }
    async close() {
        await this.pool.end();
    }
    nextProfileId() {
        return this.nextID('agent_profile');
    }
    nextProfileVersionId() {
        return this.nextID('agent_profile_version');
    }
    nextAgentResourceId() {
        return this.nextID('agent_resource');
    }
    nextAgentResourceVersionId() {
        return this.nextID('agent_resource_version');
    }
    nextSessionId() {
        return this.nextID('session');
    }
    nextEntryId() {
        return this.nextID('entry');
    }
    nextRunId() {
        return this.nextID('runtime_run');
    }
    nextActionId() {
        return this.nextID('agent_action');
    }
    nextAgentWorkspaceId() {
        return this.nextID('agent_workspace');
    }
    nextRunEventSeq(runtimeRunID) {
        return this.nextID(`runtime_event:${runtimeRunID}`);
    }
    nextRunArtifactSeq(runtimeRunID) {
        return this.nextID(`runtime_artifact:${runtimeRunID}`);
    }
    nextActionChildSeq(actionID) {
        return this.nextID(`action_child:${actionID}`);
    }
    nextActionDecisionSeq(actionID) {
        return this.nextID(`action_decision:${actionID}`);
    }
    nextSessionPermissionGrantSeq(sessionID) {
        return this.nextID(`session_permission_grant:${sessionID}`);
    }
    nextActionExecutionAttempt(actionID) {
        return this.nextID(`action_execution:${actionID}`);
    }
    async saveProfile(profile) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [existing] = await conn.execute('SELECT id FROM ai_agent_profiles WHERE workspace_id = ? AND id = ? LIMIT 1', [profile.workspace_id, profile.id]);
            await this.writeProfile(conn, profile, existing.length > 0 ? 'update' : 'create');
            await conn.commit();
            return profile;
        }
        catch (error) {
            await conn.rollback();
            if (isDuplicateKeyError(error)) {
                throw new RuntimeDomainError('agent_profile_name_exists', 'Agent profile name already exists in this workspace', 409);
            }
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async getProfile(workspaceID, profileID) {
        const rows = await this.query('SELECT * FROM ai_agent_profiles WHERE workspace_id = ? AND id = ? LIMIT 1', [workspaceID, profileID]);
        if (rows.length === 0)
            return undefined;
        return this.profileFromRow(rows[0]);
    }
    async listProfiles(workspaceID) {
        const rows = await this.query('SELECT * FROM ai_agent_profiles WHERE workspace_id = ? ORDER BY updated_at DESC, id DESC', [workspaceID]);
        return Promise.all(rows.map((row) => this.profileFromRow(row)));
    }
    async deleteProfileGuarded(workspaceID, profileID) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [profiles] = await conn.execute('SELECT * FROM ai_agent_profiles WHERE workspace_id = ? AND id = ? FOR UPDATE', [workspaceID, profileID]);
            if (profiles.length === 0) {
                await conn.rollback();
                return 'not_found';
            }
            const [publishedVersions] = await conn.execute('SELECT id FROM ai_agent_profile_versions WHERE workspace_id = ? AND profile_id = ? LIMIT 1 FOR UPDATE', [workspaceID, profileID]);
            const [sessions] = await conn.execute('SELECT id FROM ai_sessions WHERE workspace_id = ? AND agent_profile_id = ? LIMIT 1 FOR UPDATE', [workspaceID, profileID]);
            const [parentRefs] = await conn.execute(`SELECT id FROM ai_agent_profile_resource_refs
        WHERE workspace_id = ? AND profile_section = 'subagents'
          AND resource_type = 'subagent_profile' AND resource_id = ?
        LIMIT 1 FOR UPDATE`, [workspaceID, String(profileID)]);
            if (publishedVersions.length > 0 || sessions.length > 0 || parentRefs.length > 0) {
                await conn.rollback();
                return 'in_use';
            }
            const [result] = await conn.execute('DELETE FROM ai_agent_profiles WHERE workspace_id = ? AND id = ?', [workspaceID, profileID]);
            if (result.affectedRows !== 1) {
                await conn.rollback();
                return 'not_found';
            }
            await conn.commit();
            return 'deleted';
        }
        catch (error) {
            await conn.rollback();
            if (isForeignKeyConstraintError(error))
                return 'in_use';
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async saveAgentResource(resource) {
        const [result] = await this.pool.execute(agentResourceUpdateSQL, agentResourceUpdateParams(resource));
        if (result.affectedRows !== 1) {
            throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404);
        }
        return resource;
    }
    async saveAgentResourceWithVersion(resource, version, operation) {
        assertAgentResourceVersionMatchesResource(resource, version);
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            if (operation === 'create') {
                if (version.revision !== 1) {
                    throw new RuntimeDomainError('agent_resource_revision_conflict', 'New Agent resources must start at revision 1', 409);
                }
                await conn.execute(agentResourceInsertSQL, agentResourceWriteParams(resource));
            }
            else {
                const [heads] = await conn.execute('SELECT id FROM ai_agent_resources WHERE workspace_id = ? AND id = ? FOR UPDATE', [resource.workspace_id, resource.id]);
                if (heads.length !== 1) {
                    throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404);
                }
                const [revisionRows] = await conn.execute('SELECT COALESCE(MAX(revision), 0) AS revision FROM ai_agent_resource_versions WHERE workspace_id = ? AND resource_id = ? FOR UPDATE', [resource.workspace_id, resource.id]);
                const expectedRevision = Number(revisionRows[0]?.revision || 0) + 1;
                if (version.revision !== expectedRevision) {
                    throw new RuntimeDomainError('agent_resource_revision_conflict', 'Agent resource revision changed during update', 409);
                }
                const [result] = await conn.execute(agentResourceUpdateSQL, agentResourceUpdateParams(resource));
                if (result.affectedRows !== 1) {
                    throw new RuntimeDomainError('agent_resource_not_found', 'Agent resource not found', 404);
                }
            }
            await conn.execute(agentResourceVersionInsertSQL, agentResourceVersionWriteParams(version));
            await conn.commit();
            return { resource, version };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async createAgentResourceWithVersion(resource, version) {
        return this.saveAgentResourceWithVersion(resource, version, 'create');
    }
    async updateAgentResourceWithVersion(resource, version) {
        return this.saveAgentResourceWithVersion(resource, version, 'update');
    }
    async getAgentResource(workspaceID, resourceID) {
        const rows = await this.query('SELECT * FROM ai_agent_resources WHERE workspace_id = ? AND id = ? LIMIT 1', [workspaceID, resourceID]);
        return rows[0] ? this.agentResourceFromRow(rows[0]) : undefined;
    }
    async listAgentResources(workspaceID) {
        const rows = await this.query('SELECT * FROM ai_agent_resources WHERE workspace_id = ? ORDER BY updated_at DESC, id DESC', [workspaceID]);
        return rows.map(this.agentResourceFromRow);
    }
    async deleteAgentResource(workspaceID, resourceID) {
        const [result] = await this.pool.execute('DELETE FROM ai_agent_resources WHERE workspace_id = ? AND id = ?', [workspaceID, resourceID]);
        return result.affectedRows > 0;
    }
    async deleteAgentResourceGuarded(workspaceID, resourceID) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [heads] = await conn.execute('SELECT * FROM ai_agent_resources WHERE workspace_id = ? AND id = ? FOR UPDATE', [workspaceID, resourceID]);
            const head = heads[0];
            if (!head) {
                await conn.rollback();
                return 'not_found';
            }
            // Profile writes lock their referenced immutable versions before inserting
            // refs. Taking the same lock here closes the check/delete race in either order.
            await conn.execute('SELECT id FROM ai_agent_resource_versions WHERE workspace_id = ? AND resource_id = ? FOR UPDATE', [workspaceID, resourceID]);
            const [liveRefs] = await conn.execute(`SELECT refs.id FROM ai_agent_profile_resource_refs refs
        LEFT JOIN ai_agent_resource_versions versions
          ON versions.workspace_id = refs.workspace_id AND versions.id = refs.resource_version_id
        WHERE refs.workspace_id = ?
          AND refs.profile_section = ?
          AND versions.resource_id = ?
        LIMIT 1 FOR UPDATE`, [workspaceID, head.resource_kind === 'skill' ? 'skills' : 'mcp_servers', resourceID]);
            const [activeRuns] = await conn.execute(`SELECT profile_snapshot_json FROM ai_runtime_runs
        WHERE workspace_id = ? AND status IN ('queued', 'running', 'awaiting_decision', 'awaiting_input')
        FOR UPDATE`, [workspaceID]);
            const section = head.resource_kind === 'skill' ? 'skills' : 'mcp_servers';
            const activeExecution = activeRuns.some((row) => {
                const snapshot = parseJSON(row.profile_snapshot_json, {});
                return (snapshot.frozen_resources?.[section] || []).some((item) => item.id === resourceID);
            });
            if (liveRefs.length > 0 || activeExecution) {
                await conn.rollback();
                return 'in_use';
            }
            const [result] = await conn.execute('DELETE FROM ai_agent_resources WHERE workspace_id = ? AND id = ?', [workspaceID, resourceID]);
            if (result.affectedRows !== 1) {
                await conn.rollback();
                return 'not_found';
            }
            await conn.commit();
            return 'deleted';
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async appendAgentResourceVersion(version) {
        assertAgentResourceVersionSnapshotSafe(version);
        await this.pool.execute(agentResourceVersionInsertSQL, agentResourceVersionWriteParams(version));
        return version;
    }
    async getAgentResourceVersion(workspaceID, resourceVersionID) {
        const rows = await this.query('SELECT * FROM ai_agent_resource_versions WHERE workspace_id = ? AND id = ? LIMIT 1', [workspaceID, resourceVersionID]);
        return rows[0] ? this.agentResourceVersionFromRow(rows[0]) : undefined;
    }
    async listAgentResourceVersions(workspaceID, resourceID) {
        const rows = await this.query(`SELECT * FROM ai_agent_resource_versions
      WHERE workspace_id = ? AND resource_id = ?
      ORDER BY revision DESC, id DESC`, [workspaceID, resourceID]);
        return rows.map((row) => this.agentResourceVersionFromRow(row));
    }
    async resolveProviderCredentialRef(workspaceID, providerID, credentialID) {
        const numericCredentialID = Number(credentialID);
        if (!Number.isInteger(numericCredentialID) || numericCredentialID <= 0)
            return undefined;
        const numericProviderID = Number(providerID);
        const hasProviderID = Number.isInteger(numericProviderID) && numericProviderID > 0;
        const params = hasProviderID
            ? [workspaceID, numericProviderID, numericCredentialID]
            : [workspaceID, numericCredentialID];
        const sql = hasProviderID
            ? `SELECT c.id AS credential_id, c.encrypted_payload, mk.key_material
         FROM ai_providers p
         INNER JOIN credentials c
           ON c.id = p.credential_id AND c.workspace_id = p.workspace_id
         INNER JOIN master_keys mk
           ON mk.status = 'active'
         WHERE p.workspace_id = ?
           AND p.id = ?
           AND p.credential_id = ?
           AND p.status = 'active'
           AND c.status = 'active'
         LIMIT 1`
            : `SELECT c.id AS credential_id, c.encrypted_payload, mk.key_material
         FROM credentials c
         INNER JOIN master_keys mk
           ON mk.status = 'active'
         WHERE c.workspace_id = ?
           AND c.id = ?
           AND c.status = 'active'
         LIMIT 1`;
        const rows = await this.query(sql, params);
        const row = rows[0];
        if (!row)
            return undefined;
        const payload = decryptCredentialPayload(row.encrypted_payload, row.key_material);
        const secretRef = providerSecretRef(payload);
        if (Object.keys(secretRef).length === 0)
            return undefined;
        return {
            credential_id: String(row.credential_id),
            secret_ref: secretRef
        };
    }
    async saveProfileVersion(version) {
        await this.pool.execute(`INSERT INTO ai_agent_profile_versions (
        id, profile_id, workspace_id, version, snapshot_hash, snapshot_json,
        status, published_by, published_at, change_summary
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
            version.profile_version_id,
            version.profile_id,
            version.workspace_id,
            version.version,
            version.snapshot_hash,
            json(version.snapshot),
            version.status,
            version.published_by,
            toDBDate(version.published_at),
            version.change_summary
        ]);
        return version;
    }
    async publishProfileVersion(profile, version, expectedUpdatedAt) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            await this.lockProfileReferenceTargets(conn, profile, 'update');
            const [profiles] = await conn.execute('SELECT * FROM ai_agent_profiles WHERE workspace_id = ? AND id = ? FOR UPDATE', [profile.workspace_id, profile.id]);
            const current = profiles[0];
            if (!current)
                throw new RuntimeDomainError('agent_profile_not_found', 'Agent profile not found', 404);
            if ((fromDBDate(current.updated_at) || current.updated_at) !== expectedUpdatedAt) {
                throw new RuntimeDomainError('agent_profile_publish_conflict', 'Agent profile changed during publish', 409);
            }
            const [existingRows] = await conn.execute('SELECT * FROM ai_agent_profile_versions WHERE workspace_id = ? AND snapshot_hash = ? LIMIT 1 FOR UPDATE', [version.workspace_id, version.snapshot_hash]);
            const existing = existingRows[0];
            if (existing && Number(existing.profile_id) !== profile.id) {
                throw new RuntimeDomainError('agent_profile_version_conflict', 'Agent profile snapshot hash already exists', 409);
            }
            await this.writeProfile(conn, profile, 'update', false);
            if (existing) {
                await conn.commit();
                return this.versionFromRow(existing);
            }
            await conn.execute(`INSERT INTO ai_agent_profile_versions (
          id, profile_id, workspace_id, version, snapshot_hash, snapshot_json,
          status, published_by, published_at, change_summary
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                version.profile_version_id,
                version.profile_id,
                version.workspace_id,
                version.version,
                version.snapshot_hash,
                json(version.snapshot),
                version.status,
                version.published_by,
                toDBDate(version.published_at),
                version.change_summary
            ]);
            await conn.commit();
            return version;
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async listProfileVersions(workspaceID, profileID) {
        const rows = await this.query('SELECT * FROM ai_agent_profile_versions WHERE workspace_id = ? AND profile_id = ? ORDER BY version DESC', [workspaceID, profileID]);
        return rows.map(this.versionFromRow);
    }
    async getProfileVersion(workspaceID, profileVersionID) {
        const rows = await this.query('SELECT * FROM ai_agent_profile_versions WHERE workspace_id = ? AND id = ? LIMIT 1', [workspaceID, profileVersionID]);
        return rows[0] ? this.versionFromRow(rows[0]) : undefined;
    }
    async saveSession(session) {
        await this.execute(`INSERT INTO ai_sessions (
        id, workspace_id, context_tags_json, session_kind, user_id, auth_session_id,
        business_type, business_id, status, agent_profile_id, agent_profile_version_id,
        agent_profile_version_key, agent_profile_snapshot_hash, agent_workspace_runtime_id,
        agent_workspace_snapshot_hash, session_key, first_session_timestamp,
        source, model_override_json, title, title_source, title_generated_at, title_generation_error,
        entry_count, last_entry_at, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE
        status=VALUES(status), title=VALUES(title), title_source=VALUES(title_source),
        title_generated_at=VALUES(title_generated_at), title_generation_error=VALUES(title_generation_error),
        entry_count=VALUES(entry_count),
        last_entry_at=VALUES(last_entry_at), model_override_json=VALUES(model_override_json),
        updated_at=VALUES(updated_at)`, [
            session.id,
            session.workspace_id,
            json(session.context_tags),
            session.session_kind,
            session.user_id,
            session.auth_session_id,
            session.business_type,
            session.business_id,
            session.status,
            session.agent_profile_id,
            session.agent_profile_version_id,
            session.agent_profile_version_key || '',
            session.agent_profile_snapshot_hash,
            session.agent_workspace_runtime_id,
            session.agent_workspace_snapshot_hash,
            session.session_key || null,
            session.first_session_timestamp ? toDBDate(session.first_session_timestamp) : null,
            session.source || null,
            json(session.model_override || {}),
            session.title,
            session.title_source || 'fallback',
            session.title_generated_at ? toDBDate(session.title_generated_at) : null,
            session.title_generation_error || null,
            session.entry_count,
            session.last_entry_at ? toDBDate(session.last_entry_at) : null,
            toDBDate(session.created_at),
            toDBDate(session.updated_at)
        ]);
        return session;
    }
    async getSession(workspaceID, sessionID) {
        const rows = await this.query('SELECT * FROM ai_sessions WHERE workspace_id = ? AND id = ? LIMIT 1', [
            workspaceID,
            sessionID
        ]);
        return rows[0] ? this.sessionFromRow(rows[0]) : undefined;
    }
    async listSessions(workspaceID, filter = {}) {
        const where = ['workspace_id = ?'];
        const params = [workspaceID];
        for (const [column, value] of [
            ['business_type', filter.business_type],
            ['business_id', filter.business_id],
            ['status', filter.status]
        ]) {
            if (value) {
                where.push(`${column} = ?`);
                params.push(value);
            }
        }
        if (filter.context_tag) {
            where.push('JSON_CONTAINS(context_tags_json, JSON_QUOTE(?))');
            params.push(filter.context_tag);
        }
        if (filter.context_tags?.length) {
            for (const tag of filter.context_tags) {
                where.push('JSON_CONTAINS(context_tags_json, JSON_QUOTE(?))');
                params.push(tag);
            }
        }
        const rows = await this.query(`SELECT * FROM ai_sessions WHERE ${where.join(' AND ')} ORDER BY updated_at DESC, id DESC LIMIT 100`, params);
        return rows.map(this.sessionFromRow);
    }
    async findCurrentSession(key) {
        const rows = await this.query('SELECT workspace_id, session_id FROM ai_current_sessions WHERE session_key = ? LIMIT 1', [key]);
        if (!rows[0])
            return undefined;
        return this.getSession(Number(rows[0].workspace_id), Number(rows[0].session_id));
    }
    async rememberCurrentSession(key, sessionID) {
        const session = await this.query('SELECT workspace_id FROM ai_sessions WHERE id = ? LIMIT 1', [sessionID]);
        if (!session[0])
            return;
        await this.pool.execute(`INSERT INTO ai_current_sessions (session_key, workspace_id, session_id, updated_at)
      VALUES (?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE workspace_id=VALUES(workspace_id), session_id=VALUES(session_id), updated_at=VALUES(updated_at)`, [key, Number(session[0].workspace_id), sessionID, toDBDate(undefined)]);
    }
    async saveEntry(entry) {
        await this.pool.execute(`INSERT INTO ai_session_entries (
        id, session_id, workspace_id, user_id, parent_entry_id, seq, entry_type, role, status,
        content, content_blocks_json, input_json, output_json, runtime_run_id, event_seq,
        idempotency_key, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE entry_type=VALUES(entry_type), status=VALUES(status), content=VALUES(content),
        content_blocks_json=VALUES(content_blocks_json), input_json=VALUES(input_json),
        output_json=VALUES(output_json), runtime_run_id=VALUES(runtime_run_id),
        event_seq=VALUES(event_seq), updated_at=VALUES(updated_at)`, [
            entry.id,
            entry.session_id,
            entry.workspace_id,
            entry.user_id,
            entry.parent_entry_id ?? null,
            entry.seq,
            entry.entry_type,
            entry.role,
            entry.status,
            entry.content,
            json(entry.content_blocks),
            json(entry.input),
            json(entry.output),
            entry.runtime_run_id ?? null,
            entry.event_seq ?? null,
            entry.idempotency_key,
            toDBDate(entry.created_at),
            toDBDate(entry.updated_at)
        ]);
        return entry;
    }
    async listEntries(workspaceID, sessionID) {
        const rows = await this.query('SELECT * FROM ai_session_entries WHERE workspace_id = ? AND session_id = ? ORDER BY seq ASC', [workspaceID, sessionID]);
        return rows.map(this.entryFromRow);
    }
    async enqueueSessionQueueItem(input, activeItemLimit) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [sessionRows] = await conn.execute('SELECT id FROM ai_sessions WHERE workspace_id = ? AND id = ? FOR UPDATE', [input.workspace_id, input.session_id]);
            if (sessionRows.length === 0) {
                throw new RuntimeDomainError('ai_session_not_found', 'AI session not found', 404);
            }
            const [existingRows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND client_item_id = ? LIMIT 1`, [input.workspace_id, input.session_id, input.client_item_id]);
            if (existingRows[0]) {
                await conn.commit();
                return { outcome: 'existing', item: this.sessionQueueItemFromRow(existingRows[0]) };
            }
            const [aggregateRows] = await conn.execute(`SELECT
          SUM(CASE WHEN status IN ('pending', 'claimed') THEN 1 ELSE 0 END) AS active_count,
          COALESCE(MAX(item_seq), 0) AS max_item_seq,
          COALESCE(MAX(CASE WHEN status IN ('pending', 'claimed') THEN position ELSE 0 END), 0) AS max_position
        FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ?`, [input.workspace_id, input.session_id]);
            const aggregate = aggregateRows[0] || {};
            if (Number(aggregate.active_count || 0) >= activeItemLimit) {
                await conn.commit();
                return { outcome: 'capacity_exceeded' };
            }
            const itemSeq = Number(aggregate.max_item_seq || 0) + 1;
            const item = {
                queue_item_id: sessionQueueItemBusinessID(input.workspace_id, input.session_id, itemSeq),
                workspace_id: input.workspace_id,
                session_id: input.session_id,
                item_seq: itemSeq,
                position: Number(aggregate.max_position || 0) + 1,
                mode: input.mode,
                content: input.content,
                attachments: input.attachments,
                context_ref: input.context_ref,
                target_run_id: input.target_run_id,
                expires_at: input.expires_at,
                status: 'pending',
                client_item_id: input.client_item_id,
                claim_epoch: 0,
                created_by: input.created_by,
                created_at: input.created_at,
                updated_at: input.created_at
            };
            await conn.execute(`INSERT INTO ai_session_queue_items (
          queue_item_id, workspace_id, session_id, item_seq, position, mode, content,
          attachments_json, context_ref_json, target_run_id, status, client_item_id,
          claimed_by, claim_epoch, claim_expires_at, expires_at, consumed_runtime_run_id,
          error_code, error_msg, created_by, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                item.queue_item_id,
                item.workspace_id,
                item.session_id,
                item.item_seq,
                item.position,
                item.mode,
                item.content,
                json(item.attachments),
                json(item.context_ref),
                item.target_run_id ?? null,
                item.status,
                item.client_item_id,
                null,
                item.claim_epoch,
                null,
                item.expires_at ? toDBDate(item.expires_at) : null,
                item.consumed_runtime_run_id ?? null,
                null,
                null,
                item.created_by,
                toDBDate(item.created_at),
                toDBDate(item.updated_at)
            ]);
            const event = input.lifecycle_event
                ? await this.appendSessionQueueEventInTransaction(conn, input.lifecycle_event, item)
                : undefined;
            await conn.commit();
            return { outcome: 'created', item, event, event_committed: event ? true : undefined };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async listSessionQueueItems(workspaceID, sessionID) {
        const rows = await this.query(`SELECT * FROM ai_session_queue_items
      WHERE workspace_id = ? AND session_id = ?
      ORDER BY CASE WHEN status IN ('pending', 'claimed') THEN 0 ELSE 1 END ASC, position ASC, item_seq ASC`, [workspaceID, sessionID]);
        return rows.map(this.sessionQueueItemFromRow);
    }
    async cancelSessionQueueItem(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            await conn.execute('SELECT id FROM ai_sessions WHERE workspace_id = ? AND id = ? FOR UPDATE', [input.workspace_id, input.session_id]);
            const [rows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, input.queue_item_id]);
            const current = rows[0];
            if (!current) {
                await conn.commit();
                return { outcome: 'not_found' };
            }
            const item = this.sessionQueueItemFromRow(current);
            if (item.status === 'cancelled') {
                await conn.commit();
                return { outcome: 'already_cancelled', item };
            }
            if (item.status !== 'pending') {
                await conn.commit();
                return { outcome: 'not_cancellable' };
            }
            await conn.execute(`UPDATE ai_session_queue_items SET status = 'cancelled', updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? AND status = 'pending'`, [toDBDate(input.updated_at), input.workspace_id, input.session_id, input.queue_item_id]);
            const cancelled = { ...item, status: 'cancelled', updated_at: input.updated_at };
            const event = input.lifecycle_event
                ? await this.appendSessionQueueEventInTransaction(conn, input.lifecycle_event, cancelled)
                : undefined;
            await conn.commit();
            return { outcome: 'cancelled', item: cancelled, event, event_committed: event ? true : undefined };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async reorderSessionQueueItems(workspaceID, sessionID, queueItemIDs, updatedAt) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            await conn.execute('SELECT id FROM ai_sessions WHERE workspace_id = ? AND id = ? FOR UPDATE', [workspaceID, sessionID]);
            const [rows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND status = 'pending'
        ORDER BY position ASC, item_seq ASC FOR UPDATE`, [workspaceID, sessionID]);
            const items = rows.map(this.sessionQueueItemFromRow);
            if (!sameSessionQueueItemSet(items.map((item) => item.queue_item_id), queueItemIDs)) {
                await conn.commit();
                return { outcome: 'conflict' };
            }
            const byID = new Map(items.map((item) => [item.queue_item_id, item]));
            const reordered = queueItemIDs.map((queueItemID, index) => ({
                ...byID.get(queueItemID),
                position: index + 1,
                updated_at: updatedAt
            }));
            for (const item of reordered) {
                await conn.execute(`UPDATE ai_session_queue_items SET position = ?, updated_at = ?
          WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? AND status = 'pending'`, [item.position, toDBDate(updatedAt), workspaceID, sessionID, item.queue_item_id]);
            }
            await conn.commit();
            return { outcome: 'reordered', items: reordered };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async claimNextSessionQueueItem(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            await conn.execute('SELECT id FROM ai_sessions WHERE workspace_id = ? AND id = ? FOR UPDATE', [input.workspace_id, input.session_id]);
            const params = [input.workspace_id, input.session_id, toDBDate(input.updated_at)];
            let sql = `SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ?
          AND (status = 'pending' OR (status = 'claimed' AND claim_expires_at <= ?))`;
            if (input.queue_item_id) {
                sql += ' AND queue_item_id = ?';
                params.push(input.queue_item_id);
            }
            sql += ' ORDER BY position ASC, item_seq ASC LIMIT 1 FOR UPDATE';
            const [rows] = await conn.execute(sql, params);
            const current = rows[0];
            if (!current) {
                await conn.commit();
                return { outcome: 'empty' };
            }
            const item = this.sessionQueueItemFromRow(current);
            const claimEpoch = Number(item.claim_epoch || 0) + 1;
            await conn.execute(`UPDATE ai_session_queue_items
        SET status = 'claimed', claimed_by = ?, claim_epoch = ?, claim_expires_at = ?, updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ?
          AND (status = 'pending' OR (status = 'claimed' AND claim_expires_at <= ?))`, [
                input.claimed_by,
                claimEpoch,
                toDBDate(input.claim_expires_at),
                toDBDate(input.updated_at),
                input.workspace_id,
                input.session_id,
                item.queue_item_id,
                toDBDate(input.updated_at)
            ]);
            const claimed = {
                ...item,
                status: 'claimed',
                claimed_by: input.claimed_by,
                claim_epoch: claimEpoch,
                claim_expires_at: input.claim_expires_at,
                updated_at: input.updated_at
            };
            const event = input.lifecycle_event
                ? await this.appendSessionQueueEventInTransaction(conn, input.lifecycle_event, claimed)
                : undefined;
            await conn.commit();
            return {
                outcome: 'claimed',
                item: claimed,
                event,
                event_committed: event ? true : undefined
            };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async applySessionQueueSteer(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [itemRows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, input.queue_item_id]);
            const itemRow = itemRows[0];
            if (!itemRow) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const item = this.sessionQueueItemFromRow(itemRow);
            const [runRows] = await conn.execute(`SELECT * FROM ai_runtime_runs
        WHERE workspace_id = ? AND session_id = ? AND runtime_run_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, input.runtime_run_id]);
            const runRow = runRows[0];
            if (!runRow) {
                await conn.commit();
                return { outcome: 'target_not_active' };
            }
            const run = this.runFromRow(runRow);
            if (!activeSlotForRunStatus(run.status)) {
                await conn.commit();
                return { outcome: 'target_not_active' };
            }
            if (item.status === 'applied') {
                await conn.commit();
                return { outcome: 'already_applied', item, run };
            }
            if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            await conn.execute(`UPDATE ai_runtime_runs SET result_json = ?, updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND runtime_run_id = ? AND active_slot = 'active'`, [json(input.run_result), toDBDate(input.updated_at), input.workspace_id, input.session_id, input.runtime_run_id]);
            await conn.execute(`UPDATE ai_session_queue_items SET status = 'applied', updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ?
          AND status = 'claimed' AND claimed_by = ? AND claim_epoch = ?`, [toDBDate(input.updated_at), input.workspace_id, input.session_id, input.queue_item_id, input.claimed_by, input.claim_epoch]);
            const appliedItem = { ...item, status: 'applied', updated_at: input.updated_at };
            const event = input.lifecycle_event
                ? await this.appendSessionQueueEventInTransaction(conn, input.lifecycle_event, appliedItem)
                : undefined;
            await conn.commit();
            return {
                outcome: 'applied',
                item: appliedItem,
                run: { ...run, result: input.run_result, updated_at: input.updated_at },
                event,
                event_committed: event ? true : undefined
            };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async transitionSessionQueueItem(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [rows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? LIMIT 1 FOR UPDATE`, [input.item.workspace_id, input.item.session_id, input.item.queue_item_id]);
            const currentRow = rows[0];
            if (!currentRow) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const current = this.sessionQueueItemFromRow(currentRow);
            if (current.status !== 'claimed' || current.claimed_by !== input.claimed_by || current.claim_epoch !== input.claim_epoch) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            await conn.execute(`UPDATE ai_session_queue_items
        SET status = ?, claimed_by = ?, claim_expires_at = ?, error_code = ?, error_msg = ?, updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ?
          AND status = 'claimed' AND claimed_by = ? AND claim_epoch = ?`, [
                input.item.status,
                input.item.claimed_by ?? null,
                input.item.claim_expires_at ? toDBDate(input.item.claim_expires_at) : null,
                input.item.error_code ?? null,
                input.item.error_msg ?? null,
                toDBDate(input.item.updated_at),
                input.item.workspace_id,
                input.item.session_id,
                input.item.queue_item_id,
                input.claimed_by,
                input.claim_epoch
            ]);
            const event = input.lifecycle_event
                ? await this.appendSessionQueueEventInTransaction(conn, input.lifecycle_event, input.item)
                : undefined;
            await conn.commit();
            return { outcome: 'transitioned', item: input.item, event, event_committed: event ? true : undefined };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async consumeSessionQueueItem(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [itemRows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, input.queue_item_id]);
            const itemRow = itemRows[0];
            if (!itemRow) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const item = this.sessionQueueItemFromRow(itemRow);
            const [runRows] = await conn.execute(`SELECT * FROM ai_runtime_runs
        WHERE workspace_id = ? AND session_id = ? AND runtime_run_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, input.consumed_runtime_run_id]);
            const runRow = runRows[0];
            if (!runRow) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const run = this.runFromRow(runRow);
            if (item.status === 'consumed') {
                await conn.commit();
                return { outcome: 'already_consumed', item, run };
            }
            if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const [activeRows] = await conn.execute(`SELECT runtime_run_id FROM ai_runtime_runs
        WHERE workspace_id = ? AND session_id = ? AND active_slot = 'active' AND runtime_run_id <> ? LIMIT 1`, [input.workspace_id, input.session_id, input.consumed_runtime_run_id]);
            if (activeRows[0]) {
                await conn.commit();
                return { outcome: 'active_run_conflict' };
            }
            await conn.execute(`UPDATE ai_session_queue_items
        SET status = 'consumed', consumed_runtime_run_id = ?, updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ?
          AND status = 'claimed' AND claimed_by = ? AND claim_epoch = ?`, [input.consumed_runtime_run_id, toDBDate(input.updated_at), input.workspace_id, input.session_id, input.queue_item_id, input.claimed_by, input.claim_epoch]);
            await conn.commit();
            return {
                outcome: 'consumed',
                item: { ...item, status: 'consumed', consumed_runtime_run_id: input.consumed_runtime_run_id, updated_at: input.updated_at },
                run
            };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async consumeSessionQueueItemWithRun(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            await conn.execute('SELECT id FROM ai_sessions WHERE workspace_id = ? AND id = ? LIMIT 1 FOR UPDATE', [input.workspace_id, input.session_id]);
            const [itemRows] = await conn.execute(`SELECT * FROM ai_session_queue_items
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, input.queue_item_id]);
            const itemRow = itemRows[0];
            if (!itemRow) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const item = this.sessionQueueItemFromRow(itemRow);
            if (item.status === 'consumed') {
                const existingRunID = item.consumed_runtime_run_id;
                if (!existingRunID) {
                    await conn.commit();
                    return { outcome: 'stale_claim' };
                }
                const [existingRunRows] = await conn.execute(`SELECT * FROM ai_runtime_runs
          WHERE workspace_id = ? AND session_id = ? AND runtime_run_id = ? LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id, existingRunID]);
                const existingRunRow = existingRunRows[0];
                await conn.commit();
                return existingRunRow
                    ? { outcome: 'already_consumed', item, run: this.runFromRow(existingRunRow) }
                    : { outcome: 'stale_claim' };
            }
            if (item.status !== 'claimed' || item.claimed_by !== input.claimed_by || Number(item.claim_epoch || 0) !== input.claim_epoch) {
                await conn.commit();
                return { outcome: 'stale_claim' };
            }
            const [activeRows] = await conn.execute(`SELECT runtime_run_id FROM ai_runtime_runs
        WHERE workspace_id = ? AND session_id = ? AND active_slot = 'active' LIMIT 1 FOR UPDATE`, [input.workspace_id, input.session_id]);
            if (activeRows[0]) {
                await conn.commit();
                return { outcome: 'active_run_conflict' };
            }
            const run = normalizeRuntimeRunActiveSlot(input.run);
            const runParams = [
                run.id, run.runtime_run_id, run.session_id, run.workspace_id, json(run.context_tags),
                run.agent_profile_id, run.agent_profile_version_id, run.agent_profile_version_key || '',
                run.agent_profile_snapshot_hash, run.agent_workspace_runtime_id ?? null, run.agent_workspace_snapshot_hash ?? null,
                json(run.profile_snapshot || {}), run.status, activeSlotDBValue(run.status),
                run.owner_instance_id ?? null, Number(run.owner_epoch || 0),
                run.owner_lease_expires_at ? toDBDate(run.owner_lease_expires_at) : null, run.input_entry_id,
                run.output_entry_id ?? null, json(run.request), json(run.result), json(run.usage),
                run.error_code ?? null, run.error_msg ?? null, toDBDate(run.started_at),
                run.finished_at ? toDBDate(run.finished_at) : null, toDBDate(run.created_at), toDBDate(run.updated_at)
            ];
            await conn.execute(`INSERT INTO ai_runtime_runs (
          id, runtime_run_id, session_id, workspace_id, context_tags_json,
          agent_profile_id, agent_profile_version_id, agent_profile_version_key,
          agent_profile_snapshot_hash, agent_workspace_runtime_id, agent_workspace_snapshot_hash,
          profile_snapshot_json, status, active_slot,
          owner_instance_id, owner_epoch, owner_lease_expires_at, input_entry_id,
          output_entry_id, request_json, result_json,
          usage_json, error_code, error_msg, started_at, finished_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, runParams);
            for (const entry of [input.user_entry, input.assistant_entry]) {
                await conn.execute(`INSERT INTO ai_session_entries (
            id, session_id, workspace_id, user_id, parent_entry_id, seq, entry_type, role, status,
            content, content_blocks_json, input_json, output_json, runtime_run_id, event_seq,
            idempotency_key, created_at, updated_at
          ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                    entry.id, entry.session_id, entry.workspace_id, entry.user_id, entry.parent_entry_id ?? null,
                    entry.seq, entry.entry_type, entry.role, entry.status, entry.content, json(entry.content_blocks),
                    json(entry.input), json(entry.output), entry.runtime_run_id ?? null, entry.event_seq ?? null,
                    entry.idempotency_key, toDBDate(entry.created_at), toDBDate(entry.updated_at)
                ]);
            }
            await conn.execute(`UPDATE ai_sessions SET entry_count = ?, last_entry_at = ?, updated_at = ?
        WHERE workspace_id = ? AND id = ?`, [
                input.updated_session_progress.entry_count,
                input.updated_session_progress.last_entry_at ? toDBDate(input.updated_session_progress.last_entry_at) : null,
                toDBDate(input.updated_session_progress.updated_at),
                input.workspace_id,
                input.session_id
            ]);
            const consumed = {
                ...item,
                status: 'consumed',
                consumed_runtime_run_id: run.runtime_run_id,
                updated_at: input.follow_up_started_event.timestamp
            };
            await conn.execute(`UPDATE ai_session_queue_items
        SET status = 'consumed', consumed_runtime_run_id = ?, updated_at = ?
        WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ?
          AND status = 'claimed' AND claimed_by = ? AND claim_epoch = ?`, [run.runtime_run_id, toDBDate(consumed.updated_at), input.workspace_id, input.session_id, input.queue_item_id, input.claimed_by, input.claim_epoch]);
            const event = await this.appendFollowUpStartedEventInTransaction(conn, {
                ...input.follow_up_started_event,
                runtime_run_id: run.runtime_run_id,
                consumed_runtime_run_id: run.runtime_run_id,
                queue_item: consumed
            });
            await conn.commit();
            return {
                outcome: 'consumed',
                item: consumed,
                run,
                user_entry: input.user_entry,
                assistant_entry: input.assistant_entry,
                follow_up_started_event: event,
                event_committed: true
            };
        }
        catch (error) {
            await conn.rollback();
            if (isDuplicateActiveRunError(error))
                return { outcome: 'active_run_conflict' };
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async updateSessionQueueItem(item) {
        const existing = await this.query(`SELECT * FROM ai_session_queue_items
      WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ? LIMIT 1`, [item.workspace_id, item.session_id, item.queue_item_id]);
        if (!existing[0]) {
            throw new RuntimeDomainError('queue_item_not_found', 'Session queue item not found', 404);
        }
        await this.execute(`UPDATE ai_session_queue_items
      SET position = ?, mode = ?, content = ?, attachments_json = ?, context_ref_json = ?,
        target_run_id = ?, status = ?, claimed_by = ?, claim_epoch = ?, claim_expires_at = ?,
        expires_at = ?, consumed_runtime_run_id = ?, error_code = ?, error_msg = ?, updated_at = ?
      WHERE workspace_id = ? AND session_id = ? AND queue_item_id = ?`, [
            item.position,
            item.mode,
            item.content,
            json(item.attachments),
            json(item.context_ref),
            item.target_run_id ?? null,
            item.status,
            item.claimed_by ?? null,
            item.claim_epoch,
            item.claim_expires_at ? toDBDate(item.claim_expires_at) : null,
            item.expires_at ? toDBDate(item.expires_at) : null,
            item.consumed_runtime_run_id ?? null,
            item.error_code ?? null,
            item.error_msg ?? null,
            toDBDate(item.updated_at),
            item.workspace_id,
            item.session_id,
            item.queue_item_id
        ]);
        return item;
    }
    async saveRun(run) {
        const normalized = normalizeRuntimeRunActiveSlot(run);
        const activeSlot = activeSlotDBValue(normalized.status);
        const existing = await this.getRun(normalized.workspace_id, normalized.runtime_run_id);
        try {
            if (existing) {
                await this.execute(`UPDATE ai_runtime_runs
          SET context_tags_json = ?, agent_profile_id = ?, agent_profile_version_id = ?,
            agent_profile_version_key = ?, agent_profile_snapshot_hash = ?, agent_workspace_runtime_id = ?,
            agent_workspace_snapshot_hash = ?, profile_snapshot_json = ?,
            status = ?, active_slot = ?, owner_instance_id = ?, owner_epoch = ?, owner_lease_expires_at = ?, input_entry_id = ?, output_entry_id = ?,
            request_json = ?, result_json = ?, usage_json = ?, error_code = ?, error_msg = ?,
            started_at = ?, finished_at = ?, updated_at = ?
          WHERE workspace_id = ? AND runtime_run_id = ?`, [
                    json(normalized.context_tags),
                    normalized.agent_profile_id,
                    normalized.agent_profile_version_id,
                    normalized.agent_profile_version_key || '',
                    normalized.agent_profile_snapshot_hash,
                    normalized.agent_workspace_runtime_id,
                    normalized.agent_workspace_snapshot_hash,
                    json(normalized.profile_snapshot || {}),
                    normalized.status,
                    activeSlot,
                    normalized.owner_instance_id ?? null,
                    Number(normalized.owner_epoch || 0),
                    normalized.owner_lease_expires_at ? toDBDate(normalized.owner_lease_expires_at) : null,
                    normalized.input_entry_id,
                    normalized.output_entry_id ?? null,
                    json(normalized.request),
                    json(normalized.result),
                    json(normalized.usage),
                    normalized.error_code ?? null,
                    normalized.error_msg ?? null,
                    toDBDate(normalized.started_at),
                    normalized.finished_at ? toDBDate(normalized.finished_at) : null,
                    toDBDate(normalized.updated_at),
                    normalized.workspace_id,
                    normalized.runtime_run_id
                ]);
            }
            else {
                await this.execute(`INSERT INTO ai_runtime_runs (
            id, runtime_run_id, session_id, workspace_id, context_tags_json,
            agent_profile_id, agent_profile_version_id, agent_profile_version_key,
            agent_profile_snapshot_hash, agent_workspace_runtime_id, agent_workspace_snapshot_hash,
            profile_snapshot_json, status, active_slot,
            owner_instance_id, owner_epoch, owner_lease_expires_at, input_entry_id,
            output_entry_id, request_json, result_json,
            usage_json, error_code, error_msg, started_at, finished_at, created_at, updated_at
          ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                    normalized.id,
                    normalized.runtime_run_id,
                    normalized.session_id,
                    normalized.workspace_id,
                    json(normalized.context_tags),
                    normalized.agent_profile_id,
                    normalized.agent_profile_version_id,
                    normalized.agent_profile_version_key || '',
                    normalized.agent_profile_snapshot_hash,
                    normalized.agent_workspace_runtime_id,
                    normalized.agent_workspace_snapshot_hash,
                    json(normalized.profile_snapshot || {}),
                    normalized.status,
                    activeSlot,
                    normalized.owner_instance_id ?? null,
                    Number(normalized.owner_epoch || 0),
                    normalized.owner_lease_expires_at ? toDBDate(normalized.owner_lease_expires_at) : null,
                    normalized.input_entry_id,
                    normalized.output_entry_id ?? null,
                    json(normalized.request),
                    json(normalized.result),
                    json(normalized.usage),
                    normalized.error_code ?? null,
                    normalized.error_msg ?? null,
                    toDBDate(normalized.started_at),
                    normalized.finished_at ? toDBDate(normalized.finished_at) : null,
                    toDBDate(normalized.created_at),
                    toDBDate(normalized.updated_at)
                ]);
            }
        }
        catch (error) {
            if (isDuplicateActiveRunError(error)) {
                const activeRun = await this.findActiveRun(normalized.workspace_id, normalized.session_id);
                throw new RuntimeDomainError('active_run_conflict', `Session already has active runtime run ${activeRun?.runtime_run_id || 'unknown'} in status ${activeRun?.status || 'active'}`, 409, {
                    active_run: activeRun
                        ? {
                            runtime_run_id: activeRun.runtime_run_id,
                            status: activeRun.status,
                            next_steps: activeRun.status === 'awaiting_decision' ? ['approve_once', 'approve_session', 'reject', 'cancel'] : ['wait', 'cancel']
                        }
                        : { status: 'active' }
                });
            }
            throw error;
        }
        return normalized;
    }
    async getRun(workspaceID, runtimeRunID) {
        const rows = await this.query('SELECT * FROM ai_runtime_runs WHERE workspace_id = ? AND runtime_run_id = ? LIMIT 1', [workspaceID, runtimeRunID]);
        return rows[0] ? this.runFromRow(rows[0]) : undefined;
    }
    async findActiveRun(workspaceID, sessionID) {
        const rows = await this.query(`SELECT * FROM ai_runtime_runs
      WHERE workspace_id = ? AND session_id = ? AND status IN ('queued', 'running', 'awaiting_decision', 'awaiting_input')
      ORDER BY created_at DESC, id DESC LIMIT 1`, [workspaceID, sessionID]);
        return rows[0] ? this.runFromRow(rows[0]) : undefined;
    }
    async claimRunLease(workspaceID, runtimeRunID, instanceID, leaseExpiresAt, observedAt = new Date().toISOString()) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_runs
      SET owner_epoch = IF(owner_instance_id = ? AND owner_lease_expires_at > ?, GREATEST(owner_epoch, 1), owner_epoch + 1),
          owner_instance_id = ?, owner_lease_expires_at = ?, updated_at = ?
      WHERE workspace_id = ? AND runtime_run_id = ?
        AND status IN ('queued', 'running', 'awaiting_decision', 'awaiting_input')
        AND (owner_instance_id IS NULL OR owner_instance_id = ? OR owner_lease_expires_at IS NULL OR owner_lease_expires_at <= ?)`, [instanceID, toDBDate(observedAt), instanceID, toDBDate(leaseExpiresAt), toDBDate(observedAt), workspaceID, runtimeRunID, instanceID, toDBDate(observedAt)]);
        return result.affectedRows > 0 ? this.getRun(workspaceID, runtimeRunID) : undefined;
    }
    async renewRunLease(workspaceID, runtimeRunID, instanceID, ownerEpoch, leaseExpiresAt) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_runs
      SET owner_lease_expires_at = ?, updated_at = CURRENT_TIMESTAMP(3)
      WHERE workspace_id = ? AND runtime_run_id = ? AND owner_instance_id = ? AND owner_epoch = ?
        AND status IN ('queued', 'running', 'awaiting_decision', 'awaiting_input')`, [toDBDate(leaseExpiresAt), workspaceID, runtimeRunID, instanceID, ownerEpoch]);
        return result.affectedRows > 0 ? this.getRun(workspaceID, runtimeRunID) : undefined;
    }
    async releaseRunLease(workspaceID, runtimeRunID, instanceID, ownerEpoch) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_runs
      SET owner_instance_id = NULL, owner_lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP(3)
      WHERE workspace_id = ? AND runtime_run_id = ? AND owner_instance_id = ? AND owner_epoch = ?`, [workspaceID, runtimeRunID, instanceID, ownerEpoch]);
        return result.affectedRows > 0;
    }
    async interruptExpiredRunLeases(observedAt) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [rows] = await conn.query(`SELECT * FROM ai_runtime_runs
        WHERE status IN ('queued', 'running', 'awaiting_input')
          AND owner_instance_id IS NOT NULL AND owner_lease_expires_at IS NOT NULL AND owner_lease_expires_at <= ?
        FOR UPDATE`, [toDBDate(observedAt)]);
            if (rows.length === 0) {
                await conn.commit();
                return [];
            }
            const ids = rows.map((row) => Number(row.id));
            await conn.query(`UPDATE ai_runtime_runs
        SET status = 'interrupted', active_slot = NULL, owner_instance_id = NULL, owner_lease_expires_at = NULL,
            error_code = 'runtime_owner_lease_expired',
            error_msg = 'Runtime owner lease expired',
            finished_at = ?, updated_at = ?
        WHERE id IN (${ids.map(() => '?').join(',')})`, [toDBDate(observedAt), toDBDate(observedAt), ...ids]);
            await conn.commit();
            return rows.map((row) => normalizeRuntimeRunActiveSlot({
                ...this.runFromRow(row),
                status: 'interrupted',
                owner_instance_id: undefined,
                owner_lease_expires_at: undefined,
                error_code: 'runtime_owner_lease_expired',
                error_msg: `Runtime owner ${row.owner_instance_id} lease expired`,
                finished_at: observedAt,
                updated_at: observedAt
            }));
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async getRuntimeOperationsSummary(observedAt) {
        const fiveMinutesAgo = new Date(Date.parse(observedAt) - 5 * 60_000).toISOString();
        const oneHourAgo = new Date(Date.parse(observedAt) - 60 * 60_000).toISOString();
        const rows = await this.query(`SELECT
        SUM(status IN ('queued', 'running', 'awaiting_decision', 'awaiting_input')) AS active,
        SUM(status = 'awaiting_decision') AS awaiting_approval,
        SUM(status = 'awaiting_input') AS awaiting_input,
        SUM(status IN ('queued', 'running')
          AND owner_lease_expires_at IS NOT NULL AND owner_lease_expires_at <= bounds.observed_at) AS stale,
        SUM(status IN ('queued', 'running')
          AND (owner_instance_id IS NULL OR owner_lease_expires_at IS NULL)) AS orphaned,
        SUM(status = 'completed' AND COALESCE(finished_at, updated_at) BETWEEN bounds.five_minutes_ago AND bounds.observed_at) AS completed_5m,
        SUM(status = 'failed' AND COALESCE(finished_at, updated_at) BETWEEN bounds.five_minutes_ago AND bounds.observed_at) AS failed_5m,
        SUM(status = 'cancelled' AND COALESCE(finished_at, updated_at) BETWEEN bounds.five_minutes_ago AND bounds.observed_at) AS cancelled_5m,
        SUM(status = 'timeout' AND COALESCE(finished_at, updated_at) BETWEEN bounds.five_minutes_ago AND bounds.observed_at) AS timeout_5m,
        SUM(status = 'interrupted' AND COALESCE(finished_at, updated_at) BETWEEN bounds.five_minutes_ago AND bounds.observed_at) AS interrupted_5m,
        SUM(status = 'completed' AND COALESCE(finished_at, updated_at) BETWEEN bounds.one_hour_ago AND bounds.observed_at) AS completed_1h,
        SUM(status = 'failed' AND COALESCE(finished_at, updated_at) BETWEEN bounds.one_hour_ago AND bounds.observed_at) AS failed_1h,
        SUM(status = 'cancelled' AND COALESCE(finished_at, updated_at) BETWEEN bounds.one_hour_ago AND bounds.observed_at) AS cancelled_1h,
        SUM(status = 'timeout' AND COALESCE(finished_at, updated_at) BETWEEN bounds.one_hour_ago AND bounds.observed_at) AS timeout_1h,
        SUM(status = 'interrupted' AND COALESCE(finished_at, updated_at) BETWEEN bounds.one_hour_ago AND bounds.observed_at) AS interrupted_1h
      FROM ai_runtime_runs
      CROSS JOIN (SELECT ? AS observed_at, ? AS five_minutes_ago, ? AS one_hour_ago) bounds`, [toDBDate(observedAt), toDBDate(fiveMinutesAgo), toDBDate(oneHourAgo)]);
        const row = rows[0] || {};
        const failures = await this.query(`SELECT
        COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(result_json, '$.error.category')), ''),
          IF(status = 'timeout', 'provider_timeout', 'internal')) AS category,
        COALESCE(NULLIF(error_code, ''), 'runtime_error') AS code,
        COUNT(*) AS count
      FROM ai_runtime_runs
      WHERE status IN ('failed', 'timeout', 'interrupted')
        AND COALESCE(finished_at, updated_at) >= ?
        AND COALESCE(finished_at, updated_at) <= ?
      GROUP BY category, code
      ORDER BY category ASC, code ASC`, [toDBDate(oneHourAgo), toDBDate(observedAt)]);
        const terminal = (window) => ({
            completed: Number(row[`completed_${window}`] || 0),
            failed: Number(row[`failed_${window}`] || 0),
            cancelled: Number(row[`cancelled_${window}`] || 0),
            timeout: Number(row[`timeout_${window}`] || 0),
            interrupted: Number(row[`interrupted_${window}`] || 0)
        });
        return {
            observed_at: observedAt,
            runs: {
                active: Number(row.active || 0),
                awaiting_approval: Number(row.awaiting_approval || 0),
                awaiting_input: Number(row.awaiting_input || 0),
                stale: Number(row.stale || 0),
                orphaned: Number(row.orphaned || 0)
            },
            terminal_5m: terminal('5m'),
            terminal_1h: terminal('1h'),
            failures_1h: failures.map((failure) => ({
                category: String(failure.category || 'internal'),
                code: String(failure.code || 'runtime_error'),
                count: Number(failure.count || 0)
            }))
        };
    }
    async saveAction(action) {
        await this.pool.execute(`INSERT INTO ai_agent_actions (
        id, action_id, workspace_id, context_tags_json, session_id, entry_id, runtime_run_id,
        action_kind, idempotency_key, source, capability_id, input_json, input_digest, target_json,
        policy_json, display_json, status, requested_by, decided_by, decided_at,
        executed_at, result_json, error_msg, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE status=VALUES(status), decided_by=VALUES(decided_by),
        decided_at=VALUES(decided_at), executed_at=VALUES(executed_at),
        input_json=VALUES(input_json), input_digest=VALUES(input_digest), target_json=VALUES(target_json),
        policy_json=VALUES(policy_json), display_json=VALUES(display_json),
        result_json=VALUES(result_json),
        error_msg=VALUES(error_msg), updated_at=VALUES(updated_at)`, [
            action.id,
            action.action_id,
            action.workspace_id,
            json(action.context_tags),
            action.session_id,
            action.entry_id ?? null,
            action.runtime_run_id,
            action.action_kind,
            action.idempotency_key,
            action.source,
            action.capability_id,
            json(action.input_json),
            action.input_digest,
            json(action.target_json),
            json(action.policy_json),
            json(action.display_json),
            action.status,
            action.requested_by,
            action.decided_by ?? null,
            action.decided_at ? toDBDate(action.decided_at) : null,
            action.executed_at ? toDBDate(action.executed_at) : null,
            json(action.result_json),
            action.error_msg ?? null,
            toDBDate(action.created_at),
            toDBDate(action.updated_at)
        ]);
        return action;
    }
    async createToolAction(action) {
        await this.pool.execute(`INSERT INTO ai_agent_actions (
        id, action_id, workspace_id, context_tags_json, session_id, entry_id, runtime_run_id,
        action_kind, idempotency_key, source, capability_id, input_json, input_digest, target_json,
        policy_json, display_json, status, requested_by, decided_by, decided_at,
        executed_at, result_json, error_msg, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, [
            action.id,
            action.action_id,
            action.workspace_id,
            json(action.context_tags),
            action.session_id,
            action.entry_id ?? null,
            action.runtime_run_id,
            action.action_kind,
            action.idempotency_key,
            action.source,
            action.capability_id,
            json(action.input_json),
            action.input_digest,
            json(action.target_json),
            json(action.policy_json),
            json(action.display_json),
            action.status,
            action.requested_by,
            action.decided_by ?? null,
            action.decided_at ? toDBDate(action.decided_at) : null,
            action.executed_at ? toDBDate(action.executed_at) : null,
            json(action.result_json),
            action.error_msg ?? null,
            toDBDate(action.created_at),
            toDBDate(action.updated_at)
        ]);
        const stored = await this.findActionByIdentity(action.workspace_id, action.action_id, action.idempotency_key);
        if (!stored)
            return { outcome: 'created', action };
        return { outcome: stored.id === action.id ? 'created' : 'existing', action: stored };
    }
    async decideAwaitingAction(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [rows] = await conn.execute('SELECT * FROM ai_agent_actions WHERE workspace_id = ? AND id = ? FOR UPDATE', [input.workspace_id, input.action_id]);
            const current = rows[0];
            if (!current) {
                await conn.commit();
                return { outcome: 'not_found' };
            }
            const action = this.actionFromRow(current);
            if (action.status !== 'awaiting_decision') {
                await conn.commit();
                return { outcome: 'already_decided', action };
            }
            await conn.execute(`INSERT INTO ai_action_decisions (
          decision_id, decision_seq, workspace_id, session_id, runtime_run_id, action_id,
          decision_type, decision, actor_user_id, reason, input_patch_json, client_decision_id,
          idempotency_key, created_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                input.decision.decision_id,
                input.decision.decision_seq,
                input.decision.workspace_id,
                input.decision.session_id,
                input.decision.runtime_run_id,
                input.decision.action_id,
                input.decision.decision_type,
                input.decision.decision,
                input.decision.actor_user_id ?? null,
                input.decision.reason,
                json(input.decision.input_patch_json),
                input.decision.client_decision_id,
                input.decision.idempotency_key,
                toDBDate(input.decision.created_at)
            ]);
            if (input.session_grant) {
                await conn.execute(`INSERT INTO ai_session_permission_grants (
            grant_id, grant_seq, workspace_id, session_id, runtime_run_id, action_id,
            permission_key, tool_name, executor_type, resource_type, resource_id,
            status, granted_by, created_at, expires_at, revoked_at
          ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                    input.session_grant.grant_id,
                    input.session_grant.grant_seq,
                    input.session_grant.workspace_id,
                    input.session_grant.session_id,
                    input.session_grant.runtime_run_id,
                    input.session_grant.action_id,
                    input.session_grant.permission_key,
                    input.session_grant.tool_name,
                    input.session_grant.executor_type,
                    input.session_grant.resource_type,
                    input.session_grant.resource_id,
                    input.session_grant.status,
                    input.session_grant.granted_by,
                    toDBDate(input.session_grant.created_at),
                    toDBDate(input.session_grant.expires_at),
                    input.session_grant.revoked_at ? toDBDate(input.session_grant.revoked_at) : null
                ]);
            }
            const nextStatus = statusForDecision(input.decision.decision);
            await conn.execute(`UPDATE ai_agent_actions
        SET status = ?, decided_by = ?, decided_at = ?, updated_at = ?
        WHERE workspace_id = ? AND id = ? AND status = 'awaiting_decision'`, [
                nextStatus,
                input.decision.actor_user_id ?? null,
                toDBDate(input.decided_at),
                toDBDate(input.decided_at),
                input.workspace_id,
                input.action_id
            ]);
            await conn.commit();
            return {
                outcome: 'decided',
                action: {
                    ...action,
                    status: nextStatus,
                    decided_by: input.decision.actor_user_id,
                    decided_at: input.decided_at,
                    updated_at: input.decided_at
                }
            };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async claimApprovedActionExecution(input) {
        const conn = await this.pool.getConnection();
        try {
            await conn.beginTransaction();
            const [rows] = await conn.execute('SELECT * FROM ai_agent_actions WHERE workspace_id = ? AND id = ? FOR UPDATE', [input.workspace_id, input.action_id]);
            const current = rows[0];
            if (!current) {
                await conn.commit();
                return { outcome: 'not_approved' };
            }
            const action = this.actionFromRow(current);
            if (action.input_digest !== input.input_digest) {
                await conn.commit();
                return { outcome: 'input_mismatch' };
            }
            if (action.status === 'executing' || action.status === 'executed' || action.status === 'failed') {
                await conn.commit();
                return { outcome: 'already_claimed', action };
            }
            if (action.status !== 'approved') {
                await conn.commit();
                return { outcome: 'not_approved' };
            }
            const attempt = await this.nextIDWithConnection(conn, `action_execution:${action.id}`);
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
            await conn.execute(`UPDATE ai_agent_actions
        SET status = 'executing', executed_at = ?, updated_at = ?
        WHERE workspace_id = ? AND id = ? AND status = 'approved' AND input_digest = ?`, [toDBDate(input.claimed_at), toDBDate(input.claimed_at), input.workspace_id, input.action_id, input.input_digest]);
            await conn.execute(`INSERT INTO ai_action_executions (
          execution_id, workspace_id, session_id, runtime_run_id, action_id, attempt,
          executor_type, executor_ref_json, idempotency_key, status, external_request_id,
          result_json, error_code, error_msg, started_at, finished_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                execution.execution_id,
                execution.workspace_id,
                execution.session_id,
                execution.runtime_run_id,
                execution.action_id,
                execution.attempt,
                execution.executor_type,
                json(execution.executor_ref_json),
                execution.idempotency_key,
                execution.status,
                execution.external_request_id ?? null,
                json(execution.result_json),
                execution.error_code ?? null,
                execution.error_msg ?? null,
                toDBDate(execution.started_at),
                null,
                toDBDate(execution.created_at),
                toDBDate(execution.updated_at)
            ]);
            await conn.commit();
            return {
                outcome: 'claimed',
                action: { ...action, status: 'executing', executed_at: input.claimed_at, updated_at: input.claimed_at },
                execution
            };
        }
        catch (error) {
            await conn.rollback();
            throw error;
        }
        finally {
            conn.release();
        }
    }
    async findActionByIdentity(workspaceID, actionID, idempotencyKey) {
        const rows = await this.query('SELECT * FROM ai_agent_actions WHERE workspace_id = ? AND (action_id = ? OR idempotency_key = ?) LIMIT 1', [workspaceID, actionID, idempotencyKey]);
        return rows[0] ? this.actionFromRow(rows[0]) : undefined;
    }
    async getAction(workspaceID, actionID) {
        const rows = await this.query('SELECT * FROM ai_agent_actions WHERE workspace_id = ? AND id = ? LIMIT 1', [workspaceID, actionID]);
        return rows[0] ? this.actionFromRow(rows[0]) : undefined;
    }
    async getActionByBusinessID(workspaceID, actionBusinessID) {
        const rows = await this.query('SELECT * FROM ai_agent_actions WHERE workspace_id = ? AND action_id = ? LIMIT 1', [workspaceID, actionBusinessID]);
        return rows[0] ? this.actionFromRow(rows[0]) : undefined;
    }
    async appendActionEvent(event) {
        await this.pool.execute(`INSERT INTO ai_action_events (
        event_id, event_seq, workspace_id, session_id, runtime_run_id, action_id,
        entry_id, event_type, visibility, payload_json, display_json, created_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
            event.event_id,
            event.event_seq,
            event.workspace_id,
            event.session_id,
            event.runtime_run_id,
            event.action_id ?? null,
            event.entry_id ?? null,
            event.event_type,
            event.visibility,
            json(event.payload_json),
            json(event.display_json),
            toDBDate(event.created_at)
        ]);
        return event;
    }
    async listActionEvents(workspaceID, runtimeRunID, afterEventID = '', limit = 0) {
        const params = [workspaceID, runtimeRunID];
        let afterClause = '';
        if (afterEventID) {
            const afterRows = await this.query('SELECT event_seq FROM ai_action_events WHERE workspace_id = ? AND runtime_run_id = ? AND event_id = ? LIMIT 1', [workspaceID, runtimeRunID, afterEventID]);
            if (afterRows[0]) {
                afterClause = ' AND event_seq > ?';
                params.push(Number(afterRows[0].event_seq));
            }
        }
        const pageLimit = Number.isFinite(Number(limit)) && Number(limit) > 0 ? Math.floor(Number(limit)) : 0;
        const limitClause = pageLimit > 0 ? ' LIMIT ?' : '';
        if (pageLimit > 0)
            params.push(pageLimit);
        const rows = await this.query(`SELECT * FROM ai_action_events
      WHERE workspace_id = ? AND runtime_run_id = ?${afterClause}
      ORDER BY event_seq ASC${limitClause}`, params);
        return rows.map(this.actionEventFromRow);
    }
    async saveRuntimeArtifact(artifact) {
        await this.pool.execute(`INSERT INTO ai_runtime_artifacts (
        artifact_id, artifact_seq, workspace_id, session_id, runtime_run_id,
        action_id, event_id, visibility, artifact_type, mime_type, size_bytes,
        storage_ref, preview_json, created_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
            artifact.artifact_id,
            artifact.artifact_seq,
            artifact.workspace_id,
            artifact.session_id,
            artifact.runtime_run_id,
            artifact.action_id ?? null,
            artifact.event_id ?? null,
            artifact.visibility,
            artifact.artifact_type,
            artifact.mime_type,
            artifact.size_bytes,
            artifact.storage_ref,
            json(artifact.preview_json),
            toDBDate(artifact.created_at)
        ]);
        return artifact;
    }
    async getRuntimeArtifact(workspaceID, artifactID) {
        const rows = await this.query('SELECT * FROM ai_runtime_artifacts WHERE workspace_id = ? AND artifact_id = ? LIMIT 1', [workspaceID, artifactID]);
        return rows[0] ? this.runtimeArtifactFromRow(rows[0]) : undefined;
    }
    async listActionArtifacts(workspaceID, actionID) {
        const rows = await this.query('SELECT * FROM ai_runtime_artifacts WHERE workspace_id = ? AND action_id = ? ORDER BY artifact_seq ASC', [workspaceID, actionID]);
        return rows.map(this.runtimeArtifactFromRow);
    }
    async saveChildRunLink(link) {
        await this.pool.execute(`INSERT INTO ai_child_run_links (
        child_run_link_id, workspace_id, parent_runtime_run_id, parent_action_id,
        parent_action_internal_id, child_seq, child_session_id, child_runtime_run_id,
        child_profile_id, child_profile_version_id, snapshot_digest, status, created_at, completed_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE status=VALUES(status), completed_at=VALUES(completed_at)`, [
            link.child_run_link_id,
            link.workspace_id,
            link.parent_runtime_run_id,
            link.parent_action_id,
            link.parent_action_internal_id,
            link.child_seq,
            link.child_session_id,
            link.child_runtime_run_id,
            link.child_profile_id,
            link.child_profile_version_id,
            link.snapshot_digest,
            link.status,
            toDBDate(link.created_at),
            link.completed_at ? toDBDate(link.completed_at) : null
        ]);
        return link;
    }
    async listChildRunLinks(workspaceID, parentRuntimeRunID) {
        const rows = await this.query('SELECT * FROM ai_child_run_links WHERE workspace_id = ? AND parent_runtime_run_id = ? ORDER BY child_seq ASC', [workspaceID, parentRuntimeRunID]);
        return rows.map(this.childRunLinkFromRow);
    }
    async getParentRunLink(workspaceID, childRuntimeRunID) {
        const rows = await this.query('SELECT * FROM ai_child_run_links WHERE workspace_id = ? AND child_runtime_run_id = ? LIMIT 1', [workspaceID, childRuntimeRunID]);
        return rows[0] ? this.childRunLinkFromRow(rows[0]) : undefined;
    }
    async saveOrchestrationPlan(plan) {
        await this.pool.execute(`INSERT INTO ai_orchestration_plans (
        orchestration_id, workspace_id, parent_runtime_run_id, parent_session_id, status, phase,
        context_pack_digest, synthesis_summary, final_summary, error_code, error_msg,
        claim_owner, claim_epoch, claim_expires_at, created_at, updated_at, completed_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE
        status=VALUES(status), phase=VALUES(phase), context_pack_digest=VALUES(context_pack_digest),
        synthesis_summary=VALUES(synthesis_summary), final_summary=VALUES(final_summary),
        error_code=VALUES(error_code), error_msg=VALUES(error_msg), claim_owner=VALUES(claim_owner),
        claim_epoch=VALUES(claim_epoch), claim_expires_at=VALUES(claim_expires_at),
        updated_at=VALUES(updated_at), completed_at=VALUES(completed_at)`, [
            plan.orchestration_id, plan.workspace_id, plan.parent_runtime_run_id, plan.parent_session_id,
            plan.status, plan.phase, plan.context_pack_digest, plan.synthesis_summary || '', plan.final_summary || '',
            plan.error_code || null, plan.error_msg || null, plan.claim_owner || null, plan.claim_epoch || 0,
            plan.claim_expires_at ? toDBDate(plan.claim_expires_at) : null, toDBDate(plan.created_at),
            toDBDate(plan.updated_at), plan.completed_at ? toDBDate(plan.completed_at) : null
        ]);
        return plan;
    }
    async getOrchestrationPlan(workspaceID, orchestrationID) {
        const rows = await this.query(workspaceID > 0
            ? 'SELECT * FROM ai_orchestration_plans WHERE workspace_id = ? AND orchestration_id = ? LIMIT 1'
            : 'SELECT * FROM ai_orchestration_plans WHERE orchestration_id = ? LIMIT 1', workspaceID > 0 ? [workspaceID, orchestrationID] : [orchestrationID]);
        return rows[0] ? this.orchestrationPlanFromRow(rows[0]) : undefined;
    }
    async getOrchestrationPlanByParentRun(workspaceID, parentRuntimeRunID) {
        const rows = await this.query('SELECT * FROM ai_orchestration_plans WHERE workspace_id = ? AND parent_runtime_run_id = ? LIMIT 1', [workspaceID, parentRuntimeRunID]);
        return rows[0] ? this.orchestrationPlanFromRow(rows[0]) : undefined;
    }
    async claimOrchestrationPlan(input) {
        const plan = await this.getOrchestrationPlan(input.workspace_id, input.orchestration_id);
        if (!plan)
            return undefined;
        if (plan.status === 'completed' || plan.status === 'failed' || plan.status === 'cancelled')
            return plan;
        const next = {
            ...plan,
            claim_owner: input.owner,
            claim_epoch: Number(plan.claim_epoch || 0) + 1,
            claim_expires_at: input.claim_expires_at,
            updated_at: new Date().toISOString()
        };
        return this.saveOrchestrationPlan(next);
    }
    async saveOrchestrationContextPack(pack) {
        await this.pool.execute(`INSERT INTO ai_orchestration_context_packs (
        context_pack_id, workspace_id, orchestration_id, parent_runtime_run_id, source,
        trim_rules_json, resource_versions_json, token_estimate, content_json, digest, created_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE token_estimate=VALUES(token_estimate), content_json=VALUES(content_json)`, [
            pack.context_pack_id, pack.workspace_id, pack.orchestration_id, pack.parent_runtime_run_id, pack.source,
            json(pack.trim_rules), json(pack.resource_versions), pack.token_estimate || 0, json(pack.content),
            pack.digest, toDBDate(pack.created_at)
        ]);
        return pack;
    }
    async getOrchestrationContextPack(workspaceID, contextPackID) {
        const rows = await this.query(workspaceID > 0
            ? 'SELECT * FROM ai_orchestration_context_packs WHERE workspace_id = ? AND context_pack_id = ? LIMIT 1'
            : 'SELECT * FROM ai_orchestration_context_packs WHERE context_pack_id = ? LIMIT 1', workspaceID > 0 ? [workspaceID, contextPackID] : [contextPackID]);
        return rows[0] ? this.orchestrationContextPackFromRow(rows[0]) : undefined;
    }
    async saveOrchestrationTask(task) {
        await this.pool.execute(`INSERT INTO ai_orchestration_tasks (
        task_id, workspace_id, orchestration_id, parent_runtime_run_id, task_seq, task_kind, objective,
        assigned_profile_id, assigned_profile_version_id, assigned_subagent_name, mode, dependency_ids_json,
        context_pack_digest, status, claim_owner, claim_epoch, claim_expires_at, attempt, child_run_link_id,
        child_runtime_run_id, result_summary, artifact_refs_json, error_code, error_msg, created_at, updated_at, completed_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE
        status=VALUES(status), claim_owner=VALUES(claim_owner), claim_epoch=VALUES(claim_epoch),
        claim_expires_at=VALUES(claim_expires_at), attempt=VALUES(attempt), child_run_link_id=VALUES(child_run_link_id),
        child_runtime_run_id=VALUES(child_runtime_run_id), result_summary=VALUES(result_summary),
        artifact_refs_json=VALUES(artifact_refs_json), error_code=VALUES(error_code), error_msg=VALUES(error_msg),
        updated_at=VALUES(updated_at), completed_at=VALUES(completed_at)`, [
            task.task_id, task.workspace_id, task.orchestration_id, task.parent_runtime_run_id, task.task_seq, task.task_kind,
            task.objective, task.assigned_profile_id || 0, task.assigned_profile_version_id || 0, task.assigned_subagent_name || '',
            task.mode, json(task.dependency_ids || []), task.context_pack_digest || '', task.status, task.claim_owner || null,
            task.claim_epoch || 0, task.claim_expires_at ? toDBDate(task.claim_expires_at) : null, task.attempt || 0,
            task.child_run_link_id || null, task.child_runtime_run_id || null, task.result_summary || '',
            json(task.artifact_refs || []), task.error_code || null, task.error_msg || null, toDBDate(task.created_at),
            toDBDate(task.updated_at), task.completed_at ? toDBDate(task.completed_at) : null
        ]);
        return task;
    }
    async listOrchestrationTasks(workspaceID, orchestrationID) {
        const rows = await this.query('SELECT * FROM ai_orchestration_tasks WHERE workspace_id = ? AND orchestration_id = ? ORDER BY task_seq ASC', [workspaceID, orchestrationID]);
        return rows.map((row) => this.orchestrationTaskFromRow(row));
    }
    async getOrchestrationTask(workspaceID, taskID) {
        const rows = await this.query(workspaceID > 0
            ? 'SELECT * FROM ai_orchestration_tasks WHERE workspace_id = ? AND task_id = ? LIMIT 1'
            : 'SELECT * FROM ai_orchestration_tasks WHERE task_id = ? LIMIT 1', workspaceID > 0 ? [workspaceID, taskID] : [taskID]);
        return rows[0] ? this.orchestrationTaskFromRow(rows[0]) : undefined;
    }
    async claimOrchestrationTask(input) {
        const task = await this.getOrchestrationTask(input.workspace_id, input.task_id);
        if (!task)
            return undefined;
        if (!['pending', 'queued'].includes(task.status))
            return undefined;
        return this.saveOrchestrationTask({
            ...task,
            status: 'claimed',
            claim_owner: input.owner,
            claim_epoch: Number(task.claim_epoch || 0) + 1,
            claim_expires_at: input.claim_expires_at,
            updated_at: new Date().toISOString()
        });
    }
    orchestrationPlanFromRow(row) {
        return {
            orchestration_id: row.orchestration_id,
            workspace_id: Number(row.workspace_id),
            parent_runtime_run_id: row.parent_runtime_run_id,
            parent_session_id: Number(row.parent_session_id),
            status: row.status,
            phase: row.phase,
            context_pack_digest: row.context_pack_digest || '',
            synthesis_summary: row.synthesis_summary || '',
            final_summary: row.final_summary || '',
            error_code: row.error_code || undefined,
            error_msg: row.error_msg || undefined,
            claim_owner: row.claim_owner || null,
            claim_epoch: Number(row.claim_epoch || 0),
            claim_expires_at: row.claim_expires_at ? fromDBDate(row.claim_expires_at) : null,
            created_at: fromDBDate(row.created_at) || new Date().toISOString(),
            updated_at: fromDBDate(row.updated_at) || new Date().toISOString(),
            completed_at: row.completed_at ? fromDBDate(row.completed_at) : undefined
        };
    }
    orchestrationContextPackFromRow(row) {
        return {
            context_pack_id: row.context_pack_id,
            workspace_id: Number(row.workspace_id),
            orchestration_id: row.orchestration_id,
            parent_runtime_run_id: row.parent_runtime_run_id,
            source: row.source || '',
            trim_rules: parseJSON(row.trim_rules_json, {}),
            resource_versions: parseJSON(row.resource_versions_json, {}),
            token_estimate: Number(row.token_estimate || 0),
            content: parseJSON(row.content_json, {}),
            digest: row.digest || '',
            created_at: fromDBDate(row.created_at) || new Date().toISOString()
        };
    }
    orchestrationTaskFromRow(row) {
        return {
            task_id: row.task_id,
            workspace_id: Number(row.workspace_id),
            orchestration_id: row.orchestration_id,
            parent_runtime_run_id: row.parent_runtime_run_id,
            task_seq: Number(row.task_seq || 0),
            task_kind: row.task_kind || 'worker',
            objective: row.objective || '',
            assigned_profile_id: Number(row.assigned_profile_id || 0),
            assigned_profile_version_id: Number(row.assigned_profile_version_id || 0),
            assigned_subagent_name: row.assigned_subagent_name || '',
            mode: row.mode || 'read_only',
            dependency_ids: parseJSON(row.dependency_ids_json, []),
            context_pack_digest: row.context_pack_digest || '',
            status: row.status,
            claim_owner: row.claim_owner || null,
            claim_epoch: Number(row.claim_epoch || 0),
            claim_expires_at: row.claim_expires_at ? fromDBDate(row.claim_expires_at) : null,
            attempt: Number(row.attempt || 0),
            child_run_link_id: row.child_run_link_id || undefined,
            child_runtime_run_id: row.child_runtime_run_id || undefined,
            result_summary: row.result_summary || '',
            artifact_refs: parseJSON(row.artifact_refs_json, []),
            error_code: row.error_code || undefined,
            error_msg: row.error_msg || undefined,
            created_at: fromDBDate(row.created_at) || new Date().toISOString(),
            updated_at: fromDBDate(row.updated_at) || new Date().toISOString(),
            completed_at: row.completed_at ? fromDBDate(row.completed_at) : undefined
        };
    }
    async saveActionDecision(decision) {
        await this.pool.execute(`INSERT INTO ai_action_decisions (
        decision_id, decision_seq, workspace_id, session_id, runtime_run_id, action_id,
        decision_type, decision, actor_user_id, reason, input_patch_json,
        client_decision_id, idempotency_key, created_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
            decision.decision_id,
            decision.decision_seq,
            decision.workspace_id,
            decision.session_id,
            decision.runtime_run_id,
            decision.action_id,
            decision.decision_type,
            decision.decision,
            decision.actor_user_id ?? null,
            decision.reason || null,
            json(decision.input_patch_json),
            decision.client_decision_id,
            decision.idempotency_key,
            toDBDate(decision.created_at)
        ]);
        return decision;
    }
    async listActionDecisions(workspaceID, actionID) {
        const rows = await this.query('SELECT * FROM ai_action_decisions WHERE workspace_id = ? AND action_id = ? ORDER BY decision_seq ASC', [workspaceID, actionID]);
        return rows.map(this.actionDecisionFromRow);
    }
    async saveSessionPermissionGrant(grant) {
        await this.pool.execute(`INSERT INTO ai_session_permission_grants (
        grant_id, grant_seq, workspace_id, session_id, runtime_run_id, action_id,
        permission_key, tool_name, executor_type, resource_type, resource_id,
        status, granted_by, created_at, expires_at, revoked_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE status=VALUES(status), revoked_at=VALUES(revoked_at)`, [
            grant.grant_id,
            grant.grant_seq,
            grant.workspace_id,
            grant.session_id,
            grant.runtime_run_id,
            grant.action_id,
            grant.permission_key,
            grant.tool_name,
            grant.executor_type,
            grant.resource_type,
            grant.resource_id,
            grant.status,
            grant.granted_by,
            toDBDate(grant.created_at),
            toDBDate(grant.expires_at),
            grant.revoked_at ? toDBDate(grant.revoked_at) : null
        ]);
        return grant;
    }
    async listSessionPermissionGrants(workspaceID, sessionID) {
        const rows = await this.query('SELECT * FROM ai_session_permission_grants WHERE workspace_id = ? AND session_id = ? ORDER BY grant_seq ASC', [workspaceID, sessionID]);
        return rows.map(this.sessionPermissionGrantFromRow);
    }
    async saveActionExecution(execution) {
        await this.pool.execute(`INSERT INTO ai_action_executions (
        execution_id, workspace_id, session_id, runtime_run_id, action_id, attempt,
        executor_type, executor_ref_json, idempotency_key, status, external_request_id,
        result_json, error_code, error_msg, started_at, finished_at, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE status=VALUES(status), external_request_id=VALUES(external_request_id),
        result_json=VALUES(result_json), error_code=VALUES(error_code), error_msg=VALUES(error_msg),
        finished_at=VALUES(finished_at), updated_at=VALUES(updated_at)`, [
            execution.execution_id,
            execution.workspace_id,
            execution.session_id,
            execution.runtime_run_id,
            execution.action_id,
            execution.attempt,
            execution.executor_type,
            json(execution.executor_ref_json),
            execution.idempotency_key,
            execution.status,
            execution.external_request_id ?? null,
            json(execution.result_json),
            execution.error_code ?? null,
            execution.error_msg ?? null,
            toDBDate(execution.started_at),
            execution.finished_at ? toDBDate(execution.finished_at) : null,
            toDBDate(execution.created_at),
            toDBDate(execution.updated_at)
        ]);
        return execution;
    }
    async getActionExecution(workspaceID, actionID, attempt) {
        const rows = await this.query('SELECT * FROM ai_action_executions WHERE workspace_id = ? AND action_id = ? AND attempt = ? LIMIT 1', [workspaceID, actionID, attempt]);
        return rows[0] ? this.actionExecutionFromRow(rows[0]) : undefined;
    }
    async listActionExecutions(workspaceID, actionID) {
        const rows = await this.query('SELECT * FROM ai_action_executions WHERE workspace_id = ? AND action_id = ? ORDER BY attempt ASC', [workspaceID, actionID]);
        return rows.map(this.actionExecutionFromRow);
    }
    async saveAgentWorkspace(workspace) {
        await this.pool.execute(`INSERT INTO ai_runtime_workspaces (
        id, workspace_runtime_id, workspace_id, owner_user_id, workspace_key,
        provider, provider_endpoint, sandbox_id, status, image, root_path,
        repository_json, network_policy_json, secret_refs_json, quota_json, snapshot_hash,
        lease_owner_instance_id, lease_epoch, lease_expires_at,
        provision_owner_instance_id, provision_epoch, provision_expires_at, state_version,
        last_connected_at,
        paused_at, recycled_at, error_code, error_msg, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE
        provider=VALUES(provider), provider_endpoint=VALUES(provider_endpoint), sandbox_id=VALUES(sandbox_id),
        status=VALUES(status), image=VALUES(image), root_path=VALUES(root_path),
        repository_json=VALUES(repository_json), network_policy_json=VALUES(network_policy_json),
        secret_refs_json=VALUES(secret_refs_json), quota_json=VALUES(quota_json), snapshot_hash=VALUES(snapshot_hash),
        lease_owner_instance_id=VALUES(lease_owner_instance_id), lease_epoch=VALUES(lease_epoch),
        lease_expires_at=VALUES(lease_expires_at), provision_owner_instance_id=VALUES(provision_owner_instance_id),
        provision_epoch=VALUES(provision_epoch), provision_expires_at=VALUES(provision_expires_at),
        state_version=VALUES(state_version), last_connected_at=VALUES(last_connected_at),
        paused_at=VALUES(paused_at), recycled_at=VALUES(recycled_at), error_code=VALUES(error_code),
        error_msg=VALUES(error_msg), updated_at=VALUES(updated_at)`, [
            workspace.id,
            workspace.workspace_runtime_id,
            workspace.workspace_id,
            workspace.owner_user_id,
            workspace.workspace_key,
            workspace.provider,
            workspace.provider_endpoint || null,
            workspace.sandbox_id || null,
            workspace.status,
            workspace.image,
            workspace.root_path,
            json(workspace.repository),
            json(workspace.network_policy),
            JSON.stringify(workspace.secret_refs ?? []),
            json(workspace.quota),
            workspace.snapshot_hash,
            workspace.lease_owner_instance_id || null,
            Number(workspace.lease_epoch || 0),
            workspace.lease_expires_at ? toDBDate(workspace.lease_expires_at) : null,
            workspace.provision_owner_instance_id || null,
            Number(workspace.provision_epoch || 0),
            workspace.provision_expires_at ? toDBDate(workspace.provision_expires_at) : null,
            Number(workspace.state_version || 0),
            workspace.last_connected_at ? toDBDate(workspace.last_connected_at) : null,
            workspace.paused_at ? toDBDate(workspace.paused_at) : null,
            workspace.recycled_at ? toDBDate(workspace.recycled_at) : null,
            workspace.error_code || null,
            workspace.error_msg || null,
            toDBDate(workspace.created_at),
            toDBDate(workspace.updated_at)
        ]);
        return workspace;
    }
    async createOrGetDefaultAgentWorkspace(workspace) {
        await this.pool.execute(`INSERT INTO ai_runtime_workspaces (
        id, workspace_runtime_id, workspace_id, owner_user_id, workspace_key,
        provider, provider_endpoint, sandbox_id, status, image, root_path,
        repository_json, network_policy_json, secret_refs_json, quota_json, snapshot_hash,
        lease_owner_instance_id, lease_epoch, lease_expires_at,
        provision_owner_instance_id, provision_epoch, provision_expires_at, state_version,
        last_connected_at, paused_at, recycled_at, error_code, error_msg, created_at, updated_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, [
            workspace.id,
            workspace.workspace_runtime_id,
            workspace.workspace_id,
            workspace.owner_user_id,
            workspace.workspace_key,
            workspace.provider,
            workspace.provider_endpoint || null,
            workspace.sandbox_id || null,
            workspace.status,
            workspace.image,
            workspace.root_path,
            json(workspace.repository),
            json(workspace.network_policy),
            JSON.stringify(workspace.secret_refs ?? []),
            json(workspace.quota),
            workspace.snapshot_hash,
            workspace.lease_owner_instance_id || null,
            Number(workspace.lease_epoch || 0),
            workspace.lease_expires_at ? toDBDate(workspace.lease_expires_at) : null,
            workspace.provision_owner_instance_id || null,
            Number(workspace.provision_epoch || 0),
            workspace.provision_expires_at ? toDBDate(workspace.provision_expires_at) : null,
            Number(workspace.state_version || 0),
            workspace.last_connected_at ? toDBDate(workspace.last_connected_at) : null,
            workspace.paused_at ? toDBDate(workspace.paused_at) : null,
            workspace.recycled_at ? toDBDate(workspace.recycled_at) : null,
            workspace.error_code || null,
            workspace.error_msg || null,
            toDBDate(workspace.created_at),
            toDBDate(workspace.updated_at)
        ]);
        const stored = await this.getAgentWorkspaceByOwnerKey(workspace.workspace_id, workspace.owner_user_id, workspace.workspace_key);
        if (!stored)
            return { outcome: 'created', workspace };
        return { outcome: stored.id === workspace.id ? 'created' : 'existing', workspace: stored };
    }
    async claimAgentWorkspaceProvision(input) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET status = 'provisioning', provision_owner_instance_id = ?,
          provision_epoch = provision_epoch + 1, provision_expires_at = ?,
          state_version = state_version + 1, error_code = NULL, error_msg = NULL, updated_at = ?
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND owner_user_id = ?
        AND status IN ('provisioning', 'failed', 'recycled')
        AND (provision_owner_instance_id IS NULL OR provision_expires_at IS NULL OR provision_expires_at <= ?)`, [
            input.instance_id,
            toDBDate(input.lease_expires_at),
            toDBDate(input.observed_at),
            input.workspace_id,
            input.workspace_runtime_id,
            input.owner_user_id,
            toDBDate(input.observed_at)
        ]);
        if (result.affectedRows !== 1)
            return undefined;
        return this.getAgentWorkspace(input.workspace_id, input.workspace_runtime_id);
    }
    async completeAgentWorkspaceProvision(input) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET status = 'ready', sandbox_id = ?, provision_owner_instance_id = NULL,
          provision_expires_at = NULL, state_version = state_version + 1,
          error_code = NULL, error_msg = NULL, updated_at = ?
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND status = 'provisioning'
        AND provision_owner_instance_id = ? AND provision_epoch = ?`, [
            input.sandbox_id,
            toDBDate(input.updated_at),
            input.workspace_id,
            input.workspace_runtime_id,
            input.instance_id,
            input.provision_epoch
        ]);
        if (result.affectedRows !== 1)
            return undefined;
        return this.getAgentWorkspace(input.workspace_id, input.workspace_runtime_id);
    }
    async failAgentWorkspaceProvision(input) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET status = 'failed', provision_owner_instance_id = NULL,
          provision_expires_at = NULL, state_version = state_version + 1,
          error_code = ?, error_msg = ?, updated_at = ?
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND status = 'provisioning'
        AND provision_owner_instance_id = ? AND provision_epoch = ?`, [
            input.error_code,
            input.error_msg,
            toDBDate(input.updated_at),
            input.workspace_id,
            input.workspace_runtime_id,
            input.instance_id,
            input.provision_epoch
        ]);
        if (result.affectedRows !== 1)
            return undefined;
        return this.getAgentWorkspace(input.workspace_id, input.workspace_runtime_id);
    }
    async getAgentWorkspace(workspaceID, workspaceRuntimeID) {
        const rows = await this.query('SELECT * FROM ai_runtime_workspaces WHERE workspace_id = ? AND workspace_runtime_id = ? LIMIT 1', [workspaceID, workspaceRuntimeID]);
        return rows[0] ? this.agentWorkspaceFromRow(rows[0]) : undefined;
    }
    async getAgentWorkspaceByOwnerKey(workspaceID, ownerUserID, workspaceKey) {
        const rows = await this.query('SELECT * FROM ai_runtime_workspaces WHERE workspace_id = ? AND owner_user_id = ? AND workspace_key = ? LIMIT 1', [workspaceID, ownerUserID, workspaceKey]);
        return rows[0] ? this.agentWorkspaceFromRow(rows[0]) : undefined;
    }
    async listAgentWorkspaces(workspaceID, ownerUserID) {
        const params = [workspaceID];
        let ownerClause = '';
        if (ownerUserID !== undefined) {
            ownerClause = ' AND owner_user_id = ?';
            params.push(ownerUserID);
        }
        const rows = await this.query(`SELECT * FROM ai_runtime_workspaces WHERE workspace_id = ?${ownerClause} ORDER BY updated_at DESC, id DESC`, params);
        return rows.map((row) => this.agentWorkspaceFromRow(row));
    }
    async beginAgentWorkspaceRecycle(workspaceID, workspaceRuntimeID, ownerUserID, operationID, leaseExpiresAt, observedAt = new Date().toISOString()) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET status = 'recycling', lease_epoch = lease_epoch + 1,
          lease_owner_instance_id = ?, lease_expires_at = ?, paused_at = NULL,
          error_code = NULL, error_msg = NULL, updated_at = ?
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND owner_user_id = ?
        AND status IN ('ready', 'paused', 'recycled', 'failed')
        AND (lease_owner_instance_id IS NULL OR lease_expires_at IS NULL OR lease_expires_at <= ?)`, [
            operationID,
            toDBDate(leaseExpiresAt),
            toDBDate(observedAt),
            workspaceID,
            workspaceRuntimeID,
            ownerUserID,
            toDBDate(observedAt)
        ]);
        if (result.affectedRows !== 1)
            return undefined;
        return this.getAgentWorkspace(workspaceID, workspaceRuntimeID);
    }
    async claimAgentWorkspaceLease(workspaceID, workspaceRuntimeID, instanceID, leaseExpiresAt, observedAt = new Date().toISOString()) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET lease_epoch = IF(lease_owner_instance_id = ? AND lease_expires_at > ?, GREATEST(lease_epoch, 1), lease_epoch + 1),
          lease_owner_instance_id = ?, lease_expires_at = ?, last_connected_at = ?, updated_at = ?
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND status = 'ready'
        AND (lease_owner_instance_id IS NULL OR lease_owner_instance_id = ? OR lease_expires_at IS NULL OR lease_expires_at <= ?)`, [
            instanceID,
            toDBDate(observedAt),
            instanceID,
            toDBDate(leaseExpiresAt),
            toDBDate(observedAt),
            toDBDate(observedAt),
            workspaceID,
            workspaceRuntimeID,
            instanceID,
            toDBDate(observedAt)
        ]);
        if (result.affectedRows !== 1)
            return undefined;
        return this.getAgentWorkspace(workspaceID, workspaceRuntimeID);
    }
    async renewAgentWorkspaceLease(workspaceID, workspaceRuntimeID, instanceID, leaseEpoch, leaseExpiresAt) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET lease_expires_at = ?, updated_at = CURRENT_TIMESTAMP(3)
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND status = 'ready'
        AND lease_owner_instance_id = ? AND lease_epoch = ?`, [toDBDate(leaseExpiresAt), workspaceID, workspaceRuntimeID, instanceID, leaseEpoch]);
        if (result.affectedRows !== 1)
            return undefined;
        return this.getAgentWorkspace(workspaceID, workspaceRuntimeID);
    }
    async releaseAgentWorkspaceLease(workspaceID, workspaceRuntimeID, instanceID, leaseEpoch) {
        const [result] = await this.pool.execute(`UPDATE ai_runtime_workspaces
      SET lease_owner_instance_id = NULL, lease_expires_at = NULL, updated_at = CURRENT_TIMESTAMP(3)
      WHERE workspace_id = ? AND workspace_runtime_id = ? AND lease_owner_instance_id = ? AND lease_epoch = ?`, [workspaceID, workspaceRuntimeID, instanceID, leaseEpoch]);
        return result.affectedRows === 1;
    }
    async appendAgentWorkspaceAudit(audit) {
        await this.pool.execute(`INSERT INTO ai_runtime_workspace_audits (
        audit_id, workspace_runtime_id, workspace_id, owner_user_id, actor_user_id,
        operation, outcome, details_json, created_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
            audit.audit_id,
            audit.workspace_runtime_id,
            audit.workspace_id,
            audit.owner_user_id,
            audit.actor_user_id,
            audit.operation,
            audit.outcome,
            json(audit.details),
            toDBDate(audit.created_at)
        ]);
        return audit;
    }
    async listAgentWorkspaceAudits(workspaceID, workspaceRuntimeID) {
        const rows = await this.query(`SELECT * FROM ai_runtime_workspace_audits
      WHERE workspace_id = ? AND workspace_runtime_id = ?
      ORDER BY created_at ASC, audit_id ASC`, [workspaceID, workspaceRuntimeID]);
        return rows.map((row) => this.agentWorkspaceAuditFromRow(row));
    }
    async nextID(name) {
        const conn = await this.pool.getConnection();
        try {
            return await this.nextIDWithConnection(conn, name);
        }
        finally {
            conn.release();
        }
    }
    async nextIDWithConnection(conn, name) {
        await conn.execute('INSERT IGNORE INTO ai_runtime_sequences (`name`, next_value) VALUES (?, 1)', [name]);
        await conn.execute('UPDATE ai_runtime_sequences SET next_value = LAST_INSERT_ID(next_value) + 1 WHERE `name` = ?', [name]);
        const [rows] = await conn.query('SELECT LAST_INSERT_ID() AS id');
        const id = Number(rows[0]?.id || 0);
        if (id <= 0)
            throw new RuntimeDomainError('runtime_sequence_allocation_failed', 'Runtime sequence allocation failed', 500);
        return id;
    }
    async query(sql, params = []) {
        const [rows] = await this.pool.query(sql, params);
        return rows;
    }
    async execute(sql, params = []) {
        return this.pool.execute(sql, params);
    }
    async appendFollowUpStartedEventInTransaction(conn, event) {
        const [sequenceResult] = await conn.execute(`INSERT INTO ai_agent_runtime_event_sequences (session_id, last_seq)
      VALUES (?, LAST_INSERT_ID(1))
      ON DUPLICATE KEY UPDATE last_seq = LAST_INSERT_ID(last_seq + 1)`, [event.session_id]);
        const stored = {
            ...event,
            seq: Number(sequenceResult.insertId || 0)
        };
        await conn.execute(`INSERT INTO ai_agent_runtime_events (
        session_id, runtime_run_id, event_seq, event_id, event_type, event_json
      ) VALUES (?, ?, ?, ?, ?, ?)`, [stored.session_id, stored.runtime_run_id, stored.seq, stored.event_id, stored.type, json(stored)]);
        return stored;
    }
    async appendSessionQueueEventInTransaction(conn, draft, item) {
        const marker = draft.type === 'session.queue.claimed' ? item.claim_epoch : draft.marker;
        const eventBase = {
            session_id: draft.session_id,
            event_id: [draft.session_id, draft.type, item.queue_item_id, marker]
                .map((part) => encodeURIComponent(String(part).trim() || 'event'))
                .join(':'),
            seq: 0,
            timestamp: item.updated_at,
            queue_item: item
        };
        let event;
        switch (draft.type) {
            case 'session.queue.added':
                event = { ...eventBase, type: 'session.queue.added' };
                break;
            case 'session.queue.claimed':
                event = { ...eventBase, type: 'session.queue.claimed' };
                break;
            case 'session.queue.cancelled':
                event = { ...eventBase, type: 'session.queue.cancelled' };
                break;
            case 'session.queue.expired':
                event = { ...eventBase, type: 'session.queue.expired' };
                break;
            case 'session.queue.failed':
                event = { ...eventBase, type: 'session.queue.failed' };
                break;
            case 'session.steer.applied':
                event = { ...eventBase, type: 'session.steer.applied', runtime_run_id: draft.runtime_run_id };
                break;
        }
        const [sequenceResult] = await conn.execute(`INSERT INTO ai_agent_runtime_event_sequences (session_id, last_seq)
      VALUES (?, LAST_INSERT_ID(1))
      ON DUPLICATE KEY UPDATE last_seq = LAST_INSERT_ID(last_seq + 1)`, [event.session_id]);
        const stored = { ...event, seq: Number(sequenceResult.insertId || 0) };
        await conn.execute(`INSERT INTO ai_agent_runtime_events (
        session_id, runtime_run_id, event_seq, event_id, event_type, event_json
      ) VALUES (?, ?, ?, ?, ?, ?)`, [stored.session_id, stored.runtime_run_id || null, stored.seq, stored.event_id, stored.type, json(stored)]);
        return stored;
    }
    async lockProfileReferenceTargets(conn, profile, operation) {
        const subagentProfileIDs = profile.subagents
            .filter((ref) => ref.resource_type === 'subagent_profile')
            .map((ref) => Number(ref.resource_id))
            .filter((id) => Number.isInteger(id) && id > 0);
        const profileIDs = [...new Set([...(operation === 'update' ? [profile.id] : []), ...subagentProfileIDs])].sort((a, b) => a - b);
        if (profileIDs.length > 0) {
            const placeholders = profileIDs.map(() => '?').join(',');
            const [rows] = await conn.execute(`SELECT id FROM ai_agent_profiles
        WHERE workspace_id = ? AND id IN (${placeholders}) ORDER BY id FOR UPDATE`, [profile.workspace_id, ...profileIDs]);
            if (new Set(rows.map((row) => Number(row.id))).size !== profileIDs.length) {
                throw new RuntimeDomainError(operation === 'update' ? 'agent_profile_not_found' : 'agent_profile_subagent_not_found', operation === 'update' ? 'Agent profile or Subagent profile was not found' : 'Subagent profile was not found', 404);
            }
        }
        const resourceVersionIDs = [...new Set([...profile.skills, ...profile.mcp_servers]
                .map((ref) => Number(ref.resource_version_id || 0))
                .filter((id) => Number.isInteger(id) && id > 0))].sort((a, b) => a - b);
        if (resourceVersionIDs.length > 0) {
            const placeholders = resourceVersionIDs.map(() => '?').join(',');
            const [rows] = await conn.execute(`SELECT id FROM ai_agent_resource_versions
        WHERE workspace_id = ? AND id IN (${placeholders}) ORDER BY id FOR UPDATE`, [profile.workspace_id, ...resourceVersionIDs]);
            if (new Set(rows.map((row) => Number(row.id))).size !== resourceVersionIDs.length) {
                throw new RuntimeDomainError('agent_profile_resource_version_not_found', 'Agent resource version was not found', 404);
            }
        }
    }
    async writeProfile(conn, profile, operation, lockTargets = true) {
        if (lockTargets)
            await this.lockProfileReferenceTargets(conn, profile, operation);
        const commonParams = [
            profile.name, profile.description, profile.profile_kind, json(profile.context_tags), json(profile.provider),
            json(profile.binding), json(profile.model), json(profile.provider_credential_ref), json(profile.inference),
            json(profile.prompt), json(profile.context_contract), json(profile.input_schema), json(profile.output_schema),
            json(profile.tool_policy), json(profile.memory_policy), json(profile.confirmation_policy), profile.response_mode, profile.status,
            toDBDate(profile.updated_at)
        ];
        if (operation === 'create') {
            await conn.execute(`INSERT INTO ai_agent_profiles (
        id, workspace_id, name, description, profile_kind, context_tags_json,
        provider_json, binding_json, model_json, provider_credential_ref_json,
        inference_json, prompt_json, context_contract_json, input_schema_json, output_schema_json,
        tool_policy_json, memory_policy_json, confirmation_policy_json, response_mode, status, created_by, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [profile.id, profile.workspace_id, ...commonParams.slice(0, 18), profile.created_by, toDBDate(profile.created_at), commonParams[18]]);
        }
        else {
            const [result] = await conn.execute(`UPDATE ai_agent_profiles SET
          name = ?, description = ?, profile_kind = ?, context_tags_json = ?, provider_json = ?,
          binding_json = ?, model_json = ?, provider_credential_ref_json = ?, inference_json = ?, prompt_json = ?,
          context_contract_json = ?, input_schema_json = ?, output_schema_json = ?, tool_policy_json = ?,
          memory_policy_json = ?, confirmation_policy_json = ?, response_mode = ?, status = ?, updated_at = ?
        WHERE workspace_id = ? AND id = ?`, [...commonParams, profile.workspace_id, profile.id]);
            if (result.affectedRows !== 1)
                throw new RuntimeDomainError('agent_profile_not_found', 'Agent profile not found', 404);
        }
        await conn.execute('DELETE FROM ai_agent_profile_resource_refs WHERE workspace_id = ? AND profile_id = ?', [
            profile.workspace_id,
            profile.id
        ]);
        await this.insertRefs(conn, profile, 'skills', profile.skills);
        await this.insertRefs(conn, profile, 'subagents', profile.subagents);
        await this.insertRefs(conn, profile, 'mcp_servers', profile.mcp_servers);
    }
    async insertRefs(conn, profile, section, refs) {
        for (const ref of refs) {
            await conn.execute(`INSERT INTO ai_agent_profile_resource_refs (
          workspace_id, profile_id, profile_section, resource_type, resource_id, resource_id_kind,
          resource_version_id, resource_version, snapshot_digest, config_json, required, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, [
                profile.workspace_id,
                profile.id,
                section,
                ref.resource_type,
                resourceIDForDB(ref.resource_id),
                resourceIDKindForDB(ref.resource_id),
                ref.resource_version_id ?? null,
                ref.resource_version ?? null,
                ref.snapshot_digest ?? null,
                json(ref.config),
                ref.required === false ? 0 : 1,
                toDBDate(profile.updated_at),
                toDBDate(profile.updated_at)
            ]);
        }
    }
    async profileRefs(workspaceID, profileID, section) {
        const rows = await this.query(`SELECT resource_type, resource_id, resource_id_kind, resource_version_id, resource_version, snapshot_digest, config_json, required
      FROM ai_agent_profile_resource_refs
      WHERE workspace_id = ? AND profile_id = ? AND profile_section = ?
      ORDER BY id ASC`, [workspaceID, profileID, section]);
        return rows.map((row) => ({
            resource_type: row.resource_type,
            resource_id: resourceIDFromDB(row.resource_id, row.resource_id_kind),
            resource_version_id: row.resource_version_id ? Number(row.resource_version_id) : undefined,
            resource_version: row.resource_version || undefined,
            snapshot_digest: row.snapshot_digest || undefined,
            config: parseJSON(row.config_json, {}),
            required: row.required === 1
        }));
    }
    async profileFromRow(row) {
        return {
            id: Number(row.id),
            workspace_id: Number(row.workspace_id),
            name: row.name,
            description: row.description || '',
            profile_kind: row.profile_kind,
            context_tags: parseJSON(row.context_tags_json, []),
            provider: parseJSON(row.provider_json, {}),
            binding: parseJSON(row.binding_json, {}),
            model: parseJSON(row.model_json, {}),
            provider_credential_ref: parseJSON(row.provider_credential_ref_json, {}),
            inference: parseJSON(row.inference_json, {}),
            prompt: parseJSON(row.prompt_json, {}),
            skills: await this.profileRefs(Number(row.workspace_id), Number(row.id), 'skills'),
            subagents: await this.profileRefs(Number(row.workspace_id), Number(row.id), 'subagents'),
            mcp_servers: await this.profileRefs(Number(row.workspace_id), Number(row.id), 'mcp_servers'),
            context_contract: parseJSON(row.context_contract_json, {}),
            input_schema: parseJSON(row.input_schema_json, {}),
            output_schema: parseJSON(row.output_schema_json, {}),
            tool_policy: parseJSON(row.tool_policy_json, {}),
            memory_policy: parseJSON(row.memory_policy_json, {}),
            confirmation_policy: parseJSON(row.confirmation_policy_json, {}),
            response_mode: row.response_mode,
            status: row.status,
            created_by: Number(row.created_by),
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    agentResourceFromRow(row) {
        return {
            id: Number(row.id),
            workspace_id: Number(row.workspace_id),
            resource_kind: row.resource_kind,
            resource_key: row.resource_key,
            resource_id: row.resource_key,
            name: row.name,
            description: row.description || '',
            version: row.version,
            status: row.status,
            spec: parseJSON(row.spec_json, {}),
            endpoint: parseJSON(row.endpoint_json, {}),
            secret_ref: parseJSON(row.secret_ref_json, {}),
            tags: parseJSON(row.tags_json, []),
            created_by: Number(row.created_by),
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    agentResourceVersionFromRow(row) {
        const version = {
            resource_version_id: Number(row.id),
            resource_id: Number(row.resource_id),
            workspace_id: Number(row.workspace_id),
            revision: Number(row.revision),
            source_version: row.source_version,
            snapshot_digest: row.snapshot_digest,
            snapshot: parseJSON(row.snapshot_json, {}),
            created_by: Number(row.created_by),
            created_at: fromDBDate(row.created_at) || row.created_at
        };
        if (agentResourceSnapshotDigest(version.snapshot) !== version.snapshot_digest) {
            throw new RuntimeDomainError('agent_resource_version_integrity_failed', 'Agent resource version snapshot integrity verification failed', 500);
        }
        return version;
    }
    versionFromRow(row) {
        const version = {
            profile_version_id: Number(row.id),
            profile_id: Number(row.profile_id),
            workspace_id: Number(row.workspace_id),
            version: Number(row.version),
            snapshot_hash: row.snapshot_hash,
            snapshot: parseJSON(row.snapshot_json, {}),
            status: row.status,
            published_by: Number(row.published_by),
            published_at: fromDBDate(row.published_at) || row.published_at,
            change_summary: row.change_summary || ''
        };
        if (agentProfileSnapshotHash(version.snapshot) !== version.snapshot_hash) {
            throw new RuntimeDomainError('agent_profile_version_integrity_failed', 'Agent profile version snapshot integrity verification failed', 500);
        }
        return version;
    }
    sessionFromRow(row) {
        return {
            id: Number(row.id),
            workspace_id: Number(row.workspace_id),
            context_tags: parseJSON(row.context_tags_json, []),
            session_kind: row.session_kind,
            user_id: Number(row.user_id),
            auth_session_id: row.auth_session_id,
            business_type: row.business_type || '',
            business_id: row.business_id || '',
            status: row.status,
            agent_profile_id: Number(row.agent_profile_id),
            agent_profile_version_id: Number(row.agent_profile_version_id),
            agent_profile_version_key: row.agent_profile_version_key || undefined,
            agent_profile_snapshot_hash: row.agent_profile_snapshot_hash,
            agent_workspace_runtime_id: row.agent_workspace_runtime_id,
            agent_workspace_snapshot_hash: row.agent_workspace_snapshot_hash,
            session_key: row.session_key || undefined,
            first_session_timestamp: fromDBDate(row.first_session_timestamp || undefined),
            source: row.source || undefined,
            model_override: parseJSON(row.model_override_json, {}),
            title: row.title || '',
            title_source: row.title_source || 'fallback',
            title_generated_at: fromDBDate(row.title_generated_at || undefined),
            title_generation_error: row.title_generation_error || undefined,
            entry_count: Number(row.entry_count),
            last_entry_at: fromDBDate(row.last_entry_at || undefined),
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    entryFromRow(row) {
        return {
            id: Number(row.id),
            session_id: Number(row.session_id),
            workspace_id: Number(row.workspace_id),
            user_id: Number(row.user_id),
            parent_entry_id: row.parent_entry_id ? Number(row.parent_entry_id) : undefined,
            seq: Number(row.seq),
            entry_type: row.entry_type,
            role: row.role,
            status: row.status,
            content: row.content || '',
            content_blocks: parseJSON(row.content_blocks_json, []),
            input: parseJSON(row.input_json, {}),
            output: parseJSON(row.output_json, {}),
            runtime_run_id: row.runtime_run_id || undefined,
            event_seq: row.event_seq ? Number(row.event_seq) : undefined,
            idempotency_key: row.idempotency_key,
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    runFromRow(row) {
        return normalizeRuntimeRunActiveSlot({
            id: Number(row.id),
            runtime_run_id: row.runtime_run_id,
            session_id: Number(row.session_id),
            workspace_id: Number(row.workspace_id),
            context_tags: parseJSON(row.context_tags_json, []),
            agent_profile_id: Number(row.agent_profile_id),
            agent_profile_version_id: Number(row.agent_profile_version_id),
            agent_profile_version_key: row.agent_profile_version_key || undefined,
            agent_profile_snapshot_hash: row.agent_profile_snapshot_hash,
            agent_workspace_runtime_id: row.agent_workspace_runtime_id,
            agent_workspace_snapshot_hash: row.agent_workspace_snapshot_hash,
            profile_snapshot: parseJSON(row.profile_snapshot_json, {}),
            status: row.status,
            active_slot: row.active_slot || undefined,
            owner_instance_id: row.owner_instance_id || undefined,
            owner_epoch: Number(row.owner_epoch || 0) || undefined,
            owner_lease_expires_at: fromDBDate(row.owner_lease_expires_at || undefined),
            input_entry_id: Number(row.input_entry_id),
            output_entry_id: row.output_entry_id ? Number(row.output_entry_id) : undefined,
            request: parseJSON(row.request_json, {}),
            result: parseJSON(row.result_json, {}),
            usage: parseJSON(row.usage_json, {}),
            error_code: row.error_code || undefined,
            error_msg: row.error_msg || undefined,
            started_at: fromDBDate(row.started_at) || row.started_at,
            finished_at: fromDBDate(row.finished_at || undefined),
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        });
    }
    agentWorkspaceFromRow(row) {
        return {
            id: Number(row.id),
            workspace_runtime_id: row.workspace_runtime_id,
            workspace_id: Number(row.workspace_id),
            owner_user_id: Number(row.owner_user_id),
            workspace_key: row.workspace_key,
            provider: row.provider,
            provider_endpoint: row.provider_endpoint || undefined,
            sandbox_id: row.sandbox_id || undefined,
            status: row.status,
            image: row.image,
            root_path: row.root_path,
            repository: parseJSON(row.repository_json, {}),
            network_policy: parseJSON(row.network_policy_json, { mode: 'none', allow_hosts: [] }),
            secret_refs: parseJSON(row.secret_refs_json, []),
            quota: parseJSON(row.quota_json, { cpu_cores: 1, memory_mb: 1024, disk_mb: 10240, pids: 256 }),
            snapshot_hash: row.snapshot_hash,
            lease_owner_instance_id: row.lease_owner_instance_id || undefined,
            lease_epoch: Number(row.lease_epoch || 0),
            lease_expires_at: fromDBDate(row.lease_expires_at || undefined),
            provision_owner_instance_id: row.provision_owner_instance_id || undefined,
            provision_epoch: Number(row.provision_epoch || 0),
            provision_expires_at: fromDBDate(row.provision_expires_at || undefined),
            state_version: Number(row.state_version || 0),
            last_connected_at: fromDBDate(row.last_connected_at || undefined),
            paused_at: fromDBDate(row.paused_at || undefined),
            recycled_at: fromDBDate(row.recycled_at || undefined),
            error_code: row.error_code || undefined,
            error_msg: row.error_msg || undefined,
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    agentWorkspaceAuditFromRow(row) {
        return {
            audit_id: row.audit_id,
            workspace_runtime_id: row.workspace_runtime_id,
            workspace_id: Number(row.workspace_id),
            owner_user_id: Number(row.owner_user_id),
            actor_user_id: Number(row.actor_user_id),
            operation: row.operation,
            outcome: row.outcome,
            details: parseJSON(row.details_json, {}),
            created_at: fromDBDate(row.created_at) || row.created_at
        };
    }
    actionFromRow(row) {
        return {
            id: Number(row.id),
            action_id: row.action_id,
            workspace_id: Number(row.workspace_id),
            context_tags: parseJSON(row.context_tags_json, []),
            session_id: Number(row.session_id),
            entry_id: row.entry_id ? Number(row.entry_id) : undefined,
            runtime_run_id: row.runtime_run_id,
            action_kind: row.action_kind,
            idempotency_key: row.idempotency_key,
            source: row.source || 'runtime',
            capability_id: row.capability_id,
            input_json: parseJSON(row.input_json, {}),
            input_digest: row.input_digest,
            target_json: parseJSON(row.target_json, {}),
            policy_json: parseJSON(row.policy_json, {}),
            display_json: parseJSON(row.display_json, {}),
            status: row.status,
            requested_by: Number(row.requested_by),
            decided_by: row.decided_by ? Number(row.decided_by) : undefined,
            decided_at: fromDBDate(row.decided_at || undefined),
            executed_at: fromDBDate(row.executed_at || undefined),
            result_json: parseJSON(row.result_json, {}),
            error_msg: row.error_msg || undefined,
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    actionEventFromRow(row) {
        return {
            event_id: row.event_id,
            event_seq: Number(row.event_seq),
            workspace_id: Number(row.workspace_id),
            session_id: Number(row.session_id),
            runtime_run_id: row.runtime_run_id,
            action_id: row.action_id || undefined,
            entry_id: row.entry_id ? Number(row.entry_id) : undefined,
            event_type: row.event_type,
            visibility: row.visibility,
            payload_json: parseJSON(row.payload_json, {}),
            display_json: parseJSON(row.display_json, {}),
            created_at: fromDBDate(row.created_at) || row.created_at
        };
    }
    runtimeArtifactFromRow(row) {
        return {
            artifact_id: row.artifact_id,
            artifact_seq: Number(row.artifact_seq),
            workspace_id: Number(row.workspace_id),
            session_id: Number(row.session_id),
            runtime_run_id: row.runtime_run_id,
            action_id: row.action_id || undefined,
            event_id: row.event_id || undefined,
            visibility: row.visibility,
            artifact_type: row.artifact_type,
            mime_type: row.mime_type,
            size_bytes: Number(row.size_bytes),
            storage_ref: row.storage_ref,
            preview_json: parseJSON(row.preview_json, {}),
            created_at: fromDBDate(row.created_at) || row.created_at
        };
    }
    sessionQueueItemFromRow(row) {
        return {
            queue_item_id: row.queue_item_id,
            workspace_id: Number(row.workspace_id),
            session_id: Number(row.session_id),
            item_seq: Number(row.item_seq),
            position: Number(row.position),
            mode: row.mode,
            content: row.content,
            attachments: parseJSON(row.attachments_json, []),
            context_ref: parseJSON(row.context_ref_json, {}),
            target_run_id: row.target_run_id || undefined,
            status: row.status,
            client_item_id: row.client_item_id,
            claimed_by: row.claimed_by || undefined,
            claim_epoch: Number(row.claim_epoch || 0),
            claim_expires_at: fromDBDate(row.claim_expires_at || undefined),
            expires_at: fromDBDate(row.expires_at || undefined),
            consumed_runtime_run_id: row.consumed_runtime_run_id || undefined,
            error_code: row.error_code || undefined,
            error_msg: row.error_msg || undefined,
            created_by: Number(row.created_by),
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
    childRunLinkFromRow(row) {
        return {
            child_run_link_id: row.child_run_link_id,
            workspace_id: Number(row.workspace_id),
            parent_runtime_run_id: row.parent_runtime_run_id,
            parent_action_id: row.parent_action_id,
            parent_action_internal_id: Number(row.parent_action_internal_id),
            child_seq: Number(row.child_seq),
            child_session_id: Number(row.child_session_id),
            child_runtime_run_id: row.child_runtime_run_id,
            child_profile_id: Number(row.child_profile_id),
            child_profile_version_id: Number(row.child_profile_version_id),
            snapshot_digest: row.snapshot_digest,
            status: row.status,
            created_at: fromDBDate(row.created_at) || row.created_at,
            completed_at: fromDBDate(row.completed_at || undefined)
        };
    }
    actionDecisionFromRow(row) {
        return {
            decision_id: row.decision_id,
            decision_seq: Number(row.decision_seq),
            workspace_id: Number(row.workspace_id),
            session_id: Number(row.session_id),
            runtime_run_id: row.runtime_run_id,
            action_id: Number(row.action_id),
            decision_type: row.decision_type,
            decision: row.decision,
            actor_user_id: row.actor_user_id ? Number(row.actor_user_id) : undefined,
            reason: row.reason || '',
            input_patch_json: parseJSON(row.input_patch_json, {}),
            client_decision_id: row.client_decision_id,
            idempotency_key: row.idempotency_key,
            created_at: fromDBDate(row.created_at) || row.created_at
        };
    }
    sessionPermissionGrantFromRow(row) {
        return {
            grant_id: row.grant_id,
            grant_seq: Number(row.grant_seq),
            workspace_id: Number(row.workspace_id),
            session_id: Number(row.session_id),
            runtime_run_id: row.runtime_run_id,
            action_id: Number(row.action_id),
            permission_key: row.permission_key,
            tool_name: row.tool_name,
            executor_type: row.executor_type,
            resource_type: row.resource_type,
            resource_id: row.resource_id,
            status: row.status,
            granted_by: Number(row.granted_by),
            created_at: fromDBDate(row.created_at) || row.created_at,
            expires_at: fromDBDate(row.expires_at) || row.expires_at,
            revoked_at: fromDBDate(row.revoked_at || undefined)
        };
    }
    actionExecutionFromRow(row) {
        return {
            execution_id: row.execution_id,
            workspace_id: Number(row.workspace_id),
            session_id: Number(row.session_id),
            runtime_run_id: row.runtime_run_id,
            action_id: Number(row.action_id),
            attempt: Number(row.attempt),
            executor_type: row.executor_type,
            executor_ref_json: parseJSON(row.executor_ref_json, {}),
            idempotency_key: row.idempotency_key,
            status: row.status,
            external_request_id: row.external_request_id || undefined,
            result_json: parseJSON(row.result_json, {}),
            error_code: row.error_code || undefined,
            error_msg: row.error_msg || undefined,
            started_at: fromDBDate(row.started_at) || row.started_at,
            finished_at: fromDBDate(row.finished_at || undefined),
            created_at: fromDBDate(row.created_at) || row.created_at,
            updated_at: fromDBDate(row.updated_at) || row.updated_at
        };
    }
}
export function createMariadbRuntimeStore(options) {
    return MariadbRuntimeStore.create(options);
}
