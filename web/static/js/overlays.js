// Overlay color calculations, Light/Dark neutral blending, and dynamic legends.

export function sumVotes(votesMap) {
  let total = 0;
  for (const v of Object.values(votesMap || {})) total += v;
  return total;
}

export function rankParties(votesMap) {
  return Object.entries(votesMap || {}).sort((a, b) => b[1] - a[1]);
}

function hexToRgb(hex) {
  const h = (hex || '#64748b').replace('#', '');
  return [
    parseInt(h.slice(0, 2), 16),
    parseInt(h.slice(2, 4), 16),
    parseInt(h.slice(4, 6), 16)
  ];
}

function mixColor(c1, c2, t) {
  const k = Math.max(0, Math.min(1, t));
  const r = Math.round(c1[0] + (c2[0] - c1[0]) * k);
  const g = Math.round(c1[1] + (c2[1] - c1[1]) * k);
  const b = Math.round(c1[2] + (c2[2] - c1[2]) * k);
  return `rgb(${r},${g},${b})`;
}

function isDark() {
  return document.documentElement.dataset.theme === 'dark';
}

export function resolvePartyColor(state, party) {
  if (!party) return '#64748b';
  const hasGroups = state.scenario?.groups?.length > 0;
  if (!hasGroups) return party.color;

  const useGroup =
    state.colorMode === 'group' ||
    (state.colorMode === 'auto' && !state.zoomedIn);

  if (useGroup) {
    const gid = party.group_id || 'ni';
    return state.groupsById[gid]?.color || party.color;
  }
  return party.color;
}

export function computeCountyColor(state, votesMap, eligibleVoters, fallbackSeats, reportedPct = 1) {
  const dark = isDark();
  const neutral = dark ? [33, 43, 54] : [223, 228, 232];
  const centerNeutral = dark ? [52, 63, 76] : [238, 241, 244];
  const total = sumVotes(votesMap);

  if (total === 0) {
    if (state.overlay === 'winner' && fallbackSeats) {
      const rankedSeats = rankParties(fallbackSeats);
      if (rankedSeats.length > 0) {
        const p = state.partiesById[rankedSeats[0][0]];
        if (p) return mixColor(neutral, hexToRgb(resolvePartyColor(state, p)), dark ? 0.28 : 0.24);
      }
    }
    return dark ? '#212b36' : '#dfe4e8';
  }

  if (state.overlay === 'winner') {
    const ranked = rankParties(votesMap);
    const leader = state.partiesById[ranked[0][0]];
    const leadShare = ranked[0][1] / total;
    const runnerShare = ranked[1] ? ranked[1][1] / total : 0;
    const margin = leadShare - runnerShare;
    const completeness = 0.55 + 0.45 * Math.min(1, reportedPct || 1);
    const intensity = ((dark ? 0.48 : 0.42) + Math.min(0.54, margin * 2.5)) * completeness;
    return mixColor(neutral, hexToRgb(resolvePartyColor(state, leader)), intensity);
  }

  if (state.overlay === 'party') {
    const target = state.groupsById[state.heatmapPartyId] || state.partiesById[state.heatmapPartyId];
    let groupVotes = 0;
    for (const [pid, v] of Object.entries(votesMap)) {
      const p = state.partiesById[pid];
      const gid = p ? (p.group_id || (state.scenario?.groups?.length ? 'ni' : p.id)) : pid;
      if (pid === state.heatmapPartyId || gid === state.heatmapPartyId) groupVotes += v;
    }
    const share = groupVotes / total;
    const intensity = Math.min(1, share / 0.42);
    return mixColor(neutral, hexToRgb(target?.color || '#1e88e5'), 0.08 + intensity * 0.90);
  }

  if (state.overlay === 'turnout') {
    const effElig = eligibleVoters * Math.max(0.01, reportedPct || 1);
    const turnout = effElig > 0 ? total / effElig : 0;
    const t = Math.max(0.05, Math.min(1, (turnout - 0.35) / 0.45));
    return dark
      ? mixColor([28, 39, 51], [20, 184, 166], t)
      : mixColor([228, 237, 238], [13, 122, 114], t);
  }

  const axisMap = { eu_axis: 2, econ_axis: 0, mig_axis: 5 };
  if (state.overlay in axisMap) {
    const idx = axisMap[state.overlay];
    let weighted = 0;
    for (const [pid, v] of Object.entries(votesMap)) {
      const p = state.partiesById[pid];
      if (p && p.position[idx] !== undefined) weighted += (v / total) * p.position[idx];
    }
    const norm = Math.max(-1, Math.min(1, weighted / 0.55));
    if (state.overlay === 'eu_axis') {
      return norm >= 0
        ? mixColor(centerNeutral, [29, 78, 216], norm)
        : mixColor(centerNeutral, [217, 119, 6], -norm);
    }
    if (state.overlay === 'econ_axis') {
      return norm >= 0
        ? mixColor(centerNeutral, [30, 64, 175], norm)
        : mixColor(centerNeutral, [220, 38, 38], -norm);
    }
    // mig_axis: -1 open (teal) <-> +1 strict (purple/crimson)
    return norm >= 0
      ? mixColor(centerNeutral, [153, 27, 27], norm)
      : mixColor(centerNeutral, [13, 148, 136], -norm);
  }

  return dark ? '#212b36' : '#dfe4e8';
}

