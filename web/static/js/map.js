// Eurostat GISCO NUTS-3 SVG Map Engine: pan/zoom camera, LOD borders, and rAF batched coloring.
import { computeCountyColor } from './overlays.js';

const SVG_NS = 'http://www.w3.org/2000/svg';
const DEFAULT_BOX = [180, 345, 540, 580];

function svgEl(tag, attrs = {}) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

export class EuropeMap {
  constructor(svg, state, callbacks) {
    this.svg = svg;
    this.state = state;
    this.cb = callbacks;

    this.countryMeta = {};
    this.countyEls = new Map();
    this.pendingRecolor = new Set();
    this.rafQueued = false;

    this.viewBox = [...DEFAULT_BOX];
    this.animFrame = null;
    this.dragging = false;
    this.didDrag = false;
    this.dragStart = null;
  }

  async loadAndRender() {
    const [n0Res, n3Res] = await Promise.all([
      fetch('/geo/europe_nuts0.json'),
      fetch('/geo/nuts3.json')
    ]);
    if (!n0Res.ok || !n3Res.ok) {
      throw new Error(`geo HTTP ${n0Res.status}/${n3Res.status}`);
    }
    const n0 = await n0Res.json();
    const n3 = await n3Res.json();

    const ctxLayer = document.getElementById('layer-context');
    const nutsLayer = document.getElementById('layer-nuts3');
    const borderLayer = document.getElementById('layer-borders');
    const labelLayer = document.getElementById('layer-labels');

    const activeRegionIds = new Set(this.state.scenario.regions.map((r) => r.id));

    for (const c of n0.countries) {
      this.countryMeta[c.id] = c;
      const path = svgEl('path', { d: c.d, class: 'ctx-country' });
      ctxLayer.appendChild(path);
    }

    // Build NUTS-3 county polygons for active regions
    const countyCounts = {};
    for (const f of n3.features) {
      if (!activeRegionIds.has(f.c)) continue;
      countyCounts[f.c] = (countyCounts[f.c] || 0) + 1;

      this.state.precinctGeo[f.id] = {
        label: f.n,
        regionId: f.c,
        urbanType: f.u,
        center: f.p
      };

      const el = svgEl('path', {
        id: `precinct-${f.id}`,
        d: f.d,
        class: 'nuts3-cell'
      });

      el.addEventListener('mouseenter', (e) => this.cb.onHoverStart(e, f.c, f.id));
      el.addEventListener('mousemove', (e) => this.cb.onHoverMove(e));
      el.addEventListener('mouseleave', () => this.cb.onHoverEnd());
      el.addEventListener('click', (e) => {
        e.stopPropagation();
        if (this.didDrag) return;
        if (!this.state.zoomedIn) this.focusRegion(f.c);
        this.cb.onSelect('precinct', f.id);
      });

      nutsLayer.appendChild(el);
      this.countyEls.set(f.id, el);
    }

    // Crisp national borders and labels over active countries + dynamic default camera box
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const r of this.state.scenario.regions) {
      const meta = this.countryMeta[r.id];
      if (!meta) continue;

      if (meta.bbox) {
        const [bx, by, bw, bh] = meta.bbox;
        if (bx < minX) minX = bx;
        if (by < minY) minY = by;
        if (bx + bw > maxX) maxX = bx + bw;
        if (by + bh > maxY) maxY = by + bh;
      }

      borderLayer.appendChild(svgEl('path', {
        id: `border-${r.id}`,
        d: meta.d,
        class: 'sim-country-border'
      }));

      const [lx, ly] = meta.center;
      const title = svgEl('text', { x: lx, y: ly, class: 'country-label' });
      title.textContent = r.id.toUpperCase();
      labelLayer.appendChild(title);

      const totalCount = countyCounts[r.id] || r.precincts;
      this.state.regionTotalCount[r.id] = totalCount;

      const sub = svgEl('text', { id: `label-sub-${r.id}`, x: lx, y: ly + 9, class: 'country-sublabel' });
      sub.textContent = `0%`;
      labelLayer.appendChild(sub);
    }

    if (minX < Infinity) {
      const pad = Math.max(maxX - minX, maxY - minY) * 0.08;
      this.defaultBox = [minX - pad, minY - pad, (maxX - minX) + pad * 2, (maxY - minY) + pad * 2];
    } else {
      this.defaultBox = [...DEFAULT_BOX];
    }

