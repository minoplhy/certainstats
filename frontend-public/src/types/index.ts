// Matches Go AgentRow struct exactly
export interface Agent {
  agent_id:      string;
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
}

export interface DashboardResponse {
  title: string;
  dashboard_id: string;
  max_days: number;
  allowed_metrics: string[];
  agents: PublicAgent[];
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

export interface DiskOdometer {
  path:        string;
  read_bytes:  number;
  write_bytes: number;
}

export interface NetOdometer {
  total_rx_bytes: number;
  total_tx_bytes: number;
}

// Public dashboard agent (alias-resolved, no private fields)
export interface PublicAgent {
  public_id:      string;
  display_name:   string;
  is_online:      boolean;
  uptime?:        number;
  linux_version?: string;
  cpu_model?:     string;
  cpu_cores?:     number;
  ram_size?:      number;
  swap_size?:     number;
  disk_size?:     number;
  net?:           NetOdometer;
  disks?:         DiskOdometer[];
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

export interface ProvisionResponse {
  agent_id: string;
  token:    string;
  nickname: string;
  message:  string;
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
