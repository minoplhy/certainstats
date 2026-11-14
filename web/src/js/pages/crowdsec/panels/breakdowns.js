import { state } from '../state.js';
import { el, iconButton, formatCount, groupLabel } from '../format.js';
import { FIELDS, FIELD_LABELS } from '../url/state.js';

export const CARD_LIMIT = 8;

// rankedList renders "value, count, meter" rows. onPick(value, exclude) adds a filter.
export function rankedList(field, items, total, onPick) {
  const list = el('ol', 'cs-rank');
  if (!items.length) {
    list.append(el('li', 'cs-rank-empty muted', 'Nothing in this range.'));
    return list;
  }
  const top = Math.max(...items.map(i => i.alerts), 1);
  for (const item of items) {
    const row = el('li', 'cs-rank-row');
    const label = groupLabel(field, item);
    const pick = el('button', 'cs-rank-value mono', label);
    pick.type = 'button';
    pick.title = 'Show only ' + label;
    if (onPick) pick.addEventListener('click', () => onPick(item.value, false));
    else pick.disabled = true;
    const count = el('span', 'cs-rank-count mono', formatCount(item.alerts));
    const share = total ? Math.round(item.alerts / total * 100) : 0;
    count.title = share + '% of alerts';
    const track = el('span', 'usage-bar-track cs-rank-meter');
    const seg = el('span', 'usage-segment seg-disk');
    seg.style.width = (item.alerts / top * 100).toFixed(1) + '%';
    track.append(seg);
    row.append(pick);
    if (onPick) row.append(iconButton('exclude', 'Hide ' + label, () => onPick(item.value, true)));
    row.append(count, track);
    list.append(row);
  }
  return list;
}

// buildCards creates one card per breakdown slot, each with its own field picker.
export function buildCards(onFieldChange) {
  const ui = state.ui;
  ui.breakdowns.replaceChildren();
  ui.cards = state.view.groups.map((field, slot) => {
    const card = el('article', 'chart-card cs-rank-card');
    const head = el('div', 'chart-header-row');
    const select = el('select', 'form-select form-select-sm cs-group-select');
    select.setAttribute('aria-label', 'Group alerts by');
    for (const f of FIELDS) {
      const opt = el('option', '', 'By ' + FIELD_LABELS[f].toLowerCase());
      opt.value = f;
      select.append(opt);
    }
    select.value = field;
    select.addEventListener('change', () => onFieldChange(slot, select.value));
    const status = el('p', 'cs-status');
    status.setAttribute('role', 'status');
    head.append(select, el('span', 'muted mono cs-card-total'));
    const body = el('div', 'cs-card-body');
    card.append(head, status, body);
    ui.breakdowns.append(card);
    return {card, select, status, body, total: head.lastChild};
  });
}

export function renderCard(slot, data, onPick) {
  const c = state.ui.cards[slot];
  c.status.textContent = '';
  c.total.textContent = data.items.length ? 'top ' + data.items.length : '';
  c.body.replaceChildren(rankedList(data.field, data.items, data.total, (value, exclude) => onPick(data.field, value, exclude)));
}
