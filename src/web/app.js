'use strict';

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];

let cfg = {};
let status = {};
const stations = new Map(); // call -> {s, marker, track}
let messages = [];
let unread = 0;
let pickMode = false;

// ---------- ユーティリティ ----------
function esc(v) {
  return String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function toast(msg, ms = 2500) {
  const t = $('#toast');
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toast.tm);
  toast.tm = setTimeout(() => (t.hidden = true), ms);
}
function ago(ms) {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000));
  if (s < 60) return s + '秒';
  if (s < 3600) return Math.floor(s / 60) + '分';
  if (s < 86400) return Math.floor(s / 3600) + '時間';
  return Math.floor(s / 86400) + '日';
}
function hhmmss(ms) {
  const d = new Date(ms);
  return d.toLocaleTimeString('ja-JP', { hour12: false });
}
function mdhhmmss(ms) {
  const d = new Date(ms);
  return (d.getMonth() + 1) + '/' + d.getDate() + ' ' + hhmmss(ms);
}
function distKm(lat1, lon1, lat2, lon2) {
  const r = Math.PI / 180;
  const a = Math.sin((lat2 - lat1) * r / 2) ** 2 + Math.cos(lat1 * r) * Math.cos(lat2 * r) * Math.sin((lon2 - lon1) * r / 2) ** 2;
  return 12742 * Math.asin(Math.min(1, Math.sqrt(a)));
}
function bearing(lat1, lon1, lat2, lon2) {
  const r = Math.PI / 180;
  const y = Math.sin((lon2 - lon1) * r) * Math.cos(lat2 * r);
  const x = Math.cos(lat1 * r) * Math.sin(lat2 * r) - Math.sin(lat1 * r) * Math.cos(lat2 * r) * Math.cos((lon2 - lon1) * r);
  return (Math.atan2(y, x) / r + 360) % 360;
}
async function api(path, body) {
  const opt = body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
  let r;
  try {
    r = await fetch(path, opt);
  } catch {
    throw new Error('アプリ本体 (KISAXI) に接続できません。アプリが起動しているか確認してください');
  }
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

// ---------- APRS シンボル ----------
// hessu/aprs-symbols のスプライト (16列 x 6行, 24px)
function symHTML(sym) {
  let table = (sym || '')[0] || '/';
  let code = (sym || '')[1] || '/';
  let idx = code.charCodeAt(0) - 33;
  if (idx < 0 || idx > 93) { table = '/'; idx = 0; }
  const pos = i => `-${(i % 16) * 24}px -${Math.floor(i / 16) * 24}px`;
  const sheet = table === '/' ? 0 : 1;
  let ov = '';
  if (table !== '/' && table !== '\\') {
    const oi = table.charCodeAt(0) - 33;
    if (oi >= 0 && oi <= 93) ov = `<span class="ov" style="background-image:url(sym/aprs-symbols-24-2.png);background-position:${pos(oi)}"></span>`;
  }
  return `<span class="sym" style="background-image:url(sym/aprs-symbols-24-${sheet}.png);background-position:${pos(idx)}">${ov}</span>`;
}

// ---------- 地図 ----------
const map = L.map('map', { zoomControl: true, preferCanvas: false }).setView([35.6812, 139.7671], 9);
L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
  maxZoom: 19,
  attribution: '&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank">OpenStreetMap</a> contributors',
}).addTo(map);
L.control.scale({ imperial: false }).addTo(map);
const trackLayer = L.layerGroup().addTo(map);
const ambigLayer = L.layerGroup().addTo(map);
const rangeLayer = L.layerGroup().addTo(map);
const routeLayer = L.layerGroup().addTo(map);
const markerLayer = L.layerGroup().addTo(map);
// 位置曖昧さレベル 1〜4 の範囲の一辺 (度): 0.1分 / 1分 / 10分 / 1度
const AMBIG_DEG = [0, 0.1 / 60, 1 / 60, 10 / 60, 1];
let mapCentered = false;

function makeIcon(s) {
  const own = s.via === 'LOCAL';
  const rf = s.lastRf && Date.now() - s.lastRf < 3600e3;
  const cls = 'stn-icon' + (own ? ' own' : '') + (rf ? ' rf' : '');
  return L.divIcon({
    className: '',
    html: `<div class="${cls}">${symHTML(s.sym)}<span class="label">${esc(s.call)}</span></div>`,
    iconSize: [24, 24],
    iconAnchor: [12, 12],
    popupAnchor: [0, -12],
  });
}

// ---------- 気象局 (WX) ----------
const DIR16 = ['北', '北北東', '北東', '東北東', '東', '東南東', '南東', '南南東', '南', '南南西', '南西', '西南西', '西', '西北西', '北西', '北北西'];
const WX_ICON = {
  rain: '<path d="M7 14.5a3.8 3.8 0 0 1 .4-7.6 5 5 0 0 1 9.5 1.3 3.2 3.2 0 0 1 0 6.3z"/><path d="M8.5 17.5l-1 2.5M12.5 17.5l-1 2.5M16.5 17.5l-1 2.5"/>',
  temp: '<path d="M10 4.5a2 2 0 0 1 4 0v9.7a4 4 0 1 1-4 0z"/><path d="M12 9v7"/><circle cx="12" cy="17.3" r="1.6" fill="currentColor"/>',
  dir: '<circle cx="12" cy="12" r="9"/><path d="M12 3v1.8M12 19.2V21M3 12h1.8M19.2 12H21" stroke-width="1.2"/>',
  wind: '<path d="M3 8.5h10.5a2.7 2.7 0 1 0-2.7-2.7M3 12.5h14.5a2.7 2.7 0 1 1-2.7 2.7M3 16.5h6"/>',
  pres: '<path d="M4 16.5a8 8 0 1 1 16 0"/><path d="M12 16.5l3.8-4.6"/><circle cx="12" cy="16.5" r="1.3" fill="currentColor"/><path d="M6.3 10.8l1.2.9M12 7.5v1.4M17.7 10.8l-1.2.9" stroke-width="1.2"/>',
  hum: '<path d="M12 3.5c3.2 4.2 6 7.6 6 10.8a6 6 0 0 1-12 0c0-3.2 2.8-6.6 6-10.8z"/><path d="M9.5 15.5a2.6 2.6 0 0 0 2.5 2" stroke-width="1.2"/>',
};
function wxIcon(kind, rotate) {
  // 風向は「風が吹いていく向き」を矢印で示す (APRS の風向は吹いてくる方向)
  const arrow = rotate == null ? '' : `<g transform="rotate(${rotate} 12 12)"><path d="M12 6.5v11M8.8 14.3L12 17.5l3.2-3.2"/></g>`;
  return `<svg viewBox="0 0 24 24" width="26" height="26" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${WX_ICON[kind]}${arrow}</svg>`;
}
function wxHTML(s) {
  const w = s.wx;
  if (!w) return '';
  if (Object.keys(w).length === 0) return '<div class="wx"><div class="wx-head"><span>気象情報</span><span class="wx-time">観測値なし</span></div></div>';
  const f = (v, d = 1) => (v == null ? null : Number(v).toFixed(d));
  const dash = '—';
  const tiles = [
    ['rain', '雨量 (1時間)', w.rain1h != null ? `${f(w.rain1h)} mm` : dash,
      w.rain24h != null ? `24時間 ${f(w.rain24h)} mm` : w.rainMidnight != null ? `0時から ${f(w.rainMidnight)} mm` : ''],
    ['temp', '気温', w.temp != null ? `${f(w.temp)} ℃` : dash, ''],
    ['dir', '風向', w.windDir != null ? DIR16[Math.round(w.windDir / 22.5) % 16] : dash, w.windDir != null ? `${w.windDir}°` : ''],
    ['wind', '風速', w.wind != null ? `${f(w.wind)} m/s` : dash, w.gust != null ? `最大瞬間 ${f(w.gust)} m/s` : ''],
    ['pres', '気圧', w.pressure != null ? `${f(w.pressure)} hPa` : dash, ''],
    ['hum', '湿度', w.humidity != null ? `${w.humidity} %` : dash, ''],
  ];
  const cells = tiles.map(([k, label, val, sub]) =>
    `<div class="wx-tile wx-${k}">${wxIcon(k, k === 'dir' && w.windDir != null ? w.windDir : null)}` +
    `<div class="wx-label">${label}</div><div class="wx-val">${esc(val)}</div>${sub ? `<div class="wx-sub">${esc(sub)}</div>` : ''}</div>`
  ).join('');
  const t = s.wxTime ? `${hhmmss(s.wxTime)} 受信 (${ago(s.wxTime)}前)` : '';
  return `<div class="wx"><div class="wx-head"><span>気象情報</span><span class="wx-time">${t}</span></div><div class="wx-grid">${cells}</div></div>`;
}

