// Concentric SVG Parliament Hemicycle & EU Group seat ledger.

const SVG_NS = 'http://www.w3.org/2000/svg';

function svgEl(tag, attrs = {}) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

export class Hemicycle {
  constructor(svgElRef, ledgerEl) {
    this.svg = svgElRef;
    this.ledger = ledgerEl;
    this.dots = [];
  }

  build(totalSeats) {
    this.svg.innerHTML = '';
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

    // Center majority line
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
      this.svg.appendChild(dot);
      return dot;
    });
  }

  render(orderedGroups, euSeats, prevEUSeats, highlightGroupIds = null) {
    const colors = [];
    const dimmed = [];
    const hasHighlight = highlightGroupIds && highlightGroupIds.size > 0;
    for (const g of orderedGroups) {
      const s = euSeats[g.id] || 0;
      const isDim = hasHighlight && !highlightGroupIds.has(g.id);
      for (let i = 0; i < s; i++) {
        colors.push(g.color);
        dimmed.push(isDim);
      }
    }

    for (let i = 0; i < this.dots.length; i++) {
      this.dots[i].setAttribute('fill', colors[i] || 'var(--dot-empty)');
      this.dots[i].setAttribute('opacity', dimmed[i] ? '0.16' : '1');
    }

    this.ledger.innerHTML = '';
    for (const g of orderedGroups) {
      const cur = euSeats[g.id] || 0;
      const prev = prevEUSeats[g.id] || 0;
      const delta = cur - prev;
      const deltaClass = delta > 0 ? 'delta-up' : delta < 0 ? 'delta-down' : 'delta-flat';
      const deltaTxt = delta > 0 ? `+${delta}` : `${delta}`;

      const item = document.createElement('div');
      item.className = 'ledger-item';
      item.innerHTML = `
        <div class="ledger-top"><span class="swatch" style="background:${g.color}"></span><span>${g.short_name}</span></div>
        <div class="ledger-nums">
          <span class="ledger-seats">${cur}</span>
          <span class="ledger-delta ${deltaClass}">${deltaTxt}</span>
        </div>
      `;
      this.ledger.appendChild(item);
    }
  }
}
