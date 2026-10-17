import { state } from '../state.js';
import { redrawSparklines } from './hub.js';

// Hub toolbar: card/list mode, search and the install modals.

export function setAgentViewMode(mode) {
  const gridContainer = document.getElementById('agents-grid-container');
  const listContainer = document.getElementById('agents-list-container');
  const btnGrid = document.getElementById('btn-view-grid');
  const btnList = document.getElementById('btn-view-list');

  const isList = mode === 'list';
  if (gridContainer) gridContainer.hidden = isList;
  if (listContainer) listContainer.hidden = !isList;
  if (!isList) redrawSparklines();
  if (btnGrid) { btnGrid.classList.toggle('active', !isList); btnGrid.setAttribute('aria-pressed', String(!isList)); }
  if (btnList) { btnList.classList.toggle('active', isList); btnList.setAttribute('aria-pressed', String(isList)); }
  try { localStorage.setItem('certainstats_view_mode', mode); } catch (e) {}
}

export function filterAgentsList() {
  const input = document.getElementById('agent-search-input');
  const query = (input ? input.value : '').toLowerCase();
  document.querySelectorAll('.agent-card-item, .agent-row-item').forEach(el => {
    const name = (el.getAttribute('data-agent-name') || '').toLowerCase();
    const cpu = (el.getAttribute('data-cpu-model') || '').toLowerCase();
    const id = (el.getAttribute('data-agent-id') || '').toLowerCase();
    if (name.includes(query) || cpu.includes(query) || id.includes(query)) {
      el.style.display = '';
    } else {
      el.style.display = 'none';
    }
  });
  const matches=new Set(Array.from(document.querySelectorAll('.agent-card-item, .agent-row-item')).filter(node=>node.style.display!=='none').map(node=>node.getAttribute('data-agent-id')));
  let count=document.getElementById('agent-search-input-count');if (!count) {count=document.createElement('p');count.id='agent-search-input-count';count.className='muted';count.setAttribute('role','status');document.getElementById('agent-search-input').parentElement.after(count);}
  count.textContent=matches.size ? matches.size+' matching servers' : 'No matching servers.';

}

export function showInpageReinstallModal() {
  if (!state.currentActiveAgentId) return;
  const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
  window.CertainStatsTelemetry.openReinstallModal({
    agentId: state.currentActiveAgentId,
    nickname: agent ? (agent.nickname || agent.agent_id) : state.currentActiveAgentId
  });
}

export function showInpageUninstallModal() {
  if (!state.currentActiveAgentId) return;
  const agent = state.agentsData.find(a => a.agent_id === state.currentActiveAgentId);
  window.CertainStatsTelemetry.openUninstallModal({
    agentId: state.currentActiveAgentId,
    nickname: agent ? (agent.nickname || agent.agent_id) : state.currentActiveAgentId
  });
}
