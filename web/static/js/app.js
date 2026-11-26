  // Main controller: scenario loading, SSE stream lifecycle, theme toggle, and HUD events.
import { clearEl, el, safeColor, swatch } from './dom.js';
import { EuropeMap } from './map.js';
import { Hemicycle } from './hemicycle.js';
import { InspectorUI } from './inspector.js';
import { renderLegend } from './overlays.js';
import { ScenarioEditor } from './editor.js';
import { applyScenarioPatch, decodeSharePayload } from './share.js';
import { ChangelogModal } from './changelog.js';
import { ScreenshotModal } from './export.js';

const state = {
  scenarioId: '',
  customRunId: '',
  baseScenarioId: '',
  baseScenario: null,
  scenario: null,
  groupsById: {},
  partiesById: {},
  regionsById: {},
  orderedGroups: [],

  source: null,
  viewMode: 'live', // 'live' | 'forecast'
  forecastRuns: 200,
  forecast: null,
  runPhase: 'ready', // 'ready' | 'running' | 'paused' | 'done'
  durationMs: 45000,
  activeDurationMs: 45000,
  appliedSeed: 42,
  temp: 0.45,
  swing: 0.15,
  tactical: 0,
  shiftParty: '',
  shiftVal: 0,
  wavesOverride: 0,
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
  countedEligible: 0,
  ticker: [],
  selection: null
};

const simClockEl = document.getElementById('sim-clock');
const btnRun = document.getElementById('btn-run');
const btnReset = document.getElementById('btn-reset');
const btnEditor = document.getElementById('btn-editor');
const speedGroup = document.getElementById('speed-group');
const forecastControls = document.getElementById('forecast-controls');
const forecastRunsSelect = document.getElementById('forecast-runs');
const btnRunForecast = document.getElementById('btn-run-forecast');
const inputSwing = document.getElementById('input-swing');
const swingVal = document.getElementById('swing-val');
const shiftPartySelect = document.getElementById('shift-party');
const inputShift = document.getElementById('input-shift');
const shiftValEl = document.getElementById('shift-val');
const inputSeed = document.getElementById('input-seed');
const inputTemp = document.getElementById('input-temp');
const tempVal = document.getElementById('temp-val');
const inputTactical = document.getElementById('input-tactical');
const tacticalVal = document.getElementById('tactical-val');
const methodSelect = document.getElementById('method-select');
const btnVersion = document.getElementById('btn-version');
const battlegroundsEl = document.getElementById('battlegrounds');
const battlegroundsSubEl = document.getElementById('battlegrounds-sub');
const battlegroundsListEl = document.getElementById('battlegrounds-list');
const progressFill = document.getElementById('progress-fill');
const progressLabel = document.getElementById('progress-label');
const partyPickerWrap = document.getElementById('party-picker-wrap');
const colorModeRow = document.getElementById('color-mode-row');
const partySelect = document.getElementById('party-select');
const countrySelect = document.getElementById('country-select');
const scenarioSelect = document.getElementById('scenario-select');
const scenarioSubtitle = document.getElementById('scenario-subtitle');
const scenarioNoteEl = document.getElementById('scenario-note');
const seatLedgerEl = document.getElementById('seat-ledger');
const coalitionListEl = document.getElementById('coalition-list');
const legendEl = document.getElementById('map-legend');
const mapStageEl = document.getElementById('map-stage');
const topLeftHudEl = document.querySelector('.hud-top-left');
const layersHudEl = document.getElementById('layers-hud');
const hemicycleHudEl = document.getElementById('hemicycle-hud');
const hemicycleBodyEl = document.getElementById('hemicycle-body');
const btnToggleHemi = document.getElementById('btn-toggle-hemi');
const btnMobileControls = document.getElementById('btn-mobile-controls');
const btnMobileLayers = document.getElementById('btn-mobile-layers');
const btnMobileBg = document.getElementById('btn-mobile-bg');
const mobileLayersActiveEl = document.getElementById('mobile-layers-active');
const mobileLayersCaretEl = document.getElementById('mobile-layers-caret');

let map;
let hemicycle;
let inspector;
let editor;
let changelog;
let screenshotModal;
let forecastAbortCtrl = null;
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
const mobileMQ = window.matchMedia('(max-width: 820px)');
let desktopHemiCollapsed = true;
let userToggledHemi = false;

function isMobileViewport() {
  return mobileMQ.matches;
}

function syncMobileLayersLabel() {
  if (!mobileLayersActiveEl) return;
  const activeBtn = document.querySelector('.ov-btn.active');
  mobileLayersActiveEl.textContent = activeBtn?.textContent?.trim() || 'Winner';
}

function syncTuneIndicator() {
  if (!btnMobileControls) return;
  const hasOverrides = Boolean(
    (state.shiftParty && Math.abs(state.shiftVal) > 1e-6) ||
    state.methodOverride ||
    Math.abs(state.temp - 0.45) > 1e-3 ||
    Math.abs(state.swing - 0.15) > 1e-3 ||
    state.tactical > 1e-3 ||
    (state.viewMode === 'live' && state.durationMs !== 45000) ||
    (state.viewMode === 'forecast' && state.forecastRuns !== 200) ||
    state.customRunId
  );
  btnMobileControls.classList.toggle('has-overrides', hasOverrides);
}

