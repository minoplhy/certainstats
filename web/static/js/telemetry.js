/* CertainStats — Complete Client-Side Telemetry, Cluster Calculations, Reordering & Interactive UI Manager */

(function () {
  'use strict';

  const Modal = {
    open: function (target) {
      const el = typeof target === 'string' ? document.getElementById(target) : target;
      if (!el) return;
      if (el.tagName === 'DIALOG') {
        if (!el.open) el.showModal();
      } else {
        el.style.display = 'block';
      }
    },
    close: function (target) {
      const el = typeof target === 'string' ? document.getElementById(target) : target;
      if (!el) return;
      if (el.tagName === 'DIALOG') {
        if (el.open) el.close();
      } else {
        el.style.display = 'none';
      }
    },
    // Styled replacement for window.confirm(), backed by the shared #confirm-modal partial.
    // opts: { title, message, label, danger, match }. Resolves true only when confirmed.
    confirm: function (opts) {
      opts = opts || {};
      const dlg = document.getElementById('confirm-modal');
      if (!dlg) return Promise.resolve(window.confirm(opts.message || opts.title || 'Are you sure?'));
      const ok = document.getElementById('confirm-modal-ok');
      const matchBox = document.getElementById('confirm-modal-match');
      const matchInput = document.getElementById('confirm-modal-match-input');
      document.getElementById('confirm-modal-title').textContent = opts.title || 'Are you sure?';
      document.getElementById('confirm-modal-message').textContent = opts.message || '';
      document.getElementById('confirm-modal-match-text').textContent = opts.match || '';
      ok.textContent = opts.label || 'Confirm';
      ok.className = 'btn ' + (opts.danger ? 'btn-danger-solid' : 'btn-primary');
      matchBox.hidden = !opts.match;
      matchInput.value = '';
      ok.disabled = !!opts.match;
      matchInput.oninput = function () { ok.disabled = matchInput.value.trim() !== opts.match; };
      dlg.returnValue = '';
      return new Promise(function (resolve) {
        dlg.addEventListener('close', function () { resolve(dlg.returnValue === 'ok'); }, { once: true });
        Modal.open(dlg);
        (opts.match ? matchInput : ok).focus();
      });
    }
  };
  window.CertainStatsModal = Modal;

  // Forms with data-confirm ask through Modal.confirm before submitting.
  // data-confirm-flash is shown as a toast on the page the submit lands on.
  document.addEventListener('submit', function (e) {
    const form = e.target.closest('form[data-confirm]');
    if (!form) return;
    e.preventDefault();
    Modal.confirm({
      title: form.getAttribute('data-confirm-title'),
      message: form.getAttribute('data-confirm'),
      label: form.getAttribute('data-confirm-label'),
      danger: form.hasAttribute('data-confirm-danger'),
      match: form.getAttribute('data-confirm-match')
    }).then(function (confirmed) {
      if (!confirmed) return;
      const flash = form.getAttribute('data-confirm-flash');
      if (flash) {
        try { sessionStorage.setItem('certainstats_flash', flash); } catch (err) {}
      }
      form.submit();
    });
  });

  function onReady(fn) {
    if (document.readyState !== 'loading') {
      fn();
    } else {
      document.addEventListener('DOMContentLoaded', fn);
    }
  }

  const TIME_RANGES = [
    { label: '1h', value: 1 },
    { label: '6h', value: 6 },
    { label: '12h', value: 12 },
    { label: '24h', value: 24 },
    { label: '2d', value: 48 },
    { label: '7d', value: 168 },
    { label: '30d', value: 720 },
    { label: '90d', value: 2160 },
    { label: '180d', value: 4320 },
    { label: '1y', value: 8760 },
    { label: '2y', value: 17520 }
  ];

  let globalSocket = null;
  const updateListeners = [];

  function normalizeSnapshot(snap) {
    if (!snap) return null;
    return {
      cpu_usage_percent: snap.cpu_usage_percent ?? snap.CPUUsagePercent ?? snap.cpuUsagePercent ?? 0,
      cpu_iowait_percent: snap.cpu_iowait_percent ?? snap.CPUIOWaitPercent ?? snap.cpuIOWaitPercent ?? 0,
      cpu_steal_percent: snap.cpu_steal_percent ?? snap.CPUStealPercent ?? snap.cpuStealPercent ?? 0,
      ram_used_bytes: snap.ram_used_bytes ?? snap.RAMUsedBytes ?? snap.ramUsedBytes ?? 0,
      ram_swap_used_bytes: snap.ram_swap_used_bytes ?? snap.RAMSwapUsedBytes ?? snap.ramSwapUsedBytes ?? 0,
      disk_used_bytes: snap.disk_used_bytes ?? snap.DiskUsedBytes ?? snap.diskUsedBytes ?? 0,
      disk_total_bytes: snap.disk_total_bytes ?? snap.DiskTotalBytes ?? snap.diskTotalBytes ?? 0,
      disks: (snap.disks || snap.Disks || []).map(d => ({
        path: d.path || d.Path || '',
        total_bytes: d.total_bytes ?? d.TotalBytes ?? 0,
        used_bytes: d.used_bytes ?? d.UsedBytes ?? 0,
        read_bytes: d.read_bytes ?? d.ReadBytes ?? 0,
        write_bytes: d.write_bytes ?? d.WriteBytes ?? 0
      })),
      rx_bytes: snap.rx_bytes ?? snap.RXBytes ?? snap.rxBytes ?? 0,
      tx_bytes: snap.tx_bytes ?? snap.TXBytes ?? snap.txBytes ?? 0,
      rx_bps: snap.rx_bps ?? snap.RXBps ?? snap.rxBps ?? 0,
      tx_bps: snap.tx_bps ?? snap.TXBps ?? snap.txBps ?? 0,
      disk_read_bps: snap.disk_read_bps ?? snap.DiskReadBps ?? snap.diskReadBps ?? 0,
      disk_write_bps: snap.disk_write_bps ?? snap.DiskWriteBps ?? snap.diskWriteBps ?? 0,
      uptime: snap.uptime ?? snap.Uptime ?? null
    };
  }

  // Parse User-Agent string to readable OS and Browser
  function parseUserAgent(ua) {
    if (!ua) return { os: 'Unknown OS', browser: 'Unknown Browser', device: 'Desktop' };
    let os = 'Unknown OS';
    let browser = 'Unknown Browser';
    let isMobile = /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(ua);

    if (/Windows NT 10/i.test(ua)) os = 'Windows 10/11';
    else if (/Windows NT/i.test(ua)) os = 'Windows';
    else if (/Macintosh|Mac OS X/i.test(ua)) os = 'macOS';
    else if (/Android/i.test(ua)) os = 'Android';
    else if (/iPhone|iPad|iPod/i.test(ua)) os = 'iOS';
    else if (/Linux/i.test(ua)) os = 'Linux';

    if (/Edg\//i.test(ua)) browser = 'Edge';
    else if (/Chrome\//i.test(ua)) browser = 'Chrome';
    else if (/Safari\//i.test(ua) && !/Chrome/i.test(ua)) browser = 'Safari';
    else if (/Firefox\//i.test(ua)) browser = 'Firefox';
    else if (/MSIE|Trident/i.test(ua)) browser = 'Internet Explorer';

    return {
      os: os,
      browser: browser,
      label: `${browser} on ${os}`,
      device: isMobile ? 'Mobile' : 'Desktop'
    };
  }

  window.CertainStatsTelemetry = {
    TIME_RANGES: TIME_RANGES,
    onReady: onReady,
    normalizeSnapshot: normalizeSnapshot,
    parseUserAgent: parseUserAgent,

    // Global Toast Notification
    showToast: function (message, isSuccess) {
      let container = document.getElementById('toast-container');
      if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        document.body.appendChild(container);
      }

      const toast = document.createElement('div');
      toast.className = 'toast ' + (isSuccess ? 'flash-success' : 'flash-error');
      toast.textContent = message;

      container.appendChild(toast);
      setTimeout(() => {
        toast.style.opacity = '0';
        toast.style.transition = 'opacity 0.3s ease';
        setTimeout(() => toast.remove(), 300);
      }, 3500);
    },

    formatUptime: function (seconds) {
      if (!seconds || seconds <= 0) return '0m';
      const s = Math.floor(seconds);
      const days = Math.floor(s / 86400);
      const hours = Math.floor((s % 86400) / 3600);
      const minutes = Math.floor((s % 3600) / 60);

      if (days > 0) {
        return `${days}d ${hours}h`;
      }
      if (hours > 0) {
        return `${hours}h ${minutes}m`;
      }
      return `${minutes}m`;
    },

    copyText: function (text, label) {
      if (!text || text === '-') return;
      const lbl = label || 'ID';
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(() => {
          this.showToast(lbl + ' copied to clipboard', true);
        }).catch(() => {
          prompt('Copy ' + lbl + ':', text);
        });
      } else {
        prompt('Copy ' + lbl + ':', text);
      }
    },

    // Cluster Stats Calculation & DOM Rendering (4 Dual-Metric Cards)
    // Storage partition card used by both the admin and public detail views.
    partitionCardHtml: function (path, used, total, readBytes, writeBytes, opts) {
      opts = opts || {};
      const fmt = window.CertainStatsChart.formatBytes;
      const pct = total > 0 ? (used / total) * 100 : 0;
      const esc = v => String(v).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
      const idAttr = (suffix) => opts.idPrefix ? ' id="' + opts.idPrefix + suffix + '"' : '';
      const showIO = opts.showIO !== false;
      return '<div class="partition-card">' +
        '<div class="partition-head"><span class="partition-path mono">' + esc(path) + '</span>' +
        '<span class="partition-pct mono"' + idAttr('pct') + '>' + (pct > 0 ? pct.toFixed(1) + '%' : '–') + '</span></div>' +
        '<div class="partition-val mono"' + idAttr('val') + '>' + fmt(used) + (total ? ' <span class="muted">of ' + fmt(total) + '</span>' : '') + '</div>' +
        '<div class="hw-card-progress-track"><div class="hw-card-progress-fill seg-disk' + (pct >= 90 ? ' is-high' : '') + '"' + idAttr('bar') + ' style="width:' + Math.min(pct, 100).toFixed(1) + '%"></div></div>' +
        (showIO ? '<div class="partition-io mono"><span>Read ' + fmt(readBytes || 0) + '</span><span>Written ' + fmt(writeBytes || 0) + '</span></div>' : '') +
        '</div>';
    },

    renderClusterStats: function (prefix, agents, liveMetrics) {
      let liveRxBps = 0, liveTxBps = 0;
      let liveDiskReadBps = 0, liveDiskWriteBps = 0;
      let totalRxBytes = 0, totalTxBytes = 0;
      let totalDiskReadBytes = 0, totalDiskWriteBytes = 0;
      let onlineCount = 0;

      agents.forEach(a => {
        const id = a.agent_id || a.public_id;
        const rawSnap = liveMetrics[id];
        const snap = normalizeSnapshot(rawSnap);
        const isOnline = a.is_online != null ? a.is_online : a.IsOnline;
        if (isOnline) onlineCount++;

        if (snap) {
          liveRxBps += snap.rx_bps || 0;
          liveTxBps += snap.tx_bps || 0;
          liveDiskReadBps += snap.disk_read_bps || 0;
          liveDiskWriteBps += snap.disk_write_bps || 0;
        }

        if (a.net && (a.net.total_rx_bytes != null || a.net.TotalRxBytes != null)) {
          totalRxBytes += a.net.total_rx_bytes || a.net.TotalRxBytes || 0;
          totalTxBytes += a.net.total_tx_bytes || a.net.TotalTxBytes || 0;
        } else if (a.total_rx_bytes != null || a.TotalRxBytes != null) {
          totalRxBytes += a.total_rx_bytes || a.TotalRxBytes || 0;
          totalTxBytes += a.total_tx_bytes || a.TotalTxBytes || 0;
        }

        let agentDiskRead = a.total_disk_read_bytes || a.TotalDiskReadBytes || 0;
        let agentDiskWrite = a.total_disk_write_bytes || a.TotalDiskWriteBytes || 0;
        if (!agentDiskRead && !agentDiskWrite) {
          const disks = a.disks || a.Disks || [];
          disks.forEach(d => {
            agentDiskRead += d.read_bytes || d.ReadBytes || 0;
            agentDiskWrite += d.write_bytes || d.WriteBytes || 0;
          });
        }
        totalDiskReadBytes += agentDiskRead;
        totalDiskWriteBytes += agentDiskWrite;
      });

      // Update Public Header Status
      const overallDot = document.getElementById('public-overall-status-dot');
      const overallText = document.getElementById('public-overall-status-text');
      const total = agents.length;
      const offline = total - onlineCount;
      if (overallDot && overallText) {
        if (total === 0) {
          overallDot.className = 'status-dot';
          overallText.textContent = 'No servers yet';
        } else if (offline === 0) {
          overallDot.className = 'status-dot online';
          overallText.textContent = 'All systems operational';
        } else {
          overallDot.className = 'status-dot offline';
          overallText.textContent = onlineCount === 0 ? 'Major outage' : 'Partial outage';
        }
      }

      // Public status banner (mirrors the server-rendered text in public_dashboard.html)
      const banner = document.getElementById('public-status-banner');
      if (banner && total > 0) {
        const bannerText = document.getElementById('public-status-banner-text');
        const bannerDot = document.getElementById('public-status-banner-dot');
        let cls = 'is-ok', text = 'All systems operational';
        if (offline > 0 && onlineCount === 0) {
          cls = 'is-bad';
          text = 'Major outage: all ' + total + ' servers offline';
        } else if (offline > 0) {
          cls = 'is-warn';
          text = 'Partial outage: ' + offline + ' of ' + total + (total === 1 ? ' server' : ' servers') + ' offline';
        }
        banner.className = 'banner ' + cls;
        if (bannerText) bannerText.textContent = text;
        if (bannerDot) bannerDot.className = 'status-dot ' + (offline === 0 ? 'online' : 'offline');
      }

      // Update Card 1: TOTAL BANDWIDTH (Live Rate)
      const elRx = document.getElementById(prefix + 'live-rx-val');
      const elTx = document.getElementById(prefix + 'live-tx-val');
      if (elRx) elRx.innerHTML = '↓ ' + window.CertainStatsChart.formatBps(liveRxBps);
      if (elTx) elTx.innerHTML = '↑ ' + window.CertainStatsChart.formatBps(liveTxBps);

      // Update Card 2: TOTAL DISK I/O (Live Rate)
      const elDiskR = document.getElementById(prefix + 'live-disk-read-val');
      const elDiskW = document.getElementById(prefix + 'live-disk-write-val');
      if (elDiskR) elDiskR.innerHTML = window.CertainStatsChart.formatBps(liveDiskReadBps);
      if (elDiskW) elDiskW.innerHTML = window.CertainStatsChart.formatBps(liveDiskWriteBps);

      // Update Card 3: TOTAL TRAFFIC (Cumulative Lifetime)
      const elTotRx = document.getElementById(prefix + 'total-rx-val');
      const elTotTx = document.getElementById(prefix + 'total-tx-val');
      if (elTotRx) elTotRx.innerHTML = '↓ ' + window.CertainStatsChart.formatBytes(totalRxBytes);
      if (elTotTx) elTotTx.innerHTML = '↑ ' + window.CertainStatsChart.formatBytes(totalTxBytes);

      // Update Card 4: TOTAL DISK READ/WRITE (Cumulative Lifetime)
      const elTotDiskR = document.getElementById(prefix + 'total-disk-read-val');
      const elTotDiskW = document.getElementById(prefix + 'total-disk-write-val');
      if (elTotDiskR) elTotDiskR.innerHTML = window.CertainStatsChart.formatBytes(totalDiskReadBytes);
      if (elTotDiskW) elTotDiskW.innerHTML = window.CertainStatsChart.formatBytes(totalDiskWriteBytes);
    },

    // Initialize Dropdown & Custom Range Date-Time Picker
    initCustomTimePicker: function (containerId, options) {
      const container = document.getElementById(containerId);
      if (!container) return;

      const maxDays = options.maxDays || null;
      const storageKey = options.storageKey || (options.isPublic || (maxDays !== null && maxDays > 0) ? 'certainstats_public_active_hours' : 'certainstats_active_hours');
      let activeHours = parseInt(localStorage.getItem(storageKey) || '6', 10);
      if (isNaN(activeHours) || activeHours <= 0) activeHours = 6;
      let customRange = null; // { start, end }
      const onApply = options.onApply || function () {};

      let ranges = TIME_RANGES;
      if (maxDays !== null && maxDays > 0) {
        const maxHours = maxDays * 24;
        ranges = TIME_RANGES.filter(r => r.value <= maxHours);
        if (activeHours > maxHours || !ranges.some(r => r.value === activeHours)) {
          activeHours = ranges.length > 0 ? (ranges.find(r => r.value === 6)?.value || ranges[0].value) : 6;
          localStorage.setItem(storageKey, activeHours.toString());
        }
      }

      function hidePickerDropdown(dropdown) {
        dropdown.style.display = 'none';
        const trigger = document.getElementById('time-picker-trigger');
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
              <button type="button" id="time-picker-trigger" class="btn btn-secondary btn-sm${customRange ? ' is-active' : ''}" aria-haspopup="true" aria-expanded="false">
                <span id="time-picker-label">${customRange ? 'Custom range' : 'Custom'}</span>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
              </button>
              <div id="time-picker-dropdown" class="time-picker-dropdown-panel" style="display: none;">
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
                    <input type="datetime-local" id="custom-start-input" class="form-input form-input-sm">
                  </label>
                  <label class="picker-field">
                    <span>End</span>
                    <input type="datetime-local" id="custom-end-input" class="form-input form-input-sm">
                  </label>
                </div>
                <div class="picker-actions">
                  ${customRange ? `<button type="button" id="clear-custom-range" class="btn btn-secondary btn-sm">Reset</button>` : ''}
                  <button type="button" id="apply-custom-range" class="btn btn-primary btn-sm">Apply</button>
                </div>
              </div>
            </div>
          </div>
        `;

        const trigger = document.getElementById('time-picker-trigger');
        const dropdown = document.getElementById('time-picker-dropdown');
        const startInput = document.getElementById('custom-start-input');
        const endInput = document.getElementById('custom-end-input');

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

        document.addEventListener('click', function () {
          if (dropdown) hidePickerDropdown(dropdown);
        });

        container.querySelectorAll('.quick-range-pill, .dropdown-range-btn').forEach(btn => {
          btn.onclick = function (e) {
            e.preventDefault();
            e.stopPropagation();
            const val = parseInt(this.getAttribute('data-val'), 10);
            activeHours = val;
            customRange = null;
            localStorage.setItem(storageKey, val.toString());
            if (dropdown) hidePickerDropdown(dropdown);
            render();
            onApply({ hours: val, customRange: null });
          };
        });

        const applyBtn = document.getElementById('apply-custom-range');
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

        const clearBtn = document.getElementById('clear-custom-range');
        if (clearBtn) {
          clearBtn.onclick = function () {
            customRange = null;
            if (dropdown) hidePickerDropdown(dropdown);
            render();
            onApply({ hours: activeHours, customRange: null });
          };
        }
      }

      render();

      return {
        setCustomRange: function (startMs, endMs) {
          customRange = { start: startMs, end: endMs };
          render();
          onApply({ hours: activeHours, customRange: customRange });
        }
      };
    },

    // Drag-and-Drop Reordering Helper
    initReorderableList: function (containerId, onOrderChange) {
      const container = document.getElementById(containerId);
      if (!container) return;

      let draggedItem = null;

      container.querySelectorAll('.reorder-item').forEach(item => {
        item.setAttribute('draggable', 'true');

        item.addEventListener('dragstart', function (e) {
          draggedItem = this;
          e.dataTransfer.effectAllowed = 'move';
          this.style.opacity = '0.5';
        });

        item.addEventListener('dragend', function () {
          draggedItem = null;
          this.style.opacity = '1';
          if (onOrderChange) onOrderChange();
        });

        item.addEventListener('dragover', function (e) {
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
        });

        item.addEventListener('drop', function (e) {
          e.preventDefault();
          if (draggedItem && draggedItem !== this) {
            const allItems = Array.from(container.querySelectorAll('.reorder-item'));
            const draggedIdx = allItems.indexOf(draggedItem);
            const targetIdx = allItems.indexOf(this);
            if (draggedIdx < targetIdx) {
              container.insertBefore(draggedItem, this.nextSibling);
            } else {
              container.insertBefore(draggedItem, this);
            }
          }
        });
      });
    },

    getPanelPath: function () {
      return (document.body?.getAttribute('data-panel-path') || '').replace(/\/+$/, '');
    },

    getPublicPath: function () {
      return (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    },

    getStaticPath: function () {
      return (document.body?.getAttribute('data-static-path') || '').replace(/\/+$/, '');
    },

    // WebSocket Telemetry Connection
    initWebSocket: function (wsPath, onUpdate) {
      if (onUpdate && typeof onUpdate === 'function') {
        updateListeners.push(onUpdate);
      }

      if (globalSocket && globalSocket.readyState === WebSocket.OPEN) {
        return;
      }

      if (!wsPath) {
        const panelPath = this.getPanelPath();
        wsPath = (panelPath ? panelPath : '') + '/api/ws';
      }

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      let cleanPath = wsPath.replace(/\/+/g, '/');
      if (!cleanPath.startsWith('/')) {
        cleanPath = '/' + cleanPath;
      }
      const fullUrl = (wsPath.startsWith('ws://') || wsPath.startsWith('wss://'))
        ? wsPath
        : protocol + '//' + window.location.host + cleanPath;

      console.log('[CertainStats WS] Connecting to:', fullUrl);

      function connect() {
        try {
          globalSocket = new WebSocket(fullUrl);

          globalSocket.onopen = function () {
            console.log('[CertainStats WS] Live connected successfully');
          };

          globalSocket.onmessage = function (event) {
            try {
              const msg = JSON.parse(event.data);
              if (msg.type === 'agent_update' || msg.type === 'telemetry_snapshot') {
                const snaps = msg.data || {};
                updateListeners.forEach(fn => fn(snaps));
              }
            } catch (e) {
              console.error('[CertainStats WS] Parse Error:', e);
            }
          };

          globalSocket.onerror = function (err) {
            console.warn('[CertainStats WS] Error:', err);
          };

          globalSocket.onclose = function () {
            console.log('[CertainStats WS] Disconnected. Reconnecting in 3s...');
            setTimeout(connect, 3000);
          };
        } catch (e) {
          console.error('[CertainStats WS] Connect exception:', e);
          setTimeout(connect, 5000);
        }
      }

      connect();
    },

    // Scroll Position Restoration
    initScrollRestoration: function () {
      const scrollPos = sessionStorage.getItem('cs_scroll_pos');
      if (scrollPos) {
        window.scrollTo(0, parseInt(scrollPos, 10));
        sessionStorage.removeItem('cs_scroll_pos');
      }
      window.addEventListener('beforeunload', function () {
        sessionStorage.setItem('cs_scroll_pos', window.scrollY.toString());
      });
    },

    // Initialize Time Range Bar Buttons
    initTimeRangeBar: function (containerId, options) {
      const container = document.getElementById(containerId);
      if (!container) return;

      const maxDays = options.maxDays || null;
      const storageKey = options.storageKey || (options.isPublic || (maxDays !== null && maxDays > 0) ? 'certainstats_public_active_hours' : 'certainstats_active_hours');
      let activeHours = parseInt(localStorage.getItem(storageKey) || '6', 10);
      if (isNaN(activeHours) || activeHours <= 0) activeHours = 6;
      const onSelect = options.onSelect || function () {};

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
    },

    // In-Place HTML5 Path & Hash Router (BASE_PATH/{AGENT_ID})
    initRouter: function (options) {
      const onNavigate = options.onNavigate || function () {};
      const basePath = (options.basePath || '').replace(/\/+$/, '');

      function getTargetId() {
        // 1. Check legacy hash (#/{id} or #{id}) and migrate if present
        if (window.location.hash) {
          const hash = window.location.hash.replace(/^#\/?/, '').trim();
          if (hash) {
            const parts = hash.split('/');
            const targetId = parts[parts.length - 1];
            if (targetId && !['agents', 'dashboards', 'alerts', 'settings'].includes(targetId)) {
              const cleanUrl = (basePath || '') + '/' + targetId;
              try {
                history.replaceState({ targetId: targetId }, '', cleanUrl);
              } catch (e) {}
              return targetId;
            }
          }
        }

        // 2. Check pathname relative to basePath
        let path = window.location.pathname;
        if (basePath && path.startsWith(basePath)) {
          path = path.slice(basePath.length);
        }
        path = path.replace(/^\/+|\/+$/g, '');
        if (!path) return null;

        const parts = path.split('/');
        const targetId = parts[parts.length - 1];
        if (targetId && !['agents', 'dashboards', 'alerts', 'settings', 'management', 'login', 'logout', 'dashboard'].includes(targetId)) {
          return targetId;
        }
        return null;
      }

      function handleRoute() {
        const id = getTargetId();
        onNavigate(id);
      }

      window.addEventListener('popstate', handleRoute);
      window.addEventListener('hashchange', handleRoute);
      handleRoute();
    },

    initHashRouter: function (options) {
      this.initRouter(options);
    },

    // Floating Tooltip for Compact Usage Bars
    initFloatingTooltips: function () {
      let tooltip = document.getElementById('global-floating-tooltip');
      if (!tooltip) {
        tooltip = document.createElement('div');
        tooltip.id = 'global-floating-tooltip';
        document.body.appendChild(tooltip);
      }

      document.addEventListener('mouseover', function (e) {
        const target = e.target.closest('.usage-bar-track');
        if (!target) {
          tooltip.style.display = 'none';
          return;
        }

        const header = target.getAttribute('data-tooltip-header') || 'Metric';
        const rawRows = target.getAttribute('data-tooltip-rows');
        if (!rawRows) return;

        try {
          const rows = JSON.parse(rawRows);
          let html = `<div class="tooltip-header">${header}</div>`;
          rows.forEach(r => {
            html += `
              <div class="tooltip-row">
                <span class="tooltip-label"><span class="tooltip-dot" style="background: ${r.color};"></span>${r.label}</span>
                <strong>${r.val}</strong>
              </div>
            `;
          });
          tooltip.innerHTML = html;
          tooltip.style.display = 'flex';
        } catch (err) {}
      });

      document.addEventListener('mousemove', function (e) {
        if (tooltip.style.display === 'flex') {
          const x = e.clientX + 15;
          const y = e.clientY;
          tooltip.style.left = (x + 200 > window.innerWidth ? e.clientX - 190 : x) + 'px';
          tooltip.style.top = y + 'px';
        }
      });

      document.addEventListener('mouseout', function (e) {
        const target = e.target.closest('.usage-bar-track');
        if (target) {
          tooltip.style.display = 'none';
        }
      });
    },

    openReinstallModal: function (opts) {
      const modal = document.getElementById('reinstall-modal');
      if (!modal) return;

      const agentId = (typeof opts === 'string') ? opts : (opts.agentId || '');
      const nickname = (typeof opts === 'object' && opts.nickname) ? opts.nickname : agentId;

      const titleEl = document.getElementById('reinstall-modal-title');
      if (titleEl) titleEl.textContent = 'Install ' + nickname;

      const container = document.getElementById('reinstall-instructions-container');
      if (container && window.CertainStatsProvisionRenderer) {
        window.CertainStatsProvisionRenderer.loadInstallInstructions(agentId, container);
      }

      Modal.open(modal);
    },

    openUninstallModal: function (opts) {
      const modal = document.getElementById('uninstall-modal');
      if (!modal) return;

      const agentId = (typeof opts === 'string') ? opts : (opts.agentId || '');
      const nickname = (typeof opts === 'object' && opts.nickname) ? opts.nickname : agentId;

      const titleEl = document.getElementById('uninstall-modal-title');
      if (titleEl) titleEl.textContent = 'Uninstall ' + nickname;

      const container = document.getElementById('uninstall-instructions-container');
      if (container && window.CertainStatsProvisionRenderer) {
        window.CertainStatsProvisionRenderer.loadUninstallInstructions(agentId, container);
      }

      Modal.open(modal);
    },

    // Modals are native <dialog class="modal"> elements: Esc and focus trapping come from the browser.
    initModalBackdropHandlers: function () {
      document.addEventListener('click', function (e) {
        const opener = e.target.closest('[data-open-modal]');
        if (opener) {
          e.preventDefault();
          Modal.open(opener.getAttribute('data-open-modal'));
          return;
        }
        const closer = e.target.closest('[data-close-modal]');
        if (closer) {
          e.preventDefault();
          Modal.close(closer.getAttribute('data-close-modal') || closer.closest('dialog, .modal-overlay'));
          return;
        }
        // Backdrop click: the event target is the <dialog> itself, outside its content box.
        if (e.target.tagName === 'DIALOG' && e.target.classList.contains('modal')) {
          const r = e.target.getBoundingClientRect();
          const inside = e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom;
          if (!inside) Modal.close(e.target);
        }
      });
    }
  };

  onReady(function () {
    window.CertainStatsTelemetry.initFloatingTooltips();
    window.CertainStatsTelemetry.initScrollRestoration();
    window.CertainStatsTelemetry.initModalBackdropHandlers();

    let flash = null;
    try {
      flash = sessionStorage.getItem('certainstats_flash');
      sessionStorage.removeItem('certainstats_flash');
    } catch (e) {}
    if (flash) window.CertainStatsTelemetry.showToast(flash, true);

    const urlParams = new URLSearchParams(window.location.search);
    if (urlParams.has('success')) {
      window.CertainStatsTelemetry.showToast(urlParams.get('success'), true);
    } else if (urlParams.has('error')) {
      window.CertainStatsTelemetry.showToast(urlParams.get('error'), false);
    }

    const redirInput = document.getElementById('provision-redirect-to');
    if (redirInput) {
      redirInput.value = window.location.pathname + window.location.search;
    }
  });
})();

