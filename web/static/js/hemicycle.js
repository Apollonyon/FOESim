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
  constructor(svgElRef, ledgerEl, forecastSummaryEl = null, voteTotalsEl = null) {
    this.svg = svgElRef;
    this.ledger = ledgerEl;
    this.forecastSummary = forecastSummaryEl;
    this.voteTotals = voteTotalsEl;
    this.dots = [];
    this.ledgerRows = new Map();
    this.selectedForecastGroupId = '';
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
    this.selectedForecastGroupId = '';
    clearEl(this.ledger);
    if (this.forecastSummary) {
      clearEl(this.forecastSummary);
      this.forecastSummary.classList.add('hidden');
    }
    if (this.voteTotals) {
      clearEl(this.voteTotals);
      this.voteTotals.classList.add('hidden');
    }
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
        'font-size': 15, 'font-weight': 700, 'font-family': '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      });
      leftTxt.textContent = `${leftGroup.short_name} ${leftSeats}`;
      this.svg.appendChild(leftTxt);
    }
    if (rightGroup) {
      const rightTxt = svgEl('text', {
        x: barX + barW, y: 26, 'text-anchor': 'end', fill: readableScoreColor(rightGroup.color),
        'font-size': 15, 'font-weight': 700, 'font-family': '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
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
      'font-family': '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
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
      const shareEl = el('span', { className: 'ledger-share mono' });
      const seatsEl = el('span', { className: 'ledger-seats', text: '0' });
      const deltaEl = el('span', { className: 'ledger-delta delta-flat', text: '0' });
      const item = el(
        'div',
        { className: 'ledger-item' },
        el('div', { className: 'ledger-top' }, swatch(g.color), el('span', { text: g.short_name }), shareEl),
        el('div', { className: 'ledger-nums' }, seatsEl, deltaEl)
      );
      this.ledger.appendChild(item);
      this.ledgerRows.set(g.id, { item, seatsEl, deltaEl, shareEl });
    }
  }

  updateVoteSummary(orderedGroups, voteSummary) {
    const hasVotes = Boolean(voteSummary && voteSummary.totalVotes > 0);
    if (this.voteTotals) {
      clearEl(this.voteTotals);
      if (hasVotes) {
        this.voteTotals.classList.remove('hidden');
        const fmtVotes = voteSummary.totalVotes.toLocaleString('en-US');
        this.voteTotals.appendChild(
          el('span', {}, el('strong', { text: fmtVotes }), ' votes')
        );
        if (voteSummary.countedEligible > 0) {
          this.voteTotals.appendChild(
            el('span', {}, el('strong', { text: `${voteSummary.turnoutPct.toFixed(1)}%` }), ' turnout')
          );
          this.voteTotals.title = `${fmtVotes} votes cast out of ${voteSummary.countedEligible.toLocaleString('en-US')} reported eligible voters`;
        } else {
          this.voteTotals.title = `${fmtVotes} votes cast`;
        }
      } else {
        this.voteTotals.classList.add('hidden');
        this.voteTotals.title = '';
      }
    }

    if (this.ledgerRows.size === 0) return;
    for (const g of orderedGroups) {
      const row = this.ledgerRows.get(g.id);
      if (!row) continue;
      if (hasVotes) {
        const v = voteSummary.groupVotes[g.id] || 0;
        const pct = (v / voteSummary.totalVotes) * 100;
        row.shareEl.textContent = v > 0 ? `${pct.toFixed(1)}%` : '';
        row.item.title = `${g.name || g.short_name}: ${v.toLocaleString('en-US')} votes (${pct.toFixed(2)}%)`;
      } else {
        row.shareEl.textContent = '';
        row.item.title = '';
      }
    }
  }

  render(orderedGroups, euSeats, prevEUSeats, highlightGroupIds = null, voteSummary = null) {
    if (this.forecastSummary) {
      clearEl(this.forecastSummary);
      this.forecastSummary.classList.add('hidden');
    }
    this.#renderChart(orderedGroups, euSeats, prevEUSeats, highlightGroupIds);

    const hasPrev = Boolean(prevEUSeats && Object.keys(prevEUSeats).length > 0);
    this.ensureLedger(orderedGroups);
    for (const g of orderedGroups) {
      const cur = euSeats[g.id] || 0;
      const row = this.ledgerRows.get(g.id);
      if (row) {
        row.seatsEl.textContent = String(cur);
        if (hasPrev) {
          const prev = prevEUSeats[g.id] || 0;
          const delta = cur - prev;
          const deltaClass = delta > 0 ? 'delta-up' : delta < 0 ? 'delta-down' : 'delta-flat';
          const deltaTxt = delta > 0 ? `+${delta}` : `${delta}`;
          row.deltaEl.className = `ledger-delta ${deltaClass}`;
          row.deltaEl.textContent = deltaTxt;
        } else {
          row.deltaEl.className = 'ledger-delta';
          row.deltaEl.textContent = '';
        }
      }
    }
    this.updateVoteSummary(orderedGroups, voteSummary);
  }

  #resolveForecastGroup(orderedGroups, forecast) {
    if (this.selectedForecastGroupId && orderedGroups.some((g) => g.id === this.selectedForecastGroupId)) {
      return orderedGroups.find((g) => g.id === this.selectedForecastGroupId);
    }
    let best = orderedGroups[0] || null;
    let bestScore = -1;
    for (const g of orderedGroups) {
      const dist = forecast?.groups?.[g.id];
      const score = dist ? (dist.p50 * 1000 + dist.mean) : 0;
      if (score > bestScore) {
        bestScore = score;
        best = g;
      }
    }
    this.selectedForecastGroupId = best?.id || '';
    return best;
  }

  #renderForecastSummary(orderedGroups, forecast, selectedGroup) {
    if (!this.forecastSummary) return;
    clearEl(this.forecastSummary);
    this.forecastSummary.classList.remove('hidden');

    // 1. Win-the-Election Odds bar when majority probabilities exist
    const majContenders = orderedGroups
      .map((g) => ({ group: g, prob: forecast?.groups?.[g.id]?.majority_prob || 0 }))
      .filter((x) => x.prob > 0.004)
      .sort((a, b) => b.prob - a.prob);

    if (majContenders.length > 0) {
      const oddsLabels = majContenders.map((x) => (
        el('span', { className: 'forecast-odds-tag' },
          swatch(x.group.color),
          el('strong', { text: x.group.short_name }),
          ` ${Math.round(x.prob * 100)}%`
        )
      ));
      const noMajProb = Math.max(0, 1 - majContenders.reduce((acc, x) => acc + x.prob, 0));
      if ((forecast?.tie_prob || 0) >= 0.005) {
        oddsLabels.push(el('span', { className: 'forecast-odds-tag mono', text: `Tie ${Math.round(forecast.tie_prob * 100)}%` }));
      } else if (noMajProb >= 0.01) {
        oddsLabels.push(el('span', { className: 'forecast-odds-tag mono', text: `Split ${Math.round(noMajProb * 100)}%` }));
      }

      const bar = el('div', { className: 'forecast-odds-bar' });
      for (const x of majContenders) {
        bar.appendChild(el('div', {
          className: 'forecast-odds-seg',
          style: { width: `${(x.prob * 100).toFixed(1)}%`, background: safeColor(x.group.color) }
        }));
      }
      this.forecastSummary.appendChild(el('div', { className: 'forecast-odds-wrap' },
        el('div', { className: 'forecast-odds-labels' }, ...oddsLabels),
        bar
      ));
    }

    // 2. Interactive SVG Seat Distribution Histogram for selected group
    if (!selectedGroup || !forecast?.groups?.[selectedGroup.id]) return;
    const dist = forecast.groups[selectedGroup.id];
    const bins = dist.bins || [];
    const color = safeColor(selectedGroup.color);
    const hasBase = forecast?.baseline && Object.prototype.hasOwnProperty.call(forecast.baseline, selectedGroup.id);
    const baseText = hasBase ? `Base ${forecast.baseline[selectedGroup.id]} · ` : '';

    const histHead = el('div', {
      className: 'forecast-hist-head',
      title: 'Base = deterministic 0-uncertainty outcome. Symmetric uncertainty pulls winner-take-all means toward 50/50 when close states can flip.'
    },
      el('span', { className: 'forecast-hist-title' },
        swatch(color),
        el('strong', { text: selectedGroup.short_name }),
        ` ${baseText}P10 ${dist.p10} · Med ${dist.p50} · P90 ${dist.p90}`
      ),
      el('span', { className: 'forecast-hist-hint', text: 'Click row below' })
    );

    const svg = svgEl('svg', { viewBox: '0 0 340 70', class: 'forecast-hist-svg', 'aria-label': `${selectedGroup.short_name} seat distribution` });
    const padL = 8;
    const plotW = 324;
    const baseY = 52;
    const plotH = 40;
    const maxCount = Math.max(1, ...bins.map((b) => b.count));

    if (bins.length === 1) {
      const barW = 32;
      const barX = 170 - barW / 2;
      svg.appendChild(svgEl('rect', {
        x: barX, y: baseY - plotH, width: barW, height: plotH, rx: 2,
        fill: color, opacity: '0.92'
      }));
      const singleLbl = svgEl('text', {
        x: 170, y: 66, 'text-anchor': 'middle', fill: 'var(--muted)',
        'font-size': 9, 'font-family': '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      });
      singleLbl.textContent = `${bins[0].start} ${this.seatLabel} (100%)`;
      svg.appendChild(singleLbl);
    } else if (bins.length > 1) {
      const minX = bins[0].start;
      const maxX = bins[bins.length - 1].end;
      const span = Math.max(1, maxX - minX + 1);
      const xOf = (s) => padL + ((s - minX) / span) * plotW;

      for (const b of bins) {
        const x1 = xOf(b.start);
        const x2 = xOf(b.end + 1);
        const w = Math.max(2, x2 - x1 - 1);
        const h = b.count > 0 ? Math.max(2, (b.count / maxCount) * plotH) : 0;
        const belowMaj = forecast.majority <= maxX && b.end < forecast.majority;
        svg.appendChild(svgEl('rect', {
          x: x1.toFixed(1),
          y: (baseY - h).toFixed(1),
          width: w.toFixed(1),
          height: h.toFixed(1),
          rx: 1.5,
          fill: color,
          opacity: belowMaj ? '0.40' : '0.92'
        }));
      }

      if (forecast.majority >= minX - span * 0.12 && forecast.majority <= maxX + span * 0.12) {
        const majX = Math.max(padL, Math.min(padL + plotW, xOf(forecast.majority)));
        svg.appendChild(svgEl('line', {
          x1: majX.toFixed(1), y1: 6, x2: majX.toFixed(1), y2: baseY + 2,
          stroke: 'currentColor', 'stroke-opacity': '0.55', 'stroke-dasharray': '2.5,2', 'stroke-width': 1.1
        }));
        const majTxt = svgEl('text', {
          x: Math.min(314, Math.max(26, majX)).toFixed(1), y: 10, 'text-anchor': 'middle',
          fill: 'var(--muted)', 'font-size': 8.5, 'font-weight': 700,
          'font-family': '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        });
        majTxt.textContent = `Maj ${forecast.majority}`;
        svg.appendChild(majTxt);
      }

      const addTick = (x, anchor, label) => {
        const t = svgEl('text', {
          x, y: 66, 'text-anchor': anchor, fill: 'var(--muted)',
          'font-size': 9, 'font-family': '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
        });
        t.textContent = label;
        svg.appendChild(t);
      };
      addTick(padL, 'start', String(dist.min));
      addTick(Math.max(padL + 28, Math.min(padL + plotW - 28, xOf(dist.p50))).toFixed(1), 'middle', `P50 ${dist.p50}`);
      addTick(padL + plotW, 'end', String(dist.max));
    }

    this.forecastSummary.append(histHead, svg);
  }

  renderForecast(orderedGroups, forecast, highlightGroupIds = null) {
    if (this.voteTotals) {
      clearEl(this.voteTotals);
      this.voteTotals.classList.add('hidden');
    }
    const selectedGroup = this.#resolveForecastGroup(orderedGroups, forecast);
    const p50Seats = {};
    let maxP90 = 1;
    for (const g of orderedGroups) {
      const dist = forecast?.groups?.[g.id];
      p50Seats[g.id] = dist ? dist.p50 : 0;
      if (dist && dist.p90 > maxP90) maxP90 = dist.p90;
    }

    this.#renderChart(orderedGroups, p50Seats, {}, highlightGroupIds);
    this.#renderForecastSummary(orderedGroups, forecast, selectedGroup);
    this.ledgerRows.clear();
    clearEl(this.ledger);

    for (const g of orderedGroups) {
      const dist = forecast?.groups?.[g.id];
      const p10 = dist?.p10 ?? 0;
      const p50 = dist?.p50 ?? 0;
      const p90 = dist?.p90 ?? 0;
      const majProb = dist?.majority_prob ?? 0;
      const topProb = dist?.largest_prob ?? 0;

      const col = safeColor(g.color);
      const topRow = el('div', { className: 'ledger-top' }, swatch(col), el('span', { text: g.short_name }));
      if (majProb >= 0.005) {
        topRow.appendChild(el('span', { className: 'ledger-odds-pill', text: `Maj ${Math.round(majProb * 100)}%` }));
      } else if (topProb >= 0.005) {
        topRow.appendChild(el('span', { className: 'ledger-odds-pill', text: `#1 ${Math.round(topProb * 100)}%` }));
      }

      const leftPct = Math.max(0, Math.min(100, (p10 / maxP90) * 100));
      const rightPct = Math.max(leftPct, Math.min(100, (p90 / maxP90) * 100));
      const medPct = Math.max(0, Math.min(100, (p50 / maxP90) * 100));

      const track = el('div', { className: 'ledger-range-track' },
        el('div', {
          className: 'ledger-range-band',
          style: {
            left: `${leftPct.toFixed(1)}%`,
            width: `${Math.max(2, rightPct - leftPct).toFixed(1)}%`,
            background: col
          }
        }),
        el('div', {
          className: 'ledger-range-median',
          style: { left: `${medPct.toFixed(1)}%` }
        })
      );

      const selectGroup = () => {
        this.selectedForecastGroupId = g.id;
        this.renderForecast(orderedGroups, forecast, highlightGroupIds);
      };

      const item = el('div', {
        className: `ledger-item ledger-item-forecast${selectedGroup?.id === g.id ? ' is-selected' : ''}`,
        role: 'button',
        tabindex: '0',
        title: `Show ${g.short_name} seat distribution`,
        onClick: selectGroup,
        onKeydown: (e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            e.stopPropagation();
            selectGroup();
          }
        }
      },
        topRow,
        el('div', { className: 'ledger-nums' },
          el('span', { className: 'ledger-seats', text: String(p50) }),
          el('span', { className: 'ledger-range-text', text: `${p10}–${p90}` })
        ),
        track
      );
      this.ledger.appendChild(item);
    }
  }

  #renderChart(orderedGroups, euSeats, prevEUSeats, highlightGroupIds = null) {
    if (this.chartType === 'bar') {
      this.renderBar(orderedGroups, euSeats, prevEUSeats);
      return;
    }
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
}
