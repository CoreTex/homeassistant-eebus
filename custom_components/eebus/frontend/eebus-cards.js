/*
 * EEBUS cards for Home Assistant dashboards.
 *
 * Shipped and registered by the "eebus" integration (no resource to add).
 * Cards: energy flow (bridge), inverter, battery, wallbox, heat pump, grid
 * meter, and an automatic card that picks the right one for a device.
 *
 * Entities are found by device and translation key, so the cards work with
 * any entity IDs and languages. No external libraries.
 */

const VERSION = "0.5.0-dev";

// --- semantic colours (one meaning each, readable on light and dark themes)
const C = {
  pv: "#E8A33D",
  battery: "#2FA88F",
  grid: "#6574D9",
  ev: "#2E9FCB",
  heatpump: "#DD6A4D",
  limit: "#C2452D",
};

// --- strings ---------------------------------------------------------------
const STRINGS = {
  en: {
    pv: "Solar", inverter: "Inverter", battery: "Battery", grid: "Grid", home: "Home",
    wallbox: "Wallbox", heatpump: "Heat pump", meter: "Grid meter",
    import: "Import", export: "Export", charging: "Charging", discharging: "Discharging",
    idle: "Idle", yield_total: "PV yield", produced: "Produced", consumed: "Consumed",
    frequency: "Frequency", stored: "Stored", capacity: "Capacity", cycles: "Cycles",
    temperature: "Temperature", voltage: "Voltage", health: "Health",
    surplus: "PV surplus", limit_active: "Grid operator limit", limit_to: "limited to",
    simulation: "simulation", share: "§14a share", limited: "Limited", limited_to: "Limited to",
    limit: "Consumption limit", feed_in_limit: "Feed-in limit", not_limited: "Not limited",
    mode: "Charge mode", manual_power: "Manual power", plugged: "Vehicle plugged in",
    unplugged: "No vehicle", vehicle_soc: "Vehicle", power: "Power", energy: "Energy",
    feed_in: "Feed-in", limitation: "Feed-in limitation", offline: "Not connected",
    no_device: "Choose an EEBUS device in the card settings.",
    wrong_device: "This device has no data for this card.",
    last_hours: "Last {h} h",
    modes: { auto: "Auto", off: "Off", pv: "PV", min_pv: "Min+PV", fast: "Fast", manual: "Manual" },
    editor: { device_id: "Device", variant: "Layout", graph: "Show history", hours: "Hours of history",
      grid_entity: "Grid power sensor (optional)", pv_entity: "PV power sensor (optional)",
      title: "Title (optional)" },
    variants: { compact: "Compact", standard: "Standard", detailed: "Detailed" },
  },
  de: {
    pv: "PV", inverter: "Wechselrichter", battery: "Batterie", grid: "Netz", home: "Haus",
    wallbox: "Wallbox", heatpump: "Wärmepumpe", meter: "Netzzähler",
    import: "Bezug", export: "Einspeisung", charging: "Lädt", discharging: "Entlädt",
    idle: "Ruht", yield_total: "PV-Ertrag", produced: "Erzeugt", consumed: "Bezogen",
    frequency: "Frequenz", stored: "Gespeichert", capacity: "Kapazität", cycles: "Zyklen",
    temperature: "Temperatur", voltage: "Spannung", health: "Gesundheit",
    surplus: "PV-Überschuss", limit_active: "Netzbetreiber-Limit", limit_to: "begrenzt auf",
    simulation: "Simulation", share: "§14a-Anteil", limited: "Begrenzt", limited_to: "Begrenzt auf",
    limit: "Verbrauchslimit", feed_in_limit: "Einspeiselimit", not_limited: "Nicht begrenzt",
    mode: "Lademodus", manual_power: "Manuelle Leistung", plugged: "Fahrzeug eingesteckt",
    unplugged: "Kein Fahrzeug", vehicle_soc: "Fahrzeug", power: "Leistung", energy: "Energie",
    feed_in: "Einspeisung", limitation: "Einspeisebegrenzung", offline: "Nicht verbunden",
    no_device: "Wähle in den Karteneinstellungen ein EEBUS-Gerät.",
    wrong_device: "Dieses Gerät hat keine Daten für diese Karte.",
    last_hours: "Letzte {h} h",
    modes: { auto: "Auto", off: "Aus", pv: "PV", min_pv: "Min+PV", fast: "Schnell", manual: "Manuell" },
    editor: { device_id: "Gerät", variant: "Darstellung", graph: "Verlauf anzeigen", hours: "Stunden Verlauf",
      grid_entity: "Sensor Netzleistung (optional)", pv_entity: "Sensor PV-Leistung (optional)",
      title: "Titel (optional)" },
    variants: { compact: "Kompakt", standard: "Standard", detailed: "Ausführlich" },
  },
};

function strings(hass) {
  const lang = (hass && (hass.locale?.language || hass.language)) || "en";
  return STRINGS[lang.split("-")[0]] || STRINGS.en;
}

// --- entity lookup --------------------------------------------------------
const UNAVAILABLE = new Set(["unavailable", "unknown", "", undefined]);

/** Map translation_key -> entity_id for all eebus entities of a device. */
function deviceEntities(hass, deviceId) {
  const map = {};
  if (!hass || !hass.entities || !deviceId) return map;
  for (const [entityId, entry] of Object.entries(hass.entities)) {
    if (entry.device_id === deviceId && entry.platform === "eebus" && entry.translation_key) {
      map[entry.translation_key] = entityId;
    }
  }
  return map;
}

function num(hass, entityId) {
  if (!entityId) return null;
  const st = hass.states[entityId];
  if (!st || UNAVAILABLE.has(st.state)) return null;
  const v = Number(st.state);
  return Number.isFinite(v) ? v : null;
}

function str(hass, entityId) {
  if (!entityId) return null;
  const st = hass.states[entityId];
  return !st || UNAVAILABLE.has(st.state) ? null : st.state;
}

function isOn(hass, entityId) {
  return str(hass, entityId) === "on";
}

function eebusDevices(hass) {
  const ids = new Set();
  for (const entry of Object.values(hass.entities || {})) {
    if (entry.platform === "eebus" && entry.device_id) ids.add(entry.device_id);
  }
  return [...ids];
}

/** Category of a device: bridge (has site entities) or the "Device type" sensor. */
function deviceCategory(hass, deviceId) {
  const e = deviceEntities(hass, deviceId);
  if (e.site_grid_power || e.pv_surplus || e.consumption_limit) return "bridge";
  return str(hass, e.remote_category);
}

