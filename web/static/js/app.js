// Main controller: scenario loading, SSE stream lifecycle, theme toggle, and HUD events.
import { clearEl, el, swatch } from './dom.js';
import { EuropeMap } from './map.js';
import { Hemicycle } from './hemicycle.js';
import { InspectorUI } from './inspector.js';
import { renderLegend } from './overlays.js';

const state = {
  scenarioId: '',
  scenario: null,
  groupsById: {},
  partiesById: {},
  regionsById: {},
  orderedGroups: [],

  source: null,
  runPhase: 'ready', // 'ready' | 'running' | 'paused' | 'done'
  durationMs: 45000,
  activeDurationMs: 45000,
  appliedSeed: 42,
  methodOverride: '',
  overlay: 'winner',
  colorMode: 'auto',
  heatmapPartyId: '',
  zoomedIn: false,

  prevEUSeats: {},
  euSeats: {},
  coalitions: [],
  selectedCoalitionKey: null,
  streamStartTime: 0,
  hemiTab: 'seats',
  regionSeats: {},
  regionVotes: {},
  districtWinners: {},
  regionReportedCount: {},
  regionTotalCount: {},
  regionPct: {},
  precincts: {},
  precinctGeo: {},
  reportedPrecincts: 0,
  totalPrecincts: 0,
  ticker: [],
  selection: null
};

const statusBadge = document.getElementById('status-badge');
const statusText = document.getElementById('status-text');
const simClockEl = document.getElementById('sim-clock');
const btnRun = document.getElementById('btn-run');
const btnReset = document.getElementById('btn-reset');
const inputSeed = document.getElementById('input-seed');
const inputTemp = document.getElementById('input-temp');
const tempVal = document.getElementById('temp-val');
const methodSelect = document.getElementById('method-select');
const progressFill = document.getElementById('progress-fill');
const progressLabel = document.getElementById('progress-label');
const partyPickerWrap = document.getElementById('party-picker-wrap');
const colorModeRow = document.getElementById('color-mode-row');
const partySelect = document.getElementById('party-select');
const countrySelect = document.getElementById('country-select');
const scenarioSelect = document.getElementById('scenario-select');
const scenarioSubtitle = document.getElementById('scenario-subtitle');
const seatLedgerEl = document.getElementById('seat-ledger');
const coalitionListEl = document.getElementById('coalition-list');
const legendEl = document.getElementById('map-legend');

let map;
let hemicycle;
let inspector;
let hemiFrameQueued = false;
let hudFrameId = 0;
let pendingTickerRep = null;
let inspectorDirty = false;
let lastSlowHudMs = 0;
let scenarioLoadSeq = 0;
let streamWallStart = 0;
let playbackOrigin = 0;
let pausedAt = 0;
let eventQueue = [];
let queueHead = 0;
let drainRafId = 0;
let streamErrorWhilePaused = false;

function applyScenarioMeta(meta, hasGroups) {
  const title = meta.title || 'European Parliament';
  const initials = title
    .replace(/[^A-Za-z0-9 ]/g, ' ')
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0].toUpperCase())
    .join('') || 'PS';

  document.title = `ParliamentSim — ${title}`;
  document.getElementById('brand-mark').textContent = initials;
  document.getElementById('hud-kicker').textContent = title.toUpperCase();
  document.getElementById('seat-unit-label').textContent = meta.seat_label || 'seats';
  document.getElementById('overview-focus-btn').textContent = meta.overview_label || 'Europe';
  document.getElementById('map-attribution').textContent = meta.attribution || '© EuroGeographics / Eurostat';
  document.getElementById('hemi-tabs-wrap').classList.toggle('hidden', Boolean(meta.hide_coalitions));
  document.getElementById('ov-party-btn').textContent = hasGroups ? 'Group %' : 'Party %';
  document.getElementById('party-select-label').textContent = hasGroups ? 'Group' : 'Party';
  document.getElementById('ov-eu-axis').textContent = meta.axis2_label || 'Pro-EU';
}

