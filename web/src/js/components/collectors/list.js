import { el, button, api, panelPath, timeAgo, typeState } from './shared.js';

const CHEVRON = '<svg class="mgmt-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 6 6 6-6 6"/></svg>';

function middleTrim(s, head, tail) {
  return s.length > head + tail + 1 ? s.slice(0, head) + '…' + s.slice(-tail) : s;
}

// The Collectors page list, laid out like the Management page: one expandable
// row per collector (name, status, types, ID), opening to its details and actions.
export function createList(ctx) {
  const { ui, state } = ctx;
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

  // The list refreshes every 30 seconds, so each collector's row is built once
  // and then updated in place: open rows, focus, and open forms survive.
  // Handlers read rec.cur, the collector as of the last refresh.
  const recs = new Map();  // collector id -> record

  function setText(node, text) {
    if (node.textContent !== text) node.textContent = text;
  }

  function setClass(node, className) {
    if (node.className !== className) node.className = className;
  }

  // place keeps container's children in order, moving a node only when out of place.
  function place(container, nodes) {
    nodes.forEach((node, i) => {
      if (container.children[i] !== node) container.insertBefore(node, container.children[i] || null);
    });
  }

  function credRow(label, ...content) {
    const row = el('div', 'cred-row');
    const dd = el('dd', 'cred-value');
    dd.append(...content);
    row.append(el('dt', '', label), dd);
    return row;
  }

  // nameRow shows the name with Rename; Rename swaps in the inline editor, as Management does.
  function nameRow(rec) {
    const row = el('div', 'cred-row');
    const dd = el('dd');
    const read = el('div', 'cred-value');
    const edit = el('div', 'inline-edit');
    const label = el('label', 'sr-only', 'Collector name');
    const input = el('input', 'form-input form-input-sm');
    input.id = 'collector-name-' + rec.id;
    label.htmlFor = input.id;
    input.maxLength = 64;
    input.autocomplete = 'off';
    edit.hidden = true;
    rec.nameText = el('span');
    read.append(rec.nameText, button('Rename', 'btn btn-ghost btn-sm', () => {
      read.hidden = true;
      edit.hidden = false;
      input.value = rec.cur.name;
      input.focus();
      input.select();
    }));
    const cancel = () => { edit.hidden = true; read.hidden = false; };
    const save = () => {
      const name = input.value.trim();
      if (!name) { window.CertainStatsTelemetry?.showToast('Name cannot be empty', false); return; }
      if (name === rec.cur.name) { cancel(); return; }
      act(() => api('collectors/' + rec.id, {method: 'PATCH', body: JSON.stringify({name})}), 'Collector renamed to ' + name);
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

  function backfill(rec, dd) {
    dd.querySelector('.collector-backfill')?.remove();
    const form = el('form', 'collector-backfill');
    const input = el('input', 'form-input form-input-sm mono');
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
      act(() => api('collectors/' + rec.id + '/crowdsec/resync', {method: 'POST', body: JSON.stringify({since_ms: since})}), 'The collector is reading again');
    });
    dd.append(form);
    input.focus();
  }

  function typeRow(rec, type) {
    const t = {type, row: el('div', 'cred-row'), dt: el('dt'), dd: el('dd', 'cred-value'), dot: el('span'), text: el('span', 'collector-type-state'), actions: el('span', 'collector-type-actions'), key: ''};
    t.dot.setAttribute('aria-hidden', 'true');
    // The type's label leads to its page.
    t.link = el('a', 'collector-type-link');
    t.link.href = panelPath() + '/collectors/' + encodeURIComponent(type);
    t.dt.append(t.link);
    // Forms open below the buttons, inside dd, so rebuilding the buttons leaves them alone.
    t.dd.append(t.dot, t.text, t.actions);
    t.row.append(t.dt, t.dd);
    return t;
  }

  function updateTypeRow(rec, t, ct) {
    const c = rec.cur;
    const st = typeState(c, ct);
    setText(t.link, ct.label);
    setClass(t.dot, 'status-dot ' + (!ct.enabled ? 'warn' : st.level === 'ok' ? 'online' : st.level === 'bad' ? 'offline' : 'warn'));
    setText(t.text, st.text);
    // The buttons depend only on these; rebuild them only when one changes.
    const key = [ct.enabled, c.connected, ct.label].join('|');
    if (t.key === key) return;
    t.key = key;
    const buttons = [];
    if (ct.type === 'crowdsec' && ct.enabled && c.connected) buttons.push(button('Read again…', 'btn btn-ghost btn-sm', () => backfill(rec, t.dd)));
    buttons.push(button(ct.enabled ? 'Disable' : 'Enable', 'btn btn-ghost btn-sm', () => act(
      () => api('collectors/' + rec.id + '/' + ct.type, {method: ct.enabled ? 'DELETE' : 'PUT'}),
      ct.label + (ct.enabled ? ' disabled' : ' enabled'))));
    t.actions.replaceChildren(...buttons);
  }

  function createItem(c) {
    const rec = {id: c.collector_id, cur: c, types: new Map(), badgeKey: null};
    rec.details = el('details', 'mgmt-item');
    rec.details.dataset.collectorId = c.collector_id;

    const summary = el('summary', 'mgmt-summary');
    rec.summaryName = el('span', 'mgmt-name');
    rec.status = el('span');
    rec.badges = el('span', 'collector-type-badges');
    const id = el('span', 'mgmt-id mono', middleTrim(c.collector_id, 8, 5));
    id.title = c.collector_id;
    summary.append(rec.summaryName, rec.status, rec.badges, id);
    summary.insertAdjacentHTML('beforeend', CHEVRON);

    const panel = el('div', 'mgmt-panel');
    rec.list = el('dl', 'cred-list');
    rec.node = el('span');
    rec.version = el('span');
    rec.lastSeen = el('span', 'mono');
    rec.list.append(
      nameRow(rec),
      credRow('Collector ID', el('code', 'secret', c.collector_id), button('Copy', 'btn btn-ghost btn-sm', () => copy(c.collector_id, 'Collector ID'))),
      credRow('Node', rec.node),
      credRow('Version', rec.version),
      credRow('Last seen', rec.lastSeen));
    rec.fixedRows = rec.list.children.length;

    const actions = el('div', 'mgmt-actions');
    actions.append(
      button('Install', 'btn btn-primary btn-sm', () => ctx.showInstall(rec.cur)),
      button('Reset token', 'btn btn-secondary btn-sm', async () => {
        if (!await confirm({title: 'Reset the token of ' + rec.cur.name + '?', message: 'The collector disconnects at once and stays offline until its TOKEN setting is replaced.', label: 'Reset token', danger: true})) return;
        act(async () => {
          await api('collectors/' + rec.id + '/token', {method: 'POST'});
          ctx.showInstall(rec.cur);
        });
      }),
      el('span', 'toolbar-spacer'),
      button('Remove collector', 'btn btn-danger btn-sm', async () => {
        if (!await confirm({title: 'Remove ' + rec.cur.name + '?', message: 'Its token stops working at once. Data it already sent is kept.', label: 'Remove collector', danger: true, match: rec.cur.name})) return;
        act(() => api('collectors/' + rec.id, {method: 'DELETE'}), 'Collector removed');
      }));
    panel.append(rec.list, actions);
    rec.details.append(summary, panel);
    return rec;
  }

  function updateItem(rec, c) {
    rec.cur = c;
    rec.details.dataset.search = [c.name, c.collector_id, c.agent_name || '', ...c.types.map(t => t.label)].join(' ').toLowerCase();
    setText(rec.summaryName, c.name);
    setClass(rec.status, 'badge ' + (c.connected ? 'badge-online' : 'badge-offline'));
    setText(rec.status, c.connected ? 'Connected' : (c.first_connected_at ? 'Offline' : 'Never connected'));
    const labels = c.types.filter(t => t.enabled).map(t => t.label);
    if (rec.badgeKey !== labels.join('|')) {
      rec.badgeKey = labels.join('|');
      rec.badges.replaceChildren(...labels.map(l => el('span', 'badge badge-accent', l)));
    }
    setText(rec.nameText, c.name);
    setClass(rec.node, c.agent_name ? '' : 'muted');
    setText(rec.node, c.agent_name || 'Not linked');
    setClass(rec.version, c.version ? 'mono' : 'muted');
    setText(rec.version, c.version ? 'v' + c.version.replace(/^v/, '') : 'Unknown');
    setText(rec.lastSeen, c.connected ? 'Connected now' : timeAgo(c.last_seen_at));

    const rows = [];
    for (const ct of c.types) {
      let t = rec.types.get(ct.type);
      if (!t) {
        t = typeRow(rec, ct.type);
        rec.types.set(ct.type, t);
      }
      updateTypeRow(rec, t, ct);
      rows.push(t.row);
    }
    for (const [type, t] of rec.types) {
      if (!c.types.some(ct => ct.type === type)) {
        t.row.remove();
        rec.types.delete(type);
      }
    }
    // Type rows follow the fixed rows (name, ID, node, version, last seen).
    rows.forEach((row, i) => {
      const at = rec.list.children[rec.fixedRows + i];
      if (at !== row) rec.list.insertBefore(row, at || null);
    });
  }

  function applySearch() {
    let shown = 0;
    for (const node of ui.list.querySelectorAll('.mgmt-item')) {
      node.hidden = !!query && !node.dataset.search.includes(query);
      if (!node.hidden) shown++;
    }
    if (none) none.hidden = shown > 0 || !state.items.length;
  }

  let empty = null, none = null;

  function render() {
    const has = state.items.length > 0;
    ui.list.classList.toggle('mgmt-list', has);
    if (!empty) {
      empty = el('div', 'empty-state');
      empty.append(el('h2', 'empty-state-title', 'No collectors yet'), el('p', '', 'Add a collector to get a token and install steps for the host.'));
      none = el('p', 'mgmt-no-match muted', 'No collectors match your search.');
      none.hidden = true;
    }
    empty.hidden = has;
    const nodes = state.items.map(c => {
      let rec = recs.get(c.collector_id);
      if (!rec) {
        rec = createItem(c);
        recs.set(c.collector_id, rec);
      }
      updateItem(rec, c);
      return rec.details;
    });
    for (const [id, rec] of recs) {
      if (!state.items.some(c => c.collector_id === id)) {
        rec.details.remove();
        recs.delete(id);
      }
    }
    place(ui.list, [...nodes, none, empty]);
    applySearch();
  }

  // busy is true while a rename or a "Read again" form is open. Rows are updated in place, so nothing typed would be lost, but the
  // periodic refresh still waits so a row does not change under the user.
  function busy() {
    return !!ui.list.querySelector('.inline-edit:not([hidden]), .collector-backfill');
  }

  function search(text) {
    query = text.trim().toLowerCase();
    applySearch();
  }

  return { render, busy, search };
}
