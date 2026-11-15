// DOM and text helpers for the CrowdSec page.

export function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined && text !== null) node.textContent = String(text);
  return node;
}

export function button(label, className, onClick) {
  const node = el('button', className || 'btn btn-ghost btn-sm', label);
  node.type = 'button';
  if (onClick) node.addEventListener('click', onClick);
  return node;
}

const ICONS = {
  copy: 'M9 9h10v10H9zM5 15V5h10',
  filter: 'M4 5h16l-6 7v6l-4 2v-8z',
  exclude: 'M5 12h14'
};

// iconButton is a small labelled SVG button for table cells and ranked rows.
export function iconButton(icon, label, onClick) {
  const node = el('button', 'cs-icon-btn');
  node.type = 'button';
  node.setAttribute('aria-label', label);
  node.title = label;
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('d', ICONS[icon]);
  path.setAttribute('fill', 'none');
  path.setAttribute('stroke', 'currentColor');
  path.setAttribute('stroke-width', '2');
  path.setAttribute('stroke-linecap', 'round');
  path.setAttribute('stroke-linejoin', 'round');
  svg.append(path);
  node.append(svg);
  node.addEventListener('click', event => {
    // Cells sit inside <summary>; acting on a button must not toggle the row.
    event.preventDefault();
    event.stopPropagation();
    onClick(event);
  });
  return node;
}

export function formatCount(n) {
  if (n === null || n === undefined) return '–';
  return Number(n).toLocaleString('en-US');
}

export function formatTime(ms, withSeconds = false) {
  if (!ms) return '–';
  const d = new Date(ms);
  const pad = x => String(x).padStart(2, '0');
  const date = d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  return date + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) + (withSeconds ? ':' + pad(d.getSeconds()) : '');
}

// timeAgo describes a past moment: "just now", "5m ago", "3h ago", "2d ago" or "never".
export function timeAgo(ms, now = Date.now()) {
  if (!ms) return 'never';
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  return Math.floor(s / 86400) + 'd ago';
}

// relative describes a moment from now: "in 3h 12m" or "expired".
export function untilText(ms, now = Date.now()) {
  if (!ms) return 'unknown expiry';
  const diff = ms - now;
  if (diff <= 0) return 'expired';
  const m = Math.round(diff / 60000);
  if (m < 60) return 'in ' + m + 'm';
  const h = Math.floor(m / 60);
  if (h < 48) return 'in ' + h + 'h ' + (m % 60) + 'm';
  return 'in ' + Math.floor(h / 24) + 'd';
}

export function shortScenario(s) {
  if (!s) return 'unknown scenario';
  return s.includes('/') ? s.slice(s.lastIndexOf('/') + 1) : s;
}

export function rangeLabel(view) {
  if (view.all) return 'all time';
  if (view.start && view.end) return 'the selected range';
  const h = view.hours;
  if (h % 24 === 0 && h >= 48) return 'the last ' + (h / 24) + ' days';
  if (h === 24) return 'the last 24 hours';
  return h === 1 ? 'the last hour' : 'the last ' + h + ' hours';
}

// headline says the page's state in words first.
// needsAttention lists the machines and bouncers with an alert-level finding.
// A caution is shown in the list but never changes the headline.
export function needsAttention(components) {
  return (components?.items || []).filter(c => c.state === 'alert');
}

export function headline(summary, view, collectors, components) {
  const crowdsec = collectors.filter(c => c.types.some(t => t.type === 'crowdsec' && t.enabled));
  const down = crowdsec.filter(c => !c.connected || c.types.some(t => t.type === 'crowdsec' && t.status === 'error'));
  if (down.length) {
    return {bad: true, text: down.length === 1 ? down[0].name + ' is not reporting.' : down.length + ' collectors are not reporting.'};
  }
  const quiet = needsAttention(components);
  if (quiet.length === 1) {
    const c = quiet[0];
    const why = c.findings[0]?.detail || (c.firing?.[0] ? c.firing[0].rule + ' is firing' : 'unhealthy');
    return {bad: true, text: c.name + ' needs attention: ' + why + '.'};
  }
  if (quiet.length) {
    const machines = quiet.filter(c => c.kind === 'machine').length, bouncers = quiet.length - machines;
    const parts = [];
    if (machines) parts.push(machines === 1 ? '1 machine' : machines + ' machines');
    if (bouncers) parts.push(bouncers === 1 ? '1 bouncer' : bouncers + ' bouncers');
    return {bad: true, text: parts.join(' and ') + ' need attention.'};
  }
  const label = rangeLabel(view);
  if (!summary) return {bad: false, text: 'Loading alerts…'};
  if (!summary.alerts) return {bad: false, text: 'No alerts in ' + label + '.'};
  const n = summary.alerts === 1 ? '1 alert' : formatCount(summary.alerts) + ' alerts';
  const top = summary.topScenario ? ', most from ' + shortScenario(summary.topScenario) : '';
  return {bad: false, text: n + ' in ' + label + top + '.'};
}

export function groupLabel(field, item) {
  if (field === 'asn') return item.label ? 'AS' + item.value + ' ' + item.label : (item.value ? 'AS' + item.value : 'Unknown network');
  if (field === 'collector') return item.label || item.value;
  if (!item.value) return field === 'country' ? 'Unknown country' : 'None';
  return item.value;
}

export async function copyText(text, label) {
  try {
    await navigator.clipboard.writeText(text);
    window.CertainStatsTelemetry?.showToast((label || 'Value') + ' copied', true);
  } catch (e) {
    window.CertainStatsTelemetry?.showToast('Copy failed', false);
  }
}