function setHemicycleCollapsed(collapsed) {
  if (!hemicycleBodyEl || !hemicycleHudEl || !btnToggleHemi) return;
  hemicycleBodyEl.classList.toggle('hidden', collapsed);
  hemicycleHudEl.classList.toggle('is-collapsed', collapsed);
  hemicycleHudEl.classList.toggle('mobile-hemi-open', !collapsed && isMobileViewport());
  btnToggleHemi.textContent = collapsed ? '▴' : '▾';
  btnToggleHemi.setAttribute('aria-expanded', String(!collapsed));
  if (!collapsed && isMobileViewport()) {
    closeMobileDrawers();
  }
}

function maybeAutoExpandDesktopHemicycle() {
  if (isMobileViewport() || userToggledHemi) return;
  desktopHemiCollapsed = false;
  setHemicycleCollapsed(false);
}

function setMobileControlsOpen(open) {
  if (!topLeftHudEl || !btnMobileControls) return;
  topLeftHudEl.classList.toggle('controls-open', open);
  btnMobileControls.setAttribute('aria-expanded', String(open));
  btnMobileControls.textContent = open ? '⚙ ▴' : '⚙ ▾';
  if (open && isMobileViewport()) {
    setMobileLayersOpen(false);
    setMobileBattlegroundsOpen(false);
    setHemicycleCollapsed(true);
  }
}

function setMobileLayersOpen(open) {
  if (!layersHudEl || !btnMobileLayers) return;
  layersHudEl.classList.toggle('layers-open', open);
  btnMobileLayers.setAttribute('aria-expanded', String(open));
  if (mobileLayersCaretEl) mobileLayersCaretEl.textContent = open ? '▴' : '▾';
  if (open && isMobileViewport()) {
    setMobileControlsOpen(false);
    setMobileBattlegroundsOpen(false);
    setHemicycleCollapsed(true);
  }
}

function setMobileBattlegroundsOpen(open) {
  if (!battlegroundsEl || !btnMobileBg) return;
  battlegroundsEl.classList.toggle('mobile-bg-open', open);
  btnMobileBg.setAttribute('aria-expanded', String(open));
  btnMobileBg.textContent = open ? '▴' : '▾';
  if (open) {
    setMobileControlsOpen(false);
    setMobileLayersOpen(false);
    if (isMobileViewport()) setHemicycleCollapsed(true);
  }
}

function closeMobileDrawers() {
  setMobileControlsOpen(false);
  setMobileLayersOpen(false);
  setMobileBattlegroundsOpen(false);
}

function applyScenarioMeta(meta, hasGroups) {
  const title = meta.title || 'European Parliament';

  document.title = `FOES — ${title}`;
  document.getElementById('hud-kicker').textContent = title.toUpperCase();
  document.getElementById('seat-unit-label').textContent = meta.seat_label || 'seats';
  document.getElementById('overview-focus-btn').textContent = meta.overview_label || 'Europe';
  document.getElementById('map-attribution').textContent = meta.attribution || '© EuroGeographics / Eurostat';
  document.getElementById('hemi-tabs-wrap').classList.toggle('hidden', Boolean(meta.hide_coalitions));
  document.getElementById('ov-party-btn').textContent = hasGroups ? 'Group %' : 'Party %';
  document.getElementById('party-select-label').textContent = hasGroups ? 'Group' : 'Party';
  document.getElementById('ov-eu-axis').textContent = meta.axis2_label || 'Pro-EU';
  syncMobileLayersLabel();
}

function syncShiftControls() {
  if (shiftPartySelect) shiftPartySelect.value = state.shiftParty;
  if (inputShift) {
    inputShift.value = String(Math.min(0.40, Math.max(-0.40, state.shiftVal)));
    inputShift.disabled = !state.shiftParty;
  }
  if (shiftValEl) {
    const sign = state.shiftVal > 0 ? '+' : '';
    shiftValEl.textContent = `${sign}${state.shiftVal.toFixed(2)}`;
  }
}

