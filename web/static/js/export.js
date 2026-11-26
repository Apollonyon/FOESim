// YAPms-style PNG export + preview modal with Copy Image and Download PNG (zero innerHTML).
import { clearEl, el, safeColor } from './dom.js';

const EXPORT_W = 1920;
const EXPORT_H = 1080;
const FONT_SANS = '-apple-system, BlinkMacSystemFont, "Segoe UI", "Inter", sans-serif';
const FONT_MONO = 'ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace';

function readableColor(hex, isDark) {
  const clean = safeColor(hex);
  if (!isDark || !clean.startsWith('#')) return clean;
  const h = clean.slice(1);
  const full = h.length === 3 ? h[0] + h[0] + h[1] + h[1] + h[2] + h[2] : h;
  let r = parseInt(full.slice(0, 2), 16);
  let g = parseInt(full.slice(2, 4), 16);
  let b = parseInt(full.slice(4, 6), 16);
  const lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  if (lum >= 0.45) return clean;
  const boost = 0.42;
  r = Math.round(r + (255 - r) * boost);
  g = Math.round(g + (255 - g) * boost);
  b = Math.round(b + (255 - b) * boost);
  return `rgb(${r},${g},${b})`;
}

function getExportSeats(state) {
  if (state.viewMode === 'forecast' && state.forecast?.groups) {
    const out = {};
    for (const g of state.orderedGroups) {
      out[g.id] = state.forecast.groups[g.id]?.p50 ?? 0;
    }
    return out;
  }
  return state.euSeats || {};
}

function drawTopBar(ctx, state, seats, totalSeats, theme, isDark) {
  const ordered = state.orderedGroups || [];
  const total = Math.max(1, totalSeats);
  const maj = Math.floor(total / 2) + 1;

  const idxOf = new Map(ordered.map((g, i) => [g.id, i]));
  const prevSeats = state.prevEUSeats || {};
  const bySeats = [...ordered].sort((a, b) => {
    const diff = (seats[b.id] || 0) - (seats[a.id] || 0);
    if (diff !== 0) return diff;
    const prevDiff = (prevSeats[b.id] || 0) - (prevSeats[a.id] || 0);
    if (prevDiff !== 0) return prevDiff;
    return idxOf.get(a.id) - idxOf.get(b.id);
  });
  const topTwo = bySeats.slice(0, 2).sort((a, b) => idxOf.get(a.id) - idxOf.get(b.id));
  const leftGroup = topTwo[0] || null;
  const rightGroup = topTwo[1] || null;
  const middleGroups = ordered.filter((g) => g !== leftGroup && g !== rightGroup);

  const barW = 920;
  const barH = 34;
  const barX = (EXPORT_W - barW) / 2;
  const barY = 58;

  ctx.save();
  ctx.beginPath();
  ctx.roundRect(barX, barY, barW, barH, 7);
  ctx.fillStyle = theme.dotEmpty;
  ctx.fill();
  ctx.clip();

  const leftSeats = leftGroup ? (seats[leftGroup.id] || 0) : 0;
  const rightSeats = rightGroup ? (seats[rightGroup.id] || 0) : 0;
  const leftW = (leftSeats / total) * barW;
  const rightW = (rightSeats / total) * barW;

  if (leftW > 0.5) {
    ctx.fillStyle = safeColor(leftGroup?.color);
    ctx.fillRect(barX, barY, leftW, barH);
  }
  let midX = barX + leftW;
  for (const g of middleGroups) {
    const w = ((seats[g.id] || 0) / total) * barW;
    if (w > 0.5) {
      ctx.fillStyle = safeColor(g.color);
      ctx.fillRect(midX, barY, w, barH);
    }
    midX += w;
  }
  if (rightW > 0.5) {
    ctx.fillStyle = safeColor(rightGroup?.color);
    ctx.fillRect(barX + barW - rightW, barY, rightW, barH);
  }
  ctx.restore();

  ctx.beginPath();
  ctx.roundRect(barX, barY, barW, barH, 7);
  ctx.strokeStyle = theme.hudBorder;
  ctx.lineWidth = 1.5;
  ctx.stroke();

  const midLineX = barX + barW * 0.5;
  ctx.save();
  ctx.beginPath();
  ctx.setLineDash([5, 4]);
  ctx.moveTo(midLineX, barY - 8);
  ctx.lineTo(midLineX, barY + barH + 8);
  ctx.strokeStyle = theme.text;
  ctx.globalAlpha = 0.5;
  ctx.lineWidth = 2;
  ctx.stroke();
  ctx.restore();

  ctx.textBaseline = 'alphabetic';
  ctx.font = `700 28px ${FONT_MONO}`;
  if (leftGroup) {
    ctx.textAlign = 'left';
    ctx.fillStyle = readableColor(leftGroup.color, isDark);
    ctx.fillText(`${leftGroup.short_name} ${leftSeats}`, barX, barY - 14);
  }
  if (rightGroup) {
    ctx.textAlign = 'right';
    ctx.fillStyle = readableColor(rightGroup.color, isDark);
    ctx.fillText(`${rightSeats} ${rightGroup.short_name}`, barX + barW, barY - 14);
  }

  ctx.textAlign = 'center';
  ctx.textBaseline = 'top';
  ctx.font = `700 14px ${FONT_MONO}`;
  ctx.fillStyle = theme.muted;
  ctx.fillText(`${maj} TO WIN`, midLineX, barY + barH + 12);

  const extraWithSeats = middleGroups.filter((g) => (seats[g.id] || 0) > 0);
  if (extraWithSeats.length > 0) {
    ctx.font = `700 14px ${FONT_MONO}`;
    const parts = extraWithSeats.map((g) => `${g.short_name} ${seats[g.id]}`);
    ctx.textAlign = 'right';
    ctx.fillStyle = theme.muted;
    ctx.fillText(parts.join('  ·  '), barX + barW, barY + barH + 12);
  }
}