async function init() {
  const savedTheme = localStorage.getItem('ps-theme') || 'light';
  document.documentElement.dataset.theme = savedTheme;
  state.isDark = savedTheme === 'dark';

  inspector = new InspectorUI(state, {
    onFocusRegion: (rid) => focusRegion(rid),
    onSelectionChange: () => map?.requestRender()
  });

  map = new EuropeMap(document.getElementById('map-canvas'), state, {
    onHoverStart: (e, rid, pid) => inspector.showTooltip(e, rid, pid),
    onHoverCallout: (e, spec) => inspector.showCalloutTooltip(e, spec),
    onHoverMove: (e) => inspector.moveTooltip(e),
    onHoverEnd: () => inspector.hideTooltip(),
    onSelect: (type, id) => inspector.select(type, id),
    onClearSelection: () => inspector.clear(),
    onZoomThresholdChange: () => {
      if (state.overlay === 'winner' && state.colorMode === 'auto') {
        refreshLegend();
        map.recolorAll();
      }
    }
  });

  hemicycle = new Hemicycle(
    document.getElementById('hemicycle-svg'),
    document.getElementById('seat-ledger')
  );

  wireEvents();
  if (window.innerWidth <= 820) {
    document.getElementById('hemicycle-body').classList.add('hidden');
    const toggleBtn = document.getElementById('btn-toggle-hemi');
    toggleBtn.textContent = '▴';
    toggleBtn.setAttribute('aria-expanded', 'false');
  }

  let initialId = '';
  const urlScenario = new URLSearchParams(window.location.search).get('scenario') || '';
  try {
    const listRes = await fetch('/api/scenarios');
    if (listRes.ok) {
      const catalog = await listRes.json();
      const items = catalog.scenarios || [];
      if (items.length > 1) {
        clearEl(scenarioSelect);
        for (const item of items) {
          const opt = document.createElement('option');
          opt.value = item.id;
          opt.textContent = `${item.name} (${item.seats} ${item.seat_label})`;
          scenarioSelect.appendChild(opt);
        }
        scenarioSubtitle.classList.add('hidden');
        scenarioSelect.classList.remove('hidden');
      }
      const validIds = new Set(items.map((item) => item.id));
      initialId = validIds.has(urlScenario) ? urlScenario : (catalog.default_id || items[0]?.id || '');
    }
  } catch {
    // Fallback to single-scenario /api/scenario when /api/scenarios is unavailable.
  }

  await loadScenario(initialId);
}

