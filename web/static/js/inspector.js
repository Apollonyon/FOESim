// Inspector drawer, hover tooltip, and live reporting ticker.
import { sumVotes, rankParties } from './overlays.js';

const URBAN_LABELS = { 1: 'Urban', 2: 'Intermediate', 3: 'Rural' };
const METHOD_LABELS = {
  sainte_lague: 'SAINTE-LAGUË',
  dhondt: "D'HONDT",
  hare: 'HARE',
  stv: 'STV',
  fptp: 'FPTP'
};

function formatCompact(n) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return `${n}`;
}

export class InspectorUI {
  constructor(state, callbacks) {
    this.state = state;
    this.cb = callbacks;
    this.drawer = document.getElementById('inspector');
    this.tooltip = document.getElementById('map-tooltip');
    this.tickerEl = document.getElementById('ticker');
  }

  select(type, id) {
    this.state.selection = { type, id };
    document.querySelectorAll('.nuts3-cell.selected').forEach((el) => el.classList.remove('selected'));
    document.querySelectorAll('.sim-country-border.selected').forEach((el) => el.classList.remove('selected'));

    if (type === 'precinct') {
      document.getElementById(`precinct-${id}`)?.classList.add('selected');
      const geo = this.state.precinctGeo[id];
      if (geo) document.getElementById(`border-${geo.regionId}`)?.classList.add('selected');
    } else if (type === 'country') {
      document.getElementById(`border-${id}`)?.classList.add('selected');
    }
    this.render();
  }

  clear() {
    this.state.selection = null;
    document.querySelectorAll('.nuts3-cell.selected').forEach((el) => el.classList.remove('selected'));
    document.querySelectorAll('.sim-country-border.selected').forEach((el) => el.classList.remove('selected'));
    this.drawer.classList.add('hidden');
  }