function drawHemicyclePanel(ctx, state, seats, totalSeats, theme) {
  const ordered = state.orderedGroups || [];
  const total = Math.max(1, totalSeats);
  const maj = Math.floor(total / 2) + 1;
  const seatUnit = state.scenario?.meta?.seat_label || 'seats';

  const panelX = 44;
  const panelY = 842;
  const panelW = EXPORT_W - 88;
  const panelH = 188;

  ctx.save();
  ctx.beginPath();
  ctx.roundRect(panelX, panelY, panelW, panelH, 14);
  ctx.fillStyle = theme.hudBg;
  ctx.fill();
  ctx.strokeStyle = theme.hudBorder;
  ctx.lineWidth = 1.5;
  ctx.stroke();

  const cx = panelX + 205;
  const cy = panelY + panelH - 18;
  const rows = total > 500 ? 12 : total > 280 ? 9 : 7;
  const innerR = total > 500 ? 52 : 58;
  const outerR = 152;
  const dotR = total > 500 ? 2.15 : total > 280 ? 2.8 : 3.5;

  const radii = [];
  let radiusSum = 0;
  for (let r = 0; r < rows; r++) {
    const rad = innerR + (r / (rows - 1)) * (outerR - innerR);
    radii.push(rad);
    radiusSum += rad;
  }
  const rowCounts = radii.map((rad) => Math.round((rad / radiusSum) * total));
  let diff = total - rowCounts.reduce((a, b) => a + b, 0);
  for (let i = rows - 1; diff !== 0; i = (i - 1 + rows) % rows) {
    rowCounts[i] += diff > 0 ? 1 : -1;
    diff += diff > 0 ? -1 : 1;
  }

  ctx.save();
  ctx.beginPath();
  ctx.setLineDash([4, 4]);
  ctx.moveTo(cx, cy - outerR - 8);
  ctx.lineTo(cx, cy - innerR + 8);
  ctx.strokeStyle = theme.text;
  ctx.globalAlpha = 0.25;
  ctx.lineWidth = 1.25;
  ctx.stroke();
  ctx.restore();

  const coords = [];
  for (let r = 0; r < rows; r++) {
    const count = rowCounts[r];
    const rad = radii[r];
    for (let j = 0; j < count; j++) {
      const angle = count === 1 ? Math.PI / 2 : Math.PI - (j / (count - 1)) * Math.PI;
      coords.push({
        angle,
        rad,
        x: cx + Math.cos(angle) * rad,
        y: cy - Math.sin(angle) * rad
      });
    }
  }
  coords.sort((a, b) => (b.angle - a.angle) || (a.rad - b.rad));

  const colors = [];
  for (const g of ordered) {
    const count = seats[g.id] || 0;
    const col = safeColor(g.color);
    for (let i = 0; i < count; i++) colors.push(col);
  }

  for (let i = 0; i < coords.length; i++) {
    ctx.beginPath();
    ctx.arc(coords[i].x, coords[i].y, dotR, 0, Math.PI * 2);
    ctx.fillStyle = colors[i] || theme.dotEmpty;
    ctx.fill();
  }

  ctx.textAlign = 'center';
  ctx.textBaseline = 'alphabetic';
  ctx.font = `700 20px ${FONT_MONO}`;
  ctx.fillStyle = theme.text;
  ctx.fillText(String(total), cx, cy - 18);
  ctx.font = `600 11px ${FONT_MONO}`;
  ctx.fillStyle = theme.muted;
  ctx.fillText(`MAJ ${maj}`, cx, cy - 2);

  const dividerX = panelX + 410;
  ctx.beginPath();
  ctx.moveTo(dividerX, panelY + 20);
  ctx.lineTo(dividerX, panelY + panelH - 20);
  ctx.strokeStyle = theme.hudBorder;
  ctx.lineWidth = 1;
  ctx.stroke();

  const gridX = dividerX + 32;
  const gridW = panelX + panelW - gridX - 28;
  const cols = ordered.length <= 4 ? Math.max(1, ordered.length) : ordered.length <= 8 ? 4 : 5;
  const rowsGrid = Math.ceil(ordered.length / cols);
  const compact = rowsGrid >= 3;
  const colW = gridW / cols;
  const rowH = Math.min(compact ? 42 : 68, (panelH - 32) / Math.max(1, rowsGrid));
  const startY = panelY + (panelH - rowsGrid * rowH) / 2;

  for (let i = 0; i < ordered.length; i++) {
    const g = ordered[i];
    const colIdx = i % cols;
    const rowIdx = Math.floor(i / cols);
    const ix = gridX + colIdx * colW;
    const iy = startY + rowIdx * rowH;
    const s = seats[g.id] || 0;

    if (compact) {
      const midY = iy + rowH * 0.5;
      ctx.beginPath();
      ctx.roundRect(ix, midY - 6, 12, 12, 3);
      ctx.fillStyle = safeColor(g.color);
      ctx.fill();

      ctx.textAlign = 'left';
      ctx.textBaseline = 'middle';
      ctx.font = `700 13px ${FONT_SANS}`;
      ctx.fillStyle = theme.text;
      ctx.fillText(g.short_name, ix + 18, midY);

      ctx.textAlign = 'right';
      ctx.font = `700 16px ${FONT_MONO}`;
      ctx.fillStyle = theme.text;
      ctx.fillText(String(s), ix + colW - 18, midY);
    } else {
      ctx.beginPath();
      ctx.roundRect(ix, iy + 6, 14, 14, 3);
      ctx.fillStyle = safeColor(g.color);
      ctx.fill();

      ctx.textAlign = 'left';
      ctx.textBaseline = 'middle';
      ctx.font = `700 15px ${FONT_SANS}`;
      ctx.fillStyle = theme.text;
      ctx.fillText(g.short_name, ix + 22, iy + 13);

      ctx.font = `700 24px ${FONT_MONO}`;
      ctx.fillStyle = theme.text;
      ctx.fillText(String(s), ix + 22, iy + 42);

      const numW = ctx.measureText(String(s)).width;
      ctx.font = `600 12px ${FONT_MONO}`;
      ctx.fillStyle = theme.muted;
      ctx.fillText(seatUnit, ix + 28 + numW, iy + 44);
    }
  }

  ctx.restore();
}

