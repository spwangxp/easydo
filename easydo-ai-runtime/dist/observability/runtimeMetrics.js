const MAX_ROLLING_WINDOW_MS = 60 * 60_000;
const MAX_OBSERVATIONS_PER_METRIC = 10_000;
const ALLOWED_LABELS = new Set([
    'instance',
    'operation',
    'outcome',
    'status',
    'category',
    'code',
    'component',
    'phase',
    'source'
]);
export class RuntimeMetrics {
    options;
    counters = new Map();
    gauges = new Map();
    histograms = new Map();
    observations = new Map();
    buckets;
    constructor(options) {
        this.options = options;
        this.buckets = [...(options.histogramBuckets || [0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30])]
            .filter((value) => Number.isFinite(value) && value > 0)
            .sort((left, right) => left - right);
    }
    increment(name, labels = {}, value = 1) {
        const normalized = this.normalize(name, labels);
        const existing = this.counters.get(normalized.key);
        if (existing)
            existing.value += value;
        else
            this.counters.set(normalized.key, { name, labels: normalized.labels, value });
    }
    setGauge(name, value, labels = {}) {
        const normalized = this.normalize(name, labels);
        this.gauges.set(normalized.key, { name, labels: normalized.labels, value });
    }
    addGauge(name, delta, labels = {}) {
        const normalized = this.normalize(name, labels);
        const existing = this.gauges.get(normalized.key);
        this.gauges.set(normalized.key, {
            name,
            labels: normalized.labels,
            value: Math.max(0, (existing?.value || 0) + delta)
        });
    }
    observe(name, value, labels = {}, observedAt = Date.now()) {
        const normalized = this.normalize(name, labels);
        let histogram = this.histograms.get(normalized.key);
        if (!histogram) {
            histogram = {
                name,
                labels: normalized.labels,
                buckets: this.buckets,
                counts: this.buckets.map(() => 0),
                count: 0,
                sum: 0
            };
            this.histograms.set(normalized.key, histogram);
        }
        histogram.count += 1;
        histogram.sum += value;
        histogram.buckets.forEach((bucket, index) => {
            if (value <= bucket)
                histogram.counts[index] += 1;
        });
        const rolling = this.observations.get(name) || [];
        rolling.push({ value, observedAt });
        const minimumObservedAt = observedAt - MAX_ROLLING_WINDOW_MS;
        const retained = rolling
            .filter((observation) => observation.observedAt >= minimumObservedAt)
            .slice(-MAX_OBSERVATIONS_PER_METRIC);
        this.observations.set(name, retained);
    }
    latencySummary(name, windowMs, observedAt = Date.now()) {
        const minimumObservedAt = observedAt - Math.min(Math.max(windowMs, 0), MAX_ROLLING_WINDOW_MS);
        const values = (this.observations.get(name) || [])
            .filter((observation) => observation.observedAt >= minimumObservedAt && observation.observedAt <= observedAt)
            .map((observation) => observation.value * 1000)
            .sort((left, right) => left - right);
        if (values.length === 0) {
            return { count: 0, average_ms: 0, p50_ms: 0, p95_ms: 0, max_ms: 0 };
        }
        const percentile = (ratio) => values[Math.max(0, Math.ceil(values.length * ratio) - 1)] || 0;
        const rounded = (value) => Math.round(value * 1000) / 1000;
        return {
            count: values.length,
            average_ms: rounded(values.reduce((total, value) => total + value, 0) / values.length),
            p50_ms: rounded(percentile(0.5)),
            p95_ms: rounded(percentile(0.95)),
            max_ms: rounded(values[values.length - 1] || 0)
        };
    }
    render() {
        const lines = [];
        for (const metric of this.counters.values()) {
            lines.push(`${metric.name}${renderLabels(metric.labels)} ${metric.value}`);
        }
        for (const metric of this.gauges.values()) {
            lines.push(`${metric.name}${renderLabels(metric.labels)} ${metric.value}`);
        }
        for (const metric of this.histograms.values()) {
            metric.buckets.forEach((bucket, index) => {
                lines.push(`${metric.name}_bucket${renderLabels({ ...metric.labels, le: String(bucket) }, true)} ${metric.counts[index]}`);
            });
            lines.push(`${metric.name}_bucket${renderLabels({ ...metric.labels, le: '+Inf' }, true)} ${metric.count}`);
            lines.push(`${metric.name}_count${renderLabels(metric.labels)} ${metric.count}`);
            lines.push(`${metric.name}_sum${renderLabels(metric.labels)} ${metric.sum}`);
        }
        return `${lines.sort().join('\n')}\n`;
    }
    normalize(name, labels) {
        if (!/^[a-zA-Z_:][a-zA-Z0-9_:]*$/.test(name))
            throw new Error(`Invalid metric name ${name}`);
        const combined = { instance: this.options.instanceID, ...labels };
        for (const label of Object.keys(combined)) {
            if (!ALLOWED_LABELS.has(label))
                throw new Error(`Metric label ${label} is not allowed`);
        }
        const normalizedLabels = Object.fromEntries(Object.entries(combined).sort(([left], [right]) => left.localeCompare(right)));
        return { labels: normalizedLabels, key: `${name}:${JSON.stringify(normalizedLabels)}` };
    }
}
function renderLabels(labels, allowHistogramLe = false) {
    const entries = Object.entries(labels)
        .filter(([name]) => allowHistogramLe ? name === 'le' || ALLOWED_LABELS.has(name) : ALLOWED_LABELS.has(name))
        .sort(([left], [right]) => {
        if (left === 'le')
            return 1;
        if (right === 'le')
            return -1;
        return left.localeCompare(right);
    });
    if (entries.length === 0)
        return '';
    return `{${entries.map(([name, value]) => `${name}="${escapeLabel(value)}"`).join(',')}}`;
}
function escapeLabel(value) {
    return String(value).replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\n/g, '\\n');
}
