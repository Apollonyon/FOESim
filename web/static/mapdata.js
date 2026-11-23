// Cartographic European projection, coastlines, and geographic precinct seeds.
// Projection: scaled equirectangular calibrated for central Europe (~48°N).
const MapGeo = (() => {
  const LON_MIN = -11;
  const LAT_MAX = 60;
  const SCALE_X = 28;
  const SCALE_Y = 42;

  function project(lon, lat) {
    return [(lon - LON_MIN) * SCALE_X, (LAT_MAX - lat) * SCALE_Y];
  }

  function ringToPath(ring) {
    return ring
      .map(([lon, lat], idx) => {
        const [x, y] = project(lon, lat);
        return `${idx === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ') + ' Z';
  }

  function multiRingToPath(rings) {
    return rings.map(ringToPath).join(' ');
  }

  // Surrounding European landmasses for geographic context (lon, lat).
  const CONTEXT_COUNTRIES = [
    // Great Britain
    [[[-5.5, 50.1], [-3.0, 51.3], [-4.8, 51.7], [-3.1, 53.4], [-4.8, 55.0], [-5.8, 57.5], [-3.2, 58.6], [-1.8, 57.6], [-2.0, 55.8], [0.2, 53.5], [1.7, 52.5], [1.4, 51.1], [-2.5, 50.6]]],
    // Ireland
    [[[-10.1, 51.8], [-9.9, 53.9], [-7.5, 55.2], [-5.5, 54.5], [-6.1, 52.3], [-8.5, 51.6]]],
    // Spain & Portugal
    [[[-9.3, 38.7], [-8.9, 41.8], [-9.2, 43.1], [-5.0, 43.5], [-1.8, 43.4], [0.5, 42.7], [3.2, 42.4], [1.2, 40.9], [-0.4, 39.4], [-0.8, 37.6], [-5.4, 36.0], [-8.9, 37.0]]],
    // Belgium
    [[[2.55, 51.1], [3.4, 51.35], [4.9, 51.45], [5.8, 51.15], [6.15, 50.6], [6.0, 49.55], [4.9, 49.75], [3.2, 50.7]]],
    // Netherlands
    [[[3.4, 51.35], [4.5, 52.2], [4.9, 53.2], [7.2, 53.25], [7.0, 52.1], [6.0, 51.8], [5.8, 51.15], [4.9, 51.45]]],
    // Luxembourg
    [[[5.75, 49.55], [6.15, 50.15], [6.5, 49.8], [6.35, 49.45]]],
    // Switzerland
    [[[6.0, 46.2], [6.9, 47.4], [8.6, 47.75], [9.6, 47.5], [10.45, 46.9], [9.9, 46.2], [8.9, 45.85], [7.6, 45.95], [6.8, 46.0]]],
    // Austria
    [[[9.55, 47.5], [10.5, 47.5], [12.1, 47.7], [13.0, 47.9], [13.8, 48.75], [16.95, 48.6], [17.1, 47.75], [16.1, 46.85], [13.7, 46.52], [12.2, 46.7], [10.45, 46.85]]],
    // Czechia
    [[[12.1, 50.3], [14.3, 51.05], [16.8, 50.4], [18.85, 49.55], [16.95, 48.65], [14.3, 48.55], [13.8, 48.75], [12.7, 49.5]]],
    // Poland
    [[[14.2, 53.9], [16.5, 54.45], [18.8, 54.7], [22.8, 54.35], [23.9, 52.5], [24.0, 50.6], [22.7, 49.1], [18.85, 49.55], [16.8, 50.4], [14.8, 51.1], [14.5, 52.5]]],
    // Denmark
    [[[8.1, 55.5], [8.6, 57.1], [10.6, 57.7], [10.9, 56.1], [9.8, 54.9], [8.6, 54.9]]],
    [[[11.0, 55.6], [12.6, 56.0], [12.4, 55.2], [11.2, 55.2]]],
    // Southern Sweden
    [[[11.2, 58.9], [14.0, 59.6], [18.8, 59.5], [17.5, 57.2], [15.8, 56.1], [13.0, 55.4], [12.6, 56.3], [11.8, 57.7]]],
    // Southern Norway
    [[[5.0, 59.8], [9.0, 59.8], [11.2, 58.9], [8.5, 58.0], [5.8, 58.8]]],
    // Slovakia & Hungary
    [[[16.95, 48.65], [18.85, 49.55], [22.5, 49.0], [22.1, 48.4], [19.8, 48.1], [17.1, 47.9]]],
    [[[16.1, 46.85], [17.1, 47.9], [20.5, 48.5], [22.8, 48.1], [21.3, 46.2], [18.8, 45.8], [16.5, 46.4]]],
    // Slovenia & Croatia & Balkans
    [[[13.6, 46.5], [16.1, 46.85], [16.5, 46.4], [15.4, 45.5], [13.7, 45.55]]],
    [[[13.6, 45.5], [16.5, 46.4], [19.2, 45.8], [18.6, 44.8], [16.4, 43.5], [18.5, 42.4], [17.0, 42.9], [14.5, 44.8]]],
    [[[16.4, 43.5], [19.2, 45.8], [22.7, 44.6], [22.4, 42.2], [19.5, 41.2], [18.5, 42.4]]],
    // Greece
    [[[19.5, 40.2], [22.8, 41.2], [26.0, 41.0], [24.0, 39.0], [24.1, 37.6], [22.4, 36.5], [20.5, 38.8]]],
  ];

  // Active simulated regions with accurate coastlines (lon, lat),
  // default zoom focus viewBox, and realistic geographic precinct seed points.
  const REGIONS = {
    de: {
      labelPos: [10.4, 51.3],
      focusBox: [435, 195, 315, 365],
      rings: [
        [
          [8.6, 54.9], [9.8, 54.85], [10.9, 54.3], [12.1, 54.15], [13.8, 54.1],
          [14.3, 53.6], [14.4, 52.6], [14.8, 51.3], [14.7, 50.85], [13.5, 50.65],
          [12.2, 50.25], [12.6, 49.55], [13.8, 48.75], [13.0, 47.9], [12.9, 47.45],
          [11.3, 47.4], [10.2, 47.3], [9.6, 47.55], [8.5, 47.7], [7.6, 47.6],
          [7.8, 48.6], [8.2, 49.0], [7.0, 49.15], [6.4, 49.5], [6.15, 50.15],
          [6.0, 50.8], [6.2, 51.8], [7.0, 52.2], [7.2, 53.35], [8.6, 53.85]
        ]
      ],
      // 16 German Bundesländer geographic centers
      seeds: [
        { name: 'Schleswig-Holstein', lon: 9.8, lat: 54.2 },
        { name: 'Hamburg', lon: 10.0, lat: 53.55 },
        { name: 'Mecklenburg-Vorpommern', lon: 12.6, lat: 53.7 },
        { name: 'Bremen', lon: 8.8, lat: 53.1 },
        { name: 'Lower Saxony', lon: 9.5, lat: 52.7 },
        { name: 'Brandenburg', lon: 13.7, lat: 52.2 },
        { name: 'Berlin', lon: 13.4, lat: 52.52 },
        { name: 'Saxony-Anhalt', lon: 11.7, lat: 51.9 },
        { name: 'North Rhine-Westphalia', lon: 7.4, lat: 51.45 },
        { name: 'Hesse', lon: 9.0, lat: 50.65 },
        { name: 'Thuringia', lon: 11.0, lat: 50.85 },
        { name: 'Saxony', lon: 13.4, lat: 51.05 },
        { name: 'Rhineland-Palatinate', lon: 7.3, lat: 49.95 },
        { name: 'Saarland', lon: 6.95, lat: 49.35 },
        { name: 'Baden-Württemberg', lon: 9.0, lat: 48.55 },
        { name: 'Bavaria', lon: 11.5, lat: 48.85 }
      ]
    },

    fr: {
      labelPos: [2.4, 46.7],
      focusBox: [145, 345, 425, 420],
      rings: [
        // Metropolitan France
        [
          [2.55, 51.08], [3.2, 50.7], [4.9, 49.75], [5.85, 49.52], [6.4, 49.45],
          [7.1, 49.1], [8.2, 49.0], [7.6, 47.6], [7.0, 47.4], [6.0, 46.2],
          [6.85, 45.9], [6.9, 44.9], [7.65, 43.8], [6.6, 43.2], [5.1, 43.3],
          [3.6, 43.3], [3.15, 42.45], [1.4, 42.65], [-0.4, 42.8], [-1.75, 43.38],
          [-1.35, 44.7], [-1.2, 46.0], [-2.5, 47.3], [-4.75, 48.35], [-3.3, 48.8],
          [-1.8, 48.7], [-1.85, 49.65], [-0.5, 49.35], [0.1, 49.55], [1.6, 50.85]
        ],
        // Corsica
        [
          [9.4, 42.98], [9.55, 42.1], [9.25, 41.38], [8.7, 41.9], [8.65, 42.55]
        ]
      ],
      // 14 French regions (13 metropolitan + Corsica)
      seeds: [
        { name: 'Hauts-de-France', lon: 2.8, lat: 50.2 },
        { name: 'Normandy', lon: 0.1, lat: 49.1 },
        { name: 'Île-de-France', lon: 2.45, lat: 48.75 },
        { name: 'Grand Est', lon: 5.9, lat: 48.7 },
        { name: 'Brittany', lon: -2.9, lat: 48.2 },
        { name: 'Pays de la Loire', lon: -0.9, lat: 47.4 },
        { name: 'Centre-Val de Loire', lon: 1.7, lat: 47.45 },
        { name: 'Bourgogne-Franche-Comté', lon: 4.9, lat: 47.1 },
        { name: 'Nouvelle-Aquitaine North', lon: -0.2, lat: 45.8 },
        { name: 'Auvergne-Rhône-Alpes', lon: 4.5, lat: 45.45 },
        { name: 'Nouvelle-Aquitaine South', lon: -0.5, lat: 43.9 },
        { name: 'Occitanie', lon: 2.1, lat: 43.7 },
        { name: 'Provence-Alpes-Côte d’Azur', lon: 6.0, lat: 43.9 },
        { name: 'Corsica', lon: 9.1, lat: 42.15 }
      ]
    },

    it: {
      labelPos: [12.5, 43.2],
      focusBox: [470, 520, 395, 460],
      rings: [
        // Mainland Italy
        [
          [7.6, 43.8], [6.8, 44.8], [6.9, 45.9], [7.9, 46.1], [8.9, 45.9],
          [9.9, 46.2], [10.5, 46.85], [12.2, 47.05], [13.7, 46.52], [13.6, 45.65],
          [12.3, 45.45], [12.5, 44.3], [13.8, 43.6], [14.7, 42.1], [16.1, 41.85],
          [18.5, 40.15], [18.0, 39.8], [16.9, 40.4], [16.6, 39.5], [17.1, 38.9],
          [16.0, 37.95], [15.65, 38.2], [15.9, 39.2], [14.9, 40.2], [13.9, 40.9],
          [12.2, 41.8], [10.9, 42.8], [9.8, 44.1], [8.9, 44.4]
        ],
        // Sicily
        [
          [12.45, 37.8], [13.5, 38.15], [15.6, 38.25], [15.1, 36.7], [13.6, 37.3]
        ],
        // Sardinia
        [
          [8.3, 40.9], [9.4, 41.2], [9.75, 40.1], [9.5, 39.1], [8.4, 39.0], [8.2, 40.1]
        ]
      ],
      // 12 Italian geographic districts
      seeds: [
        { name: 'Piedmont & Aosta', lon: 7.8, lat: 45.1 },
        { name: 'Lombardy', lon: 9.6, lat: 45.65 },
        { name: 'Veneto & Friuli', lon: 12.1, lat: 45.9 },
        { name: 'Liguria', lon: 8.8, lat: 44.35 },
        { name: 'Emilia-Romagna', lon: 11.1, lat: 44.5 },
        { name: 'Tuscany', lon: 11.2, lat: 43.4 },
        { name: 'Marche & Umbria', lon: 12.9, lat: 43.2 },
        { name: 'Lazio & Abruzzo', lon: 13.1, lat: 41.95 },
        { name: 'Campania', lon: 14.7, lat: 40.9 },
        { name: 'Puglia & Basilicata', lon: 16.6, lat: 40.85 },
        { name: 'Calabria & Sicily', lon: 14.6, lat: 37.85 },
        { name: 'Sardinia', lon: 9.0, lat: 40.05 }
      ]
    }
  };

  // Clip polygon `poly` against half-plane (P - M) · N >= 0 using Sutherland-Hodgman.
  function clipHalfPlane(poly, mx, my, nx, ny) {
    if (poly.length === 0) return [];
    const out = [];
    for (let i = 0; i < poly.length; i++) {
      const a = poly[i];
      const b = poly[(i + 1) % poly.length];
      const da = (a[0] - mx) * nx + (a[1] - my) * ny;
      const db = (b[0] - mx) * nx + (b[1] - my) * ny;
      const aIn = da >= 0;
      const bIn = db >= 0;

      if (aIn && bIn) {
        out.push(b);
      } else if (aIn && !bIn) {
        const t = da / (da - db);
        out.push([a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1])]);
      } else if (!aIn && bIn) {
        const t = da / (da - db);
        out.push([a[0] + t * (b[0] - a[0]), a[1] + t * (b[1] - a[1])]);
        out.push(b);
      }
    }
    return out;
  }

  // Generate crisp Voronoi precinct polygons for a region (clipped by SVG clipPath).
  function buildPrecinctPolygons(regionId, count) {
    const def = REGIONS[regionId];
    if (!def) return [];

    // Project country bounding box with generous padding.
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const ring of def.rings) {
      for (const [lon, lat] of ring) {
        const [x, y] = project(lon, lat);
        if (x < minX) minX = x;
        if (y < minY) minY = y;
        if (x > maxX) maxX = x;
        if (y > maxY) maxY = y;
      }
    }
    const pad = 40;
    const bbox = [
      [minX - pad, minY - pad],
      [maxX + pad, minY - pad],
      [maxX + pad, maxY + pad],
      [minX - pad, maxY + pad]
    ];

    // Match the scenario's precinct count (fall back to grid seeds if count differs).
    const seeds = [];
    for (let i = 0; i < count; i++) {
      if (i < def.seeds.length) {
        const s = def.seeds[i];
        const [x, y] = project(s.lon, s.lat);
        seeds.push({ id: `${regionId}-${i + 1}`, label: s.name, x, y });
      } else {
        const cols = Math.ceil(Math.sqrt(count));
        const row = Math.floor(i / cols);
        const col = i % cols;
        const x = minX + ((col + 0.5) / cols) * (maxX - minX);
        const y = minY + ((row + 0.5) / Math.ceil(count / cols)) * (maxY - minY);
        seeds.push({ id: `${regionId}-${i + 1}`, label: `Precinct ${i + 1}`, x, y });
      }
    }

    return seeds.map((site, i) => {
      let poly = bbox;
      for (let j = 0; j < seeds.length; j++) {
        if (i === j) continue;
        const other = seeds[j];
        const mx = (site.x + other.x) / 2;
        const my = (site.y + other.y) / 2;
        const nx = site.x - other.x;
        const ny = site.y - other.y;
        poly = clipHalfPlane(poly, mx, my, nx, ny);
        if (poly.length === 0) break;
      }
      const d = poly.length
        ? poly.map(([x, y], idx) => `${idx === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`).join(' ') + ' Z'
        : '';
      return { id: site.id, label: site.label, x: site.x, y: site.y, d };
    });
  }

  return {
    project,
    multiRingToPath,
    CONTEXT_COUNTRIES,
    REGIONS,
    buildPrecinctPolygons,
    DEFAULT_VIEWBOX: [140, 155, 760, 790]
  };
})();
