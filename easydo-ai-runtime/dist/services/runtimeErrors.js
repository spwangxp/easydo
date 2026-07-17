import { RuntimeSourceError } from './runtimeErrorClassifier.js';
export class RuntimeDomainError extends RuntimeSourceError {
    status;
    details;
    constructor(code, message, status = 400, details = {}) {
        super({
            source: domainErrorSource(code, status),
            code,
            message,
            http_status: status,
            retryable: false
        });
        this.name = 'RuntimeDomainError';
        this.status = status;
        this.details = details;
    }
}
function domainErrorSource(code, status) {
    if (status === 403 || /permission|access_denied|approval_denied/.test(code))
        return 'permission';
    if (/cancel/.test(code))
        return 'user';
    if (status >= 400 && status < 500)
        return 'validation';
    return 'runtime';
}
