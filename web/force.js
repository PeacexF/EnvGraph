const REPULSION = 14000;  // pushes every pair apart, falls off with distance
const SPRING = 0.02;      // pulls connected nodes toward REST_LENGTH
const REST_LENGTH = 120;
const GRAVITY = 0.006;    // keeps disconnected pieces from drifting away
const DAMPING = 0.86;
const MAX_SPEED = 30;

// Below this total kinetic energy the layout has settled and the loop idles until something disturbs it again.
const SLEEP_ENERGY = 0.04;

export class Force {
  constructor(cy) {
    this.cy = cy;
    this.enabled = true;
    this.frame = null;
    this.bodies = new Map(); // node id -> velocity
    this.pinned = new Set();

    for (const node of cy.nodes()) {
      this.bodies.set(node.id(), { vx: 0, vy: 0 });
    }

    // A dragged node is driven by the cursor, not by the forces, but it still pushes its neighbours around while held.
    cy.on("grab", "node", (e) => {
      this.pinned.add(e.target.id());
      this.wake();
    });
    cy.on("free", "node", (e) => this.pinned.delete(e.target.id()));
    cy.on("drag", "node", () => this.wake());
  }

  start() {
    if (this.frame === null) this.frame = requestAnimationFrame(() => this.tick());
  }

  stop() {
    if (this.frame !== null) cancelAnimationFrame(this.frame);
    this.frame = null;
  }

  destroy() {
    this.stop();
    this.bodies.clear();
  }

  setEnabled(on) {
    this.enabled = on;
    if (on) this.start();
    else this.stop();
  }

  // wake restarts the loop after it has idled, e.g. when a node is dragged.
  wake() {
    if (this.enabled) this.start();
  }

  tick() {
    const energy = this.step();
    this.frame = energy > SLEEP_ENERGY ? requestAnimationFrame(() => this.tick()) : null;
  }

  step() {
    const nodes = this.cy.nodes();
    const count = nodes.length;
    if (count === 0) return 0;

    const positions = new Array(count);
    for (let i = 0; i < count; i++) positions[i] = nodes[i].position();

    const fx = new Float64Array(count);
    const fy = new Float64Array(count);

    let cxSum = 0;
    let cySum = 0;
    for (let i = 0; i < count; i++) {
      cxSum += positions[i].x;
      cySum += positions[i].y;
    }
    const cx = cxSum / count;
    const cy = cySum / count;

    // Every pair repels.
    for (let i = 0; i < count; i++) {
      for (let j = i + 1; j < count; j++) {
        let dx = positions[i].x - positions[j].x;
        let dy = positions[i].y - positions[j].y;
        let d2 = dx * dx + dy * dy;

        // Coincident nodes have no direction to separate along, so nudge them.
        if (d2 < 0.01) {
          dx = Math.random() - 0.5;
          dy = Math.random() - 0.5;
          d2 = 0.01;
        }

        const force = REPULSION / d2;
        const d = Math.sqrt(d2);
        const ux = (dx / d) * force;
        const uy = (dy / d) * force;

        fx[i] += ux;
        fy[i] += uy;
        fx[j] -= ux;
        fy[j] -= uy;
      }
    }

    // Edges pull their endpoints together.
    const index = new Map();
    for (let i = 0; i < count; i++) index.set(nodes[i].id(), i);

    for (const edge of this.cy.edges()) {
      const a = index.get(edge.source().id());
      const b = index.get(edge.target().id());
      if (a === undefined || b === undefined) continue;

      const dx = positions[b].x - positions[a].x;
      const dy = positions[b].y - positions[a].y;
      const d = Math.hypot(dx, dy) || 0.01;
      const pull = SPRING * (d - REST_LENGTH);
      const ux = (dx / d) * pull;
      const uy = (dy / d) * pull;

      fx[a] += ux;
      fy[a] += uy;
      fx[b] -= ux;
      fy[b] -= uy;
    }

    // Gravity toward the centre of mass.
    for (let i = 0; i < count; i++) {
      fx[i] += (cx - positions[i].x) * GRAVITY;
      fy[i] += (cy - positions[i].y) * GRAVITY;
    }

    let energy = 0;

    for (let i = 0; i < count; i++) {
      const node = nodes[i];
      const id = node.id();

      if (this.pinned.has(id)) {
        const body = this.bodies.get(id);
        if (body) {
          body.vx = 0;
          body.vy = 0;
        }
        continue;
      }

      let body = this.bodies.get(id);
      if (!body) {
        body = { vx: 0, vy: 0 };
        this.bodies.set(id, body);
      }

      body.vx = (body.vx + fx[i]) * DAMPING;
      body.vy = (body.vy + fy[i]) * DAMPING;

      const speed = Math.hypot(body.vx, body.vy);
      if (speed > MAX_SPEED) {
        body.vx = (body.vx / speed) * MAX_SPEED;
        body.vy = (body.vy / speed) * MAX_SPEED;
      }

      energy += body.vx * body.vx + body.vy * body.vy;

      node.position({
        x: positions[i].x + body.vx,
        y: positions[i].y + body.vy,
      });
    }

    return energy / count;
  }
}
