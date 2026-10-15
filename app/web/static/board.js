// Pizarra táctica: campo vertical de 68 x 105 m (portería propia abajo) en
// SVG. Fichas y balón se arrastran; en modo flecha se dibujan movimientos.
// Todo se construye con createElementNS y textContent (sin innerHTML).
"use strict";

const NS = "http://www.w3.org/2000/svg";
const W = 68, L = 105;

function el(name, attrs, parent) {
  const e = document.createElementNS(NS, name);
  for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
  if (parent) parent.appendChild(e);
  return e;
}

function drawPitch(svg) {
  const g = el("g", { class: "bp-lines" }, svg);
  el("rect", { x: 0, y: 0, width: W, height: L, rx: 0.6, class: "bp-grass" }, g);
  el("line", { x1: 0, y1: L / 2, x2: W, y2: L / 2 }, g);
  el("circle", { cx: W / 2, cy: L / 2, r: 9.15 }, g);
  el("circle", { cx: W / 2, cy: L / 2, r: 0.4, class: "bp-spot" }, g);
  for (const top of [true, false]) {
    const y = (d) => (top ? d : L - d);
    // Área grande (40,3 x 16,5), chica (18,3 x 5,5) y punto de penalti (11 m).
    el("rect", { x: (W - 40.3) / 2, y: top ? 0 : L - 16.5, width: 40.3, height: 16.5 }, g);
    el("rect", { x: (W - 18.3) / 2, y: top ? 0 : L - 5.5, width: 18.3, height: 5.5 }, g);
    el("circle", { cx: W / 2, cy: y(11), r: 0.4, class: "bp-spot" }, g);
    el("rect", { x: (W - 7.3) / 2, y: top ? -1.6 : L, width: 7.3, height: 1.6, class: "bp-goal" }, g);
  }
}

function initBoard(svg, state) {
  const defs = el("defs", {}, svg);
  const marker = el("marker", { id: "bp-head", viewBox: "0 0 10 10", refX: 8, refY: 5,
    markerWidth: 5, markerHeight: 5, orient: "auto-start-reverse" }, defs);
  el("path", { d: "M0,0 L10,5 L0,10 z", class: "bp-head" }, marker);
  drawPitch(svg);
  const arrowsG = el("g", { class: "bp-arrows" }, svg);
  const tokensG = el("g", {}, svg);

  let mode = "move", dirty = false, drag = null, drawing = null;
  const status = document.getElementById("board-status");
  const hint = document.getElementById("board-hint");
  const hints = {
    move: "Arrastra las fichas y el balón.",
    arrow: "Arrastra sobre el campo para dibujar una flecha.",
    name: "Toca una ficha para cambiar su nombre.",
    remove: "Toca una ficha o una flecha para quitarla.",
  };
  const changed = () => { dirty = true; status.textContent = "Cambios sin guardar"; };

  const pt = (ev) => {
    const p = svg.createSVGPoint();
    p.x = ev.clientX; p.y = ev.clientY;
    const q = p.matrixTransform(svg.getScreenCTM().inverse());
    return { x: Math.max(0, Math.min(W, q.x)), y: Math.max(0, Math.min(L, q.y)) };
  };

  function render() {
    arrowsG.replaceChildren();
    state.arrows.forEach((a, i) => {
      el("line", { x1: a.from.x, y1: a.from.y, x2: a.to.x, y2: a.to.y, "marker-end": "url(#bp-head)",
        class: "bp-arrow", "data-arrow": i }, arrowsG);
    });
    tokensG.replaceChildren();
    state.tokens.forEach((t, i) => {
      const g = el("g", { class: "bp-token", "data-token": i, transform: `translate(${t.x} ${t.y})` }, tokensG);
      el("circle", { r: 3.4 }, g);
      const label = el("text", { class: "bp-label", y: 0.1 }, g);
      label.textContent = t.label || String(i + 1);
      if (t.name) {
        const name = el("text", { class: "bp-name", y: 6 }, g);
        name.textContent = t.name;
      }
    });
    const b = el("g", { class: "bp-ball", "data-ball": 1, transform: `translate(${state.ball.x} ${state.ball.y})` }, tokensG);
    el("circle", { r: 1.4 }, b);
  }

  svg.addEventListener("pointerdown", (ev) => {
    const tok = ev.target.closest("[data-token]");
    const ball = ev.target.closest("[data-ball]");
    const arrow = ev.target.closest("[data-arrow]");
    if (mode === "remove") {
      if (tok) state.tokens.splice(+tok.dataset.token, 1);
      else if (arrow) state.arrows.splice(+arrow.dataset.arrow, 1);
      else return;
      changed(); render(); return;
    }
    if (mode === "name") {
      if (!tok) return;
      const t = state.tokens[+tok.dataset.token];
      const n = window.prompt("Nombre del jugador", t.name || "");
      if (n !== null) { t.name = n.trim().slice(0, 24); changed(); render(); }
      return;
    }
    if (mode === "arrow") {
      const p = pt(ev);
      drawing = { from: p, to: { ...p } };
      state.arrows.push(drawing);
    } else if (tok) {
      drag = state.tokens[+tok.dataset.token];
    } else if (ball) {
      drag = state.ball;
    } else {
      return;
    }
    svg.setPointerCapture(ev.pointerId);
    ev.preventDefault();
  });
  svg.addEventListener("pointermove", (ev) => {
    if (!drag && !drawing) return;
    const p = pt(ev);
    if (drag) { drag.x = p.x; drag.y = p.y; }
    if (drawing) { drawing.to = p; }
    render();
  });
  const end = () => {
    if (drawing) {
      const d = Math.hypot(drawing.to.x - drawing.from.x, drawing.to.y - drawing.from.y);
      if (d < 2) state.arrows.pop(); // un toque no es una flecha
      else changed();
    }
    if (drag) changed();
    drag = drawing = null;
    render();
  };
  svg.addEventListener("pointerup", end);
  svg.addEventListener("pointercancel", end);

  document.querySelectorAll(".board-tools [data-mode]").forEach((b) => b.addEventListener("click", () => {
    mode = b.dataset.mode;
    document.querySelectorAll(".board-tools [data-mode]").forEach((x) => x.classList.toggle("on", x === b));
    hint.textContent = hints[mode];
    svg.dataset.mode = mode;
  }));
  document.getElementById("board-add").addEventListener("click", () => {
    if (state.tokens.length >= 30) return;
    state.tokens.push({ label: String(state.tokens.length + 1), name: "", x: W / 2, y: L * 0.6 });
    changed(); render();
  });
  document.getElementById("board-undo").addEventListener("click", () => {
    if (state.arrows.length) { state.arrows.pop(); changed(); render(); }
  });
  const nameInput = document.getElementById("board-name");
  nameInput.addEventListener("input", changed);
  document.getElementById("board-save").addEventListener("click", async () => {
    status.textContent = "Guardando…";
    const res = await fetch(location.pathname, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: nameInput.value, state }),
    }).catch(() => null);
    if (res && res.ok) { dirty = false; status.textContent = "Guardado ✓"; }
    else status.textContent = "No se pudo guardar" + (res ? ": " + (await res.text()).trim() : " (sin conexión)");
  });
  window.addEventListener("beforeunload", (e) => { if (dirty) e.preventDefault(); });
  svg.dataset.mode = mode;
  render();
}
