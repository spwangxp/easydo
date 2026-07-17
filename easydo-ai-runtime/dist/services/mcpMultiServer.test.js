import { describe, expect, it } from 'vitest';
import { buildStdioSandboxEnv, resolveMcpClientConfigSecrets } from './mcpStreamableHttpClient.js';
import { probeMcpServerConfig } from './profileService.js';
describe('MCP multi-server and sandbox hardening', () => {
    it('stdio sandbox env does not inherit full process secrets by default', () => {
        const previous = process.env.AWS_SECRET_ACCESS_KEY;
        process.env.AWS_SECRET_ACCESS_KEY = 'super-secret';
        process.env.PATH = process.env.PATH || '/usr/bin';
        try {
            const env = buildStdioSandboxEnv({ CUSTOM_TOKEN: 'abc' });
            expect(env.CUSTOM_TOKEN).toBe('abc');
            expect(env.PATH).toBeTruthy();
            expect(env.AWS_SECRET_ACCESS_KEY).toBeUndefined();
            expect(env.EASYDO_MCP_STDIO_SANDBOX).toBe('1');
        }
        finally {
            if (previous === undefined)
                delete process.env.AWS_SECRET_ACCESS_KEY;
            else
                process.env.AWS_SECRET_ACCESS_KEY = previous;
        }
    });
    it('probe rejects incomplete MCP configs before opening a transport', async () => {
        await expect(probeMcpServerConfig({ type: 'streamable_http' })).rejects.toMatchObject({
            code: 'mcp_server_endpoint_required'
        });
        await expect(probeMcpServerConfig({ type: 'stdio' })).rejects.toMatchObject({
            code: 'mcp_server_endpoint_required'
        });
    });
    it('resolves MCP header and env secret_ref placeholders before client creation', () => {
        const resolved = resolveMcpClientConfigSecrets({
            type: 'streamable_http',
            url: 'https://mcp.example.test',
            headers: {
                Authorization: 'secret_ref:headers.Authorization',
                'X-Plain': 'visible'
            },
            env: {
                API_TOKEN: 'secret_ref:env.API_TOKEN'
            }
        }, {
            values: {
                'headers.Authorization': 'Bearer real-token',
                'env.API_TOKEN': 'env-secret'
            }
        });
        expect(resolved).toMatchObject({
            headers: {
                Authorization: 'Bearer real-token',
                'X-Plain': 'visible'
            },
            env: {
                API_TOKEN: 'env-secret'
            }
        });
    });
    it('rejects unresolved MCP secret_ref placeholders instead of sending placeholders to the server', () => {
        expect(() => resolveMcpClientConfigSecrets({
            headers: { Authorization: 'secret_ref:headers.Authorization' }
        }, { values: {} })).toThrow(/MCP secret_ref headers.Authorization is not configured/);
    });
});
