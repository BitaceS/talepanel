export interface User {
  id: string
  email: string
  username: string
  role: 'owner' | 'admin' | 'moderator' | 'user'
  totp_enabled: boolean
  created_at: string
  last_login_at: string | null
  is_active: boolean
  display_name: string | null
  avatar_url: string | null
  language: string
  timezone: string
}

export interface Server {
  id: string
  name: string
  node_id: string
  owner_id: string
  status: 'installing' | 'stopped' | 'starting' | 'running' | 'stopping' | 'crashed'
  hytale_version: string
  cpu_limit: number | null
  ram_limit_mb: number | null
  disk_limit_mb: number | null
  port: number
  auto_restart: boolean
  active_world: string | null
  created_at: string
  updated_at: string
}

export interface Node {
  id: string
  name: string
  fqdn: string
  port: number
  location: string | null
  total_cpu: number
  total_ram_mb: number
  total_disk_mb: number
  max_servers: number
  status: 'online' | 'offline' | 'draining'
  last_heartbeat: string | null
}

export interface ServerMetrics {
  cpu_percent: number
  ram_mb: number
  net_rx_bytes: number
  net_tx_bytes: number
  player_count: number
  tps: number
}

export interface ApiError {
  error: string
  code?: string
}

export interface LoginResponse {
  access_token?: string
  user?: User
  requires_totp?: boolean
}

export interface InstalledMod {
  id: string
  server_id: string
  filename: string
  display_name: string
  version: string
  download_url: string
  cf_mod_id: number | null
  cf_file_id: number | null
  installed_at: string
  source: string
  plugin_name: string | null
  author: string | null
  description: string | null
  detected_commands: string[]
  config_files: string[]
  file_hash: string | null
  last_scanned_at: string | null
  is_present: boolean
}

export interface CFModFile {
  id: number
  displayName: string
  fileName: string
  downloadUrl: string
  gameVersions: string[]
  fileDate: string
}

export interface CFMod {
  id: number
  name: string
  summary: string
  downloadCount: number
  logo: { thumbnailUrl: string; url: string } | null
  links: { websiteUrl: string }
  latestFiles: CFModFile[]
}

export interface CFSearchResult {
  data: CFMod[]
  pagination: {
    index: number
    pageSize: number
    resultCount: number
    totalCount: number
  }
}

export interface PaginatedResponse<T> {
  data: T[]
  total: number
  page: number
  per_page: number
}

export interface World {
  id: string
  server_id: string
  name: string
  seed: number | null
  generator: string | null
  is_active: boolean
  size_bytes: number | null
  thumbnail: string | null
  created_at: string
  updated_at: string
}

export interface Player {
  id: string
  server_id: string
  hytale_uuid: string
  username: string
  first_seen: string
  last_seen: string | null
  playtime_s: number
  is_whitelisted: boolean
  is_banned: boolean
  ban_reason: string | null
  banned_at: string | null
  banned_by: string | null
  is_op: boolean
  is_muted: boolean
}

export interface PlayerSession {
  joined_at: string
  left_at: string | null
  duration_s: number | null
}

export interface Backup {
  id: string
  server_id: string | null
  world_name: string | null
  type: 'full' | 'world' | 'files'
  storage: 'local' | 's3' | 'sftp'
  storage_path: string
  size_bytes: number | null
  checksum: string | null
  status: 'pending' | 'running' | 'complete' | 'failed'
  triggered_by: 'manual' | 'schedule'
  created_at: string
  completed_at: string | null
  expires_at: string | null
  error: string | null
}

export interface BackupSchedule {
  id: string
  server_id: string
  cron_expr: string
  type: 'full' | 'world' | 'files'
  storage: 'local' | 's3' | 'sftp'
  retention_count: number | null
  retention_days: number | null
  enabled: boolean
  last_run: string | null
  next_run: string | null
  created_at: string
}

export interface AlertRule {
  id: string
  server_id: string | null
  user_id: string
  type: string
  threshold: number | null
  channels: string[]
  enabled: boolean
  created_at: string
}

export interface AlertEvent {
  id: string
  rule_id: string | null
  server_id: string | null
  node_id: string | null
  type: string
  severity: 'info' | 'warning' | 'critical'
  title: string
  body: string | null
  metadata: Record<string, unknown>
  resolved: boolean
  resolved_at: string | null
  created_at: string
}

export interface ActivityLog {
  id: string
  user_id: string | null
  server_id: string | null
  action: string
  target_type: string
  target_id: string | null
  ip_address: string
  payload: Record<string, unknown>
  created_at: string
}

export interface Permission {
  key: string
  description: string
  category: string
}

export interface UserPermission {
  id: string
  user_id: string
  perm_key: string
  granted: boolean
}

export interface NotificationPref {
  id: string
  user_id: string
  alert_type: string
  email: boolean
  discord: boolean
  telegram: boolean
}

export interface ServerInvitation {
  id: string
  server_id: string
  inviter_id: string
  invitee_email: string
  token?: string
  role: string
  status: 'pending' | 'accepted' | 'declined' | 'revoked' | 'expired'
  created_at: string
  expires_at: string
}

export interface ServerDatabase {
  id: string
  server_id: string
  db_name: string
  db_user: string
  db_password: string
  host: string
  port: number
  created_at: string
}

export interface Session {
  id: string
  user_id: string
  ip_address: string
  user_agent: string
  created_at: string
  expires_at: string
  revoked: boolean
}

export interface GameCommand {
  id: string
  server_id: string | null
  category: string
  name: string
  description: string
  command_template: string
  icon: string
  params: CommandParam[]
  sort_order: number
  is_default: boolean
  min_role: string
  created_at: string
  source: string
  source_plugin: string | null
}

export interface CommandParam {
  name: string
  type: string
  required: boolean
  placeholder: string
  default?: string
}
