export interface DiskOdometer {
  path:         string;
  read_bytes?:  number;
  write_bytes?: number;
}

export interface NetOdometer {
  total_rx_bytes?: number;
  total_tx_bytes?: number;
}

export interface Agent {
  agent_id:      string;
  agent_type:    string;
  nickname:      string;
  last_seen:     string | null;
  is_online:     boolean;
  uptime:        number;
  linux_version: string;
  cpu_model:     string;
  cpu_cores:     number;
  ram_size:      number;
  swap_size:     number;
  disk_size:     number;
  net:           NetOdometer;
  disks:         DiskOdometer[];
}

export interface MetricSeries {
  labels: Record<string, string>;
  data:   [number, number][]; // [timestamp_ms, value]
}

// Matches Go MetricResponse
export interface MetricResponse {
  metric: string;
  series: MetricSeries[];
}

// Public dashboard agent (alias-resolved, no private fields)
export interface PublicAgent {
  public_id:     string;
  name:          string;
  is_online:     boolean;
  uptime:        number;
  linux_version: string;
  cpu_model:     string;
  cpu_cores:     number;
  ram_size:      number;
  swap_size:     number;
  disk_size:     number;
}

export interface Dashboard {
  dashboard_id:   string;
  slug:           string;
  title:          string;
  max_days:       number;
  allowed_fields: string[];
  agent_count:    number;
}

export interface CreateDashboardReq {
  slug:           string;
  title:          string;
  agents:         { agent_id: string; alias: string }[];
  allowed_fields: string[];
  max_days:       number;
}

export interface AgentManagement {
  agent_id:          string;
  agent_type:        string;
  nickname:          string;
  token:             string;
  beszel_public_key: string;
}

export interface ProvisionMessage {
  name:         string;
  message_type: 'copy' | 'command' | 'note' | 'big_copy' | 'warning';

  content:      string;
  description?: string;
}


export interface ProvisionResponse {
  agent_id:   string;
  nickname:   string;
  agent_type: string;
  messages:   ProvisionMessage[];
  message:    string;
}

export type MetricKey =
  | "agent_cpu_usage"
  | "agent_cpu_iowait"
  | "agent_cpu_steal"
  | "agent_ram_used"
  | "agent_swap_used"
  | "agent_disk_used"
  | "agent_disk_read_bytes"
  | "agent_disk_write_bytes"
  | "agent_rx_bytes"
  | "agent_tx_bytes";

export const METRIC_LABELS: Record<MetricKey, string> = {
  agent_cpu_usage:  "CPU Usage",
  agent_cpu_iowait: "CPU IO Wait",
  agent_cpu_steal:  "CPU Steal",
  agent_ram_used:   "RAM Usage",
  agent_swap_used:  "Swap Usage",
  agent_disk_used:  "Disk Usage",
  agent_disk_read_bytes:  "Disk Read",
  agent_disk_write_bytes: "Disk Write",
  agent_rx_bytes:   "Network RX",
  agent_tx_bytes:   "Network TX",
};

export type TriggerType =
  | "agent_down"
  | "cpu_usage"
  | "cpu_iowait"
  | "cpu_steal"
  | "ram_usage"
  | "swap_usage"
  | "disk_usage"
  | "net_rx"
  | "net_tx"
  | "disk_read"
  | "disk_write";
export type Operator = ">" | "<" | "==";
export type DestinationType = "webhook" | "discord";

export interface AlertTrigger {
  type:      TriggerType;
  operator:  Operator;
  threshold: number;
  duration:  string;
}

export interface AlertAction {
  type:        DestinationType;
  destination: string;
  payload:     string;
}

export interface AgentState {
  agent_id: string;
  status:   string;
}

export interface Alert {
  alert_id: string;
  user_id:  string;
  enabled:  boolean;
  trigger:  AlertTrigger;
  action:   AlertAction;
  agents:   AgentState[];
}

export interface AlertHistory {
  history_id: string;
  alert_id: string;
  agent_id: string;
  agent_nickname: string;
  triggered_at: string;
  resolved_at?: string;
  trigger_value: number;
  notified_status: string;
  trigger: AlertTrigger;
}
export interface AgentSnapshot {
  Timestamp: string;
  CPUUsagePercent: number;
  CPUIOWaitPercent: number;
  CPUStealPercent: number;
  RAMUsagePercent: number;
  RAMUsedBytes: number;
  RAMSwapUsagePercent: number;
  RAMSwapUsedBytes: number;
  DiskUsagePercent: number;
  DiskUsedBytes: number;
  DiskReadBps: number;
  DiskWriteBps: number;
  RXBps: number;
  TXBps: number;
  Disks?: {
    path: string;
    used_bytes: number;
    total_bytes: number;
    read_bytes: number;
    write_bytes: number;
  }[];
}