// ---------- PHG デコードと機種判定 ----------
// PHG 数値の意味は APRS101 第 7 章。通達範囲 (km) の式は phgInfo と同じ。
function decodePhg(comment) {
  const m = /PHG(\d)(\d)(\d)(\d)/.exec(comment || '');
  if (!m) return null;
  const [p, h, g, d] = m.slice(1).map(Number);
  const hft = 10 * 2 ** h;
  const km = Math.sqrt(2 * hft * Math.sqrt((p * p) / 10 * (10 ** (g / 10)) / 2)) * 1.609344;
  return { w: p * p, ft: hft, m: hft * 0.3048, g, dir: d * 45, km };
}
function phgText(r) {
  return `${r.w} W / 高さ ${Math.round(r.m)} m (${r.ft} ft) / 利得 ${r.g} dBi / ${r.dir ? r.dir + '° 指向' : '無指向'} → 通達範囲 約 ${r.km.toFixed(1)} km`;
}
// 宛先 (TOCALL) から機種・ソフトを引く。表は tocalls.json (aprs-deviceid、scripts/update-tocalls.py で更新)。
// パターンの ? は任意の1文字、* は任意の文字列。ワイルドカードが少なく文字数の多いものを優先する。
let tocallRules = [];
let tocallClasses = {};
let miceRules = [], miceLegacy = [];
fetch('tocalls.json').then(r => r.json()).then(j => {
  tocallClasses = j.classes || {};
  miceRules = j.mice || [];
  miceLegacy = j.micelegacy || [];
  tocallRules = j.tocalls.map(([pat, vendor, model, cls]) => ({
    re: new RegExp('^' + pat.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\?/g, '.').replace(/\*/g, '.*') + '$'),
    wild: (pat.match(/[?*]/g) || []).length, len: pat.length, vendor, model, cls,
  })).sort((x, y) => x.wild - y.wild || y.len - x.len);
}).catch(() => {});
function deviceText(vendor, model, cls) {
  const c = tocallClasses[cls];
  return [vendor, model].filter(Boolean).join(' ') + (c ? ` (${c})` : '');
}
// 宛先 (TOCALL) か Mic-E のコメント末尾コードで機種を判定する。
function deviceName(raw) {
  const m = /^[^>]+>([^,:]+)[^:]*:([\s\S]*)$/.exec(raw || '');
  if (!m) return '';
  const info = m[2];
  if (info[0] === "'" || info[0] === '`') { // Mic-E: 宛先は位置情報なので、コメント (info の10文字目以降) を見る
    const c = info.slice(9);
    const two = miceRules.find(r => c.length >= 2 && c.endsWith(r[0]));
    if (two) return deviceText(two[1], two[2], two[3]);
    const l = miceLegacy.filter(r => c[0] === r[0]);
    const one = l.find(r => r[1] && c.length > 1 && c.endsWith(r[1])) || l.find(r => !r[1]);
    return one ? deviceText(one[2], one[3], one[4]) : '';
  }
  const dst = m[1].toUpperCase();
  const r = tocallRules.find(r => r.re.test(dst));
  return r ? deviceText(r.vendor, r.model, r.cls) : '';
}

function popupHTML(s) {
  const rows = [];
  const add = (k, v) => { if (v !== '' && v != null) rows.push(`<tr><td>${k}</td><td>${v}</td></tr>`); };
  if (s.object) add(s.type === 'item' ? 'アイテム' : 'オブジェクト', '送信元 ' + esc(s.owner) + (s.killed ? ' (削除)' : ''));
  add('位置', fmtDM(s.lat, 'NS') + ' ' + fmtDM(s.lon, 'EW'));
  if (cfg.lat || cfg.lon) {
    const d = distKm(cfg.lat, cfg.lon, s.lat, s.lon);
    add('距離/方位', `${d.toFixed(1)} km / ${Math.round(bearing(cfg.lat, cfg.lon, s.lat, s.lon))}°`);
  }
  if (s.hasCs && (s.speed > 0 || s.course > 0)) add('速度/進行方向', `${Math.round(s.speed)} km/h / ${s.course}°`);
  if (s.hasAlt) add('高度', `${Math.round(s.alt)} m`);
  if (s.micE) add('Mic-E', esc(s.micE));
  add('コメント', esc(s.comment));
  const phg = decodePhg(s.comment);
  if (phg) add('PHG', esc(phgText(phg)));
  add('機種/ソフト', esc(deviceName(s.raw)));
  add('ステータス', esc(s.status));
  add('経由', `<span class="via-${esc(s.via)}">${esc(s.via)}</span>` + (s.path ? ' ' + esc(s.path) : ''));
  add('最終受信', `${new Date(s.last).toLocaleString('ja-JP')} (${ago(s.last)}前) / ${s.count}回`);
  const msgBtn = s.object || s.via === 'LOCAL' ? '' : `<button data-msgto="${esc(s.call)}">メッセージを送る</button>`;
  return `<div class="popup"><h3>${symHTML(s.sym)} ${esc(s.call)}</h3>${wxHTML(s)}<table>${rows.join('')}</table><div class="raw">${esc(s.raw)}</div>${msgBtn}</div>`;
}

// ---------- 重なったアイコンを広げる ----------
const SPIDER_HIT_PX = 16; // 画面上でこの距離以内にあるアイコンは「重なっている」とみなす
const spiderLayer = L.layerGroup().addTo(map);
let spider = null; // { items: Map(call -> 元の位置) }

function spiderCollapse() {
  if (!spider) return;
  for (const [call, ll] of spider.items) {
    const x = stations.get(call);
    if (x && x.marker) x.marker.setLatLng(ll);
  }
  spiderLayer.clearLayers();
  spider = null;
}

function onMarkerClick(call) {
  const e = stations.get(call);
  if (!e || !e.marker) return;
  if (spider && spider.items.has(call)) { e.marker.openPopup(); return; }
  spiderCollapse();
  const here = map.latLngToContainerPoint(e.marker.getLatLng());
  const group = [];
  for (const [c, x] of stations) {
    if (!x.marker || !markerLayer.hasLayer(x.marker)) continue;
    if (map.latLngToContainerPoint(x.marker.getLatLng()).distanceTo(here) <= SPIDER_HIT_PX) group.push(c);
  }
  if (group.length < 2) { e.marker.openPopup(); return; }
  map.closePopup();
  const n = group.length;
  const radius = Math.max(36, 11 * n);
  spider = { items: new Map() };
  const center = e.marker.getLatLng();
  group.forEach((c, i) => {
    const x = stations.get(c);
    const orig = x.marker.getLatLng();
    const a = -Math.PI / 2 + (2 * Math.PI * i) / n;
    const pt = here.add(L.point(Math.cos(a) * radius, Math.sin(a) * radius));
    const ll = map.containerPointToLatLng(pt);
    spider.items.set(c, orig);
    x.marker.setLatLng(ll);
    L.polyline([center, ll], { color: '#555', weight: 1.5, opacity: 0.8, interactive: false }).addTo(spiderLayer);
  });
  L.circleMarker(center, { radius: 3, color: '#555', fillColor: '#555', fillOpacity: 1, weight: 1, interactive: false }).addTo(spiderLayer);
}
map.on('zoomstart', spiderCollapse);
document.addEventListener('keydown', ev => { if (ev.key === 'Escape') spiderCollapse(); });

function stationVisible(s) {
  if (!$('#opt-mapfilter').checked) return true;
  return matchFilter(s);
}

function drawStation(call) {
  const e = stations.get(call);
  if (!e) return;
  const s = e.s;
  const show = s.hasPos && !s.killed && stationVisible(s);
  if (!show) {
    if (spider && spider.items.has(call)) spiderCollapse();
    if (e.marker) { markerLayer.removeLayer(e.marker); e.marker = null; }
    if (e.track) { trackLayer.removeLayer(e.track); e.track = null; }
    if (e.ambig) { ambigLayer.removeLayer(e.ambig); e.ambig = null; }
    return;
  }
  const iconKey = s.sym + '|' + s.via + '|' + (s.lastRf && Date.now() - s.lastRf < 3600e3);
  if (!e.marker) {
    e.marker = L.marker([s.lat, s.lon], { icon: makeIcon(s), zIndexOffset: s.via === 'LOCAL' ? 1000 : 0 });
    e.marker.bindPopup(() => popupHTML(stations.get(call).s), { maxWidth: 380, minWidth: s.wx ? 330 : 50 });
    e.marker.addTo(markerLayer);
    // 重なったアイコンはクリックで放射状に広げ、広げた後のクリックで個別のポップアップを開く
    e.marker.off('click', e.marker._openPopup, e.marker);
    e.marker.on('click', () => onMarkerClick(call));
    // 位置あいまいの範囲は、マーカーにカーソルを乗せている間 (とポップアップ表示中) だけ描く
    e.marker.on('mouseover', () => hoverRoute(call, true));
    e.marker.on('mouseout', () => hoverRoute(call, false));
    e.marker.on('mouseover', () => { const x = stations.get(call); if (x && x.ambig) ambigLayer.addLayer(x.ambig); });
    e.marker.on('mouseout', () => { const x = stations.get(call); if (x && x.ambig && !x.marker.isPopupOpen()) ambigLayer.removeLayer(x.ambig); });
    e.marker.on('popupclose', () => { const x = stations.get(call); if (x && x.ambig) ambigLayer.removeLayer(x.ambig); });
  } else {
    if (!(spider && spider.items.has(call))) e.marker.setLatLng([s.lat, s.lon]);
    if (e.iconKey !== iconKey) e.marker.setIcon(makeIcon(s));
    if (e.marker.isPopupOpen()) e.marker.setPopupContent(popupHTML(s));
  }
  e.iconKey = iconKey;
  if (s.ambig) {
    // マーカー (範囲の中心) を中心に、曖昧さレベルの一辺で描く
    const h = AMBIG_DEG[s.ambig] / 2;
    const b = [[s.lat - h, s.lon - h], [s.lat + h, s.lon + h]];
    if (e.ambig) e.ambig.setBounds(b);
    else e.ambig = L.rectangle(b, { color: '#e0409a', weight: 1.5, fillColor: '#ff7ac0', fillOpacity: 0.25, interactive: false });
  } else if (e.ambig) {
    ambigLayer.removeLayer(e.ambig);
    e.ambig = null;
  }
  const segs = trackSegments(s.track || []);
  if ($('#opt-tracks').checked && segs.length) {
    if (e.track) e.track.setLatLngs(segs);
    else {
      e.track = L.polyline(segs, { color: s.lastRf ? '#0b7a3e' : '#3557b7', weight: 2, opacity: 0.7 }).addTo(trackLayer);
      e.track.on('mousemove', ev => trackHover(call, ev.containerPoint));
      e.track.on('mouseout', () => hoverRouteLayer.clearLayers());
    }
  } else if (e.track) {
    trackLayer.removeLayer(e.track);
    e.track = null;
  }
}