function deviceName(hass, deviceId) {
  const d = hass.devices && hass.devices[deviceId];
  return (d && (d.name_by_user || d.name)) || "EEBUS";
}

// --- formatting -------------------------------------------------------------
function fmt(hass, value, digits) {
  const lang = (hass.locale && hass.locale.language) || hass.language || "en";
  return new Intl.NumberFormat(lang, { maximumFractionDigits: digits, minimumFractionDigits: digits }).format(value);
}

function power(hass, w) {
  if (w === null || w === undefined) return "–";
  const a = Math.abs(w);
  return a >= 1000 ? `${fmt(hass, w / 1000, a >= 10000 ? 1 : 2)}\u00a0kW` : `${fmt(hass, w, 0)}\u00a0W`;
}

function energy(hass, wh) {
  if (wh === null || wh === undefined) return "–";
  const a = Math.abs(wh);
  if (a >= 1e6) return `${fmt(hass, wh / 1e6, 2)}\u00a0MWh`;
  if (a >= 1000) return `${fmt(hass, wh / 1000, 1)}\u00a0kWh`;
  return `${fmt(hass, wh, 0)}\u00a0Wh`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// --- history (sparklines) ---------------------------------------------------
const historyCache = new Map(); // key -> {at, data}

async function fetchHistory(hass, entityIds, hours) {
  const ids = entityIds.filter(Boolean);
  if (!ids.length) return {};
  const key = `${ids.join(",")}|${hours}`;
  const cached = historyCache.get(key);
  if (cached && Date.now() - cached.at < 5 * 60 * 1000) return cached.data;
  const end = new Date();
  const start = new Date(end.getTime() - hours * 3600 * 1000);
  try {
    const res = await hass.callWS({
      type: "history/history_during_period",
      start_time: start.toISOString(),
      end_time: end.toISOString(),
      entity_ids: ids,
      minimal_response: true,
      no_attributes: true,
      significant_changes_only: false,
    });
    const data = {};
    for (const id of ids) {
      data[id] = (res[id] || [])
        .map((p) => ({ t: (p.lu || p.lc || 0) * 1000, v: Number(p.s) }))
        .filter((p) => Number.isFinite(p.v));
    }
    historyCache.set(key, { at: Date.now(), data });
    return data;
  } catch (err) {
    return {};
  }
}

/** Step-line sparkline as SVG; positive above, negative below a zero line. */
function sparkline(series, { color, hours, height = 56, signed = false, unit = "W" }) {
  const w = 300;
  const h = height;
  const end = Date.now();
  const start = end - hours * 3600 * 1000;
  const pts = (series || []).filter((p) => p.t <= end);
  if (pts.length < 2) return "";
  // extend to "now"
  pts.push({ t: end, v: pts[pts.length - 1].v });
  let min = Math.min(0, ...pts.map((p) => p.v));
  let max = Math.max(0, ...pts.map((p) => p.v));
  if (!signed) min = Math.min(min, 0);
  if (max === min) max = min + 1;
  const x = (t) => ((Math.max(t, start) - start) / (end - start)) * w;
  const y = (v) => h - 2 - ((v - min) / (max - min)) * (h - 4);
  let d = "";
  pts.forEach((p, i) => {
    const px = x(p.t).toFixed(1);
    const py = y(p.v).toFixed(1);
    if (i === 0) d += `M${px},${py}`;
    else d += `H${px}V${py}`;
  });
  const zero = y(0).toFixed(1);
  const area = `${d}V${zero}H${x(pts[0].t).toFixed(1)}Z`;
  const id = `g${Math.random().toString(36).slice(2, 8)}`;
  return `<svg class="spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" role="img" aria-label="${unit}">
      <defs><linearGradient id="${id}" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stop-color="${color}" stop-opacity=".28"/>
        <stop offset="1" stop-color="${color}" stop-opacity="0"/></linearGradient></defs>
      <path d="${area}" fill="url(#${id})"/>
      ${signed ? `<line x1="0" x2="${w}" y1="${zero}" y2="${zero}" class="zero"/>` : ""}
      <path d="${d}" fill="none" stroke="${color}" stroke-width="1.6" vector-effect="non-scaling-stroke"/>
    </svg>`;
}

// --- shared styles ------------------------------------------------------------
const STYLE = `
  :host { display:block; }
  ha-card { padding: 16px; overflow: hidden; }
  .head { display:flex; align-items:center; gap:12px; min-height:40px; }
  .head .icon { --mdc-icon-size: 24px; width:40px; height:40px; border-radius:50%;
    display:grid; place-items:center; flex:none; }
  .head .title { flex:1; min-width:0; }
  .head .name { font-size:16px; font-weight:500; line-height:20px; white-space:nowrap;
    overflow:hidden; text-overflow:ellipsis; color: var(--primary-text-color); }
  .head .sub { font-size:13px; color: var(--secondary-text-color); line-height:18px; }
  .chip { font-size:12px; line-height:20px; padding:0 8px; border-radius:10px; white-space:nowrap;
    color: var(--text-primary-color, #fff); }
  .hero { display:flex; align-items:baseline; gap:8px; margin-top:12px; }
  .big { font-size:32px; font-weight:500; line-height:36px; font-variant-numeric: tabular-nums;
    color: var(--primary-text-color); letter-spacing:-0.5px; }
  .muted { color: var(--secondary-text-color); font-size:13px; }
  .stats { display:grid; grid-template-columns: repeat(auto-fit, minmax(110px, 1fr)); gap:10px 16px;
    margin-top:14px; }
  .stat { cursor:pointer; min-width:0; }
  .stat .k { font-size:12px; color: var(--secondary-text-color); line-height:16px; }
  .stat .v { font-size:15px; font-weight:500; font-variant-numeric: tabular-nums;
    color: var(--primary-text-color); line-height:20px; white-space:nowrap; }
  .clickable { cursor:pointer; }
  .spark { display:block; width:calc(100% + 32px); height:56px; margin:14px -16px -16px; }
  .spark .zero { stroke: var(--divider-color); stroke-width:1; vector-effect:non-scaling-stroke; }
  .graphlabel { font-size:11px; color: var(--secondary-text-color); margin-top:12px; }
  .banner { display:flex; gap:8px; align-items:center; margin:12px 0 0; padding:8px 10px; border-radius:8px;
    font-size:13px; line-height:18px; color: var(--primary-text-color);
    background: color-mix(in srgb, ${C.limit} 14%, transparent); }
  .banner ha-icon { --mdc-icon-size:18px; color:${C.limit}; flex:none; }
  .warn { font-size:13px; color: var(--secondary-text-color); padding:8px 0; }
  .compact { display:flex; align-items:center; gap:12px; }
  .compact .head { flex:1; min-width:0; }
  .compact .big { font-size:20px; line-height:24px; flex:none; }
  .big { white-space:nowrap; }
  @media (prefers-reduced-motion: reduce) { .flow { animation: none !important; } }
`;

// --- base card ------------------------------------------------------------------
class EebusBaseCard extends HTMLElement {
  static cardType = "";
  static icon = "mdi:flash";
  static color = C.grid;

  constructor() {
    super();
    this.attachShadow({ mode: "open" });
    this._history = {};
    this._historyKey = "";
  }

  setConfig(config) {
    if (!config) throw new Error("Invalid configuration");
    this._config = { variant: "standard", graph: true, hours: 24, ...config };
    this._historyKey = "";
    if (this._hass) this._render();
  }

  set hass(hass) {
    const first = !this._hass;
    this._hass = hass;
    // re-render on relevant state changes only
    const sig = this._signature();
    if (first || sig !== this._lastSig) {
      this._lastSig = sig;
      this._render();
    }
    this._maybeLoadHistory();
  }

  get hass() {
    return this._hass;
  }

  getCardSize() {
    return this._config?.variant === "compact" ? 1 : this._config?.graph ? 5 : 3;
  }

  getGridOptions() {
    return this._config?.variant === "compact" ? { columns: 6, rows: 1 } : { columns: 12, min_columns: 6 };
  }

  static getConfigElement() {
    return document.createElement("eebus-card-editor");
  }

  static getStubConfig(hass) {
    const want = this.cardType;
    const dev = eebusDevices(hass).find((id) => {
      const cat = deviceCategory(hass, id);
      return want === "flow" ? cat === "bridge" : cat === want || (want === "battery" && hasBattery(hass, id));
    });
    return { device_id: dev || "", variant: "standard", graph: true, hours: 24 };
  }

  get entities() {
    return deviceEntities(this._hass, this._config.device_id);
  }

  _signature() {
    if (!this._hass || !this._config) return "";
    const ids = Object.values(this.entities).concat([this._config.grid_entity, this._config.pv_entity]);
    return ids.map((id) => (id && this._hass.states[id] ? this._hass.states[id].last_updated + this._hass.states[id].state : "")).join("|");
  }

  /** Entity IDs whose history is drawn; overridden per card. */
  historyEntities() {
    return [];
  }

  _maybeLoadHistory() {
    if (!this._config?.graph || this._config.variant === "compact") return;
    const ids = this.historyEntities().filter(Boolean);
    const key = `${ids.join(",")}|${this._config.hours}`;
    const stale = !this._historyAt || Date.now() - this._historyAt > 5 * 60 * 1000;
    if (!ids.length || (key === this._historyKey && !stale)) return;
    this._historyKey = key;
    this._historyAt = Date.now();
    fetchHistory(this._hass, ids, this._config.hours).then((data) => {
      this._history = data;
      this._render();
    });
  }

  moreInfo(entityId) {
    if (!entityId) return;
    this.dispatchEvent(new CustomEvent("hass-more-info", { detail: { entityId }, bubbles: true, composed: true }));
  }

  _wire() {
    this.shadowRoot.querySelectorAll("[data-entity]").forEach((el) => {
      el.addEventListener("click", () => this.moreInfo(el.dataset.entity));
    });
  }

  _render() {
    if (!this._hass || !this._config) return;
    const t = strings(this._hass);
    let body;
    if (!this._config.device_id) body = `<div class="warn">${t.no_device}</div>`;
    else body = this.renderBody(t) ?? `<div class="warn">${t.wrong_device}</div>`;
    this.shadowRoot.innerHTML = `<style>${STYLE}${this.constructor.extraStyle || ""}</style><ha-card>${body}</ha-card>`;
    this._wire();
    this.afterRender?.();
  }

  header(t, title, sub, chip) {
    const color = this.constructor.color;
    return `<div class="head">
      <div class="icon" style="background: color-mix(in srgb, ${color} 16%, transparent); color:${color}">
        <ha-icon icon="${this.constructor.icon}"></ha-icon></div>
      <div class="title"><div class="name">${escapeHtml(this._config.title || title)}</div>
        ${sub ? `<div class="sub">${sub}</div>` : ""}</div>
      ${chip || ""}</div>`;
  }

  stat(label, value, entityId) {
    return `<div class="stat" ${entityId ? `data-entity="${entityId}"` : ""}><div class="k">${label}</div><div class="v">${value}</div></div>`;
  }

  graph(t, entityId, color, opts = {}) {
    if (!this._config.graph || this._config.variant === "compact" || !entityId) return "";
    const svg = sparkline(this._history[entityId], { color, hours: this._config.hours, ...opts });
    return svg ? `<div class="graphlabel">${t.last_hours.replace("{h}", this._config.hours)}</div>${svg}` : "";
  }

  limitBanner(t) {
    const e = this.entities;
    if (!isOn(this._hass, e.lpc_limited)) return "";
    const st = this._hass.states[e.lpc_limited];
    const w = st?.attributes?.limit_w;
    const share = num(this._hass, e.lpc_distributed);
    return `<div class="banner clickable" data-entity="${e.lpc_limited}"><ha-icon icon="mdi:transmission-tower-off"></ha-icon>
      <span>${w !== undefined && w !== null ? `${t.limited_to} ${power(this._hass, w)}` : t.limited}${share !== null && Math.abs(share - (w ?? NaN)) >= 1 ? `, ${t.share} ${power(this._hass, share)}` : ""}</span></div>`;
  }

  connectedChip(t) {
    const e = this.entities;
    if (e.remote_connected && str(this._hass, e.remote_connected) === "off") {
      return `<span class="chip" style="background: var(--disabled-color, #888)">${t.offline}</span>`;
    }
    return "";
  }
}

function hasBattery(hass, deviceId) {
  const e = deviceEntities(hass, deviceId);
  return !!(e.remote_battery_soc || e.remote_battery_power);
}

// --- energy flow (bridge) ------------------------------------------------------------
class EebusEnergyFlowCard extends EebusBaseCard {
  static cardType = "flow";
  static icon = "mdi:home-lightning-bolt-outline";
  static color = C.pv;
  static extraStyle = `
    .flowwrap { position:relative; margin-top:8px; }
    svg.diagram { width:100%; height:auto; display:block; overflow:visible; }
    .line { fill:none; stroke: var(--divider-color); stroke-width:2; }
    .flow { fill:none; stroke-width:3; stroke-linecap:round; stroke-dasharray: 2 12;
      animation: dash linear infinite; }
    .flow.rev { animation-direction: reverse; }
    @keyframes dash { to { stroke-dashoffset: -28; } }
    .node circle.ring { fill: var(--card-background-color, var(--ha-card-background, #fff)); stroke-width:2.5; }
    .node text { font-family: inherit; text-anchor: middle; fill: var(--primary-text-color); }
    .node .val { font-size:13px; font-weight:500; font-variant-numeric: tabular-nums; }
    .node .lbl { font-size:11px; fill: var(--secondary-text-color); }
    .node { cursor:pointer; }
    .node foreignObject div { display:grid; place-items:center; width:100%; height:100%; }
    .node ha-icon { --mdc-icon-size:22px; }
    .surplus { display:flex; justify-content:space-between; margin-top:8px; font-size:13px; color: var(--secondary-text-color); }
    .surplus b { color: var(--primary-text-color); font-weight:500; font-variant-numeric: tabular-nums; }
  `;

  historyEntities() {
    const e = this.entities;
    return [this._config.grid_entity || e.site_grid_power];
  }

  renderBody(t) {
    const h = this._hass;
    const e = this.entities;
    if (!e.site_grid_power && !e.pv_surplus && !e.consumption_limit) return null;

    const pvId = this._config.pv_entity || e.site_pv_power;
    const pv = num(h, pvId);
    const ac = num(h, e.site_inverter_ac_power);
    const gridId = this._config.grid_entity || e.site_grid_power;
    const grid = num(h, gridId);
    const bat = num(h, e.site_battery_power); // + charging
    const ev = num(h, e.site_ev_charging_power);
    const hp = num(h, e.site_heat_pump_power);

    // source node: PV if known, otherwise the inverter AC side (includes the battery)
    const hybrid = pv === null && ac !== null;
    const source = hybrid ? ac : pv;
    let home = null;
    if (source !== null || grid !== null) {
      home = (source || 0) + (grid || 0) - (hybrid ? 0 : bat || 0) - (ev || 0) - (hp || 0);
      home = Math.max(0, home);
    }

    const W = 320, H = this._config.variant === "compact" ? 170 : 290;
    const cx = W / 2, cy = H / 2;
    const nodes = {
      src: { x: cx, y: 34, color: C.pv, icon: hybrid ? "mdi:solar-power-variant" : "mdi:solar-power", label: hybrid ? t.inverter : t.pv, value: source, entity: hybrid ? e.site_inverter_ac_power : pvId },
      bat: { x: 46, y: cy, color: C.battery, icon: "mdi:battery-high", label: t.battery, value: bat, entity: e.site_battery_power },
      grid: { x: W - 46, y: cy, color: C.grid, icon: "mdi:transmission-tower", label: t.grid, value: grid, entity: gridId },
      home: { x: cx, y: cy, color: "var(--primary-text-color)", icon: "mdi:home-outline", label: t.home, value: home, entity: null },
      ev: { x: 92, y: H - 62, color: C.ev, icon: "mdi:ev-station", label: t.wallbox, value: ev, entity: e.site_ev_charging_power },
      hp: { x: W - 92, y: H - 62, color: C.heatpump, icon: "mdi:heat-pump-outline", label: t.heatpump, value: hp, entity: e.site_heat_pump_power },
    };
    const show = {
      src: source !== null, bat: bat !== null, grid: grid !== null, home: true,
      ev: e.site_ev_charging_power && (ev !== null || this._config.variant === "detailed"),
      hp: e.site_heat_pump_power && (hp !== null || this._config.variant === "detailed"),
    };

    // flows: [from, to, watts] - positive means from -> to
    const flows = [];
    if (show.src) flows.push(["src", "home", source]);
    if (show.grid) flows.push(["grid", "home", grid]);
    if (show.bat) flows.push(hybrid ? ["src", "bat", bat] : ["bat", "home", -(bat || 0)]);
    if (show.ev) flows.push(["home", "ev", ev]);
    if (show.hp) flows.push(["home", "hp", hp]);

    const r = 24;
    const path = (a, b) => {
      const A = nodes[a], B = nodes[b];
      const dx = B.x - A.x, dy = B.y - A.y, len = Math.hypot(dx, dy) || 1;
      const ax = A.x + (dx / len) * r, ay = A.y + (dy / len) * r;
      const bx = B.x - (dx / len) * r, by = B.y - (dy / len) * r;
      return `M${ax.toFixed(1)},${ay.toFixed(1)} L${bx.toFixed(1)},${by.toFixed(1)}`;
    };
    const lines = flows.map(([a, b, w]) => {
      const d = path(a, b);
      const active = w !== null && Math.abs(w) >= 20;
      const color = nodes[a === "home" ? b : a].color;
      // faster for more power: 0.5 s (10 kW+) .. 3 s (small)
      const dur = active ? Math.max(0.5, 3 - Math.log10(Math.abs(w) + 1) * 0.65).toFixed(2) : 0;
      return `<path class="line" d="${d}"/>` +
        (active ? `<path class="flow${w < 0 ? " rev" : ""}" d="${d}" stroke="${color}" style="animation-duration:${dur}s"/>` : "");
    }).join("");

    const nodeSvg = Object.entries(nodes).filter(([k]) => show[k]).map(([k, n]) => {
      const val = k === "bat" && n.value !== null
        ? `${power(h, Math.abs(n.value))}`
        : k === "grid" && n.value !== null ? power(h, Math.abs(n.value)) : power(h, n.value);
      const sub = k === "grid" && n.value !== null ? (n.value >= 0 ? t.import : t.export)
        : k === "bat" && n.value !== null ? (n.value > 20 ? t.charging : n.value < -20 ? t.discharging : t.idle) : n.label;
      const below = n.y > cy + 10 || k === "home";
      const ty = k === "src" ? n.y - r - 18 : below ? n.y + r + 14 : n.y + r + 14;
      return `<g class="node" ${n.entity ? `data-entity="${n.entity}"` : ""}>
        <circle class="ring" cx="${n.x}" cy="${n.y}" r="${r}" stroke="${n.color}"/>
        <foreignObject x="${n.x - r}" y="${n.y - r}" width="${2 * r}" height="${2 * r}"><div style="color:${n.color}"><ha-icon icon="${n.icon}"></ha-icon></div></foreignObject>
        ${k === "src" ? `<text class="val" x="${n.x - r - 8}" y="${n.y + 4}" text-anchor="end" style="text-anchor:end">${val}</text><text class="lbl" x="${n.x + r + 8}" y="${n.y + 4}" style="text-anchor:start">${sub}</text>`
          : `<text class="val" x="${n.x}" y="${ty}">${val}</text><text class="lbl" x="${n.x}" y="${ty + 14}">${sub}</text>`}
      </g>`;
    }).join("");

    const limitOn = isOn(h, e.consumption_limit_active);
    const limitW = num(h, e.consumption_limit);
    const sim = isOn(h, e.lpc_simulated);
    const banner = limitOn
      ? `<div class="banner clickable" data-entity="${e.consumption_limit_active}"><ha-icon icon="mdi:transmission-tower-off"></ha-icon>
          <span>${t.limit_active}: ${power(h, limitW)}${sim ? ` (${t.simulation})` : ""}</span></div>` : "";

    const surplus = num(h, e.pv_surplus);
    const foot = this._config.variant === "compact" || surplus === null ? ""
      : `<div class="surplus clickable" data-entity="${e.pv_surplus}"><span>${t.surplus}</span><b>${power(h, surplus)}</b></div>`;

    return `${this._config.title ? this.header(t, this._config.title) : ""}${banner}
      <div class="flowwrap"><svg class="diagram" viewBox="0 0 ${W} ${H}" role="img" aria-label="${t.home}">${lines}${nodeSvg}</svg></div>
      ${foot}${this.graph(t, gridId, C.grid, { signed: true })}`;
  }
}

// --- inverter --------------------------------------------------------------------------
class EebusInverterCard extends EebusBaseCard {
  static cardType = "inverter";
  static icon = "mdi:solar-power-variant";
  static color = C.pv;

  mainPower() {
    const e = this.entities;
    return e.remote_inverter_ac_power || e.remote_pv_power || e.remote_power;
  }

  historyEntities() {
    return [this.mainPower()];
  }

  renderBody(t) {
    const h = this._hass, e = this.entities;
    const mainId = this.mainPower();
    if (!mainId && !e.remote_inverter_yield_total) return null;
    let p = num(h, mainId);
    if (mainId === e.remote_power && p !== null) p = -p; // MPC: consumption positive
    const name = deviceName(h, this._config.device_id);
    const v = this._config.variant;
    if (v === "compact") {
      return `<div class="compact clickable" data-entity="${mainId}">${this.header(t, name, t.inverter)}<div class="big">${power(h, p)}</div></div>`;
    }
    const stats = [];
    const yieldTotal = num(h, e.remote_inverter_yield_total ?? e.remote_pv_yield_total);
    if (yieldTotal !== null) stats.push(this.stat(t.yield_total, energy(h, yieldTotal), e.remote_inverter_yield_total ?? e.remote_pv_yield_total));
    if (e.remote_pv_power && mainId !== e.remote_pv_power) stats.push(this.stat(t.pv, power(h, num(h, e.remote_pv_power)), e.remote_pv_power));
    if (e.remote_battery_soc) stats.push(this.stat(t.battery, `${fmt(h, num(h, e.remote_battery_soc) ?? 0, 0)} %`, e.remote_battery_soc));
    if (v === "detailed") {
      if (e.remote_inverter_energy_produced) stats.push(this.stat(t.produced, energy(h, num(h, e.remote_inverter_energy_produced)), e.remote_inverter_energy_produced));
      if (e.remote_inverter_energy_consumed) stats.push(this.stat(t.consumed, energy(h, num(h, e.remote_inverter_energy_consumed)), e.remote_inverter_energy_consumed));
      if (e.remote_inverter_frequency) stats.push(this.stat(t.frequency, `${fmt(h, num(h, e.remote_inverter_frequency) ?? 0, 2)} Hz`, e.remote_inverter_frequency));
      if (e.lpp_limited) stats.push(this.stat(t.feed_in_limit, isOn(h, e.lpp_limited) ? power(h, h.states[e.lpp_limited].attributes.limit_w) : t.not_limited, e.lpp_limited));
      if (e.lpc_limited && !isOn(h, e.lpc_limited)) stats.push(this.stat(t.limit, t.not_limited, e.lpc_limited));
    }
    const sub = p === null ? "" : p > 20 ? t.produced : p < -20 ? t.consumed : t.idle;
    return `${this.header(t, name, t.inverter, this.connectedChip(t))}
      <div class="hero clickable" data-entity="${mainId}"><span class="big">${power(h, p)}</span><span class="muted">${sub}</span></div>
      ${this.limitBanner(t)}<div class="stats">${stats.join("")}</div>
      ${this.graph(t, mainId, C.pv, { signed: true })}`;
  }
}

// --- battery -----------------------------------------------------------------------------
class EebusBatteryCard extends EebusBaseCard {
  static cardType = "battery";
  static icon = "mdi:home-battery-outline";
  static color = C.battery;
  static extraStyle = `
    .batrow { display:flex; align-items:center; gap:18px; margin-top:12px; }
    svg.cell { width:44px; height:84px; flex:none; }
    .cell .shell { fill:none; stroke: var(--secondary-text-color); stroke-width:2; }
    .cell .cap { fill: var(--secondary-text-color); }
    .dir { display:flex; align-items:center; gap:4px; font-size:13px; color: var(--secondary-text-color); }
    .dir ha-icon { --mdc-icon-size:18px; }
  `;

  historyEntities() {
    const e = this.entities;
    return [e.remote_battery_soc];
  }

  renderBody(t) {
    const h = this._hass, e = this.entities;
    if (!e.remote_battery_soc && !e.remote_battery_power) return null;
    const soc = num(h, e.remote_battery_soc);
    const p = num(h, e.remote_battery_power); // + charging
    const name = deviceName(h, this._config.device_id);
    const state = p === null ? "" : p > 20 ? t.charging : p < -20 ? t.discharging : t.idle;
    const fillColor = soc === null ? "var(--disabled-color)" : soc < 15 ? C.limit : C.battery;
    const v = this._config.variant;
    if (v === "compact") {
      return `<div class="compact clickable" data-entity="${e.remote_battery_soc}">${this.header(t, name, `${state} ${p !== null ? power(h, Math.abs(p)) : ""}`)}<div class="big">${soc === null ? "–" : `${fmt(h, soc, 0)} %`}</div></div>`;
    }
    const level = soc === null ? 0 : Math.max(0, Math.min(100, soc));
    const fh = (level / 100) * 70;
    const cell = `<svg class="cell" viewBox="0 0 44 84" aria-hidden="true">
        <rect class="cap" x="15" y="0" width="14" height="5" rx="1.5"/>
        <rect class="shell" x="3" y="6" width="38" height="76" rx="6"/>
        <rect x="7" y="${(10 + 70 - fh).toFixed(1)}" width="30" height="${fh.toFixed(1)}" rx="3" fill="${fillColor}"/>
      </svg>`;
    const arrow = p === null || Math.abs(p) <= 20 ? "" : `<ha-icon icon="${p > 0 ? "mdi:arrow-up-bold" : "mdi:arrow-down-bold"}" style="color:${p > 0 ? C.battery : C.pv}"></ha-icon>`;
    const stats = [];
    const stored = num(h, e.remote_battery_energy_stored);
    const cap = num(h, e.remote_battery_capacity);
    if (stored !== null || cap !== null) stats.push(this.stat(t.stored, `${energy(h, stored)}${cap !== null ? ` / ${energy(h, cap)}` : ""}`, e.remote_battery_energy_stored || e.remote_battery_capacity));
    if (v === "detailed") {
      if (num(h, e.remote_battery_cycles) !== null) stats.push(this.stat(t.cycles, fmt(h, num(h, e.remote_battery_cycles), 0), e.remote_battery_cycles));
      if (num(h, e.remote_battery_temperature) !== null) stats.push(this.stat(t.temperature, `${fmt(h, num(h, e.remote_battery_temperature), 1)} °C`, e.remote_battery_temperature));
      if (num(h, e.remote_battery_voltage) !== null) stats.push(this.stat(t.voltage, `${fmt(h, num(h, e.remote_battery_voltage), 1)} V`, e.remote_battery_voltage));
      if (num(h, e.remote_battery_soh) !== null) stats.push(this.stat(t.health, `${fmt(h, num(h, e.remote_battery_soh), 0)} %`, e.remote_battery_soh));
    }
    return `${this.header(t, name, t.battery, this.connectedChip(t))}
      <div class="batrow">${cell}<div>
        <div class="big clickable" data-entity="${e.remote_battery_soc}">${soc === null ? "–" : `${fmt(h, soc, 0)} %`}</div>
        <div class="dir clickable" data-entity="${e.remote_battery_power}">${arrow}<span>${state}${p !== null && Math.abs(p) > 20 ? ` · ${power(h, Math.abs(p))}` : ""}</span></div>
      </div></div>
      <div class="stats">${stats.join("")}</div>
      ${this.graph(t, e.remote_battery_soc, C.battery, { unit: "%" })}`;
  }
}

// --- wallbox ---------------------------------------------------------------------------------
const MODES = ["auto", "off", "pv", "min_pv", "fast", "manual"];
const MODE_ICONS = { auto: "mdi:auto-mode", off: "mdi:power", pv: "mdi:solar-power", min_pv: "mdi:sun-clock-outline", fast: "mdi:lightning-bolt", manual: "mdi:tune-variant" };

class EebusWallboxCard extends EebusBaseCard {
  static cardType = "wallbox";
  static icon = "mdi:ev-station";
  static color = C.ev;
  static extraStyle = `
    .modes { display:grid; grid-template-columns: repeat(6, 1fr); gap:4px; margin-top:14px;
      background: color-mix(in srgb, var(--primary-text-color) 6%, transparent); padding:4px; border-radius:12px; }
    .modes button { all:unset; cursor:pointer; display:flex; flex-direction:column; align-items:center; gap:2px;
      padding:6px 2px; border-radius:9px; font-size:11px; color: var(--secondary-text-color); text-align:center; }
    .modes button ha-icon { --mdc-icon-size:20px; }
    .modes button[aria-pressed="true"] { background: var(--card-background-color, var(--ha-card-background, #fff));
      color:${C.ev}; box-shadow: 0 1px 2px rgba(0,0,0,.18); }
    .modes button:focus-visible { outline: 2px solid ${C.ev}; outline-offset: 1px; }
    .manual { display:flex; align-items:center; gap:12px; margin-top:12px; font-size:13px; color: var(--secondary-text-color); }
    .manual input { flex:1; accent-color:${C.ev}; }
    .manual b { color: var(--primary-text-color); font-weight:500; min-width:64px; text-align:right; font-variant-numeric: tabular-nums; }
  `;

  powerEntity() {
    const e = this.entities;
    return e.ev_charging_power || e.remote_power;
  }

  historyEntities() {
    const e = this.entities;
    return [this.powerEntity() || e.lpc_applied];
  }

  renderBody(t) {
    const h = this._hass, e = this.entities;
    if (!e.charge_mode && !e.lpc_target && !e.ev_charging_power) return null;
    const name = deviceName(h, this._config.device_id);
    const mode = str(h, e.charge_mode);
    const powerId = this.powerEntity();
    const p = num(h, powerId);
    const limited = isOn(h, e.lpc_limited);
    const limitW = limited ? h.states[e.lpc_limited]?.attributes?.limit_w : null;
    const plugged = e.ev_plugged ? isOn(h, e.ev_plugged) : null;
    const sub = plugged === null ? t.wallbox : plugged ? t.plugged : t.unplugged;

    // hero: measured charging power, else the power limit currently applied
    const heroValue = p !== null ? power(h, p) : limited ? power(h, limitW) : mode ? t.modes[mode] || mode : "–";
    const heroSub = p !== null ? t.power : limited ? t.limited : t.mode;

    if (this._config.variant === "compact") {
      return `<div class="compact">${this.header(t, name, mode ? `${t.mode}: ${t.modes[mode] || mode}` : sub)}<div class="big">${heroValue}</div></div>`;
    }
    const modes = e.charge_mode ? `<div class="modes" role="group" aria-label="${t.mode}">${MODES.map((m) =>
      `<button data-mode="${m}" aria-pressed="${m === mode}" title="${t.modes[m]}"><ha-icon icon="${MODE_ICONS[m]}"></ha-icon>${t.modes[m]}</button>`).join("")}</div>` : "";
    const manualW = num(h, e.charge_power_manual);
    const manual = mode === "manual" && e.charge_power_manual ? `<div class="manual"><span>${t.manual_power}</span>
        <input type="range" min="0" max="22000" step="100" value="${manualW ?? 0}" aria-label="${t.manual_power}">
        <b>${power(h, manualW)}</b></div>` : "";
    const stats = [];
    const soc = num(h, e.ev_soc);
    if (soc !== null) stats.push(this.stat(t.vehicle_soc, `${fmt(h, soc, 0)} %`, e.ev_soc));
    if (num(h, e.ev_energy_charged) !== null) stats.push(this.stat(t.energy, energy(h, num(h, e.ev_energy_charged)), e.ev_energy_charged));
    const heroIsLimit = p === null && limited;
    if (e.lpc_limited && !heroIsLimit) stats.push(this.stat(t.limit, limited ? power(h, limitW) : t.not_limited, e.lpc_limited));
    const share = num(h, e.lpc_distributed);
    if (share !== null && !(heroIsLimit && Math.abs(share - limitW) < 1)) stats.push(this.stat(t.share, power(h, share), e.lpc_distributed));
    return `${this.header(t, name, sub, this.connectedChip(t))}
      <div class="hero clickable" data-entity="${powerId || e.lpc_limited || e.charge_mode}"><span class="big">${heroValue}</span><span class="muted">${heroSub}</span></div>
      ${modes}${manual}<div class="stats">${stats.join("")}</div>
      ${this.graph(t, powerId || e.lpc_applied, C.ev)}`;
  }

  afterRender() {
    const e = this.entities;
    this.shadowRoot.querySelectorAll(".modes button").forEach((b) => {
      b.addEventListener("click", (ev) => {
        ev.stopPropagation();
        this._hass.callService("select", "select_option", { entity_id: e.charge_mode, option: b.dataset.mode });
      });
    });
    const range = this.shadowRoot.querySelector(".manual input");
    if (range) {
      const out = this.shadowRoot.querySelector(".manual b");
      range.addEventListener("input", () => { out.textContent = power(this._hass, Number(range.value)); });
      range.addEventListener("change", () => {
        this._hass.callService("number", "set_value", { entity_id: e.charge_power_manual, value: Number(range.value) });
      });
    }
  }
}

// --- heat pump ----------------------------------------------------------------------------------
class EebusHeatPumpCard extends EebusBaseCard {
  static cardType = "heat_pump";
  static icon = "mdi:heat-pump-outline";
  static color = C.heatpump;

  historyEntities() {
    return [this.entities.remote_power];
  }

  renderBody(t) {
    const h = this._hass, e = this.entities;
    if (!e.remote_power && !e.lpc_target) return null;
    const name = deviceName(h, this._config.device_id);
    const p = num(h, e.remote_power);
    if (this._config.variant === "compact") {
      return `<div class="compact clickable" data-entity="${e.remote_power}">${this.header(t, name, t.heatpump)}<div class="big">${power(h, p)}</div></div>`;
    }
    const stats = [];
    if (num(h, e.remote_energy_consumed) !== null) stats.push(this.stat(t.consumed, energy(h, num(h, e.remote_energy_consumed)), e.remote_energy_consumed));
    if (e.lpc_limited) stats.push(this.stat(t.limit, isOn(h, e.lpc_limited) ? power(h, h.states[e.lpc_limited].attributes.limit_w) : t.not_limited, e.lpc_limited));
    if (num(h, e.lpc_distributed) !== null) stats.push(this.stat(t.share, power(h, num(h, e.lpc_distributed)), e.lpc_distributed));
    return `${this.header(t, name, t.heatpump, this.connectedChip(t))}
      <div class="hero clickable" data-entity="${e.remote_power}"><span class="big">${power(h, p)}</span><span class="muted">${t.power}</span></div>
      ${this.limitBanner(t)}<div class="stats">${stats.join("")}</div>
      ${this.graph(t, e.remote_power, C.heatpump)}`;
  }
}

// --- grid meter -----------------------------------------------------------------------------------
class EebusGridCard extends EebusBaseCard {
  static cardType = "grid_meter";
  static icon = "mdi:transmission-tower";
  static color = C.grid;
  static extraStyle = `
    .bidir { position:relative; height:10px; margin-top:14px; border-radius:5px;
      background: color-mix(in srgb, var(--primary-text-color) 8%, transparent); }
    .bidir .mid { position:absolute; left:50%; top:-3px; bottom:-3px; width:2px; background: var(--divider-color); }
    .bidir .bar { position:absolute; top:0; bottom:0; border-radius:5px; }
    .ends { display:flex; justify-content:space-between; font-size:12px; color: var(--secondary-text-color); margin-top:4px; }
  `;

  gridId() {
    return this._config.grid_entity || this.entities.remote_grid_power;
  }

  historyEntities() {
    return [this.gridId()];
  }

  renderBody(t) {
    const h = this._hass, e = this.entities;
    const id = this.gridId();
    if (!id) return null;
    const p = num(h, id);
    const name = deviceName(h, this._config.device_id);
    const dir = p === null ? "" : p >= 0 ? t.import : t.export;
    if (this._config.variant === "compact") {
      return `<div class="compact clickable" data-entity="${id}">${this.header(t, name, dir)}<div class="big">${power(h, p === null ? null : Math.abs(p))}</div></div>`;
    }
    // bar scale: 10 kW full width per side, grows for larger values
    const scale = Math.max(10000, Math.abs(p || 0));
    const pct = p === null ? 0 : (Math.abs(p) / scale) * 50;
    const bar = p === null ? "" : `<div class="bar" style="${p >= 0 ? `left:50%;width:${pct}%;background:${C.grid}` : `right:50%;width:${pct}%;background:${C.pv}`}"></div>`;
    const stats = [];
    if (num(h, e.remote_grid_energy_consumed) !== null) stats.push(this.stat(t.import, energy(h, num(h, e.remote_grid_energy_consumed)), e.remote_grid_energy_consumed));
    if (num(h, e.remote_grid_energy_feed_in) !== null) stats.push(this.stat(t.feed_in, energy(h, num(h, e.remote_grid_energy_feed_in)), e.remote_grid_energy_feed_in));
    if (this._config.variant === "detailed") {
      if (num(h, e.remote_grid_limitation_factor) !== null) stats.push(this.stat(t.limitation, `${fmt(h, num(h, e.remote_grid_limitation_factor), 0)} %`, e.remote_grid_limitation_factor));
      if (num(h, e.remote_grid_frequency) !== null) stats.push(this.stat(t.frequency, `${fmt(h, num(h, e.remote_grid_frequency), 2)} Hz`, e.remote_grid_frequency));
    }
    return `${this.header(t, name, t.meter, this.connectedChip(t))}
      <div class="hero clickable" data-entity="${id}"><span class="big">${power(h, p === null ? null : Math.abs(p))}</span><span class="muted">${dir}</span></div>
      <div class="bidir"><div class="mid"></div>${bar}</div><div class="ends"><span>${t.export}</span><span>${t.import}</span></div>
      <div class="stats">${stats.join("")}</div>
      ${this.graph(t, id, C.grid, { signed: true })}`;
  }
}

// --- automatic card: picks the right card for a device -------------------------------------------------
const BY_CATEGORY = {
  bridge: "eebus-energy-flow-card",
  inverter: "eebus-inverter-card",
  wallbox: "eebus-wallbox-card",
  heat_pump: "eebus-heat-pump-card",
  grid_meter: "eebus-grid-card",
};

class EebusDeviceCard extends HTMLElement {
  setConfig(config) {
    this._config = config;
    this._inner = null;
    if (this._hass) this._update();
  }

  set hass(hass) {
    this._hass = hass;
    this._update();
  }

  _update() {
    const cat = deviceCategory(this._hass, this._config.device_id);
    const tag = BY_CATEGORY[cat] || (hasBattery(this._hass, this._config.device_id) ? "eebus-battery-card" : "eebus-inverter-card");
    if (!this._inner || this._inner.localName !== tag) {
      this._inner = document.createElement(tag);
      this._inner.setConfig(this._config);
      this.replaceChildren(this._inner);
    }
    this._inner.hass = this._hass;
  }

  getCardSize() {
    return this._inner?.getCardSize?.() ?? 3;
  }

  static getConfigElement() {
    return document.createElement("eebus-card-editor");
  }

  static getStubConfig(hass) {
    return { device_id: eebusDevices(hass)[0] || "", variant: "standard", graph: true, hours: 24 };
  }
}

// --- visual editor ------------------------------------------------------------------------------------------
class EebusCardEditor extends HTMLElement {
  setConfig(config) {
    this._config = config;
    this._render();
  }

  set hass(hass) {
    this._hass = hass;
    if (this._form) this._form.hass = hass;
    else this._render();
  }

  async _render() {
    if (!this._hass || !this._config) return;
    if (!customElements.get("ha-form") && window.loadCardHelpers) {
      const helpers = await window.loadCardHelpers();
      helpers.createCardElement({ type: "entities", entities: [] }); // pulls in ha-form
      await customElements.whenDefined("ha-form");
    }
    const t = strings(this._hass);
    const isFlow = deviceCategory(this._hass, this._config.device_id) === "bridge";
    const schema = [
      { name: "device_id", required: true, selector: { device: { integration: "eebus" } } },
      { name: "variant", selector: { select: { mode: "dropdown", options: ["compact", "standard", "detailed"].map((v) => ({ value: v, label: t.variants[v] })) } } },
      { name: "graph", selector: { boolean: {} } },
      { name: "hours", selector: { number: { min: 1, max: 168, mode: "box", unit_of_measurement: "h" } } },
      { name: "title", selector: { text: {} } },
    ];
    if (isFlow) {
      schema.push({ name: "grid_entity", selector: { entity: { domain: "sensor", device_class: "power" } } });
      schema.push({ name: "pv_entity", selector: { entity: { domain: "sensor", device_class: "power" } } });
    }
    if (!this._form) {
      this._form = document.createElement("ha-form");
      this._form.computeLabel = (s) => t.editor[s.name] || s.name;
      this._form.addEventListener("value-changed", (ev) => {
        const config = { ...this._config, ...ev.detail.value };
        for (const k of Object.keys(config)) if (config[k] === "" || config[k] === undefined) delete config[k];
        this._config = config;
        this.dispatchEvent(new CustomEvent("config-changed", { detail: { config }, bubbles: true, composed: true }));
        this._render();
      });
      this.replaceChildren(this._form);
    }
    this._form.hass = this._hass;
    this._form.schema = schema;
    this._form.data = { variant: "standard", graph: true, hours: 24, ...this._config };
  }
}

// --- registration -------------------------------------------------------------------------------------------
const CARDS = [
  ["eebus-energy-flow-card", EebusEnergyFlowCard, { en: ["EEBUS energy flow", "Where your energy comes from and goes to, with grid operator limits."], de: ["EEBUS Energiefluss", "Woher die Energie kommt und wohin sie geht, mit Netzbetreiber-Limits."] }],
  ["eebus-inverter-card", EebusInverterCard, { en: ["EEBUS inverter", "Inverter power, yield and limits."], de: ["EEBUS Wechselrichter", "Leistung, Ertrag und Limits des Wechselrichters."] }],
  ["eebus-battery-card", EebusBatteryCard, { en: ["EEBUS battery", "State of charge, charging and capacity."], de: ["EEBUS Batterie", "Ladezustand, Laden/Entladen und Kapazität."] }],
  ["eebus-wallbox-card", EebusWallboxCard, { en: ["EEBUS wallbox", "Charge mode, charging power and limits."], de: ["EEBUS Wallbox", "Lademodus, Ladeleistung und Limits."] }],
  ["eebus-heat-pump-card", EebusHeatPumpCard, { en: ["EEBUS heat pump", "Heat pump power and §14a dimming."], de: ["EEBUS Wärmepumpe", "Leistung und §14a-Dimmung der Wärmepumpe."] }],
  ["eebus-grid-card", EebusGridCard, { en: ["EEBUS grid meter", "Import and feed-in at the grid connection."], de: ["EEBUS Netzzähler", "Bezug und Einspeisung am Netzanschluss."] }],
  ["eebus-device-card", EebusDeviceCard, { en: ["EEBUS device", "Picks the matching card for any EEBUS device."], de: ["EEBUS Gerät", "Wählt automatisch die passende Karte für ein EEBUS-Gerät."] }],
];

const lang = (navigator.language || "en").split("-")[0] === "de" ? "de" : "en";
for (const [tag, cls, texts] of CARDS) {
  if (!customElements.get(tag)) customElements.define(tag, cls);
  window.customCards = window.customCards || [];
  if (!window.customCards.some((c) => c.type === tag)) {
    window.customCards.push({ type: tag, name: texts[lang][0], description: texts[lang][1], preview: true,
      documentationURL: "https://github.com/CoreTex/homeassistant-eebus#dashboard-cards" });
  }
}
if (!customElements.get("eebus-card-editor")) customElements.define("eebus-card-editor", EebusCardEditor);

console.info(`%c EEBUS cards %c ${VERSION} `, "background:#2FA88F;color:#fff;border-radius:3px 0 0 3px", "background:#6574D9;color:#fff;border-radius:0 3px 3px 0");