async function loadScenario(scenarioId) {
  const seq = ++scenarioLoadSeq;
  if (state.source) {
    state.source.close();
    state.source = null;
  }
  if (hudFrameId) {
    cancelAnimationFrame(hudFrameId);
    hudFrameId = 0;
  }
  clearQueue();
  streamErrorWhilePaused = false;
  state.runPhase = 'ready';
  syncRunControls();
  pendingTickerRep = null;
  inspectorDirty = false;
  inspector.hideTooltip();
  inspector.clear();

  state.methodOverride = '';
  methodSelect.value = '';
  setStatus('live', 'LOADING');

  const query = scenarioId ? `?scenario=${encodeURIComponent(scenarioId)}` : '';
  let loaded;
  try {
    const res = await fetch(`/api/scenario${query}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    loaded = await res.json();
  } catch (err) {
    if (seq !== scenarioLoadSeq) return;
    scenarioSubtitle.classList.remove('hidden');
    scenarioSubtitle.textContent = `Failed to load scenario (${err.message})`;
    setStatus('idle', 'ERROR');
    return;
  }
  if (seq !== scenarioLoadSeq) return;

  state.scenarioId = scenarioId;
  state.scenario = loaded;
  state.groupsById = {};
  state.partiesById = {};
  state.regionsById = {};
  state.orderedGroups = [];
  state.precinctGeo = {};
  state.regionTotalCount = {};
  state.prevEUSeats = {};

  const meta = loaded.meta || {};
  const hasGroups = Boolean(loaded.groups?.length);
  scenarioSubtitle.textContent = loaded.name;
  if (scenarioSelect.options.length > 1) {
    scenarioSubtitle.classList.add('hidden');
    scenarioSelect.classList.remove('hidden');
    if (scenarioId) scenarioSelect.value = scenarioId;
  }
  applyScenarioMeta(meta, hasGroups);

  if (meta.hide_coalitions && state.hemiTab === 'coalitions') {
    state.hemiTab = 'seats';
    document.querySelectorAll('.htab-btn').forEach((b) => {
      b.classList.toggle('active', b.dataset.htab === 'seats');
    });
    seatLedgerEl.classList.remove('hidden');
    coalitionListEl.classList.add('hidden');
    state.selectedCoalitionKey = null;
  }

  const groups = hasGroups ? loaded.groups : (loaded.parties || []);
  for (const g of groups) {
    state.groupsById[g.id] = g;
    if (!hasGroups) state.partiesById[g.id] = g;
  }

  state.orderedGroups = [...groups].sort((a, b) => {
    if (a.id === 'ni') return 1;
    if (b.id === 'ni') return -1;
    return (a.position[0] * 0.65 + a.position[1] * 0.35) - (b.position[0] * 0.65 + b.position[1] * 0.35);
  });

  clearEl(partySelect);
  for (const g of state.orderedGroups) {
    const opt = document.createElement('option');
    opt.value = g.id;
    opt.textContent = `${g.short_name} — ${g.name}`;
    partySelect.appendChild(opt);
  }
  state.heatmapPartyId = state.orderedGroups[Math.floor(state.orderedGroups.length / 2)]?.id || groups[0]?.id || '';
  partySelect.value = state.heatmapPartyId;

  for (const p of (loaded.parties || [])) state.partiesById[p.id] = p;

  const focusPills = document.getElementById('focus-pills');
  document.querySelectorAll('#focus-pills .focus-btn:not(#overview-focus-btn)').forEach((b) => b.remove());
  document.getElementById('overview-focus-btn').classList.add('active');
  clearEl(countrySelect);
  countrySelect.classList.add('hidden');

  const useDropdown = loaded.regions.length > 6;
  const regionLabel = (meta.region_label || 'state').toLowerCase();
  if (useDropdown) {
    countrySelect.classList.remove('hidden');
    const defOpt = document.createElement('option');
    defOpt.value = 'eu';
    defOpt.textContent = `Jump to ${regionLabel} (${loaded.regions.length})…`;
    countrySelect.appendChild(defOpt);
  }

  let totalSeats = 0;
  for (const r of loaded.regions) {
    state.regionsById[r.id] = r;
    for (const p of (r.parties || [])) state.partiesById[p.id] = p;
    totalSeats += r.seats;

    if (useDropdown) {
      const opt = document.createElement('option');
      opt.value = r.id;
      opt.textContent = `${r.name} (${r.seats})`;
      countrySelect.appendChild(opt);
    } else {
      const btn = document.createElement('button');
      btn.className = 'focus-btn';
      btn.dataset.focus = r.id;
      btn.textContent = r.name;
      focusPills.appendChild(btn);
    }
  }

  document.getElementById('hemicycle-total').textContent = totalSeats;
  document.getElementById('hemicycle-maj').textContent = Math.floor(totalSeats / 2) + 1;
  hemicycle.build(totalSeats, meta.chart_type || 'hemicycle', meta.seat_label || 'seats');

  try {
    const active = await map.loadAndRender();
    if (!active || seq !== scenarioLoadSeq) return;
  } catch (err) {
    if (seq !== scenarioLoadSeq) return;
    scenarioSubtitle.classList.remove('hidden');
    scenarioSubtitle.textContent = `Failed to load map geometry (${err.message})`;
    setStatus('idle', 'ERROR');
    return;
  }

  if (scenarioId) {
    const nextUrl = new URL(window.location.href);
    nextUrl.searchParams.set('scenario', scenarioId);
    window.history.replaceState(null, '', nextUrl);
  }

  refreshLegend();
  resetToReady();
}

function focusRegion(rid) {
  document.querySelectorAll('.focus-btn').forEach((b) => {
    b.classList.toggle('active', b.dataset.focus === rid);
  });
  if (!countrySelect.classList.contains('hidden')) {
    countrySelect.value = rid;
  }
  map.focusRegion(rid);
}

function refreshLegend() {
  renderLegend(state, legendEl, partyPickerWrap, colorModeRow);
}

function coalitionKey(c) {
  return c.group_ids.slice().sort().join('+');
}

function renderCoalitions() {
  clearEl(coalitionListEl);
  if (!state.coalitions.length) {
    coalitionListEl.appendChild(el('div', { className: 'subtitle', text: 'No minimal-winning coalition found yet.' }));
    return;
  }
  state.coalitions.forEach((c) => {
    const key = coalitionKey(c);
    const partnersEl = el('div', { className: 'coalition-partners' });
    c.group_ids.forEach((gid, i) => {
      if (i > 0) partnersEl.appendChild(el('span', { text: '+' }));
      const col = state.groupsById[gid]?.color || '#64748b';
      partnersEl.appendChild(
        el('span', { className: 'coalition-partner' }, swatch(col), c.names[i])
      );
    });
    const margin = c.seats - c.majority;
    const metaEl = el(
      'div',
      { className: 'coalition-meta' },
      el('strong', { text: c.seats }),
      ` (+${margin}) · ${c.cohesion}%`
    );
    const row = el(
      'button',
      {
        type: 'button',
        className: `coalition-row${state.selectedCoalitionKey === key ? ' active' : ''}`,
        onClick: () => {
          state.selectedCoalitionKey = state.selectedCoalitionKey === key ? null : key;
          renderCoalitions();
          scheduleHemicycleRender();
        }
      },
      partnersEl,
      metaEl
    );
    coalitionListEl.appendChild(row);
  });
}

function scheduleHemicycleRender() {
  if (hemiFrameQueued) return;
  hemiFrameQueued = true;
  requestAnimationFrame(() => {
    hemiFrameQueued = false;
    const activeCoalition = state.hemiTab === 'coalitions' && state.selectedCoalitionKey
      ? state.coalitions.find((c) => coalitionKey(c) === state.selectedCoalitionKey)
      : null;
    const highlight = activeCoalition ? new Set(activeCoalition.group_ids) : null;
    hemicycle.render(state.orderedGroups, state.euSeats, state.prevEUSeats, highlight);
    if (state.hemiTab === 'coalitions') renderCoalitions();
  });
}

function updateClock(pct, finished = false) {
  const meta = state.scenario?.meta || {};
  const startH = meta.clock_start_hour || 18.0;
  const endH = meta.clock_end_hour || 25.5;
  const tz = meta.timezone || 'CET';
  const totalMins = Math.max(60, (endH - startH) * 60);
  const pollCloseMins = totalMins * (2 / 3);
  const wrapMins = totalMins - pollCloseMins;

  let elapsedMins = 0;
  if (state.runPhase === 'ready') {
    elapsedMins = 0;
  } else if (finished || pct >= 100) {
    elapsedMins = totalMins;
  } else if (state.activeDurationMs > 0 && playbackOrigin > 0) {
    const refNow = pausedAt || performance.now();
    const f = Math.min(1, Math.max(0, (refNow - playbackOrigin) / state.activeDurationMs));
    elapsedMins = f <= 0.30 ? (f / 0.30) * pollCloseMins : pollCloseMins + ((f - 0.30) / 0.70) * wrapMins;
  } else {
    elapsedMins = (Math.min(100, Math.max(0, pct)) / 100) * totalMins;
  }

  const totalStartMins = Math.round(startH * 60 + elapsedMins);
  const hh = Math.floor(totalStartMins / 60) % 24;
  const mm = String(totalStartMins % 60).padStart(2, '0');
  simClockEl.textContent = `${String(hh).padStart(2, '0')}:${mm} ${tz}`;
}

function setStatus(kind, label) {
  statusBadge.className = `status-badge status-${kind}`;
  statusText.textContent = label;
}

function syncRunControls() {
  if (state.runPhase === 'running') {
    btnRun.textContent = '⏸ Pause';
  } else if (state.runPhase === 'paused') {
    btnRun.textContent = '▶ Resume';
  } else if (state.runPhase === 'done') {
    btnRun.textContent = '▶ Replay';
  } else {
    btnRun.textContent = '▶ Start';
  }
  btnReset.disabled = state.runPhase === 'ready';
}

function updateProgress() {
  const pct = state.countedPct ?? (state.totalPrecincts ? (state.reportedPrecincts / state.totalPrecincts) * 100 : 0);
  progressFill.style.width = `${Math.min(100, pct).toFixed(1)}%`;
  progressLabel.textContent = `${Math.round(pct)}% · ${state.reportedPrecincts}/${state.totalPrecincts}`;
  updateClock(pct);
}

function scheduleHudFlush() {
  if (hudFrameId) return;
  hudFrameId = requestAnimationFrame((now) => {
    hudFrameId = 0;
    if (state.runPhase !== 'running') return;
    setStatus('live', `LIVE · ${Math.round(state.countedPct || 0)}%`);
    updateProgress();

    if (now - lastSlowHudMs >= 120) {
      lastSlowHudMs = now;
      if (pendingTickerRep) {
        inspector.pushTicker(pendingTickerRep);
        pendingTickerRep = null;
      }
      if (inspectorDirty) {
        inspector.render();
        inspectorDirty = false;
      }
      map?.refreshHoverTooltip();
    }
  });
}

function resetStreamState() {
  state.prevEUSeats = {};
  state.euSeats = {};
  state.coalitions = [];
  state.selectedCoalitionKey = null;
  state.regionSeats = {};
  state.regionVotes = {};
  state.districtWinners = {};
  state.regionReportedCount = {};
  state.regionPctSum = {};
  state.regionPct = {};
  state.precincts = {};
  state.reportedPrecincts = 0;
  state.countedPct = 0;
  state.ticker = [];
  if (hudFrameId) {
    cancelAnimationFrame(hudFrameId);
    hudFrameId = 0;
  }
  pendingTickerRep = null;
  inspectorDirty = false;
  lastSlowHudMs = 0;
  clearEl(document.getElementById('ticker'));
  map.resetFills();

  let totalP = 0;
  for (const r of state.scenario.regions) {
    state.regionReportedCount[r.id] = 0;
    state.regionPct[r.id] = 0;
    totalP += state.regionTotalCount[r.id] || r.precincts || 1;
  }
  state.totalPrecincts = totalP;
  updateProgress();
  scheduleHemicycleRender();
  inspector.render();
}

function applyExitPoll(data) {
  state.prevEUSeats = data.prev_eu_seats || {};
  state.euSeats = data.eu_seats || {};
  state.coalitions = data.coalitions || [];
  state.regionSeats = data.region_seats || {};
  state.districtWinners = data.district_winners || {};
  state.totalPrecincts = data.total_precincts || state.totalPrecincts;

  updateProgress();
  scheduleHemicycleRender();
  map.recolorAll();
  inspector.render();
}

function applyPrecinct(data) {
  const rep = data.report;
  const existing = state.precincts[rep.precinct_id];
  const prevPct = existing?.reported_pct || 0;
  const curPct = rep.reported_pct > 0 ? rep.reported_pct : 1.0;

  if (!existing) {
    state.precincts[rep.precinct_id] = {
      ...rep,
      reported_pct: curPct,
      votes: { ...rep.votes }
    };
  } else {
    existing.reported_pct = Math.max(existing.reported_pct, curPct);
    for (const [pid, v] of Object.entries(rep.votes || {})) {
      existing.votes[pid] = (existing.votes[pid] || 0) + v;
    }
  }

  state.regionVotes[rep.region_id] = data.region_votes;
  state.regionSeats[rep.region_id] = data.region_seats;
  if (data.district_winners) {
    state.districtWinners[rep.region_id] = data.district_winners;
  }
  if (data.eu_seats) {
    state.euSeats = data.eu_seats;
    state.coalitions = data.coalitions || [];
    scheduleHemicycleRender();
  }
  state.reportedPrecincts = data.reported_precincts;
  state.countedPct = data.counted_pct ?? (state.totalPrecincts > 0 ? (state.reportedPrecincts / state.totalPrecincts) * 100 : 0);

  if (prevPct < 1.0 && curPct >= 1.0) {
    state.regionReportedCount[rep.region_id] = (state.regionReportedCount[rep.region_id] || 0) + 1;
  }

  const reg = state.regionsById[rep.region_id];
  const totalCount = state.regionTotalCount[rep.region_id] || reg?.precincts || 1;
  state.regionPctSum[rep.region_id] = (state.regionPctSum[rep.region_id] || 0) + Math.max(0, curPct - prevPct);
  state.regionPct[rep.region_id] = Math.min(100, Math.round((state.regionPctSum[rep.region_id] / Math.max(1, totalCount)) * 100));

  map.queueRecolor(rep.precinct_id);
  pendingTickerRep = state.precincts[rep.precinct_id];
  if (state.selection && (state.selection.id === rep.precinct_id || state.selection.id === rep.region_id)) {
    inspectorDirty = true;
  }
  scheduleHudFlush();
}

function applyComplete() {
  state.runPhase = 'done';
  if (drainRafId) {
    cancelAnimationFrame(drainRafId);
    drainRafId = 0;
  }
  if (hudFrameId) {
    cancelAnimationFrame(hudFrameId);
    hudFrameId = 0;
  }
  for (const r of state.scenario.regions) {
    state.regionPct[r.id] = 100;
  }
  if (pendingTickerRep) {
    inspector.pushTicker(pendingTickerRep);
    pendingTickerRep = null;
  }
  inspectorDirty = false;
  setStatus('done', 'FINAL');
  syncRunControls();
  updateProgress();
  updateClock(100, true);
  map.flushRecolor();
  map.refreshHoverTooltip();
  scheduleHemicycleRender();
  inspector.render();
}

function clearQueue() {
  if (drainRafId) {
    cancelAnimationFrame(drainRafId);
    drainRafId = 0;
  }
  eventQueue = [];
  queueHead = 0;
}

function scheduleQueueDrain() {
  if (drainRafId || state.runPhase !== 'running' || queueHead >= eventQueue.length) return;
  drainRafId = requestAnimationFrame((now) => {
    drainRafId = 0;
    drainQueue(now);
  });
}

function drainQueue(now) {
  if (state.runPhase !== 'running') return;
  const elapsed = now - playbackOrigin;
  while (queueHead < eventQueue.length) {
    const item = eventQueue[queueHead];
    if (state.activeDurationMs > 0 && item.offsetMs > elapsed) break;
    queueHead++;
    if (item.kind === 'exit_poll') {
      applyExitPoll(item.data);
    } else if (item.kind === 'precinct') {
      applyPrecinct(item.data);
    } else if (item.kind === 'complete') {
      applyComplete();
      break;
    }
  }
  if (queueHead >= eventQueue.length) {
    eventQueue = [];
    queueHead = 0;
    if (streamErrorWhilePaused && state.runPhase === 'running') {
      streamErrorWhilePaused = false;
      state.runPhase = 'ready';
      setStatus('idle', 'DISCONNECTED');
      syncRunControls();
    }
  } else if (state.runPhase === 'running') {
    scheduleQueueDrain();
  }
}

function enqueueOrApply(kind, data) {
  if (state.runPhase === 'running' && queueHead >= eventQueue.length) {
    if (kind === 'exit_poll') applyExitPoll(data);
    else if (kind === 'precinct') applyPrecinct(data);
    else if (kind === 'complete') applyComplete();
    return;
  }
  eventQueue.push({
    kind,
    data,
    offsetMs: Math.max(0, performance.now() - streamWallStart)
  });
}

function resetToReady() {
  if (state.source) {
    state.source.close();
    state.source = null;
  }
  clearQueue();
  streamErrorWhilePaused = false;
  streamWallStart = 0;
  playbackOrigin = 0;
  pausedAt = 0;
  state.runPhase = 'ready';
  resetStreamState();
  setStatus('idle', 'READY');
  syncRunControls();
}

function pauseStream() {
  if (state.runPhase !== 'running') return;
  state.runPhase = 'paused';
  pausedAt = performance.now();
  if (drainRafId) {
    cancelAnimationFrame(drainRafId);
    drainRafId = 0;
  }
  if (hudFrameId) {
    cancelAnimationFrame(hudFrameId);
    hudFrameId = 0;
  }
  setStatus('paused', `PAUSED · ${Math.round(state.countedPct || 0)}%`);
  syncRunControls();
}

function resumeStream() {
  if (state.runPhase !== 'paused') return;
  playbackOrigin += performance.now() - pausedAt;
  pausedAt = 0;
  if (queueHead >= eventQueue.length && streamErrorWhilePaused) {
    streamErrorWhilePaused = false;
    state.runPhase = 'ready';
    setStatus('idle', 'DISCONNECTED');
    syncRunControls();
    return;
  }
  state.runPhase = 'running';
  setStatus('live', `LIVE · ${Math.round(state.countedPct || 0)}%`);
  syncRunControls();
  scheduleQueueDrain();
}

function toggleRunPause() {
  if (state.runPhase === 'running') pauseStream();
  else if (state.runPhase === 'paused') resumeStream();
  else startStream();
}

function onSettingChanged() {
  if (state.runPhase === 'running' || state.runPhase === 'done') startStream();
  else if (state.runPhase === 'paused') resetToReady();
}

function startStream() {
  if (state.source) {
    state.source.close();
    state.source = null;
  }
  clearQueue();
  streamErrorWhilePaused = false;
  state.activeDurationMs = state.durationMs;
  streamWallStart = performance.now();
  playbackOrigin = streamWallStart;
  pausedAt = 0;
  state.runPhase = 'running';
  resetStreamState();

  const parsedSeed = parseInt(inputSeed.value, 10);
  const seed = Number.isNaN(parsedSeed) ? 42 : parsedSeed;
  state.appliedSeed = seed;
  const parsedTemp = parseFloat(inputTemp.value);
  const temp = Number.isNaN(parsedTemp) || parsedTemp <= 0 ? 0.45 : parsedTemp;
  const waves = state.durationMs > 0 ? 3 : 1;
  const scenarioParam = state.scenarioId ? `&scenario=${encodeURIComponent(state.scenarioId)}` : '';
  const methodParam = state.methodOverride ? `&method=${encodeURIComponent(state.methodOverride)}` : '';
  const url = `/api/stream?duration_ms=${state.durationMs}&waves=${waves}&seed=${seed}&temp=${temp}&swing=0.15${scenarioParam}${methodParam}`;

  setStatus('live', 'EXIT POLLS');
  syncRunControls();

  const es = new EventSource(url);
  state.source = es;

  es.addEventListener('exit_poll', (ev) => {
    if (state.source !== es) return;
    enqueueOrApply('exit_poll', JSON.parse(ev.data));
  });

  es.addEventListener('precinct', (ev) => {
    if (state.source !== es) return;
    enqueueOrApply('precinct', JSON.parse(ev.data));
  });

  es.addEventListener('complete', () => {
    if (state.source !== es) return;
    es.close();
    state.source = null;
    enqueueOrApply('complete', null);
  });

  es.onerror = () => {
    if (state.source === es) {
      es.close();
      state.source = null;
      if (state.runPhase === 'running' && queueHead >= eventQueue.length) {
        state.runPhase = 'ready';
        setStatus('idle', 'DISCONNECTED');
        syncRunControls();
      } else if (state.runPhase !== 'done') {
        streamErrorWhilePaused = true;
      }
    }
  };
}

function wireEvents() {
  btnRun.addEventListener('click', toggleRunPause);
  btnReset.addEventListener('click', resetToReady);
  scenarioSelect.addEventListener('change', () => {
    loadScenario(scenarioSelect.value);
  });

  document.getElementById('btn-theme').addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    state.isDark = next === 'dark';
    localStorage.setItem('ps-theme', next);
    map.refreshTheme();
    refreshLegend();
    map.recolorAll();
    scheduleHemicycleRender();
  });

  document.querySelectorAll('.seg-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.seg-btn').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
      state.durationMs = parseInt(btn.dataset.duration, 10);
      onSettingChanged();
    });
  });

  document.querySelectorAll('.cm-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.cm-btn').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
      state.colorMode = btn.dataset.colormode;
      refreshLegend();
      map.recolorAll();
    });
  });

  document.getElementById('btn-rand-seed').addEventListener('click', () => {
    inputSeed.value = Math.floor(Math.random() * 9999) + 1;
    onSettingChanged();
  });

  inputSeed.addEventListener('change', () => {
    const parsedSeed = parseInt(inputSeed.value, 10);
    const seed = Number.isNaN(parsedSeed) ? 42 : parsedSeed;
    if (seed === state.appliedSeed && state.runPhase !== 'ready') return;
    onSettingChanged();
  });
  inputSeed.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      state.appliedSeed = Number.isNaN(parseInt(inputSeed.value, 10)) ? 42 : parseInt(inputSeed.value, 10);
      onSettingChanged();
    }
  });

  inputTemp.addEventListener('input', () => {
    tempVal.textContent = parseFloat(inputTemp.value).toFixed(2);
  });
  inputTemp.addEventListener('change', onSettingChanged);

  methodSelect.addEventListener('change', () => {
    state.methodOverride = methodSelect.value;
    onSettingChanged();
  });

  document.getElementById('focus-pills').addEventListener('click', (e) => {
    const btn = e.target.closest('.focus-btn');
    if (!btn) return;
    focusRegion(btn.dataset.focus);
    if (btn.dataset.focus !== 'eu') inspector.select('country', btn.dataset.focus);
    else inspector.clear();
  });

  document.querySelectorAll('.ov-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.ov-btn').forEach((b) => {
        b.classList.remove('active');
        b.setAttribute('aria-pressed', 'false');
      });
      btn.classList.add('active');
      btn.setAttribute('aria-pressed', 'true');
      state.overlay = btn.dataset.overlay;
      refreshLegend();
      map.recolorAll();
    });
  });

  partySelect.addEventListener('change', () => {
    state.heatmapPartyId = partySelect.value;
    refreshLegend();
    map.recolorAll();
  });

  document.getElementById('btn-zoom-in').addEventListener('click', () => map.zoomBy(0.75));
  document.getElementById('btn-zoom-out').addEventListener('click', () => map.zoomBy(1.33));
  document.getElementById('btn-zoom-reset').addEventListener('click', () => focusRegion('eu'));
  document.getElementById('btn-close-inspector').addEventListener('click', () => inspector.clear());

  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && state.selection) {
      inspector.clear();
      return;
    }
    if (e.code === 'Space' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const tag = document.activeElement?.tagName;
      if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA' || tag === 'BUTTON' || tag === 'A') return;
      if (!state.scenario) return;
      e.preventDefault();
      toggleRunPause();
    }
  });

  countrySelect.addEventListener('change', () => {
    const rid = countrySelect.value;
    focusRegion(rid);
    if (rid !== 'eu') inspector.select('country', rid);
    else inspector.clear();
  });

  document.querySelectorAll('.htab-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.htab-btn').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
      state.hemiTab = btn.dataset.htab;
      const showCoalitions = state.hemiTab === 'coalitions';
      seatLedgerEl.classList.toggle('hidden', showCoalitions);
      coalitionListEl.classList.toggle('hidden', !showCoalitions);
      if (!showCoalitions) state.selectedCoalitionKey = null;
      scheduleHemicycleRender();
    });
  });

  const hemiBody = document.getElementById('hemicycle-body');
  const btnToggleHemi = document.getElementById('btn-toggle-hemi');
  btnToggleHemi.addEventListener('click', () => {
    const collapsed = hemiBody.classList.toggle('hidden');
    btnToggleHemi.textContent = collapsed ? '▴' : '▾';
    btnToggleHemi.setAttribute('aria-expanded', String(!collapsed));
  });
}

init();