// 軌跡を設定の区切り (日付が変わったら / 一定時間あいたら) で分け、2 点以上ある区間だけを返す
function trackSegments(track) {
  const mode = cfg.trackSplit || 'day';
  const gap = (cfg.trackGapMin || 30) * 60;
  const segs = [];
  let cur = [], prev = null;
  for (const p of track) {
    if (prev) {
      const brk = mode === 'gap' ? p[2] - prev[2] >= gap
        : mode === 'day' ? new Date(p[2] * 1000).toDateString() !== new Date(prev[2] * 1000).toDateString()
        : false;
      if (brk) { if (cur.length > 1) segs.push(cur); cur = []; }
    }
    cur.push([p[0], p[1]]);
    prev = p;
  }
  if (cur.length > 1) segs.push(cur);
  return segs;
}

function upsertStation(s) {
  let e = stations.get(s.call);
  if (!e) { e = { s }; stations.set(s.call, e); }
  else e.s = s;
  drawStation(s.call);
  listDirty = true;
}

function removeStation(call) {
  const e = stations.get(call);
  if (!e) return;
  if (spider && spider.items.has(call)) spiderCollapse();
  if (e.marker) markerLayer.removeLayer(e.marker);
  if (e.track) trackLayer.removeLayer(e.track);
  if (e.ambig) ambigLayer.removeLayer(e.ambig);
  stations.delete(call);
  listDirty = true;
}

map.on('popupopen', ev => {
  const b = ev.popup.getElement().querySelector('button[data-msgto]');
  if (b) b.onclick = () => openMessageTo(b.dataset.msgto);
});

map.on('click', ev => {
  spiderCollapse();
  if (objPicking) {
    objSetPos(ev.latlng.lat, ev.latlng.lng);
    objSetPick(false);
    return;
  }
  if (!pickMode) return;
  const f = $('#cfg-form');
  f.lat.value = ev.latlng.lat.toFixed(5);
  f.lon.value = ev.latlng.lng.toFixed(5);
  setPickMode(false);
  toast('位置を設定しました。「保存して適用」で反映されます');
});
function setPickMode(on) {
  if (on && objPicking) objSetPick(false);
  pickMode = on;
  $('#btn-pickpos').classList.toggle('active', on);
  map.getContainer().classList.toggle('picking', on);
}

// ---------- 局リスト ----------
let listDirty = true;
function matchFilter(s) {
  const q = $('#st-filter').value.trim().toUpperCase();
  const via = $('#st-via').value;
  if (q && !s.call.toUpperCase().includes(q)) return false;
  if (via === 'RF' && !s.lastRf && s.via !== 'LOCAL') return false;
  if (via === 'IS' && s.lastRf) return false;
  return true;
}
function renderList() {
  if (!listDirty) return;
  listDirty = false;
  const all = [...stations.values()].map(e => e.s).sort((a, b) => b.last - a.last);
  const rows = [];
  let n = 0;
  for (const s of all) {
    if (!matchFilter(s)) continue;
    n++;
    if (rows.length >= 500) continue;
    const d = s.hasPos && (cfg.lat || cfg.lon) ? distKm(cfg.lat, cfg.lon, s.lat, s.lon).toFixed(1) : '';
    const via = s.via === 'LOCAL' ? 'LOCAL' : s.lastRf ? 'RF' : 'IS';
    rows.push(`<tr data-call="${esc(s.call)}"><td>${s.sym ? symHTML(s.sym) : ''}</td><td>${esc(s.call)}</td><td class="via-${via}">${via}</td><td class="num">${d}</td><td class="num">${ago(s.last)}</td></tr>`);
  }
  $('#st-table tbody').innerHTML = rows.join('');
  $('#st-count').textContent = `${n} 局`;
}
setInterval(() => { listDirty = true; renderList(); }, 10000);
setInterval(renderList, 1000);

$('#st-table tbody').addEventListener('click', ev => {
  const tr = ev.target.closest('tr');
  if (!tr) return;
  const e = stations.get(tr.dataset.call);
  if (!e) return;
  if (e.s.hasPos) {
    map.setView([e.s.lat, e.s.lon], Math.max(map.getZoom(), 12));
    if (e.marker) e.marker.openPopup();
  } else {
    toast(`${e.s.call}: 位置情報なし`);
  }
});
for (const id of ['#st-filter', '#st-via']) {
  $(id).addEventListener('input', () => {
    listDirty = true;
    renderList();
    if ($('#opt-mapfilter').checked) for (const k of stations.keys()) drawStation(k);
  });
}
$('#opt-mapfilter').addEventListener('change', () => { for (const k of stations.keys()) drawStation(k); });
$('#opt-tracks').addEventListener('change', () => { for (const k of stations.keys()) drawStation(k); });
$('#opt-labels').addEventListener('change', ev => document.body.classList.toggle('nolabels', !ev.target.checked));

// ---------- パケットログ ----------
const pktLog = $('#pkt-log');
function pktCategory(dir) {
  if (dir.startsWith('TX')) return 'TX';
  if (dir.startsWith('GATE')) return 'GATE';
  return dir;
}
// ---------- ゲート経路 (ゲートしたパケットの経路を地図上に一瞬線で表示) ----------
const ROUTE_MS = 6000;
const ROUTE_MAX = 40;
function stationLL(call) {
  const e = stations.get(call);
  return e && e.s.hasPos && !e.s.killed ? [e.s.lat, e.s.lon] : null;
}
const lastRoutes = new Map(); // 送信局 -> {pts, color, t} 直近のゲート経路 (カーソルを合わせたとき表示)
const hoverRouteLayer = L.layerGroup().addTo(map);
function hoverRoute(call, on) {
  hoverRouteLayer.clearLayers();
  const r = on && lastRoutes.get(call);
  if (!r || !$('#opt-gateroute').checked) return;
  // 位置は表示時点の最新で引き直す
  L.polyline(r.calls.map(c => c === '' ? [cfg.lat, cfg.lon] : stationLL(c)).filter(Boolean), { color: r.color, weight: 3, opacity: 0.85, dashArray: '10 8', interactive: false }).addTo(hoverRouteLayer);
}
// 軌跡にカーソルを合わせたとき、いちばん近いポイントを RF で受けたときの経路を表示する
function trackHover(call, cp) {
  const e = stations.get(call);
  hoverRouteLayer.clearLayers();
  if (!e || !$('#opt-gateroute').checked || !(cfg.lat || cfg.lon)) return;
  const tr = e.s.track || [], tp = e.s.trackPath || [];
  const off = tr.length - tp.length; // 旧データは経路が末尾に揃う
  let best = -1, bd = 14; // px
  tr.forEach((pt, i) => {
    const d = map.latLngToContainerPoint([pt[0], pt[1]]).distanceTo(cp);
    if (d < bd) { bd = d; best = i; }
  });
  if (best < 0) return;
  const pt = tr[best], ll = [pt[0], pt[1]];
  const dot = L.circleMarker(ll, { radius: 5, color: '#333', weight: 2, fillColor: '#fff', fillOpacity: 1, interactive: false });
  const t = new Date(pt[2] * 1000).toLocaleString('ja-JP', { hour12: false });
  dot.bindTooltip(t, { direction: 'top', offset: [0, -6] });
  dot.addTo(hoverRouteLayer).openTooltip();
  const path = best - off >= 0 ? tp[best - off] : '';
  if (!path || !path.startsWith('RF')) return;
  const line = [ll];
  for (const h of path.split(',').slice(1)) if (h.endsWith('*')) { const x = stationLL(h.slice(0, -1)); if (x) line.push(x); }
  line.push([cfg.lat, cfg.lon]);
  L.polyline(line, { color: '#0aa5a5', weight: 3, opacity: 0.85, dashArray: '10 8', interactive: false }).addTo(hoverRouteLayer);
}
function showGateRoute(p) {
  if (p.dir !== 'GATE-IS' && p.dir !== 'GATE-RF') return;
  if (!cfg || !(cfg.lat || cfg.lon)) return;
  const me = [cfg.lat, cfg.lon];
  const isRF = p.dir === 'GATE-RF'; // IS→RF は元の送信局が "}" の後ろ
  let hdr = p.raw.slice(0, p.raw.indexOf(':'));
  let src;
  const pts = [], via = [];
  if (isRF) {
    const m = /:\}([^>:]+)>/.exec(p.raw);
    src = m && m[1];
  } else {
    const hops = hdr.split(',');
    src = hops[0].split('>')[0];
    for (const h of hops.slice(1)) {
      if (/^q[A-Z]{2}$/.test(h)) break;
      if (h.endsWith('*')) { const ll = stationLL(h.slice(0, -1)); if (ll) { pts.push(ll); via.push(h.slice(0, -1)); } }
    }
  }
  const sll = src && stationLL(src);
  if (!sll) return;
  lastRoutes.set(src, { calls: [src, ...via, ''], color: isRF ? '#e8750a' : '#0aa5a5' });
  // 自動で出す線は自局が RF で受信したもの (RF→IS) だけ。IS→RF など他の経路はカーソルを合わせたときだけ表示する
  if (isRF || !$('#opt-gateroute').checked) return;
  const line = [sll, ...pts, me];
  const color = '#0aa5a5';
  while (routeLayer.getLayers().length >= ROUTE_MAX) routeLayer.removeLayer(routeLayer.getLayers()[0]);
  const pl = L.polyline(line, { color, weight: 3, opacity: 0.9, className: 'gate-route', interactive: false });
  const dot = L.circleMarker(sll, { radius: 6, color, weight: 2, fillOpacity: 0, className: 'gate-route-dot', interactive: false });
  routeLayer.addLayer(pl).addLayer(dot);
  setTimeout(() => { routeLayer.removeLayer(pl); routeLayer.removeLayer(dot); }, ROUTE_MS);
}
$('#opt-gateroute').addEventListener('change', () => { if (!$('#opt-gateroute').checked) { routeLayer.clearLayers(); hoverRouteLayer.clearLayers(); } });

