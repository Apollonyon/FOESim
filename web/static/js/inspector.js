// Inspector drawer, hover tooltip, and live reporting ticker (XSS-safe DOM).
import { clearEl, el, safeColor, swatch } from './dom.js';
import { sumVotes, rankParties } from './overlays.js';

const URBAN_LABELS = { 1: 'Urban', 2: 'Suburban', 3: 'Rural' };
const METHOD_LABELS = {
  sainte_lague: 'SAINTE-LAGUË',
  dhondt: "D'HONDT",
  hare: 'HARE',
  stv: 'STV (APPROX)',
  fptp: 'FPTP'
};

function formatCompact(n) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return `${n}`;
}

function statBox(label, value) {
  return el(
    'div',
    { className: 'stat-box' },
    el('div', { className: 'stat-label', text: label }),
    el('div', { className: 'stat-value', text: value })
  );
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
    this.cb.onSelectionChange?.();
    this.render();
  }

  clear() {
    this.state.selection = null;
    this.cb.onSelectionChange?.();
    this.drawer.classList.add('hidden');
  }

  render() {
    if (!this.state.selection) return;
    this.drawer.classList.remove('hidden');

    const meta = this.state.scenario?.meta || {};
    const regionLabel = (meta.region_label || 'Member State').toUpperCase();
    const unitLabel = meta.unit_label || 'County';
    const seatLabel = meta.seat_label || 'seats';

    const kickerEl = document.getElementById('inspector-kicker');
    const titleEl = document.getElementById('inspector-title');
    const statsEl = document.getElementById('inspector-stats');
    const actionsEl = document.getElementById('inspector-actions');
    const barsEl = document.getElementById('inspector-bars');

    clearEl(statsEl);
    clearEl(actionsEl);
    clearEl(barsEl);

    if (this.state.selection.type === 'precinct') {
      const pid = this.state.selection.id;
      const geo = this.state.precinctGeo[pid];
      if (!geo) return;
      const reg = this.state.regionsById[geo.regionId];
      const rep = this.state.precincts[pid];
      const urbanTag = URBAN_LABELS[geo.urbanType] || unitLabel;
      const distPart = geo.districtId ? ` · ${geo.districtId}` : '';

      kickerEl.textContent = `${reg.name.toUpperCase()}${distPart} · ${urbanTag.toUpperCase()}`;
      titleEl.textContent = geo.label;

      actionsEl.appendChild(
        el('button', {
          className: 'btn',
          text: `View ${reg.name} Totals`,
          onClick: () => this.select('country', reg.id)
        })
      );

      if (this.state.viewMode === 'forecast' && this.state.forecast?.regions?.[reg.id]) {
        const rf = this.state.forecast.regions[reg.id];
        const probs = (geo.districtId && !this.state.methodOverride && rf.district_win_prob?.[geo.districtId])
          ? rf.district_win_prob[geo.districtId]
          : rf.win_prob;
        const rankedProb = rankParties(probs);
        const topLead = rankedProb.length ? this.state.partiesById[rankedProb[0][0]] : null;
        const topOdds = rankedProb.length ? `${topLead?.short_name || rankedProb[0][0]} ${Math.round(rankedProb[0][1] * 100)}%` : '—';
        statsEl.append(
          statBox(geo.districtId ? `${geo.districtId} Odds` : 'Region Odds', topOdds),
          statBox('Simulations', this.state.forecast.runs),
          statBox('Type', urbanTag)
        );
        this.renderForecastBars(barsEl, probs, rf.mean_seats, seatLabel);
        return;
      }

      if (!rep) {
        statsEl.append(
          statBox('Status', 'Counting'),
          statBox(unitLabel, pid),
          statBox('Type', urbanTag)
        );
        barsEl.appendChild(el('div', { className: 'subtitle', text: `Awaiting ballot count from ${geo.label}…` }));
        return;
      }

      const total = sumVotes(rep.votes);
      const repPct = Math.round((rep.reported_pct || 1) * 100);
      const effectiveEligible = rep.eligible_voters * (rep.reported_pct || 1);
      const turnout = effectiveEligible > 0 ? (total / effectiveEligible) * 100 : 0;
      const ranked = rankParties(rep.votes);
      const margin = ranked.length >= 2 && total > 0 ? ((ranked[0][1] - ranked[1][1]) / total) * 100 : 0;

      statsEl.append(
        statBox(repPct < 100 ? `Counted (${repPct}%)` : 'Votes', formatCompact(total)),
        statBox('Turnout', `${turnout.toFixed(1)}%`),
        statBox('Margin', `+${margin.toFixed(1)}%`)
      );
      this.renderBars(barsEl, rep.votes, null, reg.threshold);
      return;
    }

    // Region view
    const rid = this.state.selection.id;
    const reg = this.state.regionsById[rid];
    if (!reg) return;
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
    const splitTag = (!this.state.methodOverride && reg.district_seats > 0) ? ' + DISTRICTS' : '';
    kickerEl.textContent = usesThreshold && reg.threshold > 0
      ? `${regionLabel} · ${methodLabel} (${thrLabel}%)`
      : `${regionLabel} · ${methodLabel}${splitTag}`;
    titleEl.textContent = reg.name;

    actionsEl.appendChild(
      el('button', {
        className: 'btn',
        text: `Focus ${reg.name}`,
        onClick: () => this.cb.onFocusRegion(rid)
      })
    );

    const seatHeading = seatLabel.charAt(0).toUpperCase() + seatLabel.slice(1);
    if (this.state.viewMode === 'forecast' && this.state.forecast?.regions?.[rid]) {
      const rf = this.state.forecast.regions[rid];
      const rankedProb = rankParties(rf.win_prob);
      const topLead = rankedProb.length ? this.state.partiesById[rankedProb[0][0]] : null;
      const topOdds = rankedProb.length ? `${topLead?.short_name || rankedProb[0][0]} ${Math.round(rankedProb[0][1] * 100)}%` : '—';

      statsEl.append(
        statBox(seatHeading, reg.seats),
        statBox('Simulations', this.state.forecast.runs),
        statBox('Top Odds', topOdds)
      );

      if (rf.district_win_prob && Object.keys(rf.district_win_prob).length > 0 && !this.state.methodOverride) {
        const strip = el('div', { className: 'district-badges' });
        for (const did of Object.keys(rf.district_win_prob).sort()) {
          const dRanked = rankParties(rf.district_win_prob[did]);
          const topPid = dRanked[0]?.[0];
          const winner = topPid ? this.state.partiesById[topPid] : null;
          const pctTxt = dRanked[0] ? ` ${Math.round(dRanked[0][1] * 100)}%` : '';
          strip.appendChild(
            el(
              'span',
              { className: 'district-badge' },
              swatch(winner?.color),
              el('strong', { text: did }),
              ` ${winner ? winner.short_name : '—'}${pctTxt}`
            )
          );
        }
        barsEl.appendChild(strip);
      }

      this.renderForecastBars(barsEl, rf.win_prob, rf.mean_seats, seatLabel);
      return;
    }

    statsEl.append(
      statBox(seatHeading, reg.seats),
      statBox('Counted', `${regPct}% (${repCount}/${totalCount})`),
      statBox('Votes', total > 0 ? formatCompact(total) : 'Exit Poll')
    );

    const dw = this.state.districtWinners?.[rid];
    if (dw && Object.keys(dw).length > 0 && !this.state.methodOverride) {
      const strip = el('div', { className: 'district-badges' });
      for (const did of Object.keys(dw).sort()) {
        const winner = this.state.partiesById[dw[did]];
        strip.appendChild(
          el(
            'span',
            { className: 'district-badge' },
            swatch(winner?.color),
            el('strong', { text: did }),
            ` ${winner ? winner.short_name : '—'}`
          )
        );
      }
      barsEl.appendChild(strip);
    }

    if (total > 0) {
      this.renderBars(barsEl, votes, seats, usesThreshold ? reg.threshold : 0);
    } else {
      this.renderExitBars(barsEl, seats, reg.seats, seatLabel);
    }
  }

  renderBars(container, votesMap, seatsMap, threshold) {
    const total = sumVotes(votesMap);
    const ranked = rankParties(votesMap);

    for (const [pid, v] of ranked) {
      const p = this.state.partiesById[pid];
      if (!p) continue;
      const share = total > 0 ? v / total : 0;
      const pct = share * 100;
      const seats = seatsMap ? seatsMap[pid] || 0 : null;
      const belowThr = seatsMap && threshold > 0 && share < threshold;
      const groupObj = p.group_id
        ? this.state.groupsById[p.group_id]
        : (this.state.scenario?.groups?.length ? this.state.groupsById.ni : null);
      const col = safeColor(p.color);

      const partySpan = el('span', { className: 'bar-party' }, swatch(col), p.short_name);
      if (groupObj) {
        partySpan.appendChild(document.createTextNode(' '));
        partySpan.appendChild(el('span', { className: 'group-tag', text: groupObj.short_name }));
      }

      const numSpan = el('span', { className: 'mono' }, `${pct.toFixed(1)}%`);
      if (seats !== null) {
        numSpan.appendChild(document.createTextNode(' · '));
        numSpan.appendChild(el('strong', { text: seats }));
      }

      const row = el(
        'div',
        { className: 'bar-row', style: { opacity: belowThr ? '0.42' : '1' } },
        el('div', { className: 'bar-meta' }, partySpan, numSpan),
        el('div', { className: 'bar-track' }, el('div', {
          className: 'bar-fill',
          style: { width: `${Math.min(100, pct).toFixed(1)}%`, background: col }
        }))
      );
      container.appendChild(row);
    }
  }

  renderExitBars(container, seatsMap, totalSeats, seatLabel = 'seats') {
    const ranked = rankParties(seatsMap);
    for (const [pid, s] of ranked) {
      const p = this.state.partiesById[pid];
      if (!p) continue;
      const pct = totalSeats > 0 ? (s / totalSeats) * 100 : 0;
      const col = safeColor(p.color);
      const row = el(
        'div',
        { className: 'bar-row' },
        el(
          'div',
          { className: 'bar-meta' },
          el('span', { className: 'bar-party' }, swatch(col), p.short_name),
          el('span', { className: 'mono' }, el('strong', { text: s }), ` ${seatLabel} (Exit)`)
        ),
        el('div', { className: 'bar-track' }, el('div', {
          className: 'bar-fill',
          style: { width: `${Math.min(100, pct).toFixed(1)}%`, background: col }
        }))
      );
      container.appendChild(row);
    }
  }

  renderForecastBars(container, winProbMap, meanSeatsMap, seatLabel = 'seats') {
    const ids = new Set([
      ...Object.keys(winProbMap || {}),
      ...Object.keys(meanSeatsMap || {})
    ]);
    const sorted = [...ids].sort((a, b) => {
      const pDiff = (winProbMap?.[b] || 0) - (winProbMap?.[a] || 0);
      if (Math.abs(pDiff) > 1e-6) return pDiff;
      return (meanSeatsMap?.[b] || 0) - (meanSeatsMap?.[a] || 0);
    });

    for (const pid of sorted) {
      const prob = winProbMap?.[pid] || 0;
      const avgSeats = meanSeatsMap?.[pid] || 0;
      if (prob < 0.005 && avgSeats < 0.05) continue;
      const p = this.state.partiesById[pid];
      if (!p) continue;
      const pct = prob * 100;
      const col = safeColor(p.color);
      container.appendChild(
        el(
          'div',
          { className: 'bar-row' },
          el(
            'div',
            { className: 'bar-meta' },
            el('span', { className: 'bar-party' }, swatch(col), p.short_name),
            el('span', { className: 'mono' }, el('strong', { text: `${pct.toFixed(0)}%` }), ` win · Avg ${avgSeats.toFixed(1)} ${seatLabel}`)
          ),
          el('div', { className: 'bar-track' }, el('div', {
            className: 'bar-fill',
            style: { width: `${Math.min(100, pct).toFixed(1)}%`, background: col }
          }))
        )
      );
    }
  }

  showTooltip(e, regionId, precinctId) {
    const geo = this.state.precinctGeo[precinctId];
    const reg = this.state.regionsById[regionId];
    if (!geo || !reg) return;
    const rep = this.state.precincts[precinctId];
    const urbanTag = URBAN_LABELS[geo.urbanType] || '';

    let sub = `${urbanTag}${geo.districtId ? ` · ${geo.districtId}` : ''} · Counting…`;
    if (this.state.viewMode === 'forecast' && this.state.forecast?.regions?.[regionId]) {
      const rf = this.state.forecast.regions[regionId];
      const probs = (geo.districtId && !this.state.methodOverride && rf.district_win_prob?.[geo.districtId])
        ? rf.district_win_prob[geo.districtId]
        : rf.win_prob;
      const ranked = rankParties(probs);
      const lead = ranked.length ? this.state.partiesById[ranked[0][0]] : null;
      const probPct = ranked.length ? `${(ranked[0][1] * 100).toFixed(0)}%` : '0%';
      const scopeTxt = geo.districtId ? `${geo.districtId} odds` : 'Region odds';
      sub = `${lead ? lead.short_name : '—'} ${probPct} (${scopeTxt}) · ${urbanTag}`;
    } else if (rep) {
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

    clearEl(this.tooltip);
    this.tooltip.append(
      el('div', { className: 'tooltip-title', text: `${geo.label} (${reg.id.toUpperCase()})` }),
      el('div', { className: 'tooltip-sub', text: sub })
    );
    this.tooltip.classList.remove('hidden');
    this.tipW = this.tooltip.offsetWidth || 200;
    this.tipH = this.tooltip.offsetHeight || 52;
    this.moveTooltip(e);
  }

  showCalloutTooltip(e, spec) {
    const reg = this.state.regionsById[spec.region_id];
    if (!reg) return;
    const seatLabel = this.state.scenario?.meta?.seat_label || 'seats';
    let title = `${reg.name} (${reg.seats} ${seatLabel})`;
    let sub = 'Exit poll / counting…';

    if (this.state.viewMode === 'forecast' && this.state.forecast?.regions?.[spec.region_id]) {
      const rf = this.state.forecast.regions[spec.region_id];
      if (spec.district_id && !this.state.methodOverride && rf.district_win_prob?.[spec.district_id]) {
        const ranked = rankParties(rf.district_win_prob[spec.district_id]);
        const winner = ranked.length ? this.state.partiesById[ranked[0][0]] : null;
        title = `${spec.label} · ${reg.name} (${reg.district_seats || 1} ${seatLabel})`;
        sub = winner ? `${winner.short_name} ${(ranked[0][1] * 100).toFixed(0)}% win odds` : 'Forecast';
      } else {
        const ranked = rankParties(rf.win_prob);
        const lead = ranked.length ? this.state.partiesById[ranked[0][0]] : null;
        const avg = ranked.length ? (rf.mean_seats?.[ranked[0][0]] || 0).toFixed(1) : '0.0';
        sub = lead ? `${lead.short_name} ${(ranked[0][1] * 100).toFixed(0)}% win odds · Avg ${avg}` : 'Forecast';
      }
    } else if (spec.district_id && !this.state.methodOverride) {
      const winnerId = this.state.districtWinners?.[spec.region_id]?.[spec.district_id];
      const winner = winnerId ? this.state.partiesById[winnerId] : null;
      title = `${spec.label} · ${reg.name} (${reg.district_seats || 1} ${seatLabel})`;
      sub = winner ? `Leading: ${winner.short_name} (${winner.name})` : 'Counting…';
    } else {
      const votes = this.state.regionVotes[spec.region_id];
      const total = sumVotes(votes);
      const pct = this.state.regionPct?.[spec.region_id] ?? 0;
      if (total > 0) {
        const ranked = rankParties(votes);
        const lead = ranked.length ? this.state.partiesById[ranked[0][0]] : null;
        const leadPct = ranked.length ? ((ranked[0][1] / total) * 100).toFixed(1) : '0.0';
        sub = `${lead ? lead.short_name : '—'} ${leadPct}% · ${pct}% counted`;
      } else {
        const rankedSeats = rankParties(this.state.regionSeats[spec.region_id]);
        const lead = rankedSeats.length ? this.state.partiesById[rankedSeats[0][0]] : null;
        if (lead) sub = `Exit Poll: ${lead.short_name} · ${pct}% counted`;
      }
    }

    clearEl(this.tooltip);
    this.tooltip.append(
      el('div', { className: 'tooltip-title', text: title }),
      el('div', { className: 'tooltip-sub', text: sub })
    );
    this.tooltip.classList.remove('hidden');
    this.tipW = this.tooltip.offsetWidth || 200;
    this.tipH = this.tooltip.offsetHeight || 52;
    this.moveTooltip(e);
  }

  moveTooltip(e) {
    const pad = 12;
    const tipW = this.tipW || 200;
    const tipH = this.tipH || 52;
    let x = e.clientX + 14;
    let y = e.clientY + 14;
    if (x + tipW + pad > window.innerWidth) x = Math.max(pad, e.clientX - tipW - 14);
    if (y + tipH + pad > window.innerHeight) y = Math.max(pad, e.clientY - tipH - 14);
    this.tooltip.style.transform = `translate3d(${Math.round(x)}px, ${Math.round(y)}px, 0)`;
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

    clearEl(this.tickerEl);
    for (const item of this.state.ticker) {
      const row = el(
        'button',
        {
          type: 'button',
          className: 'ticker-row',
          onClick: () => {
            this.cb.onFocusRegion(item.regionId);
            this.select('precinct', item.precinctId);
          }
        },
        el(
          'span',
          { className: 'ticker-left' },
          swatch(item.leadColor),
          el('strong', { text: item.regionId.toUpperCase() }),
          el('span', { text: item.label })
        ),
        el('span', { className: 'ticker-right', text: `${item.leadShort} ${item.pct}` })
      );
      this.tickerEl.appendChild(row);
    }
  }
}