function drawFooter(ctx, state, theme) {
  const footY = EXPORT_H - 22;

  ctx.save();
  ctx.textAlign = 'left';
  ctx.textBaseline = 'middle';
  ctx.font = `800 14px ${FONT_SANS}`;
  ctx.fillStyle = theme.muted;
  ctx.globalAlpha = 0.85;
  ctx.fillText('FOES', 44, footY);

  const attr = state.scenario?.meta?.attribution || '';
  if (attr) {
    ctx.textAlign = 'right';
    ctx.font = `500 12px ${FONT_SANS}`;
    ctx.fillStyle = theme.muted;
    ctx.fillText(attr, EXPORT_W - 44, footY);
  }
  ctx.restore();
}

export function renderExportCanvas(map, state) {
  const cs = getComputedStyle(document.documentElement);
  const get = (name, fb) => cs.getPropertyValue(name).trim() || fb;
  const isDark = document.documentElement.dataset.theme === 'dark';
  const theme = {
    ocean: get('--ocean', '#dae4ec'),
    hudBg: get('--hud-bg', 'rgba(255,255,255,0.92)'),
    hudBorder: get('--hud-border', 'rgba(15,23,42,0.12)'),
    text: get('--text', '#0f172a'),
    muted: get('--muted', '#556578'),
    dotEmpty: get('--dot-empty', '#cbd5e1')
  };

  const canvas = document.createElement('canvas');
  canvas.width = EXPORT_W;
  canvas.height = EXPORT_H;
  const ctx = canvas.getContext('2d', { alpha: false });

  ctx.fillStyle = theme.ocean;
  ctx.fillRect(0, 0, EXPORT_W, EXPORT_H);

  let totalSeats = 0;
  for (const r of (state.scenario.regions || [])) totalSeats += r.seats || 0;
  const seats = getExportSeats(state);
  const isBar = state.scenario.meta?.chart_type === 'bar';

  if (isBar) {
    map.renderCleanTo(ctx, 40, 128, EXPORT_W - 80, EXPORT_H - 168);
    drawTopBar(ctx, state, seats, totalSeats, theme, isDark);
  } else {
    map.renderCleanTo(ctx, 36, 20, EXPORT_W - 72, 812);
    drawHemicyclePanel(ctx, state, seats, totalSeats, theme);
  }

  drawFooter(ctx, state, theme);
  return canvas;
}