function addPacket(p) {
  if ($('#pkt-pause').checked) return;
  const cats = new Set($$('.pf').filter(c => c.checked).map(c => c.value));
  if (!cats.has(pktCategory(p.dir))) return;
  const g = $('#pkt-grep').value.trim().toUpperCase();
  if (g && !p.raw.toUpperCase().includes(g)) return;
  const atBottom = pktLog.scrollHeight - pktLog.scrollTop - pktLog.clientHeight < 30;
  const div = document.createElement('div');
  div.className = 'dir-' + p.dir;
  div.innerHTML = `<span class="t">${hhmmss(p.t)}</span><span class="d">${esc(p.dir)}</span> ${esc(p.raw)}`;
  pktLog.appendChild(div);
  while (pktLog.childElementCount > 3000) pktLog.firstElementChild.remove();
  if (atBottom) pktLog.scrollTop = pktLog.scrollHeight;
}
$('#pkt-clear').addEventListener('click', () => (pktLog.innerHTML = ''));

// ---------- メッセージ ----------
function renderMessages() {
  const el = $('#msg-list');
  const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30;
  el.innerHTML = messages.map(m => {
    let st = '';
    if (m.dir === 'out') {
      if (m.acked) st = '<span class="st-ok">✓ 受信確認</span>';
      else if (m.rejected) st = '<span class="st-ng">拒否されました</span>';
      else if (m.failed) st = '<span class="st-ng">応答なし</span>';
      else st = `送信中 (${m.tries}回)`;
    }
    const who = m.dir === 'in' ? `<b data-to="${esc(m.from)}">${esc(m.from)}</b> → 自局` : `自局 → <b data-to="${esc(m.to)}">${esc(m.to)}</b>`;
    return `<div class="msg ${m.dir}"><div class="meta">${who} · ${mdhhmmss(m.time)} · ${esc(m.via || '')} ${st}</div><div class="text">${esc(m.text)}</div></div>`;
  }).join('') || '<p class="hint">メッセージはまだありません。</p>';
  if (atBottom) el.scrollTop = el.scrollHeight;
}
function upsertMessage(m) {
  const i = messages.findIndex(x => x.id === m.id);
  if (i >= 0) messages[i] = m;
  else {
    messages.push(m);
    if (m.dir === 'in') {
      if (!$('#tab-messages').classList.contains('active')) unread++;
      toast(`メッセージ受信 ${m.from}: ${m.text}`, 5000);
    }
  }
  updateBadge();
  renderMessages();
}
function updateBadge() {
  const b = $('#msg-badge');
  b.hidden = unread === 0;
  b.textContent = unread;
}
function openMessageTo(call) {
  showTab('messages');
  $('#msg-to').value = call;
  $('#msg-text').focus();
}
$('#msg-list').addEventListener('click', ev => {
  const b = ev.target.closest('b[data-to]');
  if (b) { $('#msg-to').value = b.dataset.to; $('#msg-text').focus(); }
});
$('#msg-form').addEventListener('submit', async ev => {
  ev.preventDefault();
  try {
    await api('/api/message', { to: $('#msg-to').value, text: $('#msg-text').value });
    $('#msg-text').value = '';
  } catch (e) {
    toast('送信できません: ' + e.message);
  }
});

// ---------- タブ ----------
function showTab(name) {
  $$('.tabs button').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
  $$('.tab').forEach(t => t.classList.toggle('active', t.id === 'tab-' + name));
  if (name === 'messages') { unread = 0; updateBadge(); }
  if (name === 'settings') loadPorts();
  // 編集用のオブジェクトマーカーは設定タブを開いている間だけ表示
  if (name === 'settings') { objLayer.addTo(map); objDrawMarkers(); } else { objLayer.remove(); objSetPick(false); }
  if (name === 'about') loadAbout();
  if (name === 'wx') loadWX();
  map.invalidateSize();
}
$$('.tabs button').forEach(b => b.addEventListener('click', () => showTab(b.dataset.tab)));

// ---------- オブジェクト / アイテム ----------
const MAX_OBJ = 10;
const OBJ_QUICK = [['/r', 'リピーター'], ['/;', '移動運用・イベント'], ['\\h', 'ショップ']];
const ASCII_RE = /^[\x20-\x7b\x7d]*$/; // 英数字・記号のみ (| ~ を除く)
const asciiOnly = s => s.replace(/[^\x20-\x7b\x7d]/g, '');
let objDraft = [];     // 編集中の一覧 (保存するまでサーバーには反映しない)
let objCur = -1;
let objDirty = false;
let objPicking = false;
const objLayer = L.layerGroup();

// 35°05.59' N 136°55.08' E 形式 (aprs.fi と同じ度分表記)
function fmtDM(v, hemi) {
  const t = Math.round(Math.abs(v) * 6000);
  return `${Math.floor(t / 6000)}°${String(Math.floor(t % 6000 / 100)).padStart(2, '0')}.${String(t % 100).padStart(2, '0')}' ${hemi[v < 0 ? 1 : 0]}`;
}
function fmtLat(v) { const h = v < 0 ? 'S' : 'N'; const t = Math.round(Math.abs(v) * 6000); return `${String(Math.floor(t / 6000)).padStart(2, '0')}${String(Math.floor(t % 6000 / 100)).padStart(2, '0')}.${String(t % 100).padStart(2, '0')}${h}`; }
function fmtLon(v) { const h = v < 0 ? 'W' : 'E'; const t = Math.round(Math.abs(v) * 6000); return `${String(Math.floor(t / 6000)).padStart(3, '0')}${String(Math.floor(t % 6000 / 100)).padStart(2, '0')}.${String(t % 100).padStart(2, '0')}${h}`; }
function zulu() { const d = new Date(); const p = n => String(n).padStart(2, '0'); return p(d.getUTCDate()) + p(d.getUTCHours()) + p(d.getUTCMinutes()) + 'z'; }

function objNameError(o) {
  if (!o.name) return '名前を入力してください';
  if (o.kind === 'item') {
    if (o.name.length < 3) return 'アイテムの名前は 3〜9 文字です';
    if (/[!_]/.test(o.name)) return 'アイテムの名前に「!」「_」は使えません';
  }
  if (objDraft.some(x => x !== o && x.name === o.name)) return '同じ名前が登録されています';
  return '';
}
function objAutoPacket(o) {
  const pos = `${fmtLat(o.lat)}${o.sym[0]}${fmtLon(o.lon)}${o.sym[1]}${o.comment}`;
  const info = o.kind === 'object' ? `;${o.name.padEnd(9, ' ')}*${zulu()}${pos}` : `)${o.name}!${pos}`;
  return `${cfg.callsign || 'NOCALL'}>APKSXI${cfg.beaconRfPath ? ',' + cfg.beaconRfPath : ''}:${info}`;
}
function objCheckPacket(p, o) {
  if (!ASCII_RE.test(p)) return ['err', '日本語など英数字以外の文字が含まれています (送信できません)'];
  const m = p.match(/^([A-Za-z0-9-]+)>([A-Za-z0-9-]+(?:,[A-Za-z0-9-]+\*?)*):(.+)$/);
  if (!m) return ['err', '「自局コール>宛先,パス:内容」の形式になっていません'];
  if (m[1].toUpperCase() !== (cfg.callsign || '').toUpperCase()) return ['err', `送信元は自局 (${cfg.callsign}) である必要があります`];
  if (o.kind === 'object' && !/^;.{9}[*_]\d{6}[zh/]/.test(m[3])) return ['warn', 'オブジェクトの形式 (;名前9文字*時刻…) と一致しません。このまま送信はできます'];
  if (o.kind === 'item' && !/^\).{3,9}[!_]/.test(m[3])) return ['warn', 'アイテムの形式 ()名前!…) と一致しません。このまま送信はできます'];
  return ['ok', '✓ 送信できる形式です'];
}

function objLoadFromConfig(keepId) {
  objDraft = JSON.parse(JSON.stringify(cfg.objects || []));
  objDirty = false;
  const i = objDraft.findIndex(o => o.id === keepId);
  objCur = i >= 0 ? i : objDraft.length ? 0 : -1;
  objFill();
}
function objSetDirty() { objDirty = true; $('#obj-dirty').hidden = false; }

