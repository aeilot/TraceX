/* Small, interruptible transitions. Hidden remains the source of layout truth. */
(function (root) {
  "use strict";
  const closing = new Map(), entering = new Map();
  const preference = root.matchMedia("(prefers-reduced-motion: reduce)");
  const reduced = () => preference.matches;
  function duration(el, token) {
    const value = root.getComputedStyle(el).getPropertyValue(token).trim();
    const n = parseFloat(value);
    return Number.isFinite(n) ? n * (value.endsWith("ms") ? 1 : 1000) : 0;
  }
  function visible(el, open, {immediate = false} = {}) {
    clearTimeout(closing.get(el));
    closing.delete(el);
    const wasHidden = el.hidden;
    const toast = el.classList.contains("t-toast");
    if (open) {
      el.hidden = false;
      el.inert = false;
      el.removeAttribute("aria-hidden");
      // Commit the closed pose only on first reveal; reversing a close continues
      // from its current interpolated position instead of jumping backwards.
      if (wasHidden && !reduced()) void el.offsetWidth;
      if (toast) el.classList.add("is-open");
      else el.dataset.open = "true";
    } else {
      el.inert = true;
      el.setAttribute("aria-hidden", "true");
      if (toast) el.classList.remove("is-open");
      else el.dataset.open = "false";
      if (wasHidden || immediate || reduced()) { el.hidden = true; return; }
      const wait = duration(el, toast ? "--toast-close" : "--panel-close-dur");
      closing.set(el, setTimeout(() => {
        el.hidden = true;
        closing.delete(el);
      }, wait));
    }
  }
  function enter(el) {
    entering.get(el)?.cancel();
    if (el.hidden || reduced() || !el.animate) return;
    const animation = el.animate([
      {opacity: 0, transform: "translateY(8px)"},
      {opacity: 1, transform: "translateY(0)"},
    ], {duration: duration(el, "--duration-fast"), easing: "cubic-bezier(0.22, 1, 0.36, 1)"});
    entering.set(el, animation);
    animation.onfinish = () => {
      if (entering.get(el) === animation) entering.delete(el);
    };
    animation.oncancel = animation.onfinish;
  }
  function tabs(bar, selector) {
    const buttons = [...bar.querySelectorAll(selector)];
    bar.classList.add("t-tabs");
    buttons.forEach(button => button.classList.add("t-tab"));
    const pill = document.createElement("span");
    pill.className = "t-tabs-pill";
    pill.setAttribute("aria-hidden", "true");
    bar.prepend(pill);
    let placed = false, previousTop = null;
    const update = (animate = true) => {
      const active = buttons.find(button => button.classList.contains("active") || button.classList.contains("selected"));
      if (!active || !bar.offsetWidth) { placed = false; return; }
      const snap = !animate || !placed || previousTop !== active.offsetTop;
      const previousTransition = pill.style.transition;
      if (snap) pill.style.transition = "none";
      pill.style.transform = `translateX(${active.offsetLeft}px)`;
      pill.style.width = `${active.offsetWidth}px`;
      pill.style.height = `${active.offsetHeight}px`;
      pill.style.top = `${active.offsetTop}px`;
      if (snap) { void pill.offsetWidth; pill.style.transition = previousTransition; }
      previousTop = active.offsetTop;
      placed = true;
    };
    const observer = new ResizeObserver(() => update(false));
    observer.observe(bar);
    buttons.forEach(button => observer.observe(button));
    requestAnimationFrame(() => update(false));
    return update;
  }
  const scrollBehavior = () => reduced() ? "instant" : "smooth";
  preference.addEventListener("change", () => {
    if (!reduced()) return;
    for (const animation of entering.values()) animation.cancel();
    entering.clear();
    for (const [el, timer] of closing) { clearTimeout(timer); el.hidden = true; }
    closing.clear();
  });
  root.TraceXMotion = {visible, enter, tabs, scrollBehavior};
})(globalThis);
