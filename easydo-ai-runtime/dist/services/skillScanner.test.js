import { describe, expect, it } from 'vitest';
import { scanSkillRepository } from './skillScanner.js';
function repository(url, commit = '') {
    return {
        id: 1,
        workspace_id: 11,
        resource_kind: 'skill',
        resource_key: 'repo-skill',
        resource_id: 'repo-skill',
        name: 'Repository Skill',
        description: '',
        version: '1',
        status: 'active',
        spec: {
            resource_subtype: 'skill_repository',
            repository: { url, commit, subdirectory: '.' }
        },
        endpoint: {},
        secret_ref: {},
        tags: [],
        created_by: 7,
        created_at: '2026-07-11T00:00:00.000Z',
        updated_at: '2026-07-11T00:00:00.000Z'
    };
}
describe('skillScanner repository isolation', () => {
    it('rejects local filesystem repositories in the multi-tenant Runtime', async () => {
        await expect(scanSkillRepository(repository('file:///tmp/tenant-skill', 'a'.repeat(40))))
            .rejects.toThrow('Local Skill repositories are not supported');
    });
    it('requires a resolvable remote branch or commit before cloning', async () => {
        await expect(scanSkillRepository(repository('https://example.invalid/skills.git')))
            .rejects.toThrow(/Failed to resolve skill repository branch|could not be resolved|commit is required/);
    });
});