function objRenderRows() {
  $('#obj-rows').innerHTML = objDraft.map((o, i) => `<tr data-i="${i}" class="${i === objCur ? 'sel' : ''}"><td><input type="checkbox" ${o.enabled ? 'checked' : ''} data-en="${i}"></td><td>${symHTML(o.sym)}</td><td>${esc(o.name)}${o.manual ? ' ✎' : ''}</td><td><span class="kind">${o.kind === 'object' ? 'オブジェクト' : 'アイテム'}</span></td><td>${{ RF: 'RF', IS: 'IS', BOTH: 'RF＋IS' }[o.via]}</td><td>${o.intervalMin}分</td></tr>`).join('');
  const full = objDraft.length >= MAX_OBJ;
  $('#obj-count').textContent = `(${objDraft.length} / ${MAX_OBJ})`;
  $('#obj-count').classList.toggle('full', full);
  $('#obj-add').disabled = full;
  $('#obj-limit').textContent = full ? `登録は ${MAX_OBJ} 件までです` : '';
  $('#obj-empty').hidden = objDraft.length > 0;
  $('#obj-editor').hidden = objCur < 0;
  $('#obj-dirty').hidden = !objDirty;
}
function objRenderEditor() {
  const o = objDraft[objCur];
  if (!o) return;
  $('#obj-title').textContent = o.name || '(新規)';
  $('#obj-aprs-pos').textContent = `${fmtLat(o.lat)} / ${fmtLon(o.lon)}`;
  $('#obj-sym-prev').innerHTML = symHTML(o.sym);
  $('#obj-name-label').textContent = o.kind === 'object' ? '名前 (1〜9文字)' : '名前 (3〜9文字)';
  $('#obj-kind-hint').textContent = o.kind === 'object'
    ? 'オブジェクト: 送信時刻付き。イベントや一時的な地点向け。'
    : 'アイテム: 時刻なしの固定地点。リピーターや店舗など常設の場所向け。';
  const ne = objNameError(o);
  $('#obj-name-msg').className = 'msg-line err-t';
  $('#obj-name-msg').textContent = ne;
  const manual = !!o.manual;
  const pkt = $('#obj-pkt');
  if (document.activeElement !== pkt) pkt.value = manual ? o.manual : objAutoPacket(o);
  pkt.classList.toggle('manual', manual);
  $('#obj-manual-badge').hidden = !manual;
  $('#obj-regen').disabled = !manual;
  const [lv, msg] = objCheckPacket(pkt.value, o);
  $('#obj-pkt-msg').className = 'msg-line ' + (lv === 'ok' ? 'ok-t' : lv === 'warn' ? 'warn-t' : 'err-t');
  $('#obj-pkt-msg').textContent = msg;
  $('#obj-pkt-hint').textContent = manual
    ? '手動編集中は、上の項目を変更してもパケットは変わりません。' + (o.kind === 'object' ? ' 時刻部分 (DDHHMMz) は送信時に自動で更新します。' : '') + ' APRS-IS へはパス部分を TCPIP* に置き換えて送信します。'
    : 'パケットを直接書き換えると「手動編集中」になり、その内容のまま送信します。';
}
function objDrawMarkers() {
  objLayer.clearLayers();
  objDraft.forEach((o, i) => {
    const editing = i === objCur;
    const m = L.marker([o.lat, o.lon], {
      icon: L.divIcon({ className: '', html: `<div class="stn-icon ${editing ? 'editing' : 'obj'}">${symHTML(o.sym)}<span class="label">${esc(o.name || '(名前なし)')}</span></div>`, iconSize: [24, 24], iconAnchor: [12, 12] }),
      draggable: editing, zIndexOffset: editing ? 2000 : 1500,
    }).addTo(objLayer);
    if (editing) m.on('dragend', () => { const p = m.getLatLng(); objSetPos(p.lat, p.lng); });
    else m.on('click', () => objSelect(i));
  });
}
function objRenderAll() { objRenderRows(); objRenderEditor(); objDrawMarkers(); }
function objFill() {
  const o = objDraft[objCur];
  if (o) {
    $('#obj-name').value = o.name; $('#obj-lat').value = o.lat.toFixed(5); $('#obj-lon').value = o.lon.toFixed(5);
    $('#obj-tbl').value = o.sym[0]; $('#obj-sym').value = o.sym[1]; $('#obj-cmt').value = o.comment;
    $('#obj-int').value = o.intervalMin; $('#obj-en').checked = o.enabled;
    $$('input[name=obj-via]').forEach(r => (r.checked = r.value === o.via));
    $$('input[name=obj-kind]').forEach(r => (r.checked = r.value === o.kind));
    $('#obj-paste').value = ''; $('#obj-paste-msg').textContent = ''; $('#obj-cmt-msg').textContent = '';
    $('#obj-pkt').blur();
  }
  objRenderAll();
}
function objSelect(i) {
  objCur = i;
  objFill();
  if (objDraft[i]) map.panTo([objDraft[i].lat, objDraft[i].lon]);
}
function objSetPos(lat, lon) {
  const o = objDraft[objCur]; if (!o) return;
  o.lat = lat; o.lon = lon;
  $('#obj-lat').value = lat.toFixed(5); $('#obj-lon').value = lon.toFixed(5);
  objSetDirty(); objRenderAll();
}
function objSetPick(on) {
  objPicking = on;
  if (on) setPickMode(false);
  $('#obj-pick').classList.toggle('active', on);
  map.getContainer().classList.toggle('picking', on || pickMode);
  if (on) toast('地図上の位置をクリックしてください');
}

