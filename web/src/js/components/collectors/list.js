import { el, button, api, timeAgo, typeState } from './shared.js';

// The collector rows of the Collectors page: status, rename in place, per-type actions.
export function createList(ctx) {
  const { ui, state } = ctx;

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

  function renameControl(c) {
    const wrap = el('div', 'collector-name');
    const title = el('strong', '', c.name);
    const edit = button('Rename', 'btn btn-ghost btn-sm');
    wrap.append(title, edit);
    edit.addEventListener('click', () => {
      const input = el('input', 'form-input form-input-sm');
      input.value = c.name;
      input.maxLength = 64;
      input.setAttribute('aria-label', 'Collector name');
      const save = button('Save', 'btn btn-primary btn-sm');
      const cancel = button('Cancel', 'btn btn-secondary btn-sm', () => render());
      const submit = () => act(() => api('collectors/' + c.collector_id, {method: 'PATCH', body: JSON.stringify({name: input.value})}), 'Collector renamed');
      save.addEventListener('click', submit);
      input.addEventListener('keydown', e => {
        if (e.key === 'Enter') { e.preventDefault(); submit(); }
        if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); render(); }
      });
      wrap.replaceChildren(input, save, cancel);
      input.focus();
    });
    return wrap;
  }

  function backfillControl(c, row) {
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
    row.append(form);
    input.focus();
  }

  function typeLine(c, t, row) {
    const st = typeState(c, t);
    const line = el('div', 'collector-type');
    const dot = el('span', 'status-dot ' + (st.level === 'ok' ? 'online' : st.level === 'bad' ? 'offline' : 'warn'));
    dot.setAttribute('aria-hidden', 'true');
    line.append(dot, el('span', 'collector-type-label', t.label), el('span', 'collector-type-state', st.text));
    const actions = el('span', 'collector-type-actions');
    if (t.type === 'crowdsec' && t.enabled && c.connected) actions.append(button('Read again…', 'btn btn-ghost btn-sm', () => backfillControl(c, row)));
    actions.append(button(t.enabled ? 'Disable' : 'Enable', 'btn btn-ghost btn-sm', () => act(
      () => api('collectors/' + c.collector_id + '/' + t.type, {method: t.enabled ? 'DELETE' : 'PUT'}),
      t.label + (t.enabled ? ' disabled' : ' enabled'))));
    line.append(actions);
    return line;
  }

  function row(c) {
    const item = el('article', 'collector-row' + (c.connected ? '' : ' is-offline'));
    const head = el('div', 'collector-row-head');
    const dot = el('span', 'status-dot ' + (c.connected ? 'online' : 'offline'));
    dot.setAttribute('aria-hidden', 'true');
    const facts = el('div', 'collector-facts mono');
    facts.append(
      el('span', '', c.connected ? 'Connected' : (c.first_connected_at ? 'Offline' : 'Never connected')),
      el('span', '', c.agent_name ? 'Node ' + c.agent_name : 'No node'),
      el('span', '', c.version ? 'v' + c.version.replace(/^v/, '') : 'Version unknown'),
      el('span', '', 'Seen ' + timeAgo(c.last_seen_at)));
    head.append(dot, renameControl(c), facts);
    const actions = el('div', 'collector-row-actions');
    actions.append(
      button('Install', 'btn btn-secondary btn-sm', () => ctx.showInstall(c)),
      button('Reset token', 'btn btn-ghost btn-sm', async () => {
        if (!await confirm({title: 'Reset the token of ' + c.name + '?', message: 'The collector disconnects at once and stays offline until its TOKEN setting is replaced.', label: 'Reset token', danger: true})) return;
        act(async () => {
          await api('collectors/' + c.collector_id + '/token', {method: 'POST'});
          ctx.showInstall(c);
        });
      }),
      button('Remove', 'btn btn-ghost btn-sm btn-danger', async () => {
        if (!await confirm({title: 'Remove ' + c.name + '?', message: 'Its token stops working at once. Data it already sent is kept.', label: 'Remove collector', danger: true, match: c.name})) return;
        act(() => api('collectors/' + c.collector_id, {method: 'DELETE'}), 'Collector removed');
      }));
    item.append(head);
    for (const t of c.types) item.append(typeLine(c, t, item));
    item.append(actions);
    return item;
  }

  function render() {
    ui.list.replaceChildren();
    if (!state.items.length) {
      const empty = el('div', 'empty-state');
      empty.append(el('h3', 'empty-state-title', 'No collectors yet'), el('p', '', 'Add one to start sending data from a host.'));
      ui.list.append(empty);
      return;
    }
    for (const c of state.items) ui.list.append(row(c));
  }

  return { render };
}
