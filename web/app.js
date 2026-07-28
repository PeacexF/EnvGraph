import { Force } from "./force.js";

const el = (id) => document.getElementById(id);

const statusEl = el("status");
const detailsEl = el("details");
const detailsBody = el("details-body");

// variables is indexed by name so the details panel can show a variable's full story, which the graph edges alone do not carry.
let variables = new Map();
let cy = null;
let force = null;
let filter = "all";

// Status is never carried by hue alone: ok-green and missing-red are nearly identical under deuteranopia, so each status also gets a glyph and a border.
const GLYPH = { missing: "!", unused: "?", ok: "" };

function css(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function style() {
  return [
    {
      selector: "node",
      style: {
        label: "data(label)",
        "font-family": "system-ui, -apple-system, sans-serif",
        "font-size": 11,
        color: css("--ink-2"),
        "text-valign": "center",
        "text-halign": "center",
        "text-wrap": "ellipsis",
        "text-max-width": 150,
        "border-width": 1,
        "border-color": css("--ring"),
        "transition-property": "opacity, border-width, border-color",
        "transition-duration": "140ms",
      },
    },

    // Variables are the subject of the graph, so they carry their name inside
    // a pill; files and services are context and stay small.
    {
      selector: 'node[type="variable"]',
      style: {
        shape: "round-rectangle",
        width: "label",
        height: 24,
        padding: "8px",
        "background-color": css("--surface"),
        "border-width": 2,
        "font-weight": 600,
        color: css("--ink"),
      },
    },
    {
      selector: 'node[status="ok"]',
      style: { "border-color": css("--ok"), "background-color": mix(css("--ok")) },
    },
    {
      selector: 'node[status="missing"]',
      style: {
        "border-color": css("--missing"),
        "background-color": mix(css("--missing")),
        "border-style": "double",
        "border-width": 4,
      },
    },
    {
      selector: 'node[status="unused"]',
      style: {
        "border-color": css("--unused"),
        "background-color": mix(css("--unused")),
        "border-style": "dashed",
      },
    },

    {
      selector: 'node[type="file"]',
      style: {
        shape: "ellipse",
        width: 13,
        height: 13,
        "background-color": css("--file"),
        "text-valign": "bottom",
        "text-margin-y": 5,
        "font-size": 10,
        color: css("--muted"),
      },
    },
    {
      selector: 'node[type="service"]',
      style: {
        shape: "hexagon",
        width: 26,
        height: 26,
        "background-color": css("--service"),
        "text-valign": "bottom",
        "text-margin-y": 5,
        "font-size": 10,
        "font-weight": 600,
        color: css("--service"),
      },
    },

    {
      selector: "edge",
      style: {
        width: 1.2,
        "line-color": css("--edge"),
        "target-arrow-color": css("--edge"),
        "target-arrow-shape": "triangle",
        "arrow-scale": 0.7,
        "curve-style": "bezier",
        opacity: 0.8,
      },
    },
    { selector: 'edge[relationship="consumed_by"]', style: { "line-style": "dashed" } },
    {
      selector: 'edge[relationship="passed_to"]',
      style: { "line-color": css("--service"), "target-arrow-color": css("--service"), opacity: 0.55 },
    },

    { selector: ".dimmed", style: { opacity: 0.07, "text-opacity": 0.07 } },
    {
      selector: "node.selected",
      style: { "border-color": css("--accent"), "border-width": 3, "border-style": "solid" },
    },
    {
      selector: "edge.selected",
      style: { "line-color": css("--accent"), "target-arrow-color": css("--accent"), opacity: 1, width: 2 },
    },
    { selector: "node.hit", style: { "border-color": css("--accent"), "border-width": 3 } },
  ];
}

// mix fades a status colour toward the surface for a fill that stays behind
// the label rather than competing with it.
function mix(color) {
  return `color-mix(in srgb, ${color} 14%, ${css("--surface")})`;
}

function label(node) {
  if (node.type === "variable") {
    const glyph = GLYPH[node.status] || "";
    return glyph ? `${glyph}  ${node.name}` : node.name;
  }
  if (node.type === "file") {
    const parts = node.name.split("/");
    return parts[parts.length - 1];
  }
  return node.name;
}

function toElements(graph) {
  const nodes = graph.nodes.map((n) => ({
    data: {
      id: n.id,
      label: label(n),
      type: n.type,
      status: n.status || "",
      name: n.name,
      file: n.file || "",
      line: n.line || 0,
      category: n.category || "",
    },
  }));

  // Cytoscape drops edges whose endpoints are absent, so filter defensively.
  const ids = new Set(graph.nodes.map((n) => n.id));
  const edges = graph.edges
    .filter((e) => ids.has(e.from) && ids.has(e.to))
    .map((e, i) => ({
      data: { id: `e${i}`, source: e.from, target: e.to, relationship: e.relationship },
    }));

  return [...nodes, ...edges];
}

function render(doc) {
  variables = new Map(doc.variables.map((v) => [v.name, v]));

  const counts = { ok: 0, missing: 0, unused: 0 };
  for (const v of doc.variables) counts[v.status] = (counts[v.status] || 0) + 1;

  el("count-missing").textContent = counts.missing;
  el("count-unused").textContent = counts.unused;
  el("summary").textContent =
    `${doc.files.length} files · ${doc.variables.length} variables · ` +
    `${counts.ok} ok, ${counts.missing} missing, ${counts.unused} unused`;

  if (force) force.destroy();
  if (cy) cy.destroy();

  cy = cytoscape({
    container: el("graph"),
    elements: toElements(doc.graph),
    style: style(),
    // Seed positions on a circle, then hand over to the simulation. A ring
    // gives the forces something to expand from without any node pair
    // starting coincident.
    layout: { name: "circle", animate: false, padding: 60 },
    wheelSensitivity: 0.2,
    minZoom: 0.15,
    maxZoom: 3,
  });

  cy.on("tap", "node", (event) => select(event.target));
  cy.on("tap", (event) => {
    if (event.target === cy) clearSelection();
  });

  force = new Force(cy);
  force.setEnabled(el("physics").classList.contains("active"));

  applyFilter();

  if (doc.graph.nodes.length === 0) {
    show("No configuration found in this project.");
  } else {
    statusEl.hidden = true;
    cy.fit(undefined, 50);
  }
}

function line(loc) {
  return `${loc.file}:${loc.line}`;
}

function list(items) {
  if (items.length === 0) return `<ul><li class="note">none</li></ul>`;
  return `<ul>${items.map((i) => `<li>${escapeHTML(i)}</li>`).join("")}</ul>`;
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function select(node) {
  const type = node.data("type");
  const name = node.data("name");

  let html = "";

  if (type === "variable") {
    const v = variables.get(name) || {};
    const status = v.status || "";
    const glyph = GLYPH[status];

    html = `<h2>${escapeHTML(name)}</h2>
      <span class="badge ${escapeHTML(status)}">${glyph ? `<span class="glyph">${glyph}</span>` : ""}${escapeHTML(status)}</span>
      <h3>Comes from</h3>
      ${list((v.sources || []).map((s) => {
        if (s.derivedFrom?.length) return `${line(s.location)} — derived from ${s.derivedFrom.join(", ")}`;
        if (s.fromDefault) return `${line(s.location)} — compose default`;
        return line(s.location);
      }))}
      <h3>Passed to</h3>
      ${list((v.passedTo || []).map((p) => `${p.service} — ${line(p.location)}`))}
      <h3>Used in</h3>
      ${list((v.consumers || []).map(line))}`;
  } else {
    const kind = type === "service" ? "Container" : node.data("category") || "File";
    const where = type === "service" ? `${node.data("file")}:${node.data("line")}` : "";

    html = `<h2>${escapeHTML(name)}</h2>
      <p class="note">${escapeHTML(kind)}${where ? ` · ${escapeHTML(where)}` : ""}</p>`;

    if (type === "file") {
      html += `<h3>Defines</h3>${list(related(node, "defines", "out"))}
        <h3>Reads</h3>${list(related(node, "consumed_by", "in"))}`;
    } else {
      html += `<h3>Receives</h3>${list(related(node, "passed_to", "in"))}`;
    }
  }

  detailsBody.innerHTML = html;
  detailsEl.hidden = false;

  cy.elements().removeClass("selected");
  node.closedNeighborhood().addClass("selected");
}

// related collects the variable names on the far side of an edge kind.
function related(node, relationship, direction) {
  const edges = direction === "out"
    ? node.outgoers(`edge[relationship="${relationship}"]`)
    : node.incomers(`edge[relationship="${relationship}"]`);

  return edges
    .map((e) => (direction === "out" ? e.target() : e.source()))
    .filter((n) => n.data("type") === "variable")
    .map((n) => n.data("name"))
    .sort();
}

function clearSelection() {
  detailsEl.hidden = true;
  if (cy) cy.elements().removeClass("selected");
}

function applyFilter() {
  if (!cy) return;

  cy.elements().removeClass("dimmed");
  if (filter === "all") return;

  // Keep the matching variables plus whatever they connect to, so what
  // survives still reads as a flow rather than as loose dots.
  const keep = cy.nodes(`node[status="${filter}"]`).closedNeighborhood();
  cy.elements().difference(keep).addClass("dimmed");
}

function applySearch(term) {
  if (!cy) return;

  cy.elements().removeClass("hit");
  const q = term.trim().toLowerCase();
  if (!q) return;

  const hits = cy.nodes().filter((n) => n.data("name").toLowerCase().includes(q));
  hits.addClass("hit");
  if (hits.length > 0) cy.fit(hits, 80);
}

function show(message, isError = false) {
  statusEl.textContent = message;
  statusEl.classList.toggle("error", isError);
  statusEl.hidden = false;
}

async function load() {
  show("Scanning…");
  clearSelection();

  try {
    const response = await fetch("api/graph");
    if (!response.ok) throw new Error((await response.text()) || response.statusText);
    render(await response.json());
  } catch (err) {
    show(`Could not load the graph: ${err.message}`, true);
  }
}

el("refresh").addEventListener("click", load);
el("close-details").addEventListener("click", clearSelection);
el("search").addEventListener("input", (e) => applySearch(e.target.value));

el("physics").addEventListener("click", () => {
  const button = el("physics");
  const on = !button.classList.contains("active");
  button.classList.toggle("active", on);
  button.setAttribute("aria-pressed", String(on));
  if (force) force.setEnabled(on);
});

for (const button of document.querySelectorAll(".segmented button")) {
  button.addEventListener("click", () => {
    for (const other of document.querySelectorAll(".segmented button")) {
      other.classList.toggle("active", other === button);
    }
    filter = button.dataset.filter;
    applyFilter();
  });
}

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") clearSelection();
});

// Restyling on a theme change keeps the canvas in step with the CSS, which
// Cytoscape cannot read on its own.
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (cy) cy.style(style()).update();
});

fetch("api/meta")
  .then((r) => (r.ok ? r.json() : null))
  .then((meta) => {
    if (meta) el("root").textContent = meta.root;
  })
  .catch(() => {});

load();