// 貼り付けた座標の解釈: 「35.1, 136.9」/ 度分秒 / 「北緯◯度◯分◯秒 東経…」
function parseCoord(text) {
  const t = text.trim().replace(/[，、]/g, ',');
  const m = t.match(/^(-?\d+(?:\.\d+)?)\s*[, ]\s*(-?\d+(?:\.\d+)?)$/);
  if (m) return [parseFloat(m[1]), parseFloat(m[2])];
  const parts = [...t.matchAll(/(\d+(?:\.\d+)?)\s*[°度]\s*(?:(\d+(?:\.\d+)?)\s*['′分])?\s*(?:(\d+(?:\.\d+)?)\s*["″秒])?\s*([NSEW])/gi)];
  if (parts.length === 2) {
    const v = parts.map(p => { let d = +p[1] + (+p[2] || 0) / 60 + (+p[3] || 0) / 3600; if (/[SW]/i.test(p[4])) d = -d; return d; });
    return /[EW]/i.test(parts[0][4]) ? [v[1], v[0]] : v;
  }
  const jp = [...t.matchAll(/(北緯|南緯|東経|西経)\s*(\d+(?:\.\d+)?)\s*度\s*(?:(\d+(?:\.\d+)?)\s*分)?\s*(?:(\d+(?:\.\d+)?)\s*秒)?/g)];
  if (jp.length === 2) {
    let lat, lon;
    for (const p of jp) {
      let d = +p[2] + (+p[3] || 0) / 60 + (+p[4] || 0) / 3600;
      if (p[1] === '南緯' || p[1] === '西経') d = -d;
      if (p[1].endsWith('緯')) lat = d; else lon = d;
    }
    if (lat != null && lon != null) return [lat, lon];
  }
  return null;
}

$('#obj-quick').innerHTML = OBJ_QUICK.map(([s, n]) => `<button type="button" data-s="${esc(s)}">${symHTML(s)}${n}</button>`).join('');
$('#obj-quick').addEventListener('click', e => {
  const b = e.target.closest('button'); const o = objDraft[objCur]; if (!b || !o) return;
  o.sym = b.dataset.s; $('#obj-tbl').value = o.sym[0]; $('#obj-sym').value = o.sym[1]; objSetDirty(); objRenderAll();
});
$('#obj-pick').addEventListener('click', () => objSetPick(!objPicking));
$('#obj-here').addEventListener('click', () => objSetPos(cfg.lat, cfg.lon));
$('#obj-center').addEventListener('click', () => { const c = map.getCenter(); objSetPos(c.lat, c.lng); });
for (const id of ['#obj-lat', '#obj-lon']) {
  $(id).addEventListener('input', () => {
    const la = parseFloat($('#obj-lat').value), lo = parseFloat($('#obj-lon').value), o = objDraft[objCur];
    if (o && isFinite(la) && isFinite(lo) && Math.abs(la) <= 90 && Math.abs(lo) <= 180) { o.lat = la; o.lon = lo; objSetDirty(); objRenderAll(); }
  });
}
$('#obj-paste').addEventListener('input', () => {
  const v = $('#obj-paste').value, msg = $('#obj-paste-msg');
  if (!v.trim()) { msg.textContent = ''; return; }
  const r = parseCoord(v);
  if (r && Math.abs(r[0]) <= 90 && Math.abs(r[1]) <= 180) {
    objSetPos(r[0], r[1]); map.panTo(r);
    msg.className = 'msg-line ok-t'; msg.textContent = `✓ ${r[0].toFixed(5)}, ${r[1].toFixed(5)} に設定しました`;
  } else { msg.className = 'msg-line err-t'; msg.textContent = '座標として読み取れません'; }
});
$('#obj-name').addEventListener('input', e => {
  const o = objDraft[objCur]; if (!o) return;
  o.name = e.target.value.toUpperCase().replace(/[^A-Z0-9\- ]/g, ''); e.target.value = o.name; objSetDirty(); objRenderAll();
});
for (const id of ['#obj-tbl', '#obj-sym']) {
  $(id).addEventListener('input', () => {
    const o = objDraft[objCur]; if (!o) return;
    $('#obj-tbl').value = asciiOnly($('#obj-tbl').value); $('#obj-sym').value = asciiOnly($('#obj-sym').value);
    o.sym = ($('#obj-tbl').value || '/') + ($('#obj-sym').value || '/'); objSetDirty(); objRenderAll();
  });
}
$('#obj-cmt').addEventListener('input', e => {
  const o = objDraft[objCur]; if (!o) return;
  const clean = asciiOnly(e.target.value);
  $('#obj-cmt-msg').className = 'msg-line warn-t';
  $('#obj-cmt-msg').textContent = clean !== e.target.value ? '英数字・記号以外の文字 (日本語など) は入力できません' : '';
  e.target.value = clean; o.comment = clean; objSetDirty(); objRenderEditor();
});
$('#obj-int').addEventListener('input', e => { const o = objDraft[objCur]; if (!o) return; o.intervalMin = Math.max(5, +e.target.value || 30); objSetDirty(); objRenderRows(); });
$('#obj-en').addEventListener('change', e => { const o = objDraft[objCur]; if (!o) return; o.enabled = e.target.checked; objSetDirty(); objRenderRows(); });
$$('input[name=obj-via]').forEach(r => r.addEventListener('change', () => { objDraft[objCur].via = r.value; objSetDirty(); objRenderRows(); }));
$$('input[name=obj-kind]').forEach(r => r.addEventListener('change', () => { objDraft[objCur].kind = r.value; objSetDirty(); objRenderAll(); }));
$('#obj-pkt').addEventListener('input', e => { objDraft[objCur].manual = e.target.value; objSetDirty(); objRenderRows(); objRenderEditor(); });
$('#obj-regen').addEventListener('click', () => { objDraft[objCur].manual = ''; $('#obj-pkt').blur(); objSetDirty(); objRenderAll(); });
$('#obj-rows').addEventListener('click', e => {
  if (e.target.dataset.en != null) {
    const i = +e.target.dataset.en; objDraft[i].enabled = e.target.checked;
    if (i === objCur) $('#obj-en').checked = e.target.checked;
    objSetDirty(); return;
  }
  const tr = e.target.closest('tr'); if (tr) objSelect(+tr.dataset.i);
});
$('#obj-add').addEventListener('click', () => {
  if (objDraft.length >= MAX_OBJ) return;
  const c = map.getCenter();
  objDraft.push({ id: '', kind: 'object', name: '', lat: c.lat, lon: c.lng, sym: '/;', comment: '', via: 'RF', intervalMin: 30, enabled: true, manual: '' });
  objSetDirty(); objSelect(objDraft.length - 1); $('#obj-name').focus();
});
$('#obj-del').addEventListener('click', () => {
  const o = objDraft[objCur]; if (!o) return;
  if (!confirm(`「${o.name || '(名前なし)'}」を削除しますか？\n保存すると、他局の地図からも消えるよう削除通知を送信します。`)) return;
  objDraft.splice(objCur, 1);
  objSetDirty(); objSelect(Math.min(objCur, objDraft.length - 1));
});
async function objSave() {
  const keepId = objDraft[objCur] && objDraft[objCur].id;
  cfg = await api('/api/objects', objDraft);
  // 新規登録は保存時に ID が振られるので、名前で選択を戻す
  const name = objDraft[objCur] && objDraft[objCur].name;
  const id = keepId || ((cfg.objects || []).find(o => o.name === name) || {}).id;
  objLoadFromConfig(id);
}
$('#obj-save').addEventListener('click', async () => {
  try { await objSave(); toast('オブジェクトを保存しました'); } catch (e) { toast(e.message, 6000); }
});
$('#obj-send').addEventListener('click', async () => {
  try {
    if (objDirty) await objSave();
    const o = objDraft[objCur]; if (!o) return;
    await api('/api/object/send', { id: o.id });
    toast(`「${o.name}」を送信しました`);
  } catch (e) { toast(e.message, 6000); }
});

// ---------- このソフトについて ----------
async function loadAbout() {
  try {
    const a = await api('/api/about');
    $('#about-version').textContent = 'Ver.' + a.version;
    $('#about-credit').textContent = a.credit;
    $('#about-changelog').innerHTML = a.changelog.map(r =>
      `<div class="rel"><b>Ver.${esc(r.version)}</b><span class="date">${esc(r.date)}</span><ul>${r.changes.map(c => `<li>${esc(c)}</li>`).join('')}</ul></div>`
    ).join('');
  } catch (e) {
    toast(e.message);
  }
}

// ---------- 設定 ----------
const optNumFields = ['kissPersist', 'kissSlotMs', 'kissTxTailMs', 'kissFullDup']; // 空欄 = -1 (TNC の設定のまま)
const numFields = ['lat', 'lon', 'phgPower', 'phgHeight', 'phgGain', 'phgDir', 'baud', 'txDelayMs', 'localHeardMin', 'maxIsToRfPerMin', 'digiAliasPerMin', 'beaconRfMin', 'beaconIsMin', 'stationExpireMin', 'trackPoints', 'trackGapMin', 'wxBaud', 'wxIsMin', 'wxRfMin', 'wxCwopMin', 'wxSdrDevice', 'wxSdrGain', 'wxSdrPpm', 'wxSdrId', 'wxRainMm'];
function fillForm() {
  const f = $('#cfg-form');
  for (const el of f.elements) {
    if (!el.name || !(el.name in cfg)) continue;
    if (el.type === 'checkbox') el.checked = !!cfg[el.name];
    else if (optNumFields.includes(el.name)) el.value = cfg[el.name] >= 0 ? cfg[el.name] : '';
    else el.value = cfg[el.name];
  }
  updateTncFields();
  updateWxFields();
  updateTrackFields();
  updateSymPreview();
  updatePhgPreview();
}
function readForm() {
  const f = $('#cfg-form');
  const out = {};
  for (const el of f.elements) {
    if (!el.name) continue;
    if (el.type === 'checkbox') out[el.name] = el.checked;
    else if (optNumFields.includes(el.name)) out[el.name] = el.value === '' ? -1 : Number(el.value);
    else if (numFields.includes(el.name)) out[el.name] = Number(el.value) || 0;
    else out[el.name] = el.value;
  }
  return out;
}
function updateTncFields() {
  const t = $('#cfg-form').tncType.value;
  $$('.tnc-serial').forEach(e => (e.style.display = t === 'kiss-serial' ? '' : 'none'));
  $$('.tnc-tcp').forEach(e => (e.style.display = t === 'kiss-tcp' ? '' : 'none'));
  $$('.tnc-bt').forEach(e => (e.style.display = t === 'kiss-bt' ? '' : 'none'));
  if (t === 'kiss-bt') loadBTDevices();
}
function updateTrackFields() {
  $$('.track-gap').forEach(e => (e.style.display = $('#cfg-form').trackSplit.value === 'gap' ? '' : 'none'));
}
function updateSymPreview() {
  const f = $('#cfg-form');
  $('#sym-preview').innerHTML = symHTML((f.symbolTable.value || '/') + (f.symbol.value || '/'));
}
// PHG コード (src/phg.go の phgCode と同じ丸め) と通達範囲 (km)。range(mi) = sqrt(2 * H(ft) * sqrt(P/10 * G/2))
function phgInfo(powerW, heightM, gainDB, dir) {
  const digit = v => Math.min(9, Math.max(0, Math.round(v)));
  const p = digit(Math.sqrt(Math.max(powerW, 0)));
  const ft = heightM / 0.3048;
  const h = ft > 10 ? digit(Math.log2(ft / 10)) : 0;
  const g = digit(gainDB);
  const d = dir > 0 ? Math.min(8, Math.max(1, Math.floor((dir + 22) / 45))) : 0;
  const hft = 10 * 2 ** h;
  const km = Math.sqrt(2 * hft * Math.sqrt((p * p) / 10 * (10 ** (g / 10)) / 2)) * 1.609344;
  return { code: `PHG${p}${h}${g}${d}`, km, w: p * p, m: hft * 0.3048, g, dir: d * 45 };
}
function updatePhgPreview() {
  const f = $('#cfg-form');
  const r = phgInfo(Number(f.phgPower.value) || 0, Number(f.phgHeight.value) || 0, Number(f.phgGain.value) || 0, Number(f.phgDir.value) || 0);
  $('#phg-preview').textContent = `${r.code} → 通達範囲 約 ${r.km.toFixed(1)} km (送信される値: ${r.w} W / ${r.m.toFixed(1)} m / ${r.g} dBi / ${r.dir ? r.dir + '°' : '無指向'})`;
}
function updateRangeCircle() {
  rangeLayer.clearLayers();
  if (!cfg.phgEnabled || !cfg.phgRangeMap || !(cfg.lat || cfg.lon)) return;
  const r = phgInfo(cfg.phgPower, cfg.phgHeight, cfg.phgGain, cfg.phgDir);
  if (r.km <= 0) return;
  L.circle([cfg.lat, cfg.lon], { radius: r.km * 1000, color: '#2a7de1', weight: 1.5, dashArray: '6 4', fillColor: '#2a7de1', fillOpacity: 0.06, interactive: false }).addTo(rangeLayer);
}
for (const n of ['phgPower', 'phgHeight', 'phgGain']) $('#cfg-form')[n].addEventListener('input', updatePhgPreview);
$('#cfg-form').phgDir.addEventListener('change', updatePhgPreview);
$('#cfg-form').tncType.addEventListener('change', updateTncFields);
$('#cfg-form').trackSplit.addEventListener('change', updateTrackFields);
$('#cfg-form').symbolTable.addEventListener('input', updateSymPreview);
$('#cfg-form').symbol.addEventListener('input', updateSymPreview);
$('#btn-pickpos').addEventListener('click', () => setPickMode(!pickMode));
async function loadPorts() {
  try {
    const ports = await api('/api/serialports');
    $('#ports').innerHTML = ports.map(p => `<option value="${esc(p)}">`).join('');
  } catch { /* 無視 */ }
}
// Android ブリッジ経由の Bluetooth 機器一覧 (ペアリングは Android の設定で済ませておく)
async function loadBTDevices() {
  const f = $('#cfg-form'), sel = f.btAddr, msg = $('#bt-msg');
  const cur = sel.value || cfg.btAddr || '';
  try {
    const r = await api('/api/btdevices');
    const devs = r.devices || [];
    sel.innerHTML = '<option value="">(選択してください)</option>' +
      devs.map(d => `<option value="${esc(d.addr)}" data-name="${esc(d.name)}">${esc(d.name)} (${esc(d.addr)})</option>`).join('');
    if (cur && !devs.some(d => d.addr === cur)) sel.insertAdjacentHTML('beforeend', `<option value="${esc(cur)}">${esc(cur)} (一覧にありません)</option>`);
    sel.value = cur;
    msg.textContent = r.error || (devs.length ? '' : 'ペアリング済みの機器がありません。Android の設定でペアリングしてください');
  } catch (e) {
    msg.textContent = e.message;
  }
}
$('#cfg-form').btAddr.addEventListener('change', ev => {
  const o = ev.target.selectedOptions[0];
  $('#cfg-form').btName.value = (o && o.dataset.name) || '';
});
$('#btn-btrefresh').addEventListener('click', loadBTDevices);
$('#cfg-form').addEventListener('submit', async ev => {
  ev.preventDefault();
  const res = $('#cfg-result');
  try {
    cfg = await api('/api/config', readForm());
    fillForm();
    applyConfigUI();
    res.className = 'ok';
    res.textContent = '保存しました';
  } catch (e) {
    res.className = 'err';
    res.textContent = e.message;
  }
  setTimeout(() => (res.textContent = ''), 5000);
});
function applyConfigUI() {
  $('#mycall').textContent = cfg.callsign || '(未設定)';
  listDirty = true;
  updateRangeCircle();
  for (const k of stations.keys()) drawStation(k);
}

// ---------- ステータス ----------
function renderStatus() {
  const is = status.is, tnc = status.tnc;
  const ii = $('#ind-is');
  ii.className = 'ind ' + (!is ? '' : is.connected ? (is.verified ? 'ok' : 'warn') : 'bad');
  ii.title = !is ? 'APRS-IS: 無効' : is.connected ? `APRS-IS: ${is.server || '接続中'}${is.verified ? '' : ' (未認証: 受信のみ)'}` : `APRS-IS: 切断 ${is.error || ''}`;
  $('.lbl', ii).textContent = is && is.connected && !is.verified ? 'APRS-IS (受信のみ)' : 'APRS-IS';
  const ti = $('#ind-tnc');
  ti.className = 'ind ' + (!tnc ? '' : tnc.connected ? 'ok' : 'bad');
  ti.title = !tnc ? 'TNC: 使用しない' : `${tnc.desc} ${tnc.connected ? '接続中' : '切断 ' + (tnc.error || '')}`;
  const s = status.stats || {};
  renderDX();
  renderUpdate();
  renderWxHeard();
  $('#stats').textContent = `RF受信 ${s.rfRx || 0} / IS受信 ${s.isRx || 0} / RF→IS ${s.rfToIs || 0} / IS→RF ${s.isToRf || 0} / デジ ${s.digi || 0} / 送信 RF ${s.txRf || 0}・IS ${s.txIs || 0}`;
}

function renderDX() {
  const r = status.dx, el = $('#dx');
  if (!r) {
    el.textContent = 'DX: なし';
    el.classList.remove('has');
    el.title = '最長距離 (RF 直接受信): まだ記録なし';
    return;
  }
  const d = cfg.dxUnit === 'mi' ? `${(r.km / 1.609344).toFixed(1)} mi` : `${r.km.toFixed(1)} km`;
  const t = new Date(r.t);
  el.textContent = `DX: ${r.call} ${d} ${String(t.getHours()).padStart(2, '0')}:${String(t.getMinutes()).padStart(2, '0')}`;
  el.classList.add('has');
  el.title = `${status.dxText}\n(${cfg.dxReset === 'hour' ? '毎時 0 分' : '毎日 24 時'}にリセット・クリックで地図に表示)`;
}
function renderWxHeard() {
  const el = $('#wx-heard');
  if (!el) return;
  const h = status.davis && status.davis.heard;
  el.textContent = h && h.length ? `受信できたセンサー: ${h.join(' / ')}` : '';
}
function renderUpdate() {
  const u = status.update, el = $('#upd');
  el.hidden = !(u && u.newer);
  if (el.hidden) return;
  el.textContent = `新しいバージョン Ver.${u.latest} があります`;
  el.href = u.url || '#';
  el.title = u.canApply ? 'クリックで更新して再起動します' : 'クリックで公開ページを開きます (この環境用のファイルが見つからないため手動で入れ替えてください)';
}
async function applyUpdate(u) {
  if (!confirm(`Ver.${u.latest} をダウンロードして入れ替え、再起動します。\nTNC・APRS-IS は一時的に切れます。よろしいですか？`)) return;
  try {
    await api('/api/update/apply', {});
    toast('更新しています。まもなく再起動します');
  } catch (e) {
    toast(e.message);
  }
}
$('#upd').addEventListener('click', (ev) => {
  const u = status.update;
  if (u && u.newer && u.canApply) {
    ev.preventDefault();
    applyUpdate(u);
  }
});
function updateResultText(u) {
  if (u.error) return u.error;
  if (!u.latest) return 'まだ公開されたリリースはありません';
  if (u.newer) return `新しいバージョン Ver.${u.latest} があります`;
  return `最新です (公開中の最新: Ver.${u.latest})`;
}
$('#btn-upd-check').addEventListener('click', async () => {
  const res = $('#upd-result'), btn = $('#btn-upd-check');
  btn.disabled = true;
  res.textContent = '確認しています…';
  try {
    const u = await api('/api/update/check', {});
    res.innerHTML = esc(updateResultText(u)) + (u.newer && u.url ? ` <a href="${esc(u.url)}" target="_blank" rel="noopener">公開ページを開く</a>` : '')
      + (u.newer && u.canApply ? ' <button id="btn-upd-apply" type="button">更新して再起動</button>' : '');
    const ap = $('#btn-upd-apply');
    if (ap) ap.addEventListener('click', () => applyUpdate(u));
  } catch (e) {
    res.textContent = e.message;
  }
  btn.disabled = false;
});

$('#dx').addEventListener('click', () => {
  const r = status.dx;
  if (r) map.setView([r.lat, r.lon], Math.max(map.getZoom(), 11));
});

$('#btn-beacon').addEventListener('click', async () => {
  try {
    const r = await api('/api/beacon', {});
    toast(r.rf || r.is ? `ビーコン送信: ${[r.rf && 'RF', r.is && 'APRS-IS'].filter(Boolean).join(' / ')}` : 'ビーコンを送信できません (未接続または未認証)');
  } catch (e) {
    toast(e.message);
  }
});
$('#btn-center').addEventListener('click', () => map.setView([cfg.lat, cfg.lon], Math.max(map.getZoom(), 11)));

// ---------- 気象タブ (自局のウェザーステーション) ----------
let wxSnap = null;
let wxRangeH = 24;
const wxActive = () => $('#tab-wx').classList.contains('active');

function updateWxFields() {
  const f = $('#cfg-form');
  const src = f.wxSource.value;
  $$('.wx-serial').forEach(e => (e.style.display = src === 'davis-serial' ? '' : 'none'));
  $$('.wx-tcp').forEach(e => (e.style.display = src === 'davis-tcp' ? '' : 'none'));
  $$('.wx-sdr').forEach(e => (e.style.display = src === 'davis-sdr' ? '' : 'none'));
  $$('.wx-rtl').forEach(e => (e.style.display = src === 'rtl433' ? '' : 'none'));
  const cs = (f.callsign.value || '').trim().toUpperCase();
  let auto = '';
  if (cs) {
    const base = cs.split('-')[0];
    auto = base + (cs === base + '-13' ? '-14' : '-13');
  }
  $('#wx-call').placeholder = auto || '空欄で自動';
  $('#wx-call-hint').textContent = auto
    ? `空欄のときは ${auto} で送信します。地図には I-Gate とは別の局として表示されます。`
    : '先に「自局」のコールサインを設定してください。';
}
$('#cfg-form').wxSource.addEventListener('change', updateWxFields);
$('#cfg-form').callsign.addEventListener('input', updateWxFields);

async function loadWX() {
  try {
    wxSnap = await api('/api/wx');
  } catch { return; }
  renderWX();
}
setInterval(() => { if (wxActive()) loadWX(); }, 30000);

function renderWX() {
  $('#wxp-off').hidden = wxSnap.enabled;
  $('#wxp-body').hidden = !wxSnap.enabled;
  if (!wxSnap.enabled) return;
  renderWXState();
  renderWXCur();
  renderWXCharts();
}

function renderWXState() {
  const d = (wxSnap && wxSnap.davis) || (status && status.davis);
  const dot = $('#wxp-dot'), el = $('#wxp-state');
  if (!d) { dot.className = 'ind'; el.textContent = '停止中'; return; }
  if (d.connected && !d.error) {
    dot.className = 'ind ok';
    el.textContent = `${d.desc} に接続中` + (d.last ? ` / 最終受信 ${hhmmss(d.last)}` : '') + (d.detail ? ` (${d.detail})` : '') + (d.battLow ? ' / センサー電池残量低下' : '');
  } else if (d.connected) {
    dot.className = 'ind warn';
    el.textContent = `${d.desc}: 応答がありません (${d.error})`;
  } else {
    dot.className = 'ind bad';
    el.textContent = `${d.desc}: 未接続 ${d.error || ''}`;
  }
  renderWXTx();
}

function renderWXCur() {
  const w = wxSnap && wxSnap.cur;
  $('#wxp-cur').innerHTML = w ? wxHTML({ wx: w, wxTime: wxSnap.curT }) : '<p class="hint">まだ気象データを受信していません。</p>';
  const parts = [];
  if (w) {
    if (w.luminosity != null) parts.push(`日射 ${w.luminosity} W/m²`);
    if (w.rain24h != null) parts.push(`24時間雨量 ${w.rain24h.toFixed(1)} mm`);
    if (w.rainMidnight != null) parts.push(`0時から ${w.rainMidnight.toFixed(1)} mm`);
  }
  // 本日 (0 時から) の極値。起動後に観測した分だけの集計
  const mid = new Date(); mid.setHours(0, 0, 0, 0);
  const today = ((wxSnap && wxSnap.history) || []).filter(p => p.t >= mid.getTime());
  const tmax = today.filter(p => p.tempMax != null).reduce((a, p) => (a == null || p.tempMax > a.v ? { v: p.tempMax, t: p.t } : a), null);
  const tmin = today.filter(p => p.tempMin != null).reduce((a, p) => (a == null || p.tempMin < a.v ? { v: p.tempMin, t: p.t } : a), null);
  const gmax = today.filter(p => p.gust != null).reduce((a, p) => (a == null || p.gust > a.v ? { v: p.gust, t: p.t } : a), null);
  const hm = t => new Date(t).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' });
  const ex = [];
  if (tmax) ex.push(`最高気温 ${tmax.v.toFixed(1)} ℃ (${hm(tmax.t)})`);
  if (tmin) ex.push(`最低気温 ${tmin.v.toFixed(1)} ℃ (${hm(tmin.t)})`);
  if (gmax) ex.push(`最大瞬間風速 ${gmax.v.toFixed(1)} m/s (${hm(gmax.t)})`);
  $('#wxp-extra').innerHTML = [parts.join(' / '), ex.length ? '本日: ' + ex.join(' / ') : ''].filter(Boolean).map(t => `<div>${esc(t)}</div>`).join('');
}

function renderWXTx() {
  if (!wxSnap) return;
  const rows = [`<div>気象局: <b>${esc(wxSnap.call || '(コールサイン未設定)')}</b></div>`];
  if (wxSnap.lastTx) rows.push(`<div>最後の送信: ${hhmmss(wxSnap.lastTx)}</div><div class="raw">${esc(wxSnap.lastPkt || '')}</div>`);
  else rows.push('<div>まだ送信していません。</div>');
  const nx = [];
  if (wxSnap.nextIs) nx.push(`APRS-IS ${hhmmss(wxSnap.nextIs)}`);
  if (wxSnap.nextRf) nx.push(`RF ${hhmmss(wxSnap.nextRf)}`);
  if (nx.length) rows.push(`<div>次回の送信: ${nx.join(' / ')}</div>`);
  $('#wxp-tx').innerHTML = rows.join('');
}

$('#wxp-send').addEventListener('click', async () => {
  const msg = $('#wxp-send-msg');
  try {
    wxSnap = await api('/api/wx/send', {});
    renderWX();
    msg.textContent = '送信しました';
  } catch (e) {
    msg.textContent = e.message;
  }
  setTimeout(() => (msg.textContent = ''), 5000);
});

$$('#wxp-range button').forEach(b => b.addEventListener('click', () => {
  wxRangeH = Number(b.dataset.h);
  $$('#wxp-range button').forEach(x => x.classList.toggle('active', x === b));
  renderWXCharts();
}));

// 折れ線・棒グラフ (外部ライブラリなし)。series: {get, color, axis(0=左,1=右), bars}
function wxChart(cv, tmin, tmax, pts, series, fmt) {
  const dpr = window.devicePixelRatio || 1;
  const W = cv.clientWidth || 300, H = 120;
  cv.width = W * dpr; cv.height = H * dpr; cv.style.height = H + 'px';
  const g = cv.getContext('2d');
  g.setTransform(dpr, 0, 0, dpr, 0, 0);
  g.clearRect(0, 0, W, H);
  const css = getComputedStyle(document.documentElement);
  const cMuted = css.getPropertyValue('--muted').trim(), cBorder = css.getPropertyValue('--border').trim();
  const hasR = series.some(s => s.axis === 1);
  const L = 38, R = hasR ? 34 : 8, T = 6, B = 18;
  const pw = W - L - R, ph = H - T - B;
  const X = t => L + (t - tmin) / (tmax - tmin) * pw;
  g.font = '10px sans-serif';
  // 軸の範囲
  const range = [null, null];
  for (const ax of [0, 1]) {
    let lo = Infinity, hi = -Infinity, bars = false;
    for (const s of series.filter(s => s.axis === ax)) {
      if (s.bars) bars = true;
      for (const p of pts) { const v = s.get(p); if (v != null) { lo = Math.min(lo, v); hi = Math.max(hi, v); } }
    }
    if (lo === Infinity) continue;
    if (bars) lo = 0;
    if (hi - lo < (bars ? 1 : 0.5)) { hi = lo + (bars ? 1 : 0.5); }
    const step = Math.pow(10, Math.floor(Math.log10((hi - lo) / 4)));
    const nice = [1, 2, 2.5, 5, 10].map(m => m * step).find(m => (hi - lo) / m <= 4) || step * 10;
    lo = bars ? 0 : Math.floor(lo / nice) * nice; hi = Math.ceil(hi / nice) * nice;
    range[ax] = { lo, hi, nice };
  }
  // 横軸の目盛り
  const hours = (tmax - tmin) / 3600000;
  const tickMs = hours <= 6 ? 3600000 : hours <= 24 ? 4 * 3600000 : 24 * 3600000;
  g.strokeStyle = cBorder; g.fillStyle = cMuted; g.lineWidth = 1; g.textAlign = 'center';
  const off = new Date().getTimezoneOffset() * 60000;
  for (let t = Math.ceil((tmin - off) / tickMs) * tickMs + off; t <= tmax; t += tickMs) {
    const x = Math.round(X(t)) + 0.5;
    g.beginPath(); g.moveTo(x, T); g.lineTo(x, T + ph); g.stroke();
    const d = new Date(t);
    g.fillText(tickMs >= 24 * 3600000 ? `${d.getMonth() + 1}/${d.getDate()}` : `${d.getHours()}時`, x, H - 5);
  }
  // 縦軸の目盛り
  for (const ax of [0, 1]) {
    const r = range[ax];
    if (!r) continue;
    g.textAlign = ax === 0 ? 'right' : 'left';
    for (let v = r.lo; v <= r.hi + r.nice / 2; v += r.nice) {
      const y = T + ph - (v - r.lo) / (r.hi - r.lo) * ph;
      if (ax === 0) { g.beginPath(); g.moveTo(L, Math.round(y) + 0.5); g.lineTo(L + pw, Math.round(y) + 0.5); g.stroke(); }
      g.fillText(fmt[ax](v), ax === 0 ? L - 4 : L + pw + 4, y + 3);
    }
  }
  // 系列
  for (const s of series) {
    const r = range[s.axis];
    if (!r) continue;
    const Y = v => T + ph - (v - r.lo) / (r.hi - r.lo) * ph;
    g.strokeStyle = g.fillStyle = s.color; g.lineWidth = 1.5;
    if (s.bars) {
      const bw = Math.max(1, s.binMs / (tmax - tmin) * pw - 1);
      for (const p of pts) { const v = s.get(p); if (v > 0) g.fillRect(X(p.t), Y(v), bw, T + ph - Y(v)); }
    } else {
      g.beginPath();
      let pen = false, last = null;
      for (const p of pts) {
        const v = s.get(p);
        if (v == null || (last != null && p.t - last > 15 * 60000)) pen = false; // 欠測や長い中断では線をつなげない
        if (v == null) { last = null; continue; }
        const x = X(p.t + 150000), y = Y(v);
        if (pen) g.lineTo(x, y); else g.moveTo(x, y);
        pen = true; last = p.t;
      }
      g.stroke();
      if (last != null) { // 最新値に点を打つ (点が 1 つだけでも見える)
        const lp = [...pts].reverse().find(p => s.get(p) != null);
        g.beginPath(); g.arc(X(lp.t + 150000), Y(s.get(lp)), 2.5, 0, Math.PI * 2); g.fill();
      }
    }
  }
  // マウスを乗せると値を表示
  cv.onmousemove = ev => {
    const t = tmin + (ev.offsetX - L) / pw * (tmax - tmin);
    let best = null;
    for (const p of pts) if (best == null || Math.abs(p.t + 150000 - t) < Math.abs(best.t + 150000 - t)) best = p;
    if (!best || Math.abs(best.t + 150000 - t) > 20 * 60000) { cv.title = ''; return; }
    const d = new Date(best.t);
    cv.title = `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}\n` +
      series.map(s => { const v = s.get(best); return `${s.name}: ${v == null ? '—' : s.fmt(v)}`; }).join('\n');
  };
}

function renderWXCharts() {
  if (!wxSnap || !wxSnap.enabled) return;
  const tmax = Date.now(), tmin = tmax - wxRangeH * 3600000;
  const pts = (wxSnap.history || []).filter(p => p.t + 300000 >= tmin);
  const f1 = v => v.toFixed(1);
  const nice = v => (Math.abs(v) >= 100 ? v.toFixed(0) : String(+v.toFixed(2)));
  wxChart($('#wxc-temp'), tmin, tmax, pts, [
    { name: '気温', get: p => p.temp, color: '#d9480f', axis: 0, fmt: v => v.toFixed(1) + ' ℃' },
    { name: '湿度', get: p => p.hum, color: '#1c8fc2', axis: 1, fmt: v => Math.round(v) + ' %' },
  ], [nice, nice]);
  wxChart($('#wxc-pres'), tmin, tmax, pts, [
    { name: '気圧', get: p => p.pres, color: '#7048b6', axis: 0, fmt: v => v.toFixed(1) + ' hPa' },
  ], [f1]);
  wxChart($('#wxc-wind'), tmin, tmax, pts, [
    { name: '最大瞬間', get: p => p.gust, color: '#e8a33a', axis: 0, fmt: v => v.toFixed(1) + ' m/s' },
    { name: '平均', get: p => p.wind, color: '#2a9d8f', axis: 0, fmt: v => v.toFixed(1) + ' m/s' },
  ], [nice]);
  // 雨量は見やすい幅にまとめる (6時間: 10分 / 24時間: 30分 / 72時間: 1時間 / 7日: 3時間)
  const bin = (wxRangeH <= 6 ? 10 : wxRangeH <= 24 ? 30 : wxRangeH <= 72 ? 60 : 180) * 60000;
  const bins = new Map();
  for (const p of pts) { const k = Math.floor(p.t / bin) * bin; bins.set(k, (bins.get(k) || 0) + (p.rain || 0)); }
  const rpts = [...bins].map(([t, rain]) => ({ t, rain }));
  wxChart($('#wxc-rain'), tmin, tmax, rpts, [
    { name: '雨量', get: p => p.rain, color: '#2f6fd6', axis: 0, bars: true, binMs: bin, fmt: v => v.toFixed(1) + ' mm' },
  ], [nice]);
}
window.addEventListener('resize', () => { if (wxActive()) renderWXCharts(); });

// ---------- 起動・イベント受信 ----------
async function init() {
  const st = await api('/api/state');
  cfg = st.config;
  status = st.status;
  messages = st.messages || [];
  fillForm();
  applyConfigUI();
  if (!objDirty) objLoadFromConfig(objDraft[objCur] && objDraft[objCur].id);
  renderStatus();
  if (!mapCentered) { map.setView([cfg.lat, cfg.lon], 10); mapCentered = true; }
  for (const k of [...stations.keys()]) removeStation(k);
  for (const s of st.stations || []) upsertStation(s);
  pktLog.innerHTML = '';
  for (const p of st.packets || []) addPacket(p);
  renderMessages();
  renderList();
  if (!cfg.callsign) {
    showTab('settings');
    toast('最初にコールサインなどを設定してください', 5000);
  }
}

function connectEvents() {
  const es = new EventSource('/api/events');
  es.onmessage = ev => {
    const m = JSON.parse(ev.data);
    switch (m.type) {
      case 'station': upsertStation(m.data); break;
      case 'remove': removeStation(m.data); break;
      case 'packet': showGateRoute(m.data); addPacket(m.data); break;
      case 'message': upsertMessage(m.data); break;
      case 'status': status = m.data; renderStatus(); if (wxActive()) renderWXState(); break;
      case 'wx': if (wxSnap) { wxSnap.cur = m.data.cur; wxSnap.curT = m.data.t; if (wxActive()) { renderWXCur(); renderWXState(); } } break;
    }
  };
  es.onerror = () => {
    $('#ind-is').className = 'ind bad';
    $('#ind-tnc').className = 'ind bad';
    $('#offline').hidden = false;
  };
  es.onopen = () => {
    $('#offline').hidden = true;
    if (connectEvents.opened) init().catch(() => {});
    connectEvents.opened = true;
  };
}

init().catch(e => toast('サーバーに接続できません: ' + e.message, 8000)).finally(connectEvents);