export function renderLegend(state, legendEl, partyPickerWrap, colorModeRow) {
  partyPickerWrap.classList.toggle('hidden', state.overlay !== 'party');
  colorModeRow.classList.toggle('hidden', state.overlay !== 'winner' || !(state.scenario?.groups?.length));

  const dark = isDark();
  const baseHex = dark ? '#212b36' : '#dfe4e8';
  const midHex = dark ? '#343f4c' : '#eef1f4';

  if (state.overlay === 'winner') {
    const modeNote = state.colorMode === 'auto'
      ? (state.zoomedIn ? 'National Parties' : 'EU Groups')
      : (state.colorMode === 'group' ? 'EU Groups' : 'National Parties');
    legendEl.innerHTML = `<div class="legend-labels"><span>${modeNote}</span><span>Vivid = Bigger Margin</span></div>`;
  } else if (state.overlay === 'party') {
    const p = state.groupsById[state.heatmapPartyId] || state.partiesById[state.heatmapPartyId];
    legendEl.innerHTML = `
      <div class="legend-bar" style="background:linear-gradient(90deg, ${baseHex}, ${p?.color || '#1e88e5'})"></div>
      <div class="legend-labels"><span>0%</span><span>20%</span><span>40%+</span></div>
    `;
  } else if (state.overlay === 'turnout') {
    const hi = dark ? '#14b8a6' : '#0d7a72';
    legendEl.innerHTML = `
      <div class="legend-bar" style="background:linear-gradient(90deg, ${baseHex}, ${hi})"></div>
      <div class="legend-labels"><span>35%</span><span>58%</span><span>80%+</span></div>
    `;
  } else if (state.overlay === 'eu_axis') {
    legendEl.innerHTML = `
      <div class="legend-bar" style="background:linear-gradient(90deg, #d97706, ${midHex}, #1d4ed8)"></div>
      <div class="legend-labels"><span>Eurosceptic</span><span>Neutral</span><span>Pro-EU</span></div>
    `;
  } else if (state.overlay === 'econ_axis') {
    legendEl.innerHTML = `
      <div class="legend-bar" style="background:linear-gradient(90deg, #dc2626, ${midHex}, #1e40af)"></div>
      <div class="legend-labels"><span>Econ Left</span><span>Center</span><span>Econ Right</span></div>
    `;
  } else if (state.overlay === 'mig_axis') {
    legendEl.innerHTML = `
      <div class="legend-bar" style="background:linear-gradient(90deg, #0d9488, ${midHex}, #991b1b)"></div>
      <div class="legend-labels"><span>Open</span><span>Balanced</span><span>Strict Borders</span></div>
    `;
  }
}
