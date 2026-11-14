import { state } from '../state.js';
import { el, formatCount } from '../format.js';
import { alertRow } from './row.js';

const COLUMNS = [
  {key: 'time', label: 'Time'}, {key: 'ip', label: 'IP'}, {key: 'scenario', label: 'Scenario'},
  {key: 'country', label: 'Country'}, {key: 'machine', label: 'Machine'}, {key: 'action', label: 'Action'}, {key: 'events', label: 'Events'}
];

// renderHead draws sortable column headers with aria-sort.
export function renderHead(onSort) {
  const v = state.view;
  state.ui.tableHead.replaceChildren(...COLUMNS.map(col => {
    const cell = el('span', 'cs-head-cell cs-col-' + col.key);
    cell.setAttribute('role', 'columnheader');
    const active = v.sort === col.key;
    cell.setAttribute('aria-sort', active ? (v.order === 'asc' ? 'ascending' : 'descending') : 'none');
    const btn = el('button', 'cs-sort' + (active ? ' is-active' : ''), col.label + (active ? (v.order === 'asc' ? ' ↑' : ' ↓') : ''));
    btn.type = 'button';
    btn.addEventListener('click', () => {
      if (v.sort === col.key) v.order = v.order === 'asc' ? 'desc' : 'asc';
      else { v.sort = col.key; v.order = col.key === 'time' || col.key === 'events' ? 'desc' : 'asc'; }
      v.page = 1;
      onSort();
    });
    cell.append(btn);
    return cell;
  }));
}

export function renderRows(page, actions) {
  const ui = state.ui;
  const v = state.view;
  ui.rows.replaceChildren(...page.items.map(a => alertRow(a, actions)));
  const pages = Math.max(1, Math.ceil(page.total / page.limit));
  ui.count.textContent = formatCount(page.total) + (page.total === 1 ? ' alert' : ' alerts');
  ui.page.textContent = page.total ? 'Page ' + page.page + ' of ' + formatCount(pages) : '';
  ui.prev.disabled = v.page <= 1;
  ui.next.disabled = v.page >= pages;
  ui.limit.value = String(v.limit);
  ui.tableMessage.textContent = page.items.length ? '' : (page.total ? 'This page is past the last alert.' : 'No alerts match.');
}

export function bindPaging(onChange) {
  const ui = state.ui;
  ui.prev.addEventListener('click', () => { if (state.view.page > 1) { state.view.page--; onChange(); } });
  ui.next.addEventListener('click', () => { state.view.page++; onChange(); });
  ui.limit.addEventListener('change', () => { state.view.limit = Number(ui.limit.value); state.view.page = 1; onChange(); });
}
