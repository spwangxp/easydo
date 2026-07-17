import { Type } from '@earendil-works/pi-ai';
import { filterToolsByMode } from '../services/subagentMode.js';
export function createWorkspaceTools(env, runMode = 'write') {
    const tools = [
        {
            name: 'bash',
            operation_type: 'execute',
            label: 'Run shell command',
            description: 'Run a Bash command inside the isolated Agent workspace. Use this for repository inspection, builds, tests, Git, and command-line tools.',
            parameters: Type.Object({
                command: Type.String({ description: 'Bash command to execute' }),
                cwd: Type.Optional(Type.String({ description: 'Workspace-relative working directory' })),
                timeout_seconds: Type.Optional(Type.Integer({ minimum: 1, maximum: 3600 }))
            }),
            async execute(_toolCallID, params) {
                const input = asRecord(params);
                const result = await env.exec(requiredString(input.command, 'command'), {
                    ...(optionalString(input.cwd) ? { cwd: optionalString(input.cwd) } : {}),
                    timeout: optionalPositiveNumber(input.timeout_seconds)
                });
                const value = resultOrThrow(result);
                const output = [value.stdout, value.stderr].filter(Boolean).join(value.stdout && value.stderr ? '\n' : '');
                return {
                    content: [{ type: 'text', text: output || `(command exited ${value.exitCode} with no output)` }],
                    details: { exit_code: value.exitCode, stdout: value.stdout, stderr: value.stderr }
                };
            }
        },
        {
            name: 'read_file',
            operation_type: 'read',
            label: 'Read workspace file',
            description: 'Read a UTF-8 text file from the isolated Agent workspace.',
            parameters: Type.Object({
                path: Type.String(),
                max_lines: Type.Optional(Type.Integer({ minimum: 1, maximum: 100000 }))
            }),
            async execute(_toolCallID, params) {
                const input = asRecord(params);
                const maxLines = optionalPositiveNumber(input.max_lines);
                const value = maxLines
                    ? resultOrThrow(await env.readTextLines(requiredString(input.path, 'path'), { maxLines }))
                    : resultOrThrow(await env.readTextFile(requiredString(input.path, 'path')));
                const text = Array.isArray(value) ? value.join('\n') : value;
                return { content: [{ type: 'text', text }], details: { path: input.path, max_lines: maxLines } };
            }
        },
        {
            name: 'write_file',
            operation_type: 'write',
            label: 'Write workspace file',
            description: 'Create or overwrite a UTF-8 text file in the isolated Agent workspace. Parent directories are created by the workspace backend.',
            parameters: Type.Object({
                path: Type.String(),
                content: Type.String(),
                append: Type.Optional(Type.Boolean())
            }),
            async execute(_toolCallID, params) {
                const input = asRecord(params);
                const target = requiredString(input.path, 'path');
                const content = requiredString(input.content, 'content', true);
                resultOrThrow(input.append === true ? await env.appendFile(target, content) : await env.writeFile(target, content));
                return { content: [{ type: 'text', text: `Wrote ${Buffer.byteLength(content)} bytes to ${target}` }], details: { path: target, append: input.append === true } };
            }
        },
        {
            name: 'edit_file',
            operation_type: 'write',
            label: 'Edit workspace file',
            description: 'Replace exactly one matching text block in a UTF-8 workspace file. Fails if the old text is absent or occurs more than once.',
            parameters: Type.Object({
                path: Type.String(),
                old_text: Type.String(),
                new_text: Type.String()
            }),
            async execute(_toolCallID, params) {
                const input = asRecord(params);
                const target = requiredString(input.path, 'path');
                const oldText = requiredString(input.old_text, 'old_text');
                const newText = requiredString(input.new_text, 'new_text', true);
                const source = resultOrThrow(await env.readTextFile(target));
                const occurrences = source.split(oldText).length - 1;
                if (occurrences !== 1)
                    throw new Error(`edit_file expected exactly one match in ${target}, found ${occurrences}`);
                resultOrThrow(await env.writeFile(target, source.replace(oldText, newText)));
                return { content: [{ type: 'text', text: `Edited ${target}` }], details: { path: target } };
            }
        },
        {
            name: 'list_directory',
            operation_type: 'read',
            label: 'List workspace directory',
            description: 'List direct children of a directory in the isolated Agent workspace.',
            parameters: Type.Object({ path: Type.Optional(Type.String()) }),
            async execute(_toolCallID, params) {
                const target = optionalString(asRecord(params).path) || '.';
                const entries = resultOrThrow(await env.listDir(target));
                return { content: [{ type: 'text', text: JSON.stringify(entries, null, 2) }], details: { path: target, entries } };
            }
        },
        {
            name: 'file_info',
            operation_type: 'read',
            label: 'Inspect workspace path',
            description: 'Return metadata for a file, directory, or symlink in the isolated Agent workspace.',
            parameters: Type.Object({ path: Type.String() }),
            async execute(_toolCallID, params) {
                const target = requiredString(asRecord(params).path, 'path');
                const info = resultOrThrow(await env.fileInfo(target));
                return { content: [{ type: 'text', text: JSON.stringify(info, null, 2) }], details: info };
            }
        }
    ];
    return filterToolsByMode(runMode, tools, (tool) => tool.operation_type);
}
function resultOrThrow(result) {
    if (!result.ok)
        throw result.error;
    return result.value;
}
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
}
function requiredString(value, name, allowEmpty = false) {
    if (typeof value !== 'string' || (!allowEmpty && !value))
        throw new Error(`${name} is required`);
    return value;
}
function optionalString(value) {
    return typeof value === 'string' && value.trim() ? value.trim() : '';
}
function optionalPositiveNumber(value) {
    const number = Number(value);
    return Number.isFinite(number) && number > 0 ? number : undefined;
}