  render() {
    if (!this.state.selection) return;
    this.drawer.classList.remove('hidden');

    const kickerEl = document.getElementById('inspector-kicker');
    const titleEl = document.getElementById('inspector-title');
    const statsEl = document.getElementById('inspector-stats');
    const actionsEl = document.getElementById('inspector-actions');
    const barsEl = document.getElementById('inspector-bars');

    if (this.state.selection.type === 'precinct') {
      const pid = this.state.selection.id;
      const geo = this.state.precinctGeo[pid];
      if (!geo) return;
      const reg = this.state.regionsById[geo.regionId];
      const rep = this.state.precincts[pid];
      const urbanTag = URBAN_LABELS[geo.urbanType] || 'District';

      kickerEl.textContent = `${reg.name.toUpperCase()} · ${pid} · ${urbanTag.toUpperCase()}`;
      titleEl.textContent = geo.label;

      actionsEl.innerHTML = `<button class="btn" id="btn-inspect-country">View ${reg.name} Totals</button>`;
      document.getElementById('btn-inspect-country').onclick = () => this.select('country', reg.id);

      if (!rep) {
        statsEl.innerHTML = `
          <div class="stat-box"><div class="stat-label">Status</div><div class="stat-value">Counting</div></div>
          <div class="stat-box"><div class="stat-label">NUTS-3</div><div class="stat-value">${pid}</div></div>
          <div class="stat-box"><div class="stat-label">Type</div><div class="stat-value">${urbanTag}</div></div>
        `;
        barsEl.innerHTML = `<div class="subtitle">Awaiting ballot count from ${geo.label}…</div>`;
        return;
      }

      const total = sumVotes(rep.votes);
      const repPct = Math.round((rep.reported_pct || 1) * 100);
      const effectiveEligible = rep.eligible_voters * (rep.reported_pct || 1);
      const turnout = effectiveEligible > 0 ? (total / effectiveEligible) * 100 : 0;
      const ranked = rankParties(rep.votes);
      const margin = ranked.length >= 2 && total > 0 ? ((ranked[0][1] - ranked[1][1]) / total) * 100 : 0;

      statsEl.innerHTML = `
        <div class="stat-box"><div class="stat-label">${repPct < 100 ? `Counted (${repPct}%)` : 'Votes'}</div><div class="stat-value">${formatCompact(total)}</div></div>
        <div class="stat-box"><div class="stat-label">Turnout</div><div class="stat-value">${turnout.toFixed(1)}%</div></div>
        <div class="stat-box"><div class="stat-label">Margin</div><div class="stat-value">+${margin.toFixed(1)}%</div></div>
      `;
      this.renderBars(barsEl, rep.votes, null, reg.threshold);
      return;
    }

    // Member State view
    const rid = this.state.selection.id;
    const reg = this.state.regionsById[rid];
    const votes = this.state.regionVotes[rid] || {};
    const seats = this.state.regionSeats[rid] || {};
    const repCount = this.state.regionReportedCount[rid] || 0;
    const totalCount = this.state.regionTotalCount[rid] || reg.precincts;
    const pctSum = (this.state.regionPctSum && this.state.regionPctSum[rid]) || repCount;
    const regPct = Math.min(100, Math.round((pctSum / Math.max(1, totalCount)) * 100));
    const total = sumVotes(votes);

    const activeMethod = this.state.methodOverride || reg.method || 'sainte_lague';
    const usesThreshold = activeMethod !== 'fptp' && activeMethod !== 'stv';
    const methodLabel = METHOD_LABELS[activeMethod] || activeMethod.replace('_', ' ').toUpperCase();
    const thrLabel = Number((reg.threshold * 100).toFixed(1));
    kickerEl.textContent = usesThreshold
      ? `MEMBER STATE · ${methodLabel} (${thrLabel}%)`
      : `MEMBER STATE · ${methodLabel}`;
    titleEl.textContent = reg.name;

    actionsEl.innerHTML = `<button class="btn" id="btn-zoom-country">Focus ${reg.name}</button>`;
    document.getElementById('btn-zoom-country').onclick = () => this.cb.onFocusRegion(rid);

    statsEl.innerHTML = `
      <div class="stat-box"><div class="stat-label">Seats</div><div class="stat-value">${reg.seats}</div></div>
      <div class="stat-box"><div class="stat-label">Counted</div><div class="stat-value">${regPct}% (${repCount}/${totalCount})</div></div>
      <div class="stat-box"><div class="stat-label">Votes</div><div class="stat-value">${total > 0 ? formatCompact(total) : 'Exit Poll'}</div></div>
    `;

    if (total > 0) {
      this.renderBars(barsEl, votes, seats, usesThreshold ? reg.threshold : 0);
    } else {
      this.renderExitBars(barsEl, seats, reg.seats);
    }
  }

  renderBars(container, votesMap, seatsMap, threshold) {
    const total = sumVotes(votesMap);
    const ranked = rankParties(votesMap);
    container.innerHTML = '';

    for (const [pid, v] of ranked) {
      const p = this.state.partiesById[pid];
      if (!p) continue;
      const share = total > 0 ? v / total : 0;
      const pct = share * 100;
      const seats = seatsMap ? seatsMap[pid] || 0 : null;
      const belowThr = seatsMap && threshold > 0 && share < threshold;
      const groupObj = p.group_id ? this.state.groupsById[p.group_id] : (this.state.scenario?.groups?.length ? this.state.groupsById.ni : null);
      const tagHtml = groupObj ? `<span class="group-tag">${groupObj.short_name}</span>` : '';

      const row = document.createElement('div');
      row.className = 'bar-row';
      row.style.opacity = belowThr ? '0.42' : '1';
      row.innerHTML = `
        <div class="bar-meta">
          <span class="bar-party"><span class="swatch" style="background:${p.color}"></span>${p.short_name} ${tagHtml}</span>
          <span class="mono">${pct.toFixed(1)}%${seats !== null ? ` · <strong>${seats}</strong>` : ''}</span>
        </div>
        <div class="bar-track"><div class="bar-fill" style="width:${Math.min(100, pct * 2).toFixed(1)}%;background:${p.color}"></div></div>
      `;
      container.appendChild(row);
    }
  }

