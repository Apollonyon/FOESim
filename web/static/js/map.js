// HiDPI HTML5 Canvas + Path2D Map Engine with offscreen bitmap caching & spatial grid:
// - Incremental county patching on an offscreen buffer during live SSE streaming (<0.5ms/frame)
// - Live vector state borders drawn on top of blit (zero border darkening, crisp during zoom)
// - 32x32 spatial hash grid + rAF-throttled hit-testing (O(1) hover even on 3,141+ counties)
import { computeCountyColor, computeForecastColor, rankParties, resolvePartyColor } from './overlays.js';

const DEFAULT_BOX = [180, 345, 540, 580];
const GRID_N = 32;

function pathBBox(d) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  const len = d.length;
  let i = 0;
  let isX = true;
  while (i < len) {
    const ch = d.charCodeAt(i);
    if (ch >= 97 && ch <= 122 && ch !== 122) {
      return [-Infinity, -Infinity, Infinity, Infinity];
    }
    if ((ch >= 48 && ch <= 57) || ch === 45 || ch === 46) {
      const start = i;
      i++;
      while (i < len) {
        const c = d.charCodeAt(i);
        if ((c >= 48 && c <= 57) || c === 46) i++;
        else break;
      }
      const val = parseFloat(d.slice(start, i));
      if (!Number.isNaN(val)) {
        if (isX) {
          if (val < minX) minX = val;
          if (val > maxX) maxX = val;
        } else {
          if (val < minY) minY = val;
          if (val > maxY) maxY = val;
        }
        isX = !isX;
      }
    } else {
      i++;
    }
  }
  return minX < Infinity ? [minX, minY, maxX, maxY] : [0, 0, 0, 0];
}

export class EuropeMap {
  constructor(canvas, state, callbacks) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d', { alpha: false });
    this.bufCanvas = document.createElement('canvas');
    this.bufCtx = this.bufCanvas.getContext('2d', { alpha: false });
    this.overviewCanvas = document.createElement('canvas');
    this.overviewCtx = this.overviewCanvas.getContext('2d', { alpha: false });
    this.hitCtx = document.createElement('canvas').getContext('2d');
    this.state = state;
    this.cb = callbacks;

    this.countryMeta = {};
    this.ctxPaths = [];
    this.counties = [];
    this.countyById = new Map();
    this.borders = [];
    this.borderById = new Map();
    this.callouts = [];

    this.grid = null;
    this.gridBounds = [0, 0, 1, 1];
    this.fallbackCounties = [];

    this.pendingRecolor = new Set();
    this.bufDirtyFull = true;
    this.bufViewBox = null;
    this.overviewDirtyFull = true;
    this.overviewViewBox = null;
    this.bakeTimer = null;
    this.renderQueued = false;
    this.hoverFrameQueued = false;
    this.lastPointerEvent = null;
    this.hoveredCounty = null;
    this.hoveredCallout = null;

    this.width = 1;
    this.height = 1;
    this.dpr = window.devicePixelRatio || 1;
    this.defaultBox = [...DEFAULT_BOX];
    this.viewBox = [...DEFAULT_BOX];
    this.animFrame = null;
    this.loadSeq = 0;
    this.interactionBound = false;
    this.dragging = false;
    this.didDrag = false;
    this.dragStart = null;

    this.theme = {};
    this.refreshTheme();

