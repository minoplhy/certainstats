import { TIME_RANGES } from '../../../lib/time_ranges.js';

let timePickerInstance = 0;

// Initialize Dropdown & Custom Range Date-Time Picker
export function initCustomTimePicker(containerId, options = {}) {
  const container = typeof containerId === 'string' ? document.getElementById(containerId) : containerId;
  if (!container) return;

  container._timePicker?.destroy();
  const prefix = 'time-picker-' + (++timePickerInstance) + '-';
  const control = id => container.querySelector('#' + prefix + id);
  const remember = hours => {
    try {
      localStorage.setItem(storageKey, String(hours));
    } catch (e) {}
  };
  const maxDays = options.maxDays ?? null;
  const storageKey = options.storageKey || (options.isPublic || (maxDays !== null && maxDays > 0) ? 'certainstats_public_active_hours' : 'certainstats_active_hours');
  let remembered = '6';
  try {remembered = localStorage.getItem(storageKey) || '6';} catch (e) {}
  let activeHours = parseInt(options.activeHours ?? remembered, 10);
  if (!TIME_RANGES.some(r => r.value === activeHours)) activeHours = 6;
  let customRange = null; // { start, end }
  const onApply = options.onApply || function () {};

  if (options.isPublic && maxDays <= 0) {container.hidden=true;return;}
  let ranges = TIME_RANGES;
  if (maxDays !== null && maxDays > 0) {
    const maxHours = maxDays * 24;
    ranges = TIME_RANGES.filter(r => r.value <= maxHours);
    if (activeHours > maxHours || !ranges.some(r => r.value === activeHours)) {
      activeHours = ranges.length > 0 ? (ranges.find(r => r.value === 6)?.value || ranges[0].value) : 6;
      remember(activeHours);
    }
  }

  function hidePickerDropdown(dropdown) {
    dropdown.style.display = 'none';
    const trigger = control('time-picker-trigger');
    if (trigger) trigger.setAttribute('aria-expanded', 'false');
  }

  function toDatetimeLocal(d) {
    const ten = (i) => (i < 10 ? '0' : '') + i;
    return `${d.getFullYear()}-${ten(d.getMonth() + 1)}-${ten(d.getDate())}T${ten(d.getHours())}:${ten(d.getMinutes())}`;
  }

  function render() {
    container.innerHTML = `
      <div class="time-controls-wrapper">
        <div class="time-range-bar">
          ${ranges.map(r => `
            <button type="button" class="time-range-btn quick-range-pill ${!customRange && activeHours === r.value ? 'active' : ''}" data-val="${r.value}">
              ${r.label}
            </button>
          `).join('')}
        </div>
        <div class="time-picker">
          <button type="button" id="${prefix}time-picker-trigger" class="btn btn-secondary btn-sm${customRange ? ' is-active' : ''}" aria-haspopup="true" aria-expanded="false" aria-controls="${prefix}time-picker-dropdown">
            <span id="${prefix}time-picker-label">${customRange ? 'Custom range' : 'Custom'}</span>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
          </button>
          <div id="${prefix}time-picker-dropdown" class="time-picker-dropdown-panel" style="display: none;">
            <div class="picker-label">Quick ranges</div>
            <div class="picker-grid">
              ${ranges.map(r => `
                <button type="button" class="btn btn-secondary btn-sm dropdown-range-btn${!customRange && activeHours === r.value ? ' is-active' : ''}" data-val="${r.value}">${r.label}</button>
              `).join('')}
            </div>
            <div class="picker-label">Custom range</div>
            <div class="picker-fields">
              <label class="picker-field">
                <span>Start</span>
                <input type="datetime-local" id="${prefix}custom-start-input" class="form-input form-input-sm">
              </label>
              <label class="picker-field">
                <span>End</span>
                <input type="datetime-local" id="${prefix}custom-end-input" class="form-input form-input-sm">
              </label>
            </div>
            <div class="picker-actions">
              ${customRange ? `<button type="button" id="${prefix}clear-custom-range" class="btn btn-secondary btn-sm">Reset</button>` : ''}
              <button type="button" id="${prefix}apply-custom-range" class="btn btn-primary btn-sm">Apply</button>
            </div>
          </div>
        </div>
      </div>
    `;

    if (options.isPublic || maxDays !== null) container.querySelector('.time-picker')?.remove();
    const trigger = control('time-picker-trigger');
    const dropdown = control('time-picker-dropdown');
    const startInput = control('custom-start-input');
    const endInput = control('custom-end-input');

    const now = new Date();
    const start = new Date(now.getTime() - activeHours * 3600 * 1000);
    if (startInput) {
      startInput.value = toDatetimeLocal(customRange ? new Date(customRange.start) : start);
      startInput.max = toDatetimeLocal(now);
      if (maxDays !== null && maxDays > 0) {
        startInput.min = toDatetimeLocal(new Date(now.getTime() - maxDays * 24 * 3600 * 1000));
      }
    }
    if (endInput) {
      endInput.value = toDatetimeLocal(customRange ? new Date(customRange.end) : now);
      endInput.max = toDatetimeLocal(now);
      if (maxDays !== null && maxDays > 0) {
        endInput.min = toDatetimeLocal(new Date(now.getTime() - maxDays * 24 * 3600 * 1000));
      }
    }

    if (trigger && dropdown) {
      trigger.onclick = function (e) {
        e.stopPropagation();
        const isHidden = dropdown.style.display === 'none';
        if (isHidden) {
          dropdown.style.display = 'block';
          trigger.setAttribute('aria-expanded', 'true');
          // Check collision with left/right viewport boundaries
          const triggerRect = trigger.getBoundingClientRect();
          const dropdownWidth = dropdown.offsetWidth || 290;
          if (triggerRect.right - dropdownWidth < 8) {
            dropdown.style.left = '0';
            dropdown.style.right = 'auto';
          } else {
            dropdown.style.right = '0';
            dropdown.style.left = 'auto';
          }
        } else {
          hidePickerDropdown(dropdown);
        }
      };
      dropdown.onclick = function (e) { e.stopPropagation(); };
    }

    container.querySelectorAll('.quick-range-pill, .dropdown-range-btn').forEach(btn => {
      btn.onclick = function (e) {
        e.preventDefault();
        e.stopPropagation();
        const val = parseInt(this.getAttribute('data-val'), 10);
        activeHours = val;
        customRange = null;
        remember(val);
        if (dropdown) hidePickerDropdown(dropdown);
        render();
        onApply({ hours: val, customRange: null });
      };
    });

    const applyBtn = control('apply-custom-range');
    if (applyBtn) {
      applyBtn.onclick = function () {
        if (!startInput.value || !endInput.value) return;
        const startMs = new Date(startInput.value).getTime();
        const endMs = new Date(endInput.value).getTime();
        if (isNaN(startMs) || isNaN(endMs) || startMs >= endMs) {
          alert('Invalid time range. Start time must precede end time.');
          return;
        }
        customRange = { start: startMs, end: endMs };
        if (dropdown) hidePickerDropdown(dropdown);
        render();
        onApply({ hours: activeHours, customRange: customRange });
      };
    }

    const clearBtn = control('clear-custom-range');
    if (clearBtn) {
      clearBtn.onclick = function () {
        customRange = null;
        if (dropdown) hidePickerDropdown(dropdown);
        render();
        onApply({ hours: activeHours, customRange: null });
      };
    }
  }

  const hideDropdown = () => {
    const dropdown = control('time-picker-dropdown');
    if (dropdown) hidePickerDropdown(dropdown);
  };
  const outsideClick = event => {
    if (!container.contains(event.target)) hideDropdown();
  };
  document.addEventListener('click', outsideClick);
  const keydown = function(event) {
    if (event.key === 'Escape') {
      hideDropdown();
      control('time-picker-trigger')?.focus();
      return;
    }
    if (!['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) return;
    const buttons=Array.from(container.querySelectorAll('.quick-range-pill'));
    const index=buttons.indexOf(document.activeElement);if(index<0) return;event.preventDefault();
    const next=event.key==='Home'?0:event.key==='End'?buttons.length-1:(index+(event.key==='ArrowRight'?1:-1)+buttons.length)%buttons.length;
    const value=buttons[next].getAttribute('data-val');buttons[next].click();container.querySelector('.quick-range-pill[data-val="'+value+'"]')?.focus();
  };
  container.addEventListener('keydown', keydown);
  render();

  const getState = () => ({hours: activeHours, customRange: customRange ? {...customRange} : null});
  const picker = {
    getState,
    setState: function (state, silent = true) {
      if (ranges.some(r => r.value === state.hours)) activeHours = state.hours;
      customRange = state.customRange ? {...state.customRange} : null;
      render();
      if (!silent) onApply(getState());
    },
    destroy: function () {
      document.removeEventListener('click', outsideClick);
      container.removeEventListener('keydown', keydown);
      container.replaceChildren();
      if (container._timePicker === picker) delete container._timePicker;
    },
    setCustomRange: function (startMs, endMs, silent = false) {
      customRange = { start: startMs, end: endMs };
      render();
      if (!silent) onApply(getState());
    }
  };
  container._timePicker = picker;
  return picker;
}
