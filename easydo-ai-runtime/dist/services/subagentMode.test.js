import { describe, expect, it } from 'vitest';
import { assertReadOnlyAllowsOperation, filterToolsByMode, isWriteOperationType, normalizeSubagentMode, operationTypeFromToolMeta, resolveSubagentMode } from './subagentMode.js';
import { RuntimeDomainError } from './runtimeErrors.js';
describe('normalizeSubagentMode', () => {
    it('returns write when raw contains write case-insensitively', () => {
        expect(normalizeSubagentMode('write')).toBe('write');
        expect(normalizeSubagentMode('WRITE')).toBe('write');
        expect(normalizeSubagentMode('allow_write')).toBe('write');
        expect(normalizeSubagentMode('Read_Write')).toBe('write');
    });
    it('returns read_only for anything else including empty and non-string', () => {
        expect(normalizeSubagentMode('read_only')).toBe('read_only');
        expect(normalizeSubagentMode('read')).toBe('read_only');
        expect(normalizeSubagentMode('')).toBe('read_only');
        expect(normalizeSubagentMode(null)).toBe('read_only');
        expect(normalizeSubagentMode(undefined)).toBe('read_only');
        expect(normalizeSubagentMode(42)).toBe('read_only');
    });
});
describe('resolveSubagentMode', () => {
    it('uses the configured write capability when the model omits mode', () => {
        expect(resolveSubagentMode('write', undefined)).toBe('write');
    });
    it('allows the model to narrow a configured write capability', () => {
        expect(resolveSubagentMode('write', 'read_only')).toBe('read_only');
    });
    it('never lets the model elevate a read_only or missing capability', () => {
        expect(resolveSubagentMode('read_only', 'write')).toBe('read_only');
        expect(resolveSubagentMode(undefined, 'write')).toBe('read_only');
    });
});
describe('isWriteOperationType', () => {
    it('matches toolPermissionPolicy write operation list', () => {
        for (const op of ['write', 'create', 'update', 'delete', 'mutate', 'mutation', 'execute', 'deploy', 'run']) {
            expect(isWriteOperationType(op)).toBe(true);
        }
    });
    it('is case-insensitive for known write operations', () => {
        expect(isWriteOperationType('WRITE')).toBe(true);
        expect(isWriteOperationType('Create')).toBe(true);
        expect(isWriteOperationType(' write ')).toBe(true);
    });
    it('returns false for read and unknown operations', () => {
        expect(isWriteOperationType('read')).toBe(false);
        expect(isWriteOperationType('list')).toBe(false);
        expect(isWriteOperationType('')).toBe(false);
        expect(isWriteOperationType('unknown')).toBe(false);
    });
});
describe('operationTypeFromToolMeta', () => {
    it('prefers operation_type then operationType then operation', () => {
        expect(operationTypeFromToolMeta({ operation_type: 'read', operationType: 'write', operation: 'delete' })).toBe('read');
        expect(operationTypeFromToolMeta({ operationType: 'write', operation: 'delete' })).toBe('write');
        expect(operationTypeFromToolMeta({ operation: 'delete' })).toBe('delete');
    });
    it('returns empty string when none present or values are empty', () => {
        expect(operationTypeFromToolMeta({})).toBe('');
        expect(operationTypeFromToolMeta({ operation_type: '', operationType: '  ', operation: null })).toBe('');
    });
});
describe('assertReadOnlyAllowsOperation', () => {
    it('allows any operation when mode is write', () => {
        expect(() => assertReadOnlyAllowsOperation('write', 'write')).not.toThrow();
        expect(() => assertReadOnlyAllowsOperation('write', 'delete')).not.toThrow();
        expect(() => assertReadOnlyAllowsOperation('write', '')).not.toThrow();
        expect(() => assertReadOnlyAllowsOperation('write', 'unknown')).not.toThrow();
    });
    it('allows only read when mode is read_only', () => {
        expect(() => assertReadOnlyAllowsOperation('read_only', 'read')).not.toThrow();
        expect(() => assertReadOnlyAllowsOperation('read_only', 'READ')).not.toThrow();
        expect(() => assertReadOnlyAllowsOperation('read_only', ' read ')).not.toThrow();
    });
    it('throws RuntimeDomainError 403 for write or non-read under read_only', () => {
        for (const op of ['write', 'create', '', 'unknown', 'list']) {
            try {
                assertReadOnlyAllowsOperation('read_only', op);
                expect.fail(`expected throw for operationType=${JSON.stringify(op)}`);
            }
            catch (error) {
                expect(error).toBeInstanceOf(RuntimeDomainError);
                if (!(error instanceof RuntimeDomainError))
                    throw error;
                expect(error.code).toBe('subagent_read_only_violation');
                expect(error.status).toBe(403);
                expect(error.message.length).toBeGreaterThan(0);
            }
        }
    });
});
describe('filterToolsByMode', () => {
    const tools = [
        { name: 'a', op: 'read' },
        { name: 'b', op: 'write' },
        { name: 'c', op: 'READ' },
        { name: 'd', op: 'delete' },
        { name: 'e', op: '' },
        { name: 'f', op: 'unknown' }
    ];
    it('returns all tools unchanged in write mode', () => {
        expect(filterToolsByMode('write', tools, (tool) => tool.op)).toBe(tools);
    });
    it('keeps only tools whose resolved op is read (case-insensitive) in read_only mode', () => {
        expect(filterToolsByMode('read_only', tools, (tool) => tool.op)).toEqual([
            { name: 'a', op: 'read' },
            { name: 'c', op: 'READ' }
        ]);
    });
    it('keeps read operations with surrounding whitespace in read_only mode', () => {
        const spaced = [{ name: 'read-spaced', op: ' read ' }, { name: 'write-spaced', op: ' write ' }];
        expect(filterToolsByMode('read_only', spaced, (tool) => tool.op)).toEqual([spaced[0]]);
    });
});
