import { getPublicBasePath } from './dom.js';

// In-place navigation between the status page and an agent detail, plus the toolbar.

export function handlePubItemClick(event, pubId) {
  if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) {
    return; // Allow standard browser new tab
  }
  event.preventDefault();
  navigateToPubAgent(pubId);
}

export function navigateToPubAgent(pubId) {
  const basePath = getPublicBasePath();
  const targetUrl = basePath + '/' + pubId;
  if (window.location.pathname !== targetUrl) {
    history.pushState({ pubId: pubId }, '', targetUrl);
  }
  window.dispatchEvent(new PopStateEvent('popstate'));
}

export function closePublicDetail() {
  const basePath = getPublicBasePath();
  const targetUrl = basePath || '/';
  if (window.location.pathname !== targetUrl) {
    history.pushState({}, '', targetUrl);
  }
  window.dispatchEvent(new PopStateEvent('popstate'));
}

export function setPublicViewMode(mode) {
  const gridContainer = document.getElementById('public-grid-container');
  const listContainer = document.getElementById('public-list-container');
  const btnGrid = document.getElementById('btn-pub-grid');
  const btnList = document.getElementById('btn-pub-list');

  const isList = mode !== 'grid';
  if (gridContainer) gridContainer.hidden = isList;
  if (listContainer) listContainer.hidden = !isList;
  if (btnGrid) { btnGrid.classList.toggle('active', !isList); btnGrid.setAttribute('aria-pressed', String(!isList)); }
  if (btnList) { btnList.classList.toggle('active', isList); btnList.setAttribute('aria-pressed', String(isList)); }
  try { localStorage.setItem('certainstats_public_view_mode', isList ? 'list' : 'grid'); } catch (e) {}
}

export function filterPublicMonitors() {
  const query = (document.getElementById('public-search-input').value || '').toLowerCase();
  document.querySelectorAll('.public-monitor-item, .public-monitor-row').forEach(el => {
    const name = (el.getAttribute('data-name') || '').toLowerCase();
    if (name.includes(query)) {
      el.style.display = '';
    } else {
      el.style.display = 'none';
    }
  });
  const matches=new Set(Array.from(document.querySelectorAll('.public-monitor-item, .public-monitor-row')).filter(node=>node.style.display!=='none').map(node=>node.getAttribute('data-pub-id')));
  let count=document.getElementById('public-search-input-count');if (!count) {count=document.createElement('p');count.id='public-search-input-count';count.className='muted';count.setAttribute('role','status');document.getElementById('public-search-input').parentElement.after(count);}
  count.textContent=matches.size ? matches.size+' matching servers' : 'No matching servers.';

}
