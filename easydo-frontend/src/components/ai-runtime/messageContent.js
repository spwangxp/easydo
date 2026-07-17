function firstText(...values) {
  for (const value of values) {
    if (value === undefined || value === null) continue
    const text = String(value).trim()
    if (text) return text
  }
  return ''
}

function safeJsonParse(value) {
  if (typeof value !== 'string') return null
  const text = value.trim()
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

function normalizeBlockList(value) {
  if (Array.isArray(value)) return value
  if (value && typeof value === 'object') {
    if (Array.isArray(value.blocks)) return value.blocks
    if (Array.isArray(value.content_blocks)) return value.content_blocks
    if (Array.isArray(value.items)) return value.items
    return [value]
  }
  const parsed = safeJsonParse(value)
  if (Array.isArray(parsed)) return parsed
  if (parsed && typeof parsed === 'object') {
    if (Array.isArray(parsed.blocks)) return parsed.blocks
    if (Array.isArray(parsed.content_blocks)) return parsed.content_blocks
    if (Array.isArray(parsed.items)) return parsed.items
    return [parsed]
  }
  return []
}

function blockKindFromType(type, fallbackText = '') {
  const raw = firstText(type).toLowerCase()
  if (!raw) {
    return fallbackText ? 'text' : 'text'
  }
  if (raw.includes('html') || raw === 'html' || raw === 'text/html') return 'html'
  if (raw.includes('markdown') || raw === 'md' || raw === 'text/markdown') return 'markdown'
  return 'text'
}

function normalizeBlock(block = {}, fallbackType = '', options = {}) {
  const text = firstText(
    block.text,
    block.content,
    block.value,
    block.markdown,
    block.html,
    block.body,
    block.delta,
    block.raw
  )
  const rawType = firstText(
    block.type,
    block.mime_type,
    block.content_type,
    block.format,
    fallbackType
  )
  let kind = blockKindFromType(rawType, text)
  if (options.defaultMarkdown && kind === 'text') {
    kind = 'markdown'
  }
  return {
    kind,
    rawType,
    text,
    label: firstText(block.label, block.title, block.name, kind === 'html' ? 'HTML' : kind === 'markdown' ? 'Markdown' : 'Text')
  }
}

function contentTypeFromEntry(entry = {}) {
  return firstText(
    entry.content_type,
    entry.content_format,
    entry.mime_type,
    entry.format,
    entry.output?.content_type,
    entry.output?.content_format,
    entry.output?.mime_type,
    entry.output?.message?.content_type,
    entry.output?.message?.content_format,
    entry.output?.message?.mime_type
  )
}

function contentBlocksFromEntry(entry = {}) {
  const candidates = [
    entry.content_blocks,
    entry.content_blocks_json,
    entry.output?.content_blocks,
    entry.output?.content_blocks_json,
    entry.output?.message?.content_blocks,
    entry.output?.message?.content_blocks_json,
    entry.output?.message?.blocks,
    entry.output?.message?.parts
  ]
  for (const candidate of candidates) {
    const blocks = normalizeBlockList(candidate)
    if (blocks.length > 0) {
      const fallbackType = contentTypeFromEntry(entry)
      return blocks.map((block) => normalizeBlock(block, fallbackType, { defaultMarkdown: entry.role === 'assistant' }))
    }
  }

  const text = firstText(entry.content, entry.output?.message?.content, entry.output?.message?.text)
  if (!text) return []
  return [normalizeBlock(
    { type: contentTypeFromEntry(entry) || 'text', text },
    '',
    { defaultMarkdown: entry.role === 'assistant' }
  )]
}

function sanitizeUrl(url) {
  const text = firstText(url)
  if (!text) return ''
  if (/^(https?:|mailto:|tel:|\/|#|\.{1,2}\/)/i.test(text)) return text
  return ''
}

function escapeHtml(value) {
  return String(value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

function escapeAttribute(value) {
  return escapeHtml(value).replace(/`/g, '&#96;')
}

function renderInlineMarkdown(text) {
  const source = String(text ?? '')
  const codeTokens = []
  const codePlaceholder = source.replace(/`([^`\n]+)`/g, (_, code) => {
    codeTokens.push(code)
    return `\uE000${codeTokens.length - 1}\uE000`
  })
  let rendered = escapeHtml(codePlaceholder)
  rendered = rendered.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, url) => {
    const safeUrl = sanitizeUrl(url)
    if (!safeUrl) return label
    return `<a href="${escapeAttribute(safeUrl)}" target="_blank" rel="noreferrer noopener">${label}</a>`
  })
  rendered = rendered.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  rendered = rendered.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>')
  rendered = rendered.replace(/\uE000(\d+)\uE000/g, (_, index) => {
    const code = codeTokens[Number(index)] || ''
    return `<code>${escapeHtml(code)}</code>`
  })
  return rendered
}

function isHeadingLine(line) {
  return /^(#{1,6})\s+/.test(line)
}

function isBlockquoteLine(line) {
  return /^>\s?/.test(line)
}

function isUnorderedListLine(line) {
  return /^\s*[-*+]\s+/.test(line)
}

function isOrderedListLine(line) {
  return /^\s*\d+\.\s+/.test(line)
}

function isFenceLine(line) {
  return /^```/.test(line)
}

function splitTableRow(line) {
  const trimmed = String(line ?? '').trim()
  const withoutLeadingPipe = trimmed.startsWith('|') ? trimmed.slice(1) : trimmed
  const withoutTrailingPipe = withoutLeadingPipe.endsWith('|') ? withoutLeadingPipe.slice(0, -1) : withoutLeadingPipe
  return withoutTrailingPipe.split('|').map((cell) => cell.trim())
}

function isTableSeparatorLine(line) {
  const cells = splitTableRow(line)
  return cells.length >= 2 && cells.every((cell) => /^:?-{3,}:?$/.test(cell.replace(/\s+/g, '')))
}

function isTableRowLine(line) {
  const trimmed = String(line ?? '').trim()
  if (!trimmed.includes('|')) return false
  return splitTableRow(trimmed).length >= 2
}

function renderTableCell(tag, text) {
  return `<${tag}>${renderInlineMarkdown(text)}</${tag}>`
}

function renderMarkdownTable(headerLine, bodyLines) {
  const headers = splitTableRow(headerLine)
  const body = bodyLines.map((line) => splitTableRow(line))
  const columnCount = headers.length
  const renderRow = (cells, tag) => `<tr>${Array.from({ length: columnCount }, (_, index) => renderTableCell(tag, cells[index] || '')).join('')}</tr>`
  return [
    '<table>',
    `<thead>${renderRow(headers, 'th')}</thead>`,
    body.length ? `<tbody>${body.map((row) => renderRow(row, 'td')).join('')}</tbody>` : '',
    '</table>'
  ].join('')
}

function renderMarkdownToHtml(value) {
  const lines = String(value ?? '').replace(/\r\n?/g, '\n').split('\n')
  const blocks = []
  let index = 0

  while (index < lines.length) {
    const line = lines[index]
    if (!line.trim()) {
      index += 1
      continue
    }

    const headingMatch = line.match(/^(#{1,6})\s+(.*)$/)
    if (headingMatch) {
      const level = headingMatch[1].length
      blocks.push(`<h${level}>${renderInlineMarkdown(headingMatch[2])}</h${level}>`)
      index += 1
      continue
    }

    if (isFenceLine(line)) {
      const language = line.replace(/^```+/, '').trim()
      const codeLines = []
      index += 1
      while (index < lines.length && !isFenceLine(lines[index])) {
        codeLines.push(lines[index])
        index += 1
      }
      if (index < lines.length) index += 1
      const langAttr = language ? ` data-language="${escapeAttribute(language)}"` : ''
      blocks.push(`<pre><code${langAttr}>${escapeHtml(codeLines.join('\n'))}</code></pre>`)
      continue
    }

    if (isBlockquoteLine(line)) {
      const quoteLines = []
      while (index < lines.length && isBlockquoteLine(lines[index])) {
        quoteLines.push(lines[index].replace(/^>\s?/, ''))
        index += 1
      }
      blocks.push(`<blockquote>${quoteLines.map((item) => renderInlineMarkdown(item)).join('<br>')}</blockquote>`)
      continue
    }

    if (isUnorderedListLine(line) || isOrderedListLine(line)) {
      const ordered = isOrderedListLine(line)
      const items = []
      const linePattern = ordered ? /^\s*\d+\.\s+/ : /^\s*[-*+]\s+/
      while (index < lines.length && linePattern.test(lines[index])) {
        items.push(lines[index].replace(linePattern, ''))
        index += 1
      }
      const tag = ordered ? 'ol' : 'ul'
      blocks.push(`<${tag}>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join('')}</${tag}>`)
      continue
    }

    if (isTableRowLine(line) && index + 1 < lines.length && isTableSeparatorLine(lines[index + 1])) {
      const headerLine = line
      const bodyLines = []
      index += 2
      while (index < lines.length && lines[index].trim() && isTableRowLine(lines[index])) {
        bodyLines.push(lines[index])
        index += 1
      }
      blocks.push(renderMarkdownTable(headerLine, bodyLines))
      continue
    }

    const paragraphLines = []
    while (
      index < lines.length &&
      lines[index].trim() &&
      !isFenceLine(lines[index]) &&
      !isHeadingLine(lines[index]) &&
      !isBlockquoteLine(lines[index]) &&
      !isUnorderedListLine(lines[index]) &&
      !isOrderedListLine(lines[index]) &&
      !(isTableRowLine(lines[index]) && index + 1 < lines.length && isTableSeparatorLine(lines[index + 1]))
    ) {
      paragraphLines.push(lines[index])
      index += 1
    }
    const paragraphText = paragraphLines.join('\n')
    blocks.push(`<p>${renderInlineMarkdown(paragraphText).replace(/\n/g, '<br>')}</p>`)
  }

  return blocks.join('')
}

function buildHtmlPreviewDocument(source) {
  const html = String(source ?? '').trim()
  if (!html) {
    return '<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>'
  }
  if (/^<!doctype\s+html/i.test(html) || /^<html[\s>]/i.test(html)) {
    return html
  }
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><base target="_blank"><style>html,body{margin:0;padding:0;background:#fff;color:#111827;font-family:Inter,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;line-height:1.6}body{padding:16px}</style></head><body>${html}</body></html>`
}

export {
  buildHtmlPreviewDocument,
  contentBlocksFromEntry,
  normalizeBlock,
  renderInlineMarkdown,
  renderMarkdownToHtml
}
