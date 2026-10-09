const protobuf = require('protobufjs/minimal');
protobuf.util.Long = require('long');
protobuf.configure();
const types = require('./telemetry.cjs').certainstats.browser.v1;
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);

const protocol = 'certainstats.protobuf.v1';
function decode(data) {
  const bytes = data instanceof ArrayBuffer ? new Uint8Array(data) : data;
  if (!(bytes instanceof Uint8Array)) throw new Error('Expected a binary telemetry frame');
  const envelope = types.TelemetryEnvelope.decode(bytes);
  if (!own(envelope, 'pulse') || !envelope.pulse) throw new Error('Missing telemetry pulse');
  return envelope;
}
function numeric(wrapper) {
  if (!own(wrapper, 'value')) return null;
  const value = typeof wrapper.value === 'number' ? wrapper.value : wrapper.value.toNumber();
  return Number.isFinite(value) ? value : null;
}
function timestamp(value) {
  const seconds = typeof value.seconds === 'number' ? value.seconds : value.seconds.toNumber();
  if (!Number.isSafeInteger(seconds) || seconds < -62135596800 || seconds > 253402300799 || value.nanos < 0 || value.nanos > 999999999) {
    throw new Error('Invalid telemetry timestamp');
  }
  const fraction = value.nanos ? '.' + String(value.nanos).padStart(9, '0').replace(/0+$/, '') : '';
  return new Date(seconds * 1000).toISOString().replace(/\.\d{3}Z$/, fraction + 'Z');
}
const numericFields = [
  'cpu_usage_percent', 'cpu_iowait_percent', 'cpu_steal_percent',
  'ram_used_bytes', 'ram_swap_used_bytes', 'disk_used_bytes', 'disk_total_bytes',
  'rx_bytes', 'tx_bytes', 'rx_bps', 'tx_bps', 'disk_read_bps', 'disk_write_bps'
];
function snapshot(value) {
  const out = {};
  for (const key of ['available', 'is_online', 'uptime', 'agent_id']) {
    if (own(value, key)) out[key] = value[key];
  }
  if (value.timestamp) out.timestamp = timestamp(value.timestamp);
  for (const key of numericFields) if (value[key]) out[key] = numeric(value[key]);
  if (value.disks) out.disks = value.disks.items.map(disk => {
    const item = {path: disk.path};
    for (const key of ['used_bytes', 'total_bytes', 'read_bytes', 'write_bytes']) {
      if (disk[key]) item[key] = numeric(disk[key]);
    }
    return item;
  });
  if (value.load_avg) out.load_avg = value.load_avg.values.map(v => Number.isFinite(v) ? v : null);
  if (value.temperatures) out.temperatures = Object.fromEntries(
    Object.entries(value.temperatures.values).map(([k, v]) => [k, Number.isFinite(v) ? v : null])
  );
  if (value.missing) out.missing = {...value.missing.values};
  if (value.metadata) {
    const m = value.metadata;
    out.metadata = {
      Uptime: m.uptime, LinuxVersion: m.linux_version, CpuModel: m.cpu_model,
      CpuCores: m.cpu_cores, RamSize: m.ram_size.toNumber(),
      SwapSize: m.swap_size.toNumber(), DiskSize: m.disk_size.toNumber()
    };
  }
  return out;
}
function snapshots(envelope) {
  const out = Object.create(null);
  for (const [id, value] of Object.entries(envelope.pulse.agents)) out[id] = snapshot(value);
  return out;
}
function network(envelope) {
  const pulse = envelope.pulse.network;
  if (!pulse) return undefined;
  const out = Object.create(null);
  const num = value => typeof value === 'number' ? value : value.toNumber();
  for (const [id, m] of Object.entries(pulse.monitors)) {
    const item = {agent_id: m.agent_id, enabled: m.enabled, state: m.state};
    const sync = m.sync;
    item.sync = {desired_generation: num(sync.desired_generation), acknowledged_generation: num(sync.acknowledged_generation), error: sync.error};
    for (const key of ['last_attempt', 'last_ack']) if (sync[key]) item.sync[key] = timestamp(sync[key]);
    item.latest = null;
    if (m.latest) {
      const l = m.latest, latest = {};
      for (const key of ['response_avg_ms', 'response_min_ms', 'response_max_ms', 'response_avg_1h_ms', 'response_min_1h_ms', 'response_max_1h_ms']) latest[key] = l[key] ? numeric(l[key]) : null;
      for (const key of ['loss_pct', 'loss_1h_pct']) latest[key] = l[key];
      for (const key of ['last_probe_at', 'sample_count', 'attempt_count', 'success_count', 'received_at', 'certificate_received_at']) latest[key] = num(l[key]);
      if (l.certificate) latest.certificate = {expires: num(l.certificate.expires), issuer: l.certificate.issuer};
      item.latest = latest;
    }
    out[id] = item;
  }
  return out;
}
module.exports = {protocol, decode, snapshots, network};
