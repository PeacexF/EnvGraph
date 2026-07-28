const el = (id) => document.getElementById(id);

const statusEl = el("status");
const detailsEl = el("details");
const detailsBody = el("details-body");

// variables is indexed by name so the details panel can show a variable's
// full story, which the graph edges alone do not carry.
let variables = new Map();
let cy = null;
let filter = "all";

const STYLE = [
  {
    selector: "node",
    style: {
      label: "data(label)",
      "font-size": 11,
      color: "#8a929c",
      "text-valign": "bottom",
      "text-margin-y": 4,
      "text-wrap": "ellipsis",
      "text-max-width": 140,
      width: 16,
      height: 16,
      "background-color": "#64748b",
    },
  },
  { selector: 'node[type="variable"]', style: { shape: "round-rectangle", width: 20, height: 20, "font-weight": 600 } },
  { selector: 'node[type="service"]', style: { shape: "hexagon", width: 24, height: 24, "background-color": "#7c3aed" } },
  { selector: 'node[type="file"]', style: { shape: "ellipse", "background-color": "#64748b" } },
  { selector: 'node[status="ok"]', style: { "background-color": "#16a34a" } },
  { selector: 'node[status="missing"]', style: { "background-color": "#dc2626", width: 24, height: 24 } },
  { selector: 'node[status="unused"]', style: { "background-color": "#d97706" } },
  {
    selector: "edge",
    style: {
      width: 1.2,
      "line-color": "#b6bec8",
      "target-arrow-color": "#b6bec8",
      "target-arrow-shape": "triangle",
      "arrow-scale": 0.8,
      "curve-style": "bezier",
      opacity: 0.75,
    },
  },
  { selector: 'edge[relationship="consumed_by"]', style: { "line-style": "dashed" } },
  { selector: ".dimmed", style: { opacity: 0.08, "text-opacity": 0 } },
  {
    selector: ".highlighted",
    style: { "border-width": 3, "border-color": "#2563eb", "text-opacity": 1, opacity: 1 },
  },
];

// label shortens a file path to its basename; full paths crowd the canvas.
function label(node) {
  if (node.type !== "file") return node.name;
  const parts = node.name.split("/");
  return parts[parts.length - 1];
}

function toElements(graph) {
  const nodes = graph.nodes.map((n) => ({
    data: { id: n.id, label: label(n), type: n.type, status: n.status || "", name: n.name, file: n.file || "", line: n.line || 0 },
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

  if (cy) cy.destroy();

  cy = cytoscape({
    container: el("graph"),
    elements: toElements(doc.graph),
    style: STYLE,
    layout: { name: "cose", animate: false, nodeRepulsion: 9000, idealEdgeLength: 90, padding: 40 },
    wheelSensitivity: 0.2,
  });

  cy.on("tap", "node", (event) => showDetails(event.target));
  cy.on("tap", (event) => {
    if (event.target === cy) hideDetails();
  });

  applyFilter();

  if (doc.graph.nodes.length === 0) {
    show("No configuration found in this project.");
  } else {
    statusEl.hidden = true;
  }
}

function line(loc) {
  return `${loc.file}:${loc.line}`;
}

function list(items) {
  if (items.length === 0) return "<p class='muted'>none</p>";
  return `<ul>${items.map((i) => `<li>${escape(i)}</li>`).join("")}</ul>`;
}

function escape(s) {
  return String(s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function showDetails(node) {
  const type = node.data("type");
  const name = node.data("name");

  let html = "";

  if (type === "variable") {
    const v = variables.get(name) || {};
    const status = v.status || "";

    html = `<h2>${escape(name)}</h2>
      <span class="badge ${escape(status)}">${escape(status)}</span>
      <h3>Sources</h3>
      ${list((v.sources || []).map((s) => {
        if (s.derivedFrom?.length) return `${line(s.location)} — derived from ${s.derivedFrom.join(", ")}`;
        if (s.fromDefault) return `${line(s.location)} — compose default`;
        return line(s.location);
      }))}
      <h3>Passed to</h3>
      ${list((v.passedTo || []).map((p) => `${p.service} (${line(p.location)})`))}
      <h3>Used in</h3>
      ${list((v.consumers || []).map(line))}`;
  } else {
    const heading = type === "service" ? "Service" : "File";
    const defines = connected(node, "defines", "source");
    const receives = connected(node, "passed_to", "target");
    const consumes = connected(node, "consumed_by", "target");

    html = `<h2>${escape(name)}</h2>
      <p class="muted">${escape(heading)}${node.data("file") && type === "service" ? ` · ${escape(node.data("file"))}:${node.data("line")}` : ""}</p>`;

    if (type === "file") {
      html += `<h3>Defines</h3>${list(defines)}<h3>Reads</h3>${list(consumes)}`;
    } else {
      html += `<h3>Receives</h3>${list(receives)}`;
    }
  }

  detailsBody.innerHTML = html;
  detailsEl.hidden = false;

  cy.elements().removeClass("highlighted");
  node.closedNeighborhood().addClass("highlighted");
}

// connected collects the variable names on the far side of an edge kind.
// which says whether this node sits at the edge's source or target.
function connected(node, relationship, which) {
  const edges = which === "source"
    ? node.outgoers(`edge[relationship="${relationship}"]`)
    : node.incomers(`edge[relationship="${relationship}"]`);

  return edges
    .map((e) => (which === "source" ? e.target() : e.source()))
    .filter((n) => n.data("type") === "variable")
    .map((n) => n.data("name"))
    .sort();
}

function hideDetails() {
  detailsEl.hidden = true;
  if (cy) cy.elements().removeClass("highlighted");
}

function applyFilter() {
  if (!cy) return;

  cy.elements().removeClass("dimmed");
  if (filter === "all") return;

  // Keep the matching variables plus whatever they connect to, so the
  // surviving nodes still read as a flow rather than as loose dots.
  const matches = cy.nodes(`node[status="${filter}"]`);
  const keep = matches.closedNeighborhood();
  cy.elements().difference(keep).addClass("dimmed");
}

function applySearch(term) {
  if (!cy) return;

  cy.elements().removeClass("highlighted");
  const q = term.trim().toLowerCase();
  if (!q) return;

  const hits = cy.nodes().filter((n) => n.data("name").toLowerCase().includes(q));
  hits.addClass("highlighted");
  if (hits.length > 0) cy.fit(hits, 60);
}

function show(message, isError = false) {
  statusEl.textContent = message;
  statusEl.classList.toggle("error", isError);
  statusEl.hidden = false;
}

async function load() {
  show("Scanning…");
  hideDetails();

  try {
    const response = await fetch("api/graph");
    if (!response.ok) {
      throw new Error(await response.text() || response.statusText);
    }
    render(await response.json());
  } catch (err) {
    show(`Could not load the graph: ${err.message}`, true);
  }
}

el("refresh").addEventListener("click", load);
el("close-details").addEventListener("click", hideDetails);
el("search").addEventListener("input", (e) => applySearch(e.target.value));

for (const button of document.querySelectorAll(".filters button")) {
  button.addEventListener("click", () => {
    for (const other of document.querySelectorAll(".filters button")) {
      other.classList.toggle("active", other === button);
    }
    filter = button.dataset.filter;
    applyFilter();
  });
}

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") hideDetails();
});

fetch("api/meta")
  .then((r) => (r.ok ? r.json() : null))
  .then((meta) => {
    if (meta) el("root").textContent = meta.root;
  })
  .catch(() => {});

load();
