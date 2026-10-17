// Reads the JSON a template renders into #page-config, when it is meant for this module.
export function readPageConfig(module) {
  const node = document.getElementById('page-config');
  if (!node || node.dataset.module !== module) return null;
  return JSON.parse(node.textContent);
}

// Page configuration carries Go field names; the page scripts use snake_case.
export function snake(value) {
  if (Array.isArray(value)) return value.map(snake);
  if (!value || typeof value !== 'object') return value;
  const result = {};
  for (const [key, item] of Object.entries(value)) {
    const name = key.replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2').replace(/([a-z0-9])([A-Z])/g, '$1_$2').toLowerCase();
    result[name] = snake(item);
  }
  return result;
}
