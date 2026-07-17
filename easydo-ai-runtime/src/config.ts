export interface RuntimeConfig {
  port: number
  instanceID: string
  runLeaseMs: number
  runStreamPollMs: number
  internalToken: string
  easydoServerURL: string
  workspace: {
    dockerHost: string
    image: string
  }
  database: {
    enabled: boolean
    uri?: string
    host?: string
    port: number
    user?: string
    password?: string
    name?: string
  }
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): RuntimeConfig {
  const dbHost = env.AI_RUNTIME_DB_HOST || env.DB_HOST
  const dbName = env.AI_RUNTIME_DB_NAME || env.DB_NAME
  const dbURI = env.AI_RUNTIME_DATABASE_URI || env.DATABASE_URL
  const dbEnabled = env.AI_RUNTIME_DB_ENABLED === 'true' || Boolean(dbURI) || Boolean(dbHost && dbName)
  return {
    port: Number(env.AI_RUNTIME_PORT || 8090),
    instanceID: env.AI_RUNTIME_INSTANCE_ID || env.HOSTNAME || 'runtime-local',
    runLeaseMs: Number(env.AI_RUNTIME_RUN_LEASE_MS || 15_000),
    runStreamPollMs: Number(env.AI_RUNTIME_RUN_STREAM_POLL_MS || 500),
    internalToken: env.AI_RUNTIME_INTERNAL_TOKEN || env.SERVER_INTERNAL_TOKEN || '',
    easydoServerURL: env.AI_RUNTIME_EASYDO_SERVER_URL || env.EASYDO_SERVER_URL || env.SERVER_INTERNAL_URL || '',
    workspace: {
      dockerHost: env.AI_RUNTIME_WORKSPACE_DOCKER_HOST || env.DOCKER_HOST || '',
      image: env.AI_RUNTIME_WORKSPACE_IMAGE || 'easydo-ai-workspace:latest'
    },
    database: {
      enabled: dbEnabled,
      uri: dbURI,
      host: dbHost,
      port: Number(env.AI_RUNTIME_DB_PORT || env.DB_PORT || 3306),
      user: env.AI_RUNTIME_DB_USER || env.DB_USERNAME,
      password: env.AI_RUNTIME_DB_PASSWORD || env.DB_PASSWORD,
      name: dbName
    }
  }
}
