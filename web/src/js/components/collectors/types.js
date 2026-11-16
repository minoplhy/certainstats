import { el, button, panelPath, typeState } from './shared.js';

// summarize counts, for one type, the collectors that have it enabled by where they stand.
export function summarize(items, type) {
  const out = {total: 0, reporting: 0, problems: 0, waiting: 0};
  for (const c of items) {
    const ct = (c.types || []).find(t => t.type === type && t.enabled);
    if (!ct) continue;
    out.total++;
    const level = typeState(c, ct).level;
    if (level === 'ok') out.reporting++;
    else if (level === 'bad') out.problems++;
    else out.waiting++;
  }
  return out;
}

export function summaryText(s) {
  if (!s.total) return 'No collector yet';
  const parts = [s.reporting + ' reporting'];
  if (s.problems) parts.push(s.problems + ' offline or failing');
  if (s.waiting) parts.push(s.waiting + ' waiting');
  return parts.join(' · ');
}

// The Types section of the Collectors page: one row per registered type, leading
// to the type's own page. Rows are built once and updated in place on refresh.
export function createTypes(ctx) {
  const rows = new Map(); // type -> {li, dot, text}

  function render() {
    const box = ctx.ui.typeList;
    if (!box) return;
    const nodes = [];
    for (const t of ctx.state.types) {
      let r = rows.get(t.type);
      if (!r) {
        r = {li: el('li', 'collector-type-row'), dot: el('span', 'status-dot'), text: el('span', 'collector-type-summary muted')};
        r.dot.setAttribute('aria-hidden', 'true');
        const link = el('a', 'collector-type-name', t.label);
        link.href = panelPath() + '/collectors/' + encodeURIComponent(t.type);
        const open = el('a', 'btn btn-secondary btn-sm', 'Open');
        open.href = link.href;
        open.setAttribute('aria-label', 'Open ' + t.label);
        r.li.append(r.dot, link, r.text, el('span', 'toolbar-spacer'), open,
          button('Add', 'btn btn-ghost btn-sm', () => ctx.openAdd(t.type)));
        rows.set(t.type, r);
      }
      const s = summarize(ctx.state.items, t.type);
      const text = summaryText(s);
      if (r.text.textContent !== text) r.text.textContent = text;
      const dot = 'status-dot ' + (s.problems ? 'offline' : s.reporting ? 'online' : 'warn');
      if (r.dot.className !== dot) r.dot.className = dot;
      nodes.push(r.li);
    }
    nodes.forEach((node, i) => {
      if (box.children[i] !== node) box.insertBefore(node, box.children[i] || null);
    });
    while (box.children.length > nodes.length) box.lastChild.remove();
  }

  return {render};
}
