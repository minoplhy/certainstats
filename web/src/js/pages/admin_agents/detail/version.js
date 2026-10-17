// Page-config agents carry the version at the top level; /api/agents nests it under extensions.runtime.
export function agentRuntime(agent) {
  const runtime = agent.extensions?.runtime || {};
  return {
    agent_version: runtime.agent_version || agent.agent_version || '',
    protocol_version: runtime.protocol_version || agent.protocol_version || ''
  };
}

// Shows only the versions the driver actually reports; hidden when it reports none.
export function renderAgentVersion(agent) {
  const line = document.getElementById('detail-version-line');
  if (!line) return;
  const runtime = agentRuntime(agent);
  const parts = [];
  if (runtime.agent_version) parts.push('Agent software: ' + runtime.agent_version);
  if (runtime.protocol_version) parts.push('Protocol: ' + runtime.protocol_version);
  line.textContent = parts.join(' · ');
  line.hidden = parts.length === 0;
}
