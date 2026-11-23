// Main controller: scenario loading, SSE stream lifecycle, theme toggle, and HUD events.
import { EuropeMap } from './map.js';
import { Hemicycle } from './hemicycle.js';
import { InspectorUI } from './inspector.js';
import { renderLegend } from './overlays.js';

const state = {
  scenario: null,
  groupsById: {},
  partiesById: {},
  regionsById: {},
  orderedGroups: [],

  source: null,
  durationMs: 45000,
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
  regionReportedCount: {},
  regionTotalCount: {},
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
const inputSeed = document.getElementById('input-seed');
const inputTemp = document.getElementById('input-temp');
const tempVal = document.getElementById('temp-val');
const progressFill = document.getElementById('progress-fill');
const progressLabel = document.getElementById('progress-label');
const partyPickerWrap = document.getElementById('party-picker-wrap');
const colorModeRow = document.getElementById('color-mode-row');
const partySelect = document.getElementById('party-select');
const countrySelect = document.getElementById('country-select');
const seatLedgerEl = document.getElementById('seat-ledger');
const coalitionListEl = document.getElementById('coalition-list');
const legendEl = document.getElementById('map-legend');

let map;
let hemicycle;
let inspector;
let hemiFrameQueued = false;

async function init() {
  const savedTheme = localStorage.getItem('ps-theme') || 'light';
  document.documentElement.dataset.theme = savedTheme;

  try {
    const res = await fetch('/api/scenario');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    state.scenario = await res.json();
  } catch (err) {
    document.getElementById('scenario-subtitle').textContent = `Failed to load scenario (${err.message})`;
    setStatus('idle', 'ERROR');
    return;
  }
  document.getElementById('scenario-subtitle').textContent = state.scenario.name;

  const groups = (state.scenario.groups?.length) ? state.scenario.groups : (state.scenario.parties || []);
  for (const g of groups) {
    state.groupsById[g.id] = g;
    if (!state.scenario.groups?.length) state.partiesById[g.id] = g;
  }

  state.orderedGroups = [...groups].sort((a, b) => {
    if (a.id === 'ni') return 1;
    if (b.id === 'ni') return -1;
    return (a.position[0] * 0.65 + a.position[1] * 0.35) - (b.position[0] * 0.65 + b.position[1] * 0.35);
  });

  for (const g of state.orderedGroups) {
    const opt = document.createElement('option');
    opt.value = g.id;
    opt.textContent = `${g.short_name} — ${g.name}`;
    partySelect.appendChild(opt);
  }
  state.heatmapPartyId = state.orderedGroups[Math.floor(state.orderedGroups.length / 2)]?.id || groups[0].id;
  partySelect.value = state.heatmapPartyId;

  for (const p of (state.scenario.parties || [])) state.partiesById[p.id] = p;

  const focusPills = document.getElementById('focus-pills');
  const useDropdown = state.scenario.regions.length > 6;
  if (useDropdown) {
    countrySelect.classList.remove('hidden');
    const defOpt = document.createElement('option');
    defOpt.value = 'eu';
    defOpt.textContent = `Jump to state (${state.scenario.regions.length})…`;
    countrySelect.appendChild(defOpt);
  }

  let totalSeats = 0;
  for (const r of state.scenario.regions) {
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

  inspector = new InspectorUI(state, {
    onFocusRegion: (rid) => focusRegion(rid)
  });

  map = new EuropeMap(document.getElementById('europe-map'), state, {
    onHoverStart: (e, rid, pid) => inspector.showTooltip(e, rid, pid),
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
  hemicycle.build(totalSeats);

  try {
    await map.loadAndRender();
  } catch (err) {
    document.getElementById('scenario-subtitle').textContent = `Failed to load map geometry (${err.message})`;
    setStatus('idle', 'ERROR');
    return;
  }
  wireEvents();
  refreshLegend();
  startStream();
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
  coalitionListEl.innerHTML = '';
  if (!state.coalitions.length) {
    coalitionListEl.innerHTML = '<div class="subtitle">No minimal-winning coalition found yet.</div>';
    return;
  }
  state.coalitions.forEach((c) => {
    const key = coalitionKey(c);
    const row = document.createElement('div');
    row.className = `coalition-row${state.selectedCoalitionKey === key ? ' active' : ''}`;
    const partnersHtml = c.group_ids.map((gid, i) => {
      const col = state.groupsById[gid]?.color || '#64748b';
      return `<span class="coalition-partner"><span class="swatch" style="background:${col}"></span>${c.names[i]}</span>`;
    }).join('<span>+</span>');
    const margin = c.seats - c.majority;
    row.innerHTML = `
      <div class="coalition-partners">${partnersHtml}</div>
      <div class="coalition-meta"><strong>${c.seats}</strong> (+${margin}) · ${c.cohesion}%</div>
    `;
    row.onclick = () => {
      state.selectedCoalitionKey = state.selectedCoalitionKey === key ? null : key;
      renderCoalitions();
      scheduleHemicycleRender();
    };
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
  // Align clock with staggered poll-closing window: first 30% of duration spans 18:00 -> 23:00 CET,
  // remaining 70% spans 23:00 -> 01:30 CET as final counties wrap up.
  let minsFrom18 = 0;
  if (finished || pct >= 100) {
    minsFrom18 = 450; // 01:30 CET
  } else if (state.durationMs > 0 && state.streamStartTime > 0) {
    const f = Math.min(1, Math.max(0, (performance.now() - state.streamStartTime) / state.durationMs));
    minsFrom18 = f <= 0.30 ? (f / 0.30) * 300 : 300 + ((f - 0.30) / 0.70) * 150;
  } else {
    minsFrom18 = (Math.min(100, Math.max(0, pct)) / 100) * 450;
  }
  const rounded = Math.round(minsFrom18);
  const hh = (18 + Math.floor(rounded / 60)) % 24;
  const mm = String(rounded % 60).padStart(2, '0');
  simClockEl.textContent = `${String(hh).padStart(2, '0')}:${mm} CET`;
}

function setStatus(kind, label) {
  statusBadge.className = `status-badge status-${kind}`;
  statusText.textContent = label;
}

function updateProgress() {
  const pct = state.countedPct ?? (state.totalPrecincts ? (state.reportedPrecincts / state.totalPrecincts) * 100 : 0);
  progressFill.style.width = `${Math.min(100, pct).toFixed(1)}%`;
  progressLabel.textContent = `${Math.round(pct)}% · ${state.reportedPrecincts}/${state.totalPrecincts}`;
  updateClock(pct);
}

function resetStreamState() {
  state.euSeats = {};
  state.coalitions = [];
  state.selectedCoalitionKey = null;
  state.streamStartTime = performance.now();
  state.regionSeats = {};
  state.regionVotes = {};
  state.regionReportedCount = {};
  state.regionPctSum = {};
  state.precincts = {};
  state.reportedPrecincts = 0;
  state.countedPct = 0;
  state.ticker = [];
  document.getElementById('ticker').innerHTML = '';
  map.resetFills();

  for (const r of state.scenario.regions) {
    state.regionReportedCount[r.id] = 0;
    const sub = document.getElementById(`label-sub-${r.id}`);
    if (sub) sub.textContent = `0%`;
  }
  updateProgress();
}

function startStream() {
  if (state.source) {
    state.source.close();
    state.source = null;
  }
  resetStreamState();

  const parsedSeed = parseInt(inputSeed.value, 10);
  const seed = Number.isNaN(parsedSeed) ? 42 : parsedSeed;
  const parsedTemp = parseFloat(inputTemp.value);
  const temp = Number.isNaN(parsedTemp) || parsedTemp <= 0 ? 0.85 : parsedTemp;
  const waves = state.durationMs > 0 ? 3 : 1;
  const url = `/api/stream?duration_ms=${state.durationMs}&waves=${waves}&seed=${seed}&temp=${temp}&swing=0.15`;

  setStatus('live', 'EXIT POLLS');
  btnRun.textContent = 'Restart';

  const es = new EventSource(url);
  state.source = es;

  es.addEventListener('exit_poll', (ev) => {
    const data = JSON.parse(ev.data);
    state.prevEUSeats = data.prev_eu_seats || {};
    state.euSeats = data.eu_seats || {};
    state.coalitions = data.coalitions || [];
    state.regionSeats = data.region_seats || {};
    state.totalPrecincts = data.total_precincts || 0;

    updateProgress();
    scheduleHemicycleRender();
    map.recolorAll();
    inspector.render();
  });

  es.addEventListener('precinct', (ev) => {
    const data = JSON.parse(ev.data);
    const rep = data.report;

    // Accumulate partial reporting waves into the county's running tally.
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
    state.euSeats = data.eu_seats;
    if (data.coalitions) state.coalitions = data.coalitions;
    state.reportedPrecincts = data.reported_precincts;
    state.countedPct = data.counted_pct ?? (state.totalPrecincts > 0 ? (state.reportedPrecincts / state.totalPrecincts) * 100 : 0);

    if (prevPct < 1.0 && curPct >= 1.0) {
      state.regionReportedCount[rep.region_id] = (state.regionReportedCount[rep.region_id] || 0) + 1;
    }

    const reg = state.regionsById[rep.region_id];
    const totalCount = state.regionTotalCount[rep.region_id] || reg.precincts;
    const sub = document.getElementById(`label-sub-${rep.region_id}`);
    if (sub) {
      // Track running per-region percentage sum cleanly in O(1)
      state.regionPctSum = state.regionPctSum || {};
      state.regionPctSum[rep.region_id] = (state.regionPctSum[rep.region_id] || 0) + Math.max(0, curPct - prevPct);
      const pct = Math.min(100, Math.round((state.regionPctSum[rep.region_id] / Math.max(1, totalCount)) * 100));
      sub.textContent = `${pct}%`;
    }

    setStatus('live', `LIVE · ${Math.round(state.countedPct)}%`);
    updateProgress();
    map.queueRecolor(rep.precinct_id);
    scheduleHemicycleRender();

    // Update live ticker smoothly (every report at normal speed, sampled at Instant)
    if (state.durationMs > 0 || state.reportedPrecincts % 15 === 0) {
      inspector.pushTicker(state.precincts[rep.precinct_id]);
    }

    if (state.selection && (state.selection.id === rep.precinct_id || state.selection.id === rep.region_id)) {
      inspector.render();
    }
  });

  es.addEventListener('complete', () => {
    es.close();
    state.source = null;
    setStatus('done', 'FINAL');
    updateClock(100, true);
    map.flushRecolor();
    scheduleHemicycleRender();
    inspector.render();
  });

  es.onerror = () => {
    if (state.source === es) {
      es.close();
      state.source = null;
    }
  };
}

function wireEvents() {
  btnRun.addEventListener('click', startStream);

  document.getElementById('btn-theme').addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    localStorage.setItem('ps-theme', next);
    refreshLegend();
    map.recolorAll();
  });

  document.querySelectorAll('.seg-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
      document.querySelectorAll('.seg-btn').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
      state.durationMs = parseInt(btn.dataset.duration, 10);
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
    startStream();
  });

  inputTemp.addEventListener('input', () => {
    tempVal.textContent = parseFloat(inputTemp.value).toFixed(2);
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
      document.querySelectorAll('.ov-btn').forEach((b) => b.classList.remove('active'));
      btn.classList.add('active');
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
  });
}

init();