function applyModifiersToHUD(modifiers) {
  if (!modifiers) return;
  const rawUnc = modifiers.uncertainty ?? modifiers.swing;
  state.swing = Number.isFinite(rawUnc) ? Math.min(2, Math.max(0, rawUnc)) : 0.15;
  inputSwing.value = String(Math.min(0.60, Math.max(0, state.swing)));
  swingVal.textContent = state.swing.toFixed(2);
  if ('shiftParty' in modifiers || 'shiftVal' in modifiers) {
    state.shiftParty = typeof modifiers.shiftParty === 'string' ? modifiers.shiftParty : '';
    state.shiftVal = Number.isFinite(modifiers.shiftVal) ? Math.min(2, Math.max(-2, modifiers.shiftVal)) : 0;
    if (!state.shiftParty) state.shiftVal = 0;
    syncShiftControls();
  }
  state.wavesOverride = parseInt(modifiers.wavesOverride, 10) || 0;
  if (Number.isFinite(modifiers.temp) && modifiers.temp > 0) {
    state.temp = Math.min(5, Math.max(0.05, modifiers.temp));
    inputTemp.value = String(Math.min(1.5, Math.max(0.25, state.temp)));
    tempVal.textContent = state.temp.toFixed(2);
  }
  state.tactical = Number.isFinite(modifiers.tactical) && modifiers.tactical >= 0
    ? Math.min(1, modifiers.tactical)
    : 0;
  if (inputTactical) inputTactical.value = String(Math.min(0.8, Math.max(0, state.tactical)));
  if (tacticalVal) tacticalVal.textContent = state.tactical.toFixed(2);
  if (Number.isFinite(modifiers.seed)) {
    inputSeed.value = String(modifiers.seed);
    state.appliedSeed = modifiers.seed;
  }
  syncTuneIndicator();
}

function resetModifiersToDefaults() {
  applyModifiersToHUD({
    swing: 0.15,
    uncertainty: 0.15,
    tactical: 0,
    shiftParty: '',
    shiftVal: 0,
    temp: 0.45,
    seed: 42,
    wavesOverride: 0
  });
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
    onSelect: (type, id) => {
      if (isMobileViewport()) {
        closeMobileDrawers();
        setHemicycleCollapsed(true);
      }
      inspector.select(type, id);
    },
    onClearSelection: () => {
      if (isMobileViewport()) closeMobileDrawers();
      inspector.clear();
    },
    onZoomThresholdChange: () => {
      if (state.overlay === 'winner' && state.colorMode === 'auto') {
        refreshLegend();
        map.recolorAll();
      }
    }
  });

  hemicycle = new Hemicycle(
    document.getElementById('hemicycle-svg'),
    document.getElementById('seat-ledger'),
    document.getElementById('forecast-summary'),
    document.getElementById('vote-totals')
  );

  changelog = new ChangelogModal(document.getElementById('changelog-modal'));
  changelog.init(btnVersion);
  screenshotModal = new ScreenshotModal(document.getElementById('screenshot-modal'));

  editor = new ScenarioEditor(document.getElementById('editor-modal'), {
    onApplied: async (runId, autoStart, modifiers) => {
      applyModifiersToHUD(modifiers);
      if (autoStart && state.viewMode !== 'live') {
        setViewMode('live', { skipReset: true });
      }
      await loadScenario({
        runId,
        autoStart,
        initialMethodOverride: modifiers?.methodOverride || ''
      });
    }
  });

  wireEvents();
  document.fonts?.ready?.then(() => map?.requestRender());
  setHemicycleCollapsed(true);
  syncTuneIndicator();
  mobileMQ.addEventListener('change', (e) => {
    closeMobileDrawers();
    if (e.matches) {
      setHemicycleCollapsed(true);
    } else {
      setHemicycleCollapsed(desktopHemiCollapsed);
    }
  });

  let initialId = '';
  const searchParams = new URLSearchParams(window.location.search);
  const urlScenario = searchParams.get('scenario') || '';
  const urlRun = searchParams.get('run') || '';
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

  state.baseScenarioId = initialId;
  if (window.location.hash.startsWith('#share=')) {
    try {
      const shared = await decodeSharePayload(window.location.hash.slice('#share='.length));
      const baseId = shared.base || initialId;
      const baseQuery = baseId ? `?scenario=${encodeURIComponent(baseId)}` : '';
      const baseRes = await fetch(`/api/scenario${baseQuery}`);
      if (!baseRes.ok) throw new Error(`HTTP ${baseRes.status}`);
      const baseSc = await baseRes.json();
      const patched = applyScenarioPatch(baseSc, shared.patch);
      const runRes = await fetch('/api/runs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patched)
      });
      if (!runRes.ok) throw new Error(`HTTP ${runRes.status}`);
      const { run_id: runId } = await runRes.json();
      applyModifiersToHUD(shared.mods);
      state.baseScenarioId = baseId;
      const ok = await loadScenario({
        scenarioId: baseId,
        runId,
        baseScenarioOverride: baseSc,
        initialMethodOverride: shared.mods?.methodOverride || '',
        suppressError: true
      });
      if (ok) return;
    } catch {
      window.history.replaceState(null, '', window.location.pathname + window.location.search);
    }
  }

  if (urlRun) {
    const ok = await loadScenario({ runId: urlRun, suppressError: true });
    if (ok) return;
  }
  await loadScenario({ scenarioId: initialId });
}

