// Pinta los convocados sobre la plantilla del equipo. Todo en el navegador:
// el servidor no procesa imágenes (cero CPU en Cloud Run, sin librerías).
"use strict";

async function drawCallup(canvas, imgURL, layout, names, info = []) {
  const img = new Image();
  img.src = imgURL;
  await img.decode();
  // Se espera a la fuente: si no, el primer pintado sale con la del sistema.
  await document.fonts.load('700 32px "Open Sans"');

  const W = img.naturalWidth, H = img.naturalHeight;
  canvas.width = W;
  canvas.height = H;
  const ctx = canvas.getContext("2d");
  ctx.drawImage(img, 0, 0);

  const cols = layout.columns.length;
  const rows = Math.max(1, Math.ceil(names.length / cols));
  // Con más convocados de los que caben, se comprimen filas y letra a la vez.
  const fit = rows > 1 ? (layout.bottom - layout.top) / (rows - 1) : layout.step;
  const step = Math.min(layout.step, fit);
  const size = layout.font_size * (step / layout.step) * W;

  ctx.fillStyle = layout.color;
  ctx.font = `700 ${size}px "Open Sans", sans-serif`;
  ctx.textBaseline = "middle";
  names.forEach((name, i) => {
    const col = Math.floor(i / rows), row = i % rows;
    ctx.fillText(name, layout.columns[col] * W, (layout.top + row * step) * H);
  });

  // Datos del partido: líneas centradas; la primera (rival) algo más grande.
  const box = layout.info;
  if (box && info.length) {
    ctx.fillStyle = box.color;
    ctx.textAlign = "center";
    info.forEach((line, i) => {
      const size = box.font_size * W * (i === 0 ? 1.2 : 1);
      ctx.font = `700 ${size}px "Open Sans", sans-serif`;
      ctx.fillText(line, box.x * W, (box.top + i * box.step) * H);
    });
    ctx.textAlign = "start";
  }
}

async function shareCanvas(canvas, filename) {
  const blob = await new Promise((ok) => canvas.toBlob(ok, "image/png"));
  const file = new File([blob], filename, { type: "image/png" });
  // En el móvil abre el menú de compartir (WhatsApp); en escritorio, descarga.
  if (navigator.canShare && navigator.canShare({ files: [file] })) {
    try {
      await navigator.share({ files: [file] });
      return;
    } catch (e) {
      if (e.name === "AbortError") return;
    }
  }
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 10000);
}
