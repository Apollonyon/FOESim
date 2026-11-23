(() => {
  const SVG_NS = 'http://www.w3.org/2000/svg';

  const state = {
    scenario: null,
    partiesById: {},
    regionsById: {},
    orderedParties: [],

    // Live election data
    source: null,
    streaming: false,
    delayMs: 140,
    overlay: 'winner',
    heatmapPartyId: '',

    prevEUSeats: {},
    euSeats: {},
    regionSeats: {},
    regionVotes: {},
    regionReportedCount: {},
    precincts: {},        // precinct_id -> PrecinctReport
    precinctGeo: {},      // precinct_id -> { label, x, y, regionId }
    reportedPrecincts: 0,
    totalPrecincts: 0,
    ticker: [],

    // Selection & map camera
    selection: null, // { type: 'country' | 'precinct', id }
    viewBox: [...MapGeo.DEFAULT_VIEWBOX],
    animFrame: null,
    dragging: false,
    dragStart: null,
    didDrag: false
  };

  // DOM refs
  const svg = document.getElementById('europe-map');
  const tooltip = document.getElementById('map-tooltip');
  const statusBadge = document.getElementById('status-badge');
  const statusText = document.getElementById('status-text');
  const btnRun = document.getElementById('btn-run');
  const inputSeed = document.getElementById('input-seed');
  const inputTemp = document.getElementById('input-temp');
  const tempVal = document.getElementById('temp-val');
  const progressFill = document.getElementById('progress-fill');
  const progressLabel = document.getElementById('progress-label');
  const partyPickerWrap = document.getElementById('party-picker-wrap');
  const partySelect = document.getElementById('party-select');
  const legendEl = document.getElementById('map-legend');
  const inspectorEl = document.getElementById('inspector');
  const tickerEl = document.getElementById('ticker');

  function svgEl(tag, attrs = {}) {
    const el = document.createElementNS(SVG_NS, tag);
    for (const [k, v] of Object.entries(attrs)) {
      el.setAttribute(k, v);
    }
    return el;
  }

  // ---------- Boot ----------
  async function init() {
    const res = await fetch('/api/scenario');
    state.scenario = await res.json();

    document.getElementById('scenario-subtitle').textContent = state.scenario.name;

    // Order parties left-to-right for hemicycle (by Econ + Social average).
    state.orderedParties = [...state.scenario.parties].sort((a, b) => {
      const scoreA = a.position[0] * 0.65 + a.position[1] * 0.35;
      const scoreB = b.position[0] * 0.65 + b.position[1] * 0.35;
      return scoreA - scoreB;
    });

    for (const p of state.scenario.parties) {
      state.partiesById[p.id] = p;
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = `${p.short_name} — ${p.name}`;
      partySelect.appendChild(opt);
    }
    state.heatmapPartyId = state.orderedParties[Math.floor(state.orderedParties.length / 2)]?.id || state.scenario.parties[0].id;
    partySelect.value = state.heatmapPartyId;

    const focusPills = document.getElementById('focus-pills');
    let totalSeats = 0;
    for (const r of state.scenario.regions) {
      state.regionsById[r.id] = r;
      totalSeats += r.seats;
      state.totalPrecincts += r.precincts;

      const btn = document.createElement('button');
      btn.className = 'focus-btn';
      btn.dataset.focus = r.id;
      btn.textContent = r.name;
      focusPills.appendChild(btn);
    }

    document.getElementById('hemicycle-total').textContent = totalSeats;
    document.getElementById('hemicycle-maj').textContent = Math.floor(totalSeats / 2) + 1;
    progressLabel.textContent = `0 / ${state.totalPrecincts} precincts`;

    buildMap();
    buildHemicycle(totalSeats);
    wireEvents();
    renderLegend();
    startStream();
  }

  // ---------- Build Cartographic Map ----------
  function buildMap() {
    const defs = document.getElementById('map-defs');
    const gratLayer = document.getElementById('layer-graticule');
    const ctxLayer = document.getElementById('layer-context');
    const countriesLayer = document.getElementById('layer-countries');
    const precinctsLayer = document.getElementById('layer-precincts');
    const bordersLayer = document.getElementById('layer-borders');
    const labelsLayer = document.getElementById('layer-labels');

    // Subtle lat/lon graticule grid
    for (let lon = -10; lon <= 26; lon += 4) {
      const [x1, y1] = MapGeo.project(lon, 60);
      const [x2, y2] = MapGeo.project(lon, 35);
      gratLayer.appendChild(svgEl('line', { x1, y1, x2, y2, class: 'graticule-line' }));
    }
    for (let lat = 36; lat <= 60; lat += 4) {
      const [x1, y1] = MapGeo.project(-11, lat);
      const [x2, y2] = MapGeo.project(26, lat);
      gratLayer.appendChild(svgEl('line', { x1, y1, x2, y2, class: 'graticule-line' }));
    }

    // Surrounding European countries
    for (const rings of MapGeo.CONTEXT_COUNTRIES) {
      ctxLayer.appendChild(svgEl('path', {
        d: MapGeo.multiRingToPath(rings),
        class: 'ctx-country'
      }));
    }

    // Active simulated countries + clipped Voronoi precincts
    for (const r of state.scenario.regions) {
      const geo = MapGeo.REGIONS[r.id];
      if (!geo) continue;

      const countryD = MapGeo.multiRingToPath(geo.rings);

      const clip = svgEl('clipPath', { id: `clip-${r.id}` });
      clip.appendChild(svgEl('path', { d: countryD }));
      defs.appendChild(clip);

      // Base country fill (visible when zoomed out or unreported)
      const basePath = svgEl('path', {
        id: `country-${r.id}`,
        d: countryD,
        class: 'sim-country-base'
      });
      basePath.addEventListener('click', () => {
        if (state.didDrag) return;
        selectEntity('country', r.id);
        focusRegion(r.id);
      });
      countriesLayer.appendChild(basePath);

      // Clipped Voronoi precinct polygons
      const pGroup = svgEl('g', { 'clip-path': `url(#clip-${r.id})`, id: `precinct-group-${r.id}` });
      const cells = MapGeo.buildPrecinctPolygons(r.id, r.precincts);
      for (const cell of cells) {
        state.precinctGeo[cell.id] = { label: cell.label, x: cell.x, y: cell.y, regionId: r.id };
        const pPath = svgEl('path', {
          id: `precinct-${cell.id}`,
          d: cell.d,
          class: 'precinct-cell'
        });
        pPath.addEventListener('mouseenter', (e) => showTooltip(e, r.id, cell.id));
        pPath.addEventListener('mousemove', moveTooltip);
        pPath.addEventListener('mouseleave', hideTooltip);
        pPath.addEventListener('click', (e) => {
          e.stopPropagation();
          if (state.didDrag) return;
          // If still zoomed out at Europe level, first click zooms into the country + selects precinct
          if (state.viewBox[2] > 520) {
            focusRegion(r.id);
          }
          selectEntity('precinct', cell.id);
        });
        pGroup.appendChild(pPath);
      }
      precinctsLayer.appendChild(pGroup);

      // Crisp national border overlay
      bordersLayer.appendChild(svgEl('path', {
        id: `border-${r.id}`,
        d: countryD,
        class: 'sim-country-border'
      }));

      // Floating country label + live reported %
      const [lx, ly] = MapGeo.project(geo.labelPos[0], geo.labelPos[1]);
      const title = svgEl('text', { x: lx, y: ly, class: 'country-label' });
      title.textContent = r.name.toUpperCase();
      labelsLayer.appendChild(title);

      const sub = svgEl('text', { id: `label-sub-${r.id}`, x: lx, y: ly + 12, class: 'country-sublabel' });
      sub.textContent = `0/${r.precincts} · ${r.seats} SEATS`;
      labelsLayer.appendChild(sub);
    }

    state.viewBox = fitToViewport(MapGeo.DEFAULT_VIEWBOX);
    applyViewBox();
  }

  // ---------- Pan & Zoom ----------
  function fitToViewport(box) {
    const rect = svg.getBoundingClientRect();
    if (!rect.width || !rect.height) return [...box];
    const aspect = rect.width / rect.height;
    let [x, y, w, h] = box;
    if (w / h > aspect) {
      const nh = w / aspect;
      y -= (nh - h) / 2;
      h = nh;
    } else {
      const nw = h * aspect;
      x -= (nw - w) / 2;
      w = nw;
    }
    return [x, y, w, h];
  }

  function applyViewBox() {
    svg.setAttribute('viewBox', state.viewBox.map((n) => n.toFixed(1)).join(' '));
  }

  function animateViewBox(target) {
    if (state.animFrame) cancelAnimationFrame(state.animFrame);
    const fitted = fitToViewport(target);
    const start = [...state.viewBox];
    const duration = 420;
    const t0 = performance.now();

    function step(now) {
      const p = Math.min(1, (now - t0) / duration);
      const ease = 1 - Math.pow(1 - p, 3);
      for (let i = 0; i < 4; i++) {
        state.viewBox[i] = start[i] + (fitted[i] - start[i]) * ease;
      }
      applyViewBox();
      if (p < 1) state.animFrame = requestAnimationFrame(step);
    }
    state.animFrame = requestAnimationFrame(step);
  }

  function focusRegion(regionId) {
    document.querySelectorAll('.focus-btn').forEach((b) => {
      b.classList.toggle('active', b.dataset.focus === regionId);
    });
    if (regionId === 'eu') {
      animateViewBox(MapGeo.DEFAULT_VIEWBOX);
      return;
    }
    const geo = MapGeo.REGIONS[regionId];
    if (geo) animateViewBox(geo.focusBox);
  }

  function zoomBy(factor, clientX, clientY) {
    const rect = svg.getBoundingClientRect();
    const rx = clientX != null ? (clientX - rect.left) / rect.width : 0.5;
    const ry = clientY != null ? (clientY - rect.top) / rect.height : 0.5;

    const [vx, vy, vw, vh] = state.viewBox;
    const nw = Math.max(120, Math.min(1600, vw * factor));
    const nh = (nw / vw) * vh;
    const nx = vx + (vw - nw) * rx;
    const ny = vy + (vh - nh) * ry;

    state.viewBox = [nx, ny, nw, nh];
    applyViewBox();
  }

  // ---------- SSE Live Stream ----------
  function resetState() {
    state.euSeats = {};
    state.regionSeats = {};
    state.regionVotes = {};
    state.regionReportedCount = {};
    state.precincts = {};
    state.reportedPrecincts = 0;
    state.ticker = [];
    tickerEl.innerHTML = '';

    for (const r of state.scenario.regions) {
      state.regionReportedCount[r.id] = 0;
      const sub = document.getElementById(`label-sub-${r.id}`);
      if (sub) sub.textContent = `0/${r.precincts} · ${r.seats} SEATS`;
    }

    document.querySelectorAll('.precinct-cell').forEach((el) => {
      el.style.fill = '';
      el.classList.remove('flash');
    });

    updateProgress();
  }

  function startStream() {
    if (state.source) {
      state.source.close();
      state.source = null;
    }
    resetState();

    const seed = parseInt(inputSeed.value, 10) || 42;
    const temp = parseFloat(inputTemp.value) || 0.85;
    const url = `/api/stream?delay_ms=${state.delayMs}&seed=${seed}&temp=${temp}&swing=0.15`;

    setStatus('live', 'EXIT POLLS');
    btnRun.textContent = 'Restart';
    state.streaming = true;

    const es = new EventSource(url);
    state.source = es;

    es.addEventListener('exit_poll', (ev) => {
      const data = JSON.parse(ev.data);
      state.prevEUSeats = data.prev_eu_seats || {};
      state.euSeats = data.eu_seats || {};
      state.regionSeats = data.region_seats || {};
      state.totalPrecincts = data.total_precincts || state.totalPrecincts;

      renderHemicycle();
      recolorMap();
      renderInspector();
    });

    es.addEventListener('precinct', (ev) => {
      const data = JSON.parse(ev.data);
      const rep = data.report;

      state.precincts[rep.precinct_id] = rep;
      state.regionVotes[rep.region_id] = data.region_votes;
      state.regionSeats[rep.region_id] = data.region_seats;
      state.euSeats = data.eu_seats;
      state.reportedPrecincts = data.reported_precincts;
      state.regionReportedCount[rep.region_id] = (state.regionReportedCount[rep.region_id] || 0) + 1;

      const reg = state.regionsById[rep.region_id];
      const sub = document.getElementById(`label-sub-${rep.region_id}`);
      if (sub && reg) {
        const pct = Math.round((state.regionReportedCount[rep.region_id] / reg.precincts) * 100);
        sub.textContent = `${pct}% IN · ${reg.seats} SEATS`;
      }

      setStatus('live', `LIVE · ${Math.round((state.reportedPrecincts / state.totalPrecincts) * 100)}%`);
      updateProgress();
      colorPrecinctCell(rep.precinct_id, true);
      renderHemicycle();
      pushTicker(rep);

      if (state.selection && (state.selection.id === rep.precinct_id || state.selection.id === rep.region_id)) {
        renderInspector();
      }
    });

    es.addEventListener('complete', () => {
      es.close();
      state.source = null;
      state.streaming = false;
      setStatus('done', 'FINAL RESULTS');
      renderInspector();
    });

    es.onerror = () => {
      if (state.source === es) {
        es.close();
        state.source = null;
        state.streaming = false;
      }
    };
  }

  function setStatus(kind, label) {
    statusBadge.className = `status-badge status-${kind}`;
    statusText.textContent = label;
  }

  function updateProgress() {
    const pct = state.totalPrecincts ? (state.reportedPrecincts / state.totalPrecincts) * 100 : 0;
    progressFill.style.width = `${pct.toFixed(1)}%`;
    progressLabel.textContent = `${state.reportedPrecincts} / ${state.totalPrecincts} precincts`;
  }

  // ---------- Color & Overlay Engine ----------
  function sumVotes(votesMap) {
    let total = 0;
    for (const v of Object.values(votesMap || {})) total += v;
    return total;
  }

  function rankParties(votesMap) {
    const entries = Object.entries(votesMap || {}).sort((a, b) => b[1] - a[1]);
    return entries;
  }

  function hexToRgb(hex) {
    const h = hex.replace('#', '');
    return [
      parseInt(h.slice(0, 2), 16),
      parseInt(h.slice(2, 4), 16),
      parseInt(h.slice(4, 6), 16)
    ];
  }

  function mixColor(c1, c2, t) {
    const clamped = Math.max(0, Math.min(1, t));
    const r = Math.round(c1[0] + (c2[0] - c1[0]) * clamped);
    const g = Math.round(c1[1] + (c2[1] - c1[1]) * clamped);
    const b = Math.round(c1[2] + (c2[2] - c1[2]) * clamped);
    return `rgb(${r}, ${g}, ${b})`;
  }

  function computeOverlayColor(votesMap, eligibleVoters, fallbackSeats) {
    const total = sumVotes(votesMap);

    // If precinct hasn't reported yet, show faint exit-poll tint in Winner mode
    if (total === 0) {
      if (state.overlay === 'winner' && fallbackSeats) {
        const rankedSeats = rankParties(fallbackSeats);
        if (rankedSeats.length > 0) {
          const p = state.partiesById[rankedSeats[0][0]];
          if (p) return mixColor([30, 44, 60], hexToRgb(p.color), 0.28);
        }
      }
      return '#203042';
    }

    if (state.overlay === 'winner') {
      const ranked = rankParties(votesMap);
      const leader = state.partiesById[ranked[0][0]];
      const leadShare = ranked[0][1] / total;
      const runnerShare = ranked[1] ? ranked[1][1] / total : 0;
      const margin = leadShare - runnerShare;
      // Blend from soft neutral into vivid party color based on margin of victory
      const intensity = 0.45 + Math.min(0.55, margin * 2.6);
      return mixColor([24, 34, 48], hexToRgb(leader.color), intensity);
    }

    if (state.overlay === 'party') {
      const p = state.partiesById[state.heatmapPartyId];
      const share = (votesMap[state.heatmapPartyId] || 0) / total;
      const intensity = Math.min(1, share / 0.42);
      return mixColor([20, 31, 44], hexToRgb(p ? p.color : '#38bdf8'), 0.12 + intensity * 0.88);
    }

    if (state.overlay === 'turnout') {
      const turnout = eligibleVoters > 0 ? total / eligibleVoters : 0;
      const t = (turnout - 0.35) / 0.45; // 35% -> 80%
      return mixColor([30, 41, 59], [20, 184, 166], Math.max(0.05, Math.min(1, t)));
    }

    if (state.overlay === 'eu_axis' || state.overlay === 'econ_axis') {
      const axisIdx = state.overlay === 'eu_axis' ? 2 : 0;
      let weighted = 0;
      for (const [pid, v] of Object.entries(votesMap)) {
        const p = state.partiesById[pid];
        if (p) weighted += (v / total) * p.position[axisIdx];
      }
      // Diverging scale around 0 in [-0.6, +0.6]
      const norm = Math.max(-1, Math.min(1, weighted / 0.55));
      if (state.overlay === 'eu_axis') {
        return norm >= 0
          ? mixColor([226, 232, 240], [37, 99, 235], norm)   // Pro-EU blue
          : mixColor([226, 232, 240], [217, 119, 6], -norm); // Eurosceptic amber
      }
      return norm >= 0
        ? mixColor([226, 232, 240], [30, 64, 175], norm)     // Econ Right blue
        : mixColor([226, 232, 240], [220, 38, 38], -norm);   // Econ Left red
    }

    return '#203042';
  }

  function colorPrecinctCell(precinctId, flash = false) {
    const el = document.getElementById(`precinct-${precinctId}`);
    if (!el) return;
    const rep = state.precincts[precinctId];
    const geo = state.precinctGeo[precinctId];
    const fallbackSeats = geo ? state.regionSeats[geo.regionId] : null;

    el.style.fill = computeOverlayColor(rep?.votes, rep?.eligible_voters || 0, fallbackSeats);
    if (flash) {
      el.classList.remove('flash');
      void el.offsetWidth;
      el.classList.add('flash');
    }
  }

  function recolorMap() {
    for (const pid of Object.keys(state.precinctGeo)) {
      colorPrecinctCell(pid, false);
    }
  }

  function renderLegend() {
    partyPickerWrap.classList.toggle('hidden', state.overlay !== 'party');

    if (state.overlay === 'winner') {
      legendEl.innerHTML = `
        <div class="legend-labels"><span>Faint: Exit Poll / Close</span><span>Vivid: Big Margin</span></div>
      `;
    } else if (state.overlay === 'party') {
      const p = state.partiesById[state.heatmapPartyId];
      const col = p ? p.color : '#38bdf8';
      legendEl.innerHTML = `
        <div class="legend-bar" style="background:linear-gradient(90deg, #141f2c, ${col})"></div>
        <div class="legend-labels"><span>0%</span><span>20%</span><span>40%+</span></div>
      `;
    } else if (state.overlay === 'turnout') {
      legendEl.innerHTML = `
        <div class="legend-bar" style="background:linear-gradient(90deg, #1e293b, #14b8a6)"></div>
        <div class="legend-labels"><span>35%</span><span>58%</span><span>80%+</span></div>
      `;
    } else if (state.overlay === 'eu_axis') {
      legendEl.innerHTML = `
        <div class="legend-bar" style="background:linear-gradient(90deg, #d97706, #e2e8f0, #2563eb)"></div>
        <div class="legend-labels"><span>Eurosceptic</span><span>Neutral</span><span>Pro-EU</span></div>
      `;
    } else if (state.overlay === 'econ_axis') {
      legendEl.innerHTML = `
        <div class="legend-bar" style="background:linear-gradient(90deg, #dc2626, #e2e8f0, #1e40af)"></div>
        <div class="legend-labels"><span>Econ Left</span><span>Center</span><span>Econ Right</span></div>
      `;
    }
  }

  // ---------- Parliament Hemicycle ----------
  let hemicycleDots = [];

  function buildHemicycle(totalSeats) {
    const hemiSvg = document.getElementById('hemicycle-svg');
    hemiSvg.innerHTML = '';

    const cx = 180;
    const cy = 180;
    const rows = 7;
    const innerR = 62;
    const outerR = 162;

    // Distribute seats across concentric rows proportional to circumference.
    const radii = [];
    let radiusSum = 0;
    for (let r = 0; r < rows; r++) {
      const rad = innerR + (r / (rows - 1)) * (outerR - innerR);
      radii.push(rad);
      radiusSum += rad;
    }

    const rowCounts = radii.map((rad) => Math.round((rad / radiusSum) * totalSeats));
    let diff = totalSeats - rowCounts.reduce((a, b) => a + b, 0);
    for (let i = rows - 1; diff !== 0; i = (i - 1 + rows) % rows) {
      rowCounts[i] += diff > 0 ? 1 : -1;
      diff += diff > 0 ? -1 : 1;
    }

    // Majority center marker line
    hemiSvg.appendChild(svgEl('line', {
      x1: cx, y1: cy - outerR - 10, x2: cx, y2: cy - innerR + 10,
      stroke: 'rgba(255,255,255,0.22)', 'stroke-dasharray': '3,3', 'stroke-width': 1
    }));

    const coords = [];
    for (let r = 0; r < rows; r++) {
      const count = rowCounts[r];
      const rad = radii[r];
      for (let j = 0; j < count; j++) {
        // Angle from PI (left) to 0 (right)
        const angle = count === 1 ? Math.PI / 2 : Math.PI - (j / (count - 1)) * Math.PI;
        coords.push({
          angle,
          rad,
          x: cx + Math.cos(angle) * rad,
          y: cy - Math.sin(angle) * rad
        });
      }
    }

    // Sort left-to-right by angle descending (PI -> 0), then inner to outer
    coords.sort((a, b) => (b.angle - a.angle) || (a.rad - b.rad));

    hemicycleDots = coords.map((c) => {
      const dot = svgEl('circle', {
        cx: c.x.toFixed(1),
        cy: c.y.toFixed(1),
        r: 3.7,
        fill: '#2a3a4c',
        class: 'seat-dot'
      });
      hemiSvg.appendChild(dot);
      return dot;
    });
  }

  function renderHemicycle() {
    const colors = [];
    for (const p of state.orderedParties) {
      const s = state.euSeats[p.id] || 0;
      for (let i = 0; i < s; i++) colors.push(p.color);
    }

    for (let i = 0; i < hemicycleDots.length; i++) {
      hemicycleDots[i].setAttribute('fill', colors[i] || '#2a3a4c');
    }

    const ledger = document.getElementById('seat-ledger');
    ledger.innerHTML = '';
    for (const p of state.orderedParties) {
      const cur = state.euSeats[p.id] || 0;
      const prev = state.prevEUSeats[p.id] || 0;
      const delta = cur - prev;
      const deltaClass = delta > 0 ? 'delta-up' : delta < 0 ? 'delta-down' : 'delta-flat';
      const deltaTxt = delta > 0 ? `+${delta}` : `${delta}`;

      const item = document.createElement('div');
      item.className = 'ledger-item';
      item.innerHTML = `
        <div class="ledger-top"><span class="swatch" style="background:${p.color}"></span><span>${p.short_name}</span></div>
        <div class="ledger-nums">
          <span class="ledger-seats">${cur}</span>
          <span class="ledger-delta ${deltaClass}">${deltaTxt}</span>
        </div>
      `;
      ledger.appendChild(item);
    }
  }

  // ---------- Inspector Drawer ----------
  function selectEntity(type, id) {
    state.selection = { type, id };

    document.querySelectorAll('.precinct-cell.selected').forEach((el) => el.classList.remove('selected'));
    document.querySelectorAll('.sim-country-border.selected').forEach((el) => el.classList.remove('selected'));

    if (type === 'precinct') {
      document.getElementById(`precinct-${id}`)?.classList.add('selected');
      const geo = state.precinctGeo[id];
      if (geo) document.getElementById(`border-${geo.regionId}`)?.classList.add('selected');
    } else if (type === 'country') {
      document.getElementById(`border-${id}`)?.classList.add('selected');
    }

    renderInspector();
  }

  function clearSelection() {
    state.selection = null;
    document.querySelectorAll('.precinct-cell.selected').forEach((el) => el.classList.remove('selected'));
    document.querySelectorAll('.sim-country-border.selected').forEach((el) => el.classList.remove('selected'));
    inspectorEl.classList.add('hidden');
  }

  function renderInspector() {
    if (!state.selection) return;
    inspectorEl.classList.remove('hidden');

    const kickerEl = document.getElementById('inspector-kicker');
    const titleEl = document.getElementById('inspector-title');
    const statsEl = document.getElementById('inspector-stats');
    const actionsEl = document.getElementById('inspector-actions');
    const barsEl = document.getElementById('inspector-bars');

    if (state.selection.type === 'precinct') {
      const pid = state.selection.id;
      const geo = state.precinctGeo[pid];
      const reg = state.regionsById[geo.regionId];
      const rep = state.precincts[pid];

      kickerEl.textContent = `${reg.name.toUpperCase()} · PRECINCT`;
      titleEl.textContent = geo.label;

      actionsEl.innerHTML = `<button class="btn" id="btn-inspect-country">View ${reg.name} Totals</button>`;
      document.getElementById('btn-inspect-country').onclick = () => selectEntity('country', reg.id);

      if (!rep) {
        statsEl.innerHTML = `
          <div class="stat-box"><div class="stat-label">Status</div><div class="stat-value">Counting</div></div>
          <div class="stat-box"><div class="stat-label">Batch</div><div class="stat-value">${pid.toUpperCase()}</div></div>
          <div class="stat-box"><div class="stat-label">Method</div><div class="stat-value">${reg.method}</div></div>
        `;
        barsEl.innerHTML = `<div class="subtitle">Awaiting precinct report from ${geo.label}…</div>`;
        return;
      }

      const total = sumVotes(rep.votes);
      const turnout = rep.eligible_voters > 0 ? (total / rep.eligible_voters) * 100 : 0;
      const ranked = rankParties(rep.votes);
      const margin = ranked.length >= 2 && total > 0 ? ((ranked[0][1] - ranked[1][1]) / total) * 100 : 0;

      statsEl.innerHTML = `
        <div class="stat-box"><div class="stat-label">Votes Cast</div><div class="stat-value">${formatCompact(total)}</div></div>
        <div class="stat-box"><div class="stat-label">Turnout</div><div class="stat-value">${turnout.toFixed(1)}%</div></div>
        <div class="stat-box"><div class="stat-label">Margin</div><div class="stat-value">+${margin.toFixed(1)}%</div></div>
      `;
      renderVoteBars(barsEl, rep.votes, null, reg.threshold);
      return;
    }

    // Country view
    const rid = state.selection.id;
    const reg = state.regionsById[rid];
    const votes = state.regionVotes[rid] || {};
    const seats = state.regionSeats[rid] || {};
    const repCount = state.regionReportedCount[rid] || 0;
    const total = sumVotes(votes);

    kickerEl.textContent = `MEMBER STATE · ${reg.method.toUpperCase()} (${(reg.threshold * 100).toFixed(0)}% THR)`;
    titleEl.textContent = reg.name;

    actionsEl.innerHTML = `<button class="btn" id="btn-zoom-country">Zoom to ${reg.name}</button>`;
    document.getElementById('btn-zoom-country').onclick = () => focusRegion(rid);

    statsEl.innerHTML = `
      <div class="stat-box"><div class="stat-label">Seats</div><div class="stat-value">${reg.seats}</div></div>
      <div class="stat-box"><div class="stat-label">Reported</div><div class="stat-value">${repCount}/${reg.precincts}</div></div>
      <div class="stat-box"><div class="stat-label">Votes</div><div class="stat-value">${total > 0 ? formatCompact(total) : 'Exit Poll'}</div></div>
    `;

    if (total > 0) {
      renderVoteBars(barsEl, votes, seats, reg.threshold);
    } else {
      renderSeatOnlyBars(barsEl, seats, reg.seats);
    }
  }

  function renderVoteBars(container, votesMap, seatsMap, threshold) {
    const total = sumVotes(votesMap);
    const ranked = rankParties(votesMap);
    container.innerHTML = '';

    for (const [pid, v] of ranked) {
      const p = state.partiesById[pid];
      if (!p) continue;
      const share = total > 0 ? v / total : 0;
      const pct = share * 100;
      const seats = seatsMap ? seatsMap[pid] || 0 : null;
      const belowThr = seatsMap && threshold > 0 && share < threshold;

      const row = document.createElement('div');
      row.className = 'bar-row';
      row.style.opacity = belowThr ? '0.45' : '1';
      row.innerHTML = `
        <div class="bar-meta">
          <span class="bar-party"><span class="swatch" style="background:${p.color}"></span>${p.short_name}</span>
          <span class="mono">${pct.toFixed(1)}%${seats !== null ? ` · <strong>${seats}</strong>` : ''}</span>
        </div>
        <div class="bar-track"><div class="bar-fill" style="width:${Math.min(100, pct * 2).toFixed(1)}%;background:${p.color}"></div></div>
      `;
      container.appendChild(row);
    }
  }

  function renderSeatOnlyBars(container, seatsMap, totalSeats) {
    const ranked = rankParties(seatsMap);
    container.innerHTML = '';
    for (const [pid, s] of ranked) {
      const p = state.partiesById[pid];
      if (!p) continue;
      const pct = totalSeats > 0 ? (s / totalSeats) * 100 : 0;
      const row = document.createElement('div');
      row.className = 'bar-row';
      row.innerHTML = `
        <div class="bar-meta">
          <span class="bar-party"><span class="swatch" style="background:${p.color}"></span>${p.short_name} (Exit Poll)</span>
          <span class="mono"><strong>${s}</strong> seats</span>
        </div>
        <div class="bar-track"><div class="bar-fill" style="width:${Math.min(100, pct * 2).toFixed(1)}%;background:${p.color}"></div></div>
      `;
      container.appendChild(row);
    }
  }

  // ---------- Tooltip & Ticker ----------
  function showTooltip(e, regionId, precinctId) {
    const geo = state.precinctGeo[precinctId];
    const reg = state.regionsById[regionId];
    const rep = state.precincts[precinctId];

    let sub = 'Counting ballots…';
    if (rep) {
      const total = sumVotes(rep.votes);
      const ranked = rankParties(rep.votes);
      const lead = ranked.length ? state.partiesById[ranked[0][0]] : null;
      const leadPct = total > 0 && ranked.length ? ((ranked[0][1] / total) * 100).toFixed(1) : '0.0';
      const turnout = rep.eligible_voters > 0 ? ((total / rep.eligible_voters) * 100).toFixed(1) : '0.0';
      sub = `${lead ? lead.short_name : '—'} ${leadPct}% · Turnout ${turnout}%`;
    }

    tooltip.innerHTML = `
      <div class="tooltip-title">${geo.label} (${reg.id.toUpperCase()})</div>
      <div class="tooltip-sub">${sub}</div>
    `;
    tooltip.classList.remove('hidden');
    moveTooltip(e);
  }

  function moveTooltip(e) {
    tooltip.style.left = `${e.clientX + 14}px`;
    tooltip.style.top = `${e.clientY + 14}px`;
  }

  function hideTooltip() {
    tooltip.classList.add('hidden');
  }

  function pushTicker(rep) {
    const geo = state.precinctGeo[rep.precinct_id];
    const total = sumVotes(rep.votes);
    const ranked = rankParties(rep.votes);
    const lead = ranked.length ? state.partiesById[ranked[0][0]] : null;
    const pct = total > 0 && ranked.length ? ((ranked[0][1] / total) * 100).toFixed(1) : '0.0';

    state.ticker.unshift({
      precinctId: rep.precinct_id,
      regionId: rep.region_id,
      label: geo ? geo.label : rep.precinct_name,
      leadShort: lead ? lead.short_name : '—',
      leadColor: lead ? lead.color : '#888',
      pct
    });
    if (state.ticker.length > 3) state.ticker.pop();

    tickerEl.innerHTML = '';
    for (const item of state.ticker) {
      const row = document.createElement('div');
      row.className = 'ticker-row';
      row.innerHTML = `
        <span class="ticker-left">
          <span class="swatch" style="background:${item.leadColor}"></span>
          <strong>${item.regionId.toUpperCase()}</strong>
          <span>${item.label}</span>
        </span>
        <span class="ticker-right">${item.leadShort} ${item.pct}%</span>
      `;
      row.onclick = () => {
        focusRegion(item.regionId);
        selectEntity('precinct', item.precinctId);
      };
      tickerEl.appendChild(row);
    }
  }

  function formatCompact(n) {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
    return `${n}`;
  }

  // ---------- Event Wiring ----------
  function wireEvents() {
    btnRun.addEventListener('click', startStream);

    document.querySelectorAll('.seg-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('.seg-btn').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        state.delayMs = parseInt(btn.dataset.delay, 10);
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
      if (btn.dataset.focus !== 'eu') {
        selectEntity('country', btn.dataset.focus);
      } else {
        clearSelection();
      }
    });

    document.querySelectorAll('.ov-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        document.querySelectorAll('.ov-btn').forEach((b) => b.classList.remove('active'));
        btn.classList.add('active');
        state.overlay = btn.dataset.overlay;
        renderLegend();
        recolorMap();
      });
    });

    partySelect.addEventListener('change', () => {
      state.heatmapPartyId = partySelect.value;
      renderLegend();
      recolorMap();
    });

    document.getElementById('btn-zoom-in').addEventListener('click', () => zoomBy(0.75));
    document.getElementById('btn-zoom-out').addEventListener('click', () => zoomBy(1.33));
    document.getElementById('btn-zoom-reset').addEventListener('click', () => focusRegion('eu'));
    document.getElementById('btn-close-inspector').addEventListener('click', clearSelection);

    const hemiBody = document.getElementById('hemicycle-body');
    const btnToggleHemi = document.getElementById('btn-toggle-hemi');
    btnToggleHemi.addEventListener('click', () => {
      const collapsed = hemiBody.classList.toggle('hidden');
      btnToggleHemi.textContent = collapsed ? '▴' : '▾';
    });

    // Wheel zoom + drag pan on the SVG canvas
    svg.addEventListener('wheel', (e) => {
      e.preventDefault();
      zoomBy(e.deltaY > 0 ? 1.16 : 0.86, e.clientX, e.clientY);
    }, { passive: false });

    svg.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      state.dragging = true;
      state.didDrag = false;
      state.dragStart = { x: e.clientX, y: e.clientY, vb: [...state.viewBox] };
      svg.classList.add('dragging');
    });

    window.addEventListener('pointermove', (e) => {
      if (!state.dragging) return;
      const dx = e.clientX - state.dragStart.x;
      const dy = e.clientY - state.dragStart.y;
      if (Math.abs(dx) > 4 || Math.abs(dy) > 4) state.didDrag = true;

      const rect = svg.getBoundingClientRect();
      const scaleX = state.dragStart.vb[2] / rect.width;
      const scaleY = state.dragStart.vb[3] / rect.height;
      state.viewBox[0] = state.dragStart.vb[0] - dx * scaleX;
      state.viewBox[1] = state.dragStart.vb[1] - dy * scaleY;
      applyViewBox();
    });

    window.addEventListener('pointerup', () => {
      if (!state.dragging) return;
      state.dragging = false;
      svg.classList.remove('dragging');
    });

    svg.addEventListener('click', (e) => {
      if (state.didDrag) return;
      if (e.target === svg || e.target.classList.contains('ctx-country')) {
        clearSelection();
      }
    });
  }

  init();
})();
