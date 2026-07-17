import { RuntimeSourceError, type RuntimeErrorSource } from './runtimeErrorClassifier.js'

export class RuntimeDomainError extends RuntimeSourceError {
  readonly status: number
  readonly details: Record<string, unknown>

  constructor(
    code: string,
    message: string,
    status = 400,
    details: Record<string, unknown> = {}
  ) {
    super({
      source: domainErrorSource(code, status),
      code,
      message,
      http_status: status,
      retryable: false
    })
    this.name = 'RuntimeDomainError'
    this.status = status
    this.details = details
  }
}

function domainErrorSource(code: string, status: number): RuntimeErrorSource {
  if (status === 403 || /permission|access_denied|approval_denied/.test(code)) return 'permission'
  if (/cancel/.test(code)) return 'user'
  if (status >= 400 && status < 500) return 'validation'
  return 'runtime'
}