    this.viewBox = this.fitToViewport(this.defaultBox);
    this.applyViewBox();
    this.bindPointerAndWheel();
  }

  fitToViewport(box) {
    const rect = this.svg.getBoundingClientRect();
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

  applyViewBox() {
    this.svg.setAttribute('viewBox', this.viewBox.map((n) => n.toFixed(1)).join(' '));
    const zoomedIn = this.viewBox[2] <= 470;
    if (zoomedIn !== this.state.zoomedIn) {
      this.state.zoomedIn = zoomedIn;
      this.svg.classList.toggle('zoomed-in', zoomedIn);
      this.cb.onZoomThresholdChange();
    }
  }

  animateViewBox(target) {
    if (this.animFrame) cancelAnimationFrame(this.animFrame);
    const fitted = this.fitToViewport(target);
    const start = [...this.viewBox];
    const duration = 380;
    const t0 = performance.now();

    const step = (now) => {
      const p = Math.min(1, (now - t0) / duration);
      const ease = 1 - Math.pow(1 - p, 3);
      for (let i = 0; i < 4; i++) {
        this.viewBox[i] = start[i] + (fitted[i] - start[i]) * ease;
      }
      this.applyViewBox();
      if (p < 1) this.animFrame = requestAnimationFrame(step);
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
    const rect = this.svg.getBoundingClientRect();
    const rx = clientX != null ? (clientX - rect.left) / rect.width : 0.5;
    const ry = clientY != null ? (clientY - rect.top) / rect.height : 0.5;

    const [vx, vy, vw, vh] = this.viewBox;
    const nw = Math.max(70, Math.min(1450, vw * factor));
    const nh = (nw / vw) * vh;
    this.viewBox = [vx + (vw - nw) * rx, vy + (vh - nh) * ry, nw, nh];
    this.applyViewBox();
  }

  queueRecolor(precinctId) {
    this.pendingRecolor.add(precinctId);
    if (!this.rafQueued) {
      this.rafQueued = true;
      requestAnimationFrame(() => this.flushRecolor());
    }
  }

  flushRecolor() {
    this.rafQueued = false;
    for (const pid of this.pendingRecolor) {
      this.paintCounty(pid);
    }
    this.pendingRecolor.clear();
  }

  recolorAll() {
    for (const pid of this.countyEls.keys()) {
      this.paintCounty(pid);
    }
  }

  paintCounty(precinctId) {
    const el = this.countyEls.get(precinctId);
    if (!el) return;
    const rep = this.state.precincts[precinctId];
    const geo = this.state.precinctGeo[precinctId];
    const fallbackSeats = geo ? this.state.regionSeats[geo.regionId] : null;
    el.style.fill = computeCountyColor(this.state, rep?.votes, rep?.eligible_voters || 0, fallbackSeats, rep?.reported_pct || 0);
  }

  resetFills() {
    for (const el of this.countyEls.values()) el.style.fill = '';
  }

  bindPointerAndWheel() {
    this.svg.addEventListener('wheel', (e) => {
      e.preventDefault();
      this.zoomBy(e.deltaY > 0 ? 1.18 : 0.85, e.clientX, e.clientY);
    }, { passive: false });

    this.svg.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      this.dragging = true;
      this.didDrag = false;
      this.dragStart = { x: e.clientX, y: e.clientY, vb: [...this.viewBox] };
      this.svg.classList.add('dragging');
    });

    window.addEventListener('pointermove', (e) => {
      if (!this.dragging) return;
      const dx = e.clientX - this.dragStart.x;
      const dy = e.clientY - this.dragStart.y;
      if (Math.abs(dx) > 4 || Math.abs(dy) > 4) this.didDrag = true;

      const rect = this.svg.getBoundingClientRect();
      const sx = this.dragStart.vb[2] / rect.width;
      const sy = this.dragStart.vb[3] / rect.height;
      this.viewBox[0] = this.dragStart.vb[0] - dx * sx;
      this.viewBox[1] = this.dragStart.vb[1] - dy * sy;
      this.applyViewBox();
    });

    window.addEventListener('pointerup', () => {
      if (!this.dragging) return;
      this.dragging = false;
      this.svg.classList.remove('dragging');
    });

    this.svg.addEventListener('click', (e) => {
      if (this.didDrag) return;
      if (e.target === this.svg || e.target.classList.contains('ctx-country')) {
        this.cb.onClearSelection();
      }
    });
  }
}
