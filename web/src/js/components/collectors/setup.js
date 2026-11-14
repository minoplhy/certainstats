import { el, copy } from './shared.js';

// Setup steps for a new collector, built from the types it collects.

const RELEASES = 'https://github.com/minoplhy/certainstats/releases/latest';
const IMAGE = 'ghcr.io/minoplhy/certainstats:latest';
const CROWDSEC_CREDS = '/etc/certainstats/crowdsec-lapi.yaml';

// Each type adds its environment, host steps and container mounts.
const TYPE_SETUP = {
  crowdsec: {
    env: [['COLLECT_CROWDSEC', 'true'], ['CROWDSEC_LAPI_CREDENTIALS', CROWDSEC_CREDS]],
    prepare: [{
      label: 'Register a machine login for the collector on the CrowdSec server (as root)',
      command: 'mkdir -p /etc/certainstats\ncscli machines add certainstats --auto -f ' + CROWDSEC_CREDS + '\nchmod 600 ' + CROWDSEC_CREDS
    }],
    volumes: [CROWDSEC_CREDS + ':' + CROWDSEC_CREDS + ':ro'],
    note: 'Install the collector on the machine that runs the CrowdSec Local API. If CrowdSec runs in Docker, the collector must share its network, the Local API must listen on that network, and url in ' + CROWDSEC_CREDS + ' must name the container, such as http://crowdsec:8080.'
  }
};

export function hubURL() {
  return window.location.origin + ((document.body?.getAttribute('data-panel-path') || '').replace(/\/+$/, ''));
}

function envLines(token, types) {
  const lines = [['HUB_URL', hubURL()], ['TOKEN', token]];
  for (const type of types) lines.push(...(TYPE_SETUP[type]?.env || []));
  return lines;
}

function commandBlock(label, text) {
  const item = el('div', 'instruction-item');
  item.append(el('div', 'instruction-label', label));
  const box = el('div', 'instruction-command-box');
  box.append(el('pre', 'instruction-command-code font-mono', text));
  const btn = el('button', 'instruction-copy-btn', 'Copy');
  btn.type = 'button';
  btn.addEventListener('click', () => copy(text, 'Command'));
  box.append(btn);
  item.append(box);
  return item;
}

function noteBlock(text) {
  return el('div', 'instruction-note', text);
}

export function systemdSteps(token, types) {
  const env = envLines(token, types).map(([k, v]) => k + '=' + v).join('\n');
  const steps = [];
  for (const type of types) for (const p of TYPE_SETUP[type]?.prepare || []) steps.push([p.label, p.command]);
  steps.push(['Install the collector (pick the file for your CPU from ' + RELEASES + ')',
    'curl -fsSL -o /tmp/certainstats-collector.tar.gz ' + RELEASES + '/download/certainstats-collector-linux-amd64.tar.gz\n' +
    'tar -xzf /tmp/certainstats-collector.tar.gz -C /tmp\ninstall -m 755 /tmp/certainstats-collector-linux-amd64 /usr/local/bin/certainstats-collector']);
  steps.push(['Write its settings', "install -m 600 /dev/null /etc/certainstats/collector.env\ncat > /etc/certainstats/collector.env <<'EOF'\n" + env + '\nEOF']);
  steps.push(['Check every prerequisite', 'set -a; . /etc/certainstats/collector.env; set +a\ncertainstats-collector --check']);
  steps.push(['Run it as a service', "cat > /etc/systemd/system/certainstats-collector.service <<'EOF'\n[Unit]\nDescription=CertainStats collector\nAfter=network-online.target crowdsec.service\nWants=network-online.target\n\n[Service]\nEnvironmentFile=/etc/certainstats/collector.env\nExecStart=/usr/local/bin/certainstats-collector\nRestart=always\nRestartSec=10\n\n[Install]\nWantedBy=multi-user.target\nEOF\nsystemctl daemon-reload\nsystemctl enable --now certainstats-collector"]);
  return steps;
}

export function composeFile(token, types) {
  const env = envLines(token, types).map(([k, v]) => '      ' + k + ': "' + v + '"').join('\n');
  const volumes = types.flatMap(type => TYPE_SETUP[type]?.volumes || []);
  let yaml = 'services:\n  certainstats-collector:\n    image: ' + IMAGE + '\n    command: ["./certainstats-collector"]\n    restart: unless-stopped\n    environment:\n' + env + '\n';
  if (volumes.length) yaml += '    volumes:\n' + volumes.map(v => '      - ' + v).join('\n') + '\n';
  if (types.includes('crowdsec')) yaml += '    # Join the network CrowdSec runs on, so the Local API is reachable.\n    # networks: [crowdsec]\n';
  return yaml;
}

// renderSetup fills container with tabs for systemd and Docker Compose.
export function renderSetup(container, token, types) {
  container.replaceChildren();
  const prepare = [];
  for (const type of types) for (const p of TYPE_SETUP[type]?.prepare || []) prepare.push(commandBlock(p.label, p.command));
  const tabs = [
    {name: 'systemd', build: () => systemdSteps(token, types).map(([label, cmd]) => commandBlock(label, cmd))},
    {name: 'Docker Compose', build: () => [...prepare, commandBlock('docker-compose.yml', composeFile(token, types)), commandBlock('Start it', 'docker compose up -d certainstats-collector\ndocker compose logs -f certainstats-collector')]}
  ];
  const header = el('div', 'instruction-tabs-header');
  header.setAttribute('role', 'tablist');
  const panels = el('div', 'instruction-tabs-panels');
  tabs.forEach((tab, i) => {
    const btn = el('button', 'instruction-tab-btn' + (i === 0 ? ' active' : ''), tab.name);
    btn.type = 'button';
    btn.setAttribute('role', 'tab');
    btn.setAttribute('aria-selected', String(i === 0));
    const panel = el('div', 'instruction-tab-panel');
    panel.hidden = i !== 0;
    panel.append(...tab.build());
    btn.addEventListener('click', () => {
      header.querySelectorAll('.instruction-tab-btn').forEach((b, j) => { b.classList.toggle('active', j === i); b.setAttribute('aria-selected', String(j === i)); });
      panels.querySelectorAll('.instruction-tab-panel').forEach((p, j) => { p.hidden = j !== i; });
    });
    header.append(btn);
    panels.append(panel);
  });
  container.append(header, panels);
  for (const type of types) if (TYPE_SETUP[type]?.note) container.append(noteBlock(TYPE_SETUP[type].note));
}
