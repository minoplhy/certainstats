import { TIME_RANGES } from '../../../lib/time_ranges.js';

// Initialize Time Range Bar Buttons
export function initTimeRangeBar(containerId, options) {
  const container = document.getElementById(containerId);
  if (!container) return;

  const maxDays = options.maxDays ?? null;
  const storageKey = options.storageKey || (options.isPublic || (maxDays !== null && maxDays > 0) ? 'certainstats_public_active_hours' : 'certainstats_active_hours');
  let activeHours = parseInt(localStorage.getItem(storageKey) || '6', 10);
  if (isNaN(activeHours) || activeHours <= 0) activeHours = 6;
  const onSelect = options.onSelect || function () {};

  if (options.isPublic && maxDays <= 0) {container.hidden=true;return;}
  let ranges = TIME_RANGES;
  if (maxDays !== null && maxDays > 0) {
    const maxHours = maxDays * 24;
    ranges = TIME_RANGES.filter(r => r.value <= maxHours);
    if (activeHours > maxHours || !ranges.some(r => r.value === activeHours)) {
      activeHours = ranges.length > 0 ? (ranges.find(r => r.value === 6)?.value || ranges[0].value) : 6;
      localStorage.setItem(storageKey, activeHours.toString());
    }
  }

  container.innerHTML = '';
  ranges.forEach(r => {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'time-range-btn' + (r.value === activeHours ? ' active' : '');
    btn.textContent = r.label;
    btn.onclick = function (e) {
      e.preventDefault();
      e.stopPropagation();
      container.querySelectorAll('.time-range-btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      localStorage.setItem(storageKey, r.value.toString());
      onSelect(r.value);
    };
    container.appendChild(btn);
  });
}
