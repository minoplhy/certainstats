import { el, button, api, timeAgo, typeState } from './shared.js';

const CHEVRON = '<svg class="mgmt-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>';

function middleTrim(s, head, tail) {
  return s.length > head + tail + 1 ? s.slice(0, head) + '…' + s.slice(-tail) : s;
}

// The Collectors page list, laid out like the Management page: one expandable
// row per collector (name, status, types, ID), opening to its details and actions.
export function createList(ctx) {
  const { ui, state } = ctx;
  const openIds = new Set();
  let query = '';

  function confirm(opts) {
    return window.CertainStatsModal?.confirm ? window.CertainStatsModal.confirm(opts) : Promise.resolve(false);
  }

  async function act(fn, success) {
    ui.message.textContent = '';
    try {
      await fn();
      if (success) window.CertainStatsTelemetry?.showToast(success, true);
      await ctx.refresh();
    } catch (e) {
      ui.message.textContent = e.message;
    }
  }

  function copy(text, label) {
    const done = ok => window.CertainStatsTelemetry?.showToast(ok ? label + ' copied' : 'Copy failed', ok);
    if (navigator.clipboard?.writeText) navigator.clipboard.writeText(text).then(() => done(true), () => done(false));
    else done(false);
  }

  function credRow(label, ...content) {
    const row = el('div', 'cred-row');
    const dd = el('dd', 'cred-value');
    dd.append(...content);
    row.append(el('dt', '', label), dd);
    return row;
  }

  // nameRow shows the name with Rename; Rename swaps in the inline editor, as Management does.
  function nameRow(c) {
    const row = el('div', 'cred-row');
    const dd = el('dd');
    const read = el('div', 'cred-value');
    const edit = el('div', 'inline-edit');
    const label = el('label', 'sr-only', 'Collector name');
    const input = el('input', 'form-input form-input-sm');
    input.id = 'collector-name-' + c.collector_id;
    label.htmlFor = input.id;
    input.maxLength = 64;
    input.autocomplete = 'off';
    edit.hidden = true;
    read.append(el('span', '', c.name), button('Rename', 'btn btn-ghost btn-sm', () => {
      read.hidden = true;
      edit.hidden = false;
      input.value = c.name;
      input.focus();
      input.select();
    }));
    const cancel = () => { edit.hidden = true; read.hidden = false; };
    const save = () => {
      const name = input.value.trim();
      if (!name) { window.CertainStatsTelemetry?.showToast('Name cannot be empty', false); return; }
      if (name === c.name) { cancel(); return; }
      act(() => api('collectors/' + c.collector_id, {method: 'PATCH', body: JSON.stringify({name})}), 'Collector renamed to ' + name);
    };
    input.addEventListener('keydown', e => {
      if (e.key === 'Enter') { e.preventDefault(); save(); }
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); cancel(); }
    });
    edit.append(label, input, button('Save', 'btn btn-primary btn-sm', save), button('Cancel', 'btn btn-secondary btn-sm', cancel));
    dd.append(read, edit);
    row.append(el('dt', '', 'Name'), dd);
    return row;
  }

  function backfill(c, dd) {
    dd.querySelector('.collector-backfill')?.remove();
    const form = el('form', 'collector-backfill');
    const input = el('input', 'form-input form-input-sm');
    input.type = 'datetime-local';
    input.required = true;
    input.setAttribute('aria-label', 'Read again from');
    const d = new Date(Date.now() - 7 * 86400000);
    input.value = new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
    const go = el('button', 'btn btn-primary btn-sm', 'Read again');
    go.type = 'submit';
    form.append(el('span', 'muted', 'Read alerts again from'), input, go, button('Cancel', 'btn btn-secondary btn-sm', () => form.remove()));
    form.addEventListener('submit', e => {
      e.preventDefault();
      const since = new Date(input.value).getTime();
      act(() => api('collectors/' + c.collector_id + '/crowdsec/resync', {method: 'POST', body: JSON.stringify({since_ms: since})}), 'The collector is reading again');
    });
    dd.append(form);
    input.focus();
  }

  function typeRow(c, t) {
    const st = typeState(c, t);
    const row = el('div', 'cred-row');
    const dd = el('dd', 'cred-value');
    const dot = el('span', 'status-dot ' + (!t.enabled ? 'warn' : st.level === 'ok' ? 'online' : st.level === 'bad' ? 'offline' : 'warn'));
    dot.setAttribute('aria-hidden', 'true');
    dd.append(dot, el('span', 'collector-type-state', st.text));
    if (t.type === 'crowdsec' && t.enabled && c.connected) dd.append(button('Read again…', 'btn btn-ghost btn-sm', () => backfill(c, dd)));
    dd.append(button(t.enabled ? 'Disable' : 'Enable', 'btn btn-ghost btn-sm', () => act(
      () => api('collectors/' + c.collector_id + '/' + t.type, {method: t.enabled ? 'DELETE' : 'PUT'}),
      t.label + (t.enabled ? ' disabled' : ' enabled'))));
    row.append(el('dt', '', t.label), dd);
    return row;
  }

  function item(c) {
    const details = el('details', 'mgmt-item');
    details.dataset.collectorId = c.collector_id;
    details.dataset.search = [c.name, c.collector_id, c.agent_name || '', ...c.types.map(t => t.label)].join(' ').toLowerCase();
    details.open = openIds.has(c.collector_id);
    details.addEventListener('toggle', () => { if (details.open) openIds.add(c.collector_id); else openIds.delete(c.collector_id); });

    const summary = el('summary', 'mgmt-summary');
    summary.append(
      el('span', 'mgmt-name', c.name),
      el('span', 'badge ' + (c.connected ? 'badge-online' : 'badge-offline'), c.connected ? 'Connected' : (c.first_connected_at ? 'Offline' : 'Never connected')));
    for (const t of c.types.filter(t => t.enabled)) summary.append(el('span', 'badge badge-accent', t.label));
    const id = el('span', 'mgmt-id mono', middleTrim(c.collector_id, 8, 5));
    id.title = c.collector_id;
    summary.append(id);
    summary.insertAdjacentHTML('beforeend', CHEVRON);

    const panel = el('div', 'mgmt-panel');
    const list = el('dl', 'cred-list');
    list.append(
      nameRow(c),
      credRow('Collector ID', el('code', 'secret', c.collector_id), button('Copy', 'btn btn-ghost btn-sm', () => copy(c.collector_id, 'Collector ID'))),
      credRow('Node', el('span', c.agent_name ? '' : 'muted', c.agent_name || 'Not linked')),
      credRow('Version', el('span', c.version ? 'mono' : 'muted', c.version ? 'v' + c.version.replace(/^v/, '') : 'Unknown')),
      credRow('Last seen', el('span', '', c.connected ? 'Connected now' : timeAgo(c.last_seen_at))));
    for (const t of c.types) list.append(typeRow(c, t));

    const actions = el('div', 'mgmt-actions');
    actions.append(
      button('Install', 'btn btn-primary btn-sm', () => ctx.showInstall(c)),
      button('Reset token', 'btn btn-secondary btn-sm', async () => {
        if (!await confirm({title: 'Reset the token of ' + c.name + '?', message: 'The collector disconnects at once and stays offline until its TOKEN setting is replaced.', label: 'Reset token', danger: true})) return;
        act(async () => {
          await api('collectors/' + c.collector_id + '/token', {method: 'POST'});
          ctx.showInstall(c);
        });
      }),
      el('span', 'toolbar-spacer'),
      button('Remove collector', 'btn btn-danger btn-sm', async () => {
        if (!await confirm({title: 'Remove ' + c.name + '?', message: 'Its token stops working at once. Data it already sent is kept.', label: 'Remove collector', danger: true, match: c.name})) return;
        act(() => api('collectors/' + c.collector_id, {method: 'DELETE'}), 'Collector removed');
      }));
    panel.append(list, actions);
    details.append(summary, panel);
    return details;
  }

  function applySearch() {
    let shown = 0;
    for (const node of ui.list.querySelectorAll('.mgmt-item')) {
      node.hidden = !!query && !node.dataset.search.includes(query);
      if (!node.hidden) shown++;
    }
    const none = ui.list.querySelector('.mgmt-no-match');
    if (none) none.hidden = shown > 0;
  }

  function render() {
    ui.list.replaceChildren();
    ui.list.classList.toggle('mgmt-list', state.items.length > 0);
    if (!state.items.length) {
      const empty = el('div', 'empty-state');
      empty.append(el('h2', 'empty-state-title', 'No collectors yet'), el('p', '', 'Add a collector to get a token and install steps for the host.'));
      ui.list.append(empty);
      return;
    }
    for (const c of state.items) ui.list.append(item(c));
    const none = el('p', 'mgmt-no-match muted', 'No collectors match your search.');
    none.hidden = true;
    ui.list.append(none);
    applySearch();
  }

  // busy is true while a rename or a "Read again" form is open, so the periodic
  // refresh doesn't throw away what the user is typing.
  function busy() {
    return !!ui.list.querySelector('.inline-edit:not([hidden]), .collector-backfill');
  }

  function search(text) {
    query = text.trim().toLowerCase();
    applySearch();
  }

  return { render, busy, search };
}
