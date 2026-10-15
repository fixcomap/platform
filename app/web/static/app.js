// Envío de formularios sin salir de la página: si el servidor responde un
// error se muestra como aviso flotante y no se pierde lo escrito; si va bien,
// se navega a donde indique (con su ancla). Sin JS, los formularios funcionan
// igual que siempre (mejora progresiva).
"use strict";

function showToast(msg) {
  const t = document.getElementById("toast");
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(t._timer);
  t._timer = setTimeout(() => { t.hidden = true; }, 6000);
}

// Navega a url; si solo cambia el ancla, recarga y luego baja a ella.
function go(url) {
  const u = new URL(url, location.href);
  if (u.origin !== location.origin) return; // solo dentro de la app
  if (u.pathname + u.search === location.pathname + location.search) {
    if (u.hash) sessionStorage.setItem("scrollTo", u.hash);
    location.reload();
  } else {
    location.assign(u.href);
  }
}

document.addEventListener("submit", async (e) => {
  const form = e.target;
  if (e.defaultPrevented || form.method.toLowerCase() !== "post" || form.dataset.native !== undefined) return;
  e.preventDefault();
  const btn = e.submitter;
  if (btn) btn.disabled = true;
  try {
    const res = await fetch(form.action, {
      method: "POST",
      body: new FormData(form, btn),
      headers: { "X-Requested-With": "fetch" },
    });
    const isJSON = (res.headers.get("Content-Type") || "").includes("application/json");
    const data = isJSON ? await res.json() : null;
    if (!res.ok) {
      showToast((data && data.error) || "No se pudo guardar. Vuelve a intentarlo.");
    } else if (data && data.location) {
      go(data.location);
      return;
    } else {
      go(res.url); // respuestas que no son de la app (p. ej. salir)
      return;
    }
  } catch {
    showToast("Sin conexión: no se pudo enviar. Vuelve a intentarlo cuando tengas señal.");
  }
  if (btn) btn.disabled = false;
});

window.addEventListener("load", () => {
  const h = sessionStorage.getItem("scrollTo");
  if (!h) return;
  sessionStorage.removeItem("scrollTo");
  const el = document.getElementById(h.slice(1));
  if (el) el.scrollIntoView();
});

document.addEventListener("click", (e) => {
  if (e.target.id === "toast") e.target.hidden = true;
});
