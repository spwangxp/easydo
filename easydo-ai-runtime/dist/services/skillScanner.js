import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, readdir, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { promisify } from 'node:util';
const execFileAsync = promisify(execFile);
const MAX_SKILL_FILES = 300;
const MAX_SCAN_DEPTH = 6;
function asRecord(value) {
    return value && typeof value === 'object' && !Array.isArray(value) ? { ...value } : {};
}
function asString(value) {
    return typeof value === 'string' ? value.trim() : '';
}
function repositoryConfig(resource) {
    const repository = asRecord(resource.spec.repository || resource.endpoint);
    return {
        url: asString(repository.url || resource.spec.url),
        branch: asString(repository.branch || resource.spec.branch) || 'main',
        commit: asString(repository.commit || repository.commit_sha || resource.spec.commit || resource.spec.commit_sha),
        subdirectory: asString(repository.subdirectory || resource.spec.subdirectory) || '.'
    };
}
function isLocalRepositoryURL(url) {
    return url.startsWith('file://') || isAbsolute(url);
}
async function resolveRemoteCommit(url, branch, commit) {
    const explicit = commit.trim();
    if (/^[a-f0-9]{40,64}$/i.test(explicit))
        return explicit.toLowerCase();
    const ref = (branch.trim() || 'main').replace(/^refs\/heads\//, '');
    try {
        const { stdout } = await execFileAsync('git', ['ls-remote', '--heads', url, ref], {
            timeout: 60_000,
            maxBuffer: 1024 * 1024
        });
        const line = stdout
            .split(/\r?\n/)
            .map((item) => item.trim())
            .find((item) => item && (item.endsWith(`\trefs/heads/${ref}`) || item.endsWith(` refs/heads/${ref}`)));
        const digest = line?.split(/[\s\t]+/)[0] || '';
        if (/^[a-f0-9]{40,64}$/i.test(digest))
            return digest.toLowerCase();
    }
    catch (error) {
        const message = error instanceof Error ? error.message : 'Git ls-remote failed';
        throw new Error(`Failed to resolve skill repository branch "${ref}": ${message}`);
    }
    throw new Error(`Skill repository commit is required and must be a full Git commit digest (branch "${ref}" could not be resolved)`);
}
async function cloneRepository(url, commit) {
    const baseDir = await mkdtemp(join(tmpdir(), 'easydo-skill-scan-'));
    const cloneDir = join(baseDir, 'repo');
    await mkdir(cloneDir, { recursive: true });
    try {
        await execFileAsync('git', ['clone', '--no-checkout', '--filter=blob:none', url, cloneDir], {
            timeout: 120_000,
            maxBuffer: 1024 * 1024
        });
        await execFileAsync('git', ['-C', cloneDir, 'checkout', '--detach', commit], {
            timeout: 120_000,
            maxBuffer: 1024 * 1024
        });
        return { rootDir: cloneDir, cleanupDir: baseDir, commit };
    }
    catch (error) {
        await rm(baseDir, { recursive: true, force: true });
        const message = error instanceof Error ? error.message : 'Git clone failed';
        throw new Error(message);
    }
}
async function repositoryRoot(url, branch, commit) {
    if (isLocalRepositoryURL(url)) {
        throw new Error('Local Skill repositories are not supported in the multi-tenant Runtime');
    }
    const resolvedCommit = await resolveRemoteCommit(url, branch, commit);
    return cloneRepository(url, resolvedCommit);
}
function normalizeSubdirectory(subdirectory) {
    const normalized = subdirectory.trim() || '.';
    if (normalized.includes('..')) {
        throw new Error('Skill repository subdirectory is invalid');
    }
    return normalized;
}
async function findSkillFiles(rootDir, depth = 0, found = []) {
    if (found.length >= MAX_SKILL_FILES || depth > MAX_SCAN_DEPTH)
        return found;
    const entries = await readdir(rootDir, { withFileTypes: true });
    for (const entry of entries) {
        if (found.length >= MAX_SKILL_FILES)
            break;
        if (entry.name === '.git' || entry.name === 'node_modules')
            continue;
        const path = join(rootDir, entry.name);
        if (entry.isFile() && entry.name === 'SKILL.md') {
            found.push(path);
        }
        else if (entry.isDirectory()) {
            await findSkillFiles(path, depth + 1, found);
        }
    }
    return found;
}
function parseFrontmatter(markdown) {
    if (!markdown.startsWith('---'))
        return {};
    const endIndex = markdown.indexOf('\n---', 3);
    if (endIndex < 0)
        return {};
    const block = markdown.slice(3, endIndex).trim();
    const result = {};
    for (const line of block.split(/\r?\n/)) {
        const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
        if (!match)
            continue;
        result[match[1]] = match[2].replace(/^['"]|['"]$/g, '').trim();
    }
    return result;
}
function firstHeading(markdown) {
    const match = markdown.match(/^#\s+(.+)$/m);
    return match?.[1]?.trim() || '';
}
function firstParagraph(markdown) {
    return markdown
        .replace(/^---[\s\S]*?\n---/, '')
        .split(/\r?\n\r?\n/)
        .map((item) => item.trim())
        .find((item) => item && !item.startsWith('#')) || '';
}
function readableKeySegment(value, fallback = 'skill', maxLength = 64) {
    const normalized = asString(value)
        .toLowerCase()
        .replace(/(^|\/)skill\.md$/i, '')
        .replace(/[^a-z0-9._-]+/g, '-')
        .replace(/-+/g, '-')
        .replace(/^-|-$/g, '')
        .replace(/^\.+|\.+$/g, '');
    return (normalized || fallback).slice(0, maxLength);
}
function skillNameFallback(relativePath) {
    const normalized = relativePath.replaceAll('\\', '/');
    if (normalized.toLowerCase() === 'skill.md')
        return 'skill';
    const directoryName = basename(dirname(normalized));
    return directoryName && directoryName !== '.' ? directoryName : 'skill';
}
function skillKey(repositoryKey, skillName, skillPath) {
    const suffix = readableKeySegment(skillName || skillPath);
    return `${repositoryKey}/${suffix}`;
}
async function parseSkillFile(file, scanRoot, repository, commit) {
    const markdown = await readFile(file, 'utf8');
    const frontmatter = parseFrontmatter(markdown);
    const relativePath = relative(scanRoot, file).replaceAll('\\', '/');
    const name = frontmatter.name || firstHeading(markdown) || skillNameFallback(relativePath);
    const description = frontmatter.description || firstParagraph(markdown);
    const version = frontmatter.version || 'latest';
    const tags = frontmatter.tags ? frontmatter.tags.split(',').map((item) => item.trim()).filter(Boolean) : [];
    return {
        key: skillKey(repository.resource_key, name, relativePath),
        name,
        description,
        version,
        path: relativePath,
        entry: relativePath,
        content: markdown,
        tags,
        manifest: {
            ...frontmatter,
            format: 'SKILL.md',
            repository_commit: commit
        }
    };
}
export async function scanSkillRepository(resource) {
    if (resource.resource_kind !== 'skill' || resource.spec.resource_subtype !== 'skill_repository') {
        throw new Error('Agent resource is not a skill repository');
    }
    const config = repositoryConfig(resource);
    if (!config.url) {
        throw new Error('Skill repository URL is required');
    }
    const { rootDir, cleanupDir, commit } = await repositoryRoot(config.url, config.branch, config.commit);
    try {
        const subdirectory = normalizeSubdirectory(config.subdirectory);
        const scanRoot = resolve(rootDir, subdirectory);
        if (!scanRoot.startsWith(resolve(rootDir))) {
            throw new Error('Skill repository subdirectory is invalid');
        }
        const files = await findSkillFiles(scanRoot);
        const skills = await Promise.all(files.map((file) => parseSkillFile(file, scanRoot, resource, commit)));
        return skills.sort((left, right) => left.key.localeCompare(right.key));
    }
    finally {
        if (cleanupDir) {
            await rm(cleanupDir, { recursive: true, force: true });
        }
    }
}
export async function readSkillRepositoryEntry(repository, entry) {
    if (repository.resource_kind !== 'skill' || repository.spec.resource_subtype !== 'skill_repository') {
        throw new Error('Agent resource is not a skill repository');
    }
    const config = repositoryConfig(repository);
    if (!config.url) {
        throw new Error('Skill repository URL is required');
    }
    if (!entry.trim()) {
        throw new Error('Skill repository entry is required');
    }
    const normalizedEntry = normalizeSubdirectory(entry);
    const { rootDir, cleanupDir } = await repositoryRoot(config.url, config.branch, config.commit);
    try {
        const subdirectory = normalizeSubdirectory(config.subdirectory);
        const scanRoot = resolve(rootDir, subdirectory);
        if (!scanRoot.startsWith(resolve(rootDir))) {
            throw new Error('Skill repository subdirectory is invalid');
        }
        const file = resolve(scanRoot, normalizedEntry);
        const relativeFile = relative(scanRoot, file);
        if (!relativeFile || relativeFile.startsWith('..') || isAbsolute(relativeFile)) {
            throw new Error('Skill repository entry is invalid');
        }
        return readFile(file, 'utf8');
    }
    finally {
        if (cleanupDir) {
            await rm(cleanupDir, { recursive: true, force: true });
        }
    }
}