  renderExitBars(container, seatsMap, totalSeats) {
    const ranked = rankParties(seatsMap);
    container.innerHTML = '';
    for (const [pid, s] of ranked) {
      const p = this.state.partiesById[pid];
      if (!p) continue;
      const pct = totalSeats > 0 ? (s / totalSeats) * 100 : 0;
      const row = document.createElement('div');
      row.className = 'bar-row';
      row.innerHTML = `
        <div class="bar-meta">
          <span class="bar-party"><span class="swatch" style="background:${p.color}"></span>${p.short_name}</span>
          <span class="mono"><strong>${s}</strong> seats (Exit)</span>
        </div>
        <div class="bar-track"><div class="bar-fill" style="width:${Math.min(100, pct * 2).toFixed(1)}%;background:${p.color}"></div></div>
      `;
      container.appendChild(row);
    }
  }

  showTooltip(e, regionId, precinctId) {
    const geo = this.state.precinctGeo[precinctId];
    const reg = this.state.regionsById[regionId];
    const rep = this.state.precincts[precinctId];
    const urbanTag = URBAN_LABELS[geo.urbanType] || '';

    let sub = `${urbanTag} · Counting…`;
    if (rep) {
      const total = sumVotes(rep.votes);
      const repPct = Math.round((rep.reported_pct || 1) * 100);
      const ranked = rankParties(rep.votes);
      const lead = ranked.length ? this.state.partiesById[ranked[0][0]] : null;
      const leadPct = total > 0 && ranked.length ? ((ranked[0][1] / total) * 100).toFixed(1) : '0.0';
      const effElig = rep.eligible_voters * (rep.reported_pct || 1);
      const turnout = effElig > 0 ? ((total / effElig) * 100).toFixed(1) : '0.0';
      const pctTag = repPct < 100 ? ` · ${repPct}% in` : '';
      sub = `${lead ? lead.short_name : '—'} ${leadPct}% · Turnout ${turnout}%${pctTag}`;
    }

    this.tooltip.innerHTML = `
      <div class="tooltip-title">${geo.label} (${reg.id.toUpperCase()})</div>
      <div class="tooltip-sub">${sub}</div>
    `;
    this.tooltip.classList.remove('hidden');
    this.moveTooltip(e);
  }

  moveTooltip(e) {
    const pad = 12;
    const tipW = this.tooltip.offsetWidth || 200;
    const tipH = this.tooltip.offsetHeight || 52;
    let x = e.clientX + 14;
    let y = e.clientY + 14;
    if (x + tipW + pad > window.innerWidth) x = Math.max(pad, e.clientX - tipW - 14);
    if (y + tipH + pad > window.innerHeight) y = Math.max(pad, e.clientY - tipH - 14);
    this.tooltip.style.left = `${x}px`;
    this.tooltip.style.top = `${y}px`;
  }

  hideTooltip() {
    this.tooltip.classList.add('hidden');
  }

  pushTicker(rep) {
    const geo = this.state.precinctGeo[rep.precinct_id];
    const total = sumVotes(rep.votes);
    const ranked = rankParties(rep.votes);
    const lead = ranked.length ? this.state.partiesById[ranked[0][0]] : null;
    const pct = total > 0 && ranked.length ? ((ranked[0][1] / total) * 100).toFixed(1) : '0.0';

    const repPct = Math.round((rep.reported_pct || 1) * 100);
    this.state.ticker.unshift({
      precinctId: rep.precinct_id,
      regionId: rep.region_id,
      label: geo ? geo.label : rep.precinct_name,
      leadShort: lead ? lead.short_name : '—',
      leadColor: lead ? lead.color : '#64748b',
      pct: repPct < 100 ? `${pct}% (${repPct}%)` : `${pct}%`
    });
    if (this.state.ticker.length > 3) this.state.ticker.pop();

    this.tickerEl.innerHTML = '';
    for (const item of this.state.ticker) {
      const row = document.createElement('div');
      row.className = 'ticker-row';
      row.innerHTML = `
        <span class="ticker-left">
          <span class="swatch" style="background:${item.leadColor}"></span>
          <strong>${item.regionId.toUpperCase()}</strong>
          <span>${item.label}</span>
        </span>
        <span class="ticker-right">${item.leadShort} ${item.pct}</span>
      `;
      row.onclick = () => {
        this.cb.onFocusRegion(item.regionId);
        this.select('precinct', item.precinctId);
      };
      this.tickerEl.appendChild(row);
    }
  }
}
