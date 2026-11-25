// Stateless URL diff encoding/decoding for shareable scenario links (#share=...).

const UNSAFE_KEYS = new Set(['__proto__', 'constructor', 'prototype']);

function isPlainObject(val) {
  return Boolean(val) && typeof val === 'object' && !Array.isArray(val);
}

function deepEqual(a, b) {
  if (Object.is(a, b)) return true;
  if (typeof a !== typeof b || a === null || b === null) return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a)) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (!deepEqual(a[i], b[i])) return false;
    }
    return true;
  }
  if (typeof a === 'object') {
    const ka = Object.keys(a);
    const kb = Object.keys(b);
    if (ka.length !== kb.length) return false;
    for (const k of ka) {
      if (!Object.prototype.hasOwnProperty.call(b, k) || !deepEqual(a[k], b[k])) return false;
    }
    return true;
  }
  return false;
}

function diffRegionsArray(baseRegions, curRegions) {
  if (!Array.isArray(baseRegions) || !Array.isArray(curRegions) || baseRegions.length !== curRegions.length) {
    return structuredClone(curRegions);
  }
  const byId = {};
  for (let i = 0; i < baseRegions.length; i++) {
    const br = baseRegions[i];
    const cr = curRegions[i];
    if (!br?.id || br.id !== cr?.id) {
      return structuredClone(curRegions);
    }
    const rPatch = diffObject(br, cr);
    if (Object.keys(rPatch).length > 0) {
      byId[br.id] = rPatch;
    }
  }
  return Object.keys(byId).length > 0 ? { $byId: byId } : undefined;
}

function diffObject(baseObj, curObj) {
  const patch = {};
  const keys = new Set([...Object.keys(baseObj || {}), ...Object.keys(curObj || {})]);
  for (const k of keys) {
    if (UNSAFE_KEYS.has(k)) continue;
    const inBase = baseObj != null && Object.prototype.hasOwnProperty.call(baseObj, k);
    const inCur = curObj != null && Object.prototype.hasOwnProperty.call(curObj, k);

    if (inBase && !inCur) {
      patch[k] = { $del: true };
      continue;
    }
    if (!inBase && inCur) {
      if (curObj[k] !== undefined) patch[k] = structuredClone(curObj[k]);
      continue;
    }

    const bv = baseObj[k];
    const cv = curObj[k];
    if (deepEqual(bv, cv)) continue;

    if (k === 'regions' && Array.isArray(bv) && Array.isArray(cv)) {
      const regDiff = diffRegionsArray(bv, cv);
      if (regDiff !== undefined) patch[k] = regDiff;
      continue;
    }

    if (isPlainObject(bv) && isPlainObject(cv)) {
      const sub = diffObject(bv, cv);
      if (Object.keys(sub).length > 0) patch[k] = sub;
      continue;
    }

    patch[k] = structuredClone(cv);
  }
  return patch;
}

function applyObjectPatch(target, patch) {
  if (!isPlainObject(patch)) return;
  for (const [k, val] of Object.entries(patch)) {
    if (UNSAFE_KEYS.has(k)) continue;
    if (isPlainObject(val) && val.$del === true) {
      delete target[k];
      continue;
    }
    if (k === 'regions' && isPlainObject(val) && isPlainObject(val.$byId) && Array.isArray(target.regions)) {
      for (const r of target.regions) {
        const rp = val.$byId[r.id];
        if (rp) applyObjectPatch(r, rp);
      }
      continue;
    }
    if (isPlainObject(val) && isPlainObject(target[k])) {
      applyObjectPatch(target[k], val);
      continue;
    }
    target[k] = structuredClone(val);
  }
}

export function computeScenarioPatch(baseScenario, currentScenario) {
  return diffObject(baseScenario, currentScenario);
}

export function applyScenarioPatch(baseScenario, patch) {
  const out = structuredClone(baseScenario);
  applyObjectPatch(out, patch);
  return out;
}

function bytesToBase64Url(bytes) {
  let bin = '';
  for (let i = 0; i < bytes.length; i++) {
    bin += String.fromCharCode(bytes[i]);
  }
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function base64UrlToBytes(str) {
  const padLen = (4 - (str.length % 4)) % 4;
  const b64 = str.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat(padLen);
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) {
    bytes[i] = bin.charCodeAt(i);
  }
  return bytes;
}

const MAX_SHARE_HASH_LEN = 32768;
const MAX_SHARE_DECOMPRESSED_BYTES = 2 * 1024 * 1024;

async function streamTransformBytes(bytes, transform, maxBytes = MAX_SHARE_DECOMPRESSED_BYTES) {
  const writer = transform.writable.getWriter();
  const writePromise = writer.write(bytes).then(() => writer.close()).catch(() => {});
  const reader = transform.readable.getReader();
  const chunks = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maxBytes) {
      await reader.cancel().catch(() => {});
      throw new Error('Share link payload exceeds maximum size');
    }
    chunks.push(value);
  }
  await writePromise;
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return out;
}

export async function encodeSharePayload(payload) {
  const utf8 = new TextEncoder().encode(JSON.stringify(payload));
  if (typeof CompressionStream === 'function') {
    try {
      const compressed = await streamTransformBytes(utf8, new CompressionStream('deflate-raw'));
      return `z.${bytesToBase64Url(compressed)}`;
    } catch {
      // Fall back to uncompressed UTF-8 base64url.
    }
  }
  return `j.${bytesToBase64Url(utf8)}`;
}

export async function decodeSharePayload(raw) {
  if (typeof raw !== 'string' || raw.length < 3 || raw.length > MAX_SHARE_HASH_LEN) {
    throw new Error('Invalid or oversized share payload');
  }
  let bytes;
  if (raw.startsWith('z.')) {
    if (typeof DecompressionStream !== 'function') {
      throw new Error('Browser cannot decompress deflated share links');
    }
    bytes = await streamTransformBytes(
      base64UrlToBytes(raw.slice(2)),
      new DecompressionStream('deflate-raw'),
      MAX_SHARE_DECOMPRESSED_BYTES
    );
  } else if (raw.startsWith('j.')) {
    bytes = base64UrlToBytes(raw.slice(2));
  } else {
    throw new Error('Unknown share payload format');
  }

  const parsed = JSON.parse(new TextDecoder().decode(bytes));
  if (!isPlainObject(parsed) || parsed.v !== 1 || !isPlainObject(parsed.patch)) {
    throw new Error('Invalid share payload structure');
  }
  return parsed;
}