function canvasToBlob(canvas) {
  return new Promise((resolve) => {
    if (typeof canvas.toBlob === 'function') {
      canvas.toBlob((blob) => resolve(blob), 'image/png');
    } else {
      resolve(null);
    }
  });
}

export class ScreenshotModal {
  constructor(modalEl) {
    this.modalEl = modalEl;
    this.previewWrap = null;
    this.btnCopy = null;
    this.btnDownload = null;
    this.statusEl = null;
    this.blob = null;
    this.objectUrl = '';
    this.dataUrl = '';
    this.filename = 'foes-map.png';
    this.statusTimer = null;
    this.openSeq = 0;
    this.#buildShell();
  }

  get isOpen() {
    return !this.modalEl.classList.contains('hidden');
  }

  #revokeUrl() {
    if (this.objectUrl) {
      URL.revokeObjectURL(this.objectUrl);
      this.objectUrl = '';
    }
  }

  #setStatus(msg) {
    if (this.statusTimer) {
      clearTimeout(this.statusTimer);
      this.statusTimer = null;
    }
    if (this.statusEl) this.statusEl.textContent = msg;
    if (msg) {
      this.statusTimer = setTimeout(() => {
        if (this.statusEl) this.statusEl.textContent = '';
        this.statusTimer = null;
      }, 2200);
    }
  }

  #buildShell() {
    clearEl(this.modalEl);

    const backdrop = el('div', { className: 'editor-backdrop', onClick: () => this.close() });
    this.previewWrap = el('div', { className: 'screenshot-preview-wrap' });
    this.statusEl = el('span', { className: 'screenshot-status mono' });

    const canCopy = typeof navigator !== 'undefined' &&
      Boolean(navigator.clipboard?.write) &&
      typeof window !== 'undefined' &&
      typeof window.ClipboardItem !== 'undefined';

    this.btnCopy = el('button', {
      type: 'button',
      className: 'btn',
      text: 'Copy Image',
      onClick: () => this.#copyImage()
    });
    if (!canCopy) this.btnCopy.classList.add('hidden');

    this.btnDownload = el('button', {
      type: 'button',
      className: 'btn btn-ink',
      text: 'Download PNG',
      onClick: () => this.#downloadImage()
    });

    const dialog = el('div', {
      className: 'editor-dialog screenshot-dialog hud',
      role: 'dialog',
      'aria-modal': 'true',
      'aria-labelledby': 'screenshot-title'
    },
      el('div', { className: 'editor-header' },
        el('div', { className: 'changelog-title-row' },
          el('div', { className: 'hud-kicker', id: 'screenshot-title', text: 'MAP SCREENSHOT' }),
          el('span', { className: 'mono changelog-ver-pill', text: `${EXPORT_W}×${EXPORT_H}` })
        ),
        el('button', {
          type: 'button',
          className: 'icon-btn',
          title: 'Close (Esc)',
          'aria-label': 'Close screenshot preview',
          text: '×',
          onClick: () => this.close()
        })
      ),
      el('div', { className: 'editor-body screenshot-body' }, this.previewWrap),
      el('div', { className: 'editor-footer' },
        el('div', { className: 'editor-footer-left' }, this.statusEl),
        el('div', { className: 'editor-footer-right' }, this.btnCopy, this.btnDownload)
      )
    );

    this.modalEl.append(backdrop, dialog);
  }

  async open(map, state) {
    if (!map || !state?.scenario) return;
    const seq = ++this.openSeq;
    this.#revokeUrl();
    this.blob = null;
    this.dataUrl = '';
    this.#setStatus('');

    const rawSlug = state.scenarioId || state.baseScenarioId || state.scenario.name || 'map';
    const slug = rawSlug.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'map';
    this.filename = `foes-${slug}.png`;

    const canvas = renderExportCanvas(map, state);
    canvas.className = 'screenshot-preview-img';
    canvas.setAttribute('role', 'img');
    canvas.setAttribute('aria-label', 'Map screenshot preview');
    clearEl(this.previewWrap);
    this.previewWrap.appendChild(canvas);

    this.btnCopy.disabled = true;
    this.btnDownload.disabled = true;
    this.modalEl.classList.remove('hidden');
    this.modalEl.setAttribute('aria-hidden', 'false');

    const blob = await canvasToBlob(canvas);
    if (seq !== this.openSeq || !this.isOpen) return;

    if (blob) {
      this.blob = blob;
      this.objectUrl = URL.createObjectURL(blob);
    } else {
      this.dataUrl = canvas.toDataURL('image/png');
    }
    this.btnCopy.disabled = !this.blob;
    this.btnDownload.disabled = false;
  }

  close() {
    this.openSeq++;
    this.#setStatus('');
    this.modalEl.classList.add('hidden');
    this.modalEl.setAttribute('aria-hidden', 'true');
    clearEl(this.previewWrap);
    this.blob = null;
    this.dataUrl = '';
    this.#revokeUrl();
  }

  async #copyImage() {
    if (!this.blob) return;
    try {
      await navigator.clipboard.write([new window.ClipboardItem({ 'image/png': this.blob })]);
      this.#setStatus('Copied to clipboard');
    } catch {
      this.#setStatus('Clipboard blocked — use Download PNG');
    }
  }

  #downloadImage() {
    const url = this.objectUrl || this.dataUrl;
    if (!url) return;
    const a = document.createElement('a');
    a.href = url;
    a.download = this.filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    this.#setStatus(`Saved ${this.filename}`);
  }
}