async function loadScenario({
  scenarioId = state.baseScenarioId,
  runId = '',
  autoStart = false,
  suppressError = false,
  initialMethodOverride = '',
  baseScenarioOverride = null
} = {}) {
  const seq = ++scenarioLoadSeq;
  if (forecastAbortCtrl) {
    forecastAbortCtrl.abort();
    forecastAbortCtrl = null;
    btnRunForecast.disabled = false;
    btnRunForecast.textContent = '↻ Re-run';
  }
  state.forecast = null;
  renderBattlegrounds();
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

  const query = runId
    ? `?run=${encodeURIComponent(runId)}`
    : (scenarioId ? `?scenario=${encodeURIComponent(scenarioId)}` : '');
  let loaded;
  try {
    const res = await fetch(`/api/scenario${query}`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    loaded = await res.json();
  } catch (err) {
    if (seq !== scenarioLoadSeq) return false;
    if (!suppressError) {
      scenarioSubtitle.classList.remove('hidden');
      scenarioSubtitle.textContent = `Failed to load scenario (${err.message})`;
    }
    return false;
  }
  if (seq !== scenarioLoadSeq) return false;

  state.customRunId = runId;
  btnEditor.classList.toggle('is-custom', Boolean(runId));
  if (!runId) {
    resetModifiersToDefaults();
  }
  state.methodOverride = initialMethodOverride;
  methodSelect.value = initialMethodOverride;
  if (baseScenarioOverride) {
    state.baseScenario = structuredClone(baseScenarioOverride);
  } else if (!runId) {
    state.scenarioId = scenarioId;
    state.baseScenarioId = scenarioId;
    state.baseScenario = structuredClone(loaded);
  } else if (!state.baseScenario) {
    state.baseScenario = structuredClone(loaded);
  }
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
  const desc = (meta.description || '').trim();
  scenarioNoteEl.textContent = desc;
  scenarioNoteEl.title = desc;
  scenarioNoteEl.classList.toggle('hidden', !desc);
  scenarioSubtitle.textContent = runId ? `Custom · ${loaded.name}` : loaded.name;
  const oldCustomOpt = scenarioSelect.querySelector('option[value="__custom__"]');
  if (oldCustomOpt) oldCustomOpt.remove();
  if (scenarioSelect.options.length > 1) {
    scenarioSubtitle.classList.add('hidden');
    scenarioSelect.classList.remove('hidden');
    if (runId) {
      const customOpt = document.createElement('option');
      customOpt.value = '__custom__';
      customOpt.textContent = `Custom · ${loaded.name}`;
      customOpt.disabled = true;
      scenarioSelect.prepend(customOpt);
      scenarioSelect.value = '__custom__';
    } else if (scenarioId) {
      scenarioSelect.value = scenarioId;
    }
  } else {
    scenarioSubtitle.classList.remove('hidden');
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

  if (shiftPartySelect) {
    const prevShiftParty = state.shiftParty;
    clearEl(shiftPartySelect);
    shiftPartySelect.appendChild(el('option', { value: '', text: 'None' }));
    for (const g of state.orderedGroups) {
      shiftPartySelect.appendChild(el('option', { value: g.id, text: g.short_name }));
    }
    const hasMatch = state.orderedGroups.some((g) => g.id === prevShiftParty);
    state.shiftParty = hasMatch ? prevShiftParty : '';
    if (!hasMatch) state.shiftVal = 0;
    syncShiftControls();
  }
  syncTuneIndicator();

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
    if (!active || seq !== scenarioLoadSeq) return false;
  } catch (err) {
    if (seq !== scenarioLoadSeq) return false;
    scenarioSubtitle.classList.remove('hidden');
    scenarioSubtitle.textContent = `Failed to load map geometry (${err.message})`;
    return false;
  }

  const nextUrl = new URL(window.location.href);
  if (runId) {
    nextUrl.searchParams.set('run', runId);
  } else {
    nextUrl.searchParams.delete('run');
    nextUrl.hash = '';
    if (scenarioId) nextUrl.searchParams.set('scenario', scenarioId);
  }
  window.history.replaceState(null, '', nextUrl);

  refreshLegend();
  resetToReady();
  if (autoStart) startStream();
  else if (state.viewMode === 'forecast') runForecast();
  return true;
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

function computeVoteSummary() {
  const useGroups = Boolean(state.scenario?.groups?.length);
  const groupVotes = {};
  let totalVotes = 0;
  for (const regVotes of Object.values(state.regionVotes)) {
    if (!regVotes) continue;
    for (const [pid, v] of Object.entries(regVotes)) {
      if (v <= 0) continue;
      const gid = useGroups ? (state.partiesById[pid]?.group_id || pid) : pid;
      groupVotes[gid] = (groupVotes[gid] || 0) + v;
      totalVotes += v;
    }
  }
  const countedEligible = Math.round(state.countedEligible || 0);
  const turnoutPct = countedEligible > 0 ? Math.min(100, (totalVotes / countedEligible) * 100) : 0;
  return { totalVotes, countedEligible, turnoutPct, groupVotes };
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
    if (state.viewMode === 'forecast' && state.forecast) {
      hemicycle.renderForecast(state.orderedGroups, state.forecast, highlight);
    } else {
      hemicycle.render(state.orderedGroups, state.euSeats, state.prevEUSeats, highlight, computeVoteSummary());
    }
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
      if (state.viewMode === 'live') {
        hemicycle.updateVoteSummary(state.orderedGroups, computeVoteSummary());
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
  state.countedEligible = 0;
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
  const deltaPct = Math.max(0, curPct - prevPct);
  state.countedEligible += (rep.eligible_voters || 0) * deltaPct;
  state.regionPctSum[rep.region_id] = (state.regionPctSum[rep.region_id] || 0) + deltaPct;
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
  syncRunControls();
}

function resumeStream() {
  if (state.runPhase !== 'paused') return;
  playbackOrigin += performance.now() - pausedAt;
  pausedAt = 0;
  if (queueHead >= eventQueue.length && streamErrorWhilePaused) {
    streamErrorWhilePaused = false;
    state.runPhase = 'ready';
    syncRunControls();
    return;
  }
  state.runPhase = 'running';
  syncRunControls();
  scheduleQueueDrain();
}

function toggleRunPause() {
  if (state.runPhase === 'running') pauseStream();
  else if (state.runPhase === 'paused') resumeStream();
  else startStream();
}

function onSettingChanged(fromSpeed = false) {
  syncTuneIndicator();
  if (state.viewMode === 'forecast') {
    if (!fromSpeed) runForecast();
    return;
  }
  if (state.runPhase === 'running' || state.runPhase === 'done') startStream();
  else if (state.runPhase === 'paused') resetToReady();
}

function setViewMode(mode, { skipReset = false } = {}) {
  const next = mode === 'forecast' ? 'forecast' : 'live';
  if (state.viewMode === next) return;
  state.viewMode = next;

  document.querySelectorAll('.mode-btn').forEach((b) => {
    b.classList.toggle('active', b.dataset.mode === next);
  });

  const isForecast = next === 'forecast';
  btnRun.classList.toggle('hidden', isForecast);
  btnReset.classList.toggle('hidden', isForecast);
  speedGroup.classList.toggle('hidden', isForecast);
  forecastControls.classList.toggle('hidden', !isForecast);

  if (isForecast) {
    syncTuneIndicator();
    maybeAutoExpandDesktopHemicycle();
    runForecast();
  } else {
    if (forecastAbortCtrl) {
      forecastAbortCtrl.abort();
      forecastAbortCtrl = null;
      btnRunForecast.disabled = false;
      btnRunForecast.textContent = '↻ Re-run';
    }
    state.forecast = null;
    renderBattlegrounds();
    refreshLegend();
    if (!skipReset) {
      state.swing = 0.15;
      inputSwing.value = '0.15';
      swingVal.textContent = '0.15';
      resetToReady();
    }
    syncTuneIndicator();
  }
}

function renderBattlegrounds() {
  clearEl(battlegroundsListEl);
  if (state.viewMode !== 'forecast' || !state.forecast || !state.scenario) {
    battlegroundsEl.classList.add('hidden');
    return;
  }

  battlegroundsEl.classList.remove('hidden');
  const races = [];
  for (const r of state.scenario.regions) {
    const rf = state.forecast.regions?.[r.id];
    if (!rf) continue;
    const distEntries = (r.district_seats > 0 && !state.methodOverride && rf.district_win_prob)
      ? Object.entries(rf.district_win_prob)
      : [];
    const allSeatsAreDistricts = distEntries.length > 0 && distEntries.length * r.district_seats >= r.seats;
    if (!allSeatsAreDistricts) {
      races.push({ rid: r.id, pid: '', label: r.name, seats: r.seats, probs: rf.win_prob || {} });
    }
    for (const [did, probs] of distEntries) {
      const unitGeo = state.precinctGeo[did];
      const dLabel = unitGeo?.label || did;
      races.push({ rid: r.id, pid: unitGeo ? did : '', label: `${dLabel} (${r.name})`, seats: r.district_seats, probs });
    }
  }

  const competitive = [];
  for (const race of races) {
    const ranked = Object.entries(race.probs)
      .filter(([, p]) => p > 0)
      .sort((a, b) => b[1] - a[1]);
    if (!ranked.length) continue;
    const leadProb = ranked[0][1];
    if (leadProb >= 0.85) continue;
    competitive.push({ ...race, ranked, leadProb });
  }

  competitive.sort((a, b) => a.leadProb - b.leadProb);
  const top = competitive.slice(0, 7);
  battlegroundsSubEl.textContent = `${competitive.length} <85%`;

  if (!top.length) {
    battlegroundsListEl.appendChild(el('div', {
      className: 'subtitle',
      text: `All races lean safe (>=85%) at Uncertainty ${state.swing.toFixed(2)}. Try raising Uncertainty.`
    }));
    return;
  }

  const seatUnit = state.scenario.meta?.seat_label || 'seats';
  for (const race of top) {
    const p1 = state.partiesById[race.ranked[0][0]];
    const p2 = race.ranked[1] ? state.partiesById[race.ranked[1][0]] : null;
    const pct1 = Math.round(race.ranked[0][1] * 100);
    const pct2 = race.ranked[1] ? Math.round(race.ranked[1][1] * 100) : 0;
    const c1 = safeColor(p1?.color);
    const c2 = safeColor(p2?.color, '#94a3b8');

    const bar = el('div', { className: 'bg-bar' },
      el('div', { className: 'bg-bar-seg', style: { width: `${pct1}%`, background: c1 } }),
      p2 ? el('div', { className: 'bg-bar-seg', style: { width: `${pct2}%`, background: c2 } }) : null
    );

    const oddsTxt = p2
      ? `${p1?.short_name || race.ranked[0][0]} ${pct1}% · ${p2.short_name} ${pct2}%`
      : `${p1?.short_name || race.ranked[0][0]} ${pct1}%`;

    battlegroundsListEl.appendChild(el('button', {
      type: 'button',
      className: 'bg-row',
      onClick: () => {
        if (isMobileViewport()) {
          closeMobileDrawers();
          setHemicycleCollapsed(true);
        }
        focusRegion(race.rid);
        if (race.pid) inspector.select('precinct', race.pid);
        else inspector.select('country', race.rid);
      }
    },
      el('div', { className: 'bg-row-head' },
        el('strong', { text: race.label }),
        el('span', { className: 'mono editor-muted', text: `${race.seats} ${seatUnit}` })
      ),
      el('div', { className: 'bg-row-odds mono', text: oddsTxt }),
      bar
    ));
  }
}

async function rehydrateCustomRun() {
  if (!state.customRunId || !state.scenario) return false;
  try {
    const res = await fetch('/api/runs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(state.scenario)
    });
    if (!res.ok) return false;
    const data = await res.json();
    if (!data?.run_id) return false;
    state.customRunId = data.run_id;
    const nextUrl = new URL(window.location.href);
    nextUrl.searchParams.set('run', data.run_id);
    window.history.replaceState(null, '', nextUrl);
    return true;
  } catch {
    return false;
  }
}

async function runForecast(retried = false) {
  if (!state.scenario) return;
  const reqSeq = scenarioLoadSeq;
  if (forecastAbortCtrl) forecastAbortCtrl.abort();
  const ctrl = new AbortController();
  forecastAbortCtrl = ctrl;

  if (state.source) {
    state.source.close();
    state.source = null;
  }
  clearQueue();
  state.runPhase = 'ready';
  syncRunControls();
  if (!state.forecast) {
    resetStreamState();
    renderBattlegrounds();
  }
  btnRunForecast.disabled = true;
  btnRunForecast.textContent = 'Running…';
  refreshLegend();

  const parsedSeed = parseInt(inputSeed.value, 10);
  const seed = Number.isNaN(parsedSeed) ? 42 : parsedSeed;
  const temp = Number.isFinite(state.temp) && state.temp > 0 ? state.temp : 0.45;
  const swing = Number.isFinite(state.swing) && state.swing >= 0 ? Math.min(2, state.swing) : 0.15;
  const targetParam = state.customRunId
    ? `&run=${encodeURIComponent(state.customRunId)}`
    : (state.scenarioId ? `&scenario=${encodeURIComponent(state.scenarioId)}` : '');
  const methodParam = state.methodOverride ? `&method=${encodeURIComponent(state.methodOverride)}` : '';
  const tacticalParam = state.tactical > 0 ? `&tactical=${state.tactical}` : '';
  const shiftParam = (state.shiftParty && state.shiftVal !== 0)
    ? `&shift=${encodeURIComponent(state.shiftParty)}:${state.shiftVal}`
    : '';
  const url = `/api/forecast?runs=${state.forecastRuns}&seed=${seed}&temp=${temp}&uncertainty=${swing}${tacticalParam}${shiftParam}${targetParam}${methodParam}`;

  try {
    const res = await fetch(url, { signal: ctrl.signal });
    if (res.status === 404 && state.customRunId && retried !== true) {
      const recovered = await rehydrateCustomRun();
      if (recovered && reqSeq === scenarioLoadSeq && state.viewMode === 'forecast') {
        return runForecast(true);
      }
    }
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const fc = await res.json();
    if (reqSeq !== scenarioLoadSeq || state.viewMode !== 'forecast' || forecastAbortCtrl !== ctrl) return;
    forecastAbortCtrl = null;
    state.forecast = fc;
    btnRunForecast.disabled = false;
    btnRunForecast.textContent = '↻ Re-run';
    refreshLegend();
    map.recolorAll();
    scheduleHemicycleRender();
    renderBattlegrounds();
    inspector.render();
    map.refreshHoverTooltip();
  } catch (err) {
    if (err.name === 'AbortError' || reqSeq !== scenarioLoadSeq || state.viewMode !== 'forecast') {
      if (forecastAbortCtrl === ctrl) {
        btnRunForecast.disabled = false;
        btnRunForecast.textContent = '↻ Re-run';
      }
      return;
    }
    btnRunForecast.disabled = false;
    btnRunForecast.textContent = '↻ Re-run';
  }
}

function startStream(retried = false) {
  maybeAutoExpandDesktopHemicycle();
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
  const temp = Number.isFinite(state.temp) && state.temp > 0 ? state.temp : 0.45;
  const swing = Number.isFinite(state.swing) && state.swing >= 0 ? Math.min(2, state.swing) : 0.15;
  const waves = state.wavesOverride >= 1 && state.wavesOverride <= 4
    ? state.wavesOverride
    : (state.durationMs > 0 ? 3 : 1);
  const targetParam = state.customRunId
    ? `&run=${encodeURIComponent(state.customRunId)}`
    : (state.scenarioId ? `&scenario=${encodeURIComponent(state.scenarioId)}` : '');
  const methodParam = state.methodOverride ? `&method=${encodeURIComponent(state.methodOverride)}` : '';
  const tacticalParam = state.tactical > 0 ? `&tactical=${state.tactical}` : '';
  const shiftParam = (state.shiftParty && state.shiftVal !== 0)
    ? `&shift=${encodeURIComponent(state.shiftParty)}:${state.shiftVal}`
    : '';
  const url = `/api/stream?duration_ms=${state.durationMs}&waves=${waves}&seed=${seed}&temp=${temp}&uncertainty=${swing}${tacticalParam}${shiftParam}${targetParam}${methodParam}`;

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

  es.onerror = async () => {
    if (state.source !== es) return;
    es.close();
    state.source = null;
    if (
      state.customRunId &&
      retried !== true &&
      state.runPhase === 'running' &&
      state.reportedPrecincts === 0 &&
      Object.keys(state.euSeats).length === 0
    ) {
      const seq = scenarioLoadSeq;
      const recovered = await rehydrateCustomRun();
      if (recovered && seq === scenarioLoadSeq && state.viewMode === 'live' && state.runPhase === 'running') {
        startStream(true);
        return;
      }
    }
    if (state.runPhase === 'running' && queueHead >= eventQueue.length) {
      state.runPhase = 'ready';
      syncRunControls();
    } else if (state.runPhase !== 'done') {
      streamErrorWhilePaused = true;
    }
  };
}

function openScenarioEditor() {
  if (!state.scenario) return;
  const parsedSeed = parseInt(inputSeed.value, 10);
  editor.open(
    state.baseScenario || state.scenario,
    state.scenario,
    {
      swing: state.swing,
      uncertainty: state.swing,
      tactical: state.tactical,
      shiftParty: state.shiftParty,
      shiftVal: state.shiftVal,
      temp: state.temp,
      seed: Number.isNaN(parsedSeed) ? 42 : parsedSeed,
      wavesOverride: state.wavesOverride,
      methodOverride: state.methodOverride
    },
    { baseScenarioId: state.baseScenarioId }
  );
}

function wireEvents() {
  document.querySelectorAll('.mode-btn').forEach((btn) => {
    btn.addEventListener('click', () => setViewMode(btn.dataset.mode));
  });
  forecastRunsSelect.addEventListener('change', () => {
    state.forecastRuns = parseInt(forecastRunsSelect.value, 10) || 200;
    syncTuneIndicator();
    if (state.viewMode === 'forecast') runForecast();
  });
  inputSwing.addEventListener('input', () => {
    state.swing = parseFloat(inputSwing.value) || 0;
    swingVal.textContent = state.swing.toFixed(2);
  });
  inputSwing.addEventListener('change', () => onSettingChanged());
  if (shiftPartySelect && inputShift) {
    shiftPartySelect.addEventListener('change', () => {
      const hadShift = state.shiftVal !== 0;
      state.shiftParty = shiftPartySelect.value;
      if (!state.shiftParty) state.shiftVal = 0;
      syncShiftControls();
      if (state.shiftVal !== 0 || hadShift) onSettingChanged();
    });
    inputShift.addEventListener('input', () => {
      state.shiftVal = parseFloat(inputShift.value) || 0;
      if (shiftValEl) {
        const sign = state.shiftVal > 0 ? '+' : '';
        shiftValEl.textContent = `${sign}${state.shiftVal.toFixed(2)}`;
      }
    });
    inputShift.addEventListener('change', () => {
      if (state.shiftParty) onSettingChanged();
    });
  }
  btnRunForecast.addEventListener('click', () => runForecast());
  btnVersion.addEventListener('click', () => changelog?.open());

  btnRun.addEventListener('click', toggleRunPause);
  btnReset.addEventListener('click', resetToReady);
  btnEditor.addEventListener('click', () => {
    if (isMobileViewport()) closeMobileDrawers();
    openScenarioEditor();
  });
  scenarioSelect.addEventListener('change', () => {
    if (scenarioSelect.value === '__custom__') return;
    loadScenario({ scenarioId: scenarioSelect.value });
  });

  document.getElementById('btn-screenshot')?.addEventListener('click', () => {
    if (isMobileViewport()) closeMobileDrawers();
    screenshotModal?.open(map, state);
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
      onSettingChanged(true);
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
    state.temp = parseFloat(inputTemp.value) || 0.45;
    tempVal.textContent = state.temp.toFixed(2);
  });
  inputTemp.addEventListener('change', onSettingChanged);

  if (inputTactical && tacticalVal) {
    inputTactical.addEventListener('input', () => {
      state.tactical = parseFloat(inputTactical.value) || 0;
      tacticalVal.textContent = state.tactical.toFixed(2);
    });
    inputTactical.addEventListener('change', () => onSettingChanged());
  }

  methodSelect.addEventListener('change', () => {
    state.methodOverride = methodSelect.value;
    onSettingChanged();
  });

  document.getElementById('focus-pills').addEventListener('click', (e) => {
    const btn = e.target.closest('.focus-btn');
    if (!btn) return;
    if (isMobileViewport()) closeMobileDrawers();
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
      syncMobileLayersLabel();
      refreshLegend();
      map.recolorAll();
    });
  });

  btnMobileControls?.addEventListener('click', () => {
    const isOpen = topLeftHudEl?.classList.contains('controls-open');
    setMobileControlsOpen(!isOpen);
  });

  btnMobileLayers?.addEventListener('click', () => {
    const isOpen = layersHudEl?.classList.contains('layers-open');
    setMobileLayersOpen(!isOpen);
  });

  battlegroundsEl?.querySelector('.battlegrounds-head')?.addEventListener('click', () => {
    if (!isMobileViewport()) return;
    const isOpen = battlegroundsEl.classList.contains('mobile-bg-open');
    setMobileBattlegroundsOpen(!isOpen);
  });

  if (typeof ResizeObserver !== 'undefined' && topLeftHudEl && mapStageEl) {
    const syncMobileTopHud = () => {
      if (topLeftHudEl.classList.contains('controls-open')) return;
      const h = Math.ceil(topLeftHudEl.getBoundingClientRect().height);
      if (h > 0) mapStageEl.style.setProperty('--mobile-top-hud', `${h}px`);
    };
    const ro = new ResizeObserver(syncMobileTopHud);
    ro.observe(topLeftHudEl);
    syncMobileTopHud();
  }

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
    if (e.key === 'Escape') {
      if (screenshotModal?.isOpen) {
        screenshotModal.close();
        return;
      }
      if (changelog?.isOpen) {
        changelog.close();
        return;
      }
      if (editor?.isOpen) {
        editor.close();
        return;
      }
      if (state.selection) {
        inspector.clear();
        return;
      }
      if (topLeftHudEl?.classList.contains('controls-open')) {
        setMobileControlsOpen(false);
        return;
      }
      if (layersHudEl?.classList.contains('layers-open')) {
        setMobileLayersOpen(false);
        return;
      }
      if (battlegroundsEl?.classList.contains('mobile-bg-open')) {
        setMobileBattlegroundsOpen(false);
        return;
      }
      if (isMobileViewport() && hemicycleHudEl?.classList.contains('mobile-hemi-open')) {
        setHemicycleCollapsed(true);
        return;
      }
    }
    if ((e.key === 'e' || e.key === 'E') && !e.ctrlKey && !e.metaKey && !e.altKey) {
      if (screenshotModal?.isOpen || changelog?.isOpen || editor?.isOpen || !state.scenario) return;
      const tag = document.activeElement?.tagName;
      if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA' || tag === 'BUTTON' || tag === 'A') return;
      e.preventDefault();
      openScenarioEditor();
      return;
    }
    if ((e.key === 's' || e.key === 'S') && !e.ctrlKey && !e.metaKey && !e.altKey) {
      if (screenshotModal?.isOpen || changelog?.isOpen || editor?.isOpen || !state.scenario) return;
      const tag = document.activeElement?.tagName;
      if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA' || tag === 'BUTTON' || tag === 'A') return;
      e.preventDefault();
      screenshotModal?.open(map, state);
      return;
    }
    if (e.code === 'Space' && !e.ctrlKey && !e.metaKey && !e.altKey) {
      if (screenshotModal?.isOpen || changelog?.isOpen || editor?.isOpen) return;
      if (document.activeElement?.closest('input, select, textarea, button, a, [role="button"]')) return;
      if (!state.scenario) return;
      e.preventDefault();
      if (state.viewMode === 'forecast') runForecast();
      else toggleRunPause();
    }
  });

  countrySelect.addEventListener('change', () => {
    const rid = countrySelect.value;
    if (isMobileViewport()) closeMobileDrawers();
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

  btnToggleHemi?.addEventListener('click', (e) => {
    e.stopPropagation();
    const collapsed = !hemicycleBodyEl.classList.contains('hidden');
    if (!isMobileViewport()) {
      desktopHemiCollapsed = collapsed;
      userToggledHemi = true;
    }
    setHemicycleCollapsed(collapsed);
  });

  hemicycleHudEl?.querySelector('.hud-header')?.addEventListener('click', (e) => {
    if (e.target.closest('button')) return;
    const collapsed = !hemicycleBodyEl.classList.contains('hidden');
    if (!isMobileViewport()) {
      desktopHemiCollapsed = collapsed;
      userToggledHemi = true;
    }
    setHemicycleCollapsed(collapsed);
  });
}

init();