    this.perfEnabled = new URLSearchParams(window.location.search).has('perf');
    this.perfRafFrames = 0;
    this.perfDrawCount = 0;
    this.perfDrawSum = 0;
    this.perfMaxMs = 0;
    this.perfLastSec = performance.now();
    this.perfLabel = '';
    if (this.perfEnabled) this.startPerfLoop();
  }

  startPerfLoop() {
    const tick = (now) => {
      this.perfRafFrames++;
      const elapsed = now - this.perfLastSec;
      if (elapsed >= 500) {
        const fps = Math.round((this.perfRafFrames * 1000) / elapsed);
        const avg = this.perfDrawCount > 0 ? (this.perfDrawSum / this.perfDrawCount).toFixed(2) : '0.00';
        this.perfLabel = `${fps} FPS · draw ${avg}ms (max ${this.perfMaxMs.toFixed(1)}ms) · ${this.counties.length} units`;
        this.perfRafFrames = 0;
        this.perfDrawCount = 0;
        this.perfDrawSum = 0;
        this.perfMaxMs = 0;
        this.perfLastSec = now;
        this.requestRender();
      }
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }

  refreshTheme() {
    const cs = getComputedStyle(document.documentElement);
    const get = (name, fb) => cs.getPropertyValue(name).trim() || fb;
    this.theme = {
      ocean: get('--ocean', '#dae4ec'),
      landCtx: get('--land-ctx', '#ecebe6'),
      landCtxStroke: get('--land-ctx-stroke', '#c6cfd6'),
      unreported: get('--land-unreported', '#dfe4e8'),
      countyStroke: get('--county-stroke', 'rgba(255,255,255,0.78)'),
      countryBorder: get('--country-border', 'rgba(22,32,44,0.65)'),
      borderActive: get('--country-border-active', '#0f172a'),
      labelFill: get('--label-fill', '#0f172a'),
      labelHalo: get('--label-halo', 'rgba(255,255,255,0.88)'),
      sublabelFill: get('--sublabel-fill', '#334155'),
      dotEmpty: get('--dot-empty', '#cbd5e1')
    };
    this.bufDirtyFull = true;
    this.overviewDirtyFull = true;
  }

  resizeCanvas() {
    const rect = this.canvas.getBoundingClientRect();
    const w = Math.max(1, Math.round(rect.width));
    const h = Math.max(1, Math.round(rect.height));
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    if (w !== this.width || h !== this.height || dpr !== this.dpr) {
      this.width = w;
      this.height = h;
      this.dpr = dpr;
      this.canvas.width = Math.round(w * dpr);
      this.canvas.height = Math.round(h * dpr);
      this.bufCanvas.width = this.canvas.width;
      this.bufCanvas.height = this.canvas.height;
      this.overviewCanvas.width = w;
      this.overviewCanvas.height = h;
      this.bufDirtyFull = true;
      this.overviewDirtyFull = true;
    }
  }

  buildSpatialGrid() {
    const [gx, gy, gw, gh] = this.defaultBox;
    const w = Math.max(1, gw);
    const h = Math.max(1, gh);
    this.gridBounds = [gx, gy, w, h];
    this.grid = Array.from({ length: GRID_N * GRID_N }, () => []);
    this.fallbackCounties = [];

    for (let i = 0; i < this.counties.length; i++) {
      const c = this.counties[i];
      if (!Number.isFinite(c.minX) || !Number.isFinite(c.maxX)) {
        this.fallbackCounties.push(c);
        continue;
      }
      const c0 = Math.max(0, Math.min(GRID_N - 1, Math.floor(((c.minX - gx) / w) * GRID_N)));
      const c1 = Math.max(0, Math.min(GRID_N - 1, Math.floor(((c.maxX - gx) / w) * GRID_N)));
      const r0 = Math.max(0, Math.min(GRID_N - 1, Math.floor(((c.minY - gy) / h) * GRID_N)));
      const r1 = Math.max(0, Math.min(GRID_N - 1, Math.floor(((c.maxY - gy) / h) * GRID_N)));
      for (let r = r0; r <= r1; r++) {
        const rowOff = r * GRID_N;
        for (let col = c0; col <= c1; col++) {
          this.grid[rowOff + col].push(c);
        }
      }
    }
  }

  async loadAndRender() {
    const loadId = ++this.loadSeq;
    const meta = this.state.scenario?.meta || {};
    const regionsURL = meta.regions_url || '/geo/europe_nuts0.json';
    const unitsURL = meta.units_url || '/geo/nuts3.json';

    const [n0Res, n3Res] = await Promise.all([
      fetch(regionsURL, { cache: 'no-cache' }),
      fetch(unitsURL, { cache: 'no-cache' })
    ]);
    if (!n0Res.ok || !n3Res.ok) {
      throw new Error(`geo HTTP ${n0Res.status}/${n3Res.status}`);
    }
    const [n0, n3] = await Promise.all([n0Res.json(), n3Res.json()]);
    if (loadId !== this.loadSeq) return false;

    if (this.animFrame) {
      cancelAnimationFrame(this.animFrame);
      this.animFrame = null;
    }
    if (this.bakeTimer) {
      clearTimeout(this.bakeTimer);
      this.bakeTimer = null;
    }
    this.countryMeta = {};
    this.ctxPaths = [];
    this.counties = [];
    this.countyById.clear();
    this.borders = [];
    this.borderById.clear();
    this.callouts = [];
    this.pendingRecolor.clear();
    this.hoveredCounty = null;
    this.hoveredCallout = null;
    this.lastPointerEvent = null;
    this.bufViewBox = null;
    this.bufDirtyFull = true;
    this.overviewViewBox = null;
    this.overviewDirtyFull = true;
    this.state.zoomedIn = false;

    const activeRegionIds = new Set(this.state.scenario.regions.map((r) => r.id));

    const countyCounts = {};
    const singleCountyByRegion = {};
    for (const f of (n3.features || [])) {
      if (!activeRegionIds.has(f.c) || !f.d) continue;
      countyCounts[f.c] = (countyCounts[f.c] || 0) + 1;

      this.state.precinctGeo[f.id] = {
        label: f.n,
        regionId: f.c,
        districtId: f.dist || '',
        urbanType: f.u,
        center: f.p
      };

      const [minX, minY, maxX, maxY] = pathBBox(f.d);
      const item = {
        id: f.id,
        regionId: f.c,
        path: new Path2D(f.d),
        minX,
        minY,
        maxX,
        maxY,
        fill: ''
      };
      this.counties.push(item);
      this.countyById.set(f.id, item);
      if (countyCounts[f.c] === 1) singleCountyByRegion[f.c] = item;
      else delete singleCountyByRegion[f.c];
    }

    for (const c of (n0.countries || [])) {
      this.countryMeta[c.id] = c;
      if (c.d && (!activeRegionIds.has(c.id) || !countyCounts[c.id])) {
        const bbox = c.bbox
          ? [c.bbox[0], c.bbox[1], c.bbox[0] + c.bbox[2], c.bbox[1] + c.bbox[3]]
          : pathBBox(c.d);
        this.ctxPaths.push({ path: new Path2D(c.d), bbox });
      }
    }

    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const r of this.state.scenario.regions) {
      const rMeta = this.countryMeta[r.id];
      if (!rMeta) continue;

      if (rMeta.bbox) {
        const [bx, by, bw, bh] = rMeta.bbox;
        if (bx < minX) minX = bx;
        if (by < minY) minY = by;
        if (bx + bw > maxX) maxX = bx + bw;
        if (by + bh > maxY) maxY = by + bh;
      }

      const singleCounty = countyCounts[r.id] === 1 ? singleCountyByRegion[r.id] : null;
      const borderBBox = singleCounty && Number.isFinite(singleCounty.minX)
        ? [singleCounty.minX, singleCounty.minY, singleCounty.maxX, singleCounty.maxY]
        : (rMeta.bbox
          ? [rMeta.bbox[0], rMeta.bbox[1], rMeta.bbox[0] + rMeta.bbox[2], rMeta.bbox[1] + rMeta.bbox[3]]
          : pathBBox(rMeta.d));
      const borderObj = {
        id: r.id,
        label: r.id.toUpperCase(),
        path: singleCounty ? singleCounty.path : new Path2D(rMeta.d),
        bbox: borderBBox,
        center: rMeta.center || null,
        hideLabel: Boolean(rMeta.hide_label),
        isSingleUnit: Boolean(singleCounty)
      };
      this.borders.push(borderObj);
      this.borderById.set(r.id, borderObj);

      const usesPack = Boolean(r.geo_pack || r.use_nuts3);
      this.state.regionTotalCount[r.id] = (usesPack && countyCounts[r.id]) ? countyCounts[r.id] : (r.precincts || 1);
    }

    if (Array.isArray(n0.callouts)) {
      for (const c of n0.callouts) {
        if (!activeRegionIds.has(c.region_id)) continue;
        const w = c.w || 42;
        const h = c.h || 20;
        this.callouts.push({ spec: { ...c, w, h }, fill: '' });
        if (c.x < minX) minX = c.x;
        if (c.y < minY) minY = c.y;
        if (c.x + w > maxX) maxX = c.x + w;
        if (c.y + h > maxY) maxY = c.y + h;
      }
    }

    if (minX < Infinity) {
      const spanX = maxX - minX;
      const spanY = maxY - minY;
      const pad = Math.max(spanX, spanY) * 0.06;
      const rightPad = this.callouts.length > 0 ? spanX * 0.22 : pad;
      this.defaultBox = [minX - pad, minY - pad, spanX + pad + rightPad, spanY + pad * 2];
    } else {
      this.defaultBox = [...DEFAULT_BOX];
    }

    this.buildSpatialGrid();
    this.resizeCanvas();
    this.viewBox = this.fitToViewport(this.defaultBox);
    this.syncZoomThreshold();
    this.bakeOverview();
    this.bakeBuffer();
    this.render();
    this.bindInteraction();
    return true;
  }

  fitToViewport(box) {
    if (!this.width || !this.height) return [...box];
    const aspect = this.width / this.height;
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

  syncZoomThreshold() {
    const threshold = Math.min(470, this.defaultBox[2] * 0.72);
    const zoomedIn = this.viewBox[2] <= threshold;
    if (zoomedIn !== this.state.zoomedIn) {
      this.state.zoomedIn = zoomedIn;
      this.bufDirtyFull = true;
      this.cb.onZoomThresholdChange();
    }
  }

  scheduleBake(delayMs = 110) {
    if (this.bakeTimer) clearTimeout(this.bakeTimer);
    this.bakeTimer = setTimeout(() => {
      this.bakeTimer = null;
      this.bufDirtyFull = true;
      this.requestRender();
    }, delayMs);
  }

  requestRender() {
    if (this.renderQueued) return;
    this.renderQueued = true;
    requestAnimationFrame(() => {
      this.renderQueued = false;
      this.render();
    });
  }

  queueRecolor(precinctId) {
    this.pendingRecolor.add(precinctId);
    this.requestRender();
  }

  flushRecolor() {
    this.render();
  }

  recolorAll() {
    this.pendingRecolor.clear();
    for (const c of this.counties) {
      this.updateCountyColor(c.id);
    }
    this.updateCalloutColors();
    this.bufDirtyFull = true;
    this.overviewDirtyFull = true;
    this.requestRender();
  }

  updateCountyColor(precinctId) {
    const c = this.countyById.get(precinctId);
    if (!c) return null;
    let fill;
    if (this.state.viewMode === 'forecast' && this.state.forecast) {
      const distId = this.state.precinctGeo[precinctId]?.districtId || null;
      fill = computeForecastColor(this.state, c.regionId, distId);
    } else {
      const rep = this.state.precincts[precinctId];
      const fallbackSeats = this.state.regionSeats[c.regionId];
      fill = computeCountyColor(
        this.state,
        rep?.votes,
        rep?.eligible_voters || 0,
        fallbackSeats,
        rep?.reported_pct || 0
      );
    }
    if (c.fill === fill) return null;
    c.fill = fill;
    return c;
  }

  updateCalloutColors() {
    if (!this.callouts.length) return;
    for (const item of this.callouts) {
      const spec = item.spec;
      if (this.state.viewMode === 'forecast' && this.state.forecast) {
        item.fill = computeForecastColor(this.state, spec.region_id, spec.district_id || null);
        continue;
      }
      let partyId = null;
      if (spec.district_id && !this.state.methodOverride) {
        partyId = this.state.districtWinners?.[spec.region_id]?.[spec.district_id] || null;
      }
      if (!partyId) {
        const seats = this.state.regionSeats[spec.region_id];
        const ranked = rankParties(seats);
        if (ranked.length > 0) partyId = ranked[0][0];
      }
      item.fill = (partyId && this.state.partiesById[partyId])
        ? resolvePartyColor(this.state, this.state.partiesById[partyId])
        : '';
    }
  }

  resetFills() {
    this.pendingRecolor.clear();
    for (const c of this.counties) c.fill = '';
    for (const item of this.callouts) item.fill = '';
    this.bufDirtyFull = true;
    this.overviewDirtyFull = true;
    this.requestRender();
  }

  bakeInto(ctx, cssW, cssH, dpr, box, showCountyBorders) {
    const [vx, vy, vw, vh] = box;
    const vx2 = vx + vw;
    const vy2 = vy + vh;
    const scale = cssW / vw;
    const theme = this.theme;

    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = theme.ocean;
    ctx.fillRect(0, 0, cssW, cssH);

    ctx.setTransform(scale * dpr, 0, 0, scale * dpr, -vx * scale * dpr, -vy * scale * dpr);

    // Context landmass
    ctx.fillStyle = theme.landCtx;
    ctx.strokeStyle = theme.landCtxStroke;
    ctx.lineWidth = 0.7 / scale;
    for (let i = 0; i < this.ctxPaths.length; i++) {
      const cp = this.ctxPaths[i];
      if (cp.bbox[2] < vx || cp.bbox[0] > vx2 || cp.bbox[3] < vy || cp.bbox[1] > vy2) continue;
      ctx.fill(cp.path);
      ctx.stroke(cp.path);
    }

    // Counties
    const defaultFill = theme.unreported;
    ctx.strokeStyle = theme.countyStroke;
    ctx.lineWidth = 0.6 / scale;
    for (let i = 0; i < this.counties.length; i++) {
      const c = this.counties[i];
      if (c.maxX < vx || c.minX > vx2 || c.maxY < vy || c.minY > vy2) continue;
      ctx.fillStyle = c.fill || defaultFill;
      ctx.fill(c.path);
      if (showCountyBorders) ctx.stroke(c.path);
    }
  }

  bakeOverview() {
    const box = this.fitToViewport(this.defaultBox);
    this.bakeInto(this.overviewCtx, this.width, this.height, 1, box, false);
    this.overviewViewBox = box;
    this.overviewDirtyFull = false;
  }

  // Full vector bake of background + visible counties into bufCanvas (state borders drawn live).
  bakeBuffer() {
    this.bakeInto(this.bufCtx, this.width, this.height, this.dpr, this.viewBox, this.state.zoomedIn);
    this.bufViewBox = [...this.viewBox];
    this.bufDirtyFull = false;
  }

  patchInto(ctx, cssW, dpr, box, dirtyCounties, showCountyBorders) {
    const [vx, vy, vw, vh] = box;
    const vx2 = vx + vw;
    const vy2 = vy + vh;
    const scale = cssW / vw;
    const theme = this.theme;

    ctx.setTransform(scale * dpr, 0, 0, scale * dpr, -vx * scale * dpr, -vy * scale * dpr);
    ctx.strokeStyle = theme.countyStroke;
    ctx.lineWidth = 0.6 / scale;

    for (let i = 0; i < dirtyCounties.length; i++) {
      const c = dirtyCounties[i];
      if (c.maxX < vx || c.minX > vx2 || c.maxY < vy || c.minY > vy2) continue;
      ctx.fillStyle = c.fill || theme.unreported;
      ctx.fill(c.path);
      if (showCountyBorders) ctx.stroke(c.path);
    }
  }

  // Paint ONLY newly updated counties onto the cached buffers (no border alpha-compounding!).
  patchBuffer(dirtyCounties) {
    if (dirtyCounties.length === 0) return;
    if (this.bufViewBox) {
      this.patchInto(this.bufCtx, this.width, this.dpr, this.bufViewBox, dirtyCounties, this.state.zoomedIn);
    }
    if (this.overviewViewBox && !this.overviewDirtyFull) {
      this.patchInto(this.overviewCtx, this.width, 1, this.overviewViewBox, dirtyCounties, false);
    }
  }

  animateViewBox(target) {
    if (this.animFrame) cancelAnimationFrame(this.animFrame);
    if (this.bakeTimer) {
      clearTimeout(this.bakeTimer);
      this.bakeTimer = null;
    }
    const fitted = this.fitToViewport(target);
    const start = [...this.viewBox];
    const duration = 300;
    const t0 = performance.now();

    const step = (now) => {
      const p = Math.min(1, (now - t0) / duration);
      const ease = 1 - Math.pow(1 - p, 3);
      for (let i = 0; i < 4; i++) {
        this.viewBox[i] = start[i] + (fitted[i] - start[i]) * ease;
      }
      this.syncZoomThreshold();
      if (p < 1) {
        this.render(true);
        this.animFrame = requestAnimationFrame(step);
      } else {
        this.animFrame = null;
        this.bufDirtyFull = true;
        this.render(false);
      }
    };
    this.animFrame = requestAnimationFrame(step);
  }

  focusRegion(regionId) {
    if (regionId === 'eu') {
      this.animateViewBox(this.defaultBox || DEFAULT_BOX);
      return;
    }
    const meta = this.countryMeta[regionId];
    if (!meta?.bbox) return;
    const [x, y, w, h] = meta.bbox;
    const pad = Math.max(w, h) * 0.22;
    this.animateViewBox([x - pad, y - pad, w + pad * 2, h + pad * 2]);
  }

  zoomBy(factor, clientX, clientY) {
    const rect = this.canvas.getBoundingClientRect();
    const rx = clientX != null ? (clientX - rect.left) / (rect.width || 1) : 0.5;
    const ry = clientY != null ? (clientY - rect.top) / (rect.height || 1) : 0.5;

    const [vx, vy, vw, vh] = this.viewBox;
    const maxW = Math.max(1450, this.defaultBox[2] * 2.2);
    const nw = Math.max(45, Math.min(maxW, vw * factor));
    const nh = (nw / vw) * vh;
    this.viewBox = [vx + (vw - nw) * rx, vy + (vh - nh) * ry, nw, nh];
    this.syncZoomThreshold();
    this.requestRender();
    this.scheduleBake(120);
  }

  render(skipBake = false) {
    const perfT0 = this.perfEnabled ? performance.now() : 0;
    const interacting = skipBake || this.dragging || this.animFrame !== null || this.bakeTimer !== null;

    // 1. Update county colors incrementally
    let dirtyCounties = null;
    if (this.pendingRecolor.size > 0) {
      dirtyCounties = [];
      for (const pid of this.pendingRecolor) {
        const changed = this.updateCountyColor(pid);
        if (changed) dirtyCounties.push(changed);
      }
      this.pendingRecolor.clear();
    }
    this.updateCalloutColors();

    // 2. Keep offscreen buffers up to date
    if (!this.overviewViewBox || (this.overviewDirtyFull && !interacting)) {
      this.bakeOverview();
    }
    if (!this.bufViewBox || (this.bufDirtyFull && !interacting)) {
      this.bakeBuffer();
    } else if (dirtyCounties && dirtyCounties.length > 0) {
      this.patchBuffer(dirtyCounties);
    }

    const ctx = this.ctx;
    const dpr = this.dpr;
    const [vx, vy, vw, vh] = this.viewBox;
    const vx2 = vx + vw;
    const vy2 = vy + vh;
    const scale = this.width / vw;
    const theme = this.theme;

    // 3. GPU texture blit of cached overview backdrop (only when bufCanvas misses viewport) + sharp bufCanvas
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.fillStyle = theme.ocean;
    ctx.fillRect(0, 0, this.width, this.height);

    const [bx, by, bw, bh] = this.bufViewBox;
    if (this.overviewViewBox && (bx > vx + 0.5 || by > vy + 0.5 || bx + bw < vx2 - 0.5 || by + bh < vy2 - 0.5)) {
      const [ox, oy, ow, oh] = this.overviewViewBox;
      ctx.drawImage(
        this.overviewCanvas,
        0, 0, this.overviewCanvas.width, this.overviewCanvas.height,
        (ox - vx) * scale, (oy - vy) * scale, ow * scale, oh * scale
      );
    }

    const destX = (bx - vx) * scale;
    const destY = (by - vy) * scale;
    const destW = bw * scale;
    const destH = bh * scale;
    ctx.drawImage(this.bufCanvas, 0, 0, this.bufCanvas.width, this.bufCanvas.height, destX, destY, destW, destH);

    // 4. Crisp live national/state borders (27-51 paths: zero darkening, crisp at every zoom)
    ctx.setTransform(scale * dpr, 0, 0, scale * dpr, -vx * scale * dpr, -vy * scale * dpr);
    ctx.lineJoin = 'round';
    ctx.strokeStyle = theme.countryBorder;
    ctx.lineWidth = 1.15 / scale;
    for (let i = 0; i < this.borders.length; i++) {
      const b = this.borders[i];
      if (b.bbox[2] < vx || b.bbox[0] > vx2 || b.bbox[3] < vy || b.bbox[1] > vy2) continue;
      ctx.stroke(b.path);
    }

    // 5. Selection & hover highlights
    const sel = this.state.selection;
    if (sel) {
      const selRegionId = sel.type === 'country'
        ? sel.id
        : this.state.precinctGeo[sel.id]?.regionId;
      if (selRegionId) {
        const b = this.borderById.get(selRegionId);
        if (b) {
          ctx.strokeStyle = theme.borderActive;
          ctx.lineWidth = 2.0 / scale;
          ctx.stroke(b.path);
        }
      }
      if (sel.type === 'precinct' && !(selRegionId && this.borderById.get(selRegionId)?.isSingleUnit)) {
        const c = this.countyById.get(sel.id);
        if (c) {
          ctx.strokeStyle = theme.borderActive;
          ctx.lineWidth = 1.8 / scale;
          ctx.stroke(c.path);
        }
      }
    }
    if (this.hoveredCounty && !this.dragging) {
      ctx.strokeStyle = theme.borderActive;
      ctx.lineWidth = 1.4 / scale;
      ctx.stroke(this.hoveredCounty.path);
    }

    // 6. Callout badges (hide district-only badges when method override disables split districts)
    if (this.callouts.length > 0) {
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.font = '700 9.5px ui-monospace, SFMono-Regular, Menlo, monospace';
      for (let i = 0; i < this.callouts.length; i++) {
        const item = this.callouts[i];
        const { x, y, w, h, label, district_id } = item.spec;
        if (district_id && this.state.methodOverride) continue;
        if (x + w < vx || x > vx2 || y + h < vy || y > vy2) continue;
        const isHover = this.hoveredCallout === item;
        ctx.fillStyle = item.fill || theme.dotEmpty;
        ctx.beginPath();
        ctx.roundRect(x, y, w, h, 3.5);
        ctx.fill();
        ctx.strokeStyle = isHover ? theme.borderActive : theme.countryBorder;
        ctx.lineWidth = (isHover ? 1.8 : 0.9) / scale;
        ctx.stroke();

        ctx.lineWidth = 2.2 / scale;
        ctx.strokeStyle = 'rgba(15,23,42,0.6)';
        ctx.strokeText(label, x + w * 0.5, y + h * 0.5 + 0.5);
        ctx.fillStyle = '#ffffff';
        ctx.fillText(label, x + w * 0.5, y + h * 0.5 + 0.5);
      }
    }

    // 7. Crisp screen-space region labels & % sublabels
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';
    ctx.lineJoin = 'round';
    ctx.globalAlpha = this.state.zoomedIn ? 0.30 : 1.0;

    for (let i = 0; i < this.borders.length; i++) {
      const b = this.borders[i];
      if (b.hideLabel || !b.center) continue;
      const [cx, cy] = b.center;
      if (cx < vx || cx > vx2 || cy < vy || cy > vy2) continue;
      const sx = (cx - vx) * scale;
      const sy = (cy - vy) * scale;

      ctx.font = '700 10.5px -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';
      ctx.strokeStyle = theme.labelHalo;
      ctx.lineWidth = 3;
      ctx.strokeText(b.label, sx, sy);
      ctx.fillStyle = theme.labelFill;
      ctx.fillText(b.label, sx, sy);

      const pct = this.state.regionPct?.[b.id] ?? 0;
      const subText = `${pct}%`;
      ctx.font = '600 8.5px ui-monospace, SFMono-Regular, Menlo, monospace';
      ctx.lineWidth = 2.5;
      ctx.strokeText(subText, sx, sy + 11);
      ctx.fillStyle = theme.sublabelFill;
      ctx.fillText(subText, sx, sy + 11);
    }
    ctx.globalAlpha = 1.0;

    if (this.perfEnabled) {
      const drawMs = performance.now() - perfT0;
      this.perfDrawCount++;
      this.perfDrawSum += drawMs;
      if (drawMs > this.perfMaxMs) this.perfMaxMs = drawMs;
      if (this.perfLabel) {
        ctx.font = '700 11px ui-monospace, SFMono-Regular, Menlo, monospace';
        const tw = ctx.measureText(this.perfLabel).width + 16;
        const px = this.width * 0.5 - tw * 0.5;
        const py = 12;
        ctx.fillStyle = 'rgba(15, 23, 42, 0.85)';
        ctx.beginPath();
        ctx.roundRect(px, py, tw, 24, 6);
        ctx.fill();
        ctx.fillStyle = '#34d399';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(this.perfLabel, this.width * 0.5, py + 12);
      }
    }
  }

  screenToMap(clientX, clientY) {
    const rect = this.canvas.getBoundingClientRect();
    const rx = clientX - rect.left;
    const ry = clientY - rect.top;
    const scale = this.width / this.viewBox[2];
    return [this.viewBox[0] + rx / scale, this.viewBox[1] + ry / scale];
  }

  hitTest(clientX, clientY) {
    const [mx, my] = this.screenToMap(clientX, clientY);

    for (let i = this.callouts.length - 1; i >= 0; i--) {
      const item = this.callouts[i];
      const { x, y, w, h, district_id } = item.spec;
      if (district_id && this.state.methodOverride) continue;
      if (mx >= x && mx <= x + w && my >= y && my <= y + h) {
        return { kind: 'callout', item };
      }
    }

    const hitCtx = this.hitCtx;
    const [gx, gy, gw, gh] = this.gridBounds;
    if (this.grid && mx >= gx && mx <= gx + gw && my >= gy && my <= gy + gh) {
      const col = Math.max(0, Math.min(GRID_N - 1, Math.floor(((mx - gx) / gw) * GRID_N)));
      const row = Math.max(0, Math.min(GRID_N - 1, Math.floor(((my - gy) / gh) * GRID_N)));
      const bucket = this.grid[row * GRID_N + col];
      for (let i = bucket.length - 1; i >= 0; i--) {
        const c = bucket[i];
        if (mx < c.minX || mx > c.maxX || my < c.minY || my > c.maxY) continue;
        if (hitCtx.isPointInPath(c.path, mx, my)) {
          return { kind: 'county', item: c };
        }
      }
    }

    for (let i = this.fallbackCounties.length - 1; i >= 0; i--) {
      const c = this.fallbackCounties[i];
      if (hitCtx.isPointInPath(c.path, mx, my)) {
        return { kind: 'county', item: c };
      }
    }
    return null;
  }

  refreshHoverTooltip() {
    if (!this.lastPointerEvent || this.dragging) return;
    if (this.hoveredCounty) {
      this.cb.onHoverStart(this.lastPointerEvent, this.hoveredCounty.regionId, this.hoveredCounty.id);
    } else if (this.hoveredCallout) {
      this.cb.onHoverCallout?.(this.lastPointerEvent, this.hoveredCallout.spec);
    }
  }

  processHover(e) {
    const hit = this.hitTest(e.clientX, e.clientY);
    if (hit?.kind === 'county') {
      const changed = this.hoveredCounty !== hit.item || this.hoveredCallout !== null;
      this.hoveredCounty = hit.item;
      this.hoveredCallout = null;
      this.canvas.classList.add('hovering');
      if (changed) {
        this.cb.onHoverStart(e, hit.item.regionId, hit.item.id);
        this.requestRender();
      } else {
        this.cb.onHoverMove(e);
      }
    } else if (hit?.kind === 'callout') {
      const changed = this.hoveredCallout !== hit.item || this.hoveredCounty !== null;
      this.hoveredCounty = null;
      this.hoveredCallout = hit.item;
      this.canvas.classList.add('hovering');
      if (changed) {
        this.cb.onHoverCallout?.(e, hit.item.spec);
        this.requestRender();
      } else {
        this.cb.onHoverMove(e);
      }
    } else if (this.hoveredCounty || this.hoveredCallout) {
      this.hoveredCounty = null;
      this.hoveredCallout = null;
      this.canvas.classList.remove('hovering');
      this.cb.onHoverEnd();
      this.requestRender();
    }
  }

  bindInteraction() {
    if (this.interactionBound) return;
    this.interactionBound = true;
    this.canvas.addEventListener('wheel', (e) => {
      e.preventDefault();
      this.zoomBy(e.deltaY > 0 ? 1.16 : 0.86, e.clientX, e.clientY);
    }, { passive: false });

    this.canvas.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      if (this.animFrame) {
        cancelAnimationFrame(this.animFrame);
        this.animFrame = null;
      }
      if (this.bakeTimer) {
        clearTimeout(this.bakeTimer);
        this.bakeTimer = null;
      }
      this.dragging = true;
      this.didDrag = false;
      this.dragStart = {
        x: e.clientX,
        y: e.clientY,
        vb: [...this.viewBox],
        sx: this.viewBox[2] / (this.width || 1),
        sy: this.viewBox[3] / (this.height || 1)
      };
      this.canvas.classList.add('dragging');
      this.cb.onHoverEnd();
    });

    window.addEventListener('pointermove', (e) => {
      if (this.dragging) {
        const dx = e.clientX - this.dragStart.x;
        const dy = e.clientY - this.dragStart.y;
        if (Math.abs(dx) > 4 || Math.abs(dy) > 4) this.didDrag = true;
        this.viewBox[0] = this.dragStart.vb[0] - dx * this.dragStart.sx;
        this.viewBox[1] = this.dragStart.vb[1] - dy * this.dragStart.sy;
        this.syncZoomThreshold();
        this.requestRender();
        return;
      }
      if (e.target !== this.canvas) {
        this.lastPointerEvent = null;
        if (this.hoveredCounty || this.hoveredCallout) {
          this.hoveredCounty = null;
          this.hoveredCallout = null;
          this.canvas.classList.remove('hovering');
          this.cb.onHoverEnd();
          this.requestRender();
        }
        return;
      }

      this.lastPointerEvent = e;
      if (!this.hoverFrameQueued) {
        this.hoverFrameQueued = true;
        requestAnimationFrame(() => {
          this.hoverFrameQueued = false;
          if (this.lastPointerEvent && !this.dragging) {
            this.processHover(this.lastPointerEvent);
          }
        });
      }
    });

    window.addEventListener('pointerup', () => {
      if (!this.dragging) return;
      const moved = this.didDrag;
      this.dragging = false;
      this.canvas.classList.remove('dragging');
      if (moved) {
        this.bufDirtyFull = true;
        this.requestRender();
      }
    });

    this.canvas.addEventListener('click', (e) => {
      if (this.didDrag) return;
      const hit = this.hitTest(e.clientX, e.clientY);
      if (hit?.kind === 'callout') {
        this.focusRegion(hit.item.spec.region_id);
        this.cb.onSelect('country', hit.item.spec.region_id);
        return;
      }
      if (hit?.kind === 'county') {
        if (!this.state.zoomedIn) {
          this.focusRegion(hit.item.regionId);
          this.cb.onSelect('country', hit.item.regionId);
        } else {
          this.cb.onSelect('precinct', hit.item.id);
        }
        return;
      }
      this.cb.onClearSelection();
    });

    window.addEventListener('resize', () => {
      const oldCenterX = this.viewBox[0] + this.viewBox[2] * 0.5;
      const oldCenterY = this.viewBox[1] + this.viewBox[3] * 0.5;
      const oldH = this.viewBox[3];
      this.resizeCanvas();
      const aspect = this.width / (this.height || 1);
      const newW = oldH * aspect;
      this.viewBox = [oldCenterX - newW * 0.5, oldCenterY - oldH * 0.5, newW, oldH];
      this.syncZoomThreshold();
      this.bufDirtyFull = true;
      this.requestRender();
    });
  }
}
