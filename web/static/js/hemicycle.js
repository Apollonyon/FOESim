// Concentric SVG Parliament Hemicycle, Electoral Bar chart, and reusable safe-DOM seat ledger.
import { clearEl, el, safeColor, swatch } from './dom.js';

const SVG_NS = 'http://www.w3.org/2000/svg';

function svgEl(tag, attrs = {}) {
  const node = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  return node;
}

// Lift low-luminance hex colors (e.g. #2563eb) in dark mode so score labels stay readable (WCAG AA).
function readableScoreColor(hex) {
  const clean = safeColor(hex);
  if (document.documentElement.dataset.theme !== 'dark' || !clean.startsWith('#')) {
    return clean;
  }
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

export class Hemicycle {
  constructor(svgElRef, ledgerEl) {
    this.svg = svgElRef;
    this.ledger = ledgerEl;
    this.dots = [];
    this.ledgerRows = new Map();
    this.totalSeats = 0;
    this.chartType = 'hemicycle';
    this.seatLabel = 'seats';
  }

  build(totalSeats, chartType = 'hemicycle', seatLabel = 'seats') {
    this.totalSeats = totalSeats;
    this.chartType = chartType === 'bar' ? 'bar' : 'hemicycle';
    this.seatLabel = seatLabel || 'seats';
    this.svg.replaceChildren();
    this.ledgerRows.clear();
    clearEl(this.ledger);
    this.svg.classList.toggle('bar-mode', this.chartType === 'bar');

    if (this.chartType === 'bar') {
      this.svg.setAttribute('viewBox', '0 0 360 96');
      this.dots = [];
      return;
    }

    this.svg.setAttribute('viewBox', '0 0 360 195');
    const cx = 180;
    const cy = 182;
    const rows = totalSeats > 500 ? 12 : totalSeats > 280 ? 9 : 7;
    const innerR = totalSeats > 500 ? 54 : 62;
    const outerR = 166;
    const dotR = totalSeats > 500 ? 2.15 : totalSeats > 280 ? 2.85 : 3.6;

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

    this.svg.appendChild(svgEl('line', {
      x1: cx, y1: cy - outerR - 8, x2: cx, y2: cy - innerR + 8,
      stroke: 'currentColor', 'stroke-opacity': 0.22, 'stroke-dasharray': '3,3', 'stroke-width': 1
    }));

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

    this.dots = coords.map((c) => {
      const dot = svgEl('circle', {
        cx: c.x.toFixed(1),
        cy: c.y.toFixed(1),
        r: dotR,
        fill: 'var(--dot-empty)',
        class: 'seat-dot'
      });
      dot._fill = 'var(--dot-empty)';
      dot._op = '1';
      this.svg.appendChild(dot);
      return dot;
    });
  }

  renderBar(orderedGroups, euSeats, prevEUSeats = {}) {
    this.svg.replaceChildren();
    const total = Math.max(1, this.totalSeats);
    const maj = Math.floor(total / 2) + 1;
    const barX = 12;
    const barY = 38;
    const barW = 336;
    const barH = 28;

    this.svg.appendChild(svgEl('rect', {
      x: barX, y: barY, width: barW, height: barH, rx: 5,
      fill: 'var(--dot-empty)'
    }));

    const idxOf = new Map(orderedGroups.map((g, i) => [g.id, i]));
    const bySeats = [...orderedGroups].sort((a, b) => {
      const diff = (euSeats[b.id] || 0) - (euSeats[a.id] || 0);
      if (diff !== 0) return diff;
      const prevDiff = (prevEUSeats[b.id] || 0) - (prevEUSeats[a.id] || 0);
      if (prevDiff !== 0) return prevDiff;
      return idxOf.get(a.id) - idxOf.get(b.id);
    });
    const topTwo = bySeats.slice(0, 2).sort((a, b) => idxOf.get(a.id) - idxOf.get(b.id));
    const leftGroup = topTwo[0] || null;
    const rightGroup = topTwo[1] || null;
    const middleGroups = orderedGroups.filter((g) => g !== leftGroup && g !== rightGroup);

    const drawSeg = (x, w, color) => {
      if (w <= 0.2) return;
      this.svg.appendChild(svgEl('rect', {
        x: x.toFixed(1), y: barY, width: w.toFixed(1), height: barH,
        fill: safeColor(color)
      }));
    };

    const leftSeats = leftGroup ? (euSeats[leftGroup.id] || 0) : 0;
    const rightSeats = rightGroup ? (euSeats[rightGroup.id] || 0) : 0;
    const leftW = (leftSeats / total) * barW;
    const rightW = (rightSeats / total) * barW;

    drawSeg(barX, leftW, leftGroup?.color);
    let midX = barX + leftW;
    for (const g of middleGroups) {
      const w = ((euSeats[g.id] || 0) / total) * barW;
      drawSeg(midX, w, g.color);
      midX += w;
    }
    drawSeg(barX + barW - rightW, rightW, rightGroup?.color);

    if (leftGroup) {
      const leftTxt = svgEl('text', {
        x: barX, y: 26, fill: readableScoreColor(leftGroup.color),
        'font-size': 15, 'font-weight': 700, 'font-family': 'ui-monospace, SFMono-Regular, Menlo, monospace'
      });
      leftTxt.textContent = `${leftGroup.short_name} ${leftSeats}`;
      this.svg.appendChild(leftTxt);
    }
    if (rightGroup) {
      const rightTxt = svgEl('text', {
        x: barX + barW, y: 26, 'text-anchor': 'end', fill: readableScoreColor(rightGroup.color),
        'font-size': 15, 'font-weight': 700, 'font-family': 'ui-monospace, SFMono-Regular, Menlo, monospace'
      });
      rightTxt.textContent = `${rightSeats} ${rightGroup.short_name}`;
      this.svg.appendChild(rightTxt);
    }

    const midLineX = barX + barW * 0.5;
    this.svg.appendChild(svgEl('line', {
      x1: midLineX, y1: barY - 6, x2: midLineX, y2: barY + barH + 6,
      stroke: 'currentColor', 'stroke-opacity': 0.55, 'stroke-dasharray': '3,2', 'stroke-width': 1.25
    }));
    const majLabel = svgEl('text', {
      x: midLineX, y: barY + barH + 19, 'text-anchor': 'middle',
      fill: 'var(--muted)', 'font-size': 10, 'font-weight': 600,
      'font-family': 'ui-monospace, SFMono-Regular, Menlo, monospace'
    });
    majLabel.textContent = `${maj} TO WIN`;
    this.svg.appendChild(majLabel);
  }

  ensureLedger(orderedGroups) {
    if (this.ledgerRows.size === orderedGroups.length && orderedGroups.every((g) => this.ledgerRows.has(g.id))) {
      return;
    }
    this.ledgerRows.clear();
    clearEl(this.ledger);
    for (const g of orderedGroups) {
      const seatsEl = el('span', { className: 'ledger-seats', text: '0' });
      const deltaEl = el('span', { className: 'ledger-delta delta-flat', text: '0' });
      const item = el(
        'div',
        { className: 'ledger-item' },
        el('div', { className: 'ledger-top' }, swatch(g.color), el('span', { text: g.short_name })),
        el('div', { className: 'ledger-nums' }, seatsEl, deltaEl)
      );
      this.ledger.appendChild(item);
      this.ledgerRows.set(g.id, { seatsEl, deltaEl });
    }
  }

  render(orderedGroups, euSeats, prevEUSeats, highlightGroupIds = null) {
    if (this.chartType === 'bar') {
      this.renderBar(orderedGroups, euSeats, prevEUSeats);
    } else {
      const colors = [];
      const dimmed = [];
      const hasHighlight = highlightGroupIds && highlightGroupIds.size > 0;
      for (const g of orderedGroups) {
        const s = euSeats[g.id] || 0;
        const isDim = hasHighlight && !highlightGroupIds.has(g.id);
        const col = safeColor(g.color);
        for (let i = 0; i < s; i++) {
          colors.push(col);
          dimmed.push(isDim);
        }
      }

      for (let i = 0; i < this.dots.length; i++) {
        const dot = this.dots[i];
        const fill = colors[i] || 'var(--dot-empty)';
        const op = dimmed[i] ? '0.16' : '1';
        if (dot._fill !== fill) {
          dot._fill = fill;
          dot.setAttribute('fill', fill);
        }
        if (dot._op !== op) {
          dot._op = op;
          dot.setAttribute('opacity', op);
        }
      }
    }

    this.ensureLedger(orderedGroups);
    for (const g of orderedGroups) {
      const cur = euSeats[g.id] || 0;
      const prev = prevEUSeats[g.id] || 0;
      const delta = cur - prev;
      const deltaClass = delta > 0 ? 'delta-up' : delta < 0 ? 'delta-down' : 'delta-flat';
      const deltaTxt = delta > 0 ? `+${delta}` : `${delta}`;
      const row = this.ledgerRows.get(g.id);
      if (row) {
        row.seatsEl.textContent = String(cur);
        row.deltaEl.className = `ledger-delta ${deltaClass}`;
        row.deltaEl.textContent = deltaTxt;
      }
    }
  }
}
