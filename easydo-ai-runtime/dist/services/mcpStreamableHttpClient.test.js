import { createServer } from 'node:http';
import { afterEach, describe, expect, it } from 'vitest';
import { McpSseClient, McpStdioClient, McpStreamableHttpClient } from './mcpStreamableHttpClient.js';
import { createRuntimeLogger } from '../observability/runtimeLogger.js';
const servers = [];
afterEach(async () => {
    await Promise.all(servers.splice(0).map((server) => new Promise((resolve) => server.close(() => resolve()))));
});
describe('McpStreamableHttpClient', () => {
    it('logs MCP request outcomes without logging parameters or results', async () => {
        const server = createServer((request, response) => {
            let body = '';
            request.on('data', (chunk) => { body += chunk; });
            request.on('end', () => {
                const message = JSON.parse(body);
                response.setHeader('content-type', 'application/json');
                if (message.method === 'initialize') {
                    response.setHeader('mcp-session-id', 'session-log');
                    response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: {} }));
                    return;
                }
                if (message.method === 'notifications/initialized') {
                    response.statusCode = 202;
                    response.end();
                    return;
                }
                response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: { content: 'tool result secret' } }));
            });
        });
        servers.push(server);
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const address = server.address();
        const lines = [];
        const client = new McpStreamableHttpClient(`http://127.0.0.1:${typeof address === 'object' && address ? address.port : 0}/mcp`, { Authorization: 'Bearer mcp-secret' }, 1000, createRuntimeLogger({ sink: (line) => lines.push(line) }));
        await client.request('tools/call', { prompt: 'tool parameter secret' }, 'mcp-request-1', {
            request_id: 'req-mcp-1',
            workspace_id: 11,
            session_id: 'session-1',
            runtime_run_id: 'run-mcp-1',
            mcp_server_id: 'filesystem'
        });
        expect(lines.map((line) => JSON.parse(line))).toEqual([
            expect.objectContaining({
                component: 'mcp-client',
                operation: 'tools/call',
                outcome: 'started',
                request_id: 'req-mcp-1',
                workspace_id: 11,
                session_id: 'session-1',
                runtime_run_id: 'run-mcp-1',
                mcp_server_id: 'filesystem'
            }),
            expect.objectContaining({ component: 'mcp-client', operation: 'tools/call', outcome: 'completed' })
        ]);
        expect(lines.join('\n')).not.toContain('mcp-secret');
        expect(lines.join('\n')).not.toContain('tool parameter secret');
        expect(lines.join('\n')).not.toContain('tool result secret');
    });
    it('initializes once and reuses the MCP session id for later requests', async () => {
        const calls = [];
        const server = createServer((request, response) => {
            let body = '';
            request.on('data', (chunk) => { body += chunk; });
            request.on('end', () => {
                const message = JSON.parse(body);
                calls.push({ method: message.method, session: String(request.headers['mcp-session-id'] || '') });
                response.setHeader('content-type', 'application/json');
                if (message.method === 'initialize') {
                    response.setHeader('mcp-session-id', 'session-1');
                    response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: { protocolVersion: '2025-06-18', capabilities: {} } }));
                    return;
                }
                if (message.method === 'notifications/initialized') {
                    response.statusCode = 202;
                    response.end();
                    return;
                }
                response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: { tools: [{ name: 'workspace_list' }] } }));
            });
        });
        servers.push(server);
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const address = server.address();
        const client = new McpStreamableHttpClient(`http://127.0.0.1:${typeof address === 'object' && address ? address.port : 0}/mcp`, {}, 1000);
        const first = await client.request('tools/list', {});
        const second = await client.request('tools/list', {});
        expect(first).toMatchObject({ tools: [{ name: 'workspace_list' }] });
        expect(second).toMatchObject({ tools: [{ name: 'workspace_list' }] });
        expect(calls).toEqual([
            { method: 'initialize', session: '' },
            { method: 'notifications/initialized', session: 'session-1' },
            { method: 'tools/list', session: 'session-1' },
            { method: 'tools/list', session: 'session-1' }
        ]);
    });
    it('aborts an MCP request after the configured timeout', async () => {
        const server = createServer(() => { });
        servers.push(server);
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const address = server.address();
        const client = new McpStreamableHttpClient(`http://127.0.0.1:${typeof address === 'object' && address ? address.port : 0}/mcp`, {}, 20);
        await expect(client.request('tools/list', {})).rejects.toMatchObject({
            source: 'mcp',
            code: 'mcp_timeout',
            http_status: 504,
            retryable: true,
            message: expect.stringContaining('MCP request timed out')
        });
    });
    it('marks an MCP HTTP 429 response as retryable', async () => {
        const server = createServer((request, response) => {
            let body = '';
            request.on('data', (chunk) => { body += chunk; });
            request.on('end', () => {
                const message = JSON.parse(body);
                response.setHeader('content-type', 'application/json');
                if (message.method === 'initialize') {
                    response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: { protocolVersion: '2025-06-18', capabilities: {} } }));
                    return;
                }
                if (message.method === 'notifications/initialized') {
                    response.statusCode = 202;
                    response.end();
                    return;
                }
                response.statusCode = 429;
                response.end(JSON.stringify({ error: 'rate limited' }));
            });
        });
        servers.push(server);
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const address = server.address();
        const client = new McpStreamableHttpClient(`http://127.0.0.1:${typeof address === 'object' && address ? address.port : 0}/mcp`, {}, 1000);
        await expect(client.request('tools/list', {})).rejects.toMatchObject({
            source: 'mcp',
            code: 'mcp_rate_limit',
            http_status: 429,
            retryable: true
        });
    });
    it('marks a JSON-RPC application error as non-retryable despite its synthetic 502 status', async () => {
        const server = createServer((request, response) => {
            let body = '';
            request.on('data', (chunk) => { body += chunk; });
            request.on('end', () => {
                const message = JSON.parse(body);
                response.setHeader('content-type', 'application/json');
                if (message.method === 'initialize') {
                    response.end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result: { protocolVersion: '2025-06-18', capabilities: {} } }));
                    return;
                }
                if (message.method === 'notifications/initialized') {
                    response.statusCode = 202;
                    response.end();
                    return;
                }
                response.end(JSON.stringify({
                    jsonrpc: '2.0',
                    id: message.id,
                    error: { code: -32601, message: 'Unknown MCP tool' }
                }));
            });
        });
        servers.push(server);
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const address = server.address();
        const client = new McpStreamableHttpClient(`http://127.0.0.1:${typeof address === 'object' && address ? address.port : 0}/mcp`, {}, 1000);
        await expect(client.request('tools/list', {})).rejects.toMatchObject({
            source: 'mcp',
            code: 'mcp_rpc_error',
            http_status: 502,
            retryable: false
        });
    });
    it('calls tools/list over legacy MCP SSE transport', async () => {
        let sseResponse;
        const calls = [];
        const server = createServer((request, response) => {
            if (request.method === 'GET' && request.url === '/mcp/sse') {
                response.writeHead(200, {
                    'content-type': 'text/event-stream',
                    'cache-control': 'no-cache',
                    connection: 'keep-alive'
                });
                sseResponse = response;
                response.write('event: endpoint\ndata: /mcp/sse/session-1/message\n\n');
                return;
            }
            if (request.method === 'POST' && request.url === '/mcp/sse/session-1/message') {
                let body = '';
                request.on('data', (chunk) => { body += chunk; });
                request.on('end', () => {
                    const message = JSON.parse(body);
                    calls.push(message.method);
                    response.statusCode = 202;
                    response.end();
                    if (!message.id)
                        return;
                    const result = message.method === 'tools/list'
                        ? { tools: [{ name: 'sse_tool', description: 'SSE tool' }] }
                        : { protocolVersion: '2025-06-18', capabilities: {} };
                    sseResponse?.write(`event: message\ndata: ${JSON.stringify({ jsonrpc: '2.0', id: message.id, result })}\n\n`);
                });
                return;
            }
            response.statusCode = 404;
            response.end();
        });
        servers.push(server);
        await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
        const address = server.address();
        const client = new McpSseClient(`http://127.0.0.1:${typeof address === 'object' && address ? address.port : 0}/mcp/sse`, {}, 1000);
        const result = await client.request('tools/list', {});
        await client.close();
        expect(result).toMatchObject({ tools: [{ name: 'sse_tool' }] });
        expect(calls).toEqual(['initialize', 'notifications/initialized', 'tools/list']);
    });
    it('calls tools/list over MCP stdio transport without invoking a shell', async () => {
        const script = [
            "const readline = require('node:readline')",
            "const rl = readline.createInterface({ input: process.stdin })",
            "rl.on('line', (line) => {",
            "  const message = JSON.parse(line)",
            "  if (!message.id) return",
            "  const result = message.method === 'tools/list'",
            "    ? { tools: [{ name: 'stdio_tool', description: 'stdio tool' }] }",
            "    : { protocolVersion: '2025-06-18', capabilities: {} }",
            "  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: message.id, result }) + '\\n')",
            "})"
        ].join('\n');
        const client = new McpStdioClient(process.execPath, ['-e', script], {}, '', 1000);
        const result = await client.request('tools/list', {});
        await client.close();
        expect(result).toMatchObject({ tools: [{ name: 'stdio_tool' }] });
    });
});
