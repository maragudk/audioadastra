// Play the sky: the constellation's lines dim, and a playhead sweeps each row from left to right. Every
// star it passes rings a note and bursts into a sparkle, and the lines it has swept light up white again.
// Higher stars ring higher notes, sparkles ring brighter than plain stars. The script also clears
// background stars away from the text, so none read as punctuation.
(() => {
  const svgNS = "http://www.w3.org/2000/svg";

  // clearField hides the background stars that land on the text or inside a constellation letter.
  function clearField() {
    const avoid = [...document.querySelectorAll("[data-sky-avoid]")].map((el) => el.getBoundingClientRect());
    const letters = [...document.querySelectorAll(".sky-constellation [class^='sky-letter-']")]
      .map((el) => el.getBoundingClientRect())
      .filter((b) => b.width > 0);
    for (const el of document.querySelectorAll(".sky-field-star")) {
      const b = el.getBoundingClientRect();
      const x = b.left + b.width / 2;
      const y = b.top + b.height / 2;
      const onText = avoid.some((a) => x > a.left - 16 && x < a.right + 16 && y > a.top - 16 && y < a.bottom + 16);
      const crowding = letters.some((l) => x > l.left - 12 && x < l.right + 12 && y > l.top - 12 && y < l.bottom + 12);
      el.setAttribute("visibility", onText || crowding ? "hidden" : "visible");
    }
  }
  clearField();
  let resizing;
  window.addEventListener("resize", () => {
    clearTimeout(resizing);
    resizing = setTimeout(clearField, 150);
  });
  document.fonts?.ready.then(clearField);

  const button = document.getElementById("sky-play");
  const AudioContext = window.AudioContext || window.webkitAudioContext;
  if (!button || !AudioContext) {
    return;
  }
  button.hidden = false;

  const label = button.querySelector("[data-label]");
  const playIcon = button.querySelector('[data-icon="play"]');
  const stopIcon = button.querySelector('[data-icon="stop"]');

  // A minor pentatonic over two octaves, low to high.
  const scale = [220, 261.63, 293.66, 329.63, 392, 440, 523.25, 587.33, 659.25, 783.99, 880];
  const unitsPerSecond = 9;
  const rowGap = 0.5;
  const loopGap = 1.4;

  let ctx;
  let master;
  let frame;
  let timers = [];

  function setUp() {
    ctx = ctx || new AudioContext();

    master = ctx.createGain();
    master.gain.value = 0.28;

    // A soft echo, so the notes trail off into space.
    const delay = ctx.createDelay(1);
    delay.delayTime.value = 0.36;
    const feedback = ctx.createGain();
    feedback.gain.value = 0.38;
    const tone = ctx.createBiquadFilter();
    tone.type = "lowpass";
    tone.frequency.value = 2400;
    const wet = ctx.createGain();
    wet.gain.value = 0.35;

    master.connect(ctx.destination);
    master.connect(delay);
    delay.connect(tone);
    tone.connect(feedback);
    feedback.connect(delay);
    tone.connect(wet);
    wet.connect(ctx.destination);
  }

  function ring(frequency, when, velocity) {
    const voice = ctx.createGain();
    voice.gain.setValueAtTime(0, when);
    voice.gain.linearRampToValueAtTime(0.5 * velocity, when + 0.006);
    voice.gain.exponentialRampToValueAtTime(0.0001, when + 1.2 + velocity);
    voice.connect(master);

    const partials = [
      [1, "sine", 1],
      [2, "triangle", 0.18],
      [4.02, "sine", 0.06 * velocity],
    ];
    for (const [ratio, type, gain] of partials) {
      const osc = ctx.createOscillator();
      osc.type = type;
      osc.frequency.value = frequency * ratio;
      const level = ctx.createGain();
      level.gain.value = gain;
      osc.connect(level);
      level.connect(voice);
      osc.start(when);
      osc.stop(when + 2.4);
    }
  }

  // burst a short-lived white sparkle over the star, flaring out and collapsing.
  function burst(star) {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      return;
    }
    const x = star.x;
    const y = star.y + star.top;
    const arm = 0.6;
    const w = arm * 0.22;
    const path = document.createElementNS(svgNS, "path");
    path.setAttribute(
      "d",
      `M${x} ${y - arm} Q${x + w} ${y - w} ${x + arm} ${y} Q${x + w} ${y + w} ${x} ${y + arm} ` +
        `Q${x - w} ${y + w} ${x - arm} ${y} Q${x - w} ${y - w} ${x} ${y - arm}Z`,
    );
    path.setAttribute("class", "sky-burst");
    star.el.ownerSVGElement.appendChild(path);
    path.addEventListener("animationend", () => path.remove());
  }

  // score of the visible constellation: one row per line of text, stars ordered left to right.
  function score() {
    const sky = [...document.querySelectorAll(".sky-constellation")].find((svg) => svg.getClientRects().length > 0);
    if (!sky) {
      return null;
    }

    let start = 0;
    const rows = [...sky.querySelectorAll(".sky-row")].map((row) => {
      const top = Number(row.dataset.top);
      const stars = [...row.querySelectorAll(".sky-star")]
        .map((el) => ({ el, top, x: Number(el.dataset.x), y: Number(el.dataset.y) - top }))
        .sort((a, b) => a.x - b.x);
      const from = stars[0].x - 0.5;
      const to = stars[stars.length - 1].x + 0.5;
      const sweep = row.querySelector(".sky-lit-sweep");
      const r = { top, stars, from, to, start, sweep, duration: (to - from) / unitsPerSecond };
      start += r.duration + rowGap;
      return r;
    });

    return { sky, rows, playhead: sky.querySelector(".sky-playhead"), length: start - rowGap + loopGap };
  }

  // light sets how far the white trail behind the playhead reaches in each row.
  function light(s, elapsed) {
    for (const row of s.rows) {
      const reach = Math.min(Math.max(row.from + (elapsed - row.start) * unitsPerSecond, 0), row.to);
      row.sweep.setAttribute("width", elapsed < row.start ? 0 : reach);
    }
  }

  function play(s, startedAt) {
    for (const row of s.rows) {
      for (const star of row.stars) {
        const t = row.start + (star.x - row.from) / unitsPerSecond;
        const step = Math.round((1 - Math.min(Math.max(star.y / 6, 0), 1)) * (scale.length - 1));
        const sparkle = star.el.classList.contains("sky-sparkle");
        ring(scale[step], startedAt + t, sparkle ? 1 : 0.6);

        timers.push(
          setTimeout(() => {
            star.el.classList.add("is-ringing");
            burst(star);
            timers.push(setTimeout(() => star.el.classList.remove("is-ringing"), 380));
          }, (startedAt + t - ctx.currentTime) * 1000),
        );
      }
    }

    s.playhead.classList.remove("hidden");
    const sweep = () => {
      const elapsed = ctx.currentTime - startedAt;
      light(s, elapsed);
      if (elapsed >= s.length) {
        play(s, startedAt + s.length);
        return;
      }
      const row = s.rows.find((r) => elapsed >= r.start && elapsed <= r.start + r.duration);
      if (row) {
        const x = row.from + (elapsed - row.start) * unitsPerSecond;
        s.playhead.setAttribute("x1", x);
        s.playhead.setAttribute("x2", x);
        s.playhead.setAttribute("y1", row.top - 0.9);
        s.playhead.setAttribute("y2", row.top + 6.9);
        s.playhead.style.opacity = "1";
      } else {
        s.playhead.style.opacity = "0";
      }
      frame = requestAnimationFrame(sweep);
    };
    frame = requestAnimationFrame(sweep);
  }

  function start() {
    const s = score();
    if (!s) {
      return;
    }
    setUp();
    ctx.resume();
    s.sky.classList.add("is-playing");
    setPressed(true);
    play(s, ctx.currentTime + 0.1);
  }

  function stop() {
    cancelAnimationFrame(frame);
    timers.forEach(clearTimeout);
    timers = [];
    if (master) {
      master.gain.setTargetAtTime(0, ctx.currentTime, 0.05);
      const old = master;
      setTimeout(() => old.disconnect(), 400);
      master = null;
    }
    document.querySelectorAll(".sky-star.is-ringing").forEach((el) => el.classList.remove("is-ringing"));
    document.querySelectorAll(".sky-burst").forEach((el) => el.remove());
    document.querySelectorAll(".sky-playhead").forEach((el) => el.classList.add("hidden"));
    document.querySelectorAll(".sky-lit-sweep").forEach((el) => el.setAttribute("width", 0));
    document.querySelectorAll(".sky-constellation").forEach((el) => el.classList.remove("is-playing"));
    setPressed(false);
  }

  function setPressed(pressed) {
    button.toggleAttribute("data-playing", pressed);
    label.textContent = pressed ? "Stop the sky" : "Play the sky";
    playIcon.classList.toggle("hidden", pressed);
    stopIcon.classList.toggle("hidden", !pressed);
  }

  button.addEventListener("click", () => (button.hasAttribute("data-playing") ? stop() : start()));
  document.addEventListener("visibilitychange", () => document.hidden && stop());
})();
